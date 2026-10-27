# hubd — trung tâm điều khiển nhà

Máy Ubuntu `192.168.1.254` chạy 3 container (Docker Compose): `hub-mosquitto` (MQTT), `hubd` (web + lịch sử + luật), `tuya-bridge` (Tuya ⇄ MQTT); thêm `hub-go2rtc` và `hikbridge` khi bật camera (mục Camera Hikvision), `voiced` khi bật trợ lý giọng nói (tuỳ chọn). Màn hình = iPad mở `http://192.168.1.254:8080`.

## Chạy lần đầu

```bash
cd /root/hubd
scripts/init-secrets.sh          # tạo .env (PIN + mật khẩu MQTT) và mosquitto/passwd
grep HUBD_PIN .env               # PIN đăng nhập giao diện
docker compose up -d --build
```

iPad: Safari → mở địa chỉ trên → nhập PIN → Chia sẻ → **Thêm vào Màn hình chính** (chạy toàn màn hình).

## Kết nối thiết bị Tuya

1. Lấy `devices.json` bằng đăng nhập QR của app, không cần Tuya IoT Platform: `~/tuya-venv/bin/python scripts/tuya_qr_login.py` (chạy bằng root), nhập User Code (Smart Life → Tôi → ⚙️ → Tài khoản và bảo mật), quét QR trong app. Script tự điền IP + version từ broadcast LAN và ghi `tuya/devices.json`, kèm `mapping` (dp → code, scale) của từng thiết bị. Xem IP thiết bị: thêm `--scan`. Thư mục `tuya/` **không commit** (chứa local key).
   **Thêm thiết bị mới trong app Smart Life:** chạy lại bước 1 rồi bước 3, không cần sửa tay.
2. Nếu thiết bị thiếu IP (quét UDP bị ufw chặn), tạo `tuya/overrides.json`:
   ```json
   {"<device id>": {"ip": "192.168.1.11", "version": "3.4", "dp": "1"}}
   ```
   `dp` = số công tắc chính (mặc định 1; đèn thường 20). Bridge tự nạp lại khi file đổi, không cần restart.
3. Sinh danh sách thiết bị + số đo cho hubd rồi khởi động lại hubd:
   ```bash
   python3 tuya-bridge/gen_config.py      # chỉ sửa đoạn BEGIN/END tuya-devices và tuya-entities trong config.yaml
   docker compose restart hubd
   ```
   Có `mapping` thì tự làm:
   - loại thiết bị theo category Tuya (`kg` → light, `pc`/`cz` → socket, `dlq` → breaker, `fs` → fan, `cl` → curtain, `wsdcg`… → sensor);
   - công tắc nhiều nút / ổ nhiều lỗ tách thành từng kênh `tuya_<id>_<dp>` (gom bằng `group`, lệnh JSON `{"<dp>": true}`);
   - số đo W, V, A, kWh của CB/ổ đo điện và nhiệt độ, độ ẩm, CO₂, PM2.5 của cảm biến Wi-Fi, đúng `field`/`scale`.
   Chưa có `mapping` (devices.json cũ): mỗi thiết bị một công tắc; CB `dlq` vẫn có W/V/A theo dp chuẩn 18/19/20.
4. Gán khu vực / khoá bảo vệ / đổi tên: thêm vào `tuya/overrides.json` rồi sinh lại (bước 3):
   `{"<device id>": {"zone": "khu1", "protected": true, "name": "Tủ lạnh", "area": "cafe", "names": {"2": "Sục khí"}}}`
   (gộp được với `ip`, `version`, `dp`). `area` = khu mà CB cấp điện (số đo W hiện ở khu đó thay vì tủ điện), `names` = tên từng kênh.

Thiếu `devices.json` thì bridge chỉ ghi log "chưa có thiết bị" và chờ — không lỗi.

Topic: `tuya/<id>/state` (ON/OFF), `tuya/<id>/dps` (JSON), `tuya/<id>/online`, `tuya/<id>/energy` (kWh cộng dồn, JSON theo dp), lệnh gửi vào `tuya/<id>/cmd` (`ON`/`OFF`/`TOGGLE`/`{"2":true}`).

