# hubd — trung tâm điều khiển nhà

Máy Ubuntu `192.168.1.254` chạy 3 container (Docker Compose): `hub-mosquitto` (MQTT), `hubd` (web + lịch sử + luật), `tuya-bridge` (Tuya ⇄ MQTT); thêm `hub-go2rtc` và `hikbridge` khi bật camera (mục Camera Hikvision). Màn hình = iPad mở `http://192.168.1.254:8080`.

## Chạy lần đầu

```bash
cd /root/hubd
scripts/init-secrets.sh          # tạo .env (PIN + mật khẩu MQTT) và mosquitto/passwd
grep HUBD_PIN .env               # PIN đăng nhập giao diện
docker compose up -d --build
```

iPad: Safari → mở địa chỉ trên → nhập PIN → Chia sẻ → **Thêm vào Màn hình chính** (chạy toàn màn hình).

## Kết nối thiết bị Tuya

1. Lấy `devices.json` bằng đăng nhập QR của app, không cần Tuya IoT Platform: `~/tuya-venv/bin/python scripts/tuya_qr_login.py` (chạy bằng root), nhập User Code (Smart Life → Tôi → ⚙️ → Tài khoản và bảo mật), quét QR trong app. Script tự điền IP + version từ broadcast LAN và ghi `tuya/devices.json`. Xem IP thiết bị: thêm `--scan`. Thư mục `tuya/` **không commit** (chứa local key).
2. Nếu thiết bị thiếu IP (quét UDP bị ufw chặn), tạo `tuya/overrides.json`:
   ```json
   {"<device id>": {"ip": "192.168.1.11", "version": "3.4", "dp": "1"}}
   ```
   `dp` = số công tắc chính (mặc định 1; đèn thường 20). Bridge tự nạp lại khi file đổi, không cần restart.
3. Sinh danh sách thiết bị cho hubd rồi khởi động lại hubd:
   ```bash
   python3 tuya-bridge/gen_config.py      # chỉ sửa đoạn BEGIN/END tuya-devices trong config.yaml
   docker compose restart hubd
   ```
4. Gán khu vực / khoá bảo vệ / đổi tên: thêm vào `tuya/overrides.json` rồi sinh lại (bước 3):
   `{"<device id>": {"zone": "khu1", "protected": true, "name": "Tủ lạnh"}}` (gộp được với `ip`, `version`, `dp`).

Thiếu `devices.json` thì bridge chỉ ghi log "chưa có thiết bị" và chờ — không lỗi.

Topic: `tuya/<id>/state` (ON/OFF), `tuya/<id>/dps` (JSON), `tuya/<id>/online`, lệnh gửi vào `tuya/<id>/cmd` (`ON`/`OFF`/`TOGGLE`/`{"2":true}`).

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

## Vận hành

| Việc | Lệnh |
|---|---|
| Xem log | `docker compose logs -f hubd tuya-bridge` |
| Sau khi sửa `config.yaml` | `docker compose restart hubd` |
| Cập nhật code | `docker compose up -d --build` |
| Sao lưu lịch sử | `docker run --rm -v hubd_hubd-db:/db -v $PWD:/b alpine cp /db/hubd.db /b/` |
| Đổi PIN | sửa `HUBD_PIN` trong `.env` rồi `docker compose up -d` |

Thử MQTT: `mosquitto_pub -h 192.168.1.254 -u node -P <NODE_MQTT_PASS trong .env> -t demo/nhietdo -m 27.5`.

## Kiểm thử

```bash
go test ./...                                   # rules, lịch sử, PIN, API, camera, hikbridge
cd tuya-bridge && python3 -m unittest discover  # bridge với thiết bị giả
```

## Chưa làm / giới hạn

- Chưa kiểm với thiết bị Tuya thật (chờ `devices.json`).
- Telegram, kịch bản một chạm, báo cháy CBX: chưa làm.
- Cổng 1883 và 8080 mở trong LAN; không mở ra internet. PIN dùng chung, khoá 1 phút sau 5 lần sai.
