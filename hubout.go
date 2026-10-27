package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Khu "Loa nối vào hub" (mục 18.3): hub phát thẳng ra loa cắm vào hub (jack 3,5 mm, HDMI) hoặc loa Bluetooth ghép với hub,
// không cần đầu nhận rời. Cùng luồng với các đầu nhận Snapcast: đoạn có dấu thời gian t ra loa lúc t + snapBuffer như
// snapclient, nên khớp các khu khác. hubd đẩy PCM vào `pacat` (PulseAudio dùng chung với Soloist, pulse.go), ghi sớm hơn
// một khoảng bằng bộ đệm của pacat cộng độ trễ riêng của loa (Bluetooth chậm hơn dây; chỉnh ở Cài đặt).
// Cấu hình ở nhóm "music": out = "alsa:<card>,<dev>" | "bt:<MAC>" | "" (không dùng), out_delay_ms; tên, âm lượng, tắt tiếng
// như mọi khu (zone:hub). Tắt tiếng, bỏ ngõ ra hay 30 giây không có nhạc thì dừng pacat (và PulseAudio nếu Soloist không
// dùng): không giữ tiến trình thừa. Có nhạc lại thì chạy lại, loa Bluetooth đã ghép đôi thì tự nối lại.
const (
	outZone     = "hub"
	outLatency  = 200 * time.Millisecond // bộ đệm của pacat (--latency-msec)
	outMaxDelay = 700                    // ms: độ trễ loa tối đa, còn lại của snapBuffer cho bộ đệm pacat và lúc khởi động
)

var outIdle = 30 * time.Second // không có nhạc chừng này thì dừng pacat (test rút ngắn)

type outChunk struct {
	pcm []byte
	at  time.Duration
}

type outCfg struct {
	out   string // alsa:C,D | bt:MAC | ""
	delay time.Duration
	vol   int
	muted bool
}

type hubOut struct {
	m        *hubMusic
	pulse    *hubPulse
	bt       *hubBT
	pacatBin string
	q        chan outChunk
	reload   chan struct{}

	mu         sync.Mutex
	cfg        outCfg
	state, err string // off | idle | playing | error

	// chỉ goroutine loop ghi (cmd ghi khi giữ mu để test đọc được)
	cmd     *exec.Cmd
	in      io.WriteCloser
	done    chan struct{}
	module  string // module-alsa-sink đã nạp
	pa      bool   // đang giữ PulseAudio
	retryAt time.Time
}

func newHubOut(m *hubMusic, pulse *hubPulse) *hubOut {
	o := &hubOut{m: m, pulse: pulse, bt: &hubBT{bin: envOr("HUBD_BLUETOOTHCTL", "bluetoothctl"), dbus: pulse.dbus},
		pacatBin: envOr("HUBD_PACAT", "pacat"), q: make(chan outChunk, 100), reload: make(chan struct{}, 1)}
	o.load()
	go o.loop()
	return o
}

// load: đọc cấu hình từ kho; trả cấu hình cũ để biết có phải chạy lại pacat không.
func (o *hubOut) load() (old, cur outCfg) {
	s := o.m.db.Setting("music")
	c := outCfg{}
	c.out, _ = s["out"].(string)
	d, _ := s["out_delay_ms"].(float64)
	c.delay = time.Duration(d) * time.Millisecond
	z := o.m.zone(outZone)
	c.vol, c.muted = z.Volume, z.Muted
	o.mu.Lock()
	old, o.cfg = o.cfg, c
	switch {
	case c.out == "" || c.muted:
		o.state, o.err = "off", ""
	case o.state == "" || o.state == "off":
		o.state = "idle"
	}
	o.mu.Unlock()
	return old, c
}

// apply: cấu hình vừa đổi (Cài đặt, tên/âm lượng/tắt tiếng của khu). Đổi ngõ ra, bật/tắt tiếng hay độ trễ thì dừng pacat
// ngay (luồng vào pacat liên tục: độ trễ chỉ tính lúc pacat bắt đầu phát); âm lượng áp từ đoạn sau.
func (o *hubOut) apply() {
	old, cur := o.load()
	if old.out != cur.out || old.muted != cur.muted || old.delay != cur.delay {
		select {
		case o.reload <- struct{}{}:
		default:
		}
	}
}

