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

// rewriteMoov đổi hev1 thành hvc1 tại chỗ trong track hình, trả track_ID của track đó (0 nếu không có).
func rewriteMoov(b []byte) (video uint32) {
	eachBox(b, 0, len(b), func(moov box) {
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
	return video
}

type traf struct {
	track uint32
	sync  bool // mẫu đầu là khung đồng bộ (bit 0x00010000 của cờ mẫu tắt)
	tfdt  int  // vị trí thân tfdt trong moof, -1 nếu không có
	v1    bool // tfdt 64 bit
}

// parseMoof đọc từng traf của một moof. Cờ mẫu đầu lấy theo thứ tự: first_sample_flags, cờ mẫu đầu, default_sample_flags.
func parseMoof(b []byte) (out []traf) {
	be := binary.BigEndian
	eachBox(b, 0, len(b), func(moof box) {
		eachBox(b, moof.body, moof.end, func(t box) {
			if t.typ != "traf" {
				return
			}
			tr := traf{tfdt: -1}
			var def, flags uint32
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
						def = be.Uint32(p[o:])
					}
				case "tfdt":
					tr.tfdt, tr.v1 = c.body, len(p) > 0 && p[0] == 1
				case "trun":
					if sawTrun || len(p) < 8 {
						return
					}
					sawTrun = true
					fl, o := be.Uint32(p)&0xffffff, 8
					flags = def
					if fl&1 != 0 {
						o += 4
					}
					if fl&4 != 0 && len(p) >= o+4 {
						flags = be.Uint32(p[o:])
					} else if fl&0x400 != 0 {
						if fl&0x100 != 0 {
							o += 4
						}
						if fl&0x200 != 0 {
							o += 4
						}
						if len(p) >= o+4 {
							flags = be.Uint32(p[o:])
						}
					}
				}
			})
			tr.sync = flags&0x00010000 == 0
			out = append(out, tr)
		})
	})
	return out
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

type tfdtRef struct {
	track uint32
	off   int // vị trí thân tfdt trong buf
	v1    bool
}

type recWriter struct {
	max    int64
	create func(time.Time) (io.WriteCloser, string, error)
	now    func() time.Time
	done   func(recSeg)

	head  []byte // ftyp+moov (đã sửa hvc1)
	video uint32

	w     io.WriteCloser // file đang mở
	seg   recSeg
	data  bool              // file đã có GOP
	base  map[uint32]uint64 // tfdt đầu tiên của từng track trong file
	buf   []byte            // GOP đang đệm
	refs  []tfdtRef
	gop   time.Time // lúc nhận keyframe mở GOP
	in    bool      // đang trong một GOP
	spill bool      // GOP này đã ghi bớt vì vượt recGopCap
	err   error     // lỗi ghi: dừng hẳn
}

func (s *recWriter) pair(moof, mdat []byte) error {
	ts := parseMoof(moof)
	for _, t := range ts {
		if s.video != 0 && t.track == s.video && t.sync {
			t0 := s.now()
			if err := s.endGOP(t0); err != nil {
				return err
			}
			s.in, s.gop = true, t0
			break
		}
	}
	if !s.in {
		return nil // trước keyframe đầu tiên
	}
	for _, t := range ts {
		if t.tfdt >= 0 {
			s.refs = append(s.refs, tfdtRef{t.track, len(s.buf) + t.tfdt, t.v1})
		}
	}
	s.buf = append(append(s.buf, moof...), mdat...)
	if len(s.buf) > recGopCap {
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
		p := s.buf[r.off+4:]
		var v uint64
		if r.v1 {
			v = binary.BigEndian.Uint64(p)
		} else {
			v = uint64(binary.BigEndian.Uint32(p))
		}
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
// ponytail: file vượt maxBytes chỉ khi một GOP lớn hơn recGopCap; muốn chặt hơn thì cắt GOP ở ranh giới khung (mất khả năng phát độc lập).
func recSplit(r io.Reader, maxBytes int64, create func(start time.Time) (io.WriteCloser, string, error), now func() time.Time, done func(recSeg)) error {
	s := &recWriter{max: maxBytes, create: create, now: now, done: done}
	err := s.run(r)
	if s.err != nil {
		return s.err
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
			s.video = rewriteMoov(b)
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
