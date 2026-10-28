# Linh kiện Taobao: hồng ngoại, RF433, bộ nối máy phát nhạc với cục đẩy (2026-10-27)

Bổ sung cho [danh-sach-mua.md](danh-sach-mua.md) (mục G hồng ngoại đã có, không mua trùng) và mục 8, 18 của
[thiết kế](2026-10-26-trung-tam-dieu-khien-nha-design.md). Chỉ là danh sách, chưa mua, chưa đặt.

**Chủ nhà chốt (27/10):**
- Bộ nối máy phát nhạc với cục đẩy: **ra jack 3.5 mm cắm vào AUX của cục đẩy**, có **Bluetooth**.
- **Gom một shop** cho đỡ phí ship.
- RF433: **1 bộ**.

**Giá tra thế nào:**
- Taobao bắt kéo thanh trượt xác minh và đăng nhập ngay ở trang tìm kiếm. 1688 cho xem đúng một lần tìm rồi cũng chuyển sang trang đăng nhập. Phiên này không đăng nhập, không vượt xác minh. Vì vậy **chưa xác minh được link sản phẩm cụ thể hay giá Taobao nào**.
- Cột "¥ ≈" là **ước lượng**, quy đổi ~¥1 ≈ 3.600 ₫ như danh-sach-mua.md. Link là trang tìm Taobao (cần đăng nhập app/web Taobao để xem), cùng kiểu danh-sach-mua.md.
- Cột "Đã tra 27/10" là giá thật xem được hôm nay:
  - 1688: giá sỉ, có số lượng tối thiểu.
  - AliExpress (tài khoản VN): giá lẻ, thường cao hơn Taobao.

## Cách gom một shop

Món khó tìm nhất quyết định chọn shop. Ở đơn này là **Raspberry Pi Zero 2 W** và **bộ SRX882 + STX882**.

1. Trên Taobao, tìm `树莓派 Zero 2W`. Lọc các shop vừa bán Pi vừa bán module (thường là shop "电子模块"/"开发板").
2. Vào từng shop, dùng ô **搜本店** (tìm trong shop) lần lượt `SRX882`, `PCM5102A`, `VS1838B`. Chọn shop có đủ cả ba.
3. Các món còn lại (LED, transistor, dây 3.5 mm, bộ chống ù...) shop module nào cũng có: tìm tiếp bằng 搜本店 theo keyword ở bảng dưới.
4. Thường phải mua riêng:
   - **Sonoff BASICR4**: shop module ít bán. Không có thì mua ở shop Sonoff (tách đơn), hoặc dùng ổ cắm Tuya mua ở VN (đã có đường tích hợp `tuya-bridge`).
   - **Thẻ microSD**: mua chính hãng ở VN, Taobao nhiều hàng giả.

## Giỏ hàng một shop

### 1. Hồng ngoại (bổ sung mục G, không mua trùng)

Mục G đã có 3 bộ cho 3 node không khí: VS1838B, LED 940 nm, S8050 + trở. Gom luôn vào đơn này. Dưới đây chỉ thêm phần dư và phần cho amply.