func (o *hubOut) config() outCfg   { o.mu.Lock(); defer o.mu.Unlock(); return o.cfg }
func (o *hubOut) configured() bool { return o.config().out != "" }

func (o *hubOut) setState(st, err string) {
	o.mu.Lock()
	o.state, o.err = st, err
	o.mu.Unlock()
}

// write: một đoạn của luồng chung (music.go run), không chờ. Khu tắt hay chưa chọn ngõ ra thì bỏ qua.
func (o *hubOut) write(pcm []byte, at time.Duration) {
	if c := o.config(); c.out == "" || c.muted {
		return
	}
	select {
	case o.q <- outChunk{pcm, at}:
	default: // pacat kẹt: bỏ đoạn, không làm chậm các khu khác
	}
}

func (o *hubOut) loop() {
	idle := time.NewTimer(outIdle)
	for {
		select {
		case <-o.reload:
			o.stop("")
		case <-idle.C:
			o.stop("")
		case c := <-o.q:
			o.play(c) // có thể ngủ tới gần 1 giây chờ đúng giờ: đặt lại hẹn giờ im lặng sau đó
			idle.Reset(outIdle)
		}
	}
}

func (o *hubOut) play(c outChunk) {
	cfg := o.config()
	if cfg.out == "" || cfg.muted {
		o.stop("")
		return
	}
	if o.cmd == nil {
		if time.Now().Before(o.retryAt) {
			return
		}
		if err := o.start(cfg); err != nil {
			log.Printf("loa nối hub: %v", err)
			o.stop(err.Error())
			o.retryAt = time.Now().Add(15 * time.Second)
			return
		}
	}
	wait := c.at + snapBuffer*time.Millisecond - outLatency - cfg.delay - o.m.snap.now()
	if wait < -20*time.Millisecond { // trễ (vừa khởi động, máy bận): bỏ đoạn, không để lệch dồn lại
		return
	}
	time.Sleep(wait)
	if _, err := o.in.Write(scalePCM(c.pcm, cfg.vol)); err != nil {
		o.stop("pacat dừng: " + err.Error())
	}
}

// start: giữ PulseAudio, mở sink (nạp module-alsa-sink, hoặc chờ sink của loa Bluetooth, nối lại nếu cần), chạy pacat.
func (o *hubOut) start(cfg outCfg) error {
	if err := o.pulse.acquire("out", nil); err != nil { // PulseAudio chết thì pacat cũng thoát, đoạn sau chạy lại
		return err
	}
	o.pa = true
	sink := "hubout"
	kind, arg, _ := strings.Cut(cfg.out, ":")
	switch kind {
	case "alsa":
		idx, err := o.pulse.ctl("load-module", "module-alsa-sink", "device=hw:"+arg, "sink_name=hubout")
		if err != nil {
			return fmt.Errorf("không mở được ngõ ra %s: %w", arg, err)
		}
		o.module = idx
	case "bt":
		sink = "bluez_sink." + strings.ReplaceAll(arg, ":", "_") + ".a2dp_sink"
		if !o.pulse.hasSink(sink) {
			o.bt.reconnect(arg) // đã ghép đôi và tin cậy: nối (lại) để bluetoothd đưa A2DP cho PulseAudio của hub
			for i := 0; i < 16 && !o.pulse.hasSink(sink); i++ {
				time.Sleep(500 * time.Millisecond)
			}
			if !o.pulse.hasSink(sink) {
				return errors.New("loa Bluetooth chưa nối: bật loa, để gần hub; hub sẽ tự thử lại")
			}
		}
	default:
		return fmt.Errorf("ngõ ra không hợp lệ: %q", cfg.out)
	}
	c := exec.Command(o.pacatBin, "--playback", "--raw", "--format=s16le", "--rate=44100", "--channels=2",
		"--latency-msec="+strconv.Itoa(int(outLatency/time.Millisecond)), "--device="+sink, "--client-name=hubd")
	c.Env = []string{o.pulse.server(), "HOME=" + o.pulse.dir, "PATH=/usr/bin:/bin"}
	in, err := c.StdinPipe()
	if err != nil {
		return err
	}
	if _, err := startLogged(c, func(l string) { log.Printf("pacat: %s", l) }); err != nil {
		return fmt.Errorf("không chạy được pacat: %w", err)
	}
	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	o.mu.Lock()
	o.cmd, o.in, o.done = c, in, done
	o.mu.Unlock()
	o.setState("playing", "")
	return nil
}

