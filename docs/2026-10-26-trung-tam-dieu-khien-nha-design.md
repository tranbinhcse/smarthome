# Trung tâm điều khiển nhà — Thiết kế phương án

Ngày: 2026-10-26 · Trạng thái: bản nháp chờ chủ nhà duyệt

## 1. Mục đích và lý do

Một bộ điều khiển trung tâm **gọn, chạy cục bộ, tiết kiệm điện** để giám sát và điều khiển:

- công tắc, CB, ổ cắm Tuya (Wi-Fi) đang có;
- inverter hybrid Deye và pin lưu trữ dùng JK BMS;
- cảm biến theo khu: khói, gas, bụi PM10, bụi mịn PM2.5, chất lượng không khí (CO2, VOC, NOx), nhiệt độ/độ ẩm, cường độ ánh sáng, mưa.

Chủ nhà đã thử hướng Home Assistant và thấy **quá nhiều thứ, khó dùng**. Yêu cầu: thiết bị chuyên dụng, nhỏ, dễ quản lý, ít điện, OS tối giản chỉ chạy đúng chức năng cần.

## 2. Mục tiêu, không làm, thất bại nếu

**Mục tiêu (v1)**

- Cảm biến đặt ở từng khu, gửi về trung tâm qua **Wi-Fi nội bộ**.
- **Trung tâm = máy Ubuntu `192.168.1.254` chạy 24/7; màn hình = iPad Pro 10" treo/đặt trong nhà**, mở web của `hubd` bằng Safari (thêm vào Màn hình chính, chạy toàn màn hình). Điện thoại mở cùng giao diện qua mạng.
- Mất internet vẫn chạy đủ, trừ thông báo ra ngoài. Mất điện lưới vẫn chạy nhờ ngõ backup của Deye (máy Ubuntu, router và iPad cắm vào mạch này); iPad có pin riêng nên vẫn hiện được tới khi hết pin.
- Cảnh báo: khói, gas, pin bất thường, mất điện lưới, mưa, không khí xấu, thiết bị mất kết nối.
- Điện năng trung tâm: máy Ubuntu có sẵn, đang chạy VM/dịch vụ khác nên phần thêm của `hubd` + Mosquitto + cầu nối Tuya không đáng kể (**chưa đo**).

**Không làm ở v1**

App điện thoại riêng (điện thoại mở trang web), phân quyền nhiều người dùng, giao diện kéo thả để soạn tự động hoá (sửa file YAML), **ghi thông số vào Deye hoặc JK BMS**, image OS tự đóng bằng Buildroot, ghi hình camera (NVR đã làm).

Camera Hikvision và trợ lý giọng nói AI **nằm trong phạm vi nhưng làm sau lõi**, xem mục 15.

**Hệ báo cháy CBX tích hợp chỉ-đọc, không sửa firmware** (mục 17). Trung tâm chỉ nhận telemetry của tủ, không điều khiển tủ. Phần dò cháy và hú còi của hệ CBX vẫn chạy độc lập với trung tâm.

**Thất bại dù vẫn chạy được, nếu:**

- Báo khói/gas phụ thuộc trung tâm. Hệ báo cháy CBX và đầu báo của nó phải tự kêu khi trung tâm chết.
- Sửa firmware tủ CBX, hoặc chặn/sửa bản tin trên đường đi làm hệ báo cháy hoạt động sai.
- Tự động hoá cắt nhầm nguồn của trung tâm, router hoặc tủ lạnh.
- Chức năng cốt lõi phụ thuộc cloud.
- Thêm một thiết bị mới phải sửa code thay vì chỉ sửa file cấu hình.
- Trung tâm cần chăm sóc thường xuyên: hỏng thẻ nhớ, treo, phải cập nhật hằng tuần.

## 3. Ràng buộc và giả định

- Wi-Fi IoT dùng băng 2.4 GHz. Thiết bị Tuya đã có sẵn.
- **Deye:** có dongle Wi-Fi, chưa rõ đọc dữ liệu cục bộ được không. Chủ nhà đồng ý đọc bằng ESP32 qua cổng RS485 của inverter. Model ghi ở Giai đoạn 0.
- **Pin:** **3 pack JK BMS nối song song**, mỗi pack có Bluetooth và cổng RS485. Model ghi ở Giai đoạn 0.
- **Camera:** đã có hệ thống Hikvision. Model NVR/camera ghi ở Giai đoạn 0.
- **Phạm vi giám sát:**
  - 3 khu vực (tên khu ghi ở Giai đoạn 0);
  - giếng trời: mưa, ánh sáng, nhiệt độ/độ ẩm ngoài trời;
  - khu inverter/pin;
  - máy lọc RO.

  Hồ cá đã bỏ khỏi phạm vi.
- **Wi-Fi phủ khắp nhà.** Mọi node đi Wi-Fi 2.4 GHz. **Không dùng LoRa, không dùng Zigbee.**
- **Báo cháy = hệ 3SCORP CBX có sẵn, giữ nguyên chức năng, tích hợp chỉ-đọc qua DNS.** Tủ CBX và đầu báo vẫn là hệ báo cháy đã kiểm định, chạy độc lập; phần dò cháy và hú còi tại chỗ không phụ thuộc mạng, không sửa firmware. Để trung tâm giám sát (kết nối, pin, báo cháy/khói từng đầu báo): router trỏ DNS `cbx.vn` về trung tâm, trung tâm chạy bộ nghe UDP nhận bản tin của tủ. Đổi lại, sau khi trỏ DNS tủ không gửi về máy chủ 3SCORP nữa, app/giám sát của hãng ngừng — chủ nhà xác nhận không cần. Chi tiết mục 17.
- **Vỏ node = tái dùng 10 vỏ đầu báo CBX cũ** (bản lỗi, hết pin, không dùng). Mỗi vỏ ráp ESP32 + cảm biến của mình, kết nối Wi-Fi. Buồng khói quang trong vỏ cũ còn tốt, có thể cho ESP32 đọc làm **cảnh báo khói phụ** — không phải đầu báo kiểm định, không thay hệ CBX.
- Chủ nhà là dev, có PC để build firmware và deploy. Chỉ một người vận hành.
- **Trung tâm: Ubuntu PC `192.168.1.254`** (có sẵn, x86-64, Linux, chạy 24/7). Chạy `hubd` + Mosquitto + `cbxd`. Xem mục 6.
- **Màn hình: iPad Pro 10"** (có sẵn), Safari kiosk mở web của `hubd`.
- **Surface Pro 5 m3 đã mua giờ dư** (trung tâm chuyển sang Ubuntu PC, màn hình dùng iPad). Giữ làm màn hình thứ hai hoặc dự phòng. **Chưa mua Pi 5.**

## 4. Kiến trúc

