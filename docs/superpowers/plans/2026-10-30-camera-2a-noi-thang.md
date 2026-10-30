# Phần 2A: Camera nối thẳng — kế hoạch làm

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hub nhận thẳng từng camera, không qua đầu thu:
- tự dò camera trong mạng nhà;
- chủ nhà nhập mật khẩu, đặt tên, chọn khu;
- xem trực tiếp và chụp ảnh như camera của đầu thu;
- thiết bị mất kết nối quá 10 phút thì báo (kể cả qua Telegram).

**Architecture:**
- Camera nối thẳng lưu trong một bảng SQLite riêng `cameras`, không qua API cấu hình chung, để mật khẩu không bao giờ bị trả ra. API riêng `/api/cameras` trả `pass_set` thay cho mật khẩu.
- Thiết bị camera vẫn sinh vào `camera.yaml` (cơ chế `syncCamGen` hiện có, đổi danh sách thì hubd khởi động lại), gồm cả kênh đầu thu cũ lẫn camera nối thẳng.
- Xem trực tiếp đi qua `go2rtc` (luồng phụ, sau PIN) như hiện nay, mỗi camera một luồng tên theo id.
- Ảnh chụp:
  - Hikvision lấy bằng ISAPI của chính camera (gói `isapi`, có khoá tạm khi bị từ chối mật khẩu);
  - kiểu RTSP lấy một khung hình từ `go2rtc`.
- Dò camera bằng cách quét lần lượt địa chỉ trong dải LAN chủ nhà nhập (mạng Docker không cho multicast).

**Tech Stack:** Go (stdlib + gói `isapi` sẵn có), SQLite (`history.go`), go2rtc 1.9.14 (container sẵn có), JS thuần (`web/setup-camera.js`).

**Spec:** `docs/superpowers/specs/2026-10-29-camera-bao-dong-telegram-design.md`, mục 2 (điểm 1–2), mục 4, mục 7.1, 7.4, và loại tin "Thiết bị" ở mục 5.

**Phần 2 chia ba kế hoạch,** làm theo thứ tự:
- **2A** (kế hoạch này): danh sách camera, dò, xem, ảnh, báo mất kết nối.
- **2B: ghi hình** (mục 7.3).
  - Bắt đầu bằng một bản thử ghi từ camera thật, để chọn giữa thư viện RTSP thuần Go và `ffmpeg -c copy`. Viết kế hoạch sau khi 2A chạy.
  - Gồm: thư mục ghi gắn từ máy chủ, file ≤ 50 MB cắt ở keyframe, chế độ toàn thời gian/khung giờ, giới hạn theo camera và 80% ổ, xem lại, loại tin "Lưu trữ".
- **2C: sự kiện từ camera** (mục 7.2).
  - `alertStream` ISAPI của từng camera, và client ONVIF (WS‑Security: PullPoint, GetStreamUri, GetSnapshotUri) để camera hãng khác tự lấy được URL luồng và ảnh.
  - Đưa vào MQTT `cam/<id>/<sự kiện>`.

## Global Constraints

- **Nơi chạy:** mọi phần mới nằm trong `hubd`. Không thêm container. Không thêm thư viện Go ngoài ở kế hoạch này.
- **Bí mật:**
  - mật khẩu camera không bao giờ trả ra trình duyệt (API trả `pass_set`);
  - không có trong log;
  - không có trong `GET /api/backup`.
- **Mật khẩu trong URL RTSP** phải được mã hoá đúng (`url.UserPassword`), vì mật khẩu có thể chứa `@ : / # %`.
- **Đầu thu cũ:**
  - nhóm cấu hình `nvr`, id `cam_ch<N>` và topic `hik/<kênh>/…` giữ nguyên, các test cũ phải còn đạt;
  - camera nối thẳng có id `cam_<tên viết thường không dấu>`, không bao giờ trùng `cam_ch<N>`.
