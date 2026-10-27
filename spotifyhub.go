package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Spotify phát qua hub (mục 18.3): hubd chạy Soloist (trình nhận Spotify Connect chính thức của Spotify) và PulseAudio như
// tiến trình con trong chính container hubd: Hub Center chỉ có 4 GB RAM, không thêm container. Chỉ chạy khi đã nhập khoá API
// Soloist ở Cài đặt → Nhạc (nhóm cấu hình "soloist", trường api_key). Spotify không cho phân phối lại Soloist: hubd tải bản
// dựng lúc chạy (If-Modified-Since) vào thư mục dữ liệu; bản dựng hết hạn sau 90 ngày (thoát mã 10) thì tải lại.
// Âm thanh: Soloist → PulseAudio module-pipe-sink → FIFO → các đoạn 20 ms; hubMusic.chunk() lấy từ đây trước nhạc MP3 nên
// mọi khu nghe Spotify. Trạng thái và lệnh qua API WebSocket của Soloist (127.0.0.1 trong container, không xác thực).
// Ghép đôi: app Spotify trong LAN thấy hub nhờ avahi của máy chủ (avahi/spotify-connect.service) rồi gọi ZeroConf ở /zc
// của hubd; hubd chuyển tới cổng ZeroConf của Soloist (mDNS của Soloist không ra khỏi mạng Docker).
type hubSpotify struct {
	db                      *DB
	pulse                   *hubPulse // dùng chung với khu Loa nối vào hub (pulse.go)
	dir, url, name, preload string
	pcm                     chan []byte
	wake                    chan struct{}
	loopDone                chan struct{}
	readOnce                sync.Once

	mu       sync.Mutex
	sol      *exec.Cmd
	solDone  chan struct{}
	state    string // nokey | downloading | starting | running | expired | error
	lastErr  string
	lastLine string // dòng log cuối của Soloist, kèm vào lỗi khi nó thoát
	version  string
	days     int  // số ngày còn lại của bản dựng, Soloist in lúc khởi động
	again    bool // dừng Soloist để chạy lại ngay (đổi khoá, Cập nhật Soloist), không tính là lỗi
	stopped  bool
	ws       *websocket.Conn
	zc       int // cổng ZeroConf của Soloist
	now      soloNow
	last     time.Time // lúc nhận đoạn Spotify có tiếng gần nhất
}

var (
	soloistHTTP = &http.Client{Timeout: 10 * time.Minute}
	soloVerRe   = regexp.MustCompile(`soloist (\d[\w.]*) build`)
	soloDaysRe  = regexp.MustCompile(`expires in (\d+) day`)
)

func newHubSpotify(db *DB, pulse *hubPulse) *hubSpotify {
	env := envOr
	arch := map[string]string{"amd64": "x86_64", "arm64": "arm64", "arm": "arm32"}[runtime.GOARCH]
	s := &hubSpotify{db: db, pulse: pulse, dir: env("HUBD_SOLOIST_DIR", "/db/soloist"),
		url:     env("HUBD_SOLOIST_URL", "https://soloist-builds.spotifycdn.com/soloist_release_"+arch+".tar.gz"),
		name:    env("HUBD_SPOTIFY_NAME", "iNIX Smart"),
		preload: "/usr/local/lib/soloist-key.so", pcm: make(chan []byte, 25), wake: make(chan struct{}, 1),
		loopDone: make(chan struct{}), state: "nokey"}
	if v, ok := os.LookupEnv("HUBD_SOLOIST_PRELOAD"); ok { // test: không có thư viện, Soloist giả tự đọc biến môi trường
		s.preload = v
	}
	go s.run()
	return s
}

func (s *hubSpotify) key() string {
	k, _ := s.db.Setting("soloist")["api_key"].(string)
	return strings.TrimSpace(k)
}

func (s *hubSpotify) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *hubSpotify) set(state, err string) {
	s.mu.Lock()
	s.state, s.lastErr = state, err
	s.mu.Unlock()
}