| Món | SL | Keyword Taobao (bấm để tìm) | ¥ ≈ | Đã tra 27/10 | Vì sao / lưu ý |
|---|---|---|---|---|---|
| Mắt thu VS1838B (dư cho node chỉ có hồng ngoại) | +2 | [VS1838B 红外接收头](https://s.taobao.com/search?q=VS1838B+红外接收头) | 0,3–1/cái | 1688: ¥0,15–0,35 (chân trần), ¥0,64–1,03 (mạch KY-022) | Chạy 3.3 V, chân OUT nối thẳng GPIO27 như `firmware/ir.yaml`. Mua loại **chân trần có vỏ sắt (带屏蔽)**, không cần mạch KY-022 |
| LED phát 940 nm 5 mm (dư) | +4 | [940nm 红外发射管 5mm](https://s.taobao.com/search?q=940nm+红外发射管+5mm) | 2–4/gói 10–20 | chưa tra | Đi theo sơ đồ mục G: 5V → 2 LED nối tiếp → 10 Ω → S8050 |
| Dây phát hồng ngoại có jack 3.5 mm, dán keo | 1 | [红外发射线 3.5mm 延长线](https://s.taobao.com/search?q=红外发射线+3.5mm+延长线) | 2–5 | AliExpress ~20.000 ₫ | **Chỉ mua nếu** amply/vang có remote nằm trong tủ, node không chiếu tới. Dán đầu LED lên ô thu hồng ngoại của máy |
| Jack 3.5 mm hàn mạch PJ-320 | 2 | [PJ-320 3.5mm 耳机座](https://s.taobao.com/search?q=PJ-320+3.5mm+耳机座) | 0,2–0,5/cái | chưa tra | Cắm dây phát trên vào node. Mắc dây phát thành nhánh song song với 2 LED sẵn có, đi riêng một trở ~47 Ω. Trở này **chỉnh theo thực tế** (chưa đo) |

### 2. RF433: một bộ SRX882 + STX882

| Món | SL | Keyword Taobao | ¥ ≈ | Đã tra 27/10 | Vì sao / lưu ý |
|---|---|---|---|---|---|
| Bộ thu SRX882 + phát STX882 433 MHz kèm 2 ăng-ten lò xo | **1 bộ** | [SRX882 STX882 433M 超外差](https://s.taobao.com/search?q=SRX882+STX882+433M+超外差) | 6–12 | AliExpress ~31.000 ₫/bộ (nhiều shop, 166–332 lượt bán) | Xem lý do chọn ngay dưới bảng |

**Vì sao chọn SRX882 + STX882:**
- Hợp ESPHome `remote_receiver`/`remote_transmitter`: hai component này đọc và phát xung OOK trên **một chân dữ liệu**. Hai module này có đúng một chân DATA nối thẳng GPIO, không phải cấu hình gì thêm. Mã học được gửi lên MQTT giống đường hồng ngoại.
- Chạy 3.3 V, không cần chuyển mức:
  - SRX882S: NiceRF ghi dải nguồn 2,0–5,5 V, điển hình 3 V.
  - STX882: hãng ghi công suất danh định ở 3,6 V.
  - Cấp 3.3 V từ ESP32 thì ngõ ra DATA cũng là 3.3 V, an toàn cho GPIO.
- Bộ thu siêu ngoại sai (superheterodyne) chọn lọc tốt, ít nhiễu. Loại siêu tái sinh rẻ (XY-MK-5V, FS1000A) phải chạy 5 V và xả nhiễu liên tục vào `remote_receiver`: **không mua**.

**Không chọn CC1101 (vẫn là phương án dự phòng):**
- ESPHome đã có component `cc1101` (trang tài liệu bản 2026.10), nhưng phải nối SPI (CS, SCK, MOSI, MISO) cộng GDO0 (phát), GDO2 (thu), rồi gọi `cc1101.begin_tx`/`begin_rx` quanh mỗi lần phát.
- Diễn đàn Home Assistant có nhiều báo cáo "thu được mà phát không ăn" phải tinh chỉnh bộ lọc.
- Chỉ đáng khi cần đổi tần số (315/868 MHz) hoặc muốn một module lo cả thu và phát.

**Lưu ý lắp:**
- Gắn vào node gần thiết bị RF nhất. Trên node-cafe, các chân 21, 22, 16, 17, 25, 26, 27, 4 đã dùng. Gợi ý:
  - **SRX882 DATA → GPIO32**, chân CS nối VCC (thả nổi hoặc mức cao thì bộ thu chạy);
  - **STX882 DATA → GPIO33**;
  - cả hai cấp 3V3 + GND.
  - Sơ đồ chân lấy từ trang bán, **đối chiếu nhãn trên mạch thật** trước khi hàn.
- Ăng-ten: dùng lò xo kèm bộ, hoặc dây đơn dài **17,3 cm** (1/4 bước sóng 433,92 MHz).
- Chỉ dùng được với **mã cố định** (remote kiểu EV1527/PT2262: rèm, quạt trần, chuông, ổ cắm RF). Cổng/cửa cuốn mã nhảy (rolling code) không học-phát lại được, và không nên làm.
- Chưa biết thiết bị RF nào trong nhà cần điều khiển. Biết thì chọn node gắn module cho gần.

### 3. Bộ nối máy phát nhạc với cục đẩy: phương án A + E của mục 18

> **Đã thay (chủ nhà chốt 2026-10-28):** ESP32-S3 + PCM5102A + mạch công suất 2.1 ZK-TB21, thay cả Pi Zero 2 W lẫn cục đẩy cũ, mua ở Shopee. Xem mục 18.3 thiết kế và `docs/danh-sach-mua.md`. Phần dưới giữ để tham khảo.

**Chọn A (Pi Zero 2 W + DAC PCM5102A) + E (bật/tắt nguồn), vì khớp ba điều chủ nhà chốt:**

- **Jack 3.5 mm vào AUX:** mạch PCM5102A có sẵn jack 3.5 mm, mức ra ~2,1 Vrms, đủ kéo ngõ AUX của cục đẩy.
- **Bluetooth:** Pi Zero 2 W có sẵn Wi-Fi + Bluetooth, không mua thêm.
- **Phần mềm:** đề xuất moOde (chạy cục bộ), bật ba bộ phát:
  - **Spotify Connect** (librespot): hub điều khiển qua Spotify Web API như mục 18.2, đặt tên chứa id khu, vd. "cafe";
  - **Bluetooth**: khách/nhân viên ghép điện thoại;
  - AirPlay.
  - Nguồn nào bấm phát sau thì chiếm loa. Như vậy "chọn nguồn" Spotify/Bluetooth nằm trong Pi, cục đẩy chỉ cần một ngõ AUX.
- **Gom một shop:** toàn bộ là linh kiện bán ở shop module.

**Âm lượng:**
- Chỉnh ở Spotify (dashboard) hoặc trên điện thoại khi dùng Bluetooth.
- Núm gain của cục đẩy **chỉnh một lần**: mở Spotify 100% mà loa vẫn an toàn, rồi để yên. Đây là núm hiệu chỉnh duy nhất, không cần triết áp số.

**Không chọn B (WiiM Mini...) dù B ổn định hơn khi phát cả ngày:**
- WiiM Mini cũng có 3.5 mm + Bluetooth + Spotify Connect, AliExpress/Amazon ~70–99 USD, nhưng bán ở shop hãng, phải tách đơn.
- Up2Stream Mini V3 của Arylic đã ngừng sản xuất.
- Nếu Pi chạy ở quán không ổn, đổi sang WiiM Mini, phần còn lại giữ nguyên.

Bộ cho **một khu (quán)**. Các khu khác chờ chủ nhà trả lời câu 1 ở mục 18 (khu nào có amply/cục đẩy).

| Món | SL / khu | Keyword Taobao | ¥ ≈ | Đã tra 27/10 | Vì sao / lưu ý |
|---|---|---|---|---|---|
| Raspberry Pi Zero 2 W | 1 | [树莓派 Zero 2W](https://s.taobao.com/search?q=树莓派+Zero+2W) | 110–150 (chưa xác minh) | AliExpress: các listing trộn lẫn Zero/Zero W/Zero 2 W, giá lệch nhau quá nhiều nên không dùng làm mốc | Chọn đúng **Zero 2 W**: Zero W đời cũ yếu và moOde bản mới không chạy. Bản có sẵn chân cắm (2WH) đỡ phải hàn |
| Mạch DAC PCM5102A **có jack 3.5 mm**, khổ pHAT | 1 | [PCM5102A 解码板 3.5mm 树莓派](https://s.taobao.com/search?q=PCM5102A+解码板+3.5mm+树莓派) | 15–25 | AliExpress: ~72.000–76.000 ₫ (loại có jack 3.5 mm); ~31.000 ₫ (loại GY-PCM5102 không jack) | Nối I2S (xem dưới bảng). Không cần I2C, chọn overlay `hifiberry-dac` |
| Hàng rào chân 2×20 (nếu Pi không có chân) + dây Dupont cái-cái | 1 + 1 bó | [2x20P 排针](https://s.taobao.com/search?q=2x20P+排针) · [杜邦线 母对母](https://s.taobao.com/search?q=杜邦线+母对母) | 1 + 2–4 | chưa tra | |
| Nguồn 5 V 2,5 A cổng micro-USB | 1 | [5V2.5A 树莓派 电源 micro](https://s.taobao.com/search?q=5V2.5A+树莓派+电源+micro) | 8–15 | chưa tra | Hoặc dùng củ sạc 5 V tốt sẵn có. Nguồn yếu làm Pi khởi động lại, gây rè tiếng |
| Dây AUX 3.5 mm đực-đực, 1–1,5 m, có chống nhiễu | 1 | [3.5mm 音频线 公对公 AUX](https://s.taobao.com/search?q=3.5mm+音频线+公对公+AUX) | 3–8 | chưa tra | Cục đẩy có AUX dạng hoa sen (RCA) thì mua thêm dây [3.5mm 转双莲花](https://s.taobao.com/search?q=3.5mm转双莲花+音频线) (~¥3–8) |
| Bộ chống ù (cách ly mát) 3.5 mm | 1 | [3.5mm 音频隔离器 共地 消除电流声](https://s.taobao.com/search?q=3.5mm+音频隔离器+共地+消除电流声) | 5–10 | AliExpress ~31.000 ₫ | Rẻ, mua kèm. **Chỉ cắm khi loa có tiếng ù/rè** do Pi và cục đẩy khác nguồn |
| Hộp nhựa cho Pi + DAC | 1 | — | — | — | In 3D (PETG) như các node khác |
| **Sonoff BASICR4** (ESP32-C3, rơ-le 10 A) bật/tắt điện cục đẩy | 1 | [SONOFF BASICR4](https://s.taobao.com/search?q=SONOFF+BASICR4) | 40–60 (chưa xác minh) | AliExpress ~201.000–249.000 ₫ | Xem phần bật/tắt dưới bảng |
| Thẻ microSD 16–32 GB loại High Endurance | 1 | — | — | — | **Mua chính hãng ở VN** (Taobao nhiều thẻ giả). Bật chế độ chỉ đọc cho hệ thống để đỡ hỏng thẻ (rủi ro A trong mục 18) |

**Bật/tắt cục đẩy bằng Sonoff BASICR4:**
- Mạch có vỏ, rơ-le 10 A/2400 W, chip ESP32-C3. Nạp ESPHome qua 4 chân 3V3/RX/TX/GND có nhãn sẵn trên mạch, **nạp khi đã rút hẳn 220 V**.
- Sau đó nó là một node MQTT như các node khác, không qua cloud eWeLink.
- **Không gom được một shop:** shop module ít bán Sonoff. Phương án thay: ổ cắm Tuya mua ở VN (đúng phích VN), đi đường `tuya-bridge` có sẵn.

**Đấu nối và vận hành:**
- Pi ↔ PCM5102A (theo chân I2S chuẩn của Pi; **đối chiếu nhãn trên mạch thật**):

  | Mạch PCM5102A | Pi Zero 2 W |
  |---|---|
  | VIN | 5V (chân 2) |
  | GND | GND (chân 6) |
  | BCK | GPIO18 (chân 12) |
  | LCK | GPIO19 (chân 35) |
  | DIN | GPIO21 (chân 40) |
  | SCK | nối GND (hoặc hàn cầu sẵn trên mạch) |

  Các cầu hàn mặt sau mạch thường để mặc định: FLT, DEMP, FMT mức thấp; XSMT mức cao.
- Cục đẩy:
  - Để công tắc nguồn của cục đẩy ở **ON**, Sonoff cấp hoặc ngắt điện.
  - **Thử trước:** rút điện rồi cắm lại, cục đẩy phải tự lên. Nếu cục đẩy có nút nguồn điện tử/standby thì không tự lên; khi đó bật/tắt bằng hồng ngoại (nếu có remote) hoặc nút tay.
  - Cục đẩy biến áp xuyến lớn (trên ~1000 W) có dòng khởi động cao: báo lại để chọn khởi động từ thay Sonoff.
- Thứ tự để loa không kêu "bụp": Pi luôn bật. Cục đẩy bật sau cùng, tắt trước tiên.
- Rủi ro chưa đo:
  - Pi Zero 2 W dùng chung một ăng-ten cho Wi-Fi và Bluetooth, có thể rè/ngắt khi Wi-Fi bận. Gặp thì đặt Pi gần router hơn, hoặc chuyển sang WiiM Mini.
  - moOde chưa chạy thử trên Zero 2 W trong dự án này.
- Hub chưa có phần điều khiển moOde (đổi nguồn, âm lượng Bluetooth). Hiện hub điều khiển được phần Spotify như mục 18.2 và bật/tắt cục đẩy qua node Sonoff. Phần mềm thêm sau, ngoài phạm vi danh sách này.

## Tạm tính (ước lượng, chưa ship)

| Nhóm | ¥ ≈ | ₫ ≈ |
|---|---|---|
| Hồng ngoại thêm (VS1838B ×2, LED ×4, dây phát, jack) | 5–12 | 20–45k |
| RF433 (SRX882 + STX882 ×1 bộ) | 6–12 | 20–45k |
| Bộ nối cục đẩy, một khu, chưa tính Sonoff và thẻ nhớ | 145–215 | 0,52–0,77 tr |
| Sonoff BASICR4 (thường tách đơn) | 40–60 | 0,15–0,22 tr (AliExpress ~0,2–0,25 tr) |
| **Cộng cả đơn (hồng ngoại + RF433 + nhạc cho một khu)** | **~200–300** | **~0,7–1,1 tr** |

Mỗi khu nhạc thêm: ~¥185–275 (bộ nối + Sonoff).

Món đắt nhất và chưa xác minh giá là Pi Zero 2 W. Xem giá thật trên app Taobao trước khi chốt shop.