- **Quét mạng:**
  - chỉ quét dải chủ nhà nhập, tối đa 256 địa chỉ (từ /24 trở xuống);
  - tối đa 32 kết nối cùng lúc, mỗi kết nối chờ tối đa 400 ms;
  - chỉ thử các cổng 80, 8080, 2020;
  - không đăng nhập thử khi quét: chỉ hỏi những gì không cần mật khẩu.
- **Chống bị camera khoá IP:** camera Hikvision từ chối mật khẩu thì hub tạm ngừng 15 phút cho đúng tài khoản trên đúng camera đó (`isapi.Transport`, như đầu thu hiện nay). Xem trực tiếp cũng không mở luồng trong thời gian đó.
- **Cách làm việc:**
  - test chạy trên máy Ubuntu bằng `scripts/test-on-hub.sh`, không chạy trên Mac;
  - chữ giao diện tiếng Việt có dấu;
  - conventional commits, không ghi dòng AI attribution.

## Review Focus

1. **Mật khẩu có ký tự đặc biệt** (`p@ss:w/rd#%`): URL RTSP gửi `go2rtc` vẫn đúng, và mật khẩu không lọt vào log. Test ở Task 2.
2. **Hai camera cùng tên** ("Camera cổng"): id không trùng nhau, camera sau không ghi đè camera trước. Test ở Task 1.
3. **Chủ nhà nhập dải quá rộng** (`192.168.0.0/16`): bị từ chối kèm lời giải thích, không quét 65 536 địa chỉ. Test ở Task 5.
4. **Mật khẩu sai mà nhiều màn hình đang xem:** sau lần bị từ chối đầu tiên, hub không gửi thêm mật khẩu tới camera đó trong 15 phút (cả ảnh lẫn xem trực tiếp). Test ở Task 4.
5. **Xoá một camera đang được xem hoặc đang có trong luật:** sau khi hubd khởi động lại, gọi ảnh và xem trực tiếp trả 404 rõ ràng, dashboard không vỡ, các camera khác không ảnh hưởng. Test ở Task 3 và Task 4.

---

### Task 1: Kho camera nối thẳng

**Files:**
- Create: `camstore.go`, `camstore_test.go`
- Modify: `history.go` (thêm bảng `cameras(id TEXT PRIMARY KEY, data TEXT NOT NULL)`)

**Interfaces:**
- Produces:
  - `type camRec struct { ID, Name, Zone, Kind, Host string; HTTPPort, RTSPPort int; User, Pass, MainPath, SubPath string }`;
  - `Kind` thuộc `{"hik_cam", "rtsp"}`. Thêm `"onvif"` ở 2C;
  - `func camList(db *DB) []camRec` (sắp theo tên);
  - `func camGet(db *DB, id string) (camRec, bool)`;
  - `func camSave(db *DB, c camRec) (camRec, error)`:
    - id rỗng thì tạo id mới `cam_<slug>`, trùng thì thêm `_2`, `_3`…;
    - `Pass` rỗng khi sửa thì giữ mật khẩu cũ;
    - đổi `Host` mà `Pass` rỗng thì bỏ mật khẩu cũ (cùng quy tắc `nvrPatch`);
  - `func camDelete(db *DB, id string) bool`;
  - `func (c camRec) view() map[string]any`: JSON cho trình duyệt, không có `pass`, có `pass_set`.
- Khoá JSON dùng chung cho API và giao diện: `id, name, zone, kind, host, http_port, rtsp_port, user, pass, main_path, sub_path`. `pass` chỉ có ở chiều gửi lên; chiều trả về dùng `pass_set`.
- Kiểm hợp lệ (`camSave` trả lỗi tiếng Việt):
  - `Name` 1–40 ký tự;
  - `Host` theo `nvrHost` (không có cổng);
  - cổng 1–65535 (mặc định HTTP 80, RTSP 554);
  - `Kind` thuộc tập trên; `Zone` có trong `cfg.zones`;
  - `MainPath`/`SubPath` (chỉ kiểu `rtsp`) bắt đầu bằng `/`, không chứa khoảng trắng, không chứa `://`.

