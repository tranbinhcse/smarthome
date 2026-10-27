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
`POST /api/cmd` nhận thêm mã nút hồng ngoại (`{"device":"cafe_ml","to":"temp_24"}`) cho thiết bị `kind` `tv`/`speaker`/`fan`/`ac`; hub giữ trạng thái giả định vì hồng ngoại một chiều.
Giả lập node chưa lắp (trỏ vào broker thử, không phải trung tâm thật): `PUB="mosquitto_pub -h <broker> -u <user> -P <mật khẩu>" scripts/sim-nodes.sh`.

## Camera Hikvision

Ba phần, bật bằng profile `camera` (lõi chạy đủ khi tắt):

- `go2rtc` (`go2rtc/go2rtc.yaml`): RTSP luồng phụ H.264 của đầu ghi (`/Streaming/Channels/<kênh>02`) sang WebRTC/MSE, không chuyển mã. Không giữ tài khoản: mỗi lần mở luồng hubd gửi nguồn RTSP kèm tên `ch<kênh>`. API chỉ nằm trong mạng Docker; cổng mở duy nhất là `8555` (media WebRTC).
- `hikbridge`: đọc `/ISAPI/Event/notification/alertStream` của đầu ghi, gửi MQTT (bảng dưới); hỏi trạng thái kênh 30 giây một lần. Lấy cấu hình đầu ghi từ hubd (`/api/service/nvr`, token `HUBD_CAMERA_TOKEN`); nhận `hubd/settings/nvr` = `changed` thì thoát để Docker chạy lại và nạp.
- `hubd`: `/api/cam/<id>/snapshot` (ảnh ISAPI `/ISAPI/Streaming/channels/<kênh>01/picture`) và `/api/cam/<id>/ws` (WebSocket của go2rtc), đều sau PIN. Trình duyệt không thấy IP hay tài khoản đầu ghi; mật khẩu lưu trên trang Cài đặt chỉ hiện là "đã lưu".

Bật lần đầu:

1. Trên đầu ghi: tạo tài khoản riêng, chỉ có quyền xem (xem trực tiếp từ xa các kênh), mật khẩu chỉ gồm chữ và số (nằm trong URL RTSP). Bật ISAPI nếu đang tắt (Mạng → Nâng cao → Giao thức tích hợp). Luồng phụ của từng camera để H.264 (không H.265, tắt H.264+).
2. Thêm tài khoản MQTT `hik` (cài đặt mới thì `scripts/init-secrets.sh` đã tạo sẵn):
   ```bash
   HIK=$(head -c 24 /dev/urandom | base64 | tr -dc A-Za-z0-9 | head -c 20)
   docker run --rm -v "$PWD/mosquitto:/m" eclipse-mosquitto:2 mosquitto_passwd -b /m/passwd hik "$HIK"
   printf 'HIK_MQTT_USER=hik\nHIK_MQTT_PASS=%s\n' "$HIK" >> .env
   docker compose kill -s SIGHUP mosquitto   # nạp lại file mật khẩu, không ngắt kết nối
   ```
3. `.env`: thêm dòng `COMPOSE_PROFILES=camera`; `.env` cũ chưa có `HUBD_CAMERA_TOKEN` thì chạy lại `scripts/init-secrets.sh` (chỉ thêm token, không đổi khoá cũ). `docker compose up -d --build`.
4. Dashboard → Cài đặt → **Camera (đầu ghi Hikvision)**: nhập IP đầu ghi, tài khoản chỉ xem, mật khẩu; bấm **Thử kết nối** để xem các kênh đầu ghi trả về; chọn khu cho từng kênh (để "Không dùng" nếu bỏ qua), sửa tên hiển thị nếu muốn; **Lưu**. hubd ghi thiết bị `cam_ch<kênh>` vào `HUBD_GEN_DIR/camera.yaml` rồi tự khởi động lại; tải lại trang để thấy camera. Nút gạt **Bật camera** tắt/bật cả tích hợp. Đổi IP đầu ghi thì phải nhập lại mật khẩu: hubd bỏ mật khẩu đã lưu để không gửi nó tới máy khác.
5. Xem `docker compose logs -f hikbridge` có dòng `đã nối alertStream`. Thêm entity sự kiện trong `config.yaml` nếu cần cho luật (thiết bị camera sinh ra có id `cam_ch<kênh>`).

Cách cũ vẫn chạy: trang Cài đặt chưa có IP đầu ghi thì hubd dùng `NVR_HOST`/`NVR_USER`/`NVR_PASS` trong `.env`; `HUBD_CAMERA_TOKEN` trống thì hikbridge đọc thẳng `NVR_*`. Khối camera để `#` trong `config.yaml` cũng vẫn bỏ `#` dùng được, nhưng đừng gán cùng kênh ở cả hai nơi (sẽ ra hai thẻ).

| Topic | Giá trị |
|---|---|
| `hik/<kênh>/<sự kiện>` | `ON`/`OFF` (retained). Sự kiện = `eventType` viết thường: `vmd` chuyển động, `linedetection` vượt hàng rào ảo, `fielddetection` xâm nhập vùng, `shelteralarm` che camera, `videoloss` mất hình |
| `hik/<kênh>/human`, `hik/<kênh>/vehicle` | `ON`/`OFF` khi camera (dòng AcuSense) báo loại mục tiêu |
| `hik/<kênh>/online` | `online`/`offline` (retained), dùng làm `online_topic` của camera |
| `hik/bridge/status` | `online`/`offline` (LWT) |

