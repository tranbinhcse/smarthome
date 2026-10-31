package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Xem lại: quét file fMP4 thành các GOP, dựng playlist HLS byte range và đoạn đã ghi theo ngày.

type recGOP struct {
	Off, Len int64
	Dur      float64 // giây
}

// recScan đọc đầu box của file, không đọc mdat. init = ftyp+moov; mỗi GOP là các cặp moof+mdat liền nhau từ một
// keyframe hình tới trước keyframe kế. Box cuối cụt thì bỏ, không báo lỗi.
func recScan(path string) (initLen int64, gops []recGOP, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, nil, err
	}
	size := st.Size()
	hdr := func(pos int64) (typ string, n int64, ok bool) {
		var h [16]byte
		if k, _ := f.ReadAt(h[:], pos); k < 8 {
			return "", 0, false
		}
		n = int64(binary.BigEndian.Uint32(h[:]))
		if n == 1 {
			n = int64(binary.BigEndian.Uint64(h[8:]))
		}
		return string(h[4:8]), n, n >= 8 && pos+n <= size
	}
	var (
		video      uint32
		scale      float64 = 1
		trex, tdur         = map[uint32]uint32{}, map[uint32]uint32{}
	)
	for pos := int64(0); ; {
		typ, n, ok := hdr(pos)
		if !ok {
			return initLen, gops, nil
		}
		switch typ {
		case "ftyp":
			initLen = pos + n
		case "moov":
			b := make([]byte, n)
			if _, err := f.ReadAt(b, pos); err != nil {
				return initLen, gops, nil
			}
			video, trex = rewriteMoov(b)
			scale, tdur = moovInfo(b, video)
			initLen = pos + n
		case "moof":
			t2, n2, ok2 := hdr(pos + n)
			if !ok2 || t2 != "mdat" {
				return initLen, gops, nil // cặp cuối chưa đủ
			}
			b := make([]byte, n)
			if _, err := f.ReadAt(b, pos); err != nil {
				return initLen, gops, nil
			}
			ts, err := parseMoof(b, trex)
			if err != nil {
				return initLen, gops, nil // moof hỏng: giữ phần đã quét
			}
			for _, t := range ts {
				if t.track != video || video == 0 {
					continue
				}
				if t.sync {
					gops = append(gops, recGOP{Off: pos})
				}
				if len(gops) > 0 {
					g := &gops[len(gops)-1]
					g.Len = pos + n + n2 - g.Off
					g.Dur += float64(moofDur(b, video, tdur[video])) / scale
				}
			}
			n += n2
		}
		pos += n
	}
}

// moovInfo: timescale mdhd và default_sample_duration (trex) của track hình.
func moovInfo(b []byte, video uint32) (scale float64, dur map[uint32]uint32) {
	scale, dur = 1, map[uint32]uint32{}
	eachBox(b, 0, len(b), func(moov box) {
		if mvex, ok := descend(b, moov, "mvex"); ok {
			eachBox(b, mvex.body, mvex.end, func(x box) {
				if x.typ == "trex" && x.end-x.body >= 24 {
					dur[binary.BigEndian.Uint32(b[x.body+4:])] = binary.BigEndian.Uint32(b[x.body+12:])
				}
			})
		}
		eachBox(b, moov.body, moov.end, func(trak box) {
			tkhd, ok := descend(b, trak, "tkhd")
			mdhd, ok2 := descend(b, trak, "mdia", "mdhd")
			if trak.typ != "trak" || !ok || !ok2 || mdhd.end-mdhd.body < 16 {
				return
			}
			off := tkhd.body + 12
			if tkhd.end > tkhd.body && b[tkhd.body] == 1 {
				off += 8
			}
			if off+4 > tkhd.end || binary.BigEndian.Uint32(b[off:]) != video {
				return
			}
			o := mdhd.body + 12
			if b[mdhd.body] == 1 {
				o = mdhd.body + 20
			}
			if o+4 <= mdhd.end {
				if ts := binary.BigEndian.Uint32(b[o:]); ts > 0 {
					scale = float64(ts)
				}
			}
		})
	})
	return
}

