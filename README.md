# hubd — trung tâm điều khiển nhà

Máy Ubuntu `192.168.1.254` chạy 4 container (Docker Compose): `hub-mosquitto` (MQTT), `hubd` (web + lịch sử + luật), `tuya-bridge` (Tuya ⇄ MQTT), `voiced` (trợ lý giọng nói, tuỳ chọn). Màn hình = iPad mở `http://192.168.1.254:8080`.

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

## Luật tự động

Khai báo trong `config.yaml` mục `rules:` (xem ví dụ trong file): `above`/`below`, `clear_below`/`clear_above` (chống dao động), `for` (giữ liên tục). Thiết bị `protected: true` không bao giờ bị luật tắt.

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

Thêm vào `.env`: `HUBD_TLS_CERT=/tls/hub.crt`, `HUBD_TLS_KEY=/tls/hub.key`, và `ANTHROPIC_API_KEY=...` nếu muốn hỏi câu tự do (trống = chỉ lệnh cố định). Mở `ufw` cho LAN vào cổng `8443`. Sau đó `docker compose up -d --build`. Build `voiced` lần đầu tải mô hình (~250 MB) vào image, sau đó chạy không cần internet.

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
go test ./...                                   # rules, lịch sử, PIN, API
cd tuya-bridge && python3 -m unittest discover  # bridge với thiết bị giả
cd voiced && python3 -m unittest                # trợ lý: khớp lệnh, chặn PIN, vòng tool use (Claude giả); cần pip install anthropic numpy
```

## Chưa làm / giới hạn

- Chưa kiểm với thiết bị Tuya thật (chờ `devices.json`).
- Telegram, báo cháy CBX: chưa làm. Cảnh báo `urgent` chưa lặp lại tới khi xác nhận.
- Cổng 1883 và 8080 mở trong LAN; không mở ra internet. PIN dùng chung, khoá 1 phút sau 5 lần sai.
