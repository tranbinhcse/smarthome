package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hubd/isapi"
)

// Bộ ghi: mỗi camera một luồng MP4 phân mảnh từ go2rtc (GET /api/stream.mp4), recSplit cắt file, ghi chỉ mục, xoá theo
// recPlan. URL nguồn có mật khẩu: log chỉ có id camera và câu cố định hoặc mã HTTP, không in lỗi của http.Client (lỗi đó
// chứa URL).

type recSource struct {
	ID, URL string       // URL có mật khẩu, không bao giờ log
	client  *http.Client // client ISAPI dùng chung với ảnh chụp, Thử kết nối (giữ khoá 15 phút); nil = camera RTSP thường
	probe   string       // GET qua client trước mỗi lần nối go2rtc: sai mật khẩu thì 401 bật khoá
}

// locked: tài khoản vừa bị từ chối; không mở luồng vì go2rtc gửi mật khẩu qua RTSP mỗi lần nối (đầu ghi khoá IP của hub).
func (s recSource) locked() bool {
	if s.client == nil {
		return false
	}
	t, ok := s.client.Transport.(*isapi.Transport)
	return ok && t.Locked()
}

type recHold struct {
	cam          string
	from, to, at time.Time
}

type recRun struct {
	url    string
	cancel context.CancelFunc
}

type recorder struct {
	db       *DB
	go2rtc   *url.URL
	dir      string
	disk     func() recDisk
	sources  func() []recSource
	policies func() map[string]recPolicy
	notify   func(level, kind, text string)

	// test thay được
	create        func(cam string, start time.Time) (io.WriteCloser, string, error)
	tick          <-chan time.Time // nil = mỗi 60 giây
	backoff, idle time.Duration    // chờ nối lại sau luồng đã ra file; hạn không nhận byte nào thì coi luồng đứng

	plan    sync.Mutex // kiểm ổ, recPlan + xoá file, đánh dấu giữ: chạy lần lượt (file vừa held không bị xoá theo quyết định cũ)
	probeMu sync.Mutex // thử ISAPI lần lượt: các kênh cùng đầu ghi gửi tối đa một lần sai mật khẩu, kênh sau gặp khoá
	wg      sync.WaitGroup

	mu      sync.Mutex
	runs    map[string]*recRun
	holds   []recHold // lệnh giữ chờ file đang ghi
	ok      bool      // ổ đạt ở lần kiểm gần nhất và chưa gặp lỗi ghi; lúc khởi động false nên không báo
	stopped bool      // recPlan báo Stop: chờ còn trống từ 3%
}

func newRecorder(db *DB, go2rtc *url.URL, dir string, disk func() recDisk, sources func() []recSource,
	policies func() map[string]recPolicy, notify func(level, kind, text string)) *recorder {
	r := &recorder{db: db, go2rtc: go2rtc, dir: dir, disk: disk, sources: sources, policies: policies, notify: notify,
		backoff: 2 * time.Second, idle: 30 * time.Second, runs: map[string]*recRun{}}
	r.create = r.createFile
	return r
}

// recSources: luồng chính của mọi camera: kênh đầu ghi (chưa có IP đầu ghi thì bỏ qua) rồi camera nối thẳng. Đầu ghi và
// camera Hikvision kèm client ISAPI của c (cùng client với ảnh chụp, Thử kết nối) để thấy khoá tài khoản.
func recSources(db *DB, c *cams) []recSource {
	out := []recSource{}
	if n := c.conf(); n.On {
		cl, probe := c.client(n), "http://"+n.Host+"/ISAPI/System/deviceInfo"
		for _, d := range camDevices(db.Setting("nvr")) {
			out = append(out, recSource{d.ID, nvrRTSP(n, d.Channel, false), cl, probe})
		}
	}
	direct, err := camList(db)
	if err != nil {
		log.Print("ghi hình: không đọc được danh sách camera")
	}
	for _, rec := range direct {
		s := recSource{ID: rec.ID, URL: rec.rtsp(false)}
		if rec.Kind == "hik_cam" {
			hp := net.JoinHostPort(rec.Host, strconv.Itoa(rec.HTTPPort))
			s.client, s.probe = c.clientFor(hp, rec.User, rec.Pass), "http://"+hp+"/ISAPI/System/deviceInfo"
		}
		out = append(out, s)
	}
	return out
}