CB/ổ đo điện không tự báo W/V/A và `status()` trả số cũ, nên bridge gửi `updatedps` mỗi 10 giây. Thiết bị không trả lời gì (kể cả heartbeat) quá 30 giây thì báo `offline` và nối lại. Tổng kWh (cộng các lần thiết bị báo `add_ele`) lưu ở volume `tuya-state`.

## Node ESP32 (cảm biến + hồng ngoại)

Firmware ESPHome trong `firmware/`, cách nạp, tài khoản MQTT riêng từng node, học mã hồng ngoại: [firmware/README.md](firmware/README.md).
Trang Cài đặt → Node ESP32: danh sách node, tạo/khoá/xoá tài khoản MQTT của node (cần bật một lần: `scripts/init-secrets.sh`
rồi `docker compose up -d mosquitto hubd`, xem mục Tài khoản MQTT của node), chọn hãng máy lạnh, gán mã hồng ngoại vừa học.
`POST /api/cmd` nhận thêm mã nút hồng ngoại (`{"device":"cafe_ml","to":"temp_24"}`) cho thiết bị `kind` `tv`/`speaker`/`fan`/`ac`; hub giữ trạng thái giả định vì hồng ngoại một chiều.
Giả lập node chưa lắp (trỏ vào broker thử, không phải trung tâm thật): `PUB="mosquitto_pub -h <broker> -u <user> -P <mật khẩu>" scripts/sim-nodes.sh`.

## Camera Hikvision

Ba phần, bật bằng profile `camera` (lõi chạy đủ khi tắt):

- `go2rtc` (`go2rtc/go2rtc.yaml`): RTSP luồng phụ H.264 của đầu ghi (`/Streaming/Channels/<kênh>02`) sang WebRTC/MSE, không chuyển mã. API chỉ nằm trong mạng Docker; cổng mở duy nhất là `8555` (media WebRTC).
- `hikbridge`: đọc `/ISAPI/Event/notification/alertStream` của đầu ghi, gửi MQTT (bảng dưới); hỏi trạng thái kênh 30 giây một lần.
- `hubd`: `/api/cam/<id>/snapshot` (ảnh ISAPI `/ISAPI/Streaming/channels/<kênh>01/picture`) và `/api/cam/<id>/ws` (WebSocket của go2rtc), đều sau PIN. Trình duyệt không thấy IP hay tài khoản đầu ghi.

Bật lần đầu:

1. Trên đầu ghi: tạo tài khoản riêng, chỉ có quyền xem (xem trực tiếp từ xa các kênh), mật khẩu chỉ gồm chữ và số (nằm trong URL RTSP). Bật ISAPI nếu đang tắt (Mạng → Nâng cao → Giao thức tích hợp). Luồng phụ của từng camera để H.264 (không H.265, tắt H.264+).
2. Thêm tài khoản MQTT `hik` (cài đặt mới thì `scripts/init-secrets.sh` đã tạo sẵn):
   ```bash
   HIK=$(head -c 24 /dev/urandom | base64 | tr -dc A-Za-z0-9 | head -c 20)
   docker run --rm -v "$PWD/mosquitto:/m" eclipse-mosquitto:2 mosquitto_passwd -b /m/passwd hik "$HIK"
   printf 'HIK_MQTT_USER=hik\nHIK_MQTT_PASS=%s\n' "$HIK" >> .env
   docker compose kill -s SIGHUP mosquitto   # nạp lại file mật khẩu, không ngắt kết nối
   ```
3. Điền `NVR_HOST` (chỉ IP), `NVR_USER`, `NVR_PASS` vào `.env`, thêm dòng `COMPOSE_PROFILES=camera`.
4. `config.yaml`: sửa khối camera (kênh → camera → khu) cho đúng rồi bỏ `#`; thêm entity sự kiện nếu cần cho luật.
5. `docker compose up -d --build`, xem `docker compose logs -f hikbridge` có dòng `đã nối alertStream`.