- [ ] **Step 1: Viết test đỏ:**
  - `TestCamStoreCRUD`: lưu, liệt kê, sửa không nhập mật khẩu thì mật khẩu giữ nguyên, đổi host mà không nhập mật khẩu thì mật khẩu bị bỏ, xoá.
  - `TestCamStoreUniqueIDs` (Review Focus 2): hai camera "Camera cổng" → `cam_camera_cong` và `cam_camera_cong_2`.
  - `TestCamStoreRejectsInvalid`: quét một bảng cố định các trường sai, mỗi dòng một trường sai một cách, mong đợi lỗi đúng trường. Bảng gồm:
    - tên rỗng hoặc dài 41 ký tự;
    - host có `http://`, có `/`, có khoảng trắng;
    - cổng 0 và 70000;
    - kind `abc`;
    - zone không tồn tại;
    - path không có `/` đầu, có `rtsp://`.
  - `TestCamViewHidesPassword`: `view()` không có khoá `pass`, có `pass_set: true`; chuỗi JSON không chứa mật khẩu.
- [ ] **Step 2: Chạy** `scripts/test-on-hub.sh`. Mong đợi: FAIL (`camSave undefined`).
- [ ] **Step 3: Viết `camstore.go`.**
- [ ] **Step 4: Chạy lại.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): store directly connected cameras`

### Task 2: Địa chỉ luồng và ảnh theo kiểu camera

**Files:**
- Modify: `camstore.go`
- Test: `camstore_test.go`

**Interfaces:**
- Produces:
  - `func (c camRec) rtsp(sub bool) string`:
    - `hik_cam` → `rtsp://<user>:<pass>@<host>:<rtsp>/Streaming/Channels/101` (luồng chính) hoặc `/102` (luồng phụ);
    - `rtsp` → `MainPath`/`SubPath` (thiếu `SubPath` thì dùng `MainPath`).
  - `func (c camRec) snapURL() string`: `hik_cam` → `http://<host>:<http>/ISAPI/Streaming/channels/101/picture`; `rtsp` → rỗng (ảnh lấy từ go2rtc ở Task 4).
  - `var camTemplates = []camTemplate{...}`: mẫu đường dẫn theo hãng cho giao diện (`{Brand, MainPath, SubPath, RTSPPort}`), lấy từ báo cáo mục 3.2:
    - Dahua/KBVision/Imou: `/cam/realmonitor?channel=1&subtype=0` và `subtype=1`, cổng 554;
    - TP‑Link Tapo/VIGI: `/stream1`, `/stream2`, 554;
    - Yoosee: `/onvif1`, `/onvif2`, 554.

- [ ] **Step 1: Viết test đỏ:**
  - `TestCamRTSPURL`: mỗi kiểu, cả luồng chính và luồng phụ, đúng host và cổng.
  - Thêm Review Focus 1: mật khẩu `p@ss:w/rd#%` → `url.Parse` URL trả về lấy lại đúng tài khoản và mật khẩu, và URL không chứa mật khẩu ở dạng chưa mã hoá.
