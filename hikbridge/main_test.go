package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Cấu hình đầu ghi lấy từ hubd (trang Cài đặt → Camera) bằng token camera; token sai thì báo lỗi, không đoán.
func TestFetchConfFromHubd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/service/nvr" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "cần token camera", 401)
			return
		}
		io.WriteString(w, `{"enabled":true,"host":"192.168.1.64","user":"viewer","pass":"p@ss"}`)
	}))
	defer srv.Close()
	c, err := fetchConf(srv.URL, "tok")
	if err != nil || c != (nvrConf{true, "192.168.1.64", "viewer", "p@ss"}) {
		t.Fatalf("conf=%+v err=%v", c, err)
	}
	if _, err := fetchConf(srv.URL, "sai"); err == nil {
		t.Fatal("token sai mà không báo lỗi")
	}
}
