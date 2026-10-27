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
Hành động `{ notify: "câu", level: info|warn|urgent, kind: co2 }` gửi câu lên MQTT `hubd/alert` để trợ lý đọc thành tiếng. Có sẵn luật đọc CO₂ cao (`kind: co2`), bụi mịn (`bui`), trời bắt đầu mưa (`mua`), nóng bất thường (`nong`); bật/tắt từng `kind` ở Cài đặt → Đọc thông báo (loại bị tắt không đọc, lệnh tới thiết bị của luật vẫn chạy). `notify` không có `kind` luôn đọc.

## Trợ lý giọng nói (`voiced`)

Thiết kế: mục 15.2 của `docs/2026-10-26-trung-tam-dieu-khien-nha-design.md`. Container thứ 4, tắt đi thì lõi vẫn chạy đủ.

- **Nói:** nút micro trên thanh trên của dashboard. Chạm để nghe, chạm lần nữa (hoặc sau 8 giây) để gửi. Âm thanh đi qua hubd `/voice/*` (cần đăng nhập PIN) tới `voiced`, không mở cổng nào khác.
- **Hiểu:** chuyển giọng nói thành chữ ngay trên máy Ubuntu (sherpa-onnx, Zipformer tiếng Việt). Lệnh cố định khớp theo tên trong `config.yaml`, chạy cả khi mất internet: "bật đèn kho", "tắt đèn phòng lạnh", "ra khỏi nhà" (tên kịch bản), "pin còn bao nhiêu", "đèn kho đang bật không", "tắt báo động", "tình hình thế nào". Câu khác gửi AI qua API chat completions **tương thích OpenAI** (OpenAI, OpenRouter, Groq, Gemini, Ollama/LM Studio trong LAN...) kèm tool `get_state`, `query_history`, `set_device`, `run_scene`, `ack_alarm`. Chưa cài AI thì chỉ chạy lệnh cố định.
- **Đọc:** VieNeu-TTS giọng Hải Đăng, chạy cục bộ. Nút loa trên thanh trên đọc tóm tắt tình hình: thời tiết (cảm biến ngoài trời), không khí trong nhà (CO₂, bụi mịn cao nhất ở đâu), pin/điện mặt trời/lưới, cảnh báo đang mở; tính ngay trong nhà, không cần AI. Cảnh báo `notify` của luật được publish lên MQTT `hubd/alert`; `voiced` đọc lên mọi màn hình đang mở dashboard. Câu cảnh báo cố định được tạo sẵn âm thanh lúc khởi động. **Đọc chạy cả trên HTTP**; chỉ micro cần HTTPS.
- **Quyền:** `voiced` gọi hubd bằng `HUBD_ASSISTANT_TOKEN`, chỉ qua `/api/assistant/*`, quyền như người chưa nhập PIN. hubd từ chối (403) lệnh tới thiết bị `pin: true`/`protected: true` và kịch bản `pin: true` hoặc có bước đụng các thiết bị đó. PIN của iPad không truyền sang trợ lý.

Bật lần đầu (máy đã có `.env`):

```bash
scripts/init-secrets.sh            # thêm HUBD_ASSISTANT_TOKEN + tài khoản MQTT voiced, không đổi khoá cũ
docker compose restart mosquitto   # nạp tài khoản MQTT mới
```

Bật dịch vụ bằng profile `voice`: thêm `voice` vào `COMPOSE_PROFILES` trong `.env` (vd. `COMPOSE_PROFILES=voice` hoặc `camera,voice`), rồi `docker compose up -d --build`. Build `voiced` lần đầu tải mô hình (~250 MB) vào image, sau đó chạy không cần internet. Rồi trên dashboard:

