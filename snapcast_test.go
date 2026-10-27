package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

// Đầu nhận giả nói giao thức nhị phân Snapcast (doc/binary_protocol.md của snapcast), như snapclient trên ESP32 / Pi.
type fakeSnapClient struct {
	t *testing.T
	c net.Conn
}

func dialSnap(t *testing.T, addr, id string) *fakeSnapClient {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	f := &fakeSnapClient{t, c}
	hello, _ := json.Marshal(map[string]any{"Arch": "xtensa", "ClientName": "Snapclient", "HostName": "loa-" + id, "ID": id, "Instance": 1,
		"MAC": id, "OS": "ESP32", "SnapStreamProtocolVersion": 2, "Version": "0.27.0"})
	f.send(5, 1, 0, 0, sized(hello))
	return f
}

func sized(b []byte) []byte {
	return append(binary.LittleEndian.AppendUint32(nil, uint32(len(b))), b...)
}

func (f *fakeSnapClient) send(typ, id, refersTo uint16, sentUsec int64, payload []byte) {
	h := make([]byte, 26)
	binary.LittleEndian.PutUint16(h[0:], typ)
	binary.LittleEndian.PutUint16(h[2:], id)
	binary.LittleEndian.PutUint16(h[4:], refersTo)
	binary.LittleEndian.PutUint32(h[6:], uint32(sentUsec/1e6))
	binary.LittleEndian.PutUint32(h[10:], uint32(sentUsec%1e6))
	binary.LittleEndian.PutUint32(h[22:], uint32(len(payload)))
	if _, err := f.c.Write(append(h, payload...)); err != nil {
		f.t.Fatal(err)
	}
}

type snapRecv struct {
	typ, id, refersTo uint16
	sent, recv        int64 // micro giây theo đồng hồ của máy chủ
	payload           []byte
}

func (f *fakeSnapClient) read() snapRecv {
	f.t.Helper()
	f.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	h := make([]byte, 26)
	if _, err := io.ReadFull(f.c, h); err != nil {
		f.t.Fatalf("đọc tin Snapcast: %v", err)
	}
	le := binary.LittleEndian
	tv := func(o int) int64 { return int64(int32(le.Uint32(h[o:])))*1e6 + int64(int32(le.Uint32(h[o+4:]))) }
	m := snapRecv{typ: le.Uint16(h[0:]), id: le.Uint16(h[2:]), refersTo: le.Uint16(h[4:]), sent: tv(6), recv: tv(14), payload: make([]byte, le.Uint32(h[22:]))}
	if _, err := io.ReadFull(f.c, m.payload); err != nil {
		f.t.Fatalf("đọc thân tin: %v", err)
	}
	return m
}

// readType bỏ qua tin khác loại (vd. đoạn nhạc đang chạy) cho tới tin cần xem.
func (f *fakeSnapClient) readType(typ uint16) snapRecv {
	f.t.Helper()
	for i := 0; i < 200; i++ {
		if m := f.read(); m.typ == typ {
			return m
		}
	}
	f.t.Fatalf("không thấy tin loại %d", typ)
	return snapRecv{}
}

func settingsOf(t *testing.T, m snapRecv) (s struct {
	BufferMs, Latency, Volume int
	Muted                     bool
}) {
	t.Helper()
	if m.typ != 3 || json.Unmarshal(m.payload[4:], &s) != nil || int(binary.LittleEndian.Uint32(m.payload)) != len(m.payload)-4 {
		t.Fatalf("Server Settings sai: type=%d %q", m.typ, m.payload)
	}
	return s
}

func startSnap(t *testing.T, vol map[string]int) (*snapServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := newSnapServer(func(id string) (int, bool) {
		if v, ok := vol[id]; ok {
			return v, v == 0
		}
		return 100, false
	})
	go s.serve(ln)
	t.Cleanup(func() { ln.Close() })
	return s, ln.Addr().String()
}

