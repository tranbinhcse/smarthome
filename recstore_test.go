package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func recT(h, m int) time.Time { return time.Date(2026, 10, 30, h, m, 0, 0, tgICT) }

func TestRecWantedScheduleOvernight(t *testing.T) {
	p := recPolicy{Mode: "schedule", Windows: []recWindow{{From: "22:00", To: "06:00"}}}
	cases := []struct {
		s, e time.Time
		want bool
	}{
		{recT(5, 58), recT(6, 1), true},
		{recT(6, 1), recT(6, 3), false},
		{recT(21, 59), recT(22, 0), true},
	}
	for _, c := range cases {
		if got := recWanted(p, recFile{Start: c.s, End: c.e}); got != c.want {
			t.Errorf("%v-%v: got %v want %v", c.s, c.e, got, c.want)
		}
	}
	f := recFile{Start: recT(12, 0), End: recT(12, 1)}
	if !recWanted(recPolicy{Mode: "always"}, f) || recWanted(recPolicy{Mode: "off"}, f) {
		t.Error("always/off sai")
	}
}

// recSet: các file 10 byte cách nhau 100 s, cam gán theo tham số; ID tăng dần.
func recMk(id int64, cam string, i int, held bool) recFile {
	b := recT(3, 0)
	return recFile{ID: id, Cam: cam, Start: b.Add(time.Duration(i) * 100 * time.Second),
		End: b.Add(time.Duration(i+1) * 100 * time.Second), Bytes: 10, Held: held}
}

func TestRecPlanRules(t *testing.T) {
	ok := recDisk{OK: true, Total: 1000, Free: 900}
	pa := map[string]recPolicy{"a": {Mode: "always"}}
	// 1: chế độ off -> xoá file thường cũ trước, giữ file mới nhất
	f := []recFile{recMk(1, "a", 0, false), recMk(2, "a", 1, true), recMk(3, "a", 2, false), recMk(4, "a", 3, false)}
	got := recPlan(f, map[string]recPolicy{"a": {Mode: "off"}}, ok, 80)
	if want := []int64{1, 3}; !reflect.DeepEqual(got.Delete, want) || got.Stop {
		t.Errorf("rule1: %+v", got)
	}
	// 2: hạn mức 20 byte của a, b không giới hạn
	f = []recFile{recMk(1, "a", 2, false), recMk(2, "a", 0, false), recMk(3, "a", 1, false), recMk(4, "b", 0, false)}
	got = recPlan(f, map[string]recPolicy{"a": {Mode: "always", LimitBytes: 20}}, ok, 80)
	if want := []int64{2}; !reflect.DeepEqual(got.Delete, want) {
		t.Errorf("rule2: %+v", got)
	}
	// 3: ổ 100 byte, 50% -> file thường tối đa 50; có 6 file 60 byte, xoá cũ nhất toàn ổ, giữ mới nhất mỗi cam
	f = []recFile{recMk(1, "a", 0, false), recMk(2, "b", 1, false), recMk(3, "a", 2, false), recMk(4, "b", 3, false), recMk(5, "a", 4, false), recMk(6, "b", 5, false)}
	got = recPlan(f, pa, recDisk{OK: true, Total: 100, Free: 50}, 50)
	if want := []int64{1}; !reflect.DeepEqual(got.Delete, want) {
		t.Errorf("rule3: %+v", got)
	}
	// 4: còn 1% trống -> xoá tiếp kể cả file mới nhất; hết file thường mà vẫn dưới 2% -> Stop
	f = []recFile{recMk(1, "a", 0, false), recMk(2, "a", 1, false)}
	got = recPlan(f, pa, recDisk{OK: true, Total: 1000, Free: 10}, 80)
	if want := []int64{1}; !reflect.DeepEqual(got.Delete, want) || got.Stop {
		t.Errorf("rule4 một file: %+v", got)
	}
	got = recPlan(f, pa, recDisk{OK: true, Total: 2000, Free: 0}, 80)
	if want := []int64{1, 2}; !reflect.DeepEqual(got.Delete, want) || !got.Stop {
		t.Errorf("rule4 stop: %+v", got)
	}
	if got = recPlan(f, pa, recDisk{OK: false, Total: 1000}, 80); len(got.Delete) != 0 || got.Stop {
		t.Errorf("ổ lỗi: %+v", got)
	}
	if got = recPlan(f, pa, recDisk{OK: true, Total: 0}, 80); len(got.Delete) != 0 || got.Stop {
		t.Errorf("Total 0: %+v", got)
	}
}

