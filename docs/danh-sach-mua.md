# Danh sách mua linh kiện (bản cập nhật 2026-10-26)

Nguồn duy nhất cho linh kiện của [thiết kế](2026-10-26-trung-tam-dieu-khien-nha-design.md). Giá là giá niêm yết lúc tìm trên Shopee (26/10/2026), **chưa gồm ship, có thể đổi**.

**Chốt hướng:**
- Mọi node **tự ráp, đi Wi-Fi**. Không Zigbee, không LoRa.
- **Vỏ node = tái dùng 10 vỏ đầu báo CBX cũ** (gắn trần). Giếng trời và node RO dùng hộp riêng.
- **Hệ báo cháy CBX giữ nguyên**, tích hợp bằng phần mềm (trỏ DNS + bộ nghe UDP), **không mua phần cứng** cho phần này (mục 17 thiết kế).

**Ký hiệu cột TT:** ✓ đã có trong giỏ · ➕ cần thêm · ♻ tái dùng, không mua · 💻 phần mềm, không mua.

---

## 🛒 Đặt Taobao (ưu tiên — rẻ hơn Shopee ~2–2,5×)

Giá ¥ là **tham khảo** (Taobao trôi giá liên tục, bấm vào xem giá thật). Quy đổi ~**¥1 ≈ 3.600 ₫**. Chưa gồm ship + phí gom hàng về VN (lô này nhẹ, thường 150–350k). Bấm tên tiếng Trung để ra trang tìm Taobao; hoặc copy keyword dán vào app Taobao / đơn vị order hộ.

