package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// recFx ghi một file ghi hình thật (qua recSplit) vào thư mục tạm rồi thêm vào chỉ mục, không kèm chỉ mục GOP (như file
// nhận nuôi: playlist phải quét file).
func recFx(t *testing.T, db *DB, cam string, start time.Time, gops [][]int) (recFile, []byte) {
	t.Helper()
	return recFxIdx(t, db, cam, start, gops, false)
}

// recFxIdx như recFx; idx = lưu chỉ mục GOP recSplit trả, như bộ ghi làm khi file xong.
func recFxIdx(t *testing.T, db *DB, cam string, start time.Time, gops [][]int, idx bool) (recFile, []byte) {
	t.Helper()
	files, segs, err := runSplit(bytes.NewReader(fakeFMP4("hev1", gops)), 1<<30)
	if err != nil || len(files) != 1 {
		t.Fatal(err, len(files))
	}
	p := filepath.Join(t.TempDir(), "x.mp4")
	if err := os.WriteFile(p, files[0], 0o600); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range gops {
		n += len(g)
	}
	f := recFile{Cam: cam, Path: p, Start: start, End: start.Add(time.Duration(n) * time.Second / 30), Bytes: int64(len(files[0]))}
	if idx {
		f.Index = &segs[0].Index
	}
	id, err := recAdd(db, f)
	if err != nil {
		t.Fatal(err)
	}
	f.ID = id
	return f, files[0]
}

