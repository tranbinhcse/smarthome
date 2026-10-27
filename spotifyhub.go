package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Spotify phát qua hub (mục 18.3, giai đoạn 2): hubd chạy librespot (v0.8.0, dựng tĩnh) như tiến trình con, hiện là
// thiết bị Spotify "iNIX Smart". librespot ghi PCM 44,1 kHz/16 bit stereo vào một FIFO; hubMusic.chunk() lấy từ đây
// trước nhạc MP3 nên mọi khu nghe Spotify. Không bật mDNS (container không ở mạng host): đăng nhập một lần bằng OAuth
// của librespot (--enable-oauth --oauth-port 0): librespot in "Browse to: <url>" rồi đọc địa chỉ trả về từ stdin; hub
// đưa url lên Cài đặt và chuyển địa chỉ người dùng dán vào. Thông tin đăng nhập librespot lưu ở thư mục cache, lần sau tự vào.
type hubSpotify struct {
	bin, cache, fifo, name string
	pcm                    chan []byte
	readOnce               sync.Once

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	gen     int    // mỗi lần chạy librespot; tiến trình cũ thoát thì không đụng trạng thái của lần mới
	state   string // off | starting | login | running
	url     string // địa chỉ đăng nhập "Browse to" khi đang chờ người dùng
	lastErr string
	last    time.Time // lúc nhận đoạn Spotify gần nhất
	retry   time.Duration
	stopped bool
}

var (
	browseRe = regexp.MustCompile(`Browse to:\s*(https://accounts\.spotify\.com/\S+)`)
	// địa chỉ trả về librespot nhận: http://127.0.0.1[:cổng]/login?..., chỉ ký tự in được, không khoảng trắng hay xuống dòng
	redirectRe = regexp.MustCompile(`^http://127\.0\.0\.1(:[0-9]{1,5})?/login\?[\x21-\x7e]*code=[\x21-\x7e]+$`)
)

func newHubSpotify() *hubSpotify {
	env := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	s := &hubSpotify{bin: env("HUBD_LIBRESPOT", "librespot"), cache: env("HUBD_SPOTIFY_CACHE", "/db/librespot"),
		name: env("HUBD_SPOTIFY_NAME", "iNIX Smart"), pcm: make(chan []byte, 25), state: "off", retry: 5 * time.Second}
	s.fifo = filepath.Join(s.cache, "audio.fifo")
	if s.available() && s.loggedIn() {
		s.start(false)
	}
	return s
}

func (s *hubSpotify) available() bool { _, err := exec.LookPath(s.bin); return err == nil }
func (s *hubSpotify) loggedIn() bool {
	_, err := os.Stat(filepath.Join(s.cache, "credentials.json"))
	return err == nil
}

// start: chạy (lại) librespot; login = bắt đầu đăng nhập OAuth, chờ người dùng dán địa chỉ trả về.
func (s *hubSpotify) start(login bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kill()
	if err := os.MkdirAll(s.cache, 0o700); err != nil {
		return err
	}
	if st, err := os.Stat(s.fifo); err != nil || st.Mode()&os.ModeNamedPipe == 0 {
		os.Remove(s.fifo)
		if err := syscall.Mkfifo(s.fifo, 0o600); err != nil {
			return err
		}
	}
	s.readOnce.Do(func() { go s.readLoop() })
	args := []string{"--name", s.name, "--backend", "pipe", "--device", s.fifo, "--format", "S16", "--bitrate", "320",
		"--disable-discovery", "--system-cache", s.cache, "--disable-audio-cache", "--initial-volume", "100"}
	if login {
		args = append(args, "--enable-oauth", "--oauth-port", "0")
	}
	c := exec.Command(s.bin, args...)
	in, err := c.StdinPipe()
	if err != nil {
		return err
	}
	r, w := io.Pipe()
	c.Stdout, c.Stderr = w, w
	if err := c.Start(); err != nil {
		s.lastErr = err.Error()
		return err
	}
	s.gen++
	gen := s.gen
	s.cmd, s.stdin, s.url, s.stopped = c, in, "", false
	s.state = map[bool]string{true: "login", false: "starting"}[login]
	go s.watch(gen, r)
	go func() {
		err := c.Wait()
		w.Close()
		s.exited(gen, err)
	}()
	return nil
}