### A. Node không khí × 3 (vỏ đầu báo cũ)
| Món | SL | Keyword Taobao (bấm để tìm) | ¥ ≈ |
|---|---|---|---|
| ESP32 DevKit CH340C Type-C | 7* | [ESP32开发板 CH340C](https://s.taobao.com/search?q=ESP32开发板+CH340C) | 13–18 |
| SCD40 — CO₂/nhiệt/ẩm | 3 | [SCD40 二氧化碳传感器模块](https://s.taobao.com/search?q=SCD40+二氧化碳传感器模块) | 45–75 |
| PMS7003 — bụi mịn | 3 | [PMS7003 激光粉尘传感器](https://s.taobao.com/search?q=PMS7003+激光粉尘传感器) | 38–55 |
| SGP41 — VOC/NOx | 3 | [SGP41 VOC 传感器模块](https://s.taobao.com/search?q=SGP41+VOC+传感器模块) | 22–38 |
| BH1750 — ánh sáng | 3 | [BH1750 光照传感器模块](https://s.taobao.com/search?q=BH1750+光照传感器模块) | 3–6 |
| LD2410C — hiện diện người | 1–3 | [HLK-LD2410C 人体存在传感器](https://s.taobao.com/search?q=HLK-LD2410C+人体存在传感器) | 13–22 |

\*ESP32 mua 7 cái dùng chung mọi node (6 node + 1 dự phòng).

### B. Node giếng trời × 1 (hộp IP65 ngoài trời)
| Món | SL | Keyword Taobao | ¥ ≈ |
|---|---|---|---|
| VEML7700 — ánh sáng ~120k lux | 1 | [VEML7700 光照传感器模块](https://s.taobao.com/search?q=VEML7700+光照传感器模块) | 10–18 |
| SHT40 — nhiệt/ẩm ngoài trời | 1 | [SHT40 温湿度传感器模块](https://s.taobao.com/search?q=SHT40+温湿度传感器模块) | 12–25 |
| Tấm cảm biến mưa | 2 | [雨滴传感器模块](https://s.taobao.com/search?q=雨滴传感器模块) | 1.5–3 |
| Hộp ABS chống nước IP65 | 1 | [ABS防水接线盒 室外 IP65](https://s.taobao.com/search?q=ABS防水接线盒+室外+IP65) | 6–15 |
| Keo phủ mạch 3-chống (thay Relife 70) | 1 | [三防漆 电路板防潮](https://s.taobao.com/search?q=三防漆+电路板防潮) | 15–30 |

### C. Node nước RO × 1
| Món | SL | Keyword Taobao | ¥ ≈ |
|---|---|---|---|
| ADS1115 — ADC 16-bit | 1 | [ADS1115 16位ADC模块](https://s.taobao.com/search?q=ADS1115+16位ADC模块) | 6–12 |
| DS18B20 chống nước 1 m | 1 | [DS18B20 防水温度传感器](https://s.taobao.com/search?q=DS18B20+防水温度传感器) | 3–6 |
| Mạch TDS analog (kiểu DFRobot) | 2 | [模拟TDS传感器模块](https://s.taobao.com/search?q=模拟TDS传感器模块) | 16–28 |
| Đầu dò TDS gắn ống RO (2 đầu) | 1 | [RO纯水机 在线TDS探头](https://s.taobao.com/search?q=RO纯水机+在线TDS探头) | 18–45 |
| YF-S401 — lưu lượng nước | 1 | [YF-S401 水流量传感器](https://s.taobao.com/search?q=YF-S401+水流量传感器) | 10–18 |

(Tấm mưa dò rò nước dưới máy: lấy 1 trong 2 tấm ở nhóm B.)

### D. Node năng lượng × 1 (Deye + 3 pack JK)
| Món | SL | Keyword Taobao | ¥ ≈ |
|---|---|---|---|
| Mạch TTL→RS485 tự đảo chiều | 2 | [TTL转RS485模块 自动收发](https://s.taobao.com/search?q=TTL转RS485模块+自动收发) | 3–8 |

### E. Nguồn 220V→5V cho node
| Món | SL | Keyword Taobao | ¥ ≈ |
|---|---|---|---|
| Hi-Link HLK-10M05 (220V→5V 10W) | 6 | [海凌科 HLK-10M05 电源模块](https://s.taobao.com/search?q=海凌科+HLK-10M05+电源模块) | 13–20 |
| Cầu chì 5×20 + đế | 1 bộ | [5x20保险丝座 带保险丝](https://s.taobao.com/search?q=5x20保险丝座+带保险丝) | 1–3 |

### F. Vật tư chung
| Món | SL | Keyword Taobao | ¥ ≈ |
|---|---|---|---|
| Bo đục lỗ 5×7 cm | 6 | [万能板 洞洞板 5x7](https://s.taobao.com/search?q=万能板+洞洞板+5x7) | 0.5–1 |
| Tán cấy nhiệt M3 + ốc | 1 bộ | [M3热熔铜螺母 嵌件](https://s.taobao.com/search?q=M3热熔铜螺母+嵌件) | 8–15 |
| Dây silicon + JST + ống co nhiệt | 1 bộ | [硅胶线 JST 热缩管 套装](https://s.taobao.com/search?q=硅胶线+JST+热缩管+套装) | 20–40 |
| Dung dịch chuẩn TDS 342 ppm | 1 | [TDS校准液 342ppm](https://s.taobao.com/search?q=TDS校准液+342ppm) | 8–15 |

### G. Hồng ngoại gắn vào node không khí × 3 (điều khiển TV, loa, quạt, máy lạnh — `firmware/ir.yaml`)
| Món | SL | Keyword Taobao | ¥ ≈ |
|---|---|---|---|
| Mắt thu VS1838B (học mã remote) | 3 | [VS1838B 红外接收头](https://s.taobao.com/search?q=VS1838B+红外接收头) | chưa tra |
| LED phát hồng ngoại 940 nm 5 mm | 6 | [940nm 红外发射管 5mm](https://s.taobao.com/search?q=940nm+红外发射管+5mm) | chưa tra |
| Transistor S8050 + trở 1 kΩ, 10 Ω | 3 bộ | [S8050 三极管](https://s.taobao.com/search?q=S8050+三极管) | chưa tra |

Phòng có thiết bị hồng ngoại mà không có node không khí (kho, ngu2, docsach): dùng ESP32 dự phòng làm node chỉ có hồng ngoại, hoặc mua thêm ESP32.

### Tạm tính Taobao
- Toàn bộ điện tử + nguồn + vật tư: **~¥850–1.000 ≈ 3,1–3,6 tr ₫** (chưa ship/gom hàng).
- So với Shopee ~8,1 tr → **rẻ hơn ~2–2,5×**.
- Nhựa in PETG/ASA: bạn tự in, không mua.

> Các bảng Shopee bên dưới giữ làm **đối chiếu giá + dự phòng** nếu món nào Taobao khó gom.

---

## Các node và linh kiện

### Node không khí × 3 khu (vỏ đầu báo cũ, gắn trần)

| TT | Linh kiện | SL | Chọn phân loại | Link | Giá/cái |
|---|---|---|---|---|---|
| ✓ | ESP32 DevKit 30 chân CH340C, USB-C | (trong 7) | `ESP32 30Pin CH340C` | [770245757/23825959681](https://shopee.vn/product/770245757/23825959681) | ~127.050 ₫ |
| ✓ | SCD40 (CO2, nhiệt độ, độ ẩm) | 3 | **SCD40** | [770245757/41401245242](https://shopee.vn/product/770245757/41401245242) | ~496.650 ₫ |
| ➕ | PMS7003 (bụi PM1/2.5/10) | 3 | **Pms7003** | [240781775/9409211052](https://shopee.vn/product/240781775/9409211052) | ~449.747 ₫ |
| ✓ | SGP41 (VOC/NOx) | 3 | SGP41 bảng tím vuông | [1679957109/27345582646](https://shopee.vn/product/1679957109/27345582646) | ~222.280 ₫ |
| ✓ | BH1750 (ánh sáng) | 3 | | [770245757/22547166772](https://shopee.vn/product/770245757/22547166772) | ~36.772 ₫ |
| ✓/➕ | LD2410C (phát hiện người) | 1 có, +2 nếu muốn đủ 3 khu | Ld2410c-p | [770245757/18883267489](https://shopee.vn/product/770245757/18883267489) | ~134.948 ₫ |

### Node giếng trời × 1 (hộp IP65 riêng, ngoài trời)

| TT | Linh kiện | SL | Ghi chú | Link | Giá/cái |
|---|---|---|---|---|---|
| ✓ | ESP32 DevKit | (trong 7) | | như trên | ~127.050 ₫ |
| ✓ | VEML7700 (ánh sáng tới ~120k lux) | 1 | | [770245757/26986875489](https://shopee.vn/product/770245757/26986875489) | ~64.680 ₫ |
| ➕ | SHT40 (nhiệt độ/độ ẩm ngoài trời) | 1 | | [1057548384/42254995125](https://shopee.vn/product/1057548384/42254995125) | ~60.031 ₫ |
| ✓ | Tấm cảm biến mưa 2 mặt | 1 (trong 2) | Loại điện trở, **sẽ ăn mòn, phủ keo và thay định kỳ** | [770245757/17367275769](https://shopee.vn/product/770245757/17367275769) | ~39.270 ₫ |
| ➕ | Hộp nhựa ABS chống nước (đựng ESP32) | 1 | Để ngoài trời, in 3D khó kín nước | [56533840/18690763378](https://shopee.vn/product/56533840/18690763378) | ~57.330 ₫ |
| ➕ | Keo phủ mạch Relife 70 (chống ẩm) | 1 | Phủ mạch ngoài trời | [1796780050/57109254874](https://shopee.vn/product/1796780050/57109254874) | ~131.449 ₫ |

Giếng trời là **ngoài trời**, nên **không dùng vỏ đầu báo** (không kín nước). SHT40 đặt trong chắn bức xạ in 3D.

### Node nước RO × 1 (dưới bồn rửa)

| TT | Linh kiện | SL | Ghi chú | Link | Giá/cái |
|---|---|---|---|---|---|
| ✓ | ESP32 DevKit | (trong 7) | | như trên | ~127.050 ₫ |
| ✓ | ADS1115 (ADC 16-bit) | 1 | | [770245757/22146935614](https://shopee.vn/product/770245757/22146935614) | ~70.521 ₫ |
| ✓ | DS18B20 chống nước cáp 1 m | 1 | Dán sát ống | [770245757/26269813441](https://shopee.vn/product/770245757/26269813441) | ~45.045 ₫ |
| ➕ | Mạch TDS Meter V1.0 (giống DFRobot) | 2 | Đọc lần lượt 2 đầu dò | [1394580583/24790494765](https://shopee.vn/product/1394580583/24790494765) | ~129.240 ₫ |
| ➕ | Bộ đo TDS gắn ống cho RO (lấy 2 đầu dò) | 1 | Một trước màng, một ngay sau màng | [301618214/24100284067](https://shopee.vn/product/301618214/24100284067) | ~240.000 ₫ |
| ➕ | Cảm biến lưu lượng YF-S401 | 1 | Đặt ở **đường nước vào** | [882465674/20060254736](https://shopee.vn/product/882465674/20060254736) | ~57.534 ₫ |
| ✓ | Tấm cảm biến mưa 2 mặt (dò rò nước dưới máy) | 1 (trong 2) | Nước bắc cầu 2 cực | [770245757/17367275769](https://shopee.vn/product/770245757/17367275769) | ~39.270 ₫ |

### Node năng lượng × 1 (tủ điện: Deye + 3 pack JK)

| TT | Linh kiện | SL | Ghi chú | Link | Giá/cái |
|---|---|---|---|---|---|
| ✓ | ESP32 DevKit | (trong 7) | 2 UART cho 2 bus RS485 | như trên | ~127.050 ₫ |
| ✓ | Mạch TTL→RS485 tự đảo chiều | 2 | 1 đọc Deye, 1 đọc mạng RS485 của 3 pack JK | [1679957109/40333561215](https://shopee.vn/product/1679957109/40333561215) | ~116.810 ₫ |

## Nguồn điện cho node (mua thêm)

Mỗi node cắm 220V qua module nhỏ ra 5V. Với node gắn trần, module đặt trong **hộp kín giấu trên trần**, chỉ kéo 5V xuống vỏ đầu báo (vỏ không có 220V).

| TT | Linh kiện | SL | Ghi chú | Link | Giá/cái |
|---|---|---|---|---|---|
| ➕ | Nguồn Hi-Link **HLK-10M05** (220V→5V, 10W) | 6 | Dư công suất cho ESP32 + PMS7003. Node RO/năng lượng có thể thay bằng củ sạc 5V | [1156302552/45965598765](https://shopee.vn/product/1156302552/45965598765) | ~118.509 ₫ |
| ➕ | Cầu chì ống 5×20 + đế + domino | 1 bộ | An toàn đầu vào 220V. Mua ở shop điện | — | ~50.000 ₫ |

## Vật tư chung (mua thêm / đã có)

| TT | Linh kiện | SL | Ghi chú | Link | Giá/cái |
|---|---|---|---|---|---|
| ✓ | ESP32 DevKit dự phòng | (trong 7) | Giỏ có 7, dùng 6 node + 1 dự phòng | | |
| ✓/➕ | Bo đục lỗ 5×7 cm | 1 có, nên +5 | Mỗi node hàn module lên 1 tấm | [770245757/24843133733](https://shopee.vn/product/770245757/24843133733) | ~44.603 ₫ |
| ➕ | Tán cấy nhiệt M3 + ốc (bắt tấm đỡ vào vỏ) | 1 bộ | | [889466159/25579929619](https://shopee.vn/product/889466159/25579929619) | ~37.942 ₫ |
| ➕ | Dây silicon, đầu JST/Dupont, ống co nhiệt | 1 bộ | Shop điện tử | — | ~150.000 ₫ |
| ➕ | Dung dịch chuẩn TDS 342 ppm (hiệu chỉnh) | 1 | | tìm "dung dịch chuẩn TDS 342" | ~100.000 ₫ |
| ➕ | Nhựa in **PETG** (vỏ trong nhà) + **ASA** (chắn bức xạ) | tuỳ | Chủ nhà tự in | — | — |

## Tái dùng, không mua

| TT | Thứ | Dùng làm |
|---|---|---|
| ♻ | 10 vỏ đầu báo CBX cũ | Vỏ 3 node không khí (gắn trần). Còn lại: dự phòng, lấy linh kiện |
| ♻ | Buồng khói quang trong vỏ cũ | *Tuỳ chọn:* ESP32 đọc làm cảnh báo khói phụ (không phải thiết bị kiểm định) |
| ♻ | Module LoRa HopeRF, còi piezo trong vỏ cũ | Dự phòng. Không dùng trong hướng Wi-Fi |

## Phần mềm, không mua

| TT | Thứ | Ghi chú |
|---|---|---|
| 💻 | Tích hợp hệ báo cháy CBX | Trỏ DNS `cbx.vn` về trung tâm + bộ nghe UDP `cbxd`. Không mua phần cứng (mục 17 thiết kế). Cần biết model router |

## Đã bỏ hẳn so với bản trước

| Bỏ | Lý do |
|---|---|
| Toàn bộ thiết bị Zigbee Moes (đầu báo khói, còi, rò nước, mưa, nhiệt độ) | Báo cháy đã có hệ CBX; cảm biến tự ráp đi Wi-Fi |
| Bộ điều phối SMLIGHT SLZB-06 | Không còn dùng Zigbee |
| Mạch UPS 18650 + pin 18650 + còi 5V | Node cắm điện, báo cháy là việc của CBX |
| Hộp/giá treo đầu báo in 3D ngoài | Tái dùng vỏ đầu báo cũ |

## Tổng tạm tính (giá niêm yết, chưa ship)

| Nhóm | Tạm tính |
|---|---|
| Đã có trong giỏ (ESP32×7, SCD40×3, BH1750×3, LD2410C×1, ADS1115, DS18B20, protoboard, tấm mưa×2, VEML7700, SGP41×3, RS485×2) | ~4,6 tr |
| Cần thêm — không khí (PMS7003×3) | ~1,35 tr |
| Cần thêm — giếng trời (SHT40, hộp IP65, keo phủ) | ~0,25 tr |
| Cần thêm — RO (TDS×2 mạch, bộ đầu dò, YF-S401) | ~0,56 tr |
| Cần thêm — nguồn (HLK-10M05 ×6, cầu chì) | ~0,76 tr |
| Cần thêm — vật tư (bo×5, insert, dây, dung dịch chuẩn) | ~0,6 tr |
| **Tổng** | **khoảng 8,1 tr** |

LD2410C cho đủ 3 khu: thêm ~0,27 tr. Pin CR123A nếu dùng buồng khói tái dùng: mua lẻ shop pin.