func (r *recorder) run(ctx context.Context) {
	r.adopt()
	tick := r.tick
	if tick == nil {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		tick = t.C
	}
	for first := true; ; first = false {
		// khởi động mà ổ không đạt nhưng đã từng ghi (ổ USB chưa gắn sau mất điện): báo một lần; chưa ghi bao giờ thì thôi
		if d := r.check(ctx); first && !d.OK && r.hasFiles() {
			log.Print("ghi hình: ổ không đạt lúc khởi động")
			r.notify("warn", "luu_tru", d.Reason)
		}
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return
		case <-tick:
		}
	}
}

// adopt: file trên đĩa chưa có trong chỉ mục (hubd dừng giữa lúc ghi, recAdd lỗi) được thêm vào để quy tắc xoá thấy nó.
// Start lấy từ <YYYYMMDD>/<HHMMSS>[-n].mp4 theo giờ ICT, End là mtime.
func (r *recorder) adopt() {
	files, err := recAll(r.db)
	if err != nil {
		log.Print("ghi hình: không đọc được chỉ mục")
		return
	}
	known := map[string]bool{}
	for _, f := range files {
		known[f.Path] = true
	}
	paths, _ := filepath.Glob(filepath.Join(r.dir, "*", "*", "*.mp4"))
	for _, p := range paths {
		if known[p] {
			continue
		}
		day := filepath.Dir(p)
		hms, _, _ := strings.Cut(strings.TrimSuffix(filepath.Base(p), ".mp4"), "-")
		start, err := time.ParseInLocation("20060102150405", filepath.Base(day)+hms, tgICT)
		st, err2 := os.Stat(p)
		if err != nil || err2 != nil {
			continue
		}
		f := recFile{Cam: filepath.Base(filepath.Dir(day)), Path: p, Start: start, End: st.ModTime(), Bytes: st.Size()}
		if _, err := recAdd(r.db, f); err != nil {
			log.Printf("ghi hình %s: không thêm được file mồ côi vào chỉ mục", f.Cam)
		}
	}
}

func (r *recorder) hasFiles() bool {
	var n int
	r.db.mu.Lock()
	defer r.db.mu.Unlock()
	return r.db.sql.QueryRow(`SELECT EXISTS(SELECT 1 FROM recordings)`).Scan(&n) == nil && n == 1
}

// check: lần kiểm 60 giây. Ổ không đạt thì dừng mọi luồng (báo khi vừa chuyển từ đạt), đang dừng vì đầy thì thôi,
// còn lại mở luồng thiếu và đóng luồng thừa.
func (r *recorder) check(ctx context.Context) recDisk {
	d := r.retain()
	r.mu.Lock()
	was := r.ok
	r.ok = d.OK
	if !d.OK || r.stopped {
		r.stopLocked()
	}
	open := d.OK && !r.stopped
	r.mu.Unlock()
	if !d.OK && was {
		log.Print("ghi hình: ổ không đạt, dừng ghi")
		r.notify("warn", "luu_tru", d.Reason)
	}
	if open {
		r.sync(ctx)
	}
	return d
}

// recDelMax: số file xoá tối đa mỗi lần dọn; phần còn lại để lần sau (mỗi file xong và mỗi lần kiểm 60 giây đều dọn),
// để các luồng chờ r.plan không bị chặn lâu khi phải xoá hàng nghìn file (hạ % dùng tối đa, đặt giới hạn GB nhỏ).
const recDelMax = 100

