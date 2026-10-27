package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/go-mp3"
)

// Nhạc trên hub (mục 18.3 thiết kế): hubd tự phát file MP3 trong các thư mục nhạc (bộ nhớ hub, USB, thư mục mạng gắn
// vào hub) tới các đầu nhận Snapcast cạnh amply (snapcast.go). Giải mã MP3 bằng Go thuần, đổi về 44,1 kHz của luồng.
// Trình duyệt chỉ chọn file có trong thư viện đã quét, không bao giờ gửi đường dẫn; mở file qua os.Root nên không thoát
// được khỏi thư mục nhạc. Khu = một đầu nhận: tên, âm lượng, tắt tiếng lưu ở kho cấu hình nhóm "music" (khoá zone:<id>).
type musicFile struct {
	ID     string `json:"id"` // "<số thứ tự thư mục nhạc>/<đường dẫn trong thư mục>"
	Title  string `json:"title"`
	Folder string `json:"folder"`
}

type musicZone struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Volume    int    `json:"volume"`
	Muted     bool   `json:"muted"`
}

type hubMusic struct {
	dirs []string // thư mục nhạc trên hub (HUBD_MUSIC_DIRS); thêm thư mục NAS nếu đã chọn ở Cài đặt (nas.go)
	db   *DB
	snap *snapServer

	mu       sync.Mutex
	files    []musicFile
	byID     map[string]musicFile
	rootStat []map[string]any // tình trạng từng nguồn ở lần quét gần nhất
	scanned  time.Time
	scanning bool
	queue    []musicFile
	index    int
	playing  bool
	src      *musicSource

	spot     *hubSpotify // Spotify phát qua hub (spotifyhub.go)
	spotHold bool        // người dùng chọn MP3 khi Spotify còn tiếng: bỏ tiếng Spotify tới khi nó ngừng hẳn
}

var macRe = regexp.MustCompile(`^([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}$`)

const musicMaxFiles = 20000 // ponytail: quét cả cây mỗi lần; thư viện lớn hơn thì cần chỉ mục lưu SQLite

func mountMusic(mux *http.ServeMux, h *Hub, a *Auth) *hubMusic {
	dirs := os.Getenv("HUBD_MUSIC_DIRS")
	if dirs == "" {
		dirs = "/music:/media" // thư mục nhạc của hub, USB tự gắn dưới /media
	}
	m := &hubMusic{dirs: filepath.SplitList(dirs), db: h.db}
	m.snap = newSnapServer(func(id string) (int, bool) { z := m.zone(id); return z.Volume, z.Muted })
	m.spot = newHubSpotify(h.db)
	go m.run()
	if addr := os.Getenv("HUBD_SNAP_LISTEN"); addr != "" {
		if ln, err := net.Listen("tcp", addr); err != nil {
			log.Printf("nhạc: không mở được cổng đầu nhận %s: %v", addr, err)
		} else {
			go m.snap.serve(ln)
		}
	}
	mux.HandleFunc("GET /api/music/library", a.Require(m.library))
	mux.HandleFunc("GET /api/music/state", a.Require(m.state))
	mux.HandleFunc("POST /api/music/cmd", a.Require(m.cmd))
	mux.HandleFunc("POST /api/music/nas", a.Require(m.nasBrowse))
	mux.HandleFunc("GET /api/spotify/hub", a.Require(m.spot.status))
	mux.HandleFunc("POST /api/spotify/hub", a.Require(m.spot.action))
	mux.HandleFunc("/zc", m.spot.zeroconf) // ghép đôi Spotify Connect từ app trong LAN: không PIN
	return m
}

// musicRoot: một nguồn nhạc (thư mục trên hub, thư mục trên NAS). open trả hệ thống file của nguồn và hàm đóng.
type musicRoot struct {
	name string
	open func() (fs.FS, func(), error)
}