1. **Cài đặt → Trợ lý AI:** nhập địa chỉ API (base URL, vd `https://api.openai.com/v1`, `https://openrouter.ai/api/v1`, `http://192.168.1.x:11434/v1` cho Ollama), API key (trống nếu máy AI trong nhà không cần), bấm **Thử kết nối** để lấy danh sách model (endpoint không trả danh sách thì gõ tên model), chọn model, **Lưu**. Khoá lưu trong hubd (nhóm `ai`), trình duyệt chỉ thấy "đã lưu"; nút Thử đi qua hubd → `voiced`, trình duyệt không gọi thẳng endpoint. Đổi địa chỉ thì phải nhập lại khoá (khoá cũ không gửi sang máy khác). `voiced` đọc lại cấu hình ngay khi lưu (MQTT `hubd/settings/ai`).
2. **Cài đặt → HTTPS:** bấm **Bật HTTPS** rồi cài chứng chỉ lên iPad theo các bước hiện ngay trong mục đó (mục HTTPS bên dưới). Chạm màn hình một lần sau khi mở dashboard để iOS cho phép tự đọc cảnh báo.
3. **Cài đặt → Đọc thông báo:** tắt loại không muốn nghe.

Câu hỏi tự do và trạng thái thiết bị dạng chữ được gửi tới địa chỉ AI đã nhập; âm thanh không rời khỏi nhà.

Kịch bản khai báo ở mục `scenes:` của `config.yaml`, câu cảnh báo ở `notify:` của luật (xem ví dụ trong file).

| API trợ lý (Bearer `HUBD_ASSISTANT_TOKEN`) | |
|---|---|
| `GET /api/assistant/config` | khu, số đo, thiết bị, kịch bản, `locked` (id cần PIN), `phrases` (câu cảnh báo cố định) |
| `GET /api/assistant/state` | `values` như `/api/state`, `alerts` chưa xác nhận |
| `GET /api/assistant/history?id=&hours=` | như `/api/history` |
| `POST /api/assistant/cmd` `{device, to}` | 204; 403 khi cần PIN |
| `POST /api/assistant/scene/{id}` | 204; 403 khi cần PIN; 502 khi có bước không gửi được |
| `POST /api/assistant/alarm/ack` | xác nhận cảnh báo, publish `hubd/alert/ack` |
| `GET /api/assistant/settings/ai` | `{url, api_key, model}` đầy đủ cho `voiced` (token trợ lý chỉ đọc được nhóm `ai`) |

| `voiced` qua hubd `/voice/*` (cần PIN) | |
|---|---|
| `POST /voice/ask` | WAV hoặc `{text}` → `{heard, reply, audio}` |
| `POST /voice/brief` | tóm tắt tình hình → `{reply, audio}` |
| `POST /voice/ai/check` `{url, api_key, model}` | ô trống = giá trị đã lưu → `{ok, models, message}`; chỉ gọi `{url}/models`, `{url}/chat/completions`, không theo chuyển hướng |
| `GET /voice/events` | SSE cảnh báo `{type: alert, level, text, audio}` / `{type: ack}` |

## HTTPS (dùng chung: micro của trợ lý, đăng nhập Spotify)

hubd luôn phục vụ **cùng mọi trang và API** ở cổng 8443 (HTTPS) như cổng 8080, nên tính năng nào cần HTTPS chỉ việc dùng `https://<IP hub>:8443/<đường dẫn>`; không thêm cổng, không thêm chứng chỉ. Ví dụ redirect URI OAuth của Spotify: `https://192.168.1.254:8443/api/spotify/callback`. Cookie đăng nhập dùng chung giữa 8080 và 8443 (cookie không phân biệt cổng).