// run: vòng giám sát. Có khoá thì tải (nếu có bản mới) và chạy Soloist tới khi nó thoát; lỗi thì chờ lâu dần rồi chạy lại.
// Không có khoá, lỗi, hết hạn: không giữ PulseAudio.
func (s *hubSpotify) run() {
	defer close(s.loopDone)
	retry := 5 * time.Second
	for {
		s.mu.Lock()
		stopped := s.stopped
		s.again = false
		s.mu.Unlock()
		if stopped {
			s.pulse.release("soloist")
			return
		}
		key := s.key()
		if key == "" {
			s.pulse.release("soloist")
			s.set("nokey", "")
			<-s.wake
			continue
		}
		t0 := time.Now()
		err := s.fetch()
		if err != nil {
			s.set("error", err.Error())
		} else {
			err = s.runSoloist(key)
		}
		s.mu.Lock()
		again := s.again || s.stopped
		s.mu.Unlock()
		if again {
			continue
		}
		if time.Since(t0) > time.Minute {
			retry = 5 * time.Second
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 10 {
			s.set("expired", "Bản Soloist đã hết hạn mà Spotify chưa có bản mới: hub sẽ tự tải lại")
		} else {
			s.mu.Lock()
			s.state = "error"
			if s.lastErr == "" { // giữ lỗi rõ hơn đã đọc từ log (vd. Spotify từ chối khoá)
				s.lastErr = err.Error()
			}
			s.mu.Unlock()
		}
		log.Printf("soloist: dừng (%v), chạy lại sau %v", err, retry)
		s.pulse.release("soloist")
		select {
		case <-s.wake:
		case <-time.After(retry):
		}
		retry = min(2*retry, 10*time.Minute)
	}
}

// fetch: tải bản dựng Soloist khi CDN có bản mới hơn file đang có (If-Modified-Since theo giờ sửa file, đặt bằng
// Last-Modified của CDN). Mất mạng mà đã có bản cũ thì dùng bản cũ: hết hạn thì Soloist tự báo (thoát mã 10).
func (s *hubSpotify) fetch() error {
	bin := filepath.Join(s.dir, "soloist")
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	req, err := http.NewRequest("GET", s.url, nil)
	if err != nil {
		return err
	}
	st, err := os.Stat(bin)
	have := err == nil
	if have {
		req.Header.Set("If-Modified-Since", st.ModTime().UTC().Format(http.TimeFormat))
	}
	keep := func(err error) error {
		if have {
			log.Printf("soloist: không lấy được bản mới (%v), chạy bản đang có", err)
			return nil
		}
		return fmt.Errorf("không tải được Soloist: %w", err)
	}
	res, err := soloistHTTP.Do(req)
	if err != nil {
		return keep(err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 304:
		return nil
	case 200:
	default:
		return keep(errors.New(res.Status))
	}
	s.set("downloading", "")
	if err := untarSoloist(res.Body, bin); err != nil {
		return keep(err)
	}
	if t, err := http.ParseTime(res.Header.Get("Last-Modified")); err == nil {
		os.Chtimes(bin, t, t)
	}
	log.Printf("soloist: đã tải bản dựng %s", res.Header.Get("Last-Modified"))
	return nil
}

// untarSoloist: chỉ lấy file thực thi "soloist" trong gói (bỏ CHANGELOG, giấy phép), ghi file tạm rồi đổi tên.
func untarSoloist(r io.Reader, bin string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return fmt.Errorf("gói không có file soloist: %w", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != "soloist" {
			continue
		}
		tmp := bin + ".new"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(tr, 512<<20))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, bin)
	}
}