- [ ] **Step 2: Chạy.** Mong đợi: FAIL.
- [ ] **Step 3: Viết hàm.**
- [ ] **Step 4: Chạy lại.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): stream and snapshot addresses per camera kind`

### Task 3: Sinh thiết bị cho camera nối thẳng

**Files:**
- Modify: `camera.go` (`camDevices`, `syncCamGen`, `newCams` nhận cả camera nối thẳng)
- Test: `camera_test.go`

**Interfaces:**
- Consumes: `camList` (Task 1).
- Produces:
  - `camera.yaml` có thêm các thiết bị `{id: <cam id>, name, zone, kind: camera, online_topic: "cam/<id>/online"}` cho mỗi camera nối thẳng, sau các kênh đầu thu;
  - khi nhóm `nvr` có `enabled: false`, chỉ các kênh đầu thu bị bỏ, camera nối thẳng vẫn còn;
  - `syncCamGen(db)` đọc cả hai nguồn. `camSave`/`camDelete` (qua route ở Task 6) gọi `syncCamGen`, file đổi thì khởi động lại như hiện nay.

- [ ] **Step 1: Viết test đỏ:**
  - `TestCamGenIncludesDirectCameras`: 1 kênh đầu thu cộng 2 camera nối thẳng → `camera.yaml` có 3 thiết bị, đúng id, khu, `online_topic`. Tắt đầu thu → còn 2.
  - `TestCamGenUnchangedForNVROnly`: chỉ có đầu thu thì file giống hệt bản trước thay đổi (test cũ vẫn đạt).
  - Review Focus 5: xoá một camera → `syncCamGen` trả true và thiết bị đó biến khỏi file.
- [ ] **Step 2: Chạy.** Mong đợi: FAIL.
- [ ] **Step 3: Sửa `camera.go`.**
- [ ] **Step 4: Chạy lại toàn bộ.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): generate devices for directly connected cameras`

### Task 4: Ảnh chụp và xem trực tiếp cho camera nối thẳng

**Files:**
- Modify: `camera.go` (`snapshot`, `live`: camera nối thẳng đi nhánh riêng; client ISAPI theo từng camera)
- Test: `camera_test.go`

**Interfaces:**
- Consumes: `camGet`, `rtsp`, `snapURL` (Task 1–2).
- Produces:
  - `GET /api/cam/{id}/snapshot` cho camera nối thẳng:
    - `hik_cam`: gọi `snapURL()` bằng client digest riêng cho từng (host, user, pass), dùng lại `c.client` theo khoá mới;
    - `rtsp`: hub gọi `go2rtc` `PUT /api/streams?name=<id>&src=<rtsp phụ>` rồi `GET /api/frame.jpeg?src=<id>`.
  - Bộ đệm ảnh trong hub: giữ ảnh mới nhất của mỗi camera 10 giây. Nhiều người xem trong 10 giây chỉ gọi camera một lần.
  - `func (c *cams) freshSnapshot(ctx context.Context, id string) ([]byte, string, error)`: lấy ảnh mới, bỏ qua bộ đệm. Phần 3 dùng cho ảnh báo động.
  - `GET /api/cam/{id}/ws` cho camera nối thẳng: chuyển tới `go2rtc` với `name=<id>`, `src=<rtsp phụ>`. Giữ nguyên kiểm Origin. Không mở luồng khi tài khoản của camera đó đang bị khoá tạm.
  - id không tồn tại (kể cả camera vừa xoá) → 404 với câu tiếng Việt.

- [ ] **Step 1: Viết test đỏ**, dùng camera ISAPI giả (`httptest`, digest) và go2rtc giả:
  - `TestDirectCamSnapshotHikvision`: trả ảnh đúng `Content-Type`; gọi 3 lần trong 10 giây thì camera giả chỉ nhận 1 lần; `freshSnapshot` luôn gọi camera.
  - `TestDirectCamSnapshotRTSPViaGo2rtc`: go2rtc giả nhận `PUT /api/streams` với `src` đúng, rồi `frame.jpeg`.
  - `TestDirectCamLockoutStopsRetries` (Review Focus 4):
    - camera giả từ chối mật khẩu;
    - lần ảnh thứ hai trong 15 phút không gửi yêu cầu tới camera;
    - `ws` trả 503;
    - đầu thu và các camera khác không bị ảnh hưởng.
  - `TestDirectCamUnknownID` (Review Focus 5): ảnh và `ws` của id lạ trả 404.
  - `TestDirectCamLiveSrc`: `ws` chuyển tới go2rtc giả với `name=<id>` và `src` là luồng phụ. Mật khẩu không xuất hiện trong log (bắt log trong test).