Theo mô tả của cộng đồng (chưa kiểm với đầu ghi của nhà), đầu ghi gửi `active` lặp lại khi còn chuyển động và có thể không gửi `inactive`, nên `hikbridge` tự về `OFF` sau `HIK_HOLD` (mặc định 10 giây) không có tin mới. hubd đọc `ON`/`OFF` thành 1/0: luật dùng `above: 0.5`.

Tải đo được (đầu ghi giả, luồng H.264 704×576 ~510 kbit/s): 10 kênh, mỗi kênh 1 người xem qua hubd tốn go2rtc 2,2% + hubd 1,4% của một luồng CPU; chi tiết ở mục 15.1 tài liệu thiết kế.

Đầu ghi từ chối mật khẩu thì hubd và hikbridge tạm ngừng gửi tài khoản đó 15 phút để đầu ghi không khoá IP của trung tâm (nút Thử kết nối cũng vậy); nhập mật khẩu khác trên trang Cài đặt là thử lại được ngay. Không mở cổng đầu ghi ra internet; xem từ xa qua WireGuard (MSE đi qua hubd nên chạy được; WebRTC cần tới cổng 8555).

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

## Nhạc (Spotify)

Thiết kế và các cách nối hub với amply: mục 18 của `docs/2026-10-26-trung-tam-dieu-khien-nha-design.md` (**chờ chủ nhà cho biết amply/loa từng khu** trước khi chọn đầu thu). Tiếng nhạc không đi qua hub; hub điều khiển Spotify Connect qua Web API, amply vẫn bật/tắt, đổi nguồn vào bằng hồng ngoại như "Loa quán".

- **Cần Spotify Premium** cho cả tài khoản tạo app lẫn tài khoản phát nhạc. App ở chế độ Development dùng tối đa 5 tài khoản; tài khoản khác tài khoản tạo app phải thêm vào User Management của app.
- **Một tài khoản chỉ phát ở một nơi tại một thời điểm**: trình phát hiện luồng đang phát, nút chọn thiết bị chuyển luồng sang đầu thu khác.
- Refresh token lưu trong SQLite của hub (nhóm cấu hình `spotify`), access token chỉ ở RAM; trình duyệt chỉ gọi `/api/spotify/*` sau PIN, không thấy token.

Bật lần đầu:

1. Hub phải bật HTTPS cổng 8443 (dùng chung với micro trợ lý, các bước ở mục Trợ lý giọng nói: `scripts/tls-selfsigned.sh`, `HUBD_TLS_CERT`/`HUBD_TLS_KEY`, iPad tin `tls-ca/ca.crt`). Spotify chỉ trả về địa chỉ HTTPS hoặc `127.0.0.1`.
2. developer.spotify.com → Dashboard → Create app → chọn Web API → Redirect URI `https://192.168.1.254:8443/api/spotify/callback` (trang Cài đặt hiện đúng địa chỉ theo IP đang mở). Chép Client ID (PKCE không cần client secret).
3. Dashboard → Cài đặt → Spotify: dán Client ID → **Kết nối Spotify** → đồng ý trên trang Spotify. Trang trả về báo "Đã kết nối Spotify"; quay lại Hub Center.
4. Đặt tên đầu thu Spotify Connect chứa tên hoặc id khu trong `config.yaml` (vd. "Khu cafe", "cafe", "Phòng ngủ 1") để trình phát trên trang phòng tự nhận đầu thu của phòng đó.

| API (sau PIN) | |
|---|---|
| `POST /api/spotify/login` | `{url}` trang đồng ý của Spotify (PKCE S256, `state` dùng một lần, hết hạn 10 phút) |
| `GET /api/spotify/callback` | Spotify trả về đây (không cần cookie: `state` xác thực), hub đổi mã lấy token |
| `GET /api/spotify/player` | `{connected, playing, track, artist, art, device}` |
| `GET /api/spotify/devices` | `{devices: [{id, name, type, is_active, volume_percent, supports_volume}]}` |
| `POST /api/spotify/cmd` `{do, device?, volume?}` | `do`: `play`, `pause`, `next`, `prev`, `volume` (0–100), `transfer` (cần `device`). 403 khi cần Premium, 409 khi chưa kết nối hoặc chưa có thiết bị đang mở |

Ngắt kết nối: nút **Ngắt kết nối** trong Cài đặt (xoá refresh token ở hub). Muốn chắc chắn thu hồi quyền: spotify.com → Tài khoản → Ứng dụng → gỡ app.

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
go test ./...                                   # rules, lịch sử, PIN, API, firmware, camera, hikbridge, trợ lý, Spotify (Spotify giả)
cd tuya-bridge && python3 -m unittest discover  # bridge + gen_config với thiết bị giả
python3 -m unittest discover -s scripts         # đăng nhập QR Tuya, giả lập node năng lượng
cd voiced && python3 -m unittest                # trợ lý: khớp lệnh, chặn PIN, vòng tool use (Claude giả); cần pip install anthropic numpy
```

Inverter (Deye, LuxPower, Lumentree) đọc qua RS485 bằng node ESPHome: xem [firmware/inverter/README.md](firmware/inverter/README.md).

## Chưa làm / giới hạn

- Cảm biến Tuya Wi-Fi chạy pin thường ngủ, không nhận kết nối local: chỉ cảm biến cắm điện mới đọc được ổn định.
- Telegram, kịch bản một chạm, báo cháy CBX: chưa làm. Cảnh báo `urgent` chưa lặp lại tới khi xác nhận.
- Cổng 1883 và 8080 mở trong LAN; không mở ra internet. PIN dùng chung, khoá 1 phút sau 5 lần sai.