// runSoloist: chạy Soloist tới khi nó thoát. Khoá qua biến môi trường, "-k @SOLOIST_API_KEY" chỉ là chữ giữ chỗ:
// soloist-key.so (LD_PRELOAD, soloist/key.c) thay bằng khoá trước main() nên /proc/<pid>/cmdline không có khoá.
// Soloist chỉ nhận môi trường tối thiểu, không thừa hưởng PIN, mật khẩu MQTT của hubd.
func (s *hubSpotify) runSoloist(key string) error {
	data, cache := filepath.Join(s.dir, "data"), filepath.Join(s.dir, "cache")
	for _, d := range []string{data, cache} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	if s.stoppedNow() {
		return nil
	}
	if err := s.pulse.acquire("soloist", s.pulseDied); err != nil {
		return err
	}
	s.readOnce.Do(func() { go s.readLoop(s.pulse.fifo()) })
	os.Remove(filepath.Join(data, "ws.port")) // của lần chạy trước
	c := exec.Command(filepath.Join(s.dir, "soloist"), "-n", s.name, "-k", "@SOLOIST_API_KEY", "-D", data, "-C", cache,
		"-z", "100", "-i", "100", "--ws", "127.0.0.1:0")
	// PULSE_SINK: luôn phát vào sink "hub" (FIFO), kể cả khi PulseAudio có thêm sink ALSA/Bluetooth của khu Loa nối vào hub
	c.Env = []string{"HOME=" + s.dir, "PATH=/usr/bin:/bin", s.pulse.server(), "PULSE_SINK=hub", "SOLOIST_API_KEY=" + key}
	if s.preload != "" {
		if _, err := os.Stat(s.preload); err != nil { // thiếu thì Soloist nhận chữ giữ chỗ làm khoá: báo rõ thay vì "khoá sai"
			return fmt.Errorf("image hubd thiếu %s, cần dựng lại image", s.preload)
		}
		c.Env = append(c.Env, "LD_PRELOAD="+s.preload)
	}
	s.mu.Lock()
	if s.stopped || s.again { // đổi khoá, dừng trong lúc tải
		s.mu.Unlock()
		return nil
	}
	out, err := startLogged(c, func(line string) {
		line = strings.ReplaceAll(line, key, "***") // khoá không bao giờ vào log, kể cả khi Soloist in ra
		log.Printf("soloist: %s", line)
		s.mu.Lock()
		if m := soloVerRe.FindStringSubmatch(line); m != nil {
			s.version = m[1]
		}
		if m := soloDaysRe.FindStringSubmatch(line); m != nil {
			s.days, _ = strconv.Atoi(m[1])
		}
		if strings.Contains(line, "login failed") {
			s.lastErr = "Spotify từ chối khoá API: kiểm tra lại khoá ở Cài đặt → Nhạc"
		}
		s.lastLine = line
		s.mu.Unlock()
	})
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("không chạy được Soloist: %w", err)
	}
	done := make(chan struct{})
	s.sol, s.solDone, s.state, s.lastErr, s.lastLine, s.zc = c, done, "starting", "", "", 0
	s.mu.Unlock()
	go s.connect(data, done)
	err = c.Wait()
	select { // đọc hết log trước khi lấy dòng cuối; tiến trình con của Soloist (crashpad) giữ pipe thì không chờ mãi
	case <-out:
	case <-time.After(2 * time.Second):
	}
	close(done)
	s.mu.Lock()
	ws, last := s.ws, s.lastLine
	s.sol, s.ws, s.zc, s.now = nil, nil, 0, soloNow{}
	s.mu.Unlock()
	if ws != nil {
		ws.Close()
	}
	if err == nil {
		err = errors.New("Soloist tự thoát")
	}
	if last != "" {
		return fmt.Errorf("%w: %s", err, last)
	}
	return err
}

// startLogged: chạy c, mỗi dòng stdout/stderr qua line; kênh trả về đóng khi đã đọc hết (tiến trình và con của nó thoát).
func startLogged(c *exec.Cmd, line func(string)) (<-chan struct{}, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c.Stdout, c.Stderr = w, w
	err = c.Start()
	w.Close()
	if err != nil {
		r.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer r.Close()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line(sc.Text())
		}
	}()
	return done, nil
}