// stop: dừng pacat, bỏ sink ALSA (trả thiết bị âm thanh), thôi giữ PulseAudio. err rỗng = dừng bình thường.
func (o *hubOut) stop(err string) {
	if o.cmd != nil {
		o.in.Close() // pacat phát nốt phần đệm rồi thoát
		terminate(o.cmd, o.done)
		o.mu.Lock()
		o.cmd = nil
		o.mu.Unlock()
	}
	if o.module != "" {
		o.pulse.ctl("unload-module", o.module)
		o.module = ""
	}
	if o.pa {
		o.pulse.release("out")
		o.pa = false
	}
	cfg := o.config()
	switch {
	case err != "":
		o.setState("error", err)
	case cfg.out == "" || cfg.muted:
		o.setState("off", "")
	default:
		o.setState("idle", "")
	}
}

// scalePCM: âm lượng của khu, đường cong bậc ba như tai nghe (50% ≈ nhỏ đi rõ, không phải giảm nửa biên độ).
func scalePCM(pcm []byte, vol int) []byte {
	if vol >= 100 {
		return pcm
	}
	f := float64(vol) / 100
	f = f * f * f
	out := make([]byte, len(pcm))
	for i := 0; i+1 < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(out[i:], uint16(int16(float64(int16(binary.LittleEndian.Uint16(pcm[i:])))*f)))
	}
	return out
}

type outOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var pcmNodeRe = regexp.MustCompile(`^pcmC(\d+)D(\d+)p$`)

// alsaOutputs: ngõ ra cắm thẳng vào hub, mỗi thiết bị phát /dev/snd/pcmC<card>D<dev>p (cần /dev/snd trong container).
// Tên theo chip codec ở /sys/class/sound/hwC<card>D0/chip_name: HDMI/DP thì "HDMI n", còn lại thiết bị 0 là jack 3,5 mm.
// (/proc/asound có tên đẹp hơn nhưng Docker che thư mục này trong container.)
func alsaOutputs() []outOption {
	dev, sys := envOr("HUBD_SND_DIR", "/dev/snd"), envOr("HUBD_SND_SYS", "/sys/class/sound")
	ents, _ := os.ReadDir(dev)
	out, hdmi := []outOption{}, map[string]int{}
	for _, e := range ents {
		m := pcmNodeRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		chip, _ := os.ReadFile(filepath.Join(sys, "hwC"+m[1]+"D0", "chip_name"))
		c := strings.TrimSpace(string(chip))
		name := "Ngõ ra số " + m[2]
		switch {
		case strings.Contains(c, "HDMI") || strings.Contains(c, "DP"):
			name = "HDMI " + strconv.Itoa(hdmi[m[1]])
			hdmi[m[1]]++
		case m[2] == "0":
			name = "Jack 3,5 mm"
		}
		if c != "" {
			name += " (" + c + ")"
		}
		out = append(out, outOption{ID: "alsa:" + m[1] + "," + m[2], Name: name})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.HasPrefix(out[i].Name, "Jack") && !strings.HasPrefix(out[j].Name, "Jack")
	})
	return out
}

// hubBT: loa Bluetooth qua bluetoothd của máy chủ (socket D-Bus hệ thống gắn vào container hubd, không chạy bluetoothd
// trong container), điều khiển bằng bluetoothctl. Chỉ làm khi người dùng bấm ở Cài đặt (quét, ghép đôi, nối, ngắt) và khi
// khu Loa nối vào hub cần phát mà loa chưa nối (nối lại).
type hubBT struct {
	bin, dbus string

	mu        sync.Mutex
	scanUntil time.Time
	lastRetry time.Time
}

type btDevice struct {
	MAC       string `json:"mac"`
	Name      string `json:"name"`
	Paired    bool   `json:"paired"`
	Trusted   bool   `json:"trusted"`
	Connected bool   `json:"connected"`
}

var (
	btMacRe = regexp.MustCompile(`^([0-9A-F]{2}:){5}[0-9A-F]{2}$`)
	btDevRe = regexp.MustCompile(`(?m)^Device ((?:[0-9A-F]{2}:){5}[0-9A-F]{2}) (.*)$`)
)