```
                      Wi-Fi nội bộ (SSID IoT 2.4 GHz)
 ┌───────────────────────────────────────────────────────────────┐
 │ Node khu vực (ESP32, ESPHome)    ── MQTT ──┐                  │
 │ Node năng lượng: RS485→Deye, BLE→JK ─ MQTT ┤                  │
 │ Tuya đã flash OpenBeken          ── MQTT ──┤                  │
 │ Tuya giữ firmware gốc  ◄── giao thức local ┤                  │
 └────────────────────────────────────────────┼──────────────────┘
                                              ▼
          ┌──── TRUNG TÂM: máy Ubuntu 192.168.1.254 (Docker Compose) ────┐
          │ hub-mosquitto (MQTT broker, user/mật khẩu)                   │
          │ hubd: trạng thái · lịch sử SQLite · luật · web · PIN         │
          │ tuya-bridge: Tuya (giao thức local) ⇄ MQTT                   │
          └───────┬───────────────────────────────────┬──────────────────┘
                  │ HTTP + SSE (LAN)                  │ HTTPS (về sau)
        iPad Pro 10" (Safari, PWA)             Telegram (cảnh báo)
        Điện thoại (trình duyệt)

(Zigbee2MQTT + SLZB-06 và `cbxd` là phần mở rộng, chưa triển khai.)
```

**Nguyên tắc cốt lõi: mọi thứ nói MQTT.** `hubd` chỉ hiểu MQTT, cộng thêm giao thức local Tuya cho thiết bị không flash được. Thêm thiết bị mới = thêm vài dòng vào `config.yaml`.

**Vị trí đặt**

- **Máy Ubuntu** giữ nguyên chỗ hiện tại, IP cố định (DHCP reservation). Máy còn chạy VM/libvirt, postfix, WireGuard và các container khác — `hubd` chỉ nằm trong `/root/hubd` (3 container `hub-mosquitto`, `hubd`, `tuya-bridge`), không đụng phần còn lại. `ufw` đang bật: UDP 6667 (quét Tuya) bị chặn vào, nhưng TCP ra thiết bị Tuya cổng 6668 thì chạy bình thường, nên cầu nối dùng IP tĩnh.
- **iPad Pro 10"** đặt/treo phòng khách, cắm sạc liên tục; bật *Guided Access* nếu muốn khoá vào một app.
- Máy Ubuntu, router (và SLZB-06 nếu có) nên cắm vào **mạch backup (Load/EPS) của Deye**.
- Node năng lượng đặt cạnh inverter. Nếu buộc phải nằm trong tủ kim loại thì dùng board có anten ngoài.

## 5. Phần cứng

**Danh sách mua đầy đủ** (số lượng, giá ước tính, link Shopee, vỏ in 3D) nằm ở [danh-sach-mua.md](danh-sach-mua.md). File đó là nguồn duy nhất cho linh kiện; mục này chỉ giữ các nguyên tắc chọn.

- **Một loại board cho mọi node:** ESP32 DevKit (WROOM-32) 30 chân, chip nạp CP2102, cổng USB-C.
- **Node không khí:** dùng PMS7003 (bụi) + SGP41 (VOC/NOx) + SCD41 (CO2, T/RH) + BH1750 + LD2410C. Không dùng SEN55 vì Shopee không bán (kiểm ngày 2026-10-26).
- **Giếng trời:** không đặt node ESP32 nào ở đây.
  - Mưa và ánh sáng đo bằng **cảm biến mưa Zigbee chạy pin mặt trời**, loại Tuya RB-SRAIN01, Zigbee2MQTT có hỗ trợ.
  - Nhiệt độ/độ ẩm đo bằng **cảm biến Zigbee đặt trong chắn bức xạ in 3D**.
  - Nhờ vậy không cần đi dây ra giếng trời, không phải mua hộp IP65, RG-9, VEML7700 hay SHT40.
- **Node năng lượng** cấp nguồn bằng củ sạc 5 V thường. Shopee không có nguồn DIN rail Mean Well HDR-15-5.
- **Zigbee** chỉ dùng cho đầu báo khói, còi và cảm biến rò nước. Coordinator là SLZB-06 nối LAN, không chiếm cổng USB của máy Ubuntu.
- **Không dùng các loại sau:**
  - DHT11/22;
  - MQ-2/MQ-135;
  - CCS811 (eCO2 chỉ là ước lượng);
  - tấm mưa FC-37 (bị ăn mòn);
  - BH1750 ngoài trời (bão hoà khoảng 65k lux).
- **Pi 5 không cần mua.** Trung tâm đã là máy Ubuntu có sẵn. Nếu sau này chuyển sang Pi, `hubd` build lại cho arm64 là chạy, không sửa code.
- **Điện năng phần thêm:** các node ESP32 khoảng vài W tổng cộng. Đây là ước tính, **chưa đo**.

### 5.1 Kiểm tra máy Ubuntu và iPad

- **Máy Ubuntu:** có Docker + Compose; `ufw` cho phép LAN vào cổng `8080` (web) và `1883` (MQTT); còn dung lượng đĩa (hiện còn nhiều); IP `192.168.1.254` đã đặt DHCP reservation.
- **Kết nối ra thiết bị Tuya:** từ máy Ubuntu, `nc -vz <ip> 6668` tới từng thiết bị phải thông (đã xác nhận TCP ra được; UDP 6667 vào bị chặn nên không quét được).
- **iPad:** mở được `http://192.168.1.254:8080`, đăng nhập PIN, "Thêm vào Màn hình chính" chạy toàn màn hình; đặt Auto-Lock = Never khi cắm sạc, tắt Low Power Mode.
- **Cảm ứng/độ lớn nút:** nút thao tác ≥ 56 px, thử ngón tay trên iPad thật ở 4 góc.

## 6. Hệ điều hành và triển khai (máy Ubuntu + iPad)

**Nền:** máy Ubuntu có sẵn, **không cài lại, không đổi cấu hình hệ thống**. Mọi thứ của `hubd` nằm trong `/root/hubd` và chạy bằng Docker Compose (`restart: unless-stopped` nên tự lên sau khi khởi động lại máy).

**Dịch vụ (3 container)**
- `hub-mosquitto` (eclipse-mosquitto:2): bắt buộc user/mật khẩu (`hubd`, `tuya`, `node`), file mật khẩu và `.env` sinh bằng `scripts/init-secrets.sh`, không commit.
- `hubd`: binary Go trong image distroless; SQLite ở volume `hubd-db`; cấu hình `config.yaml` gắn read-only.
- `tuya-bridge` (Python + tinytuya): `network_mode: host`, đọc `tuya/devices.json` (không commit).
- Dịch vụ khác trên máy (VM/libvirt, postfix, WireGuard, container DNS…) **không thuộc hệ này và không được đụng tới**.

**Màn hình:** iPad mở `http://192.168.1.254:8080`, đăng nhập PIN một lần (cookie 90 ngày), thêm vào Màn hình chính. Không cần kiosk/Chromium/`cage`.
- Chế độ đêm làm trong giao diện (nền tối sẵn). Việc tắt/bật nền màn hình theo người đi vào dùng Auto-Lock/Guided Access của iPad; agent `backlight` của phương án Surface bị bỏ.

**Tự phục hồi:** `restart: unless-stopped`; mất kết nối MQTT thì `hubd` và `tuya-bridge` tự nối lại; giao diện tự kết nối lại SSE.

**Cập nhật:** `git pull`/đồng bộ mã vào `/root/hubd` rồi `docker compose up -d --build`. Quay về bản cũ bằng `git checkout <commit>` rồi build lại. Cấu hình luật/thiết bị: sửa `config.yaml`, `docker compose restart hubd`.