func TestRecPlanSweepNeverDeletesHeld(t *testing.T) {
	type disk struct {
		name string
		d    recDisk
	}
	type limit struct {
		name string
		v    int64
	}
	// position: file mục tiêu của camera a nằm ở đâu (to hơn 20 byte, các file khác 10 byte); sole = các file còn lại đều held.
	type position struct {
		name string
		idx  int
		sole bool
	}
	heldSet := []bool{false, true}
	limits := []limit{{"khong", 0}, {"3file", 30}}
	disks := []disk{
		{"du", recDisk{OK: true, Total: 1000, Free: 900}},
		{"vuot", recDisk{OK: true, Total: 100, Free: 50}},
		{"duoi2", recDisk{OK: true, Total: 1000, Free: 10}},
		{"het", recDisk{OK: true, Total: 100000, Free: 0}}, // xoá hết file thường vẫn dưới 2%
	}
	positions := []position{{"sole", 1, true}, {"oldest", 0, false}, {"middle", 2, false}, {"newest", 3, false}}
	n, stops := 0, 0
	for _, mode := range recModes {
		for _, held := range heldSet {
			for _, lim := range limits {
				for _, dk := range disks {
					for _, pos := range positions {
						n++
						name := fmt.Sprintf("%s/held=%v/lim=%s/%s/%s", mode, held, lim.name, dk.name, pos.name)
						var files []recFile
						id := int64(0)
						newest := map[string]int64{}
						for _, cam := range []string{"a", "b"} {
							for i := 0; i < 4; i++ {
								id++
								f := recMk(id, cam, i, false)
								if cam == "a" {
									f.Held = i == pos.idx && held || pos.sole && i != pos.idx
									if i == pos.idx {
										f.Bytes = 20
									}
								}
								files = append(files, f)
								newest[cam] = id
							}
						}
						w := []recWindow{{From: "22:00", To: "23:00"}}
						pol := map[string]recPolicy{
							"a": {Mode: mode, Windows: w, LimitBytes: lim.v},
							"b": {Mode: mode, Windows: w, LimitBytes: lim.v},
						}
						d := dk.d
						got := recPlan(files, pol, d, 50)
						in := map[int64]bool{}
						var freed, freedNoNewest int64
						byID := map[int64]recFile{}
						for _, f := range files {
							byID[f.ID] = f
						}
						isNewest := func(x int64) bool { return x == newest["a"] || x == newest["b"] }
						newestGone := false
						for _, x := range got.Delete {
							if in[x] {
								t.Errorf("%s: id %d trùng", name, x)
							}
							in[x] = true
							freed += byID[x].Bytes
							if isNewest(x) {
								newestGone = true
							} else {
								freedNoNewest += byID[x].Bytes
							}
							if byID[x].Held { // (a)
								t.Errorf("%s: xoá file held %d", name, x)
							}
						}
						// (b) file mới nhất chỉ bị xoá khi ổ vẫn dưới 2% sau khi xoá các file khác
						if newestGone && (d.Free+freedNoNewest)*100 >= 2*d.Total {
							t.Errorf("%s: xoá file mới nhất khi ổ không dưới 2%%", name)
						}
						if got.Stop { // (c)
							stops++
							for _, f := range files {
								if !f.Held && !in[f.ID] {
									t.Errorf("%s: Stop nhưng file thường %d chưa xoá", name, f.ID)
								}
							}
							if (d.Free+freed)*100 >= 2*d.Total {
								t.Errorf("%s: Stop khi ổ không dưới 2%%", name)
							}
						}
					}
				}
			}
		}
	}
	if want := len(recModes) * len(heldSet) * len(limits) * len(disks) * len(positions); n != want {
		t.Fatalf("chạy %d ca, cần %d", n, want)
	}
	// ổ "het" trong mọi ca phải dừng ghi: không để (c) rỗng
	if want := len(recModes) * len(heldSet) * len(limits) * len(positions); stops != want {
		t.Fatalf("Stop ở %d ca, cần %d", stops, want)
	}
	t.Logf("đã chạy %d ca, Stop ở %d ca", n, stops)
}

