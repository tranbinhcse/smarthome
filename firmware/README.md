# Firmware node ESP32 (ESPHome → MQTT)

Thiết kế mục 8 và 16; linh kiện theo [danh-sach-mua.md](../docs/danh-sach-mua.md) (bản mới hơn, thắng khi khác thiết kế).
Không dùng Home Assistant (`api:`), node nói MQTT thẳng với Mosquitto ở `192.168.1.254`.

| File | Node / vai trò |
|---|---|
| `common.yaml` | Gói chung: Wi-Fi + AP dự phòng, MQTT (tài khoản riêng), OTA có mật khẩu, `wifi`, `uptime` |
| `khongkhi.yaml` | Gói cảm biến không khí: SCD40, SGP41, PMS7003, BH1750 |
| `ir.yaml` | Gói hồng ngoại: thu học mã, phát Pronto, máy lạnh qua `climate_ir` |
| `node-cafe.yaml` | Khu cafe: không khí + hồng ngoại + LD2410C |
| `node-lamviec.yaml`, `node-ngu1.yaml` | Không khí + hồng ngoại |
| `node-gieng-troi.yaml` | SHT40 + VEML7700 + tấm mưa (ngoài trời, hộp IP65) |
| `node-ro.yaml` | 2 đầu dò TDS (ADS1115) + DS18B20 + YF-S401 + tấm dò rò nước |

Sơ đồ chân và núm hiệu chỉnh ghi ở đầu từng file.

## Nạp lần đầu

```bash
cp firmware/secrets.example.yaml firmware/secrets.yaml   # điền Wi-Fi, mật khẩu AP/OTA (không commit)
python3 scripts/node_mqtt_user.py node-cafe              # sinh mật khẩu MQTT riêng vào secrets.yaml, in dòng băm
```

Đưa dòng băm lên trung tâm (chỉ bản băm đi qua mạng, mật khẩu gốc ở lại PC), Mosquitto nạp lại khi nhận HUP:

```bash
python3 scripts/node_mqtt_user.py node-cafe | ssh root@192.168.1.254 'f=/root/hubd/mosquitto/passwd; read -r l; sed -i "/^${l%%:*}:/d" $f; echo "$l" >> $f; docker kill -s HUP hub-mosquitto'
```

```bash
cd firmware && esphome run node-cafe.yaml     # lần đầu cắm USB; các lần sau tự nạp qua OTA
```

## Topic

| Topic | Nội dung |
|---|---|
| `node-<khu>/status` | `online` / `offline` (LWT) |
| `node-<khu>/sensor/<tên>/state` | Số đo; `config.yaml` của hubd khai báo entity theo topic này |
| `node-<khu>/ir/send` | hubd gửi chuỗi Pronto đã học, node phát |
| `node-<khu>/ir/ac` | hubd gửi mã nút máy lạnh (`on`, `off`, `temp_24`, `mode_cool`, `fan_2`, `swing`, `sleep`, `turbo`) |
| `node-<khu>/ir/learned` | node gửi Pronto của tín hiệu remote vừa thu |
| `node-ro/button/da_thay_loi/command` | gửi `PRESS` sau khi thay lõi lọc, đếm lại số lít |

Cảm biến 0/1 (`co_nguoi`, `mua`, `ro_nuoc`) gửi dạng số để hubd ghi lịch sử và viết luật (`above: 0.5`).

## Hồng ngoại

Máy lạnh hãng có trong ESPHome: đặt `ac_platform` trong file node (danh sách ở đầu `ir.yaml`), khai báo thiết bị
`kind: ac`, `command_topic: node-<khu>/ir/ac` trong `config.yaml`. Dashboard bấm nút nào, hubd chuyển mã đó cho node.

TV, loa, quạt (và máy lạnh hãng không hỗ trợ) dùng mã học được:

1. Nghe topic học mã trên trung tâm:
   ```bash
   ssh root@192.168.1.254 'cd /root/hubd && . ./.env && docker exec hub-mosquitto mosquitto_sub -u "$HUBD_MQTT_USER" -P "$HUBD_MQTT_PASS" -t "node-cafe/ir/learned"'
   ```
2. Chĩa remote gốc vào mắt thu của node, bấm từng nút; mỗi nút in ra một chuỗi `0000 006D ...`.
3. Chép vào `codes:` của thiết bị trong `config.yaml`, khoá là mã nút dashboard, rồi `docker compose restart hubd`:
   `power`, `vol_up`, `vol_down`, `mute`, `input`, `ch_up`, `ch_down`, `up`, `down`, `left`, `right`, `ok`,
   `back`, `home`, `menu` (TV); `speed_1..3`, `swing`, `timer_1/2/4`, `mode_normal/natural/sleep` (quạt);
   `play`, `next`, `prev`, `src_bt/aux/usb` (loa). Nút chưa học: hubd từ chối, dashboard báo chưa gửi được.

Hồng ngoại một chiều: hubd giữ trạng thái giả định theo lệnh gần nhất. Bấm remote gốc thì dashboard không biết.
Thiết bị `protected` không nhận lệnh tắt hồng ngoại nào (người bấm lẫn luật).

## Hiệu chỉnh (Giai đoạn 4)

| Việc | Cách |
|---|---|
| Nhiệt/ẩm trong nhà | Đặt các node cạnh một nhiệt kế chuẩn 24 giờ, chỉnh `scd_t_offset`, `rh_offset` (lệch ≤ 0,5 °C / 3 %) |
| Ánh sáng | `lux_factor` (vỏ đầu báo), `glass_factor` (nắp hộp giếng trời) so với máy đo lux |
| Mưa / rò nước | Nhỏ nước lên tấm, xem `mua_v` trong log, chỉnh `rain_v` / `leak_v` |
| TDS | Ngâm từng đầu dò vào dung dịch 342 ppm: `k mới = k cũ × 342 / số đọc` (lệch ≤ 5 %) |
| Lưu lượng | Hứng đúng 1 L, chỉnh `pulses_per_l`; đo `product_ratio` lúc lắp |