// roots: thư mục trên hub (qua os.Root: không thoát ra ngoài, kể cả qua symlink) rồi thư mục NAS. Số thứ tự là phần đầu id file.
func (m *hubMusic) roots() []musicRoot {
	var rs []musicRoot
	for _, d := range m.dirs {
		rs = append(rs, musicRoot{d, func() (fs.FS, func(), error) {
			r, err := os.OpenRoot(d)
			if err != nil {
				return nil, nil, err
			}
			return r.FS(), func() { r.Close() }, nil
		}})
	}
	if c, ok, trusted := m.nasCfg(); ok {
		rs = append(rs, musicRoot{"NAS //" + c.Host + "/" + c.Share + strings.TrimSuffix("/"+c.Path, "/"), func() (fs.FS, func(), error) {
			if !trusted { // địa chỉ/tài khoản đổi mà chưa nhập lại mật khẩu: không gửi mật khẩu cũ tới máy mới
				return nil, nil, errors.New("cần nhập lại mật khẩu NAS ở Cài đặt cho địa chỉ này")
			}
			conn, err := nasDial(c)
			if err != nil {
				return nil, nil, err
			}
			f, err := conn.fsys(c.Share, c.Path)
			if err != nil {
				conn.close()
				return nil, nil, err
			}
			return f, conn.close, nil
		}})
	}
	return rs
}

// refresh: quét lại các nguồn khi quá 30 giây (thấy USB vừa cắm) hoặc khi bị yêu cầu. Quét ngoài khoá m.mu: quét NAS đi
// qua mạng, giữ khoá thì nhạc đang phát bị ngắt. Mọi file .mp3 thường (không symlink), bỏ file/thư mục ẩn (._, .Trashes...).
func (m *hubMusic) refresh(force bool) {
	m.mu.Lock()
	if m.scanning || !force && time.Since(m.scanned) < 30*time.Second {
		m.mu.Unlock()
		return
	}
	m.scanning = true
	m.mu.Unlock()
	files, byID, stat := []musicFile{}, map[string]musicFile{}, []map[string]any{}
	for i, r := range m.roots() {
		fsys, done, err := r.open()
		st := map[string]any{"path": r.name, "ok": err == nil}
		if stat = append(stat, st); err != nil {
			st["error"] = err.Error()
			continue
		}
		fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || len(files) >= musicMaxFiles {
				return nil // thư mục không đọc được (USB vừa rút, NAS chập chờn): bỏ qua
			}
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !strings.EqualFold(path.Ext(p), ".mp3") {
				return nil
			}
			dir := path.Dir(p)
			if dir == "." {
				dir = ""
			}
			f := musicFile{ID: strconv.Itoa(i) + "/" + p, Title: strings.TrimSuffix(path.Base(p), path.Ext(p)), Folder: dir}
			files, byID[f.ID] = append(files, f), f
			return nil
		})
		done()
	}
	m.mu.Lock()
	m.files, m.byID, m.rootStat, m.scanned, m.scanning = files, byID, stat, time.Now(), false
	m.mu.Unlock()
}

func (m *hubMusic) library(w http.ResponseWriter, r *http.Request) {
	m.refresh(r.URL.Query().Get("rescan") == "1")
	m.mu.Lock()
	files, stat := m.files, m.rootStat
	m.mu.Unlock()
	writeJSON(w, map[string]any{"files": files, "roots": stat})
}

// zone: tên, âm lượng, tắt tiếng đã lưu của một đầu nhận (chưa lưu: 100%, bật tiếng).
func (m *hubMusic) zone(id string) musicZone {
	z := musicZone{ID: id, Volume: 100}
	if v, ok := m.db.Setting("music")["zone:"+id].(map[string]any); ok {
		z.Name, _ = v["name"].(string)
		if n, ok := v["volume"].(float64); ok {
			z.Volume = int(n)
		}
		z.Muted, _ = v["muted"].(bool)
	}
	return z
}

func (m *hubMusic) zones() []musicZone {
	out, seen := []musicZone{}, map[string]bool{}
	for _, c := range m.snap.clients() {
		z := m.zone(c.ID)
		z.Connected = true
		if z.Name == "" { // chưa đặt tên: theo hostID (--hostID khu-cafe); hostID là MAC (mặc định snapclient) thì theo hostname
			z.Name = c.ID
			if macRe.MatchString(c.ID) {
				z.Name = c.Name
			}
		}
		out, seen[c.ID] = append(out, z), true
	}
	for k := range m.db.Setting("music") {
		if id, ok := strings.CutPrefix(k, "zone:"); ok && !seen[id] {
			out = append(out, m.zone(id))
		}
	}
	return out
}