// pulseDied: PulseAudio chết khi Soloist đang chạy: dừng Soloist để vòng giám sát chạy lại cả hai.
func (s *hubSpotify) pulseDied() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sol != nil && !s.stopped {
		log.Printf("soloist: PulseAudio dừng, chạy lại cùng Soloist")
		s.sol.Process.Signal(syscall.SIGTERM)
	}
}

func (s *hubSpotify) stoppedNow() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopped || s.again }

// terminate: SIGTERM để tiến trình dọn dẹp (Soloist xoá ws.port, lưu phiên), quá 5 giây thì SIGKILL. Chờ tới khi thoát.
func terminate(c *exec.Cmd, done <-chan struct{}) {
	c.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		c.Process.Kill()
		<-done
	}
}

// restart: chạy lại Soloist ngay (đổi khoá, Cập nhật Soloist): dừng bản đang chạy, vòng giám sát đọc lại khoá, hỏi CDN.
func (s *hubSpotify) restart() {
	s.mu.Lock()
	s.again = true
	c, done := s.sol, s.solDone
	s.mu.Unlock()
	if c != nil {
		go terminate(c, done)
	}
	s.poke()
}

func (s *hubSpotify) stop() {
	s.mu.Lock()
	s.stopped = true
	c, done := s.sol, s.solDone
	s.mu.Unlock()
	s.poke()
	if c != nil {
		terminate(c, done)
	}
	select {
	case <-s.loopDone:
	case <-time.After(15 * time.Second):
	}
	s.pulse.release("soloist")
}

// connect: chờ Soloist ghi cổng WebSocket (ws.port trong thư mục dữ liệu) rồi nối vào, đọc sự kiện tới khi Soloist dừng.
func (s *hubSpotify) connect(data string, done <-chan struct{}) {
	var c *websocket.Conn
	for c == nil {
		select {
		case <-done:
			return
		case <-time.After(200 * time.Millisecond):
		}
		if p, _ := os.ReadFile(filepath.Join(data, "ws.port")); len(bytes.TrimSpace(p)) > 0 {
			c, _, _ = websocket.DefaultDialer.Dial("ws://127.0.0.1:"+string(bytes.TrimSpace(p)), nil)
		}
	}
	s.mu.Lock()
	select {
	case <-done:
		s.mu.Unlock()
		c.Close()
		return
	default:
	}
	s.ws, s.state = c, "running"
	s.mu.Unlock()
	for {
		var ev soloEvent
		if err := c.ReadJSON(&ev); err != nil {
			return
		}
		if ev.Type == "error" {
			log.Printf("soloist: API báo lỗi: %s", ev.Message)
		}
		s.mu.Lock()
		s.now.apply(ev)
		s.mu.Unlock()
	}
}

// soloEntity: bài/tập podcast trong sự kiện của Soloist (entity envelope của API WebSocket).
type soloEntity struct {
	URI         string `json:"uri"`
	Decorations struct {
		Identity       struct{ Name string } `json:"identity"`
		VisualIdentity struct {
			Cover []struct{ URL, Size string } `json:"cover"`
		} `json:"visual_identity"`
		Parent   *struct{ Entity soloEntity }  `json:"parent"`
		Creators []struct{ Entity soloEntity } `json:"creators"`
		Playback struct {
			DurationMS int64 `json:"duration_ms"`
		} `json:"playback"`
	} `json:"decorations"`
}

// soloEvent: mọi sự kiện Soloist gửi (auth_state, playback_state, track_changed, playback_changed, volume_changed,
// device_changed, options_changed, position_sync, error...). Trường nào có thì áp trường đó.
type soloEvent struct {
	Type     string
	LoggedIn *bool `json:"logged_in"`
	IsActive *bool `json:"is_active"`
	Status   string
	Item     *soloEntity
	Position *struct {
		PositionMS  int64 `json:"position_ms"`
		TimestampMS int64 `json:"timestamp_ms"`
		Speed       float64
	}
	Volume  *int
	Options *struct{ Shuffle bool }
	Message string
}

