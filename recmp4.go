package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

// Cắt luồng MP4 phân mảnh của go2rtc (ftyp, moov, rồi từng cặp moof+mdat) thành nhiều file, mỗi file mở đầu ở keyframe
// và không quá recMaxBytes. Không chuyển mã: chỉ sửa đầu box (hev1 thành hvc1 cho Safari, tfdt đếm lại từ 0).
const (
	recMaxBytes = 50_000_000
	recGopCap   = 16 << 20  // đệm tối đa cho một GOP; vượt thì ghi luôn vào file hiện tại
	recMaxBox   = 256 << 20 // box lớn hơn mức này coi là luồng hỏng
)

type recSeg struct {
	Path       string
	Start, End time.Time
	Bytes      int64
}

type box struct {
	typ            string
	hdr, body, end int // vị trí trong slice gốc: đầu box, đầu phần thân, cuối box
}

// eachBox duyệt các box liền nhau trong b[from:to]; gặp box hỏng thì dừng.
func eachBox(b []byte, from, to int, f func(box)) {
	for from+8 <= to {
		size, hl := int(binary.BigEndian.Uint32(b[from:])), 8
		if size == 1 && from+16 <= to {
			size, hl = int(binary.BigEndian.Uint64(b[from+8:])), 16
		} else if size == 0 {
			size = to - from
		}
		if size < hl || size > to-from {
			return
		}
		f(box{string(b[from+4 : from+8]), from, from + hl, from + size})
		from += size
	}
}

func descend(b []byte, p box, names ...string) (box, bool) {
	for _, n := range names {
		var next box
		found := false
		eachBox(b, p.body, p.end, func(c box) {
			if !found && c.typ == n {
				next, found = c, true
			}
		})
		if !found {
			return box{}, false
		}
		p = next
	}
	return p, true
}

// rewriteMoov đổi hev1 thành hvc1 tại chỗ trong track hình, trả track_ID của track đó (0 nếu không có)
// và default_sample_flags của mvex/trex theo từng track (cờ dự phòng cuối cùng cho khung không khai cờ).
func rewriteMoov(b []byte) (video uint32, trex map[uint32]uint32) {
	trex = map[uint32]uint32{}
	eachBox(b, 0, len(b), func(moov box) {
		if mvex, ok := descend(b, moov, "mvex"); ok {
			eachBox(b, mvex.body, mvex.end, func(x box) {
				if x.typ == "trex" && x.end-x.body >= 24 {
					trex[binary.BigEndian.Uint32(b[x.body+4:])] = binary.BigEndian.Uint32(b[x.body+20:])
				}
			})
		}
		eachBox(b, moov.body, moov.end, func(trak box) {
			if trak.typ != "trak" || video != 0 {
				return
			}
			hdlr, ok := descend(b, trak, "mdia", "hdlr")
			if !ok || hdlr.end-hdlr.body < 12 || string(b[hdlr.body+8:hdlr.body+12]) != "vide" {
				return
			}
			tkhd, ok := descend(b, trak, "tkhd")
			if !ok {
				return
			}
			off := tkhd.body + 12
			if tkhd.end > tkhd.body && b[tkhd.body] == 1 {
				off += 8
			}
			if off+4 > tkhd.end {
				return
			}
			if stsd, ok := descend(b, trak, "mdia", "minf", "stbl", "stsd"); ok && stsd.end-stsd.body >= 8 {
				eachBox(b, stsd.body+8, stsd.end, func(e box) {
					if e.typ == "hev1" {
						copy(b[e.hdr+4:], "hvc1")
					}
				})
			}
			video = binary.BigEndian.Uint32(b[off:])
		})
	})
	return video, trex
}

type traf struct {
	track uint32
	sync  bool // mẫu đầu là khung đồng bộ (bit 0x00010000 của cờ mẫu tắt)
	tfdt  int  // vị trí thân tfdt trong moof, -1 nếu không có
	v1    bool // tfdt 64 bit
}

