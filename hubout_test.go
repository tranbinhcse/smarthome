package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Công cụ âm thanh giả cho khu Loa nối vào hub, chạy lại chính file test (TestMain, HUBD_FAKE=pacat|pactl|btctl).
// Trạng thái nằm trong thư mục PulseAudio của hub thử (lấy từ PULSE_SERVER, hoặc từ địa chỉ D-Bus đặt cùng thư mục):
//   - pacat: ghi tham số vào pacat.args, PCM nhận được vào pacat.raw;
//   - pactl: load-module/unload-module/list short sinks, sink thêm ghi ở file sinks;
//   - bluetoothctl: thiết bị ở bt.json; connect thêm sink bluez_sink.<MAC>.a2dp_sink như PulseAudio thật khi loa nối A2DP.
//
// Lệnh nào cũng ghi vào <công cụ>.log.
func fakeAudioTool(tool string) {
	dir := filepath.Dir(strings.TrimPrefix(os.Getenv("PULSE_SERVER"), "unix:"))
	if tool == "btctl" {
		dir = filepath.Dir(strings.TrimPrefix(os.Getenv("DBUS_SYSTEM_BUS_ADDRESS"), "unix:path="))
	}
	a := os.Args[1:]
	appendLine(filepath.Join(dir, tool+".log"), strings.Join(a, " "))
	sinks := filepath.Join(dir, "sinks")
	switch tool {
	case "pacat":
		os.WriteFile(filepath.Join(dir, "pacat.args"), []byte(strings.Join(a, " ")), 0o644)
		f, _ := os.OpenFile(filepath.Join(dir, "pacat.raw"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		io.Copy(f, os.Stdin)
	case "pactl":
		switch a[0] {
		case "load-module":
			for _, x := range a {
				if n, ok := strings.CutPrefix(x, "sink_name="); ok {
					appendLine(sinks, n)
				}
			}
			fmt.Println("17")
		case "unload-module":
			removeLine(sinks, "hubout")
		case "list":
			fmt.Println("0\thub\tmodule-pipe-sink.c\ts16le 2ch 44100Hz\tIDLE")
			for i, s := range readLines(sinks) {
				fmt.Printf("%d\t%s\tmodule-x.c\ts16le 2ch 44100Hz\tIDLE\n", i+1, s)
			}
		}
	case "btctl":
		if a[0] == "--timeout" {
			a = a[2:]
		}
		var devs []btDevice
		b, _ := os.ReadFile(filepath.Join(dir, "bt.json"))
		json.Unmarshal(b, &devs)
		find := func(mac string) *btDevice {
			for i := range devs {
				if devs[i].MAC == mac {
					return &devs[i]
				}
			}
			fmt.Printf("Device %s not available\n", mac)
			os.Exit(1)
			return nil
		}
		sink := func(mac string) string { return "bluez_sink." + strings.ReplaceAll(mac, ":", "_") + ".a2dp_sink" }
		if _, err := os.Stat(filepath.Join(dir, "bt.denied")); err == nil { // y như bluetoothctl 5.82 khi AppArmor chặn D-Bus
			fmt.Println("dbus[1]: arguments to dbus_connection_get_object_path_data() were incorrect, assertion \"connection != NULL\" failed in file ../../dbus/dbus-connection.c line 5974.")
			fmt.Println("This is normally a bug in some application using the D-Bus library.\n\n  D-Bus not built with -rdynamic so unable to print a backtrace")
			os.Exit(139)
		}
		switch a[0] {
		case "show":
			fmt.Println("Controller F0:68:E3:A7:5F:59 (public)\n\tPowered: yes")
		case "devices":
			for _, d := range devs {
				if len(a) == 1 || a[1] == "Paired" && d.Paired || a[1] == "Trusted" && d.Trusted || a[1] == "Connected" && d.Connected {
					fmt.Printf("Device %s %s\n", d.MAC, d.Name)
				}
			}
		case "scan": // loa đang chờ ghép đôi, và một thiết bị chưa có tên (không hiện)
			devs = append(devs, btDevice{MAC: "AA:BB:CC:00:00:01", Name: "Loa thử"}, btDevice{MAC: "11:22:33:44:55:66", Name: "11-22-33-44-55-66"})
		case "pair":
			find(a[1]).Paired = true
			fmt.Println("Pairing successful")
		case "trust":
			find(a[1]).Trusted = true
		case "connect":
			d := find(a[1])
			if _, err := os.Stat(filepath.Join(dir, "native")); err != nil { // như bluez: chưa có endpoint A2DP (PulseAudio chưa chạy)
				fmt.Println("Failed to connect: org.bluez.Error.NotAvailable br-connection-profile-unavailable")
				os.Exit(1)
			}
			if !d.Paired {
				fmt.Println("Failed to connect: org.bluez.Error.Failed")
				os.Exit(1)
			}
			d.Connected = true
			appendLine(sinks, sink(d.MAC))
		case "disconnect":
			find(a[1]).Connected = false
			removeLine(sinks, sink(a[1]))
		}
		if a[0] != "show" && a[0] != "devices" { // lệnh chỉ đọc không ghi lại: chạy song song với quét sẽ ghi đè thiết bị mới thấy
			b, _ = json.Marshal(devs)
			os.WriteFile(filepath.Join(dir, "bt.json"), b, 0o644)
		}
	}
}

func readLines(p string) []string {
	b, _ := os.ReadFile(p)
	return strings.Fields(string(b))
}

func appendLine(p, s string) {
	f, _ := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	fmt.Fprintln(f, s)
	f.Close()
}

func removeLine(p, s string) {
	ls := slices.DeleteFunc(readLines(p), func(x string) bool { return x == s })
	os.WriteFile(p, []byte(strings.Join(ls, "\n")+"\n"), 0o644)
}

func fakeBin(t *testing.T, mode string) string {
	p := filepath.Join(t.TempDir(), mode)
	os.WriteFile(p, []byte(fmt.Sprintf("#!/bin/sh\nHUBD_FAKE=%s exec '%s' \"$@\"\n", mode, os.Args[0])), 0o755)
	return p
}

// outHub: hub thử có card âm thanh giả (jack ALC897 ở card 1, bốn HDMI ở card 0, như máy hub thật), PulseAudio, pacat,
// pactl, bluetoothctl giả. idle: im lặng bao lâu thì dừng pacat. Trả thư mục trạng thái của các công cụ giả.
func outHub(t *testing.T, idle time.Duration) (*http.ServeMux, *Hub, []*http.Cookie, string) {
	t.Helper()
	dir, snd, sys := t.TempDir(), t.TempDir(), t.TempDir()
	for _, n := range []string{"controlC0", "pcmC0D3p", "pcmC0D7p", "pcmC0D8p", "pcmC0D9p", "pcmC1D0p", "pcmC1D0c", "timer"} {
		os.WriteFile(filepath.Join(snd, n), nil, 0o644)
	}
	for c, chip := range map[string]string{"0": "Renoir HDMI", "1": "ALC897"} {
		os.MkdirAll(filepath.Join(sys, "hwC"+c+"D0"), 0o755)
		os.WriteFile(filepath.Join(sys, "hwC"+c+"D0", "chip_name"), []byte(chip+"\n"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "dbus"), nil, 0o644) // "có" D-Bus hệ thống: PulseAudio nạp module Bluetooth
	t.Setenv("HUBD_PULSE_DIR", dir)
	t.Setenv("HUBD_DBUS_SOCKET", filepath.Join(dir, "dbus"))
	t.Setenv("HUBD_SND_DIR", snd)
	t.Setenv("HUBD_SND_SYS", sys)
	t.Setenv("HUBD_PULSEAUDIO", fakeBin(t, "pulse"))
	t.Setenv("HUBD_PACAT", fakeBin(t, "pacat"))
	t.Setenv("HUBD_PACTL", fakeBin(t, "pactl"))
	t.Setenv("HUBD_BLUETOOTHCTL", fakeBin(t, "btctl"))
	old := outIdle
	outIdle = idle
	t.Cleanup(func() { outIdle = old })
	mux, h, ck, _ := musicMux(t, musicLib(t))
	t.Cleanup(func() { // không để pacat, PulseAudio giả chạy sau test
		h.music.out.setTestMuted()
		h.music.spot.stop()
	})
	return mux, h, ck, dir
}

// setTestMuted: tắt khu rồi chờ vòng phát dừng pacat (dọn sau test).
func (o *hubOut) setTestMuted() {
	o.m.db.PutSetting("music", map[string]any{"out": ""})
	o.apply()
	for i := 0; i < 200 && o.status() != "off"; i++ {
		time.Sleep(20 * time.Millisecond)
	}
}

func (o *hubOut) status() string { o.mu.Lock(); defer o.mu.Unlock(); return o.state }

type outState struct {
	Out, State, Error string
	Delay             int `json:"delay_ms"`
	Outputs           []outOption
	BT                struct {
		Available, Powered, Scanning bool
		Devices                      []btDevice
	}
}

func getOut(t *testing.T, mux http.Handler, ck []*http.Cookie) (s outState) {
	t.Helper()
	w := call(mux, "GET", "/api/music/out", "", "", ck...)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil {
		t.Fatalf("GET /api/music/out: %d %s", w.Code, w.Body)
	}
	return
}

func postOut(t *testing.T, mux http.Handler, ck []*http.Cookie, body string, want int) {
	t.Helper()
	if w := call(mux, "POST", "/api/music/out", body, "", ck...); w.Code != want {
		t.Fatalf("%s: code=%d muốn %d: %s", body, w.Code, want, w.Body)
	}
}

func hubZone(t *testing.T, mux http.Handler, ck []*http.Cookie) (z struct {
	ID, Name         string
	Connected, Muted bool
	Volume           int
}, ok bool) {
	t.Helper()
	for _, x := range getState(t, mux, ck).Zones {
		if x.ID == outZone {
			z.ID, z.Name, z.Connected, z.Muted, z.Volume = x.ID, x.Name, x.Connected, x.Muted, x.Volume
			return z, true
		}
	}
	return z, false
}

func alive(pid int) bool { return pid != 0 && syscall.Kill(pid, 0) == nil }

func pacatPid(o *hubOut) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cmd != nil && o.cmd.Process != nil {
		return o.cmd.Process.Pid
	}
	return 0
}

// Loa cắm dây: chọn jack 3,5 mm → khu "Loa nối vào hub" hiện như các khu khác, nhạc ra pacat qua sink ALSA của đúng thiết
// bị. Tắt tiếng thì pacat, sink ALSA, PulseAudio đều dừng (Soloist không dùng).
func TestHubOutWiredZone(t *testing.T) {
	mux, h, ck, dir := outHub(t, time.Minute) // tắt tiếng phải dừng ngay, không đợi hết giờ im lặng
	if w := call(mux, "GET", "/api/music/out", "", ""); w.Code != 401 {
		t.Fatalf("không có PIN: code=%d", w.Code)
	}
	s := getOut(t, mux, ck)
	names := []string{}
	for _, x := range s.Outputs {
		names = append(names, x.ID+"="+x.Name)
	}
	if strings.Join(names, "|") != "alsa:1,0=Jack 3,5 mm (ALC897)|alsa:0,3=HDMI 0 (Renoir HDMI)|alsa:0,7=HDMI 1 (Renoir HDMI)|alsa:0,8=HDMI 2 (Renoir HDMI)|alsa:0,9=HDMI 3 (Renoir HDMI)" {
		t.Fatalf("ngõ ra có dây: %v", names)
	}
	if _, ok := hubZone(t, mux, ck); ok || s.State != "off" {
		t.Fatalf("chưa chọn ngõ ra thì không có khu Loa nối vào hub: %+v", s)
	}
	for _, b := range []string{`{"do":"set","out":"alsa:5,0"}`, `{"do":"set","out":"alsa:1,0","delay_ms":900}`, `{"do":"set","out":"bt:loa"}`,
		`{"do":"set","out":"alsa:1,0; rm -rf /"}`, `{"do":"pair","mac":"AA:BB"}`, `{"do":"x"}`} {
		postOut(t, mux, ck, b, 400)
	}
	postOut(t, mux, ck, `{"do":"set","out":"alsa:1,0","delay_ms":40}`, 204)
	if s := getOut(t, mux, ck); s.Out != "alsa:1,0" || s.Delay != 40 || s.State != "idle" {
		t.Fatalf("đã chọn jack: %+v", s)
	}
	z, ok := hubZone(t, mux, ck)
	if !ok || z.Name != "Loa nối vào hub" || !z.Connected || z.Muted {
		t.Fatalf("khu Loa nối vào hub: %+v %v", z, ok)
	}
	cmd(t, mux, ck, `{"do":"zone","id":"hub","volume":50}`, 204)
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	waitFor(t, "nhạc ra pacat", func() bool {
		st, _ := os.Stat(filepath.Join(dir, "pacat.raw"))
		return st != nil && st.Size() > 44100*4/2
	})
	args, _ := os.ReadFile(filepath.Join(dir, "pacat.args"))
	if !strings.Contains(string(args), "--device=hubout") || !strings.Contains(string(args), "--latency-msec=200") ||
		!strings.Contains(string(args), "--format=s16le") || !strings.Contains(string(args), "--rate=44100") {
		t.Fatalf("tham số pacat: %s", args)
	}
	if l, _ := os.ReadFile(filepath.Join(dir, "pactl.log")); !strings.Contains(string(l), "load-module module-alsa-sink device=hw:1,0 sink_name=hubout") {
		t.Fatalf("phải nạp sink ALSA của jack (card 1, thiết bị 0): %s", l)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "pacat.raw"))
	if len(bytes.Trim(raw, "\x00")) == 0 {
		t.Fatal("pacat chỉ nhận im lặng")
	}
	if script, _ := os.ReadFile(filepath.Join(dir, "hub.pa")); !strings.Contains(string(script), "module-bluez5-discover headset=ofono") ||
		!strings.Contains(string(script), ".nofail") {
		t.Fatalf("có D-Bus hệ thống thì PulseAudio nạp Bluetooth (không bắt buộc): %s", script)
	}
	// Loa Bluetooth đã nối sẵn lúc hubd khởi động thì thành sink mặc định; Soloist mở luồng vào sink mặc định nên Spotify
	// đi thẳng ra loa đó, bỏ qua luồng chung: các đầu nhận (Mac chạy snapclient) im. Sink mặc định phải luôn là "hub".
	if script, _ := os.ReadFile(filepath.Join(dir, "hub.pa")); !regexp.MustCompile(`sink_name=hub [^\n]*\nset-default-sink hub\n`).Match(script) {
		t.Fatalf("PulseAudio phải giữ sink mặc định là hub ngay sau khi nạp: %s", script)
	}
	pacat, pa := pacatPid(h.music.out), pulsePid(h.music.out.pulse)
	if !alive(pacat) || !alive(pa) {
		t.Fatalf("đang phát phải có pacat (%d) và PulseAudio (%d)", pacat, pa)
	}
	// hết bài, hàng đợi cạn: tắt tiếng phải dừng pacat ngay chứ không chờ hết giờ im lặng (ở đây 1 phút)
	waitFor(t, "hết bài", func() bool { return !getState(t, mux, ck).Playing })
	time.Sleep(time.Second)
	cmd(t, mux, ck, `{"do":"zone","id":"hub","muted":true}`, 204)
	waitFor(t, "tắt tiếng thì dừng pacat và PulseAudio", func() bool { return !alive(pacat) && !alive(pa) })
	if l, _ := os.ReadFile(filepath.Join(dir, "pactl.log")); !strings.Contains(string(l), "unload-module 17") {
		t.Fatalf("tắt khu phải trả thiết bị ALSA (unload-module): %s", l)
	}
	if s := getOut(t, mux, ck); s.State != "off" {
		t.Fatalf("khu tắt tiếng: %+v", s)
	}
}

// Không có nhạc 30 giây (test: 0,7 giây) thì dừng pacat; có nhạc lại thì chạy lại.
func TestHubOutIdleStops(t *testing.T) {
	mux, h, ck, dir := outHub(t, 700*time.Millisecond)
	postOut(t, mux, ck, `{"do":"set","out":"alsa:0,3"}`, 204)
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	waitFor(t, "pacat chạy", func() bool { return alive(pacatPid(h.music.out)) })
	pacat := pacatPid(h.music.out)
	waitFor(t, "hết bài, im 0,7 giây thì dừng pacat", func() bool { return !alive(pacat) && pulsePid(h.music.out.pulse) == 0 })
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	waitFor(t, "có nhạc lại thì chạy lại", func() bool { p := pacatPid(h.music.out); return alive(p) && p != pacat })
	if args, _ := os.ReadFile(filepath.Join(dir, "pactl.log")); !strings.Contains(string(args), "device=hw:0,3") {
		t.Fatalf("HDMI 0 = card 0 thiết bị 3: %s", args)
	}
}

// Đổi độ trễ lúc đang phát: luồng vào pacat liên tục, ghi sớm hơn chỉ làm dài hàng đợi chứ không dời tiếng ra loa, nên
// phải chạy lại pacat để độ trễ mới tính từ đầu (từng lưu 200 ms mà loa Bluetooth vẫn lệch như cũ).
func TestHubOutDelayChangeRestartsPacat(t *testing.T) {
	mux, h, ck, _ := outHub(t, time.Minute)
	postOut(t, mux, ck, `{"do":"set","out":"alsa:0,3"}`, 204)
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	waitFor(t, "pacat chạy", func() bool { return alive(pacatPid(h.music.out)) })
	pacat := pacatPid(h.music.out)
	postOut(t, mux, ck, `{"do":"set","out":"alsa:0,3","delay_ms":200}`, 204)
	waitFor(t, "đổi độ trễ thì chạy lại pacat", func() bool { p := pacatPid(h.music.out); return !alive(pacat) && alive(p) && p != pacat })
}

// Loa Bluetooth: quét → ghép đôi (ghép đôi, tin cậy, nối) → tự chọn làm ngõ ra → nhạc ra sink Bluetooth. Loa tắt rồi bật
// lại (sink mất): khi có nhạc, hub tự nối lại.
func TestHubOutBluetooth(t *testing.T) {
	mux, h, ck, dir := outHub(t, time.Minute)
	if s := getOut(t, mux, ck); !s.BT.Available || !s.BT.Powered || len(s.BT.Devices) != 0 {
		t.Fatalf("Bluetooth của máy chủ: %+v", s.BT)
	}
	postOut(t, mux, ck, `{"do":"scan"}`, 202)
	waitFor(t, "thấy loa khi quét", func() bool { return len(getOut(t, mux, ck).BT.Devices) == 1 })
	s := getOut(t, mux, ck)
	if d := s.BT.Devices[0]; d.MAC != "AA:BB:CC:00:00:01" || d.Name != "Loa thử" || d.Paired || !s.BT.Scanning {
		t.Fatalf("thiết bị chưa tên không hiện, loa mới chưa ghép đôi: %+v", s.BT)
	}
	if w := call(mux, "POST", "/api/music/out", `{"do":"pair","mac":"aa:bb:cc:00:00:01"}`, ""); w.Code != 401 {
		t.Fatalf("ghép đôi không có PIN: code=%d", w.Code)
	}
	postOut(t, mux, ck, `{"do":"pair","mac":"aa:bb:cc:00:00:01"}`, 204)
	if l, _ := os.ReadFile(filepath.Join(dir, "btctl.log")); !strings.Contains(string(l), "pair AA:BB:CC:00:00:01\ntrust AA:BB:CC:00:00:01\n--timeout 20 connect AA:BB:CC:00:00:01") {
		t.Fatalf("ghép đôi = pair, trust, connect: %s", l)
	}
	s = getOut(t, mux, ck)
	if d := s.BT.Devices[0]; !d.Paired || !d.Trusted || !d.Connected || s.Out != "bt:AA:BB:CC:00:00:01" {
		t.Fatalf("ghép đôi xong phải dùng luôn làm ngõ ra: %+v", s)
	}
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	waitFor(t, "nhạc ra loa Bluetooth", func() bool {
		a, _ := os.ReadFile(filepath.Join(dir, "pacat.args"))
		return strings.Contains(string(a), "--device=bluez_sink.AA_BB_CC_00_00_01.a2dp_sink")
	})
	// loa tắt: sink mất, pacat dừng (tắt tiếng rồi bật lại khu cho nhanh); có nhạc lại thì hub nối lại loa
	postOut(t, mux, ck, `{"do":"disconnect","mac":"AA:BB:CC:00:00:01"}`, 204)
	cmd(t, mux, ck, `{"do":"zone","id":"hub","muted":true}`, 204)
	waitFor(t, "khu tắt", func() bool { return pacatPid(h.music.out) == 0 })
	os.Remove(filepath.Join(dir, "pacat.args"))
	cmd(t, mux, ck, `{"do":"zone","id":"hub","muted":false}`, 204)
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	waitFor(t, "tự nối lại loa rồi phát", func() bool {
		a, _ := os.ReadFile(filepath.Join(dir, "pacat.args"))
		return strings.Contains(string(a), "bluez_sink.AA_BB_CC_00_00_01")
	})
	if l, _ := os.ReadFile(filepath.Join(dir, "btctl.log")); strings.Count(string(l), "connect AA:BB:CC:00:00:01") < 2 {
		t.Fatalf("phải nối lại loa: %s", l)
	}
}

// Đúng giờ như snapclient: đoạn có dấu thời gian t được ghi vào pacat lúc t + 1000 ms − 200 ms (bộ đệm pacat) − độ trễ loa;
// âm lượng theo đường cong bậc ba.
func TestHubOutTimingAndVolume(t *testing.T) {
	_, h, _, _ := outHub(t, time.Minute)
	r, w := io.Pipe() // pacat giả trong bộ nhớ; hubOut riêng, không có vòng phát
	o := &hubOut{m: h.music, cmd: &exec.Cmd{}, in: w, cfg: outCfg{out: "alsa:1,0", delay: 150 * time.Millisecond, vol: 50}}
	pcm := make([]byte, snapFrames*4)
	binary.LittleEndian.PutUint16(pcm, 16000)
	got := make(chan []byte)
	go func() { b := make([]byte, len(pcm)); io.ReadFull(r, b); got <- b }()
	t0 := time.Now()
	o.play(outChunk{pcm, o.m.snap.now()})
	b := <-got
	if d := time.Since(t0); d < 620*time.Millisecond || d > 700*time.Millisecond {
		t.Fatalf("đoạn ghi vào pacat sau %v, muốn ≈ 650 ms (1000 − 200 − 150)", d)
	}
	if v := int16(binary.LittleEndian.Uint16(b)); v != 2000 { // 16000 × 0,5³
		t.Fatalf("âm lượng 50%%: mẫu %d, muốn 2000", v)
	}
	start := time.Now()
	o.play(outChunk{pcm, o.m.snap.now() - 2*time.Second}) // đoạn đã trễ: bỏ, không chờ, không ghi
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("đoạn trễ không được làm chậm vòng phát")
	}
}