**Sao lưu:** volume `hubd-db` (SQLite) và thư mục `/root/hubd` (trừ `tuya/` và `.env`, sao lưu riêng vào nơi an toàn).

**Quản trị:** SSH (máy đã có WireGuard cho truy cập từ xa; `hubd` **không** mở ra internet).

**Vì sao Mosquitto chạy riêng thay vì nhúng vào `hubd`:** cập nhật hoặc khởi động lại `hubd` không làm rớt kết nối của cả nhà, và Mosquitto đã được kiểm chứng lâu năm.

**Về sau, không thuộc v1:** Telegram, Zigbee2MQTT, `cbxd`, camera, giọng nói.

## 7. Phần mềm trung tâm `hubd`

**Công nghệ**

- Go, một file binary, build trong Docker (`golang` → image distroless) cho máy Ubuntu x86-64. Chuyển sang Pi thì chỉ cần build `arm64`, không sửa code.
- Thư viện ngoài chỉ 3: `paho.mqtt.golang`, `modernc.org/sqlite` (Go thuần, không cần cgo), `gopkg.in/yaml.v3`.
- Web dùng `net/http` của thư viện chuẩn, đẩy dữ liệu tức thời bằng **SSE**, file tĩnh nhúng bằng `go:embed`.
- Biểu đồ dùng uPlot, nhúng sẵn trong binary, chạy offline.

**Các module**

1. **mqtt:** subscribe các topic khai báo trong config. Theo dõi online/offline của node qua topic `<node>/status` (ESPHome tự gửi LWT).
2. **state:** lưu trong bộ nhớ: `entity → giá trị, đơn vị, thời điểm, online`.
3. **history (SQLite trong `/data`):**
   - bảng `samples(entity, ts, value)` giữ **dữ liệu thô 30 ngày**;
   - job chạy hằng đêm gộp dữ liệu cũ thành **trung bình 5 phút, giữ 2 năm**.
4. **rules:** đọc luật từ `rules.yaml`, đánh giá mỗi khi trạng thái đổi, cộng một ticker 1 giây cho điều kiện `for`. Mỗi luật có:
   - ngưỡng `above`/`below`, có `clear_*` để chống bật tắt liên tục;
   - điều kiện `for` (giữ bao lâu mới kích hoạt);
   - `and` để ghép điều kiện;
   - khung giờ;
   - `then` và `on_clear`.
5. **devices:** lệnh `set` gửi qua MQTT (ESPHome, OpenBeken, Zigbee2MQTT) hoặc qua Tuya local. **Thiết bị `protected: true` không luật nào tắt được.** Ràng buộc này nằm trong code, không phụ thuộc người viết luật.
6. **web/API:**
   - `GET /api/state`;
   - `GET /events` (SSE);
   - `GET /api/history?entity=&from=&to=`;
   - `POST /api/cmd`;
   - `POST /api/scene/{id}`;
   - `POST /api/alarm/ack`.
7. **notify:** gửi Telegram theo 3 mức:
   - *khẩn*: lặp lại mỗi phút đến khi có người xác nhận;
   - *cảnh báo*;
   - *thông tin*.

   Mất internet thì xếp hàng trong SQLite và gửi bù khi có mạng.
8. **watchdog:** node im lặng quá 10 phút thì cảnh báo.

**Cấu hình**

- `config.yaml`: khai báo khu, entity, thiết bị, kịch bản.
- `rules.yaml`: luật tự động.
- `systemctl reload hubd` (SIGHUP) sẽ nạp lại. **File sai cú pháp hoặc tham chiếu thì giữ cấu hình cũ** và báo lỗi.
- Nguồn gốc cấu hình là một repo git trên PC. Secrets để riêng, không đưa vào git.

```yaml
# config.yaml (trích)
zones:
  - { id: phongkhach, name: Phòng khách }
entities:
  - { id: phongkhach.co2, topic: node-phongkhach/sensor/co2/state, unit: ppm, zone: phongkhach }
  - { id: deye.soc, topic: node-nangluong/sensor/battery_soc/state, unit: "%" }
devices:
  - { id: quat_hut_pk, name: Quạt hút PK, kind: mqtt, command_topic: obk-quathut/1/set }
  - { id: cb_nonglanh, name: CB bình nóng lạnh, kind: tuya, tuya: { ip: 192.168.20.31, id: "...", key_ref: cb_nonglanh, dp: 1 } }
  - { id: cb_tu_lanh, name: CB tủ lạnh, kind: tuya, protected: true, tuya: { ... } }
scenes:
  - { id: ra_khoi_nha, name: Ra khỏi nhà, do: [ { set: den_pk, to: off }, { set: den_bep, to: off } ] }

# rules.yaml (trích)
- id: co2_cao_pk
  when: { entity: phongkhach.co2, above: 1000, clear_below: 900, for: 2m }
  then:     [ { set: quat_hut_pk, to: on } ]
  on_clear: [ { set: quat_hut_pk, to: off } ]
```

**Bảo mật**

- `hubd` chỉ lắng nghe trên LAN và Tailscale. **Không mở port ra internet.**
- Ghép thiết bị một lần: mở `/pair`, nhập PIN quản trị. Server lưu token ngẫu nhiên vào cookie HttpOnly. Mọi API đều đòi token này.
- **Cắt CB cần nhập PIN.** Tắt còi thì không cần PIN.
- MQTT bắt buộc username/password, mỗi node một tài khoản. Tài khoản node tạo trên dashboard chỉ đi được topic của chính nó (mục 8).

**Kiểm thử**

- Rules engine có test bảng bằng `go test`. Bao phủ các trường hợp ngưỡng, `for`, `clear_*`, `and`, và thiết bị `protected`.
- Kiểm tra nghiệm thu bằng cách giả lập cảm biến: `mosquitto_pub` gửi giá trị giả.

## 8. Firmware node (ESPHome, không dùng Home Assistant)

**Build và nạp**

- ESPHome CLI chạy trên PC: `esphome run node-xxx.yaml`. Lần đầu nạp qua USB, các lần sau nạp qua mạng (OTA, có mật khẩu).
- Bỏ `api:`, dùng `mqtt:` trỏ IP trung tâm. Có `wifi:` với AP dự phòng, cộng `logger`.
- Một gói chung `common.yaml` qua `packages:`. Mỗi node một file, chỉ bật module của khu đó.
- Mã nguồn: `firmware/` ([README](../firmware/README.md)). Topic `node-<khu>/sensor/<tên>/state`, mỗi node một tài khoản MQTT.
- Hồng ngoại (bổ sung 2026-10-27): node không khí gắn mắt thu + LED phát. Máy lạnh dùng `climate_ir`, thiết bị khác dùng mã học được lưu trong `config.yaml`; `hubd` giữ trạng thái giả định.
- Cấu hình node trên dashboard (bổ sung 2026-10-27, Cài đặt → Node ESP32): danh sách node, tài khoản MQTT từng node do `hubd` tạo/khoá/xoá qua plugin dynamic security của Mosquitto (tài khoản quản trị riêng; tài khoản dịch vụ vẫn ở `passwd`), hãng máy lạnh, gán mã hồng ngoại vừa học (lưu trong kho cấu hình của `hubd`, đè lên `config.yaml`). Lý do và cách chuyển: [firmware/README.md](../firmware/README.md).