// retain: kiểm ổ, chạy recPlan, xoá tối đa recDelMax file trong Delete (không xoá được thì giữ dòng). Stop thì dừng mọi
// luồng và báo một lần; đang dừng mà ổ còn trống từ 3% thì cho ghi lại.
func (r *recorder) retain() recDisk {
	r.plan.Lock()
	defer r.plan.Unlock()
	d := r.disk()
	if !d.OK {
		return d
	}
	files, err := recAll(r.db)
	if err != nil {
		log.Print("ghi hình: không đọc được chỉ mục")
		return d
	}
	path := map[int64]string{}
	for _, f := range files {
		path[f.ID] = f.Path
	}
	dec := recPlan(files, r.policies(), d, recNormalPct(r.db))
	if len(dec.Delete) > recDelMax {
		dec.Delete = dec.Delete[:recDelMax]
	}
	var gone []int64
	for _, id := range dec.Delete {
		if err := os.Remove(path[id]); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("ghi hình: không xoá được file %d", id)
			continue
		}
		gone = append(gone, id)
	}
	if err := recRemove(r.db, gone); err != nil {
		log.Print("ghi hình: không xoá được dòng chỉ mục")
	}
	r.mu.Lock()
	full := dec.Stop && !r.stopped
	resume := !dec.Stop && r.stopped && d.Free*100 >= 3*d.Total
	switch {
	case full:
		r.stopped = true
		r.stopLocked()
	case resume:
		r.stopped = false
	}
	r.mu.Unlock()
	if full {
		r.notify("urgent", "luu_tru", "Ổ ghi hình gần đầy: đã dừng ghi hình thường, chỉ còn giữ đoạn báo động.")
	}
	if resume {
		r.notify("info", "luu_tru", "Ổ ghi hình đã có chỗ, ghi hình lại.")
	}
	return d
}

func (r *recorder) stopLocked() {
	for id, run := range r.runs {
		run.cancel()
		delete(r.runs, id)
	}
}

// sync: mở luồng cho nguồn mới (trừ nguồn đang bị khoá tài khoản), đóng luồng của nguồn đã bỏ hoặc đổi URL (mật khẩu mới).
func (r *recorder) sync(ctx context.Context) {
	want := map[string]recSource{}
	for _, s := range r.sources() {
		want[s.ID] = s
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ok || r.stopped { // file vừa xong có thể đã dừng ghi sau lúc check xem ổ
		return
	}
	for id, run := range r.runs {
		if want[id].URL != run.url {
			run.cancel()
			delete(r.runs, id)
		}
	}
	for id, s := range want {
		if r.runs[id] == nil && !s.locked() {
			sctx, cancel := context.WithCancel(ctx)
			run := &recRun{s.URL, cancel}
			r.runs[id] = run
			r.wg.Add(1)
			go r.stream(sctx, s, run)
		}
	}
}

// stream: nối go2rtc, ghi tới khi luồng hết. Mỗi lần go2rtc nối là một lần gửi mật khẩu qua RTSP, sai vài lần thì đầu ghi
// khoá IP của hub: nguồn có client ISAPI (đầu ghi, camera Hikvision) thử tài khoản qua ISAPI trước mỗi lần nối, bị khoá
// thì thôi. Lần nối đã ra file thì chờ 2 giây rồi nối lại; không ra file thì chờ lần kiểm 60 giây. Lỗi ghi file thì thôi hẳn
// tới lần kiểm ổ kế tiếp.
func (r *recorder) stream(ctx context.Context, s recSource, me *recRun) {
	defer r.wg.Done()
	defer func() {
		me.cancel()
		r.mu.Lock()
		if r.runs[s.ID] == me {
			delete(r.runs, s.ID)
		}
		r.mu.Unlock()
	}()
	for {
		if s.client != nil {
			r.probe(ctx, s)
			if s.locked() {
				return
			}
		}
		files, bad := r.once(ctx, s)
		if bad {
			r.writeFailed()
			return
		}
		if files == 0 {
			return
		}
		t := time.NewTimer(r.backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		r.mu.Lock()
		on := r.ok && !r.stopped
		r.mu.Unlock()
		if !on {
			return
		}
	}
}

// probe: một yêu cầu ISAPI qua client dùng chung; sai mật khẩu thì Transport bật khoá 15 phút và sync bỏ qua nguồn này.
// Chạy lần lượt (probeMu): Transport chỉ xem khoá lúc bắt đầu yêu cầu, thử song song thì mỗi kênh gửi một lần sai.
// Không log lỗi của client (có địa chỉ). Giới hạn đã chấp nhận: không tới được ISAPI (hay 401 không phải Digest) thì không
// khoá, vẫn nối RTSP mỗi lần kiểm.
// ponytail: một khoá cho mọi camera, thiết bị không trả lời giữ khoá tới 10 giây; khoá theo client nếu nhiều camera chết.
func (r *recorder) probe(ctx context.Context, s recSource) {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.probe, nil)
	res, err := s.client.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		log.Printf("ghi hình %s: camera từ chối tài khoản, tạm ngừng ghi 15 phút", s.ID)
	}
}