// moofDur: tổng thời lượng mẫu của track video trong một moof (đơn vị timescale).
func moofDur(b []byte, video, trexDur uint32) (total uint64) {
	be := binary.BigEndian
	eachBox(b, 0, len(b), func(moof box) {
		eachBox(b, moof.body, moof.end, func(t box) {
			if t.typ != "traf" {
				return
			}
			def, mine := trexDur, false
			eachBox(b, t.body, t.end, func(c box) {
				p := b[c.body:c.end]
				switch c.typ {
				case "tfhd":
					if len(p) < 8 {
						return
					}
					mine = be.Uint32(p[4:]) == video
					fl, o := be.Uint32(p)&0xffffff, 8
					if fl&1 != 0 {
						o += 8
					}
					if fl&2 != 0 {
						o += 4
					}
					if fl&8 != 0 && len(p) >= o+4 {
						def = be.Uint32(p[o:])
					}
				case "trun":
					if !mine || len(p) < 8 {
						return
					}
					fl, n, o := be.Uint32(p)&0xffffff, int(be.Uint32(p[4:])), 8
					if fl&1 != 0 {
						o += 4
					}
					if fl&4 != 0 {
						o += 4
					}
					if fl&0x100 == 0 {
						total += uint64(def) * uint64(n)
						return
					}
					step := 4
					for _, m := range []uint32{0x200, 0x400, 0x800} {
						if fl&m != 0 {
							step += 4
						}
					}
					for i := 0; i < n && o+4 <= len(p); i, o = i+1, o+step {
						total += uint64(be.Uint32(p[o:]))
					}
				}
			})
		})
	})
	return
}

const recMaxSpan = 24 * time.Hour

func mountRecPlay(mux *http.ServeMux, db *DB, a *Auth) {
	mux.HandleFunc("GET /api/cam/{id}/rec", a.Require(func(w http.ResponseWriter, r *http.Request) {
		day, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("day"), tgICT)
		if err != nil {
			http.Error(w, "ngày không hợp lệ", 400)
			return
		}
		end := day.AddDate(0, 0, 1)
		files, err := recRange(db, r.PathValue("id"), day, end)
		if err != nil {
			http.Error(w, "không đọc được chỉ mục ghi hình", 500)
			return
		}
		spans := [][2]int64{}
		for _, f := range files {
			s, e := max(f.Start.UnixMilli(), day.UnixMilli()), min(f.End.UnixMilli(), end.UnixMilli())
			if e <= s {
				continue
			}
			if n := len(spans); n > 0 && s-spans[n-1][1] <= 10_000 {
				spans[n-1][1] = max(spans[n-1][1], e)
			} else {
				spans = append(spans, [2]int64{s, e})
			}
		}
		writeJSON(w, map[string]any{"spans": spans})
	}))
	mux.HandleFunc("GET /api/cam/{id}/rec.m3u8", a.Require(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, e1 := strconv.ParseInt(q.Get("from"), 10, 64)
		to, e2 := strconv.ParseInt(q.Get("to"), 10, 64)
		if e1 != nil || e2 != nil || from < 0 || to <= from || to-from > recMaxSpan.Milliseconds() {
			http.Error(w, "khoảng thời gian không hợp lệ", 400)
			return
		}
		files, err := recRange(db, r.PathValue("id"), time.UnixMilli(from), time.UnixMilli(to))
		if err != nil {
			http.Error(w, "không đọc được chỉ mục ghi hình", 500)
			return
		}
		var sb strings.Builder
		longest, n := 0.0, 0
		for _, f := range files {
			initLen, gops, err := recScan(f.Path) // file mất hay hỏng thì bỏ qua
			if err != nil {
				continue
			}
			t, first := f.Start.UnixMilli(), true
			for _, g := range gops {
				gs, ge := t, t+int64(g.Dur*1000)
				t = ge
				if ge <= from || gs > to {
					continue
				}
				if first {
					if n > 0 {
						sb.WriteString("#EXT-X-DISCONTINUITY\n")
					}
					fmt.Fprintf(&sb, "#EXT-X-MAP:URI=\"rec/%d.mp4\",BYTERANGE=\"%d@0\"\n", f.ID, initLen)
					first = false
				}
				longest = math.Max(longest, g.Dur)
				fmt.Fprintf(&sb, "#EXTINF:%.3f,\n#EXT-X-BYTERANGE:%d@%d\nrec/%d.mp4\n", g.Dur, g.Len, g.Off, f.ID)
				n++
			}
		}
		if n == 0 {
			http.Error(w, "không có hình trong khoảng này", 404)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n%s#EXT-X-ENDLIST\n", int(math.Ceil(longest)), sb.String())
	}))
	// {file} là "<id>.mp4" (ServeMux Go 1.22 không cho "{file}.mp4"); đường dẫn đĩa chỉ lấy từ chỉ mục
	mux.HandleFunc("GET /api/cam/{id}/rec/{file}", a.Require(func(w http.ResponseWriter, r *http.Request) {
		name, ok := strings.CutSuffix(r.PathValue("file"), ".mp4")
		id, err := strconv.ParseInt(name, 10, 64)
		if !ok || err != nil {
			http.NotFound(w, r)
			return
		}
		fs, err := recQuery(db, `WHERE id = ? AND cam = ?`, id, r.PathValue("id"))
		if err != nil || len(fs) != 1 {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(fs[0].Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "", fs[0].End, f)
	}))
}
