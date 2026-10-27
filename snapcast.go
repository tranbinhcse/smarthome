package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Máy chủ giao thức Snapcast (mục 18.3 thiết kế; doc/binary_protocol.md của snapcast): hubd tự phát nhạc tới các
// đầu nhận cạnh amply (snapclient trên ESP32 / Pi, cắm AUX). Mọi đầu nhận nhận cùng một luồng PCM có dấu thời gian
// theo đồng hồ của hubd; đầu nhận tự đồng bộ đồng hồ (tin Time) rồi phát đúng lúc nên các phòng nghe khớp nhau.
// Bật/tắt, âm lượng từng khu = Server Settings gửi riêng cho đầu nhận đó (snapclient chỉnh âm lượng phần mềm).
const (
	snapRate   = 44100
	snapFrames = snapRate / 50 // 20 ms một đoạn
	snapBuffer = 1000          // ms: đầu nhận phát đoạn nhạc sau dấu thời gian chừng này, bù Wi-Fi chập chờn
)

type snapClient struct {
	ID, Name  string
	Connected bool `json:"connected"`
}

type snapMsg struct {
	typ, refersTo uint16
	recv          time.Duration // lúc máy chủ nhận yêu cầu (tin Time)
	payload       []byte
}

type snapConn struct {
	id, name string
	out      chan snapMsg // ghi ở goroutine riêng: đầu nhận chậm không làm treo các khu khác
	n        uint16
}

type snapServer struct {
	start time.Time
	vol   func(id string) (int, bool) // âm lượng, tắt tiếng đã lưu của từng đầu nhận
	mu    sync.Mutex
	conns map[*snapConn]bool
}

func newSnapServer(vol func(id string) (int, bool)) *snapServer {
	return &snapServer{start: time.Now(), vol: vol, conns: map[*snapConn]bool{}}
}

// now: đồng hồ của máy chủ (đơn điệu, tính từ lúc chạy), dùng chung cho dấu thời gian đoạn nhạc và tin Time.
func (s *snapServer) now() time.Duration { return time.Since(s.start) }

func (s *snapServer) serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.session(c)
	}
}

var le = binary.LittleEndian

func sizedJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return append(le.AppendUint32(nil, uint32(len(b))), b...)
}

// Tiêu đề WAV 44 byte cho Codec Header "pcm": 16 bit, 2 kênh, snapRate Hz.
func pcmHeader() []byte {
	h := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00")
	h = le.AppendUint16(h, 1)
	h = le.AppendUint16(h, 2)
	h = le.AppendUint32(h, snapRate)
	h = le.AppendUint32(h, snapRate*4)
	h = le.AppendUint16(h, 4)
	h = le.AppendUint16(h, 16)
	return append(h, "data\x00\x00\x00\x00"...)
}

func (s *snapServer) session(c net.Conn) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, helloID, _, body, err := readSnap(c)
	var hello struct {
		ID, HostName string
		Instance     int
	}
	if err != nil || typ != 5 || len(body) < 4 || json.Unmarshal(body[4:], &hello) != nil || hello.ID == "" {
		return
	}
	id := hello.ID
	if hello.Instance > 1 {
		id += "#" + strconv.Itoa(hello.Instance)
	}
	sc := &snapConn{id: id, name: hello.HostName, out: make(chan snapMsg, 256)}
	vol, muted := s.vol(id)
	// trả lời Hello: snapclient chờ tin có refersTo = id của Hello, quá 2 giây thì ngắt rồi nối lại
	sc.out <- snapMsg{typ: 3, refersTo: helloID, payload: settingsPayload(vol, muted)}
	codec := append(le.AppendUint32(nil, 3), "pcm"...)
	sc.out <- snapMsg{typ: 1, payload: append(codec, append(le.AppendUint32(nil, 44), pcmHeader()...)...)}
	s.mu.Lock()
	s.conns[sc] = true
	s.mu.Unlock()
	log.Printf("nhạc: đầu nhận %s (%s) đã nối", id, hello.HostName)
	defer func() {
		s.mu.Lock()
		delete(s.conns, sc)
		s.mu.Unlock()
		close(sc.out)
		log.Printf("nhạc: đầu nhận %s ngắt", id)
	}()
	go s.writer(c, sc)
	for {
		c.SetReadDeadline(time.Now().Add(time.Minute)) // snapclient hỏi Time mỗi giây; im lâu = mất kết nối
		typ, mid, sent, _, err := readSnap(c)          // Client Info (âm lượng chỉnh trên đầu nhận): hubd giữ âm lượng, bỏ qua
		if err != nil {
			return
		}
		if typ == 4 { // Time: latency = lúc máy chủ nhận − lúc đầu nhận gửi; đầu nhận tự suy ra độ lệch đồng hồ
			recv := s.now().Truncate(time.Microsecond) // giao thức tính bằng micro giây nguyên
			lat := recv - sent
			p := le.AppendUint32(nil, uint32(int32(lat/time.Second)))
			sc.push(snapMsg{typ: 4, refersTo: mid, recv: recv, payload: le.AppendUint32(p, uint32(int32(lat%time.Second/time.Microsecond)))})
		}
	}
}

