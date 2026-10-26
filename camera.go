package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"hubd/isapi"
)

// Camera Hikvision (mục 15.1): ảnh chụp ISAPI và hình trực tiếp từ go2rtc, cả hai đi qua hubd sau PIN.
// Trình duyệt chỉ thấy /api/cam/...; địa chỉ, tài khoản đầu ghi và go2rtc ở phía máy chủ (.env).
// Camera = thiết bị có `channel` (số kênh trên đầu ghi); luồng go2rtc tên ch<kênh> (go2rtc/go2rtc.yaml).
type cams struct {
	nvr    string // host[:port] đầu ghi; rỗng = chưa cấu hình
	client *http.Client
	go2rtc *url.URL
	byID   map[string]Device
}

func newCams(devs []Device) *cams {
	c := &cams{nvr: os.Getenv("NVR_HOST"), byID: map[string]Device{}}
	c.client = &http.Client{Timeout: 10 * time.Second,
		Transport: &isapi.Transport{User: os.Getenv("NVR_USER"), Pass: os.Getenv("NVR_PASS")}}
	g := os.Getenv("GO2RTC_URL")
	if g == "" {
		g = "http://go2rtc:1984" // chỉ trong mạng Docker, không mở ra LAN
	}
	c.go2rtc, _ = url.Parse(g)
	for _, d := range devs {
		if d.Channel > 0 {
			c.byID[d.ID] = d
		}
	}
	return c
}

func (c *cams) register(mux *http.ServeMux, auth *Auth) {
	mux.HandleFunc("GET /api/cam/{id}/snapshot", auth.Require(c.snapshot))
	mux.HandleFunc("GET /api/cam/{id}/ws", auth.Require(c.live))
	mux.HandleFunc("GET /api/cam-js/{file}", auth.Require(c.js))
}

func (c *cams) snapshot(w http.ResponseWriter, r *http.Request) {
	d, ok := c.byID[r.PathValue("id")]
	if !ok {
		http.Error(w, "không có camera này", 404)
		return
	}
	if c.nvr == "" {
		http.Error(w, "chưa cấu hình đầu ghi (NVR_HOST)", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/ISAPI/Streaming/channels/%d01/picture", c.nvr, d.Channel), nil)
	res, err := c.client.Do(req)
	if err != nil {
		log.Printf("ảnh camera %s: %v", d.ID, err)
		http.Error(w, "đầu ghi không trả ảnh", 502)
		return
	}
	defer res.Body.Close()
	// chỉ chuyển ảnh; lỗi XML của đầu ghi ghi log, không đưa ra trình duyệt
	if ct := res.Header.Get("Content-Type"); res.StatusCode != 200 || !strings.HasPrefix(ct, "image/") {
		log.Printf("ảnh camera %s: HTTP %d %s", d.ID, res.StatusCode, ct)
		http.Error(w, "đầu ghi không trả ảnh", 502)
		return
	}
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=30")
	io.Copy(w, io.LimitReader(res.Body, 8<<20))
}

// live chuyển tiếp WebSocket của go2rtc (MSE + báo hiệu WebRTC). Tên luồng lấy từ cấu hình, bỏ mọi tham số
// của trình duyệt: go2rtc nhận src là URL hay lệnh tuỳ ý.
func (c *cams) live(w http.ResponseWriter, r *http.Request) {
	d, ok := c.byID[r.PathValue("id")]
	if !ok {
		http.Error(w, "không có camera này", 404)
		return
	}
	c.proxy(w, r, "/api/ws", "src=ch"+strconv.Itoa(d.Channel))
}

// js: trình phát video-rtc.js của go2rtc, lấy từ chính máy chủ go2rtc để luôn cùng phiên bản giao thức.
func (c *cams) js(w http.ResponseWriter, r *http.Request) {
	f := r.PathValue("file")
	if f != "video-rtc.js" {
		http.Error(w, "không có", 404)
		return
	}
	c.proxy(w, r, "/"+f, "")
}

func (c *cams) proxy(w http.ResponseWriter, r *http.Request, path, query string) {
	(&httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(c.go2rtc)
			p.Out.URL.Path, p.Out.URL.RawPath, p.Out.URL.RawQuery = path, "", query
			p.Out.Header.Del("Cookie") // token PIN của hub không đi tiếp
			p.Out.Header.Del("Origin") // go2rtc so Origin với Host của nó; hub đã kiểm PIN
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("go2rtc: %v", err)
			http.Error(w, "go2rtc chưa chạy", 502)
		},
	}).ServeHTTP(w, r)
}