| Topic | Giá trị |
|---|---|
| `hik/<kênh>/<sự kiện>` | `ON`/`OFF` (retained). Sự kiện = `eventType` viết thường: `vmd` chuyển động, `linedetection` vượt hàng rào ảo, `fielddetection` xâm nhập vùng, `shelteralarm` che camera, `videoloss` mất hình |
| `hik/<kênh>/human`, `hik/<kênh>/vehicle` | `ON`/`OFF` khi camera (dòng AcuSense) báo loại mục tiêu |
| `hik/<kênh>/online` | `online`/`offline` (retained), dùng làm `online_topic` của camera |
| `hik/bridge/status` | `online`/`offline` (LWT) |

Theo mô tả của cộng đồng (chưa kiểm với đầu ghi của nhà), đầu ghi gửi `active` lặp lại khi còn chuyển động và có thể không gửi `inactive`, nên `hikbridge` tự về `OFF` sau `HIK_HOLD` (mặc định 10 giây) không có tin mới. hubd đọc `ON`/`OFF` thành 1/0: luật dùng `above: 0.5`.

Tải đo được (đầu ghi giả, luồng H.264 704×576 ~510 kbit/s): 10 kênh, mỗi kênh 1 người xem qua hubd tốn go2rtc 2,2% + hubd 1,4% của một luồng CPU; chi tiết ở mục 15.1 tài liệu thiết kế.

Đầu ghi từ chối mật khẩu thì hubd và hikbridge tạm ngừng gửi mật khẩu 15 phút để đầu ghi không khoá IP của trung tâm; sửa `.env` rồi `docker compose up -d` để thử lại ngay. Không mở cổng đầu ghi ra internet; xem từ xa qua WireGuard (MSE đi qua hubd nên chạy được; WebRTC cần tới cổng 8555).

## Luật tự động

Khai báo trong `config.yaml` mục `rules:` (xem ví dụ trong file): `above`/`below`, `clear_below`/`clear_above` (chống dao động), `for` (giữ liên tục). Thiết bị `protected: true` không bao giờ bị luật tắt.
Luật có `alert: "lời cảnh báo"` (không cần `then`) hiện ở chuông dashboard khi bật, tự nhả khi qua ngưỡng `clear_*`; đang dùng cho pin JK (`pin_*`, xem `firmware/jk-bms/README.md`).

## Trợ lý giọng nói (`voiced`)

Thiết kế: mục 15.2 của `docs/2026-10-26-trung-tam-dieu-khien-nha-design.md`. Container thứ 4, tắt đi thì lõi vẫn chạy đủ.

- **Nói:** nút micro trên thanh trên của dashboard. Chạm để nghe, chạm lần nữa (hoặc sau 8 giây) để gửi. Âm thanh đi qua hubd `/voice/*` (cần đăng nhập PIN) tới `voiced`, không mở cổng nào khác.
- **Hiểu:** chuyển giọng nói thành chữ ngay trên máy Ubuntu (sherpa-onnx, Zipformer tiếng Việt). Lệnh cố định khớp theo tên trong `config.yaml`, chạy cả khi mất internet: "bật đèn kho", "tắt đèn phòng lạnh", "ra khỏi nhà" (tên kịch bản), "pin còn bao nhiêu", "đèn kho đang bật không", "tắt báo động". Câu khác gửi Claude (`claude-opus-5-5`, effort thấp) kèm tool `get_state`, `query_history`, `set_device`, `run_scene`, `ack_alarm`.
- **Đọc:** VieNeu-TTS giọng Hải Đăng, chạy cục bộ. Cảnh báo `notify` của luật được publish lên MQTT `hubd/alert`; `voiced` đọc lên iPad đang mở dashboard. Câu cảnh báo cố định được tạo sẵn âm thanh lúc khởi động.
- **Quyền:** `voiced` gọi hubd bằng `HUBD_ASSISTANT_TOKEN`, chỉ qua `/api/assistant/*`, quyền như người chưa nhập PIN. hubd từ chối (403) lệnh tới thiết bị `pin: true`/`protected: true` và kịch bản `pin: true` hoặc có bước đụng các thiết bị đó. PIN của iPad không truyền sang trợ lý.

Bật lần đầu (máy đã có `.env`):