// once: một lần nối go2rtc. Trả số file đã xong và bad = lỗi tạo/ghi/đóng file (khác lỗi đọc luồng). Quá r.idle không
// nhận byte nào (kể cả lúc chờ đầu trả lời) thì huỷ yêu cầu: luồng đứng.
// Tên luồng go2rtc rec_<id>: id camera luôn bắt đầu bằng cam_, nên không trùng luồng xem trực tiếp (ch<kênh>, <id>).
func (r *recorder) once(ctx context.Context, s recSource) (files int, bad bool) {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	idle := time.AfterFunc(r.idle, func() { stalled.Store(true); cancel() })
	defer idle.Stop()
	u := *r.go2rtc
	u.Path, u.RawQuery = "/api/stream.mp4", url.Values{"name": {"rec_" + s.ID}, "src": {s.URL}}.Encode()
	req, _ := http.NewRequestWithContext(rctx, http.MethodGet, u.String(), nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		switch {
		case stalled.Load():
			log.Printf("ghi hình %s: luồng đứng", s.ID)
		case ctx.Err() == nil:
			log.Printf("ghi hình %s: go2rtc không trả lời", s.ID)
		}
		return 0, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Printf("ghi hình %s: go2rtc HTTP %d", s.ID, res.StatusCode)
		return 0, false
	}
	err = recSplit(recIdle{res.Body, idle, r.idle}, recMaxBytes, func(start time.Time) (io.WriteCloser, string, error) {
		w, p, err := r.create(s.ID, start)
		if err != nil {
			bad = true
			return nil, "", err
		}
		return recOut{w, &bad}, p, nil
	}, time.Now, func(seg recSeg) {
		files++
		idle.Stop() // ghi chỉ mục, chờ r.plan và dọn ổ không phải luồng đứng
		r.finished(s.ID, seg)
		idle.Reset(r.idle)
	})
	switch {
	case bad:
		log.Printf("ghi hình %s: không ghi được file", s.ID)
	case stalled.Load():
		log.Printf("ghi hình %s: luồng đứng", s.ID)
	case err != nil && ctx.Err() == nil:
		log.Printf("ghi hình %s: luồng ngắt hoặc hỏng", s.ID)
	}
	return files, bad
}

// recIdle: đọc được byte nào thì lùi hẹn giờ luồng đứng.
type recIdle struct {
	io.Reader
	t *time.Timer
	d time.Duration
}

func (b recIdle) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if n > 0 {
		b.t.Reset(b.d)
	}
	return n, err
}

// recOut đánh dấu lỗi ghi (ổ bị rút, ổ đầy) để phân biệt với lỗi đọc luồng go2rtc.
type recOut struct {
	io.WriteCloser
	bad *bool
}