- [ ] **Step 2: Chạy.** Mong đợi: FAIL.
- [ ] **Step 3: Viết nhánh camera nối thẳng.** Giữ nguyên nhánh đầu thu.
- [ ] **Step 4: Chạy lại toàn bộ.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): snapshots and live view for directly connected cameras`

### Task 5: Dò camera trong mạng

**Files:**
- Create: `camscan.go`, `camscan_test.go`

**Interfaces:**
- Cấu hình: nhóm `camera`, khoá `lan_cidr` (ví dụ `192.168.1.0/24`). Trống thì không dò được và báo "Nhập dải mạng của nhà, ví dụ 192.168.1.0/24".
- Produces:
  - `POST /api/cameras/scan`: bắt đầu quét ở nền, trả 202. Đang quét thì trả 409.
  - `GET /api/cameras/scan` → `{"running": bool, "found": [{"host","port","kind","model"}], "error": ""}`.
  - Với mỗi địa chỉ trong dải và mỗi cổng 80, 8080, 2020 trả lời TCP trong 400 ms:
    - **ONVIF:** `POST http://host:port/onvif/device_service` với SOAP `GetSystemDateAndTime` (ONVIF cho phép gọi không cần đăng nhập). Trả SOAP hợp lệ thì `kind: "onvif"`.
    - **Hikvision:** `GET http://host:port/ISAPI/System/deviceInfo`. Trả 401 có `WWW-Authenticate: Digest`, hoặc 200 có `<DeviceInfo>`, thì `kind: "hik_cam"`. Gặp cả hai thì ưu tiên `hik_cam`.
  - Bỏ các địa chỉ đã có trong `cameras` và địa chỉ đầu thu.
  - Không gửi tài khoản nào khi quét.
- Để test được: danh sách địa chỉ và hàm kết nối là biến tiêm được.

- [ ] **Step 1: Viết test đỏ**, dùng máy chủ giả trên `127.0.0.1` thay cho dải thật:
  - `TestCamScanFindsOnvifAndHikvision`: một máy giả ONVIF, một máy giả Hikvision, một máy chỉ mở cổng mà không phải camera → `found` có đúng 2, đúng `kind`.
  - `TestCamScanSkipsKnownHosts`: camera đã lưu và đầu thu không hiện lại.
  - `TestCamScanRejectsWideRange` (Review Focus 3): `192.168.0.0/16` và `10.0.0.0/8` bị từ chối kèm lời giải thích; `/24`, `/28` được nhận.
  - `TestCamScanSendsNoCredentials`: máy giả ghi lại mọi header; không có `Authorization` nào.
  - `TestCamScanConcurrencyAndTimeout`: dùng hàm kết nối tiêm, đếm số kết nối đồng thời (≤ 32) và kiểm từng kết nối có thời hạn 400 ms.
- [ ] **Step 2: Chạy.** Mong đợi: FAIL.
- [ ] **Step 3: Viết `camscan.go`.** Kết quả lần quét gần nhất giữ trong RAM.
- [ ] **Step 4: Chạy lại.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): find cameras on the home network`

### Task 6: API camera nối thẳng và thử kết nối

**Files:**
- Modify: `camstore.go` (thêm `mountCameras(mux, cams, auth)`), `main.go`
- Test: `camstore_test.go`

**Interfaces:**
- Consumes: Task 1–5.
- Produces (mọi route qua `a.Require`, không nhận token trợ lý):
  - `GET /api/cameras` → `{"cameras": [view…], "templates": camTemplates}`;
  - `PUT /api/cameras` với body là một camera (id rỗng = thêm mới) → camera đã lưu (`view`); sai thì 400 kèm lỗi; lưu xong gọi `syncCamGen`, đổi thì khởi động lại như trang đầu thu;
  - `DELETE /api/cameras/{id}` → 204, hoặc 404;
  - `POST /api/cameras/test` với `{host, http_port, rtsp_port, user, pass, kind, id?}`:
    - `pass` rỗng mà có `id` thì dùng mật khẩu đã lưu, **chỉ khi host không đổi**;
    - `hik_cam`: đọc `/ISAPI/System/deviceInfo` bằng digest → `{"ok": true, "model", "firmware"}`;
    - `rtsp`: kết nối TCP tới cổng RTSP trong 2 giây → `{"ok": true}`;
    - lỗi thì `{"ok": false, "message": "<câu tiếng Việt>"}` (sai mật khẩu, tạm khoá 15 phút, không kết nối được). Không có địa chỉ hay mật khẩu trong câu báo.

- [ ] **Step 1: Viết test đỏ:**
  - `TestCamerasAPI`: thêm, sửa, xoá qua API. `GET` không bao giờ chứa mật khẩu. Không có cookie thì 401; token trợ lý thì 401.
  - `TestCamerasTestKeepsSecretWithHost`:
    - thử với `id` và `pass` rỗng → dùng mật khẩu đã lưu;
    - đổi host mà `pass` rỗng → không gửi mật khẩu cũ tới host mới (camera giả ghi lại header).
  - `TestCamerasTestMessages`: sai mật khẩu, bị khoá, không kết nối được → đúng câu, không lộ host hay mật khẩu.
  - `TestBackupExcludesCameraPasswords`: `GET /api/backup` không chứa mật khẩu camera.
- [ ] **Step 2: Chạy.** Mong đợi: FAIL.
- [ ] **Step 3: Viết route và nối trong `main.go`.**
- [ ] **Step 4: Chạy lại toàn bộ.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): API to add, test and remove direct cameras`