func (b *hubBT) run(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, b.bin, args...)
	c.Env = []string{"PATH=/usr/bin:/bin", "DBUS_SYSTEM_BUS_ADDRESS=unix:path=" + b.dbus}
	out, err := c.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		name := args[0]
		if name == "--timeout" {
			name = args[2]
		}
		lines := strings.Split(s, "\n")
		msg := strings.TrimSpace(lines[len(lines)-1])
		if strings.Contains(s, "connection != NULL") || strings.Contains(s, "AppArmor") { // bluetoothctl sập, không in lý do
			msg = "không nối được D-Bus hệ thống của máy (trên Ubuntu: AppArmor chặn D-Bus từ container)"
		}
		return s, fmt.Errorf("bluetoothctl %s: %s", name, msg)
	}
	return s, nil
}

// adapter: bluetoothd của máy chủ trả lời được không, adapter đã bật chưa; không được thì lý do (vd. AppArmor chặn D-Bus).
func (b *hubBT) adapter() (ok, powered bool, err error) {
	if _, err := os.Stat(b.dbus); err != nil {
		return false, false, errors.New("container hubd chưa được gắn D-Bus hệ thống của máy (docker-compose.yml: socket D-Bus + profile AppArmor inix-hubd, tài liệu mục 18.3)")
	}
	out, err := b.run(5*time.Second, "show")
	if err == nil && !strings.Contains(out, "Controller ") {
		err = errors.New("máy không có Bluetooth")
	}
	return err == nil, strings.Contains(out, "Powered: yes"), err
}