func playMux(t *testing.T) (*http.ServeMux, *DB, []*http.Cookie) {
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	a := NewAuth("1234", h.db)
	ck := call(newMux(h, a), "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	mux := http.NewServeMux()
	mountRecPlay(mux, h.db, a)
	return mux, h.db, ck
}

func TestRecScan(t *testing.T) {
	files, _, _ := runSplit(bytes.NewReader(fakeFMP4("hev1", uniformGOPs(3, 4, 100))), 1<<30)
	p := filepath.Join(t.TempDir(), "a.mp4")
	os.WriteFile(p, files[0], 0o600)
	initLen, gops, err := recScan(p)
	if err != nil {
		t.Fatal(err)
	}
	bs := topBoxes(t, files[0])
	if want := int64(len(bs[0].b) + len(bs[1].b)); initLen != want {
		t.Fatalf("initLen %d, muốn %d", initLen, want)
	}
	if len(gops) != 3 {
		t.Fatalf("%d GOP", len(gops))
	}
	sum := initLen
	for _, g := range gops {
		if g.Off != sum || math.Abs(g.Dur-4*3000.0/90000) > 1e-9 {
			t.Fatalf("GOP %+v, off muốn %d", g, sum)
		}
		sum += g.Len
	}
	if sum != int64(len(files[0])) {
		t.Fatalf("tổng %d, file %d", sum, len(files[0]))
	}
}

func TestRecScanTruncated(t *testing.T) {
	files, _, _ := runSplit(bytes.NewReader(fakeFMP4("hev1", [][]int{{100, 100}, {100, 100}, {100}})), 1<<30)
	p := filepath.Join(t.TempDir(), "a.mp4")
	os.WriteFile(p, files[0][:len(files[0])-10], 0o600) // cụt giữa mdat cuối: GOP cuối (một khung) không đủ
	_, gops, err := recScan(p)
	if err != nil || len(gops) != 2 {
		t.Fatalf("err=%v gops=%d, muốn 2 GOP đủ", err, len(gops))
	}
}

var reRange = regexp.MustCompile(`#EXT-X-BYTERANGE:(\d+)@(\d+)`)

func TestRecPlaylist(t *testing.T) {
	mux, db, ck := playMux(t)
	t0 := time.Date(2026, 10, 30, 10, 0, 0, 0, tgICT)
	f1, _ := recFx(t, db, "cam1", t0, uniformGOPs(3, 30, 100))
	f2, _ := recFx(t, db, "cam1", t0.Add(3*time.Second), uniformGOPs(2, 30, 100))
	u := fmt.Sprintf("/api/cam/cam1/rec.m3u8?from=%d&to=%d", t0.UnixMilli(), f2.End.UnixMilli())
	w := call(mux, "GET", u, "", "", ck...)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for s, n := range map[string]int{"#EXT-X-DISCONTINUITY": 1, "#EXT-X-MAP": 2, "#EXT-X-ENDLIST": 1, "#EXTINF": 5} {
		if c := strings.Count(body, s); c != n {
			t.Errorf("%s: %d, muốn %d\n%s", s, c, n, body)
		}
	}
	if !strings.HasPrefix(body, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:1\n") || !strings.HasSuffix(body, "#EXT-X-ENDLIST\n") {
		t.Errorf("đầu/cuối sai:\n%s", body)
	}
	if strings.Contains(body, "http") || strings.Contains(body, os.TempDir()) || strings.Contains(body, `"/rec/`) || strings.Contains(body, "\n/") {
		t.Errorf("playlist có đường dẫn tuyệt đối:\n%s", body)
	}
	if !strings.Contains(body, fmt.Sprintf("URI=\"rec/%d.mp4\"", f1.ID)) || !strings.Contains(body, fmt.Sprintf("\nrec/%d.mp4\n", f2.ID)) {
		t.Errorf("thiếu URI file:\n%s", body)
	}
	var want []string
	for _, f := range []recFile{f1, f2} {
		_, gs, _ := recScan(f.Path)
		for _, g := range gs {
			want = append(want, fmt.Sprintf("%d@%d", g.Len, g.Off))
		}
	}
	var got []string
	for _, m := range reRange.FindAllStringSubmatch(body, -1) {
		got = append(got, m[1]+"@"+m[2])
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BYTERANGE %v, muốn %v", got, want)
	}
	// from giữa GOP thứ hai của file 1, to trong GOP thứ hai: chỉ lấy GOP chứa from tới GOP chứa to
	u = fmt.Sprintf("/api/cam/cam1/rec.m3u8?from=%d&to=%d", t0.Add(1500*time.Millisecond).UnixMilli(), t0.Add(1700*time.Millisecond).UnixMilli())
	if c := strings.Count(call(mux, "GET", u, "", "", ck...).Body.String(), "#EXTINF"); c != 1 {
		t.Errorf("cắt theo from/to: %d đoạn, muốn 1", c)
	}
	// khoảng sai
	for _, bad := range []string{"from=5&to=5", "from=x&to=9", fmt.Sprintf("from=0&to=%d", 25*3600*1000), "from=-1&to=5", "from=0&to=9223372036854775807", ""} {
		if c := call(mux, "GET", "/api/cam/cam1/rec.m3u8?"+bad, "", "", ck...).Code; c != 400 {
			t.Errorf("%q: %d, muốn 400", bad, c)
		}
	}
}

func TestRecSpansMerge(t *testing.T) {
	mux, db, ck := playMux(t)
	t0 := time.Date(2026, 10, 30, 10, 0, 0, 0, tgICT)
	for _, s := range []int{0, 15, 55} { // mỗi file 10 giây: khoảng cách 5 giây rồi 30 giây
		recAdd(db, recFile{Cam: "c", Path: fmt.Sprint("p", s), Start: t0.Add(time.Duration(s) * time.Second), End: t0.Add(time.Duration(s+10) * time.Second)})
	}
	w := call(mux, "GET", "/api/cam/c/rec?day=2026-10-30", "", "", ck...)
	want := fmt.Sprintf(`{"spans":[[%d,%d],[%d,%d]]}`, t0.UnixMilli(), t0.Add(25*time.Second).UnixMilli(), t0.Add(55*time.Second).UnixMilli(), t0.Add(65*time.Second).UnixMilli())
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Fatalf("%s\nmuốn %s", got, want)
	}
	if got := strings.TrimSpace(call(mux, "GET", "/api/cam/khong/rec?day=2026-10-30", "", "", ck...).Body.String()); got != `{"spans":[]}` {
		t.Errorf("camera lạ: %s", got)
	}
	if got := strings.TrimSpace(call(mux, "GET", "/api/cam/c/rec?day=2026-10-29", "", "", ck...).Body.String()); got != `{"spans":[]}` {
		t.Errorf("ngày khác: %s", got)
	}
	if c := call(mux, "GET", "/api/cam/c/rec?day=hom-nay", "", "", ck...).Code; c != 400 {
		t.Errorf("ngày sai: %d", c)
	}
}