// PulseAudio dùng chung: chạy khi bên đầu tiên cần, dừng khi bên cuối thôi dùng.
func TestPulseSharedBetweenUsers(t *testing.T) {
	t.Setenv("HUBD_PULSE_DIR", t.TempDir())
	t.Setenv("HUBD_PULSEAUDIO", fakeBin(t, "pulse"))
	p := newHubPulse()
	if err := p.acquire("soloist", nil); err != nil {
		t.Fatal(err)
	}
	pid := pulsePid(p)
	if err := p.acquire("out", nil); err != nil || pulsePid(p) != pid {
		t.Fatalf("bên thứ hai dùng lại PulseAudio đang chạy: %v %d %d", err, pid, pulsePid(p))
	}
	p.release("soloist")
	if !alive(pid) {
		t.Fatal("còn khu Loa nối vào hub dùng thì không được dừng")
	}
	p.release("out")
	if alive(pid) || p.running() {
		t.Fatal("không còn ai dùng thì dừng PulseAudio")
	}
}

// Không nói chuyện được với bluetoothd (vd. AppArmor của máy chủ chặn D-Bus): Cài đặt hiện đúng lý do, tìm loa báo lỗi;
// loa cắm dây không bị ảnh hưởng.
func TestHubOutBluetoothDenied(t *testing.T) {
	mux, _, ck, dir := outHub(t, time.Minute)
	os.WriteFile(filepath.Join(dir, "bt.denied"), nil, 0o644)
	s := getOut(t, mux, ck)
	var raw struct{ BT struct{ Error string } }
	json.Unmarshal(call(mux, "GET", "/api/music/out", "", "", ck...).Body.Bytes(), &raw)
	if s.BT.Available || !strings.Contains(raw.BT.Error, "AppArmor") || len(s.Outputs) == 0 {
		t.Fatalf("Bluetooth bị chặn phải báo lý do, ngõ ra có dây vẫn còn: %+v %q", s, raw.BT.Error)
	}
	if w := call(mux, "POST", "/api/music/out", `{"do":"scan"}`, "", ck...); w.Code != 502 || !strings.Contains(w.Body.String(), "AppArmor") {
		t.Fatalf("tìm loa khi bị chặn: %d %s", w.Code, w.Body)
	}
	// compose chưa gắn socket D-Bus (mặc định hiện nay): báo đúng là chưa gắn, không chạy bluetoothctl
	os.Remove(filepath.Join(dir, "dbus"))
	os.Remove(filepath.Join(dir, "btctl.log"))
	json.Unmarshal(call(mux, "GET", "/api/music/out", "", "", ck...).Body.Bytes(), &raw)
	if !strings.Contains(raw.BT.Error, "chưa được gắn D-Bus") {
		t.Fatalf("thiếu socket D-Bus: %q", raw.BT.Error)
	}
	if _, err := os.Stat(filepath.Join(dir, "btctl.log")); err == nil {
		t.Fatal("thiếu socket D-Bus thì không gọi bluetoothctl")
	}
}