### Task 7: Báo thiết bị mất kết nối (loại tin "Thiết bị")

**Files:**
- Create: `offline.go`, `offline_test.go`
- Modify: `notify.go` (`GET /api/notify` thêm loại có sẵn `thiet_bi`), `main.go`, `camera.go` (kiểm camera nối thẳng còn sống)

**Interfaces:**
- Consumes: `Hub.notify(level, kind, text)` (Phần 1), trạng thái `online:<id>` có sẵn của hub.
- Produces:
  - **Kiểm camera nối thẳng:** mỗi 60 giây, mỗi camera một kết nối TCP tới cổng RTSP trong 3 giây. Kết quả ghi vào trạng thái `online:<id>` (`online`/`offline`), như thiết bị có `online_topic`.
  - **Theo dõi mất kết nối:** mỗi phút xem mọi thiết bị có trạng thái `online:<id>`:
    - offline liên tục quá **10 phút** → `h.notify("warn", "thiet_bi", "<Tên> mất kết nối hơn 10 phút.")`, một lần cho mỗi lần mất;
    - online trở lại sau khi đã báo → `h.notify("info", "thiet_bi", "<Tên> đã kết nối lại.")`.
  - `GET /api/notify` có thêm `{"kind": "thiet_bi", "texts": ["Thiết bị mất kết nối quá 10 phút"]}`, để trang Đọc thông báo hiện dòng này với ô "Đọc" và ô "Telegram" như các loại khác.
  - Đồng hồ tiêm được; test không ngủ thật.

- [ ] **Step 1: Viết test đỏ:**
  - `TestOfflineNotifiesAfter10Minutes`: offline 9 phút → không báo; 10 phút 1 giây → 1 tin; vẫn offline thêm 1 giờ → không báo thêm; online lại → tin "đã kết nối lại"; mất lần nữa → báo lại sau 10 phút.
  - `TestOfflineShortBlipsIgnored`: mất 5 phút rồi về → không có tin nào.
  - `TestNotifyKindsIncludeThietBi`: `GET /api/notify` có `thiet_bi`. Tắt `tg:thiet_bi` thì không có tin Telegram, nhưng vẫn đọc thành tiếng.
  - `TestDirectCamOnlineProbe`: camera giả nghe TCP → `online:<id>` = `online`; đóng cổng → `offline`.