// devices: thiết bị bluetoothd biết (đã ghép đôi, hoặc vừa thấy khi quét). Bỏ thiết bị chưa có tên mà chưa ghép đôi.
func (b *hubBT) devices() ([]btDevice, error) {
	all, err := b.run(10*time.Second, "devices")
	if err != nil {
		return nil, err
	}
	in := func(filter string) map[string]bool {
		out, _ := b.run(10*time.Second, "devices", filter)
		set := map[string]bool{}
		for _, m := range btDevRe.FindAllStringSubmatch(out, -1) {
			set[m[1]] = true
		}
		return set
	}
	paired, trusted, conn := in("Paired"), in("Trusted"), in("Connected")
	out := []btDevice{}
	for _, m := range btDevRe.FindAllStringSubmatch(all, -1) {
		d := btDevice{MAC: m[1], Name: strings.TrimSpace(m[2]), Paired: paired[m[1]], Trusted: trusted[m[1]], Connected: conn[m[1]]}
		if !d.Paired && strings.ReplaceAll(d.Name, "-", ":") == d.MAC {
			continue
		}
		out = append(out, d)
	}
	rank := func(d btDevice) int { // đang nối, đã ghép đôi, rồi mới thấy
		switch {
		case d.Connected:
			return 0
		case d.Paired:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out, nil
}

// scan: tìm loa trong 30 giây (loa phải đang ở chế độ ghép đôi). Adapter tắt thì bật.
func (b *hubBT) scan() error {
	ok, powered, err := b.adapter()
	if !ok {
		return fmt.Errorf("hub không nói chuyện được với Bluetooth của máy chủ (bluetoothd qua D-Bus): %w", err)
	}
	if !powered {
		if _, err := b.run(10*time.Second, "power", "on"); err != nil {
			return err
		}
	}
	b.mu.Lock()
	if time.Now().Before(b.scanUntil) {
		b.mu.Unlock()
		return nil
	}
	b.scanUntil = time.Now().Add(30 * time.Second)
	b.mu.Unlock()
	go b.run(40*time.Second, "--timeout", "30", "scan", "on")
	return nil
}

func (b *hubBT) scanning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().Before(b.scanUntil)
}

// pair: ghép đôi (loa thường không cần mã), tin cậy (để loa tự nối lại khi bật), nối.
func (b *hubBT) pair(mac string) error {
	if out, err := b.run(40*time.Second, "--timeout", "30", "pair", mac); err != nil && !strings.Contains(out, "AlreadyExists") {
		return err
	}
	if _, err := b.run(10*time.Second, "trust", mac); err != nil {
		return err
	}
	return b.connect(mac)
}

func (b *hubBT) connect(mac string) error {
	_, err := b.run(30*time.Second, "--timeout", "20", "connect", mac)
	return err
}

func (b *hubBT) disconnect(mac string) error {
	_, err := b.run(15*time.Second, "disconnect", mac)
	return err
}

// reconnect: khu cần phát mà chưa có sink của loa: ngắt rồi nối lại (loa có thể đã nối trước khi PulseAudio của hub chạy,
// lúc đó bluetoothd không đưa A2DP cho hub). Tối đa một lần mỗi 20 giây.
func (b *hubBT) reconnect(mac string) {
	b.mu.Lock()
	if time.Since(b.lastRetry) < 20*time.Second {
		b.mu.Unlock()
		return
	}
	b.lastRetry = time.Now()
	b.mu.Unlock()
	b.disconnect(mac)
	if err := b.connect(mac); err != nil {
		log.Printf("loa nối hub: nối lại %s: %v", mac, err)
	}
}

// outStatus: GET /api/music/out: ngõ ra đang chọn, tình trạng, ngõ ra có dây, loa Bluetooth.
func (o *hubOut) outStatus(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	out := map[string]any{"out": o.cfg.out, "delay_ms": int(o.cfg.delay / time.Millisecond), "state": o.state, "error": o.err}
	o.mu.Unlock()
	out["outputs"] = alsaOutputs()
	bt := map[string]any{"scanning": o.bt.scanning(), "devices": []btDevice{}}
	ok, powered, err := o.bt.adapter()
	bt["available"], bt["powered"] = ok, powered
	if err != nil {
		bt["error"] = err.Error()
	} else if ds, err := o.bt.devices(); err != nil {
		bt["error"] = err.Error()
	} else {
		bt["devices"] = ds
	}
	out["bt"] = bt
	writeJSON(w, out)
}

// outCmd: POST /api/music/out: {do: set, out, delay_ms} chọn ngõ ra; {do: scan}; {do: pair | connect | disconnect, mac}.
func (o *hubOut) outCmd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Do, Out, MAC string
		DelayMS      *int `json:"delay_ms"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b) != nil {
		http.Error(w, "cần JSON {do, ...}", 400)
		return
	}
	mac := strings.ToUpper(b.MAC)
	if b.Do == "pair" || b.Do == "connect" || b.Do == "disconnect" {
		if !btMacRe.MatchString(mac) {
			http.Error(w, "địa chỉ Bluetooth không hợp lệ", 400)
			return
		}
	}
	var err error
	switch b.Do {
	case "set":
		ok := b.Out == "" || strings.HasPrefix(b.Out, "bt:") && btMacRe.MatchString(b.Out[3:])
		for _, x := range alsaOutputs() {
			ok = ok || x.ID == b.Out
		}
		if !ok || b.DelayMS != nil && (*b.DelayMS < 0 || *b.DelayMS > outMaxDelay) {
			http.Error(w, fmt.Sprintf("ngõ ra không có trên hub, hoặc độ trễ ngoài 0–%d ms", outMaxDelay), 400)
			return
		}
		patch := map[string]any{"out": b.Out}
		if b.DelayMS != nil {
			patch["out_delay_ms"] = *b.DelayMS
		}
		err = o.save(patch)
	case "scan":
		if err = o.bt.scan(); err == nil {
			w.WriteHeader(202)
			return
		}
	case "pair":
		if err = o.withPulse(func() error { return o.bt.pair(mac) }); err == nil && !o.configured() {
			err = o.save(map[string]any{"out": "bt:" + mac}) // chưa chọn ngõ ra: dùng luôn loa vừa ghép đôi
		}
	case "connect":
		err = o.withPulse(func() error { return o.bt.connect(mac) })
	case "disconnect":
		err = o.bt.disconnect(mac)
	default:
		http.Error(w, "lệnh không hợp lệ", 400)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.WriteHeader(204)
}

// withPulse: giữ PulseAudio trong lúc nối loa: bluez chỉ nối A2DP khi đã có endpoint (PulseAudio của hub đăng ký), không thì
// báo profile-unavailable. Nối xong mà khu chưa phát thì PulseAudio dừng; khi có nhạc hub nối lại (hubOut.start).
func (o *hubOut) withPulse(f func() error) error {
	if err := o.pulse.acquire("bt", nil); err != nil {
		return err
	}
	defer o.pulse.release("bt")
	return f()
}

func (o *hubOut) save(patch map[string]any) error {
	if err := o.m.db.PutSetting("music", patch); err != nil {
		return err
	}
	o.apply()
	return nil
}