**Xử lý số đo**

- Bộ lọc: trung bình trượt + `throttle` 10–30 s + `delta` để bớt lưu lượng.
- **Đặt cảm biến cách xa chip ESP32**, vì chip toả nhiệt làm sai số đo.
- Có núm hiệu chỉnh: `offset` nhiệt độ của SCD41. SGP41 lấy nhiệt độ/độ ẩm từ SCD41 để bù. Hiệu chỉnh bằng cách đặt các node cạnh nhau 24 giờ.

**Deye**

- `modbus_controller`, thanh ghi lấy theo cấu hình cộng đồng (klatremis/esphome-for-deye hoặc Lewa-Reka/esphome-deye-inverter). Đọc mỗi 5–10 s.
- **Chỉ khai báo `sensor`, không khai báo `number`/`select`/`switch`.** Nhờ vậy về mặt kỹ thuật không thể ghi vào inverter.
- Đối chiếu sơ đồ chân RJ45 trong manual của model trước khi bấm cáp.
- Giữ logger Wi-Fi để app Deye vẫn xem được. ESP32 dùng cổng RS485 riêng. Nếu model đó cho logger và cổng RS485 dùng chung một bus thì không cắm song song, vì hai bên cùng hỏi sẽ xung đột. Khi đó phải chọn một trong hai.

**JK BMS (3 pack song song)**

- **Ưu tiên RS485.** Nối ESP32 vào mạng RS485 nội bộ giữa các pack (dòng JK-PB). Bản ESPHome cộng đồng `txubelaxu/esphome-jk-bms` đọc được nhiều pack trên mạng này. Mỗi pack có địa chỉ riêng: master = 0, các slave = 1, 2.
- **Không dùng CAN để giám sát**, vì qua CAN chỉ đọc được một BMS.
- **Không cắm vào bus đang có thiết bị khác hỏi dữ liệu**, ví dụ cổng nối inverter.
- **Dự phòng BLE:** `jk_bms_ble` (syssi/esphome-jk-bms), protocol `JK02_32S` cho dòng PB. Một ESP32 giữ được tối đa 3 kết nối BLE. Khi ESP32 đang giữ kết nối thì app điện thoại không vào được pack đó.
- Chỉ khai báo sensor, **không khai báo switch sạc/xả**.
- **Xác minh ở Giai đoạn 0:**
  - model và hardware version từng pack;
  - pack nào nối inverter, qua cổng nào (CAN hay RS485);
  - địa chỉ DIP của từng pack.

**Tuya**

- **Ưu tiên** flash OpenBeken bằng tuya-cloudcutter, nạp qua Wi-Fi, không cần tháo máy. Sau đó thiết bị tự nói MQTT.
- Thiết bị không flash được: `hubd` dùng giao thức local, phiên bản 3.3/3.4/3.5, tham chiếu tinytuya. Local key lấy qua Smart Life.
- **Không tháo CB để flash qua cổng serial.**

## 9. Màn hình: giao diện

Màn hình là iPad Pro 10" (khoảng 1112×834 điểm, ngang). Bố cục 3 khu vực + giếng trời + năng lượng + nước RO; nút bấm cỡ ngón tay (≥ 56 px), chữ lớn, nền tối.

**Các trang**

| Trang | Nội dung |
|---|---|
| **Tổng quan** | PV / tải / lưới / % pin. Thời tiết và mưa. Cảnh báo đang mở. Hàng nút kịch bản |
| **Năng lượng** | Luồng công suất, áp từng cell (có tô màu cell lệch), biểu đồ 24 giờ / 7 ngày |
| **Khu vực** | Mỗi khu một thẻ: T/RH, PM2.5, CO2, lux, có người, công tắc của khu |
| **Tủ điện** | CB và công tắc Tuya xếp theo tủ. Thiết bị `protected` có khoá |
| **Báo động** | Khi có khói/gas, toàn màn hình chuyển đỏ, bật sáng tối đa, có nút tắt còi và xác nhận |

**Kịch bản một chạm:** Ra khỏi nhà · Đi ngủ · Tiết kiệm pin · Tắt còi.

**Chế độ đêm:**
- 22:00–06:00 giảm độ sáng.
- LD2410 ở phòng khách thấy người thì `hubd` gửi lệnh bật sáng.
- Không có người 2 phút thì tắt màn hình.

## 10. Tự động hoá v1

| Khi | Thì |
|---|---|
| Lưới < 10 W trong 30 s (mất điện lưới) | Telegram mức cảnh báo |
| Mất lưới **và** SOC < 30% | Cắt tải không thiết yếu (bình nóng lạnh, máy lạnh phụ). Telegram |
| PV dư > 1,5 kW **và** SOC > 95% trong 5 phút | Bật bình nóng lạnh/máy bơm. Tắt khi PV dư < 0,5 kW |
| Cell > 3,60 V, < 2,90 V, lệch > 50 mV, hoặc nhiệt BMS > 45°C | Telegram mức **khẩn**. Mục đích là báo trước khi BMS tự ngắt bảo vệ |
| Khói / nhiệt / gas | Còi, màn hình báo động, bật hết đèn, tắt quạt/điều hoà, Telegram **khẩn** |
| PM2.5 trong nhà > 35 µg/m³ trong 5 phút | Bật máy lọc. Tắt khi < 25 |
| PM2.5 ngoài trời > 55 (chỉ khi lắp PMS5003 ở giếng trời) | Nhắc đóng cửa sổ |
| CO2 > 1000 ppm trong 2 phút | Bật quạt hút. Tắt khi < 900 |
| Cảm biến mưa Zigbee báo mưa | Telegram "thu đồ", đóng rèm/mái che (nếu có), hoãn tưới |
| Node im lặng > 10 phút | Telegram mức cảnh báo |

Ngưỡng là giá trị khởi điểm (lựa chọn của mình), chỉnh lại sau 2 tuần chạy thật. Ngưỡng cell áp dụng cho pin LiFePO4.

**Một luật chỉ được bật khi:**
- Tải đó có thật và đã có thiết bị điều khiển. Việc này khảo sát ở Giai đoạn 0.
- Với luật cắt tải khi mất lưới: tải đó nằm trên ngõ backup. Tải ngoài ngõ backup đã mất điện sẵn rồi.
- Thiết bị điều khiển bằng cách cắt/cấp điện (máy lạnh, máy lọc) đã được thử là tự chạy lại khi có điện. Không tự chạy lại thì cần điều khiển IR hoặc thiết bị có điều khiển riêng.
- Máy bơm chỉ được tự bật nếu có phao hoặc rơ-le áp tự ngắt.

## 11. An toàn