// watch: đọc log của librespot: địa chỉ đăng nhập, đăng nhập xong, lỗi.
func (s *hubSpotify) watch(gen int, r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		log.Printf("librespot: %s", line)
		s.mu.Lock()
		if gen == s.gen {
			switch {
			case browseRe.MatchString(line):
				s.url = browseRe.FindStringSubmatch(line)[1]
			case strings.Contains(line, "Authenticated as"):
				s.state, s.url, s.lastErr, s.retry = "running", "", "", 5*time.Second
			case strings.Contains(line, "ERROR"):
				s.lastErr = line
			}
		}
		s.mu.Unlock()
	}
}

// exited: librespot thoát ngoài ý muốn mà đã có thông tin đăng nhập thì chạy lại, chờ lâu dần (5 giây → 1 phút).
func (s *hubSpotify) exited(gen int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		return
	}
	s.cmd, s.state, s.url = nil, "off", ""
	if s.stopped || !s.loggedIn() {
		return
	}
	log.Printf("librespot dừng (%v), chạy lại sau %v", err, s.retry)
	d := s.retry
	s.retry = min(2*s.retry, time.Minute)
	time.AfterFunc(d, func() {
		s.mu.Lock()
		again := gen == s.gen && !s.stopped
		s.mu.Unlock()
		if again {
			s.start(false)
		}
	})
}

// kill: dừng tiến trình đang chạy. Gọi khi giữ s.mu.
func (s *hubSpotify) kill() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	s.gen++
	s.cmd, s.state, s.url = nil, "off", ""
}

func (s *hubSpotify) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.kill()
}

// readLoop: FIFO → các đoạn 20 ms. Hàng đầy (0,5 giây) thì chặn, nên librespot cũng chỉ giải mã kịp tốc độ phát.
func (s *hubSpotify) readLoop() {
	for {
		f, err := os.OpenFile(s.fifo, os.O_RDONLY, 0) // chờ tới khi librespot mở để ghi
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		for {
			b := make([]byte, snapFrames*4)
			if _, err := io.ReadFull(f, b); err != nil {
				break // librespot đóng (dừng phát, thoát): mở lại
			}
			s.pcm <- b
		}
		f.Close()
	}
}

// take: một đoạn Spotify nếu đang có, không chờ.
func (s *hubSpotify) take() []byte {
	select {
	case b := <-s.pcm:
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
	s.mu.Lock()
	out := map[string]any{"available": s.available(), "logged_in": s.loggedIn(), "state": s.state, "url": s.url, "name": s.name, "error": s.lastErr}
	s.mu.Unlock()
	writeJSON(w, out)
}

// action: {do: login} → {url}; {do: code, url: địa chỉ trang trả về}; {do: logout}.
func (s *hubSpotify) action(w http.ResponseWriter, r *http.Request) {
	var b struct{ Do, URL string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b) != nil {
		http.Error(w, "cần JSON {do, url}", 400)
		return
	}
	switch b.Do {
	case "login":
		if !s.available() {
			http.Error(w, "hub chưa có librespot", 409)
			return
		}
		if err := s.start(true); err != nil {
			http.Error(w, "không chạy được librespot: "+err.Error(), 502)
			return
		}
		for i := 0; i < 100; i++ { // librespot in địa chỉ đăng nhập ngay khi chạy
			s.mu.Lock()
			u := s.url
			s.mu.Unlock()
			if u != "" {
				writeJSON(w, map[string]string{"url": u})
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		http.Error(w, "librespot chưa đưa địa chỉ đăng nhập", 502)
	case "code":
		if len(b.URL) > 2048 || !redirectRe.MatchString(b.URL) {
			http.Error(w, "dán nguyên địa chỉ trang lỗi sau khi đăng nhập, dạng http://127.0.0.1/login?code=...", 400)
			return
		}
		s.mu.Lock()
		in, st := s.stdin, s.state
		s.mu.Unlock()
		if st != "login" || in == nil {
			http.Error(w, "chưa bắt đầu đăng nhập: bấm Đăng nhập cho hub trước", 409)
			return
		}
		if _, err := io.WriteString(in, b.URL+"\n"); err != nil {
			http.Error(w, "librespot không nhận", 502)
			return
		}
		w.WriteHeader(204)
	case "logout":
		s.stop()
		if err := os.Remove(filepath.Join(s.cache, "credentials.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "không xoá được thông tin đăng nhập", 500)
			return
		}
		s.mu.Lock()
		s.stopped = false
		s.mu.Unlock()
		w.WriteHeader(204)
	default:
		http.Error(w, "lệnh không hợp lệ", 400)
	}
}
