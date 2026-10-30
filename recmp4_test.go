package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

// Bộ dựng luồng MP4 phân mảnh giả giống go2rtc: ftyp, moov (một track hình), rồi từng cặp moof+mdat.

func mkBox(typ string, parts ...[]byte) []byte {
	n := 8
	for _, p := range parts {
		n += len(p)
	}
	b := binary.BigEndian.AppendUint32(nil, uint32(n))
	b = append(b, typ...)
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

func u32(v ...uint32) []byte {
	var b []byte
	for _, x := range v {
		b = binary.BigEndian.AppendUint32(b, x)
	}
	return b
}

// fmp4Head dựng ftyp+moov: track hình ID 1, mục mẫu codec ("hev1" hoặc "avc1"), mdhd timescale 90000.
func fmp4Head(codec string) []byte {
	tkhd := mkBox("tkhd", u32(0, 0, 0, 1, 0, 0))
	mdhd := mkBox("mdhd", u32(0, 0, 0, 90000, 0, 0))
	hdlr := mkBox("hdlr", u32(0, 0), []byte("vide"), u32(0, 0, 0), []byte{0})
	stsd := mkBox("stsd", u32(0, 1), mkBox(codec, make([]byte, 78)))
	stbl := mkBox("stbl", stsd)
	minf := mkBox("minf", stbl)
	mdia := mkBox("mdia", mdhd, hdlr, minf)
	moov := mkBox("moov", mkBox("mvhd", make([]byte, 100)), mkBox("trak", tkhd, mdia))
	return append(mkBox("ftyp", []byte("isom"), u32(0), []byte("isomhvc1")), moov...)
}

// fmp4Frame dựng một cặp moof+mdat một mẫu. tfdtVer 0 hoặc 1; payload là size byte có mẫu dễ nhận ra.
func fmp4Frame(track uint32, dts uint64, key bool, size int, tfdtVer int) []byte {
	flags := uint32(0x01010000)
	if key {
		flags = 0x02000000
	}
	tfhd := mkBox("tfhd", u32(0x020038, track, 3000, uint32(size), flags))
	var tfdt []byte
	if tfdtVer == 1 {
		tfdt = mkBox("tfdt", u32(1<<24), binary.BigEndian.AppendUint64(nil, dts))
	} else {
		tfdt = mkBox("tfdt", u32(0, uint32(dts)))
	}
	trun := mkBox("trun", u32(1, 1, 0))
	moof := mkBox("moof", mkBox("mfhd", u32(0, 1)), mkBox("traf", tfhd, tfdt, trun))
	binary.BigEndian.PutUint32(moof[len(moof)-4:], uint32(len(moof)+8))
	pay := make([]byte, size)
	for i := range pay {
		pay[i] = byte(int(dts/3000) + i)
	}
	return append(moof, mkBox("mdat", pay)...)
}

// fakeFMP4 dựng luồng hình: mỗi phần tử của gops là một GOP, các số là cỡ mdat từng khung (khung đầu là keyframe).
// tfdt tăng 3000 mỗi khung hình, liên tục qua các GOP.
func fakeFMP4(codec string, gops [][]int) []byte {
	out := fmp4Head(codec)
	dts := uint64(1_000_000)
	for _, g := range gops {
		for i, sz := range g {
			out = append(out, fmp4Frame(1, dts, i == 0, sz, 1)...)
			dts += 3000
		}
	}
	return out
}

func uniformGOPs(n, frames, size int) [][]int {
	g := make([][]int, n)
	for i := range g {
		g[i] = make([]int, frames)
		for j := range g[i] {
			g[i][j] = size
		}
	}
	return g
}

type nopWC struct{ *bytes.Buffer }

func (nopWC) Close() error { return nil }

// runSplit chạy recSplit vào bộ nhớ; now tăng 1 giây mỗi lần gọi.
func runSplit(in io.Reader, max int64) (files [][]byte, segs []recSeg, err error) {
	var cur *bytes.Buffer
	t := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	err = recSplit(in, max, func(time.Time) (io.WriteCloser, string, error) {
		cur = &bytes.Buffer{}
		files = append(files, nil)
		return nopWC{cur}, "f", nil
	}, func() time.Time { t = t.Add(time.Second); return t }, func(s recSeg) {
		files[len(files)-1] = append([]byte(nil), cur.Bytes()...)
		segs = append(segs, s)
	})
	return
}

type tbox struct {
	typ  string
	b    []byte // cả box
	body []byte
}

// topBoxes tách file thành các box cấp ngoài; báo lỗi nếu có byte thừa hoặc box cụt.
func topBoxes(t *testing.T, b []byte) (out []tbox) {
	t.Helper()
	n := 0
	eachBox(b, 0, len(b), func(x box) {
		out = append(out, tbox{x.typ, b[x.hdr:x.end], b[x.body:x.end]})
		n = x.end
	})
	if n != len(b) {
		t.Fatalf("file có box dở: đọc được %d/%d byte", n, len(b))
	}
	return
}

func mdatBytes(t *testing.T, b []byte) (out []byte) {
	for _, x := range topBoxes(t, b) {
		if x.typ == "mdat" {
			out = append(out, x.body...)
		}
	}
	return
}

func TestRecSplitCutsAtKeyframesUnderLimit(t *testing.T) {
	in := fakeFMP4("hev1", uniformGOPs(30, 50, 2000))
	files, segs, err := runSplit(bytes.NewReader(in), 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 || len(segs) != len(files) {
		t.Fatalf("files=%d segs=%d", len(files), len(segs))
	}
	var got []byte
	for i, f := range files {
		if len(f) > 1_000_000 || segs[i].Bytes != int64(len(f)) || !segs[i].End.After(segs[i].Start) {
			t.Fatalf("file %d: %d byte, seg %+v", i, len(f), segs[i])
		}
		bs := topBoxes(t, f)
		if bs[0].typ != "ftyp" || bs[1].typ != "moov" || bs[2].typ != "moof" {
			t.Fatalf("file %d mở đầu sai: %s %s %s", i, bs[0].typ, bs[1].typ, bs[2].typ)
		}
		if tr := parseMoof(bs[2].b); len(tr) != 1 || !tr[0].sync {
			t.Fatalf("file %d không bắt đầu ở keyframe", i)
		}
		got = append(got, mdatBytes(t, f)...)
	}
	if !bytes.Equal(got, mdatBytes(t, in)) {
		t.Fatal("mdat nối lại khác đầu vào")
	}
}

func TestRecSplitRenamesHev1(t *testing.T) {
	for codec, want := range map[string]string{"hev1": "hvc1", "avc1": "avc1"} {
		files, _, err := runSplit(bytes.NewReader(fakeFMP4(codec, uniformGOPs(2, 3, 100))), recMaxBytes)
		if err != nil {
			t.Fatal(err)
		}
		moov := topBoxes(t, files[0])[1].b
		if !bytes.Contains(moov, []byte(want)) || bytes.Contains(moov, []byte("hev1")) && codec == "hev1" {
			t.Fatalf("%s: moov không có %s", codec, want)
		}
	}
}

func fileTfdts(t *testing.T, f []byte) (out []uint64) {
	for _, x := range topBoxes(t, f) {
		if x.typ == "moof" {
			tr := parseMoof(x.b)
			out = append(out, readTfdt(x.b, tr[0]))
		}
	}
	return
}

func TestRecSplitRebasesTfdt(t *testing.T) {
	files, _, err := runSplit(bytes.NewReader(fakeFMP4("hev1", uniformGOPs(10, 20, 3000))), 100_000)
	if err != nil || len(files) < 3 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	for i, f := range files {
		d := fileTfdts(t, f)
		if d[0] != 0 {
			t.Fatalf("file %d: tfdt đầu = %d", i, d[0])
		}
		for j := 1; j < len(d); j++ {
			if d[j] != d[j-1]+3000 {
				t.Fatalf("file %d: tfdt %d -> %d", i, d[j-1], d[j])
			}
		}
	}
}

func TestRecSplitRebasesTfdtV0(t *testing.T) {
	in := fmp4Head("avc1")
	for i := 0; i < 4; i++ {
		in = append(in, fmp4Frame(1, uint64(5000+3000*i), i%2 == 0, 500, 0)...)
	}
	files, _, err := runSplit(bytes.NewReader(in), recMaxBytes)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	if d := fileTfdts(t, files[0]); len(d) != 4 || d[0] != 0 || d[3] != 9000 {
		t.Fatalf("tfdt v0 = %v", d)
	}
}

func TestRecSplitDropsUntilFirstKeyframe(t *testing.T) {
	in := fmp4Head("hev1")
	for i := 0; i < 3; i++ {
		in = append(in, fmp4Frame(1, uint64(3000*i), false, 111, 1)...)
	}
	in = append(in, fmp4Frame(1, 9000, true, 222, 1)...)
	in = append(in, fmp4Frame(1, 12000, false, 333, 1)...)
	files, _, err := runSplit(bytes.NewReader(in), recMaxBytes)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	bs := topBoxes(t, files[0])
	if len(bs) != 6 || len(bs[3].body) != 222 || len(bs[5].body) != 333 {
		t.Fatalf("đầu ra không bắt đầu ở keyframe: %d box", len(bs))
	}
}

func TestRecSplitTruncatedStream(t *testing.T) {
	in := fakeFMP4("hev1", uniformGOPs(10, 10, 2000))
	files, segs, err := runSplit(bytes.NewReader(in[:len(in)-500]), 60_000)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("err = %v", err)
	}
	if len(files) < 2 || len(segs) != len(files) {
		t.Fatalf("files=%d segs=%d", len(files), len(segs))
	}
	for _, f := range files {
		topBoxes(t, f)
	}
	// khung cụt không có trong đầu ra: tổng mdat thiếu đúng khung cuối
	var n int
	for _, f := range files {
		n += len(mdatBytes(t, f))
	}
	if n != 99*2000 {
		t.Fatalf("tổng mdat = %d", n)
	}
}

func TestRecSplitLongGOP(t *testing.T) {
	// GOP 3 MB, maxBytes 5 MB: mỗi file đúng một GOP.
	files, _, err := runSplit(bytes.NewReader(fakeFMP4("hev1", uniformGOPs(4, 300, 10_000))), 5_000_000)
	if err != nil || len(files) != 4 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	for _, f := range files {
		if len(f) > 5_000_000 {
			t.Fatalf("file %d byte", len(f))
		}
	}
}

// eofProbe ghi lại số byte đã ra file ngay khi luồng hết, để thấy bộ đệm GOP có bị chặn không.
type eofProbe struct {
	r       io.Reader
	written *int
	atEOF   int
}

func (p *eofProbe) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err == io.EOF {
		p.atEOF = *p.written
	}
	return n, err
}