func TestRecPlaybackAuth(t *testing.T) {
	mux, db, _ := playMux(t)
	f, _ := recFx(t, db, "c", time.Now(), uniformGOPs(1, 5, 50))
	for _, u := range []string{"/api/cam/c/rec?day=2026-10-30", "/api/cam/c/rec.m3u8?from=1&to=2", fmt.Sprintf("/api/cam/c/rec/%d.mp4", f.ID)} {
		if c := call(mux, "GET", u, "", "").Code; c != 401 {
			t.Errorf("%s không cookie: %d", u, c)
		}
	}
}

func TestRecFileOtherCamera(t *testing.T) {
	mux, db, ck := playMux(t)
	f, _ := recFx(t, db, "cam_a", time.Now(), uniformGOPs(1, 5, 50))
	if c := call(mux, "GET", fmt.Sprintf("/api/cam/cam_b/rec/%d.mp4", f.ID), "", "", ck...).Code; c != 404 {
		t.Errorf("camera khác: %d", c)
	}
	for _, bad := range []string{"abc.mp4", "999999.mp4", strconv.FormatInt(f.ID, 10)} {
		if c := call(mux, "GET", "/api/cam/cam_a/rec/"+bad, "", "", ck...).Code; c != 404 {
			t.Errorf("%s: %d", bad, c)
		}
	}
	if c := call(mux, "GET", fmt.Sprintf("/api/cam/cam_a/rec/%d.mp4", f.ID), "", "", ck...).Code; c != 200 {
		t.Errorf("đúng camera: %d", c)
	}
}

func TestRecFileRange(t *testing.T) {
	mux, db, ck := playMux(t)
	f, data := recFx(t, db, "c", time.Now(), uniformGOPs(1, 5, 500))
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/cam/c/rec/%d.mp4", f.ID), nil)
	r.Header.Set("Range", "bytes=0-99")
	r.AddCookie(ck[0])
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Body)
	if w.Code != 206 || len(b) != 100 || !bytes.Equal(b, data[:100]) {
		t.Fatalf("%d, %d byte", w.Code, len(b))
	}
}

func TestDashReplayWired(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("web", "dash.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, s := range []string{"data-replay", "Giờ này không có hình", "[data-replay]", "rec.m3u8", "Xem lại chạy trên Safari"} {
		if !strings.Contains(js, s) {
			t.Errorf("dash.js thiếu %q", s)
		}
	}
	i := strings.Index(js, "function liveFill")
	if i < 0 || !strings.Contains(js[i:i+900], "[data-replay]") {
		t.Error("liveFill không giữ [data-replay]")
	}
}

// trunFrame dựng cặp moof+mdat một mẫu, thời lượng khai theo từng nguồn: "tfhd" (default_sample_duration, như go2rtc),
// "trun" (cờ 0x100 từng mẫu) hoặc "trex" (không khai trong moof).
func trunFrame(src string, dts uint64, key bool, dur uint32) []byte {
	flags := uint32(0x01010000)
	if key {
		flags = 0x02000000
	}
	tfhdFl, tfhd, trunFl, trun := uint32(0x020020), u32(1, flags), uint32(1), u32(0)
	switch src {
	case "tfhd":
		tfhdFl, tfhd = 0x020028, u32(1, dur, flags)
	case "trun":
		trunFl, trun = 0x101, u32(0, dur)
	}
	moof := mkBox("moof", mkBox("mfhd", u32(0, 1)), mkBox("traf", mkBox("tfhd", append(u32(tfhdFl), tfhd...)),
		mkBox("tfdt", u32(1<<24), binary.BigEndian.AppendUint64(nil, dts)), mkBox("trun", append(u32(trunFl, 1), trun...))))
	return append(moof, mkBox("mdat", make([]byte, 50))...)
}

