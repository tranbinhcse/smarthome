# hubd — trung tâm điều khiển nhà

Máy Ubuntu `192.168.1.254` chạy 3 container (Docker Compose): `hub-mosquitto` (MQTT), `hubd` (web + lịch sử + luật), `tuya-bridge` (Tuya ⇄ MQTT). Màn hình = iPad mở `http://192.168.1.254:8080`.

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
go test ./...                                   # rules, lịch sử, PIN, API
cd tuya-bridge && python3 -m unittest discover  # bridge + gen_config với thiết bị giả
cd scripts && python3 -m unittest test_tuya_qr_login  # mapping từ đăng nhập QR
```

## Chưa làm / giới hạn

- Cảm biến Tuya Wi-Fi chạy pin thường ngủ, không nhận kết nối local: chỉ cảm biến cắm điện mới đọc được ổn định.
- Telegram, kịch bản một chạm, báo cháy CBX: chưa làm.
- Cổng 1883 và 8080 mở trong LAN; không mở ra internet. PIN dùng chung, khoá 1 phút sau 5 lần sai.