// Bắt tay đúng thứ tự giao thức: Hello → Server Settings → Codec Header (PCM 44,1 kHz, 16 bit, stereo) → Wire Chunk.
func TestSnapHandshakeAndChunks(t *testing.T) {
	s, addr := startSnap(t, map[string]int{"aa:bb": 35})
	f := dialSnap(t, addr, "aa:bb")
	first := f.read()
	if st := settingsOf(t, first); st.Volume != 35 || st.Muted || st.BufferMs < 500 {
		t.Fatalf("âm lượng đã lưu của đầu nhận phải gửi ngay khi nối: %+v", st)
	}
	// snapclient gửi Hello như một yêu cầu và chờ 2 giây tin trả lời có refersTo = id của Hello, không thì ngắt nối lại
	if first.refersTo != 1 {
		t.Fatalf("Server Settings đầu tiên phải trả lời Hello (refersTo=1), được %d", first.refersTo)
	}
	ch := f.read()
	le := binary.LittleEndian
	if ch.typ != 1 {
		t.Fatalf("sau Server Settings phải là Codec Header, được type=%d", ch.typ)
	}
	n := le.Uint32(ch.payload)
	codec, rest := string(ch.payload[4:4+n]), ch.payload[4+n:]
	wav := rest[4:]
	if codec != "pcm" || int(le.Uint32(rest)) != len(wav) || len(wav) != 44 || string(wav[0:4]) != "RIFF" || string(wav[8:16]) != "WAVEfmt " ||
		le.Uint16(wav[20:]) != 1 || le.Uint16(wav[22:]) != 2 || le.Uint32(wav[24:]) != 44100 || le.Uint32(wav[28:]) != 44100*4 ||
		le.Uint16(wav[32:]) != 4 || le.Uint16(wav[34:]) != 16 || string(wav[36:40]) != "data" {
		t.Fatalf("Codec Header PCM sai: codec=%q wav=% x", codec, wav)
	}
	for !s.has("aa:bb") {
		time.Sleep(5 * time.Millisecond)
	}
	pcm := bytes.Repeat([]byte{1, 2, 3, 4}, snapFrames)
	at := s.now()
	s.write(pcm, at)
	wc := f.readType(2)
	ts := int64(int32(le.Uint32(wc.payload)))*1e6 + int64(int32(le.Uint32(wc.payload[4:])))
	if ts != at.Microseconds() || int(le.Uint32(wc.payload[8:])) != len(pcm) || !bytes.Equal(wc.payload[12:], pcm) {
		t.Fatalf("Wire Chunk sai: ts=%d muốn %d, %d byte", ts, at.Microseconds(), len(wc.payload))
	}
	if cs := s.clients(); len(cs) != 1 || cs[0].ID != "aa:bb" || cs[0].Name != "loa-aa:bb" {
		t.Fatalf("danh sách đầu nhận: %+v", cs)
	}
}

// Đồng bộ đồng hồ: trả Time với refersTo = id yêu cầu, latency = lúc máy chủ nhận − lúc đầu nhận gửi (theo snapserver).
func TestSnapTimeSync(t *testing.T) {
	_, addr := startSnap(t, nil)
	f := dialSnap(t, addr, "cc:dd")
	f.read()
	f.read()
	f.send(4, 77, 0, 1_500_000, make([]byte, 8))
	m := f.readType(4)
	le := binary.LittleEndian
	lat := int64(int32(le.Uint32(m.payload)))*1e6 + int64(int32(le.Uint32(m.payload[4:])))
	if m.refersTo != 77 || m.recv <= 0 || m.sent < m.recv || lat != m.recv-1_500_000 {
		t.Fatalf("Time sai: refersTo=%d recv=%d sent=%d latency=%d", m.refersTo, m.recv, m.sent, lat)
	}
}

// Bật/tắt, chỉnh âm lượng một khu: chỉ đầu nhận đó nhận Server Settings mới. Đầu nhận ngừng đọc không làm treo các khu khác.
func TestSnapVolumeAndSlowClient(t *testing.T) {
	s, addr := startSnap(t, nil)
	a, b := dialSnap(t, addr, "a"), dialSnap(t, addr, "b")
	for _, f := range []*fakeSnapClient{a, b} {
		f.read()
		f.read()
	}
	for !s.has("a") || !s.has("b") {
		time.Sleep(5 * time.Millisecond)
	}
	s.settings("b", 20, true)
	if st := settingsOf(t, b.readType(3)); st.Volume != 20 || !st.Muted {
		t.Fatalf("khu b: %+v", st)
	}
	// a không đọc nữa: ghi vài giây nhạc vẫn không bị chặn
	done := make(chan bool)
	go func() {
		for i := 0; i < 2000; i++ {
			s.write(make([]byte, snapFrames*4), s.now())
		}
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("một đầu nhận ngừng đọc làm treo việc phát")
	}
}
