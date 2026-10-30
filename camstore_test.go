package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func camTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := OpenDB(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func zoneIs(zs ...string) func(string) bool {
	return func(z string) bool {
		for _, x := range zs {
			if x == z {
				return true
			}
		}
		return false
	}
}

func goodCam() camRec {
	return camRec{Name: "Camera cổng", Zone: "z1", Kind: "hik_cam", Host: "192.168.1.50", User: "admin", Pass: "s3cret"}
}

func TestCamStoreCRUD(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	c, err := camSave(d, goodCam(), ok)
	if err != nil || c.ID != "cam_camera_cong" || c.HTTPPort != 80 || c.RTSPPort != 554 {
		t.Fatalf("save: %+v %v", c, err)
	}
	if l, err := camList(d); err != nil || len(l) != 1 || l[0].ID != c.ID {
		t.Fatalf("list: %+v", l)
	}
	// sửa không nhập mật khẩu: giữ mật khẩu cũ
	e := c
	e.Pass, e.Name = "", "Cổng chính"
	if e, err = camSave(d, e, ok); err != nil || e.Pass != "s3cret" || e.ID != c.ID {
		t.Fatalf("edit: %+v %v", e, err)
	}
	// đổi host không nhập mật khẩu: bỏ mật khẩu cũ
	e.Pass, e.Host = "", "192.168.1.51"
	if e, err = camSave(d, e, ok); err != nil || e.Pass != "" {
		t.Fatalf("host change: %+v %v", e, err)
	}
	if g, found, err := camGet(d, c.ID); err != nil || !found || g.Pass != "" || g.Host != "192.168.1.51" {
		t.Fatalf("get: %+v %v", g, found)
	}
	del1, e1 := camDelete(d, c.ID)
	del2, e2 := camDelete(d, c.ID)
	if l, _ := camList(d); !del1 || del2 || e1 != nil || e2 != nil || len(l) != 0 {
		t.Fatal("delete")
	}
	if _, err := camSave(d, camRec{ID: "cam_nope", Name: "x", Zone: "z1", Kind: "rtsp", Host: "h", MainPath: "/a"}, ok); err == nil {
		t.Fatal("sửa id không tồn tại phải lỗi")
	}
}

func TestCamStoreUniqueIDs(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	a, _ := camSave(d, goodCam(), ok)
	b, _ := camSave(d, goodCam(), ok)
	if a.ID != "cam_camera_cong" || b.ID != "cam_camera_cong_2" {
		t.Fatalf("ids: %s %s", a.ID, b.ID)
	}
	// không bao giờ trùng mẫu id của đầu ghi cam_ch<số>
	n := goodCam()
	n.Name = "ch1"
	c, _ := camSave(d, n, ok)
	if nvrCamID.MatchString(c.ID) {
		t.Fatalf("id đụng id đầu ghi: %s", c.ID)
	}
}

func TestCamStoreRejectsInvalid(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	rtsp := func(m func(*camRec)) camRec {
		c := camRec{Name: "A", Zone: "z1", Kind: "rtsp", Host: "10.0.0.2", MainPath: "/s1", SubPath: "/s2"}
		m(&c)
		return c
	}
	cases := []struct {
		name string
		c    camRec
		want string
	}{
		{"tên rỗng", rtsp(func(c *camRec) { c.Name = "" }), "tên"},
		{"tên 41", rtsp(func(c *camRec) { c.Name = strings.Repeat("a", 41) }), "tên"},
		{"host http", rtsp(func(c *camRec) { c.Host = "http://1.2.3.4" }), "địa chỉ"},
		{"host /", rtsp(func(c *camRec) { c.Host = "1.2.3.4/x" }), "địa chỉ"},
		{"host space", rtsp(func(c *camRec) { c.Host = "1.2.3 4" }), "địa chỉ"},
		{"host có cổng", rtsp(func(c *camRec) { c.Host = "1.2.3.4:554" }), "địa chỉ"},
		{"http port 0", rtsp(func(c *camRec) { c.HTTPPort = -1 }), "HTTP"},
		{"http port 70000", rtsp(func(c *camRec) { c.HTTPPort = 70000 }), "HTTP"},
		{"rtsp port 70000", rtsp(func(c *camRec) { c.RTSPPort = 70000 }), "RTSP"},
		{"kind abc", rtsp(func(c *camRec) { c.Kind = "abc" }), "loại"},
		{"zone lạ", rtsp(func(c *camRec) { c.Zone = "zx" }), "khu vực"},
		{"main không /", rtsp(func(c *camRec) { c.MainPath = "s1" }), "đường dẫn"},
		{"main rtsp://", rtsp(func(c *camRec) { c.MainPath = "/rtsp://x" }), "đường dẫn"},
		{"main rỗng", rtsp(func(c *camRec) { c.MainPath = "" }), "đường dẫn"},
		{"sub space", rtsp(func(c *camRec) { c.SubPath = "/a b" }), "đường dẫn"},
	}
	for _, tc := range cases {
		_, err := camSave(d, tc.c, ok)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("%s: err=%v, cần chứa %q", tc.name, err, tc.want)
		}
	}
	if l, _ := camList(d); len(l) != 0 {
		t.Fatal("bản ghi sai không được lưu")
	}
	// cổng 0 nghĩa là mặc định, không phải lỗi
	if _, err := camSave(d, rtsp(func(c *camRec) { c.HTTPPort = 0 }), ok); err != nil {
		t.Fatal(err)
	}
}