- **Báo cháy là việc của hệ CBX, chạy độc lập.** Trung tâm chỉ **đọc** telemetry của tủ (mục 17), không điều khiển, không chặn, không sửa bản tin. Firmware tủ không đụng tới. Cảm biến khói tự làm (bụi mịn, buồng khói tái dùng) chỉ là lớp cảnh báo phụ, không phải thiết bị kiểm định.
- **Không tái dùng vỏ đầu báo cũ cho việc có 220V.** Vỏ có khe thoáng, chỉ chứa mạch 5V. Nguồn 220V→5V nằm trong hộp kín riêng giấu trên trần.
- Đánh dấu `protected` cho CB cấp điện cho máy Ubuntu, router và tủ lạnh. Code chặn mọi luật tắt các thiết bị này.
- Trung tâm là máy Ubuntu chạy dịch vụ khác: chỉ thao tác trong `/root/hubd` và 3 container của hệ này (mục 6).
- v1 không ghi gì vào Deye hay JK, và ràng buộc này có trong cấu hình firmware (mục 8).
- Đấu CB, tủ điện, RS485 trong tủ inverter: ngắt cả nguồn AC lẫn DC pin. Nên giao thợ điện.
- Wi-Fi: SSID IoT riêng. Đặt IP cố định (DHCP reservation) cho trung tâm, màn hình và các node.

## 12. Lộ trình

Mỗi giai đoạn tính theo buổi tối và cuối tuần. Tổng khoảng 6–8 tuần.

| # | Giai đoạn | Việc chính | Xong khi |
|---|---|---|---|
| 0 | Khảo sát (1–2 ngày) | **Deye:** model, số inverter, sơ đồ chân RS485, logger Wi-Fi cắm cổng nào. **Pin (3 pack):** model + hardware version từng pack, địa chỉ DIP, pack nào nối Deye và qua cổng nào (CAN/RS485), khoảng cách pin–inverter. **Camera:** model NVR và camera, số kênh, codec luồng phụ, có nhận diện người/xe không, ISAPI đã bật chưa. **Mạch nào nằm trên ngõ backup** (router, trung tâm, tủ lạnh, các tải sẽ cắt). **Tải trong mục 10:** có thật không, điều khiển bằng gì. **Tuya:** loại, chip, có trong danh sách cloudcutter không, thiết bị nào phải giữ app Smart Life. Sơ đồ khu vực. Đo sóng Wi-Fi ở từng vị trí node | Có bảng thiết bị `devices.md`. Sóng tại mọi vị trí node ≥ −70 dBm, hoặc đã có phương án AP |
| 1 | Nền tảng (1 tuần) | Máy Ubuntu: Docker Compose (Mosquitto + `hubd` + `tuya-bridge`). iPad mở web. `hubd` v0: MQTT → state → trang Tổng quan + SSE. 1 node thử (ESP32 + SCD41) | Hà hơi vào SCD41, CO2 và độ ẩm trên màn hình đổi trong ≤ 3 s. Rút internet vẫn chạy. Khởi động lại máy Ubuntu, 3 container tự lên ≤ 2 phút và iPad tự kết nối lại |
| 2 | Năng lượng (1 tuần) | Node năng lượng. Lưu lịch sử. Trang Năng lượng | PV/tải/lưới/SOC lệch ≤ 2% so với màn hình inverter. Đủ áp từng cell. Cập nhật ≤ 10 s. Có biểu đồ 24 giờ |
| 3 | Tuya (1–2 tuần, **rủi ro cao nhất**) | Flash OpenBeken cho máy được. Module Tuya local cho phần còn lại. Trang Tủ điện. `protected`. PIN | Rút internet, mọi CB/công tắc vẫn bật tắt được từ màn hình. Test chứng minh luật không tắt được thiết bị `protected` |
| 4 | Cảm biến khu vực + nước (1 tuần) | Node 3 khu + cảm biến Zigbee ở giếng trời + node RO. Hiệu chỉnh 24 giờ | Thắp nhang cạnh node, PM2.5/VOC tăng. Nhỏ nước lên cảm biến mưa Zigbee, có báo mưa. Sau hiệu chỉnh, T/RH giữa các node lệch ≤ 0,5°C / 3%. Ngâm hai đầu dò TDS vào dung dịch chuẩn 342 ppm, số đọc lệch ≤ 5% |
| 5 | An toàn + cảnh báo (1 tuần) | SLZB-06 + Zigbee2MQTT. Khói/nhiệt/gas/còi. Telegram. Màn hình báo động | Bấm test đầu báo: còi tại chỗ + màn hình đỏ + Telegram ≤ 5 s. Tắt trung tâm, đầu báo vẫn kêu. Mất internet, Telegram gửi bù khi có mạng |
| 6 | Tự động hoá + gia cố (1 tuần) | Luật v1, kịch bản. Root chỉ đọc. Watchdog. Backup hằng đêm (`sqlite3 .backup` + config) lên cloud bằng rclone. Tailscale. Diễn tập khôi phục | Mọi luật pass khi giả lập bằng `mosquitto_pub`. Dựng lại trên một máy khác từ backup ≤ 30 phút |

**Để sau v1:** panel ESP32-S3 4" ở các phòng, ghi Modbus vào Deye (từng thanh ghi, sau khi đã đọc ổn định ≥ 2 tuần), image Buildroot.

## 13. Rủi ro

| Rủi ro | Xử lý |
|---|---|
| Nhiều thiết bị Tuya không flash được | Viết module Tuya local (phần khó nhất của `hubd`). Lâu dài thay dần bằng thiết bị Zigbee/ESPHome |
| Logger Wi-Fi mới của Deye khoá truy cập local | Không ảnh hưởng, vì đọc qua RS485 |
| Bản ESPHome cộng đồng không đọc được mạng RS485 của 3 pack | Dự phòng BLE: một ESP32 giữ 3 kết nối |
| Router quá tải vì nhiều thiết bị Wi-Fi | Dùng AP riêng cho SSID IoT |
| Máy Ubuntu hỏng hoặc mất điện: **mất trung tâm** (iPad vẫn mở được nhưng không có dữ liệu) | Backup `hubd-db` + `/root/hubd`. Dựng lại trên máy x86 bất kỳ hoặc Pi 5 (build arm64) bằng `docker compose up -d`, mất ≤ 30 phút. Đầu báo khói vẫn tự kêu trong lúc đó |
| Cập nhật kernel linux-surface làm hỏng cảm ứng hoặc không boot được | Khoá phiên bản kernel. Giữ kernel cũ trong menu boot. Chỉ cập nhật chủ động |
| Pin thay (không chính hãng) phồng | Bật Battery Limit 50%. Mỗi tháng nhìn mép màn hình một lần |
| Máy Ubuntu quá tải khi chạy thêm camera và giọng nói (còn đang chạy VM) | Camera đã đo: 10 luồng phụ ≈ 3,5% một luồng CPU (mục 15.1). Giọng nói đo khi làm mục 15.2. Không đủ thì tách dịch vụ nặng sang máy khác |

## 14. Quyết định

**Chủ nhà đã chốt**
- Không dùng Home Assistant.
- Trung tâm là thiết bị chuyên dụng, nhỏ, tiết kiệm điện, OS tối giản.
- Cảm biến gửi về qua Wi-Fi nội bộ.
- Màn hình kết nối vào trung tâm.
- **Trung tâm = máy Ubuntu `192.168.1.254` có sẵn; màn hình = iPad Pro 10". Không dùng Surface làm trung tâm, chưa mua Pi 5.** (Thay quyết định cũ "gộp vào Surface".)