func (m *hubMusic) state(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	out := map[string]any{"playing": m.playing, "index": m.index, "queue": len(m.queue), "file": nil, "pos_ms": 0, "dur_ms": 0, "source": ""}
	switch { // nguồn đang ra loa
	case m.spot.active() && !m.spotHold:
		out["source"] = "spotify"
	case m.playing:
		out["source"] = "file"
	}
	if m.index < len(m.queue) {
		out["file"] = m.queue[m.index]
	}
	if s := m.src; s != nil {
		out["pos_ms"], out["dur_ms"] = s.frames*1000/snapRate, s.total*1000/snapRate
	}
	m.mu.Unlock()
	out["zones"] = m.zones()
	out["spotify"] = m.spot.player() // Spotify đang phát trên hub (Soloist), null khi chưa có
	writeJSON(w, out)
}

// cmd: {do: play (files + index = hàng đợi mới; không có files = phát tiếp) | pause | next | prev | stop | zone}.
func (m *hubMusic) cmd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Do, ID string
		Files  []string
		Index  int
		Volume *int
		Muted  *bool
		Name   *string
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&b) != nil {
		http.Error(w, "cần JSON {do, ...}", 400)
		return
	}
	if b.Do == "zone" {
		m.setZone(w, b.ID, b.Volume, b.Muted, b.Name)
		return
	}
	if b.Do == "play" && len(b.Files) > 0 {
		m.refresh(false) // thư viện để kiểm file người dùng chọn
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if b.Do == "play" && m.spot.active() {
		m.spotHold = true // chọn MP3 khi Spotify còn phát qua hub: MP3 thắng tới khi Spotify ngừng
	}
	switch b.Do {
	case "play":
		if len(b.Files) == 0 {
			if len(m.queue) == 0 {
				http.Error(w, "chưa chọn bài", 409)
				return
			}
			m.playing = true
			if m.src == nil {
				m.openCur()
			}
			break
		}
		if len(b.Files) > 2000 || b.Index < 0 || b.Index >= len(b.Files) {
			http.Error(w, "hàng đợi tối đa 2000 bài, index trong hàng đợi", 400)
			return
		}
		q := make([]musicFile, len(b.Files))
		for i, id := range b.Files {
			f, ok := m.byID[id]
			if !ok {
				http.Error(w, "bài không có trong thư viện nhạc", 400)
				return
			}
			q[i] = f
		}
		m.queue, m.index, m.playing = q, b.Index, true
		m.closeSrc()
		m.openCur()
	case "pause":
		m.playing = false
	case "stop":
		m.playing, m.index = false, 0
		m.closeSrc()
	case "next", "prev":
		switch {
		case b.Do == "next" && m.index+1 < len(m.queue):
			m.index++
		case b.Do == "prev" && m.index > 0 && (m.src == nil || m.src.frames < 3*snapRate): // đã nghe quá 3 giây: về đầu bài
			m.index--
		}
		m.closeSrc()
		m.openCur()
	default:
		http.Error(w, "lệnh nhạc không hợp lệ", 400)
		return
	}
	w.WriteHeader(204)
}

func (m *hubMusic) setZone(w http.ResponseWriter, id string, vol *int, muted *bool, name *string) {
	known := m.snap.has(id)
	if _, ok := m.db.Setting("music")["zone:"+id]; ok {
		known = true
	}
	if id == "" || !known || vol != nil && (*vol < 0 || *vol > 100) || name != nil && utf8.RuneCountInString(*name) > 80 {
		http.Error(w, "khu không hợp lệ (âm lượng 0–100, tên tối đa 80 ký tự)", 400)
		return
	}
	z := m.zone(id)
	if vol != nil {
		z.Volume = *vol
	}
	if muted != nil {
		z.Muted = *muted
	}
	if name != nil {
		z.Name = strings.TrimSpace(*name)
	}
	if err := m.db.PutSetting("music", map[string]any{"zone:" + id: map[string]any{"name": z.Name, "volume": z.Volume, "muted": z.Muted}}); err != nil {
		http.Error(w, "không lưu được", 500)
		return
	}
	m.snap.settings(id, z.Volume, z.Muted)
	w.WriteHeader(204)
}

// openCur: mở bài ở vị trí index; file hỏng hoặc vừa bị xoá thì bỏ qua sang bài sau. Gọi khi giữ m.mu.
func (m *hubMusic) openCur() bool {
	for ; m.index < len(m.queue); m.index++ {
		s, err := m.open(m.queue[m.index])
		if err == nil {
			m.src = s
			return true
		}
		log.Printf("nhạc: bỏ qua %s: %v", m.queue[m.index].ID, err)
	}
	m.playing, m.index = false, max(0, len(m.queue)-1)
	return false
}

func (m *hubMusic) closeSrc() {
	m.src = nil
}