- **Bật:** Cài đặt → HTTPS → **Bật HTTPS**, mở dashboard bằng IP (vd `http://192.168.1.254:8080`). hubd tạo CA riêng của nhà ("iNIX Home CA <ngày>") và chứng chỉ 825 ngày (giới hạn của iOS) cho IP đó; có hiệu lực ngay, không khởi động lại. **Cấp lại** khi đổi IP hoặc sắp hết hạn: giữ nguyên CA, iPad không phải cài lại.
- **Cài lên iPad (một lần):** Safari (không mở từ biểu tượng màn hình chính) → `http://192.168.1.254:8080/api/tls/ca.crt` → Cho phép; Cài đặt → Đã tải về hồ sơ → Cài đặt (nhập mật mã) → Xong; Cài đặt chung → Giới thiệu → Cài đặt tin cậy chứng nhận → bật "iNIX Home CA ...". Mở `https://192.168.1.254:8443`, nhập PIN, Chia sẻ → Thêm vào MH chính. Mục HTTPS trên trang Cài đặt có đủ các bước và mã SHA-256 để so.
- **An toàn:** khoá CA nằm trong SQLite của hubd (nhóm `sys:tls`, API cấu hình không đọc được). CA bị giới hạn tên (chỉ `10/8`, `172.16/12`, `192.168/16` và `*.home.arpa`) nên lộ khoá cũng không giả được trang web ngoài internet. `/api/tls/ca.crt` không cần PIN (chứng chỉ công khai).
- **Chứng chỉ riêng:** đặt `HUBD_TLS_CERT`/`HUBD_TLS_KEY` trong `.env` (vd từ `scripts/tls-selfsigned.sh`, giữ khoá CA ngoài container, hoặc chứng chỉ tên miền thật) thì hubd dùng tệp đó, trang Cài đặt chỉ hiện trạng thái.
- Cổng 8443 do Docker publish nên vào được từ LAN dù `ufw` chỉ mở 22. Không mở 8080/8443 ra internet.

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

**Trình phát đầy đủ:** mở thanh nhạc trên dashboard rồi chạm đĩa hoặc tên bài. Bảng có bìa lớn, tiến độ (kéo để tua), trộn bài, trước/dừng/sau, âm lượng, nơi phát; ô tìm (bài hát, playlist, album, nghệ sĩ), playlist của tài khoản, bài đã thích, hàng đợi (nút + ở mỗi bài để thêm). Chạm một bài để phát; chạm playlist/album để xem bài rồi phát từ bài được chọn; chạm nghệ sĩ để phát nhạc của nghệ sĩ đó. Spotify chỉ cho xem bài của playlist do mình tạo hoặc cùng sửa; playlist khác (vd. của Spotify) vẫn phát được cả danh sách.

**Kết nối từ trước bản này thì phải bấm "Kết nối lại"** (Cài đặt → Spotify) để cấp thêm quyền `playlist-read-private`, `playlist-read-collaborative`, `user-library-read`. Hub so scope Spotify đã cấp với scope cần; thiếu thì Cài đặt và bảng trình phát hiện gợi ý. Phát, dừng, chọn thiết bị vẫn chạy khi chưa kết nối lại.

| API (sau PIN) | |
|---|---|
| `POST /api/spotify/login` | `{url}` trang đồng ý của Spotify (PKCE S256, `state` dùng một lần, hết hạn 10 phút) |
| `GET /api/spotify/callback` | Spotify trả về đây (không cần cookie: `state` xác thực), hub đổi mã lấy token |
| `GET /api/spotify/player` | `{connected, playing, track, artist, art, device}` |
| `GET /api/spotify/devices` | `{devices: [{id, name, type, is_active, volume_percent, supports_volume}]}` |
| `POST /api/spotify/cmd` `{do, device?, ...}` | `do`: `play` (tuỳ chọn `context` = uri playlist/album/nghệ sĩ, `offset` = uri bài bắt đầu (chỉ playlist/album), hoặc `uris` = tối đa 50 bài), `pause`, `next`, `prev`, `volume` (`volume` 0–100), `transfer` (cần `device`), `queue` (`uri` bài), `seek` (`position_ms`), `shuffle` (`state`). 403 khi cần Premium hoặc thiếu quyền, 409 khi chưa kết nối hoặc chưa có thiết bị đang mở |
| `GET /api/spotify/search?q=` | `{tracks, playlists, albums, artists}`, tối đa 10 mỗi loại |
| `GET /api/spotify/playlists` | `{playlists}`: 50 playlist đầu của tài khoản |
| `GET /api/spotify/tracks?of=` | `{tracks}`: 50 bài đầu của `spotify:playlist:<id>`, `spotify:album:<id>` hoặc `liked` (bài đã thích) |
| `GET /api/spotify/queue` | `{queue}`: 20 bài kế tiếp |

