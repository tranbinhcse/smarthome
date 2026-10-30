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
}

func TestRecPlanSweepNeverDeletesHeld(t *testing.T) {
	heldSet := []bool{false, true}
	limits := []int64{0, 30}
	disks := map[string]recDisk{
		"du":    {OK: true, Total: 1000, Free: 900},
		"vuot":  {OK: true, Total: 100, Free: 50},
		"duoi2": {OK: true, Total: 1000, Free: 10},
	}
	diskNames := []string{"du", "vuot", "duoi2"}
	positions := []string{"sole", "oldest", "middle", "newest"}
	n := 0
	for _, mode := range recModes {
		for _, held := range heldSet {
			for _, lim := range limits {
				for _, dn := range diskNames {
					for _, pos := range positions {
						n++
						name := fmt.Sprintf("%s/held=%v/lim=%d/%s/%s", mode, held, lim, dn, pos)
						idx := map[string]int{"sole": 1, "oldest": 0, "middle": 1, "newest": 3}[pos]
						var files []recFile
						id := int64(0)
						for _, cam := range []string{"a", "b"} {
							for i := 0; i < 4; i++ {
								id++
								h := false
								if cam == "a" {
									// sole: file mục tiêu là file thường duy nhất của a
									h = i == idx && held || pos == "sole" && i != idx
								}
								files = append(files, recMk(id, cam, i, h))
							}
						}
						pol := map[string]recPolicy{
							"a": {Mode: mode, Windows: []recWindow{{From: "22:00", To: "23:00"}}, LimitBytes: lim},
							"b": {Mode: mode, Windows: []recWindow{{From: "22:00", To: "23:00"}}, LimitBytes: lim},
						}
						d := disks[dn]
						got := recPlan(files, pol, d, 50)
						in := map[int64]bool{}
						var freed int64
						byID := map[int64]recFile{}
						for _, f := range files {
							byID[f.ID] = f
						}
						for _, x := range got.Delete {
							if in[x] {
								t.Errorf("%s: id %d trùng", name, x)
							}
							in[x] = true
							freed += byID[x].Bytes
							if byID[x].Held { // (a)
								t.Errorf("%s: xoá file held %d", name, x)
							}
						}
						for _, cam := range []string{"a", "b"} { // (b)
							nw := byID[map[string]int64{"a": 4, "b": 8}[cam]]
							if in[nw.ID] && dn != "duoi2" {
								t.Errorf("%s: xoá file mới nhất %d khi ổ chưa dưới 2%%", name, nw.ID)
							}
						}
						if got.Stop { // (c)
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
	if want := len(recModes) * len(heldSet) * len(limits) * len(diskNames) * len(positions); n != want {
		t.Fatalf("chạy %d ca, cần %d", n, want)
	}
	t.Logf("đã chạy %d ca", n)
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