**Lựa chọn của mình (chủ nhà kiểm tra lại)**
- Zigbee (nếu dùng) là SLZB-06 qua LAN, không chiếm cổng USB của máy Ubuntu.
- Viết `hubd` bằng Go.
- Mosquitto chạy riêng.
- MQTT là chuẩn chung.
- ESPHome cho node.
- Zigbee chỉ dùng cho khói/gas/còi.
- Telegram để báo.
- v1 chỉ đọc Deye/JK.
- Debian tối giản trước, Buildroot để sau.
- Các ngưỡng tự động hoá ở mục 10.
- Cả 3 khu đo đủ: bụi (PMS7003), VOC/NOx (SGP41), CO2 + T/RH (SCD41), ánh sáng (BH1750).
- Giữ logger Wi-Fi của Deye.
- JK đọc qua RS485 nội bộ, BLE dự phòng.
- Trung tâm ≤ 10 W khi màn hình sáng là mục tiêu, không phải giới hạn cứng.

**Còn chờ chủ nhà trả lời (thay đổi thứ sẽ làm)**
1. Đầu báo khói/gas/còi dùng Zigbee, tức là thêm SLZB-06 + Zigbee2MQTT, được không? Nếu không, phải dùng đầu báo Wi-Fi chạy pin. Loại này phụ thuộc cloud Tuya cho phần thông báo, và trung tâm không nhận được báo khi mất internet.
2. Thiết bị Tuya flash OpenBeken sẽ **mất app Smart Life vĩnh viễn**, kèm lịch hẹn giờ và liên kết Google/Alexa. Thiết bị nào phải giữ app? Các thiết bị đó đi đường Tuya local, làm Giai đoạn 3 dài hơn.
3. Có trần ngân sách không? Câu trả lời quyết định có đo bụi/VOC ở mọi phòng hay chỉ ở phòng khách.

**Để người làm tự quyết**
- Chọn chân GPIO.
- Thiết kế hộp node.
- Chi tiết giao diện (màu, bố cục từng thẻ).
- Bảng thanh ghi Deye cụ thể.
- Schema SQLite chi tiết.
- Cấu trúc code của `hubd`.

## 15. Mở rộng: camera Hikvision và trợ lý giọng nói AI

Chủ nhà bổ sung ngày 2026-10-26. Cả hai phần **làm sau lõi** (Giai đoạn 1–6), mỗi phần có thiết kế chi tiết và kế hoạch riêng. Lõi phải chạy đủ khi hai phần này tắt.

### 15.1 Camera Hikvision

**Nguyên tắc: không làm lại việc NVR và camera đã làm.**
- Ghi hình vẫn do NVR đảm nhận, trung tâm không ghi.
- Nhận diện người/xe dùng tính năng có sẵn trên camera (nếu model có). Không chạy Frigate hay AI nhận diện trên trung tâm.

**Ba việc trung tâm làm:**

| Việc | Cách làm |
|---|---|
| Xem trực tiếp trên iPad | `go2rtc` (một file binary) chạy trên trung tâm, chuyển RTSP của NVR sang WebRTC/MSE cho trình duyệt. Dùng luồng phụ H.264 để không phải chuyển mã. Trình duyệt đi qua `hubd` (PIN), không gọi thẳng go2rtc hay NVR. Giao diện: widget **Camera** (ảnh chụp mỗi phút) và bảng xem trực tiếp từng camera |
| Nhận sự kiện | Cầu nối nhỏ `hikbridge` đọc luồng sự kiện ISAPI `/ISAPI/Event/notification/alertStream` của NVR, gửi MQTT `hik/<kênh>/<sự kiện>` = ON/OFF. `hubd` coi đó là cảm biến thường (ON/OFF = 1/0). Luật viết trong `config.yaml`, ví dụ: có người ở cổng thì bật đèn sân và báo Telegram (khung giờ, Telegram: chưa có trong `hubd`) |
| Ảnh kèm cảnh báo | Lấy ảnh chụp từ `/ISAPI/Streaming/channels/<id>/picture`, gửi kèm tin Telegram |

**Bảo mật camera:**
- Tạo tài khoản NVR riêng, chỉ có quyền xem. Mật khẩu để trong secrets.
- Không mở port NVR ra internet. Xem từ xa qua Tailscale.

**Tải khi chuyển tiếp (đo 2026-10-27, đầu ghi giả):** máy Ubuntu Ryzen 7 5700G (16 luồng), go2rtc 1.9.14 và hubd chạy trong Docker như bản thật, đầu ghi giả phát RTSP một clip H.264 704×576, 15 fps, ~510 kbit/s cho mỗi kênh. Người xem kéo MSE qua WebSocket của hubd (đường iPad đi khi không dùng WebRTC); CPU đọc từ bộ đếm cgroup trong 60 giây; mọi người xem nhận đủ 528 kbit/s, không phải nối lại. Đơn vị: % của **một** luồng CPU.

| Kịch bản | go2rtc | hubd (proxy) | RAM go2rtc |
|---|---|---|---|
| Không ai xem | 0% | 0% | — |
| 4 kênh × 1 người xem | 0,86% | 0,63% | ~48 MiB |
| 10 kênh × 1 người xem | 2,16% | 1,35% | ~53 MiB |
| 4 kênh × 3 người xem (12 luồng ra) | 1,47% | 1,39% | ~49 MiB |

Kết luận: chuyển tiếp không chuyển mã gần như không tốn CPU (10 kênh ≈ 3,5% một luồng, ≈ 0,2% cả máy), không cần tách sang máy khác. **Chưa đo:** người xem WebRTC (go2rtc mã hoá SRTP thêm) và luồng phụ thật của đầu ghi (độ phân giải, bitrate có thể khác). Giải mã video do iPad làm (màn hình không còn là Chromium trên Surface).

### 15.2 Trợ lý giọng nói AI

**Chủ nhà muốn 3 việc:** đọc cảnh báo thành tiếng; nghe lệnh; xử lý tác vụ bằng giọng nói.

**Nguyên tắc:**
- Lõi (luật, cảnh báo, điều khiển) vẫn chạy cục bộ. Trợ lý là lớp thêm vào.
- Trợ lý **không được** điều khiển thiết bị hay kịch bản cần PIN, và không tắt được thiết bị `protected`. Nó dùng token riêng, quyền bằng người dùng chưa nhập PIN.
- `hubd` publish mọi cảnh báo (`notify` của luật) lên MQTT `hubd/alert`, xác nhận lên `hubd/alert/ack`. Phần giọng nói chỉ cần nghe hai topic này.
- Chương trình giọng nói `voiced` chạy trên máy Ubuntu (mic/loa lấy từ thiết bị nghe/nói ở phòng khách — **chưa chọn**, xem bảng dưới), nói chuyện với `hubd` qua MQTT + API.

**Luồng xử lý:**

```
Mic ─► nút nói (iPad) / wake word (loa vệ tinh, sau) ─► chuyển giọng nói thành chữ (cục bộ) ─► hiểu ý
                                                          ├─ lệnh cố định ─► khớp cục bộ ─► API hubd
                                                          └─ câu tự do ───► Claude (tool use) ─► API hubd
Loa ◄─ đọc câu trả lời / cảnh báo bằng TTS tiếng Việt ◄──────────────────────┘
```

