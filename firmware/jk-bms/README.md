# JK BMS: 3 pack JK-PB song song → MQTT

Package ESPHome cho node năng lượng (thiết kế mục 8). **Chỉ đọc**: không có thực thể switch/number/button nào, bản RS485
còn không nối chân TX nên ESP32 không phát gì lên bus. `TestJKFirmwareReadOnly` giữ hai ràng buộc này. (Bản RS485 có
khai báo nền tảng `switch`/`number` của `jk_rs485_bms` ở dạng rỗng chỉ vì header của component luôn include mã của
chúng; test chỉ cho phép đúng dạng rỗng đó.)

| File | Vai trò |
|---|---|
| `jk-bms.yaml` | Bản chính: nghe mạng RS485 nội bộ giữa các pack (`txubelaxu/esphome-jk-bms`, `jk_rs485_sniffer` + `jk_rs485_bms`) |
| `jk-bms-ble.yaml` | Dự phòng: 3 kết nối BLE (`syssi/esphome-jk-bms`, `jk_bms_ble`, `JK02_32S`). Cùng id, cùng topic |
| `jk-pack.yaml`, `jk-pack-ble.yaml` | Số đo một pack, include 3 lần với `vars: { n: <địa chỉ DIP> }` |
| `jk-total.yaml`, `jk_total.h` | Tổng 3 pack (template sensor) và hàm gộp; `jk_total_test.cpp` là test của hàm |

## Dùng trong node

`firmware/node-nangluong.yaml` (session inverter sở hữu) chỉ cần:

```yaml
packages:
  jk: !include jk-bms/jk-bms.yaml        # hoặc jk-bms/jk-bms-ble.yaml
```

Node phải nằm ở `firmware/` vì ESPHome tính đường dẫn `includes` (`jk-bms/jk_total.h`) từ thư mục của file YAML chính.
Node lo `wifi`, `mqtt`, `logger`, `esp32`. Substitution chỉnh được (đặt trong `substitutions:` của node):

| Tên | Mặc định | Ý nghĩa |
|---|---|---|
| `jk_rx_pin` | `GPIO26` | Chân nhận dữ liệu bus JK (UART thứ hai; UART kia của Deye) |
| `jk_p0_ah`, `jk_p1_ah`, `jk_p2_ah` | `280` | Dung lượng danh định từng pack (Ah), trọng số SOC chung |
| `jk_alarm_mask` | `0xFFFFFFFF` | Bit lỗi BMS được đếm; xoá bit nào báo thường trực mà vô hại |

Bản BLE đọc MAC từ `firmware/secrets.yaml` (đã gitignore): `jk_p0_mac`, `jk_p1_mac`, `jk_p2_mac`.

## Đấu dây RS485 (ngắt cả AC lẫn DC pin trước khi làm, nên giao thợ điện)

- DIP: pack master địa chỉ **0**, hai pack còn lại **1** và **2**. Master tự hỏi hai pack kia; ESP32 nghe cả hỏi lẫn đáp.
- Các pack nối chuỗi qua cổng RS485 nội bộ; ESP32 cắm vào **đầu chuỗi** còn trống. Không cắm vào cổng đang nối inverter (CAN/RS485 của Deye).
- Mạch TTL→RS485 tự đảo chiều (mạch thứ hai trong danh sách mua): A, B, GND sang cổng pack; **RO/RXD → `jk_rx_pin`**;
  **để trống DI/TXD**. Cấp 3,3 V cho mạch (chân ESP32 không chịu 5 V).
- Chân RJ45 của JK-PB theo README của `txubelaxu/esphome-jk-bms` (1 = B, 2 = A, 3 = GND trên một cổng); đối chiếu manual
  của đúng model trước khi bấm cáp.

Lắp xong, log ESPHome phải thấy cả 3 địa chỉ, `nangluong/jk/online` = 3, SOC và điện áp từng pack khớp app JK.

## Lược đồ MQTT (retain, mỗi 10 s)

Tổng ở `nangluong/jk/<số đo>`, từng pack ở `nangluong/jk/p<n>/<số đo>` (n = địa chỉ DIP). Entity hubd tương ứng:
`jk.<số đo>` và `jk.p<n>.<số đo>` trong `config.yaml`. Pack mất kết nối gửi `nan` (hubd giữ nguyên để hiển thị,
không ghi lịch sử, không đưa vào luật) và `errors` = `Offline`.

| Số đo | Đơn vị | Từng pack | Tổng 3 pack |
|---|---|---|---|
| `soc` | % | SOC của BMS | Σ(SOC × Ah) / Σ Ah: trung bình theo dung lượng |
| `power` | W | điện áp × dòng, **+ sạc / − xả** | cộng dồn |
| `current` | A | + sạc / − xả | cộng dồn |
| `voltage` | V | điện áp pack | trung bình (song song nên gần bằng nhau) |
| `temp` | °C | nóng nhất trong T1, T2, MOS | pack nóng nhất |
| `cell_min` / `cell_max` | mV | cell thấp / cao nhất | thấp / cao nhất cả 3 pack |
| `cell_delta` | mV | cao − thấp trong pack | pack lệch nhiều nhất |
| `cycles` | | số chu kỳ | pack nhiều nhất |
| `alarms` | | số bit lỗi đang bật | cộng dồn |
| `errors` | chữ | tên lỗi, "" khi không lỗi | — |
| `online` | | — | số pack đang có số đo (0–3) |

Tổng chỉ gộp các pack đang có số đo; `online` < 3 nghĩa là tổng đang thiếu pack.

## Cảnh báo

Luật trong `config.yaml` (mục `rules`, id `pin_*`) đọc các số đo tổng, có `for` và `clear_*` chống dao động.
Khi bật, hubd đặt state `alert:<id>` và dashboard hiện ở chuông. Ngưỡng khởi điểm cho LiFePO4 (thiết kế mục 10):
SOC < 20 %, cell < 2,90 V hoặc > 3,60 V, lệch > 50 mV, nóng > 45 °C, BMS báo lỗi, mất pack quá 2 phút.

## Thử khi chưa có phần cứng

- `go test ./...` (thư mục `hubd`): `TestJKFirmwareTopicsMatchConfig` so topic của cả hai bản firmware với `config.yaml`;
  `TestJKFixtures` phát lại 6 kịch bản `testdata/jk/*.txt` vào hub dựng từ `config.yaml` thật và kiểm đúng bộ cảnh báo,
  mỗi luật `pin_*` phải có kịch bản làm nó bật; `TestJKTotalCpp` dịch và chạy `jk_total_test.cpp`.
- Giả lập node trên một stack thử: `scripts/jk-sim.sh testdata/jk/<kịch bản>.txt -h <broker> -p <cổng> -u node -P <mật khẩu>`.
  Không phát vào broker đang chạy thật (số đo giả được retain).
- Kiểm cú pháp, biên dịch firmware: đặt một node thử ở `firmware/` include package này rồi
  `docker run --rm -v "$PWD/firmware:/config" ghcr.io/esphome/esphome:stable config <node>.yaml` (hoặc `compile`).
