package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecDiskSameDeviceRefused(t *testing.T) {
	d := t.TempDir()
	sys := filepath.Join(d, "config.yaml")
	os.WriteFile(sys, nil, 0o600)
	if r := recDiskCheck(d, sys); r.OK || r.Reason != recOnSys {
		t.Fatalf("%+v", r)
	}
}

func TestRecDiskMissing(t *testing.T) {
	if r := recDiskCheck(filepath.Join(t.TempDir(), "khong-co"), t.TempDir()); r.OK || r.Reason != "Chưa gắn thư mục ghi hình" {
		t.Fatalf("%+v", r)
	}
	if r := recDiskCheck("", t.TempDir()); r.OK || r.Reason != "Chưa gắn thư mục ghi hình" {
		t.Fatalf("%+v", r)
	}
}

// shmDir: thư mục trên /dev/shm (thiết bị khác thư mục tạm của test), không có thì bỏ qua test.
func shmDir(t *testing.T) string {
	if st, err := os.Stat("/dev/shm"); err != nil || !st.IsDir() {
		t.Skip("không có /dev/shm")
	}
	dir, err := os.MkdirTemp("/dev/shm", "rec")
	if err != nil {
		t.Skip("không ghi được /dev/shm")
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestRecDiskOtherDevice(t *testing.T) {
	dir := shmDir(t)
	r := recDiskCheck(dir, t.TempDir())
	if !r.OK || r.Total <= 0 {
		t.Fatalf("%+v", r)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".hubd-write-test*")); len(left) != 0 {
		t.Fatalf("file thử chưa xoá: %v", left)
	}
}

// API dung lượng và bộ ghi kiểm ổ cùng lúc: không lần nào được báo nhầm là không ghi được.
func TestRecDiskConcurrentChecks(t *testing.T) {
	dir, sys := shmDir(t), t.TempDir()
	var wg sync.WaitGroup
	var bad atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if !recDiskCheck(dir, sys).OK {
					bad.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if n := bad.Load(); n != 0 {
		t.Fatalf("%d/400 lần kiểm báo nhầm ổ không đạt", n)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".hubd-write-test*")); len(left) != 0 {
		t.Fatalf("file thử chưa xoá: %v", left)
	}
}

func TestRecNormalPctClamp(t *testing.T) {
	db := newDB(t)
	if p := recNormalPct(db); p != 80 {
		t.Fatalf("thiếu: %d", p)
	}
	for in, want := range map[float64]int{30: 50, 99: 95, 70: 70} {
		db.PutSetting("camera", map[string]any{"rec_max_pct": in})
		if p := recNormalPct(db); p != want {
			t.Errorf("%v -> %d, muốn %d", in, p, want)
		}
	}
}

func TestRecStorageAPI(t *testing.T) {
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	a := NewAuth("1234", h.db)
	ck := call(newMux(h, a), "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	mux := http.NewServeMux()
	mountRecStorage(mux, h.db, a, func() recDisk { return recDisk{OK: true, Total: 1_000_000, Free: 400_000} })
	if w := call(mux, "GET", "/api/rec/storage", "", ""); w.Code != 401 {
		t.Fatalf("không cookie: %d", w.Code)
	}
	now := time.Now()
	add := func(cam string, ago time.Duration, b int64, held bool) {
		if _, err := recAdd(h.db, recFile{Cam: cam, Path: cam + ago.String(), Start: now.Add(-ago - time.Minute), End: now.Add(-ago), Bytes: b, Held: held}); err != nil {
			t.Fatal(err)
		}
	}
	add("cam_ch1", time.Hour, 1000, false)
	add("cam_ch1", 72*time.Hour, 500, false)
	add("cam_ch2", 2*time.Hour, 2500, false)
	add("cam_ch2", 90*time.Hour, 700, true)
	w := call(mux, "GET", "/api/rec/storage", "", "", ck...)
	var got struct {
		OK     bool   `json:"ok"`
		Total  int64  `json:"total"`
		Normal int64  `json:"normal_bytes"`
		Held   int64  `json:"held_bytes"`
		MaxPct int    `json:"max_pct"`
		Reason string `json:"reason"`
		Cams   []struct {
			ID    string   `json:"id"`
			Bytes int64    `json:"bytes"`
			Days  *float64 `json:"days"`
		} `json:"cams"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !got.OK || got.Total != 1_000_000 || got.Normal != 4000 || got.Held != 700 || got.MaxPct != 80 || len(got.Cams) != 2 {
		t.Fatalf("%+v", got)
	}
	// ngân sách 80% × 1 000 000 chia theo rate 1000:2500; days = 800000/3500 = 228,6
	for i, b := range []int64{1500, 3200} {
		c := got.Cams[i]
		if c.Bytes != b || c.Days == nil || *c.Days != 228.6 {
			t.Errorf("%d: %+v", i, c)
		}
	}
	// camera nối thẳng chưa có file: có trong danh sách, days null
	if _, err := camSave(h.db, camRec{Name: "Sân", Kind: "rtsp", Host: "10.0.0.9", MainPath: "/s"}, nil); err != nil {
		t.Fatal(err)
	}
	var again struct {
		Cams []struct {
			Bytes int64
			Days  *float64
		}
	}
	json.Unmarshal(call(mux, "GET", "/api/rec/storage", "", "", ck...).Body.Bytes(), &again)
	if len(again.Cams) != 3 {
		t.Fatalf("%+v", again)
	}
	for _, c := range again.Cams {
		if c.Bytes == 0 && c.Days != nil {
			t.Error("rate 0 phải là null")
		}
	}
}

// Không kiểm được ổ hệ thống thì từ chối, không mặc định cho ghi.
func TestRecDiskFailsClosed(t *testing.T) {
	dir, err := os.MkdirTemp("/dev/shm", "rec")
	if err != nil {
		t.Skip("không ghi được /dev/shm")
	}
	defer os.RemoveAll(dir)
	for _, sys := range []string{"", "/nonexistent/config.yaml"} {
		if r := recDiskCheck(dir, sys); r.OK || r.Reason != recOnSys {
			t.Errorf("sysRef %q: %+v", sys, r)
		}
	}
}

func TestRecStorageDaysNullWhenDiskNotOK(t *testing.T) {
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	a := NewAuth("1234", h.db)
	ck := call(newMux(h, a), "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	mux := http.NewServeMux()
	mountRecStorage(mux, h.db, a, func() recDisk { return recDisk{Reason: recNoDir} })
	recAdd(h.db, recFile{Cam: "cam_ch1", Path: "p", Start: time.Now().Add(-time.Minute), End: time.Now(), Bytes: 100})
	if b := call(mux, "GET", "/api/rec/storage", "", "", ck...).Body.String(); !strings.Contains(b, `"days":null`) {
		t.Fatal(b)
	}
}