// parseMoof đọc từng traf của một moof. Cờ mẫu đầu lấy theo thứ tự: first_sample_flags, cờ mẫu đầu,
// default_sample_flags của tfhd, rồi trex; không có cờ nào thì coi là khung không đồng bộ. tfdt hỏng trả lỗi.
func parseMoof(b []byte, trex map[uint32]uint32) (out []traf, err error) {
	be := binary.BigEndian
	eachBox(b, 0, len(b), func(moof box) {
		eachBox(b, moof.body, moof.end, func(t box) {
			if t.typ != "traf" {
				return
			}
			tr := traf{tfdt: -1}
			var def, flags uint32
			var haveDef, have bool
			sawTrun := false
			eachBox(b, t.body, t.end, func(c box) {
				p := b[c.body:c.end]
				switch c.typ {
				case "tfhd":
					if len(p) < 8 {
						return
					}
					fl, o := be.Uint32(p)&0xffffff, 8
					tr.track = be.Uint32(p[4:])
					for _, bit := range []struct {
						m uint32
						n int
					}{{1, 8}, {2, 4}, {8, 4}, {0x10, 4}} {
						if fl&bit.m != 0 {
							o += bit.n
						}
					}
					if fl&0x20 != 0 && len(p) >= o+4 {
						def, haveDef = be.Uint32(p[o:]), true
					}
				case "tfdt":
					tr.tfdt, tr.v1 = c.body, len(p) > 0 && p[0] == 1
					if len(p) < 8 || tr.v1 && len(p) < 12 {
						err = errors.New("rec: tfdt hỏng")
					}
				case "trun":
					if sawTrun || len(p) < 8 {
						return
					}
					sawTrun = true
					fl, o := be.Uint32(p)&0xffffff, 8
					if be.Uint32(p[4:]) == 0 {
						return
					}
					if fl&1 != 0 {
						o += 4
					}
					if fl&4 != 0 {
						if len(p) >= o+4 {
							flags, have = be.Uint32(p[o:]), true
						}
					} else if fl&0x400 != 0 {
						if fl&0x100 != 0 {
							o += 4
						}
						if fl&0x200 != 0 {
							o += 4
						}
						if len(p) >= o+4 {
							flags, have = be.Uint32(p[o:]), true
						}
					}
				}
			})
			if !have && haveDef {
				flags, have = def, true
			}
			if v, ok := trex[tr.track]; !have && ok {
				flags, have = v, true
			}
			tr.sync = have && flags&0x00010000 == 0
			out = append(out, tr)
		})
	})
	return out, err
}

func readTfdt(b []byte, t traf) uint64 {
	if t.v1 {
		return binary.BigEndian.Uint64(b[t.tfdt+4:])
	}
	return uint64(binary.BigEndian.Uint32(b[t.tfdt+4:]))
}

// readBox đọc nguyên một box cấp ngoài (cả đầu). Hết sạch ở ranh giới box trả io.EOF, hết giữa box trả io.ErrUnexpectedEOF.
func readBox(r io.Reader) (string, []byte, error) {
	var h [16]byte
	if _, err := io.ReadFull(r, h[:8]); err != nil {
		return "", nil, err
	}
	size, hl := int64(binary.BigEndian.Uint32(h[:])), 8
	if size == 1 {
		if _, err := io.ReadFull(r, h[8:]); err != nil {
			return "", nil, io.ErrUnexpectedEOF
		}
		size, hl = int64(binary.BigEndian.Uint64(h[8:])), 16
	}
	if size < int64(hl) || size > recMaxBox {
		return "", nil, errors.New("rec: box sai kích thước")
	}
	var buf bytes.Buffer
	buf.Grow(int(min(size, 1<<20)))
	buf.Write(h[:hl])
	if _, err := io.CopyN(&buf, r, size-int64(hl)); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return "", nil, err
	}
	return string(h[4:8]), buf.Bytes(), nil
}

type recWriter struct {
	max    int64
	create func(time.Time) (io.WriteCloser, string, error)
	now    func() time.Time
	done   func(recSeg)

	head  []byte // ftyp+moov (đã sửa hvc1)
	video uint32
	trex  map[uint32]uint32
	seen  bool // đã gặp keyframe hình

	w     io.WriteCloser // file đang mở
	seg   recSeg
	data  bool              // file đã có GOP
	base  map[uint32]uint64 // tfdt đầu tiên của từng track trong file
	buf   []byte            // GOP đang đệm
	refs  []traf            // traf có tfdt, vị trí tfdt tính trong buf
	gop   time.Time         // lúc nhận keyframe mở GOP
	in    bool              // đang trong một GOP
	spill bool              // GOP này đã ghi bớt vì vượt recGopCap
	err   error             // lỗi ghi: dừng hẳn
}