func TestCamViewHidesPassword(t *testing.T) {
	v := camRec{ID: "cam_a", Name: "A", Pass: "hunter2xyz", Host: "h"}.view()
	if _, has := v["pass"]; has || v["pass_set"] != true {
		t.Fatalf("view: %v", v)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "hunter2xyz") {
		t.Fatal("lộ mật khẩu: " + string(b))
	}
	if (camRec{}).view()["pass_set"] != false {
		t.Fatal("pass_set phải false khi chưa có mật khẩu")
	}
}

func TestCamRTSPURL(t *testing.T) {
	h := camRec{Kind: "hik_cam", Host: "10.0.0.5", RTSPPort: 8554, HTTPPort: 8080, User: "admin", Pass: "pw"}
	if g := h.rtsp(false); g != "rtsp://admin:pw@10.0.0.5:8554/Streaming/Channels/101" {
		t.Fatal(g)
	}
	if g := h.rtsp(true); g != "rtsp://admin:pw@10.0.0.5:8554/Streaming/Channels/102" {
		t.Fatal(g)
	}
	if g := h.snapURL(); g != "http://10.0.0.5:8080/ISAPI/Streaming/channels/101/picture" {
		t.Fatal(g)
	}
	r := camRec{Kind: "rtsp", Host: "10.0.0.6", RTSPPort: 554, User: "u", Pass: "p", MainPath: "/stream1", SubPath: "/stream2"}
	if r.rtsp(false) != "rtsp://u:p@10.0.0.6:554/stream1" || r.rtsp(true) != "rtsp://u:p@10.0.0.6:554/stream2" || r.snapURL() != "" {
		t.Fatal(r.rtsp(false), r.rtsp(true))
	}
	r.SubPath = ""
	if r.rtsp(true) != "rtsp://u:p@10.0.0.6:554/stream1" {
		t.Fatal("thiếu SubPath phải dùng MainPath")
	}
	if len(camTemplates) < 3 || camTemplates[0].MainPath == "" {
		t.Fatal("camTemplates")
	}
}

// Review Focus 1: mật khẩu có ký tự đặc biệt phải được mã hoá trong URL và lấy lại đúng.
func TestCamRTSPURLEscapesPassword(t *testing.T) {
	const pw = "p@ss:w/rd#%"
	for _, c := range []camRec{
		{Kind: "hik_cam", Host: "10.0.0.5", RTSPPort: 554, User: "ad min", Pass: pw},
		{Kind: "rtsp", Host: "10.0.0.5", RTSPPort: 554, User: "ad min", Pass: pw, MainPath: "/s"},
	} {
		s := c.rtsp(false)
		if strings.Contains(s, pw) {
			t.Fatalf("mật khẩu chưa mã hoá: %s", s)
		}
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		gp, _ := u.User.Password()
		if u.User.Username() != "ad min" || gp != pw || u.Hostname() != "10.0.0.5" || u.Port() != "554" {
			t.Fatalf("lấy lại sai: %q %q %s", u.User.Username(), gp, s)
		}
	}
}

// Dòng hỏng (JSON không đọc được) không được bị camera mới ghi đè; camList bỏ qua dòng hỏng, camGet báo lỗi.
func TestCamStoreCorruptRowNotOverwritten(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	const bad = "{not json"
	if _, err := d.sql.Exec(`INSERT INTO cameras(id, data) VALUES('cam_camera_cong', ?)`, bad); err != nil {
		t.Fatal(err)
	}
	c, err := camSave(d, goodCam(), ok)
	if err != nil || c.ID != "cam_camera_cong_2" {
		t.Fatalf("save: %+v %v", c, err)
	}
	var raw string
	d.sql.QueryRow(`SELECT data FROM cameras WHERE id='cam_camera_cong'`).Scan(&raw)
	if raw != bad {
		t.Fatalf("dòng hỏng bị ghi đè: %q", raw)
	}
	if l, err := camList(d); err != nil || len(l) != 1 || l[0].ID != c.ID {
		t.Fatalf("list: %+v %v", l, err)
	}
	if _, found, err := camGet(d, "cam_camera_cong"); found || err == nil {
		t.Fatalf("camGet dòng hỏng phải lỗi: %v %v", found, err)
	}
	e := goodCam()
	e.ID = "cam_camera_cong"
	if _, err := camSave(d, e, ok); err == nil {
		t.Fatal("sửa dòng hỏng phải lỗi")
	}
}

func TestCamStoreDBErrorsSurface(t *testing.T) {
	d := camTestDB(t)
	d.sql.Close()
	if _, err := camList(d); err == nil {
		t.Error("camList")
	}
	if _, _, err := camGet(d, "x"); err == nil {
		t.Error("camGet")
	}
	if _, err := camDelete(d, "x"); err == nil {
		t.Error("camDelete")
	}
	if _, err := camSave(d, goodCam(), nil); err == nil {
		t.Error("camSave")
	}
}