- [ ] **Step 2: Chạy.** Mong đợi: FAIL.
- [ ] **Step 3: Viết `offline.go` và bước kiểm camera.**
- [ ] **Step 4: Chạy lại toàn bộ.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): report devices offline for more than 10 minutes`

### Task 8: Cài đặt → Camera: thêm camera nối thẳng

**Files:**
- Modify:
  - `web/setup-camera.js`: thêm phần "Camera nối thẳng". Phần đầu thu giữ nguyên, đổi tiêu đề phụ thành "Đầu ghi (cách cũ)";
  - `web/index.html`: tăng `?v=` mọi thẻ.
- Test: `web_selectors_test.go`, cộng kiểm cú pháp JS bằng QuickJS trên hub (`scripts/test_voice_js.py` đã có sẵn cách chạy).

**Interfaces:**
- Consumes: route ở Task 5–6, `camTemplates`.
- Nội dung:
  - ô "Dải mạng của nhà" (`camera.lan_cidr`, nút Lưu) và nút "Tìm camera". Đang quét thì hiện đang tìm; xong thì hiện danh sách "Tìm thấy camera mới" (IP, kiểu, model nếu có);
  - bấm một camera tìm thấy (hoặc "Thêm tay") → form: kiểu (Hikvision; hoặc RTSP với mẫu hãng Dahua/KBVision/Imou, Tapo/VIGI, Yoosee, Khác), IP, tài khoản, mật khẩu (bí mật, chỉ hiện "đã lưu"), tên, khu, các nút "Thử kết nối" và "Lưu";
  - danh sách camera đã thêm: tên, khu, kiểu, trạng thái kết nối, nút Sửa và Xoá (xoá hỏi lại);
  - lưu xong hiện "Hub đang khởi động lại để cập nhật camera" như phần đầu thu;
  - ô "Chế độ ghi" trong form (mục 2 điểm 2 của thiết kế) thêm ở kế hoạch 2B, khi đã có ghi hình;
  - chế độ dữ liệu mẫu không gọi API;
  - mọi chữ lấy từ camera hay từ mạng (model, tên) phải qua `U.esc`;
  - mật khẩu không đi qua bản nháp chung của trang (`data-sfield`). Chỉ đọc lúc bấm, gửi một lần rồi xoá khỏi ô.

- [ ] **Step 1: Viết test đỏ** `TestCameraSetupWired`:
  - `setup-camera.js` gọi `/api/cameras`, `/api/cameras/scan`, `/api/cameras/test`;
  - ô mật khẩu camera không có `data-sfield`;
  - `index.html` cùng một `?v=`.
- [ ] **Step 2: Chạy.** Mong đợi: FAIL.
- [ ] **Step 3: Viết giao diện.** Kiểm cú pháp `setup-camera.js` bằng QuickJS trên hub.
- [ ] **Step 4: Chạy lại toàn bộ.** Mong đợi: PASS.
- [ ] **Step 5: Commit** `feat(camera): settings to find and add direct cameras`

### Task 9: Chạy thật trên hub

**Files:**
- `README.md` (mục Camera: thêm camera nối thẳng, dải mạng, mẫu hãng);
- thiết kế chính mục 15.1 (trỏ tới phần đã làm).

- [ ] **Step 1: Triển khai lên hub** theo quy trình bundle, `docker compose up -d --build hubd`.
- [ ] **Step 2: Kiểm giao diện** bằng trình duyệt ở chế độ dữ liệu mẫu. Console không có lỗi.
- [ ] **Step 3: Cùng chủ nhà:**
  - nhập dải mạng, bấm "Tìm camera";
  - chủ nhà chọn một camera, tự nhập mật khẩu camera (Claude không nhập), đặt tên, chọn khu, thử kết nối, lưu;
  - kiểm ảnh chụp và xem trực tiếp trên dashboard.

  Nếu camera trong nhà còn nằm sau cổng PoE của đầu thu (không quét thấy trong LAN), ghi lại và dừng ở bước này, báo chủ nhà.
- [ ] **Step 4: Đo** RAM và CPU của `hubd` trước và sau khi thêm camera (không ghi hình, chỉ kiểm sống mỗi 60 giây); thời gian quét một dải /24.
- [ ] **Step 5: Commit tài liệu** `docs(camera): add directly connected cameras`