Mỗi mục trong danh sách chỉ gồm `{uri, name, sub, art, ms, total}` (sub = nghệ sĩ hoặc chủ playlist); `/api/spotify/player` có thêm `uri`, `context`, `progress_ms`, `duration_ms`, `shuffle`, `reconnect`.

Ngắt kết nối: nút **Ngắt kết nối** trong Cài đặt (xoá refresh token ở hub). Muốn chắc chắn thu hồi quyền: spotify.com → Tài khoản → Ứng dụng → gỡ app.

## Nhạc trên hub (file MP3, phát tới các amply)

Hub tự phát nhạc, không cần mở app nào (mục 18.3 tài liệu thiết kế, `music.go` + `snapcast.go`). Mỗi amply có một **đầu nhận** cắm dây AUX/RCA: ESP32 + DAC I2S (PCM5102A) hoặc Raspberry Pi Zero 2 W + DAC, chạy snapclient có sẵn. Các khu phát đồng bộ cùng một nhạc; trên dashboard bật/tắt tiếng, chỉnh âm lượng, đặt tên từng khu.

- **Nhạc:** chép file `.mp3` vào thư mục `music/` cạnh `docker-compose.yml`, hoặc cắm USB (máy Ubuntu cần tự gắn USB dưới `/media`, ví dụ bằng `udisks2`). Hub quét lại sau 30 giây; bỏ file và thư mục ẩn (`._*`, `.Trashes`) và symlink.
- **Đầu nhận:** snapclient trỏ tới IP hub, cổng `1704` (vd. Pi: `apt install snapclient`, sửa `/etc/default/snapclient` thành `SNAPCLIENT_OPTS="--host 192.168.1.254 --hostID khu-cafe"`). Mở `ufw` cho LAN vào cổng 1704. Đặt `--hostID` theo khu để nhận ra dễ, đổi tên hiển thị ngay trên dashboard.
- **Dashboard:** mở thanh nhạc → chạm đĩa → bảng trình phát → nguồn **Trên hub**: thư mục → bài, ô tìm, khu phát.

| API (sau PIN) | |
|---|---|
| `GET /api/music/library` | `{files: [{id, title, folder}], roots: [{path, ok}]}` |
| `GET /api/music/state` | `{playing, file, index, queue, pos_ms, dur_ms, zones: [{id, name, connected, volume, muted}]}` |
| `POST /api/music/cmd` | `{do: play, files: [id...], index}` (hàng đợi mới, tối đa 2000 bài), `{do: play}` phát tiếp, `pause`, `next`, `prev`, `stop`, `{do: zone, id, volume?, muted?, name?}` |

Luồng: PCM 44,1 kHz/16 bit stereo, khoảng 1,4 Mbit/s mỗi đầu nhận trong LAN, đệm 1 giây. Spotify phát qua hub (librespot) là giai đoạn sau.

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
cd voiced && python3 -m unittest                # trợ lý: khớp lệnh, chặn PIN, tool calling với endpoint OpenAI giả, tóm tắt; cần pip install numpy (Python ≥ 3.9)
```

Inverter (Deye, LuxPower, Lumentree) đọc qua RS485 bằng node ESPHome: xem [firmware/inverter/README.md](firmware/inverter/README.md).

## Chưa làm / giới hạn

- Cảm biến Tuya Wi-Fi chạy pin thường ngủ, không nhận kết nối local: chỉ cảm biến cắm điện mới đọc được ổn định.
- Telegram, kịch bản một chạm, báo cháy CBX: chưa làm. Cảnh báo `urgent` chưa lặp lại tới khi xác nhận.
- Cổng 1883 và 8080 mở trong LAN; không mở ra internet. PIN dùng chung, khoá 1 phút sau 5 lần sai.
