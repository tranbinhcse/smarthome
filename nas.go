package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
)

// NAS trong nhà (mục 18.3): một thư mục nhạc trên chia sẻ SMB làm nguồn nhạc của hub. hubd tự đọc bằng Go thuần (go-smb2),
// không gắn ổ mạng trên máy chủ, không cần quyền đặc biệt cho container. Cấu hình ở kho cấu hình nhóm "music": nas_host,
// nas_user, nas_pass (bí mật: trình duyệt chỉ thấy nas_pass_set), nas_share, nas_path.
type nasCfg struct{ Host, User, Pass, Share, Path string }

// nasConn: một phiên SMB tới NAS (test thay bằng thư mục thường qua nasDial).
type nasConn interface {
	shares() ([]string, error)
	fsys(share, dir string) (fs.FS, error)
	close()
}

var (
	nasHostRe  = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`) // tên máy hoặc IPv4; cổng luôn là 445
	nasShareRe = regexp.MustCompile(`^[^/\\:*?"<>|\x00-\x1f]{1,80}$`)
)

var nasDial = func(c nasCfg) (nasConn, error) {
	tcp, err := net.DialTimeout("tcp", net.JoinHostPort(c.Host, "445"), 5*time.Second)
	if err != nil {
		return nil, err
	}
	tcp.SetDeadline(time.Now().Add(15 * time.Second)) // bắt tay; bỏ hạn sau khi vào được
	s, err := (&smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: c.User, Password: c.Pass}}).Dial(tcp)
	if err != nil {
		tcp.Close()
		return nil, err
	}
	tcp.SetDeadline(time.Time{})
	return &smbConn{s: s, tcp: tcp}, nil
}

type smbConn struct {
	s      *smb2.Session
	tcp    net.Conn
	mounts []*smb2.Share
}

func (c *smbConn) shares() ([]string, error) { return c.s.ListSharenames() }

func (c *smbConn) fsys(share, dir string) (fs.FS, error) {
	sh, err := c.s.Mount(share)
	if err != nil {
		return nil, err
	}
	c.mounts = append(c.mounts, sh)
	if dir == "" {
		dir = "."
	}
	if _, err := fs.Stat(sh.DirFS("."), dir); err != nil {
		return nil, err
	}
	return sh.DirFS(dir), nil
}

func (c *smbConn) close() {
	for _, sh := range c.mounts {
		sh.Umount()
	}
	c.s.Logoff()
	c.tcp.Close()
}

// nasBind: dấu ràng buộc mật khẩu đã lưu với đúng máy và tài khoản lúc lưu (lệnh save). Đổi địa chỉ hay tài khoản mà không
// nhập lại mật khẩu (vd. qua API cấu hình chung) thì dấu không khớp và hub không gửi mật khẩu cũ tới máy mới: NTLM gửi phản
// hồi bẻ được ngoại tuyến. Không biết mật khẩu thì không tạo được dấu cho máy khác. Khoá *_secret: trình duyệt không thấy.
func nasBind(host, user, pass string) string {
	s := sha256.Sum256([]byte(strings.ToLower(host) + "\x00" + user + "\x00" + pass))
	return hex.EncodeToString(s[:])
}

// nasCfg: thư mục NAS đã chọn ở Cài đặt. configured = đủ địa chỉ và chia sẻ (là một nguồn nhạc); trusted = mật khẩu đã lưu
// đúng cho địa chỉ và tài khoản này (dấu nasBind khớp).
func (m *hubMusic) nasCfg() (c nasCfg, configured, trusted bool) {
	s := m.db.Setting("music")
	str := func(k string) string { v, _ := s[k].(string); return strings.TrimSpace(v) }
	c = nasCfg{Host: str("nas_host"), User: str("nas_user"), Pass: str("nas_pass"), Share: str("nas_share"), Path: strings.Trim(str("nas_path"), "/")}
	configured = nasHostRe.MatchString(c.Host) && nasShareRe.MatchString(c.Share) && (c.Path == "" || fs.ValidPath(c.Path))
	return c, configured, str("nas_bind_secret") != "" && str("nas_bind_secret") == nasBind(c.Host, c.User, c.Pass)
}