// soloNow: Spotify đang phát trên hub, như dashboard cần. Pos là vị trí ở mốc at (ms Unix), tăng theo speed khi đang phát.
type soloNow struct {
	LoggedIn bool    `json:"-"`
	Active   bool    `json:"active"` // hub là thiết bị Spotify đang phát của tài khoản
	Status   string  `json:"status"` // idle | playing | paused | buffering
	URI      string  `json:"uri"`
	Track    string  `json:"track"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Art      string  `json:"art"`
	Dur      int64   `json:"dur_ms"`
	Pos      int64   `json:"pos_ms"`
	Volume   int     `json:"volume"`
	Shuffle  bool    `json:"shuffle"`
	at       int64   // mốc của Pos
	speed    float64 // 0 = đứng yên
}

func (n *soloNow) apply(ev soloEvent) {
	if ev.LoggedIn != nil {
		if !*ev.LoggedIn {
			*n = soloNow{}
			return
		}
		n.LoggedIn = true
	}
	if ev.IsActive != nil {
		n.Active = *ev.IsActive
	}
	if ev.Status != "" {
		n.Status = ev.Status
	}
	if ev.Volume != nil {
		n.Volume = *ev.Volume
	}
	if ev.Options != nil {
		n.Shuffle = ev.Options.Shuffle
	}
	if e := ev.Item; e != nil {
		d := e.Decorations
		if e.URI != n.URI {
			n.Pos, n.at, n.speed = 0, time.Now().UnixMilli(), 1
		}
		n.URI, n.Track, n.Dur, n.Album, n.Art, n.Artist = e.URI, d.Identity.Name, d.Playback.DurationMS, "", "", ""
		if d.Parent != nil {
			n.Album = d.Parent.Entity.Decorations.Identity.Name
		}
		var names []string
		for _, c := range d.Creators {
			names = append(names, c.Entity.Decorations.Identity.Name)
		}
		n.Artist = strings.Join(names, ", ")
		for _, size := range []string{"large", "default", "xlarge", "small"} { // bìa vừa cho đĩa nhạc và bìa lớn
			for _, c := range d.VisualIdentity.Cover {
				if c.Size == size && n.Art == "" {
					n.Art = c.URL
				}
			}
		}
	}
	if p := ev.Position; p != nil {
		n.Pos, n.at, n.speed = p.PositionMS, p.TimestampMS, p.Speed
	}
}

// player: Spotify đang phát trên hub (nil khi chưa ghép đôi hoặc chưa có bài), vị trí tính tới bây giờ.
func (s *hubSpotify) player() *soloNow {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.now.LoggedIn || s.now.Track == "" {
		return nil
	}
	n := s.now
	if n.Status == "playing" && n.at > 0 {
		n.Pos += int64(float64(time.Now().UnixMilli()-n.at) * n.speed)
	}
	if n.Dur > 0 {
		n.Pos = min(max(n.Pos, 0), n.Dur)
	}
	return &n
}

// readLoop: FIFO → các đoạn 20 ms. Hàng đầy (0,5 giây) thì chặn, nên PulseAudio và Soloist chỉ chạy kịp tốc độ phát.
func (s *hubSpotify) readLoop(fifo string) {
	for {
		f, err := os.OpenFile(fifo, os.O_RDONLY, 0) // chờ tới khi PulseAudio mở để ghi
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		for {
			b := make([]byte, snapFrames*4)
			if _, err := io.ReadFull(f, b); err != nil {
				break // PulseAudio đóng (dừng): mở lại
			}
			s.pcm <- b
		}
		f.Close()
	}
}

// take: một đoạn Spotify nếu đang có, không chờ. Đoạn toàn số 0 (sink im: đang tải, khoảng lặng) không tính là đang phát.
func (s *hubSpotify) take() []byte {
	select {
	case b := <-s.pcm:
		if len(bytes.Trim(b, "\x00")) == 0 {
			return nil
		}
		s.mu.Lock()
		s.last = time.Now()
		s.mu.Unlock()
		return b
	default:
		return nil
	}
}

// active: Spotify vừa có tiếng (trong 1 giây qua).
func (s *hubSpotify) active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.last) < time.Second
}

func (s *hubSpotify) status(w http.ResponseWriter, r *http.Request) {
	keySet := s.key() != ""
	s.mu.Lock()
	out := map[string]any{"state": s.state, "error": s.lastErr, "version": s.version, "days": s.days, "name": s.name,
		"key_set": keySet, "logged_in": s.now.LoggedIn, "active": s.now.Active}
	s.mu.Unlock()
	writeJSON(w, out)
}

// action: {do: play | pause | next | prev | seek (position_ms) | volume (0-100) | update (Cập nhật Soloist)}.
func (s *hubSpotify) action(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Do         string
		PositionMS *int64 `json:"position_ms"`
		Volume     *int
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b) != nil {
		http.Error(w, "cần JSON {do, ...}", 400)
		return
	}
	c := map[string]any{"type": "command"}
	switch b.Do {
	case "update":
		s.restart()
		w.WriteHeader(202)
		return
	case "play", "pause":
		c["command"] = b.Do
	case "next", "prev":
		c["command"] = "skip_" + b.Do
	case "seek":
		if b.PositionMS == nil || *b.PositionMS < 0 {
			http.Error(w, "cần position_ms ≥ 0", 400)
			return
		}
		c["command"], c["position_ms"] = "seek", *b.PositionMS
	case "volume":
		if b.Volume == nil || *b.Volume < 0 || *b.Volume > 100 {
			http.Error(w, "âm lượng 0-100", 400)
			return
		}
		c["command"], c["volume"] = "set_volume", *b.Volume
	default:
		http.Error(w, "lệnh không hợp lệ", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws == nil || !s.now.LoggedIn {
		http.Error(w, "Spotify trên hub chưa sẵn sàng: cần khoá API và chọn thiết bị "+s.name+" trong app Spotify một lần", 409)
		return
	}
	s.ws.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := s.ws.WriteJSON(c); err != nil {
		http.Error(w, "Soloist không nhận lệnh", 502)
		return
	}
	w.WriteHeader(204)
}

// zeroconf: app Spotify trong LAN gọi ZeroConf (getInfo, addUser) ở /zc cổng 8080 của hubd (avahi của máy chủ quảng bá),
// hubd chuyển tới Soloist. Không cần PIN, như mọi loa Spotify Connect: addUser chỉ nhận thông tin đăng nhập do app Spotify mã
// hoá cho đúng thiết bị này.
func (s *hubSpotify) zeroconf(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.zc == 0 && s.sol != nil {
		s.zc = zeroconfPort(s.sol.Process.Pid)
	}
	p := s.zc
	s.mu.Unlock()
	if p == 0 {
		http.Error(w, "Spotify trên hub chưa chạy", 503)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(p)}
	(&httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) { pr.SetURL(target) }}).ServeHTTP(w, r)
}

// zeroconfPort: cổng ZeroConf của Soloist (ngẫu nhiên, Soloist không có tuỳ chọn đặt cổng): socket TCP đang nghe ngoài
// loopback mà tiến trình Soloist giữ. hubd và Soloist chung mạng của container nên đọc được qua /proc.
func zeroconfPort(pid int) int {
	inode := map[string]bool{}
	fds, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	for _, f := range fds {
		if l, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, f.Name())); err == nil && strings.HasPrefix(l, "socket:[") {
			inode[strings.Trim(l[len("socket:"):], "[]")] = true
		}
	}
	for _, n := range []string{"tcp", "tcp6"} {
		b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/net/%s", pid, n))
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line) // sl local_address rem_address st ... inode (cột 10)
			if len(f) < 10 || f[3] != "0A" || !inode[f[9]] {
				continue
			}
			ip, port, _ := strings.Cut(f[1], ":")
			if ip == "0100007F" || ip == "00000000000000000000000001000000" { // 127.0.0.1, ::1: API WebSocket
				continue
			}
			if p, err := strconv.ParseUint(port, 16, 16); err == nil {
				return int(p)
			}
		}
	}
	return 0
}