func settingsPayload(vol int, muted bool) []byte {
	return sizedJSON(map[string]any{"bufferMs": snapBuffer, "latency": 0, "muted": muted, "volume": vol})
}

// writer: dựng tiêu đề và đặt giờ gửi ngay trước khi ghi, để tin Time không bị lệch bởi thời gian chờ trong hàng.
func (s *snapServer) writer(c net.Conn, sc *snapConn) {
	for m := range sc.out {
		sc.n++
		h := le.AppendUint16(nil, m.typ)
		h = le.AppendUint16(h, sc.n)
		h = le.AppendUint16(h, m.refersTo)
		h = appendTV(h, s.now())
		h = appendTV(h, m.recv)
		h = le.AppendUint32(h, uint32(len(m.payload)))
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Write(append(h, m.payload...)); err != nil {
			c.Close() // vòng đọc thoát, dọn kết nối
			for range sc.out {
			}
			return
		}
	}
}

func appendTV(b []byte, d time.Duration) []byte {
	b = le.AppendUint32(b, uint32(int32(d/time.Second)))
	return le.AppendUint32(b, uint32(int32(d%time.Second/time.Microsecond)))
}

// push không chờ: hàng của đầu nhận đầy (mạng kẹt quá 5 giây nhạc) thì bỏ tin, đầu nhận tự bắt kịp ở đoạn sau.
func (sc *snapConn) push(m snapMsg) {
	select {
	case sc.out <- m:
	default:
	}
}

// write: một đoạn PCM (16 bit, stereo, snapRate) có dấu thời gian `at` theo now(), gửi tới mọi đầu nhận.
func (s *snapServer) write(pcm []byte, at time.Duration) {
	p := appendTV(nil, at)
	p = append(le.AppendUint32(p, uint32(len(pcm))), pcm...)
	s.mu.Lock()
	defer s.mu.Unlock()
	for sc := range s.conns {
		sc.push(snapMsg{typ: 2, payload: p})
	}
}

// settings: bật/tắt, âm lượng của một đầu nhận (một khu).
func (s *snapServer) settings(id string, vol int, muted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sc := range s.conns {
		if sc.id == id {
			sc.push(snapMsg{typ: 3, payload: settingsPayload(vol, muted)})
		}
	}
}

func (s *snapServer) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sc := range s.conns {
		if sc.id == id {
			return true
		}
	}
	return false
}

func (s *snapServer) clients() []snapClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []snapClient{}
	for sc := range s.conns {
		out = append(out, snapClient{ID: sc.id, Name: sc.name, Connected: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// readSnap: một tin (tiêu đề 26 byte + thân). sent = giờ gửi ghi trong tiêu đề (đồng hồ của bên gửi).
func readSnap(r io.Reader) (typ, id uint16, sent time.Duration, body []byte, err error) {
	h := make([]byte, 26)
	if _, err = io.ReadFull(r, h); err != nil {
		return
	}
	n := le.Uint32(h[22:])
	if n > 1<<20 {
		return 0, 0, 0, nil, errors.New("tin Snapcast quá lớn")
	}
	body = make([]byte, n)
	_, err = io.ReadFull(r, body)
	sent = time.Duration(int32(le.Uint32(h[6:])))*time.Second + time.Duration(int32(le.Uint32(h[10:])))*time.Microsecond
	return le.Uint16(h[0:]), le.Uint16(h[2:]), sent, body, err
}