```bash
scripts/init-secrets.sh            # thêm HUBD_ASSISTANT_TOKEN + tài khoản MQTT voiced, không đổi khoá cũ
docker compose restart mosquitto   # nạp tài khoản MQTT mới
scripts/tls-selfsigned.sh          # Safari chỉ cho dùng micro trên HTTPS; tạo tls/ + tls-ca/ (không commit)
```

Thêm vào `.env`: `HUBD_TLS_CERT=/tls/hub.crt`, `HUBD_TLS_KEY=/tls/hub.key`, và `ANTHROPIC_API_KEY=...` nếu muốn hỏi câu tự do (trống = chỉ lệnh cố định). Mở `ufw` cho LAN vào cổng `8443`. Bật dịch vụ bằng profile `voice`: thêm `voice` vào `COMPOSE_PROFILES` trong `.env` (vd. `COMPOSE_PROFILES=voice` hoặc `camera,voice`). Sau đó `docker compose up -d --build`. Build `voiced` lần đầu tải mô hình (~250 MB) vào image, sau đó chạy không cần internet.

iPad: cài `tls-ca/ca.crt` (AirDrop → Cài đặt → Đã tải về hồ sơ; rồi Cài đặt chung → Giới thiệu → Cài đặt tin cậy chứng nhận → bật "iNIX Home CA"), mở `https://192.168.1.254:8443`, đăng nhập PIN. Chạm màn hình một lần sau khi mở để iOS cho phép tự đọc cảnh báo.

Kịch bản khai báo ở mục `scenes:` của `config.yaml`, câu cảnh báo ở `notify:` của luật (xem ví dụ trong file).

| API trợ lý (Bearer `HUBD_ASSISTANT_TOKEN`) | |
|---|---|
| `GET /api/assistant/config` | khu, số đo, thiết bị, kịch bản, `locked` (id cần PIN), `phrases` (câu cảnh báo cố định) |
| `GET /api/assistant/state` | `values` như `/api/state`, `alerts` chưa xác nhận |
| `GET /api/assistant/history?id=&hours=` | như `/api/history` |
| `POST /api/assistant/cmd` `{device, to}` | 204; 403 khi cần PIN |
| `POST /api/assistant/scene/{id}` | 204; 403 khi cần PIN; 502 khi có bước không gửi được |
| `POST /api/assistant/alarm/ack` | xác nhận cảnh báo, publish `hubd/alert/ack` |

## Vận hành

| Việc | Lệnh |
|---|---|
| Xem log | `docker compose logs -f hubd tuya-bridge voiced` |
| Sau khi sửa `config.yaml` | `docker compose restart hubd` |
| Cập nhật code | `docker compose up -d --build` |
| Sao lưu lịch sử | `docker run --rm -v hubd_hubd-db:/db -v $PWD:/b alpine cp /db/hubd.db /b/` |
| Đổi PIN | sửa `HUBD_PIN` trong `.env` rồi `docker compose up -d` |

Thử MQTT: `mosquitto_pub -h 192.168.1.254 -u node -P <NODE_MQTT_PASS trong .env> -t demo/nhietdo -m 27.5`.

## Kiểm thử

```bash
go test ./...                                   # rules, lịch sử, PIN, API, firmware, camera, hikbridge, trợ lý
cd tuya-bridge && python3 -m unittest discover  # bridge + gen_config với thiết bị giả
python3 -m unittest discover -s scripts         # đăng nhập QR Tuya, giả lập node năng lượng
cd voiced && python3 -m unittest                # trợ lý: khớp lệnh, chặn PIN, vòng tool use (Claude giả); cần pip install anthropic numpy
```

Inverter (Deye, LuxPower, Lumentree) đọc qua RS485 bằng node ESPHome: xem [firmware/inverter/README.md](firmware/inverter/README.md).

## Chưa làm / giới hạn

- Cảm biến Tuya Wi-Fi chạy pin thường ngủ, không nhận kết nối local: chỉ cảm biến cắm điện mới đọc được ổn định.
- Telegram, kịch bản một chạm, báo cháy CBX: chưa làm. Cảnh báo `urgent` chưa lặp lại tới khi xác nhận.
- Cổng 1883 và 8080 mở trong LAN; không mở ra internet. PIN dùng chung, khoá 1 phút sau 5 lần sai.
