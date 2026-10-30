package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Bộ ghi: mỗi camera một luồng MP4 phân mảnh từ go2rtc (GET /api/stream.mp4), recSplit cắt file, ghi chỉ mục, xoá theo
// recPlan. URL nguồn có mật khẩu: log chỉ có id camera và câu cố định hoặc mã HTTP, không in lỗi của http.Client (lỗi đó
// chứa URL).

type recSource struct{ ID, URL string } // URL có mật khẩu, không bao giờ log

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
	create               func(cam string, start time.Time) (io.WriteCloser, string, error)
	tick                 <-chan time.Time // nil = mỗi 60 giây
	backoff0, backoffMax time.Duration

	plan sync.Mutex // kiểm ổ, recPlan + xoá file, đánh dấu giữ: chạy lần lượt (file vừa held không bị xoá theo quyết định cũ)
	wg   sync.WaitGroup

	mu      sync.Mutex
	runs    map[string]*recRun
	holds   []recHold // lệnh giữ chờ file đang ghi
	ok      bool      // ổ đạt ở lần kiểm gần nhất và chưa gặp lỗi ghi; lúc khởi động false nên không báo
	stopped bool      // recPlan báo Stop: chờ còn trống từ 3%
}

func newRecorder(db *DB, go2rtc *url.URL, dir string, disk func() recDisk, sources func() []recSource,
	policies func() map[string]recPolicy, notify func(level, kind, text string)) *recorder {
	r := &recorder{db: db, go2rtc: go2rtc, dir: dir, disk: disk, sources: sources, policies: policies, notify: notify,
		backoff0: 2 * time.Second, backoffMax: time.Minute, runs: map[string]*recRun{}}
	r.create = r.createFile
	return r
}

// recSources: luồng chính của mọi camera: kênh đầu ghi (chưa có IP đầu ghi thì bỏ qua) rồi camera nối thẳng.
func recSources(db *DB, c *cams) []recSource {
	out := []recSource{}
	if n := c.conf(); n.On {
		for _, d := range camDevices(db.Setting("nvr")) {
			out = append(out, recSource{d.ID, nvrRTSP(n, d.Channel, false)})
		}
	}
	direct, err := camList(db)
	if err != nil {
		log.Print("ghi hình: không đọc được danh sách camera")
	}
	for _, rec := range direct {
		out = append(out, recSource{rec.ID, rec.rtsp(false)})
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
	for {
		r.check(ctx)
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

// check: lần kiểm 60 giây. Ổ không đạt thì dừng mọi luồng (báo khi vừa chuyển từ đạt), đang dừng vì đầy thì thôi,
// còn lại mở luồng thiếu và đóng luồng thừa.
func (r *recorder) check(ctx context.Context) {
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
}

// retain: kiểm ổ, chạy recPlan, xoá file trong Delete (không xoá được thì giữ dòng). Stop thì dừng mọi luồng và báo
// một lần; đang dừng mà ổ còn trống từ 3% thì cho ghi lại.
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

// sync: mở luồng cho nguồn mới, đóng luồng của nguồn đã bỏ hoặc đổi URL (mật khẩu mới).
func (r *recorder) sync(ctx context.Context) {
	want := map[string]string{}
	for _, s := range r.sources() {
		want[s.ID] = s.URL
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, run := range r.runs {
		if want[id] != run.url {
			run.cancel()
			delete(r.runs, id)
		}
	}
	for id, u := range want {
		if r.runs[id] == nil {
			sctx, cancel := context.WithCancel(ctx)
			run := &recRun{u, cancel}
			r.runs[id] = run
			r.wg.Add(1)
			go r.stream(sctx, recSource{id, u}, run)
		}
	}
}

// stream: nối go2rtc, ghi tới khi luồng hết, chờ rồi nối lại (2 giây gấp đôi tới 60 giây; ghi xong một file thì về 2 giây).
// Lỗi ghi file thì thôi hẳn tới lần kiểm ổ kế tiếp.
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
	wait := r.backoff0
	for {
		files, bad := r.once(ctx, s)
		if bad {
			r.writeFailed()
			return
		}
		if files > 0 {
			wait = r.backoff0
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		wait = min(2*wait, r.backoffMax)
		r.mu.Lock()
		on := r.ok && !r.stopped
		r.mu.Unlock()
		if !on {
			return
		}
	}
}

// once: một lần nối go2rtc. Trả số file đã xong và bad = lỗi tạo/ghi/đóng file (khác lỗi đọc luồng).
// ponytail: không hẹn giờ đọc; go2rtc giữ kết nối mà không gửi gì thì camera đó ngừng ghi tới khi nguồn đổi hay hubd
// khởi động lại. Gặp thật thì bọc res.Body bằng bộ đọc có hạn chờ.
func (r *recorder) once(ctx context.Context, s recSource) (files int, bad bool) {
	u := *r.go2rtc
	u.Path, u.RawQuery = "/api/stream.mp4", url.Values{"name": {s.ID + "_rec"}, "src": {s.URL}}.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("ghi hình %s: go2rtc không trả lời", s.ID)
		}
		return 0, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Printf("ghi hình %s: go2rtc HTTP %d", s.ID, res.StatusCode)
		return 0, false
	}
	err = recSplit(res.Body, recMaxBytes, func(start time.Time) (io.WriteCloser, string, error) {
		w, p, err := r.create(s.ID, start)
		if err != nil {
			bad = true
			return nil, "", err
		}
		return recOut{w, &bad}, p, nil
	}, time.Now, func(seg recSeg) {
		files++
		r.finished(s.ID, seg)
	})
	switch {
	case bad:
		log.Printf("ghi hình %s: không ghi được file", s.ID)
	case err != nil && ctx.Err() == nil:
		log.Printf("ghi hình %s: luồng ngắt hoặc hỏng", s.ID)
	}
	return files, bad
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

// finished: file xong thì vào chỉ mục, áp lệnh giữ đang chờ phủ file này, rồi chạy quy tắc xoá. Lệnh giữ bỏ khi đã có file
// của camera kết thúc sau khoảng giữ, hoặc sau 10 phút.
func (r *recorder) finished(cam string, s recSeg) {
	if _, err := recAdd(r.db, recFile{Cam: cam, Path: s.Path, Start: s.Start, End: s.End, Bytes: s.Bytes}); err != nil {
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
		if h.cam == cam && s.End.After(h.to) || now.Sub(h.at) > 10*time.Minute {
			continue
		}
		keep = append(keep, h)
	}
	r.holds = keep
	r.mu.Unlock()
	for _, h := range apply {
		r.hold(h)
	}
	r.retain()
}

// KeepAround giữ đoạn [t−30s, t+30s] của camera: file đã có trong chỉ mục giữ ngay, file đang ghi giữ khi ghi xong.
func (r *recorder) KeepAround(cam string, t time.Time) {
	h := recHold{cam, t.Add(-30 * time.Second), t.Add(30 * time.Second), time.Now()}
	r.mu.Lock()
	r.holds = append(r.holds, h) // trước recHoldRange: file xong giữa hai bước vẫn thấy lệnh giữ
	r.mu.Unlock()
	r.hold(h)
}

func (r *recorder) hold(h recHold) {
	r.plan.Lock()
	defer r.plan.Unlock()
	if _, err := recHoldRange(r.db, h.cam, h.from, h.to); err != nil {
		log.Printf("ghi hình %s: không đánh dấu giữ được", h.cam)
	}
}