// savedPass: mật khẩu đã lưu, chỉ khi hỏi đúng máy và tài khoản đã lưu cùng nó.
func (m *hubMusic) savedPass(host, user string) (string, bool) {
	c, _, trusted := m.nasCfg()
	return c.Pass, trusted && strings.EqualFold(c.Host, host) && c.User == user
}

// nasBrowse: chọn thư mục nhạc. {host, user, pass} → {shares}; thêm share (+ path) → {dirs, mp3} trong thư mục đó.
// {do: save, host, user, pass, share, path} lưu thư mục đã chọn; {do: unset} bỏ. pass để trống = mật khẩu đã lưu, chỉ khi
// đúng máy và tài khoản đã lưu (ô mật khẩu trên Cài đặt chỉ hiện "đã lưu", không có giá trị).
func (m *hubMusic) nasBrowse(w http.ResponseWriter, r *http.Request) {
	var b struct{ Do, Host, User, Pass, Share, Path string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil {
		http.Error(w, "cần JSON {host, user, pass, share, path}", 400)
		return
	}
	if b.Do == "unset" {
		if err := m.db.PutSetting("music", map[string]any{"nas_share": nil, "nas_path": nil}); err != nil {
			http.Error(w, "không lưu được", 500)
			return
		}
		m.refresh(true)
		w.WriteHeader(204)
		return
	}
	if !nasHostRe.MatchString(b.Host) || b.Share != "" && !nasShareRe.MatchString(b.Share) || b.Path != "" && (!fs.ValidPath(b.Path) || b.Share == "") ||
		strings.HasPrefix(b.Share, ".") {
		http.Error(w, "địa chỉ NAS (tên máy hoặc IP, không kèm cổng), chia sẻ hoặc thư mục không hợp lệ", 400)
		return
	}
	if b.Pass == "" {
		p, ok := m.savedPass(b.Host, b.User)
		if !ok {
			http.Error(w, "Nhập mật khẩu NAS (mật khẩu đã lưu chỉ dùng cho đúng địa chỉ và tài khoản lúc lưu)", 400)
			return
		}
		b.Pass = p
	}
	if b.Do == "save" {
		if b.Share == "" {
			http.Error(w, "chọn chia sẻ trên NAS", 400)
			return
		}
		err := m.db.PutSetting("music", map[string]any{"nas_host": b.Host, "nas_user": b.User, "nas_pass": b.Pass, "nas_share": b.Share,
			"nas_path": b.Path, "nas_bind_secret": nasBind(b.Host, b.User, b.Pass)})
		if err != nil {
			http.Error(w, "không lưu được", 500)
			return
		}
		m.refresh(true)
		w.WriteHeader(204)
		return
	}
	conn, err := nasDial(nasCfg{Host: b.Host, User: b.User, Pass: b.Pass})
	if err != nil {
		log.Printf("nhạc: nối NAS %s: %v", b.Host, err)
		http.Error(w, "Không nối được NAS: "+err.Error(), 502)
		return
	}
	defer conn.close()
	if b.Share == "" {
		names, err := conn.shares()
		if err != nil {
			http.Error(w, "NAS không cho xem danh sách chia sẻ: "+err.Error(), 502)
			return
		}
		out := []string{}
		for _, n := range names {
			if !strings.HasSuffix(n, "$") { // IPC$, ADMIN$, C$: chia sẻ hệ thống
				out = append(out, n)
			}
		}
		sort.Strings(out)
		writeJSON(w, map[string]any{"shares": out})
		return
	}
	fsys, err := conn.fsys(b.Share, b.Path)
	if err != nil {
		http.Error(w, "Không mở được thư mục: "+err.Error(), 502)
		return
	}
	es, err := fs.ReadDir(fsys, ".")
	if err != nil {
		http.Error(w, "Không đọc được thư mục: "+err.Error(), 502)
		return
	}
	dirs, mp3s := []string{}, 0
	for _, e := range es {
		switch {
		case strings.HasPrefix(e.Name(), "."):
		case e.IsDir():
			dirs = append(dirs, e.Name())
		case strings.EqualFold(path.Ext(e.Name()), ".mp3"):
			mp3s++
		}
	}
	sort.Strings(dirs)
	writeJSON(w, map[string]any{"dirs": dirs, "mp3": mp3s})
}