func TestRecWantedMoreShapes(t *testing.T) {
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, tgICT) }
	night := recPolicy{Mode: "schedule", Windows: []recWindow{{From: "22:00", To: "23:00"}}}
	if !recWanted(night, recFile{Start: day(1, 10, 0), End: day(3, 10, 0)}) {
		t.Error("file nhiều ngày phải trùng khung")
	}
	noon := recPolicy{Mode: "schedule", Windows: []recWindow{{From: "12:00", To: "13:30"}}}
	for _, c := range []struct {
		s, e time.Time
		want bool
	}{
		{day(1, 13, 29), day(1, 13, 40), true},
		{day(1, 13, 31), day(1, 13, 40), false},
		{day(1, 11, 0), day(1, 11, 59), false},
		{day(1, 11, 59), day(1, 12, 0), true},
	} {
		if got := recWanted(noon, recFile{Start: c.s, End: c.e}); got != c.want {
			t.Errorf("%v-%v: %v, cần %v", c.s, c.e, got, c.want)
		}
	}
	// khung hỏng toàn bộ: không khung nào khớp nên false (Task 5 sẽ từ chối chính sách như vậy)
	bad := recPolicy{Mode: "schedule", Windows: []recWindow{{From: "abc", To: "24:99"}, {From: "", To: ""}}}
	if recWanted(bad, recFile{Start: day(1, 12, 0), End: day(1, 12, 1)}) {
		t.Error("khung hỏng phải không khớp")
	}
}

func TestRecAddHeldRoundTrip(t *testing.T) {
	db := newDB(t)
	b := time.Unix(9000, 0)
	recAdd(db, recFile{Cam: "x", Path: "h", Start: b, End: b.Add(time.Second), Bytes: 1, Held: true})
	recAdd(db, recFile{Cam: "x", Path: "n", Start: b.Add(time.Minute), End: b.Add(2 * time.Minute), Bytes: 1})
	all, err := recAll(db)
	if err != nil || len(all) != 2 || !all[0].Held || all[1].Held {
		t.Fatalf("held: %+v %v", all, err)
	}
}

func TestRecHoldRange(t *testing.T) {
	b := time.Unix(1000, 0)
	at := func(s int) time.Time { return b.Add(time.Duration(s) * time.Second) }
	cases := []struct {
		sec  int
		want []string
	}{{105, []string{"A", "B"}}, {150, []string{"B"}}, {195, []string{"B", "C"}}}
	for _, c := range cases {
		db := newDB(t)
		names := map[int64]string{}
		for i, n := range []string{"A", "B", "C"} {
			id, err := recAdd(db, recFile{Cam: "x", Path: "/rec/" + n, Start: at(i * 100), End: at((i + 1) * 100), Bytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			names[id] = n
		}
		// báo động: 10 s trước, 10 s sau
		k, err := recHoldRange(db, "x", at(c.sec-10), at(c.sec+10))
		if err != nil || k != len(c.want) {
			t.Fatalf("%d s: giữ %d (%v), cần %d", c.sec, k, err, len(c.want))
		}
		all, _ := recAll(db)
		var got []string
		for _, f := range all {
			if f.Held {
				got = append(got, names[f.ID])
			}
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%d s: %v, cần %v", c.sec, got, c.want)
		}
	}
}

func TestRecIndexRangeRemove(t *testing.T) {
	db := newDB(t)
	b := time.Unix(5000, 0)
	i1, _ := recAdd(db, recFile{Cam: "x", Path: "p1", Start: b, End: b.Add(10 * time.Second), Bytes: 5})
	recAdd(db, recFile{Cam: "y", Path: "p2", Start: b, End: b.Add(10 * time.Second), Bytes: 5})
	if _, err := recAdd(db, recFile{Cam: "x", Path: "p1"}); err == nil {
		t.Error("path trùng phải lỗi")
	}
	r, _ := recRange(db, "x", b.Add(5*time.Second), b.Add(20*time.Second))
	if len(r) != 1 || r[0].Path != "p1" || !r[0].Start.Equal(b) {
		t.Fatalf("range: %+v", r)
	}
	if err := recRemove(db, []int64{i1}); err != nil {
		t.Fatal(err)
	}
	if all, _ := recAll(db); len(all) != 1 || all[0].Cam != "y" {
		t.Errorf("sau xoá: %+v", all)
	}
}