func TestRecScanDurationSources(t *testing.T) {
	trexDur := uint32(4500)
	for _, src := range []string{"tfhd", "trun", "trex"} {
		head := fmp4Head("hev1")
		if src == "trex" { // trex: track 1, desc 1, duration 4500, size 0, flags 0
			head = fmp4HeadTrexDur("hev1", trexDur)
		}
		in := head
		for i := 0; i < 6; i++ {
			in = append(in, trunFrame(src, uint64(i)*4500, i%3 == 0, 4500)...)
		}
		p := filepath.Join(t.TempDir(), src+".mp4")
		files, _, err := runSplit(bytes.NewReader(in), 1<<30)
		if err != nil || len(files) != 1 {
			t.Fatal(src, err)
		}
		os.WriteFile(p, files[0], 0o600)
		_, gops, err := recScan(p)
		var sum float64
		for _, g := range gops {
			sum += g.Dur
		}
		if err != nil || len(gops) != 2 || math.Abs(sum-6*4500.0/90000) > 1e-9 {
			t.Errorf("%s: err=%v gops=%d tổng Dur %v, muốn 2 GOP và 0.3", src, err, len(gops), sum)
		}
	}
}

// fmp4HeadTrexDur như fmp4Head, thêm mvex/trex có default_sample_duration.
func fmp4HeadTrexDur(codec string, dur uint32) []byte {
	h := fmp4Head(codec)
	bs := topBoxes(&testing.T{}, h)
	moov := append([]byte(nil), bs[1].b...)
	moov = append(moov, mkBox("mvex", mkBox("trex", u32(0, 1, 1, dur, 0, 0)))...)
	binary.BigEndian.PutUint32(moov, uint32(len(moov)))
	return append(append([]byte(nil), bs[0].b...), moov...)
}

func TestRecScanTruncatedMidGOP(t *testing.T) {
	files, _, _ := runSplit(bytes.NewReader(fakeFMP4("hev1", [][]int{{100, 100, 100}, {100, 100, 100}})), 1<<30)
	p := filepath.Join(t.TempDir(), "a.mp4")
	os.WriteFile(p, files[0][:len(files[0])-10], 0o600) // cụt giữa mdat khung thứ 3 của GOP 2
	_, gops, err := recScan(p)
	last := int64(len(fmp4Frame(1, 0, false, 100, 1)))
	if err != nil || len(gops) != 2 {
		t.Fatalf("err=%v gops=%d", err, len(gops))
	}
	if end := gops[1].Off + gops[1].Len; end != int64(len(files[0]))-last || math.Abs(gops[1].Dur-2*3000.0/90000) > 1e-9 {
		t.Errorf("GOP cuối kết thúc %d (muốn %d), Dur %v (muốn 2 khung)", end, int64(len(files[0]))-last, gops[1].Dur)
	}
}

// splitIndexMatchesScan: mỗi file recSplit ghi ra có chỉ mục GOP (recSeg.Index) trùng hệt kết quả quét file.
func splitIndexMatchesScan(t *testing.T, name string, in []byte, max int64, wantFiles int) {
	t.Helper()
	files, segs, err := runSplit(bytes.NewReader(in), max)
	if err != nil || len(files) != wantFiles || len(segs) != wantFiles {
		t.Fatalf("%s: err=%v files=%d, muốn %d", name, err, len(files), wantFiles)
	}
	for i, f := range files {
		p := filepath.Join(t.TempDir(), "f.mp4")
		os.WriteFile(p, f, 0o600)
		init, gops, err := recScan(p)
		if err != nil || len(gops) == 0 {
			t.Fatalf("%s file %d: quét %v %d GOP", name, i, err, len(gops))
		}
		if segs[i].Index.Init != init || !reflect.DeepEqual(segs[i].Index.GOPs, gops) {
			t.Errorf("%s file %d: chỉ mục lúc ghi %d %+v, quét %d %+v", name, i, segs[i].Index.Init, segs[i].Index.GOPs, init, gops)
		}
	}
}

