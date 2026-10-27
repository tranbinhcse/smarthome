package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// hubPulse: một PulseAudio tối giản trong container hubd, dùng chung cho Soloist (spotifyhub.go: sink "hub" ghi PCM vào FIFO
// cho readLoop) và khu Loa nối vào hub (hubout.go: sink ALSA của jack/HDMI, hoặc sink Bluetooth). Chỉ chạy khi còn ít nhất
// một bên dùng; bên cuối thôi dùng thì dừng, không giữ tiến trình thừa. Có socket D-Bus hệ thống của máy chủ thì nạp thêm
// module Bluetooth: PulseAudio đăng ký endpoint A2DP với bluetoothd của máy chủ (không chạy bluetoothd trong container),
// loa Bluetooth nối vào thì có sink bluez_sink.<MAC>.a2dp_sink.
type hubPulse struct {
	bin, ctlBin, dir, dbus string

	mu    sync.Mutex
	cmd   *exec.Cmd
	done  chan struct{}
	users map[string]func() // bên đang dùng → gọi khi PulseAudio chết ngoài ý muốn (nil: bên đó tự thấy)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newHubPulse() *hubPulse {
	return &hubPulse{bin: envOr("HUBD_PULSEAUDIO", "pulseaudio"), ctlBin: envOr("HUBD_PACTL", "pactl"),
		dir: envOr("HUBD_PULSE_DIR", "/run/hubd-pulse"), dbus: envOr("HUBD_DBUS_SOCKET", "/run/dbus/system_bus_socket"),
		users: map[string]func(){}}
}

func (p *hubPulse) fifo() string   { return filepath.Join(p.dir, "audio.fifo") }
func (p *hubPulse) server() string { return "PULSE_SERVER=unix:" + filepath.Join(p.dir, "native") }
func (p *hubPulse) running() bool  { p.mu.Lock(); defer p.mu.Unlock(); return p.cmd != nil }

// acquire: bên `user` cần PulseAudio; chưa chạy thì chạy. Chờ tới khi socket sẵn sàng (Soloist, pacat nối ngay sau đó).
func (p *hubPulse) acquire(user string, died func()) error {
	sock := filepath.Join(p.dir, "native")
	p.mu.Lock()
	p.users[user] = died
	if p.cmd == nil {
		if err := p.start(sock); err != nil {
			p.mu.Unlock()
			return err
		}
	}
	done := p.done
	p.mu.Unlock()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(sock); err == nil {
			return nil
		}
		select {
		case <-done:
			return errors.New("PulseAudio thoát ngay khi khởi động")
		case <-time.After(50 * time.Millisecond):
		}
	}
	return errors.New("PulseAudio không mở socket")
}

// start: chạy PulseAudio (-n: không nạp default.pa) với kịch bản riêng. Gọi khi giữ p.mu.
func (p *hubPulse) start(sock string) error {
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return err
	}
	fifo := p.fifo()
	if st, err := os.Stat(fifo); err != nil || st.Mode()&os.ModeNamedPipe == 0 {
		os.Remove(fifo)
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			return err
		}
	}
	os.Remove(sock)
	// sink "hub": PCM 44,1 kHz/16 bit stereo vào FIFO, chạy theo tốc độ người đọc nên Soloist chỉ giải mã kịp tốc độ phát
	pa := fmt.Sprintf("load-module module-native-protocol-unix auth-anonymous=1 socket=%s\n"+
		"load-module module-pipe-sink file=%s sink_name=hub format=s16le rate=44100 channels=2\n", sock, fifo)
	if _, err := os.Stat(p.dbus); err == nil { // headset=ofono: chỉ A2DP, không giành HFP/HSP của máy chủ
		pa += ".nofail\nload-module module-bluetooth-policy\nload-module module-bluez5-discover headset=ofono\n"
	}
	script := filepath.Join(p.dir, "hub.pa")
	if err := os.WriteFile(script, []byte(pa), 0o600); err != nil {
		return err
	}
	c := exec.Command(p.bin, "-n", "-F", script, "--daemonize=no", "--exit-idle-time=-1", "--disallow-exit", "--use-pid-file=no",
		"--realtime=no", "--high-priority=no", "--disable-shm=yes", "--log-target=stderr", "--log-level=error")
	c.Env = []string{"HOME=" + p.dir, "XDG_RUNTIME_DIR=" + p.dir, "PATH=/usr/bin:/bin", "DBUS_SYSTEM_BUS_ADDRESS=unix:path=" + p.dbus}
	if _, err := startLogged(c, func(l string) { log.Printf("pulseaudio: %s", l) }); err != nil {
		return fmt.Errorf("không chạy được PulseAudio: %w", err)
	}
	done := make(chan struct{})
	p.cmd, p.done = c, done
	go func() {
		err := c.Wait()
		close(done)
		p.mu.Lock()
		var died []func()
		if p.cmd == c { // chết ngoài ý muốn (release thì đã bỏ p.cmd trước)
			p.cmd = nil
			log.Printf("pulseaudio dừng (%v)", err)
			for _, f := range p.users {
				if f != nil {
					died = append(died, f)
				}
			}
		}
		p.mu.Unlock()
		for _, f := range died {
			f()
		}
	}()
	return nil
}

// release: bên `user` thôi dùng; không còn ai thì dừng PulseAudio.
func (p *hubPulse) release(user string) {
	p.mu.Lock()
	delete(p.users, user)
	c, done := p.cmd, p.done
	if len(p.users) > 0 || c == nil {
		p.mu.Unlock()
		return
	}
	p.cmd = nil
	p.mu.Unlock()
	terminate(c, done)
}

// ctl: một lệnh pactl tới PulseAudio này (nạp/bỏ sink ALSA, xem các sink).
func (p *hubPulse) ctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, p.ctlBin, args...)
	c.Env = []string{p.server(), "HOME=" + p.dir, "PATH=/usr/bin:/bin"}
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pactl %s: %v %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// hasSink: PulseAudio có sink tên này (loa Bluetooth đã nối vào hub chưa).
func (p *hubPulse) hasSink(name string) bool {
	out, err := p.ctl("list", "short", "sinks")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 1 && f[1] == name {
			return true
		}
	}
	return false
}
