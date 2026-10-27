package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Cấu hình sinh tự động (Tuya sau khi quét QR trên dashboard) nằm ở thư mục riêng ngoài git: thiết bị và số đo
// trong đó thay mục cùng id của config.yaml, mục mới thì thêm vào. File hỏng bị bỏ qua, không làm hubd sập.
func TestLoadConfigMergesGeneratedDir(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "config.yaml")
	os.WriteFile(main, []byte(`devices:
  - {id: a, name: "Cũ", kind: light}
  - {id: b, name: "Giữ", kind: socket}
entities:
  - {id: e1, name: "Giữ"}
`), 0o644)
	gen := filepath.Join(dir, "gen")
	os.Mkdir(gen, 0o755)
	os.WriteFile(filepath.Join(gen, "tuya.yaml"), []byte(`devices:
  - {"id": "a", "name": "Mới", "kind": "light"}
  - {"id": "c", "name": "Thêm", "kind": "fan"}
entities:
  - {"id": "e2", "name": "Thêm"}
`), 0o644)
	os.WriteFile(filepath.Join(gen, "hong.yaml"), []byte("devices: [ {id: x"), 0o644)
	os.WriteFile(filepath.Join(gen, "ghi-chu.txt"), []byte("devices: [{id: y}]"), 0o644)
	t.Setenv("HUBD_GEN_DIR", gen)
	cfg, _, err := LoadConfig(main)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range cfg.Devices {
		got = append(got, d.ID+"="+d.Name)
	}
	if want := []string{"a=Mới", "b=Giữ", "c=Thêm"}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("thiết bị = %v, muốn %v", got, want)
	}
	if len(cfg.Entities) != 2 || cfg.Entities[0].ID != "e1" || cfg.Entities[1].ID != "e2" {
		t.Fatalf("số đo = %+v", cfg.Entities)
	}
	t.Setenv("HUBD_GEN_DIR", filepath.Join(dir, "khong-co"))
	if _, _, err := LoadConfig(main); err != nil {
		t.Fatalf("chưa có thư mục sinh (chưa quét QR lần nào) phải chạy bình thường: %v", err)
	}
}

// Quét QR xong, file sinh đổi: hubd biết để khởi động lại (Docker chạy lại) và nạp thiết bị mới.
func TestWatchFilesReportsChange(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "tuya.yaml")
	os.WriteFile(f, []byte("a"), 0o644)
	changed := make(chan string, 1)
	stop := watchFiles([]string{f, filepath.Join(dir, "chua-co.yaml")}, 10*time.Millisecond, func(p string) { changed <- p })
	defer stop()
	time.Sleep(30 * time.Millisecond)
	select {
	case p := <-changed:
		t.Fatalf("chưa đổi gì mà đã báo đổi %s", p)
	default:
	}
	os.WriteFile(f, []byte("bb"), 0o644)
	os.Chtimes(f, time.Now().Add(time.Second), time.Now().Add(time.Second))
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("file đổi mà không báo")
	}
}

// Ổ cắm Hồ Cá hiện hai thẻ (2026-10-27): config.yaml còn khai nguyên ổ (tuya_x), quét QR sinh từng lỗ (tuya_x_1, _2)
// cùng command_topic. Thiết bị tách ra thay thiết bị gốc; thiết bị khác dùng chung topic (khai tay) vẫn giữ.
func TestGeneratedSplitDeviceReplacesWholeDevice(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "config.yaml")
	os.WriteFile(main, []byte(`devices:
  - {id: tuya_x, name: "Ổ cắm Hồ Cá", kind: socket, command_topic: tuya/x/cmd, protected: true}
  - {id: bom, name: "Bơm hồ cá", kind: socket, command_topic: tuya/x/cmd}
  - {id: tuya_x2, name: "Ổ khác", kind: socket, command_topic: tuya/x2/cmd}
  - {id: cam_a, name: "Camera", kind: camera}
`), 0o644)
	gen := filepath.Join(dir, "gen")
	os.Mkdir(gen, 0o755)
	os.WriteFile(filepath.Join(gen, "tuya.yaml"), []byte(`devices:
  - {"id": "tuya_x_1", "name": "Lỗ 1", "kind": "socket", "command_topic": "tuya/x/cmd", "group": "Ổ cắm Hồ Cá"}
  - {"id": "tuya_x_2", "name": "Lỗ 2", "kind": "socket", "command_topic": "tuya/x/cmd", "group": "Ổ cắm Hồ Cá"}
  - {"id": "cam_a_1", "name": "Camera sinh", "kind": "camera"}
`), 0o644)
	t.Setenv("HUBD_GEN_DIR", gen)
	cfg, _, err := LoadConfig(main)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range cfg.Devices {
		got = append(got, d.ID)
	}
	if want := []string{"bom", "tuya_x2", "cam_a", "tuya_x_1", "tuya_x_2", "cam_a_1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("thiết bị = %v, muốn %v", got, want)
	}
}