func TestRecSplitIndexMatchesScan(t *testing.T) {
	for _, src := range []string{"tfhd", "trun", "trex"} { // ba nguồn thời lượng mẫu
		head := fmp4Head("hev1")
		if src == "trex" {
			head = fmp4HeadTrexDur("hev1", 4500)
		}
		in := head
		for i := 0; i < 9; i++ {
			in = append(in, trunFrame(src, uint64(i)*4500, i%3 == 0, 4500)...)
		}
		splitIndexMatchesScan(t, src, in, 1<<30, 1)
		splitIndexMatchesScan(t, src+" cắt nhiều file", in, int64(len(head)+3*len(trunFrame(src, 0, true, 4500))), 3)
	}
	// âm thanh xen giữa: GOP kết thúc ở cặp hình cuối, như recScan
	in := fmp4Head("hev1")
	dts := uint64(0)
	for g := 0; g < 3; g++ {
		for i := 0; i < 4; i++ {
			in = append(in, fmp4Frame(1, dts, i == 0, 1000, 1)...)
			in = append(in, fmp4Frame(2, dts, true, 100, 1)...)
			dts += 3000
		}
	}
	splitIndexMatchesScan(t, "âm thanh", in, 9_000, 3)
	// GOP vượt recGopCap (ghi bớt giữa GOP, sang file mới)
	huge := make([]int, 20)
	for i := range huge {
		huge[i] = 1_000_000
	}
	splitIndexMatchesScan(t, "GOP lớn", fakeFMP4("hev1", [][]int{{1000, 1000}, huge, {1000}}), 5_000_000, 3)
}

// Playlist dựng từ chỉ mục lưu trùng playlist quét file; có chỉ mục thì không mở file (xoá file vẫn ra playlist).
func TestRecPlaylistStoredIndex(t *testing.T) {
	mux, db, ck := playMux(t)
	t0 := time.Date(2026, 10, 30, 10, 0, 0, 0, tgICT)
	f1, _ := recFxIdx(t, db, "cam1", t0, uniformGOPs(3, 30, 100), true)
	f2, _ := recFxIdx(t, db, "cam1", t0.Add(3*time.Second), uniformGOPs(2, 30, 100), true)
	u := fmt.Sprintf("/api/cam/cam1/rec.m3u8?from=%d&to=%d", t0.UnixMilli(), f2.End.UnixMilli())
	stored := call(mux, "GET", u, "", "", ck...)
	if stored.Code != 200 || strings.Count(stored.Body.String(), "#EXTINF") != 5 {
		t.Fatalf("%d\n%s", stored.Code, stored.Body)
	}
	var keep []string
	for _, f := range []recFile{f1, f2} {
		var raw string
		db.sql.QueryRow(`SELECT gops FROM recording_gops WHERE id = ?`, f.ID).Scan(&raw)
		keep = append(keep, raw)
	}
	db.sql.Exec(`DELETE FROM recording_gops`)
	if scanned := call(mux, "GET", u, "", "", ck...).Body.String(); scanned != stored.Body.String() {
		t.Fatalf("playlist quét khác playlist từ chỉ mục:\n%s\n---\n%s", scanned, stored.Body)
	}
	// file còn nhưng nội dung hỏng: có chỉ mục thì không quét file, playlist vẫn y như cũ
	for i, f := range []recFile{f1, f2} {
		db.sql.Exec(`INSERT INTO recording_gops(id, gops) VALUES(?, ?)`, f.ID, keep[i])
		os.WriteFile(f.Path, []byte("hỏng"), 0o600)
	}
	if w := call(mux, "GET", u, "", "", ck...); w.Code != 200 || w.Body.String() != stored.Body.String() {
		t.Fatalf("có chỉ mục mà vẫn đọc file: %d\n%s", w.Code, w.Body)
	}
}