func (o recOut) Write(b []byte) (int, error) {
	n, err := o.WriteCloser.Write(b)
	*o.bad = *o.bad || err != nil
	return n, err
}

func (o recOut) Close() error {
	err := o.WriteCloser.Close()
	*o.bad = *o.bad || err != nil
	return err
}

// createFile: dir/<id>/<YYYYMMDD>/<HHMMSS>.mp4 theo giờ ICT; trùng tên thì thêm -1, -2…
func (r *recorder) createFile(cam string, start time.Time) (io.WriteCloser, string, error) {
	t := start.In(tgICT)
	day := filepath.Join(r.dir, cam, t.Format("20060102"))
	if err := os.MkdirAll(day, 0o755); err != nil {
		return nil, "", err
	}
	base := filepath.Join(day, t.Format("150405"))
	p := base + ".mp4"
	for i := 1; ; i++ {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, p, nil
		}
		if !errors.Is(err, fs.ErrExist) || i > 99 {
			return nil, "", err
		}
		p = fmt.Sprintf("%s-%d.mp4", base, i)
	}
}

// writeFailed: đánh dấu ổ hỏng tới lần kiểm kế tiếp (luồng khác không nối lại), báo một lần mỗi lần chuyển trạng thái.
func (r *recorder) writeFailed() {
	r.mu.Lock()
	was := r.ok
	r.ok = false
	r.mu.Unlock()
	if was {
		r.notify("warn", "luu_tru", "Không ghi được vào ổ ghi hình, đã dừng ghi.")
	}
}

// finished: file xong thì vào chỉ mục (kèm chỉ mục GOP) và áp lệnh giữ đang chờ phủ file này (cùng dưới r.plan: quy tắc
// xoá không chen vào giữa), rồi chạy quy tắc xoá. Lệnh giữ chỉ bỏ khi chính camera đó có file kết thúc sau khoảng giữ
// (file camera khác không tính: camera ghi chậm có file dài hơn 10 phút), hoặc sau 6 giờ.
func (r *recorder) finished(cam string, s recSeg) {
	r.plan.Lock()
	if _, err := recAdd(r.db, recFile{Cam: cam, Path: s.Path, Start: s.Start, End: s.End, Bytes: s.Bytes, Index: &s.Index}); err != nil {
		log.Printf("ghi hình %s: không ghi được chỉ mục", cam) // file mồ côi: lần khởi động sau sẽ thêm lại
	}
	now := time.Now()
	var apply []recHold
	r.mu.Lock()
	keep := r.holds[:0]
	for _, h := range r.holds {
		if h.cam == cam && !s.Start.After(h.to) && !s.End.Before(h.from) {
			apply = append(apply, h)
		}
		if h.cam == cam && s.End.After(h.to) || now.Sub(h.at) > 6*time.Hour {
			continue
		}
		keep = append(keep, h)
	}
	r.holds = keep
	r.mu.Unlock()
	for _, h := range apply {
		r.hold(h)
	}
	r.plan.Unlock()
	r.retain()
}

// KeepAround giữ đoạn [t−30s, t+30s] của camera: file đã có trong chỉ mục giữ ngay, file đang ghi giữ khi ghi xong.
func (r *recorder) KeepAround(cam string, t time.Time) {
	h := recHold{cam, t.Add(-30 * time.Second), t.Add(30 * time.Second), time.Now()}
	r.plan.Lock()
	defer r.plan.Unlock()
	r.mu.Lock()
	r.holds = append(r.holds, h)
	r.mu.Unlock()
	r.hold(h)
}

// hold: bên gọi giữ r.plan.
func (r *recorder) hold(h recHold) {
	if _, err := recHoldRange(r.db, h.cam, h.from, h.to); err != nil {
		log.Printf("ghi hình %s: không đánh dấu giữ được", h.cam)
	}
}
