package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// alert: một sự kiện đã làm sạch. Ch là số kênh; Type, Target chỉ còn [a-z0-9_] để làm topic MQTT an toàn.
type alert struct {
	Ch, Type, Target string
	Active           bool
}

var (
	startTag = []byte("<EventNotificationAlert")
	endTag   = []byte("</EventNotificationAlert>")
	unsafe   = regexp.MustCompile(`[^a-z0-9_]`)
)

// splitAlert cắt luồng thành từng khối XML sự kiện, bỏ mọi thứ khác (đường biên multipart, header, ảnh JPEG).
// Không dựa vào khung multipart vì firmware Hikvision hay gửi lệch chuẩn (pyhik của Home Assistant cũng làm vậy).
func splitAlert(data []byte, atEOF bool) (int, []byte, error) {
	i := bytes.Index(data, startTag)
	if i < 0 {
		if atEOF {
			return len(data), nil, nil
		}
		return max(0, len(data)-len(startTag)), nil, nil
	}
	j := bytes.Index(data[i:], endTag)
	if j < 0 {
		if atEOF {
			return len(data), nil, nil
		}
		return i, nil, nil
	}
	end := i + j + len(endTag)
	i += bytes.LastIndex(data[i:i+j], startTag) // khối cụt thiếu thẻ đóng: lấy thẻ mở gần thẻ đóng nhất
	return end, data[i:end], nil
}

// readAlerts đọc tới khi luồng đứt; alertStream không tự kết thúc nên luôn trả lỗi để bên gọi nối lại.
func readAlerts(r io.Reader, fn func(alert)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	sc.Split(splitAlert)
	for sc.Scan() {
		if a, ok := parseAlert(sc.Bytes()); ok {
			fn(a)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func clean(s string) string {
	return unsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "")
}

func parseAlert(b []byte) (alert, bool) {
	var x struct {
		Ch      string   `xml:"channelID"`
		DynCh   string   `xml:"dynChannelID"`
		Type    string   `xml:"eventType"`
		State   string   `xml:"eventState"`
		Target  string   `xml:"targetType"`
		Regions []string `xml:"DetectionRegionList>DetectionRegionEntry>detectionTarget"`
	}
	if xml.Unmarshal(b, &x) != nil {
		return alert{}, false
	}
	ch := strings.TrimSpace(x.Ch)
	if ch == "" {
		ch = strings.TrimSpace(x.DynCh)
	}
	n, err := strconv.Atoi(ch)
	a := alert{Ch: strconv.Itoa(n), Type: clean(x.Type), Target: clean(x.Target), Active: strings.EqualFold(strings.TrimSpace(x.State), "active")}
	for _, r := range x.Regions {
		if a.Target == "" {
			a.Target = clean(r)
		}
	}
	return a, err == nil && n >= 0 && a.Type != ""
}

// parseStatus đọc /ISAPI/ContentMgmt/InputProxy/channels/status: kênh -> camera còn nối với đầu ghi.
func parseStatus(b []byte) (map[string]bool, error) {
	var x struct {
		Ch []struct {
			ID     string `xml:"id"`
			Online bool   `xml:"online"`
		} `xml:"InputProxyChannelStatus"`
	}
	if err := xml.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	st := map[string]bool{}
	for _, c := range x.Ch {
		if n, err := strconv.Atoi(strings.TrimSpace(c.ID)); err == nil {
			st[strconv.Itoa(n)] = c.Online
		}
	}
	return st, nil
}

// tracker giữ trạng thái đã publish. Đầu ghi gửi "active" lặp lại suốt lúc có chuyển động và thường không gửi
// "inactive", nên topic tự về OFF sau hold không có tin mới.
type tracker struct {
	mu     sync.Mutex
	hold   time.Duration
	pub    func(topic, payload string)
	on     map[string]time.Time // topic đang ON -> lần active gần nhất
	seen   map[string]bool      // topic đã publish ít nhất một lần
	online map[string]bool      // kênh -> online
}

func newTracker(hold time.Duration, pub func(topic, payload string)) *tracker {
	return &tracker{hold: hold, pub: pub, on: map[string]time.Time{}, seen: map[string]bool{}, online: map[string]bool{}}
}

func (t *tracker) alert(a alert, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.set("hik/"+a.Ch+"/"+a.Type, a.Active, now)
	if a.Active && a.Target != "" {
		t.set("hik/"+a.Ch+"/"+a.Target, true, now)
	}
}

func (t *tracker) set(topic string, on bool, now time.Time) {
	_, was := t.on[topic]
	if on {
		t.on[topic] = now
	} else {
		delete(t.on, topic)
	}
	if was != on || !t.seen[topic] {
		t.seen[topic] = true
		t.pub(topic, map[bool]string{true: "ON", false: "OFF"}[on])
	}
}

func (t *tracker) expire(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, topic := range slices.Sorted(maps.Keys(t.on)) {
		if now.Sub(t.on[topic]) >= t.hold {
			t.set(topic, false, now)
		}
	}
}

// status: trạng thái kênh từ đầu ghi; err != nil (không tới được đầu ghi) thì mọi kênh đã biết về offline.
func (t *tracker) status(st map[string]bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		st = map[string]bool{}
		for ch := range t.online {
			st[ch] = false
		}
	}
	for _, ch := range slices.Sorted(maps.Keys(st)) {
		if old, ok := t.online[ch]; !ok || old != st[ch] {
			t.online[ch] = st[ch]
			t.pub("hik/"+ch+"/online", map[bool]string{true: "online", false: "offline"}[st[ch]])
		}
	}
}