func (s *recWriter) pair(moof, mdat []byte) error {
	ts, err := parseMoof(moof, s.trex)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if s.video != 0 && t.track == s.video && t.sync {
			t0 := s.now()
			if err := s.endGOP(t0); err != nil {
				return err
			}
			s.in, s.gop, s.seen = true, t0, true
			break
		}
	}
	if !s.in {
		return nil // trước keyframe đầu tiên
	}
	for _, t := range ts {
		if t.tfdt >= 0 {
			t.tfdt += len(s.buf)
			s.refs = append(s.refs, t)
		}
	}
	s.buf = append(append(s.buf, moof...), mdat...)
	if len(s.buf) > recGopCap {
		// lần ghi bớt đầu tiên: file đã có dữ liệu mà không chứa nổi thì đóng file, GOP sang file mới
		if !s.spill && s.w != nil && s.data && s.seg.Bytes+int64(len(s.buf)) > s.max {
			if err := s.closeFile(s.now()); err != nil {
				return err
			}
		}
		s.spill = true
		return s.write()
	}
	return nil
}

// endGOP chốt GOP đang đệm tại thời điểm t: file đầy thì đóng rồi mở file mới, sau đó ghi GOP.
func (s *recWriter) endGOP(t time.Time) error {
	if !s.in {
		return nil
	}
	s.in = false
	if len(s.buf) > 0 {
		if !s.spill && s.w != nil && s.data && s.seg.Bytes+int64(len(s.buf)) > s.max {
			if err := s.closeFile(t); err != nil {
				return err
			}
		}
		if err := s.write(); err != nil {
			return err
		}
	}
	s.spill = false
	return nil
}

func (s *recWriter) write() (err error) {
	defer func() {
		if err != nil {
			s.err = err
			if s.w != nil {
				s.w.Close()
				s.w = nil
			}
		}
	}()
	if s.w == nil {
		w, path, err := s.create(s.gop)
		if err != nil {
			return err
		}
		s.w, s.base, s.data = w, map[uint32]uint64{}, false
		s.seg = recSeg{Path: path, Start: s.gop}
		n, err := w.Write(s.head)
		s.seg.Bytes += int64(n)
		if err != nil {
			return err
		}
	}
	for _, r := range s.refs {
		p := s.buf[r.tfdt+4:]
		v := readTfdt(s.buf, r)
		b, ok := s.base[r.track]
		if !ok {
			b = v
			s.base[r.track] = v
		}
		v -= min(v, b)
		if r.v1 {
			binary.BigEndian.PutUint64(p, v)
		} else {
			binary.BigEndian.PutUint32(p, uint32(v))
		}
	}
	n, err := s.w.Write(s.buf)
	s.seg.Bytes += int64(n)
	s.buf, s.refs, s.data = s.buf[:0], s.refs[:0], true
	return err
}

func (s *recWriter) closeFile(end time.Time) error {
	if s.w == nil {
		return nil
	}
	w := s.w
	s.w = nil
	s.seg.End = end
	if err := w.Close(); err != nil {
		s.err = err
		return err
	}
	s.done(s.seg)
	return nil
}

// recSplit đọc luồng MP4 phân mảnh từ r, cắt ở keyframe thành các file dưới maxBytes (create mở file mới, trả
// đường dẫn để báo trong recSeg). Luồng hết hay lỗi đọc thì ghi nốt phần GOP đầy đủ, đóng file, gọi done.
// Trả nil khi hết sạch ở ranh giới box, io.ErrUnexpectedEOF khi cụt giữa box.
// Lỗi ghi hay Close: file đó không gọi done (recorder dò lại file mồ côi lúc khởi động); luồng không có keyframe hình trả lỗi.
// ponytail: file chỉ vượt maxBytes khi một GOP lớn hơn chỗ còn lại sau lần ghi bớt đầu tiên (mở file mới nếu file hiện tại không chứa nổi);
// muốn chặt hơn thì cắt GOP ở ranh giới khung (mất khả năng phát độc lập).
func recSplit(r io.Reader, maxBytes int64, create func(start time.Time) (io.WriteCloser, string, error), now func() time.Time, done func(recSeg)) error {
	s := &recWriter{max: maxBytes, create: create, now: now, done: done}
	err := s.run(r)
	if s.err != nil {
		return s.err
	}
	if !s.seen && err == nil {
		err = errors.New("rec: luồng không có keyframe hình")
	}
	t := now()
	if e := s.endGOP(t); e != nil {
		return e
	}
	if e := s.closeFile(t); e != nil {
		return e
	}
	return err
}

func (s *recWriter) run(r io.Reader) error {
	var moof []byte
	for {
		typ, b, err := readBox(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch typ {
		case "ftyp":
			s.head = append(s.head, b...)
		case "moov":
			s.video, s.trex = rewriteMoov(b)
			s.head = append(s.head, b...)
		case "moof":
			moof = b
		case "mdat":
			if moof != nil {
				err := s.pair(moof, b)
				moof = nil
				if err != nil {
					return err
				}
			}
		}
	}
}
