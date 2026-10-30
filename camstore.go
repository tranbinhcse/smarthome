package main

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Camera nối thẳng vào hub (không qua đầu ghi): mỗi camera một dòng JSON trong bảng cameras.
// Mật khẩu lưu cùng bản ghi nhưng không bao giờ ra trình duyệt: view() chỉ trả pass_set.
type camRec struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Zone     string `json:"zone"`
	Kind     string `json:"kind"` // hik_cam | rtsp
	Host     string `json:"host"`
	HTTPPort int    `json:"http_port"`
	RTSPPort int    `json:"rtsp_port"`
	User     string `json:"user"`
	Pass     string `json:"pass"`
	MainPath string `json:"main_path"`
	SubPath  string `json:"sub_path"`
}

var (
	camKinds = map[string]bool{"hik_cam": true, "rtsp": true}
	nvrCamID = regexp.MustCompile(`^cam_ch[0-9]+$`) // id kênh đầu ghi, id camera nối thẳng không được trùng
	camSlugX = regexp.MustCompile(`[^a-z0-9]+`)
	viMap    = buildViMap()
)

const (
	viLower = "àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵ"
	viBase  = "aaaaaaaaaaaaaaaaaeeeeeeeeeeeiiiiiooooooooooooooooouuuuuuuuuuuyyyyy"
)

func buildViMap() map[rune]rune {
	m := map[rune]rune{'đ': 'd'}
	b := []rune(viBase)
	for i, r := range []rune(viLower) {
		m[r] = b[i]
	}
	return m
}

// camSlug: chữ thường, bỏ dấu tiếng Việt, ký tự khác chữ-số thành "_".
func camSlug(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		if x, ok := viMap[r]; ok {
			r = x
		}
		sb.WriteRune(r)
	}
	return strings.Trim(camSlugX.ReplaceAllString(sb.String(), "_"), "_")
}

func camPort(p *int, def int, label string) error {
	if *p == 0 {
		*p = def
	}
	if *p < 1 || *p > 65535 {
		return errors.New("Cổng " + label + " phải từ 1 đến 65535")
	}
	return nil
}

func camPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.ContainsAny(p, " \t\r\n") && !strings.Contains(p, "://")
}

func (c *camRec) validate(zoneOK func(string) bool) error {
	if n := len([]rune(c.Name)); n < 1 || n > 40 {
		return errors.New("Tên camera phải từ 1 đến 40 ký tự")
	}
	if !nvrHost.MatchString(c.Host) || strings.Contains(c.Host, ":") {
		return errors.New("Địa chỉ camera không hợp lệ (chỉ IP hoặc tên máy, không có http://, đường dẫn hay cổng)")
	}
	if err := camPort(&c.HTTPPort, 80, "HTTP"); err != nil {
		return err
	}
	if err := camPort(&c.RTSPPort, 554, "RTSP"); err != nil {
		return err
	}
	if !camKinds[c.Kind] {
		return errors.New("Loại camera không hợp lệ")
	}
	if zoneOK != nil && !zoneOK(c.Zone) {
		return errors.New("Khu vực không tồn tại")
	}
	if c.Kind == "rtsp" {
		if !camPath(c.MainPath) || (c.SubPath != "" && !camPath(c.SubPath)) {
			return errors.New("Đường dẫn luồng phải bắt đầu bằng /, không có khoảng trắng hay ://")
		}
	} else {
		c.MainPath, c.SubPath = "", ""
	}
	return nil
}

func camList(db *DB) []camRec {
	out := []camRec{}
	rows, err := db.sql.Query(`SELECT data FROM cameras`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var c camRec
		if rows.Scan(&raw) == nil && json.Unmarshal([]byte(raw), &c) == nil {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name || out[i].Name == out[j].Name && out[i].ID < out[j].ID })
	return out
}

func camGet(db *DB, id string) (camRec, bool) {
	var raw string
	var c camRec
	if db.sql.QueryRow(`SELECT data FROM cameras WHERE id = ?`, id).Scan(&raw) != nil {
		return c, false
	}
	return c, json.Unmarshal([]byte(raw), &c) == nil
}

// camSave: id rỗng = thêm mới (id sinh từ tên), có id = sửa. zoneOK kiểm khu vực theo cấu hình đang chạy.
// Pass rỗng khi sửa = giữ mật khẩu cũ, trừ khi đổi Host (cùng quy tắc nvrPatch: không gửi mật khẩu cũ tới máy khác).
func camSave(db *DB, c camRec, zoneOK func(string) bool) (camRec, error) {
	if err := c.validate(zoneOK); err != nil {
		return c, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if c.ID == "" {
		base := "cam_" + camSlug(c.Name)
		if base == "cam_" || nvrCamID.MatchString(base) {
			base += "_cam"
		}
		c.ID = base
		for n := 2; ; n++ {
			if _, taken := camGet(db, c.ID); !taken {
				break
			}
			c.ID = base + "_" + strconv.Itoa(n)
		}
	} else {
		old, ok := camGet(db, c.ID)
		if !ok {
			return c, errors.New("Không tìm thấy camera")
		}
		if c.Pass == "" && c.Host == old.Host {
			c.Pass = old.Pass
		}
	}
	raw, _ := json.Marshal(c)
	_, err := db.sql.Exec(`INSERT INTO cameras(id, data) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET data = excluded.data`, c.ID, string(raw))
	return c, err
}

func camDelete(db *DB, id string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	r, err := db.sql.Exec(`DELETE FROM cameras WHERE id = ?`, id)
	if err != nil {
		return false
	}
	n, _ := r.RowsAffected()
	return n > 0
}

// view: JSON cho trình duyệt. Không có "pass", chỉ "pass_set".
func (c camRec) view() map[string]any {
	return map[string]any{
		"id": c.ID, "name": c.Name, "zone": c.Zone, "kind": c.Kind, "host": c.Host,
		"http_port": c.HTTPPort, "rtsp_port": c.RTSPPort, "user": c.User,
		"main_path": c.MainPath, "sub_path": c.SubPath, "pass_set": c.Pass != "",
	}
}