// open: đọc cả bài vào bộ nhớ (MP3 vài MB) rồi giải mã: file trên NAS đọc qua mạng, đọc dần thì mạng chập chờn làm ngắt tiếng.
// ponytail: mở bài kế tiếp khi giữ m.mu nên đổi bài trên NAS có thể lặng một nhịp; cần thì đọc trước bài kế ở goroutine riêng.
func (m *hubMusic) open(f musicFile) (*musicSource, error) {
	i, rel, _ := strings.Cut(f.ID, "/")
	n, err := strconv.Atoi(i)
	rs := m.roots()
	if err != nil || n < 0 || n >= len(rs) {
		return nil, errors.New("nguồn nhạc không có")
	}
	fsys, done, err := rs[n].open()
	if err != nil {
		return nil, err
	}
	defer done()
	file, err := fsys.Open(rel)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(file, 200<<20))
	file.Close()
	if err != nil {
		return nil, err
	}
	dec, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return &musicSource{dec: dec, step: float64(dec.SampleRate()) / snapRate,
		total: dec.Length() / 4 * snapRate / int64(dec.SampleRate())}, nil
}

// musicSource: một bài đang phát. go-mp3 trả PCM 16 bit stereo ở tần số của file; đổi về snapRate bằng nội suy tuyến tính.
type musicSource struct {
	dec           *mp3.Decoder
	buf           []byte  // PCM gốc chưa dùng hết
	pos, step     float64 // vị trí đọc (khung gốc, tính từ buf[0]); số khung gốc mỗi khung ra
	frames, total int64   // khung đã phát / tổng khung, ở snapRate
	eof           bool
}

// read điền out (bội số 4 byte) bằng khung snapRate; trả số khung, ít hơn len(out)/4 nghĩa là hết bài.
func (s *musicSource) read(out []byte) int {
	n := 0
	for n < len(out)/4 {
		i := int(s.pos)
		for !s.eof && len(s.buf)/4 < i+2 {
			tmp := make([]byte, 4608)
			k, err := io.ReadFull(s.dec, tmp)
			s.buf = append(s.buf, tmp[:k]...)
			s.eof = err != nil
		}
		if len(s.buf)/4 < i+2 {
			break
		}
		frac := s.pos - float64(i)
		for c := 0; c < 4; c += 2 {
			a, b := float64(int16(le.Uint16(s.buf[i*4+c:]))), float64(int16(le.Uint16(s.buf[i*4+4+c:])))
			le.PutUint16(out[n*4+c:], uint16(int16(a+(b-a)*frac)))
		}
		n++
		if s.pos += s.step; i > 16384 { // bỏ phần PCM gốc đã phát qua
			s.buf, s.pos = s.buf[i*4:], s.pos-float64(i)
		}
	}
	s.frames += int64(n)
	return n
}

// chunk: 20 ms nhạc kế tiếp; hết bài thì sang bài sau trong hàng đợi, hết hàng đợi thì dừng (phần thiếu là im lặng).
func (m *hubMusic) chunk() ([]byte, bool) {
	sp := m.spot.take()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case sp != nil && !m.spotHold: // Spotify phát trên "iNIX Smart": ra mọi khu, nhạc MP3 dừng
		m.playing = false
		return sp, true
	case sp == nil && m.spotHold && !m.spot.active():
		m.spotHold = false
	}
	if !m.playing || m.src == nil && !m.openCur() {
		return nil, false
	}
	out, n := make([]byte, snapFrames*4), 0
	for n < snapFrames {
		n += m.src.read(out[n*4:])
		if n < snapFrames {
			m.closeSrc()
			if m.index+1 >= len(m.queue) {
				m.playing = false
				break
			}
			m.index++
			if !m.openCur() {
				break
			}
		}
	}
	return out, n > 0
}

// run: mỗi 20 ms gửi các đoạn tới hạn. Dấu thời gian nối tiếp nhau đúng 20 ms; trễ quá (máy bận) hoặc đang dừng thì
// đặt lại mốc theo giờ hiện tại thay vì dồn một loạt đoạn cũ.
func (m *hubMusic) run() {
	t := time.NewTicker(20 * time.Millisecond)
	next := m.snap.now()
	for range t.C {
		now := m.snap.now()
		if next < now-200*time.Millisecond {
			next = now
		}
		for next <= now {
			pcm, ok := m.chunk()
			if !ok {
				next = now
				break
			}
			m.snap.write(pcm, next)
			next += 20 * time.Millisecond
		}
	}
}