| Phần | Phương án đề xuất | Ghi chú |
|---|---|---|
| Đọc cảnh báo | Câu cảnh báo cố định (`notify` trong `rules:` của `config.yaml`) được `voiced` tạo sẵn âm thanh lúc khởi động bằng VieNeu-TTS (giọng Hải Đăng) | Đọc được cả khi mất internet |
| Lệnh cố định | Khớp cục bộ theo tên thiết bị/kịch bản trong `config.yaml`: "bật quạt phòng khách", "ra khỏi nhà", "pin còn bao nhiêu" | Chạy được khi mất internet |
| Câu tự do | Claude API, model `claude-opus-5-5`, effort thấp để trả lời nhanh. Các tool: `get_state`, `query_history`, `set_device`, `run_scene`, `ack_alarm`, đều gọi API của `hubd` | Cần internet. Câu hỏi dạng chữ và trạng thái thiết bị được gửi lên Anthropic |
| Câu trả lời động | TTS lúc chạy bằng VieNeu-TTS cục bộ (bản int8), cache theo câu. Không cần TTS cloud | Đã đo trên máy Ubuntu: 0,4 s cho câu ngắn, 1,2–1,4 s cho câu ~3,7 s (mục 15.2.1) |
| Thiết bị nghe/nói | Phòng khách: iPad (có mic + loa, nhưng Safari hạn chế nghe nền) hoặc loa vệ tinh ESP32-S3. Các phòng khác: loa vệ tinh ESP32-S3 có mic (tuỳ chọn) | Chờ chủ nhà quyết |

**Còn chờ chủ nhà quyết:**
1. Nghe/nói ở đâu: chỉ phòng khách (iPad hoặc 1 loa vệ tinh), hay mỗi phòng một loa vệ tinh? Trong lúc chờ, bản đầu dùng iPad: nút nói trên dashboard, cảnh báo đọc trên iPad đang mở dashboard.
2. Chuyển giọng nói thành chữ chạy cục bộ hay gửi âm thanh lên cloud? Đề xuất **cục bộ** và bản đầu đã làm cục bộ: trung tâm giờ là máy Ubuntu Ryzen 7 5700G (không còn m3), đo được ~27 ms mỗi câu (mục 15.2.1), âm thanh không rời khỏi nhà. Cloud chỉ còn là dự phòng nếu dùng thật thấy nhận sai nhiều.
3. Cách có HTTPS cho iPad (Safari chỉ cho dùng micro trên trang HTTPS): CA tự ký cài lên iPad (đã có `scripts/tls-selfsigned.sh`), hay chứng chỉ thật cho một tên miền trỏ về IP nội bộ.

#### 15.2.1 Bản đầu đã làm và số đo (2026-10-27)

**Đã làm:** API trợ lý trong `hubd` (`/api/assistant/*`, token `HUBD_ASSISTANT_TOKEN`, chặn thiết bị/kịch bản cần PIN ngay trong `hubd`), luật `notify` → `hubd/alert`, container `voiced`, nút nói trên dashboard, HTTPS tuỳ chọn cổng 8443. Vận hành: README mục "Trợ lý giọng nói".

**Đo trên máy Ubuntu** (Ryzen 7 5700G 8 nhân/16 luồng, 14 GiB RAM, đang chạy VM và dịch vụ khác; trong container, 4 luồng; mẫu là 6 câu lệnh do VieNeu-TTS đọc bằng 6 giọng khác nhau + 4 câu ghi âm thật từ bộ thử của sherpa-onnx):

| Chuyển giọng nói thành chữ | Thời gian / câu (1–4 s) | RTF | 6 câu lệnh mẫu: đúng hẳn / chỉ mất dấu / sai |
|---|---|---|---|
| **sherpa-onnx Zipformer vi 30M int8** (2026-02-09, đã chọn, 26 MB) | **15–47 ms**, trung bình 27 ms | 0,012 | 4 / 1 / 1. Mất dấu: "RA KHỎI NHA" (khớp lệnh vẫn đúng vì so chữ không dấu). Sai: "Pin" → "TIỀN" |
| sherpa-onnx Zipformer vi int8 (2025-04-20, 60 MB) | 20–53 ms, trung bình 33 ms | 0,016 | 3 / 1 / 2 (thêm "đèn" → "NỀN") |
| faster-whisper small int8 | 1,3–4,5 s | 0,81 | 2 / 0 / 4 |
| faster-whisper large-v3-turbo int8 | 5,9–6,3 s | 2,86 | 2 / 1 / 3 |

| Đọc thành tiếng (VieNeu-TTS v3 Turbo, giọng Hải Đăng) | Câu 0,9 s | Câu 1,1 s | Câu 3,6–3,8 s | Nạp mô hình |
|---|---|---|---|---|
| **ONNX int8 (đã chọn)** | 0,40 s | 0,37 s | 1,19–1,39 s | 1,5 s |
| ONNX fp32 | 0,67 s | 0,57 s | 1,58–1,64 s | 2,0 s |

Hotword của sherpa-onnx (thử với "PIN") không sửa được lỗi "Pin" → "TIỀN" trên mẫu này. Chạy thử end-to-end qua `hubd` (stack thử riêng): "Tắt CB tổng bếp." nghe thành "hcb tổng bếp" (chữ cái Latin như "CB" dễ sai), "Đèn kho đang bật không?" nghe thành "đen khổ đang bật không" nhưng vẫn khớp đúng nhờ so chữ không dấu. Mẫu là giọng tổng hợp; độ chính xác với giọng thật qua micro iPad **chưa đo**.

Cả vòng qua `hubd` (nhận WAV → STT → khớp lệnh → gửi lệnh → trả âm thanh): ~20 ms khi câu trả lời đã có trong cache (STT 14–28 ms), ~0,5 s khi phải đọc câu mới. `voiced`: image 2,05 GB, RAM ~615 MiB sau khi nạp mô hình, ~860 MiB sau vài lượt.

**Dự phòng:** STT cục bộ nhận sai nhiều với giọng thật thì (1) thêm tên gọi khác cho thiết bị trong `config.yaml`, (2) thử bản Zipformer 2025-04-20 hoặc mô hình lớn hơn của sherpa-onnx, (3) cuối cùng mới gửi âm thanh lên STT cloud (âm thanh rời khỏi nhà, cần chủ nhà đồng ý). TTS cục bộ đủ nhanh, không cần dự phòng cloud; lỗi TTS thì vẫn trả câu trả lời dạng chữ.

**Chưa làm:** loa vệ tinh ESP32-S3 + wake word (chờ quyết định 1); `voiced` đã nhận WAV qua `POST /voice/ask` nên loa vệ tinh chỉ cần gửi WAV sau wake word và phát file trả về. Cảnh báo `urgent` chưa lặp lại tới khi xác nhận. Gọi Claude thật chưa chạy (chờ khoá API).

## 16. Chất lượng nước uống (máy lọc RO)

Chủ nhà bổ sung ngày 2026-10-26. Hồ cá đã bỏ khỏi phạm vi. Phần này làm cùng Giai đoạn 4.