// File đã mất khỏi đĩa (ổ thay, chưa gắn) thì playlist bỏ qua nó, vẫn phát các file còn lại; mất hết thì 404.
func TestRecPlaylistSkipsMissingFile(t *testing.T) {
	mux, db, ck := playMux(t)
	t0 := time.Date(2026, 10, 30, 10, 0, 0, 0, tgICT)
	f1, _ := recFxIdx(t, db, "cam1", t0, uniformGOPs(3, 30, 100), true)
	f2, _ := recFxIdx(t, db, "cam1", t0.Add(3*time.Second), uniformGOPs(2, 30, 100), true)
	u := fmt.Sprintf("/api/cam/cam1/rec.m3u8?from=%d&to=%d", t0.UnixMilli(), f2.End.UnixMilli())
	os.Remove(f1.Path)
	w := call(mux, "GET", u, "", "", ck...)
	b := w.Body.String()
	if w.Code != 200 || strings.Count(b, "#EXT-X-MAP") != 1 || strings.Contains(b, fmt.Sprintf("rec/%d.mp4", f1.ID)) ||
		strings.Count(b, fmt.Sprintf("rec/%d.mp4", f2.ID)) != 3 || strings.Contains(b, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("%d\n%s", w.Code, b)
	}
	os.Remove(f2.Path)
	if w := call(mux, "GET", u, "", "", ck...); w.Code != 404 {
		t.Fatalf("mất hết file: %d, muốn 404", w.Code)
	}
}

// Chỉ mục GOP nằm ở bảng riêng recording_gops (bảng recordings giữ hẹp cho lượt dọn file). Mở được cả hai dạng DB cũ:
// recordings chưa có cột gops (trước 2B), và có cột gops (bản 2B đầu, 24a0b16): chuyển chỉ mục sang bảng riêng, bỏ cột.
// Mở lần nữa không lỗi; recRemove xoá cả chỉ mục.
func TestOpenDBMovesGOPIndex(t *testing.T) {
	const tbl = `CREATE TABLE recordings(id INTEGER PRIMARY KEY, cam TEXT NOT NULL, path TEXT NOT NULL UNIQUE, start INTEGER NOT NULL, end INTEGER NOT NULL, bytes INTEGER NOT NULL, held INTEGER NOT NULL DEFAULT 0`
	cases := []struct {
		name, schema string
		want         bool // dòng 1 có chỉ mục sau khi mở
	}{
		{"chua-co-cot", tbl + `);
INSERT INTO recordings(cam, path, start, end, bytes) VALUES('c', 'p', 1, 2, 3);`, false},
		{"co-cot-gops", tbl + `, gops TEXT);
INSERT INTO recordings(cam, path, start, end, bytes, gops) VALUES('c', 'p', 1, 2, 3, '{"init":5,"gops":[{"o":5,"l":8,"d":2}]}');
INSERT INTO recordings(cam, path, start, end, bytes, gops) VALUES('c', 'p2', 4, 5, 6, NULL);`, true},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "cu.db")
		old, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(c.schema); err != nil {
			t.Fatal(c.name, err)
		}
		old.Close()
		for range 2 {
			db, err := OpenDB(path)
			if err != nil {
				t.Fatal(c.name, err)
			}
			x, ok := recIndexOf(db, 1)
			if ok != c.want || ok && (x.Init != 5 || len(x.GOPs) != 1 || x.GOPs[0] != (recGOP{5, 8, 2})) {
				t.Fatalf("%s: chỉ mục dòng cũ %v %+v", c.name, ok, x)
			}
			var cols int
			db.sql.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('recordings') WHERE name = 'gops'`).Scan(&cols)
			if cols != 0 {
				t.Fatalf("%s: recordings vẫn còn cột gops", c.name)
			}
			id, err := recAdd(db, recFile{Cam: "c", Path: fmt.Sprint("q", time.Now().UnixNano()), Index: &recIndex{Init: 7, GOPs: []recGOP{{7, 9, 2.5}}}})
			if x, ok := recIndexOf(db, id); err != nil || !ok || x.Init != 7 || len(x.GOPs) != 1 || x.GOPs[0] != (recGOP{7, 9, 2.5}) {
				t.Fatalf("%s: dòng mới: %v %v %+v", c.name, err, ok, x)
			}
			if err := recRemove(db, []int64{id}); err != nil {
				t.Fatal(err)
			}
			var left int
			db.sql.QueryRow(`SELECT COUNT(*) FROM recording_gops WHERE id = ?`, id).Scan(&left)
			if _, ok := recIndexOf(db, id); ok || left != 0 {
				t.Fatalf("%s: recRemove để sót chỉ mục", c.name)
			}
			db.sql.Close()
		}
	}
}