type countWC struct {
	bytes.Buffer
	n *int
}

func (c *countWC) Write(b []byte) (int, error) { *c.n += len(b); return c.Buffer.Write(b) }
func (c *countWC) Close() error                { return nil }

func TestRecSplitHugeGOPBufferCapped(t *testing.T) {
	huge := make([]int, 20)
	for i := range huge {
		huge[i] = 1_000_000
	}
	in := fakeFMP4("hev1", [][]int{{1000, 1000}, huge})
	var out int
	p := &eofProbe{r: bytes.NewReader(in), written: &out}
	var files []*countWC
	err := recSplit(p, 5_000_000, func(time.Time) (io.WriteCloser, string, error) {
		files = append(files, &countWC{n: &out})
		return files[len(files)-1], "f", nil
	}, time.Now, func(recSeg) {})
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	if got := len(mdatBytes(t, files[0].Bytes())); got != 20_002_000 {
		t.Fatalf("ghi %d byte mdat", got)
	}
	// tới lúc luồng hết, phần vượt recGopCap đã phải ra file, không nằm cả trong bộ nhớ
	if p.atEOF < 20_000_000-recGopCap {
		t.Fatalf("lúc hết luồng mới ghi %d byte: bộ đệm không bị chặn", p.atEOF)
	}
}

func TestRecSplitAudioMoofNotCut(t *testing.T) {
	in := fmp4Head("hev1")
	dts := uint64(0)
	for g := 0; g < 3; g++ {
		for i := 0; i < 4; i++ {
			in = append(in, fmp4Frame(1, dts, i == 0, 1000, 1)...)
			// âm thanh (track 2) mang cờ đồng bộ nhưng không được thành chỗ cắt
			in = append(in, fmp4Frame(2, dts, true, 100, 1)...)
			dts += 3000
		}
	}
	files, _, err := runSplit(bytes.NewReader(in), 9_000)
	if err != nil || len(files) != 3 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	for i, f := range files {
		if tr := parseMoof(topBoxes(t, f)[2].b); tr[0].track != 1 || !tr[0].sync {
			t.Fatalf("file %d mở đầu bằng moof track %d", i, tr[0].track)
		}
	}
}
