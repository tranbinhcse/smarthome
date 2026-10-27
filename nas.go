package main

import (
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

// nasCfg: thư mục NAS đã chọn ở Cài đặt (đủ địa chỉ và chia sẻ thì mới là một nguồn nhạc).
func (m *hubMusic) nasCfg() (nasCfg, bool) {
	s := m.db.Setting("music")
	str := func(k string) string { v, _ := s[k].(string); return strings.TrimSpace(v) }
	c := nasCfg{Host: str("nas_host"), User: str("nas_user"), Pass: str("nas_pass"), Share: str("nas_share"), Path: strings.Trim(str("nas_path"), "/")}
	return c, nasHostRe.MatchString(c.Host) && nasShareRe.MatchString(c.Share) && (c.Path == "" || fs.ValidPath(c.Path))
}

// nasBrowse: chọn thư mục nhạc. {host, user, pass} → {shares}; thêm share (+ path) → {dirs, mp3} trong thư mục đó.
// pass để trống = mật khẩu đã lưu (ô mật khẩu trên Cài đặt chỉ hiện "đã lưu", không có giá trị).
func (m *hubMusic) nasBrowse(w http.ResponseWriter, r *http.Request) {
	var b struct{ Host, User, Pass, Share, Path string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil {
		http.Error(w, "cần JSON {host, user, pass, share, path}", 400)
		return
	}
	if !nasHostRe.MatchString(b.Host) || b.Share != "" && !nasShareRe.MatchString(b.Share) || b.Path != "" && (!fs.ValidPath(b.Path) || b.Share == "") ||
		strings.HasPrefix(b.Share, ".") {
		http.Error(w, "địa chỉ NAS (tên máy hoặc IP, không kèm cổng), chia sẻ hoặc thư mục không hợp lệ", 400)
		return
	}
	if b.Pass == "" {
		b.Pass, _ = m.db.Setting("music")["nas_pass"].(string)
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