**Không phải sửa code `hubd`:**
- mỗi chỉ số là một entity;
- cảnh báo viết trong `rules.yaml`;
- giá trị dẫn xuất (tỉ lệ loại bỏ TDS, số lít đã lọc) do firmware tính.

### 16.1 Đo được gì, không đo được gì

| Chỉ số | Đo liên tục? | Ghi chú |
|---|---|---|
| TDS trước màng và ngay sau màng | ✅ 2 đầu dò TDS gắn ống 1/4" | Cho biết màng RO còn tốt hay không |
| Nhiệt độ nước | ✅ DS18B20 dán sát ống | Dùng để bù nhiệt cho TDS |
| Số lít nước vào máy | ✅ cảm biến lưu lượng YF-S401 đặt ở đường nước vào | Dùng để nhắc thay lõi |
| Rò nước dưới máy | ✅ cảm biến rò nước Zigbee | Báo mức khẩn |
| Clo dư | ❌ cảm biến đo liên tục là hàng công nghiệp | Dùng que thử |
| Vi khuẩn (coliform, E. coli) | ❌ không đo liên tục được | **Gửi mẫu xét nghiệm định kỳ.** TDS thấp không có nghĩa là nước sạch vi sinh |

### 16.2 Node RO

**Vị trí đầu dò TDS:**
- đầu dò "trước" đặt ở đường nước vào;
- đầu dò "sau" đặt **ngay sau màng RO, trước các lõi bù khoáng**. Máy RO bán ở Việt Nam hay có lõi khoáng/ORP/alkaline phía sau màng, làm TDS ở vòi tăng trở lại. Đo ở vòi thì không đánh giá được màng.

**Cách đọc:**
- **Đọc lần lượt từng đầu dò** bằng cách cấp nguồn mạch TDS qua chân GPIO. Hai đầu dò trên cùng một đường nước đọc cùng lúc sẽ nhiễu nhau.
- Đọc tín hiệu qua ADS1115.
- Hiệu chỉnh bằng dung dịch chuẩn 342 ppm.

**Firmware tính:**
- tỉ lệ loại bỏ = (1 − TDS sau / TDS trước) × 100%;
- tổng số lít từ lần thay lõi gần nhất.

**Vật liệu và vị trí:**
- Hai đầu dò TDS lấy từ **bộ đo TDS gắn ống làm sẵn cho máy RO**. Đầu dò này vốn được thiết kế để lắp vào đường nước lọc.
- Cảm biến lưu lượng YF-S401 **đặt ở đường nước vào, trước các lõi lọc**, để không chạm vào nước đã lọc.
- Ngưỡng nhắc thay lõi tính theo **số lít nước vào máy**. Tỉ lệ nước thành phẩm so với nước vào đo một lần lúc lắp đặt.

### 16.3 Cảnh báo (ngưỡng khởi điểm)

| Khi | Thì |
|---|---|
| Tỉ lệ loại bỏ < 90%, hoặc TDS ngay sau màng > 50 ppm | Nhắc kiểm tra hoặc thay màng RO |
| Tổng số lít vượt mức hãng khuyến cáo cho từng lõi | Nhắc thay lõi |
| Rò nước dưới máy RO | **Khẩn** |

### 16.4 Ghi ở Giai đoạn 0

- Máy RO có những lõi nào nằm sau màng (lõi khoáng, ORP, alkaline).
- Hãng khuyến cáo thay từng lõi sau bao nhiêu lít hoặc bao nhiêu tháng.

## 17. Tích hợp hệ báo cháy CBX (chỉ-đọc, không sửa firmware)

Chủ nhà bổ sung ngày 2026-10-26. Mục tiêu: trung tâm giám sát kết nối, pin và trạng thái báo cháy/khói từng đầu báo của hệ 3SCORP CBX, mà **không sửa firmware tủ**.

### 17.1 Cơ sở (từ đọc firmware `CBXlrg-vosat_1.bin`, chỉ đọc)

- Firmware ESP32, ESP-IDF v4.3.6. Tủ gửi telemetry lên máy chủ **`cbx.vn` bằng UDP** (qua Wi-Fi, hoặc 4G SIMCom nếu có SIM).
- Địa chỉ `cbx.vn` **ghi cứng trong firmware**, không có ô đổi trong trang Setting.
- Tủ **chờ gói UDP phản hồi**; không nhận được thì tự ngắt Wi-Fi (log "Disconnected to wifi cause not recv udp").
- Bản tin **nhị phân, chưa giải mã**. Firmware có trường từng node: `id`, `rssi`, `snr`, `voltage`, `pwr_tx`, và sự kiện ALARM / End ALARM / Hush / Test.
- Tủ có API JSON cục bộ: `GET /nodes_info`, `/nodes_info_change`, `/system_info`.

### 17.2 Cách tích hợp

1. **Cho tủ vào Wi-Fi nhà** (phải là đường Wi-Fi, không phải 4G, để router kiểm soát DNS). Nếu tủ có SIM thì rút/tắt SIM.
2. **Router trỏ DNS `cbx.vn` → IP trung tâm** (local DNS / host override).
3. **Trung tâm chạy `cbxd`**: bộ nghe UDP nhận bản tin của tủ, **phản hồi** để tủ giữ kết nối, giải mã rồi đẩy lên MQTT `cbx/<id_node>/...` (link, voltage, alarm). `hubd` coi đó là entity thường.
4. **Bổ sung:** `hubd` cũng gọi `GET /nodes_info` định kỳ để lấy pin và chất lượng kết nối, phòng khi giải mã UDP chưa đủ.

### 17.3 Việc phải làm và rủi ro

- **Giải mã gói UDP:** chưa làm được từ firmware. Cần **bắt gói thật**: vài gói lúc bình thường + vài gói lúc bấm TEST một đầu báo, đối chiếu byte đổi để tìm trường ALARM/voltage.
- **Gói phản hồi giữ kết nối:** phải học từ bản tin bắt được. Nếu tủ đòi phản hồi có định dạng/mã hoá riêng mà `cbxd` không tái tạo được, tủ sẽ báo lỗi kết nối — **nhưng phần dò cháy và hú còi tại chỗ vẫn chạy**. Luôn có sẵn thao tác **đảo DNS về như cũ** để quay lại.
- **4G:** nếu tủ rớt về 4G thì DNS của SIM không override được, telemetry không về trung tâm. Phải đảm bảo tủ chạy Wi-Fi.
- **Mất giám sát của hãng:** sau khi trỏ DNS, `cbx.vn` không nhận dữ liệu nữa, app/giám sát 3SCORP ngừng. Chủ nhà xác nhận không cần.
- **An toàn:** `cbxd` chỉ **đọc và phản hồi giữ-sống**, không chặn, không sửa nội dung bản tin báo cháy. Không đụng firmware. Hệ CBX vẫn là hệ báo cháy chính.

### 17.4 Phần cứng

Không mua gì. Chỉ phần mềm (`cbxd`) + cấu hình router. Nếu tủ chỉ bắt được DNS khi ở cùng mạng mà router không cho override, phương án dự phòng là một AP/định tuyến nhỏ đặt trước tủ — ghi ở Giai đoạn 0 sau khi biết model router.
