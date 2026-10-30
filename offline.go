package main

import (
	"context"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

// Báo thiết bị mất kết nối: mỗi phút xem mọi thiết bị có trạng thái online:<id>; offline liên tục quá 10 phút thì báo
// một lần (loại "thiet_bi", cả đọc to lẫn Telegram), online lại sau khi đã báo thì báo "đã kết nối lại". Mất ngắn hơn
// 10 phút (nhấp nháy Wi-Fi) không báo gì.
const offlineAfter = 10 * time.Minute

type offlineWatch struct {
	h     *Hub
	since map[string]time.Time // thiết bị đang offline: lúc thấy offline đầu tiên của lần mất này
	told  map[string]bool      // lần mất này đã báo
}

// ponytail: trạng thái nằm trong RAM; hubd khởi động lại thì đồng hồ 10 phút tính lại từ đầu. Lưu SQLite nếu cần báo xuyên khởi động.
func newOfflineWatch(h *Hub) *offlineWatch {
	return &offlineWatch{h: h, since: map[string]time.Time{}, told: map[string]bool{}}
}

func (w *offlineWatch) check(now time.Time) {
	snap := w.h.Snapshot()
	for _, d := range w.h.cfg.Devices {
		st, ok := snap["online:"+d.ID]
		if !ok {
			continue
		}
		name := d.Name
		if name == "" {
			name = d.ID
		}
		if st != "offline" {
			if w.told[d.ID] {
				w.h.notify("info", "thiet_bi", name+" đã kết nối lại.")
			}
			delete(w.since, d.ID)
			delete(w.told, d.ID)
			continue
		}
		if _, ok := w.since[d.ID]; !ok {
			w.since[d.ID] = now
		}
		if !w.told[d.ID] && now.Sub(w.since[d.ID]) > offlineAfter {
			w.told[d.ID] = true
			w.h.notify("warn", "thiet_bi", name+" mất kết nối hơn 10 phút.")
		}
	}
}

// probeCams: một kết nối TCP tới cổng RTSP của mỗi camera nối thẳng (tối đa 8 cùng lúc, mỗi kết nối 3 giây),
// kết quả ghi vào online:<id> như thiết bị có online_topic. Không đăng nhập nên không có mật khẩu để lộ; log chỉ có id.
func probeCams(h *Hub, cams []camRec, now time.Time) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, c := range cams {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			port := c.RTSPPort
			if port == 0 {
				port = 554 // cùng mặc định với camstore
			}
			state := "online"
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.Host, strconv.Itoa(port)), 3*time.Second)
			if err != nil {
				state = "offline"
			} else {
				conn.Close()
			}
			h.Ingest("online:"+c.ID, state, now)
		}()
	}
	wg.Wait()
}

// runOffline chạy nền: kiểm camera nối thẳng và theo dõi mất kết nối, mỗi phút một lần.
func runOffline(ctx context.Context, h *Hub) {
	w := newOfflineWatch(h)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if cams, err := camList(h.db); err != nil {
			log.Printf("không đọc được danh sách camera để kiểm kết nối: %v", err)
		} else {
			probeCams(h, cams, time.Now())
		}
		w.check(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
