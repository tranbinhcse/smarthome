# Inverter qua RS485 (Modbus RTU) — chỉ đọc

Node năng lượng (`../node-nangluong.yaml`) nạp **một** gói theo hãng. Mọi gói đăng cùng một lược đồ MQTT, nên đổi hãng chỉ là đổi một dòng `packages:`; `hubd`, `config.yaml` và dashboard giữ nguyên.

## Lược đồ MQTT chung

Topic `nangluong/inverter/<khoá>`, payload là số (ESPHome giữ retain). Node báo `nangluong/status` = `online`/`offline`.

| Khoá | Đơn vị | Ý nghĩa | Entity hubd |
|---|---|---|---|
| `pv_power` | W | tổng các chuỗi PV | `deye.pv` |
| `load_power` | W | tải nhà | `deye.load` |
| `grid_power` | W | **+ mua / − bán** | `deye.grid` |
| `battery_power` | W | **+ sạc / − xả** | `deye.battery` |
| `battery_soc` | % | SOC pin theo inverter | `deye.soc` |
| `grid_voltage` | V | điện áp lưới (pha 1) | `deye.grid_v` |
| `grid_frequency` | Hz | tần số lưới | `deye.grid_hz` |
| `temperature` | °C | nhiệt độ tản nhiệt inverter | `deye.temp` |
| `pv_today` · `load_today` | kWh | PV, tải trong ngày | `deye.pv_today` · `deye.load_today` |
| `buy_today` · `sell_today` | kWh | mua, bán lưới trong ngày | `deye.buy_today` · `deye.sell_today` |
| `charge_today` · `discharge_today` | kWh | sạc, xả pin trong ngày | `deye.charge_today` · `deye.discharge_today` |

Id giữ tiền tố `deye.` vì dashboard đang dùng; nó là tên của "inverter", không phụ thuộc hãng. Khoá nào hãng không có thì gói không đăng, `hubd` hiện "—", lý do ghi ở `fixtures/<gói>.json` → `missing`.

## Các gói

| Gói | Máy | Bus | Ghi chú |
|---|---|---|---|
| `deye-1pha.yaml` | Deye SUN-xK-SG0xLP1 (1 pha) | 9600, holding | **mặc định** |
| `deye-3pha.yaml` | Deye SG04/05LP3, SG01/02HP3 (3 pha) | 9600, holding 5xx–6xx | |
| `luxpower.yaml` | LuxPower SNA 3–6K, LXP-LB-EU | 19200, input, bank 40 thanh ghi | tải và kWh tải là số tính |
| `lumentree.yaml` | Lumentree SUNT-3.6–6.0KW-H | 9600, holding + input (kWh) | không có `sell_today` |

Nguồn thanh ghi và các điểm phải xác nhận nằm ở đầu từng file. Tóm tắt:

- **Deye:** đối chiếu chéo Lewa-Reka/esphome-deye-inverter, klatremis/esphome-for-deye (3 pha) và kellerza/sunsynk. Đã đủ tin cậy; chỉ cần biết máy 1 hay 3 pha và có tải phía lưới (CT ngoài) không.
- **LuxPower:** tài liệu "Modbus RTU Protocol" V58 của Lux chỉ mô tả khung có số serial (dùng cho dongle); sáu dự án cộng đồng đọc được bằng khung RTU chuẩn. **Chưa ai thử trên SNA 5000/6000 hay LXP-5K.** Nếu máy không trả lời: dùng paulsteigel/luxpower_sna (ESPHome đọc dongle qua TCP 8000) hoặc lxp-bridge trên máy Ubuntu.
- **Lumentree:** bảng thanh ghi từ syssi/esphome-lumentree (giải mã qua BLE, chạy trên SUNT-6.0KW-H); qua RS485 chỉ một người thử (kthemall/lumentree_rs485, cổng RJ45 "Remote CT"). Nếu RS485 không đọc được: dùng component `lumentree_ble` của syssi qua Bluetooth (một kết nối, app Lumentree mất kết nối trong lúc đó, bỏ mọi thực thể ghi).

## Đấu dây và ràng buộc

- ESP32 GPIO17 → DI (TX), GPIO16 ← RO (RX), 3V3, GND; A/B của mạch RS485 vào A/B của cổng inverter, nối GND chung. Mạch tự đảo chiều nên không cần chân DE/RE.
- **Đối chiếu sơ đồ chân RJ45 trong manual đúng model trước khi bấm cáp.** Không nối chân +12 V của cổng (Lumentree chân 1–2) vào mạch.
- **Một bus chỉ một master.** Nếu logger/dongle Wi-Fi dùng chung bus với cổng ESP32 cắm vào thì không cắm song song; LuxPower cho phép nếu hỏi thưa (≥ 60 s).
- **v1 chỉ đọc**: gói chỉ được khai báo `uart`, `modbus`, `modbus_controller`, `sensor` (platform `modbus_controller` đọc holding/input, hoặc `combination`). Không `number/select/switch/output/button`, không `lambda`, không `custom_command`, không `on_*`. `go test ./...` (`TestInverterFirmwareReadOnly`) chặn mọi thứ ngoài danh sách đó.

## Kiểm thử không cần phần cứng

```bash
go test -run Inverter ./...                       # chỉ đọc, đủ lược đồ, giải mã fixtures/<gói>.json
python3 -m unittest discover -s scripts           # script giả lập gửi đúng gói MQTT
esphome config firmware/node-nangluong.yaml       # cần firmware/secrets.yaml (chép từ secrets.example.yaml)
```

`fixtures/<gói>.json` là bộ thanh ghi dựng tay theo bảng thanh ghi (chưa phải dump thật) cùng số phải ra. Khi lắp máy thật: đọc một lần, ghi lại thanh ghi thô và số trên màn hình inverter vào fixture; nghiệm thu Giai đoạn 2 là lệch ≤ 2 %.

Giả lập node khi chưa có ESP32 (trỏ vào broker thử, không phải broker đang chạy thật):

```bash
MQTT_USER=node MQTT_PASS=... python3 scripts/gia_lap_inverter.py --host 127.0.0.1 --port 1883 --loop 10
```

## Thêm hãng khác

1. Chép một gói gần giống thành `<hãng>.yaml`, giữ nguyên id `uart_inverter`, `modbus_inverter`, `inverter` và các `state_topic`.
2. Khai báo thanh ghi, ghi nguồn ở đầu file, đổi dấu cho đúng lược đồ (`multiply: -1` hoặc `combination` `linear`).
3. Thêm `fixtures/<hãng>.json`; `go test` tự quét gói mới.
4. Hãng không có Modbus RTU đọc được (chỉ có cloud hoặc giao thức riêng): viết bộ nối riêng ra cùng lược đồ MQTT, `hubd` không cần sửa.
