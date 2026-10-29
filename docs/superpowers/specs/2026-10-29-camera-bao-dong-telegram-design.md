# Camera không đầu ghi, báo động theo khu, tin Telegram

Thiết kế ngày 2026-10-29, từ trao đổi với chủ nhà ngày 28–29/10. Thay nguyên tắc "ghi hình vẫn do NVR đảm nhận, trung tâm không ghi" ở mục 15.1 của [thiết kế chính](../../2026-10-26-trung-tam-dieu-khien-nha-design.md).

Ký hiệu nguồn quyết định:
- **[chủ nhà]**: chủ nhà nói hoặc xác nhận;
- **[Claude]**: Claude tự quyết, chủ nhà sửa nếu sai;
- **[báo cáo]**: lấy từ `research/2026-10-27-bo-sung-thiet-bi-cu-wifi-camera-pccc.md`, mục 3.

## 1. Vì sao

- **Bớt thiết bị:** bỏ đầu thu (NVR). Hub Center tự nhận camera, tự ghi hình, tự báo động. [chủ nhà]
- **Biết chuyện khi đi vắng:** báo động và các cảnh báo khác gửi qua Telegram, kèm ảnh chụp. [chủ nhà]
- **Hợp hướng sản phẩm:** phần mềm mở chạy trên thiết bị người dùng đã có, nhận camera nhiều hãng. Hikvision làm trước, hãng khác sau. [chủ nhà]

## 2. Chủ nhà đã chốt

1. Hub nhận trực tiếp mọi camera trong mạng nhà, không qua đầu thu. Camera dây cắm nguồn riêng hoặc lấy điện từ switch PoE; camera Wi‑Fi chỉ cần cắm điện. Sau này mở rộng thêm hãng khác.
2. Thêm camera mới:
   - cắm camera vào mạng, hub tự dò thấy (ONVIF, Hikvision trước);
   - Cài đặt hiện "Tìm thấy camera mới";
   - chủ nhà nhập mật khẩu camera, đặt tên, chọn khu, chọn chế độ ghi;
   - xong là xem và ghi được ngay.
3. Muốn ghi hình thì chủ nhà lắp ổ cứng vào thiết bị chạy hub. Các thiết bị hỗ trợ lắp ổ cứng.
4. Mỗi camera chọn chế độ ghi: **toàn thời gian** hoặc **theo khung giờ**.
5. **Mọi camera đều luôn ghi**, cắt thành từng file tối đa **50 MB**. Camera không bật ghi, hoặc đang ngoài khung giờ ghi, thì ghi xong file nào xoá file đó.
6. Khi có báo động:
   - lưu riêng phần hình **30 giây trước** và **30 giây sau** thời điểm báo động, bằng cách giữ lại các file 50 MB chứa đoạn đó;
   - các file này **không tự xoá** khi chạm giới hạn dung lượng;
   - **mỗi tuần nhắc chủ nhà dọn**.
7. Đặt được **dung lượng tối đa cho từng camera**. Chạm mức đó thì tự xoá video cũ nhất để ghi tiếp.
8. Mặc định ghi hình thường chỉ dùng tối đa **80% ổ cứng**, kể cả khi không camera nào đặt giới hạn riêng. **20%** còn lại để dành cho đoạn báo động.
9. Còi hú lắp trong **cụm cảm biến** (node) của từng khu.
10. Báo động gửi đến các còi theo khu được cấu hình liên kết, hoặc báo tất cả. Câu hỏi gốc của chủ nhà; cách chọn theo từng khu ở mục 6.
11. Tin ra ngoài nhà đi qua **Telegram**. **Cấu hình được loại tin nào gửi Telegram.**
12. Khi báo động, **chụp hình ngay và gửi hình đó qua Telegram**.
13. AI nhận ra người là **phiên bản sau**, thuộc gói chuyên nghiệp, chạy trên máy chủ cấu hình cao.

## 3. Không làm, và thất bại nếu

**Không làm ở bản này:**
- AI nhận người/xe trên hub. [chủ nhà]
- Xem camera trực tiếp từ ngoài nhà qua hub. Ra ngoài nhà chỉ có tin và ảnh Telegram; ai cần xem trực tiếp từ xa thì dùng Tailscale như mục 15.1. [Claude]
- Gửi video qua Telegram. Video xem trên dashboard. [Claude]
- Lưu trữ đám mây; mở cổng từ internet vào nhà. [Claude, theo nguyên tắc hiện có]
- Thêm container mới. [chủ nhà, đã chốt từ trước: Hub Center chỉ có 4 GB RAM]

**Thất bại nếu, dù tính năng vẫn chạy:**
- Đoạn báo động bị mất hoặc bị tự xoá.
- Ổ đầy làm ghi hình dừng mà không ai được báo.
- Còi hú vô cớ nhiều lần (cây lá, đèn xe) đến mức chủ nhà tắt hẳn báo động.
- Người lạ tắt được còi hay tắt canh mà không cần PIN, ví dụ bằng giọng nói.
- Hub 4 GB hết RAM hoặc nghẽn CPU khi có nhiều camera.
- Lộ mật khẩu camera hoặc token bot Telegram (qua API, log, bản sao lưu).

## 4. Ràng buộc

- **Nơi chạy:** mọi phần mới nằm trong `hubd` (tiến trình con của `hubd` thì được), không thêm container. `go2rtc` giữ vai trò xem trực tiếp như hiện nay.
- **Không chuyển mã:** ghi đúng luồng camera gửi (H.264 hoặc H.265), không giải mã rồi nén lại.
- **Không ghi vào ổ hệ thống:** chỉ ghi vào ổ cứng chủ nhà lắp và chọn trong Cài đặt. Ổ hệ thống (eMMC/SSD chạy hệ điều hành) không dùng để ghi hình. [báo cáo 3.4]
- **Bí mật:** mật khẩu camera và token bot xử lý như các khoá hiện có: API chỉ trả `*_set`, không in ra log, bản sao lưu cấu hình không chứa chúng (như `withoutSecrets`).
- **Đường ra internet:** chỉ đi ra (gửi tin Telegram, hỏi lệnh mới bằng long polling). Không webhook, không mở cổng.
- **Đầu thu cũ** vẫn chạy như hiện nay trong lúc chuyển từng camera sang nối trực tiếp. Các luật đang dùng topic `hik/<kênh>/…` không phải sửa. [Claude, theo báo cáo 3.7]
  - Trong lúc chuyển, các kênh của đầu thu (`cam_ch<N>`) **tham gia đủ mọi phần** như camera nối trực tiếp:
    - sự kiện `hik/<kênh>/…` từ `hikbridge` tính là sự kiện của camera thuộc khu đó;
    - hub ghi luồng chính của kênh qua RTSP của đầu thu;
    - ảnh chụp lấy qua đầu thu.

    Như vậy báo động dùng được ngay, không phải đợi đi lại dây từng camera. [Claude]
- **Bản này chỉ chạy Linux trong Docker**, như hiện nay. Gói chạy native, Windows, Android để sau. [Claude]
- **Ổ cứng trong Docker:** container `hubd` không được mount ổ và không được định dạng ổ. Profile AppArmor `inix-hubd` có `deny mount`, và máy thử dùng chung cho dịch vụ khác. Vì vậy ở bản này:
  - **hệ điều hành gắn ổ** vào một thư mục cố định, ví dụ `/srv/hubd-video`;
  - thư mục đó gắn vào `hubd` ở chế độ đọc ghi, ví dụ `/rec`;
  - hub chỉ ghi vào thư mục được gắn.

  Tự nhận ổ khi cắm và định dạng ổ ngay trong Cài đặt để sau, làm cùng bản hệ điều hành riêng của Hub Center. Lần đầu gắn ổ trên máy thử thì Claude làm, sau khi chủ nhà đồng ý. [Claude]
- **Mạng Docker không cho multicast ra LAN,** nên dò camera không dùng được WS‑Discovery multicast từ container. Dò bằng cách quét lần lượt từng địa chỉ trong dải LAN của hub (cổng ONVIF, cổng ISAPI). Không đổi `hubd` sang `network_mode: host`. [Claude]

## 5. Phần 1: Tin Telegram

**Cài đặt → Thông báo:**
- Nhập token bot. Chủ nhà tự tạo bot bằng BotFather.
- **Người nhận:**
  - người muốn nhận tin nhắn `/start` cho bot;
  - Cài đặt hiện yêu cầu, chủ nhà bấm duyệt (cần PIN);
  - chỉ người đã duyệt mới nhận tin và bấm được nút. [Claude]
- **Bảng loại tin:** mỗi loại có công tắc **"Gửi Telegram"**, đặt cạnh công tắc "Đọc thành tiếng" đang có (`notify.go`, nhóm cấu hình `notify`).

**Các loại tin:**

| Loại | Nguồn | Mặc định gửi Telegram |
|---|---|---|
| An ninh | Báo động ở khu đang canh (Phần 3) | Luôn gửi, không tắt được [Claude] |
| Cháy | Báo cháy CBX khi làm (mục 17 thiết kế chính) | Luôn gửi, không tắt được [Claude] |
| Theo `kind` có sẵn | co2, bui, mua, nong… | Bật |
| `notify` không có `kind` | khói, rò nước… (hiện luôn báo) | Bật |
| Thiết bị | Camera, node, ổ cắm mất kết nối quá 10 phút | Bật [Claude] |
| Lưu trữ | Ổ sắp đầy, dừng ghi, nhắc dọn đoạn báo động hằng tuần | Bật [Claude] |
| Báo cáo hằng ngày | Tóm tắt như nút "Đọc tình hình" (`brief`), giờ gửi chỉnh được | Tắt [Claude] |

**Tin an ninh:**
- Chữ: khu, camera, giờ, loại sự kiện.
- **Ảnh chụp ngay lúc báo động.** [chủ nhà] Lấy ảnh như mục 7.4.
- Nút trong tin: **"Tắt còi"** và **"Tắt canh"**. "Tắt canh" hỏi xác nhận thêm một lần. [Claude]

**Chống làm phiền:** cùng một nội dung chỉ gửi tối đa một tin mỗi 30 phút. Không áp dụng cho tin an ninh và tin cháy. [Claude]

**Mất internet:** tin xếp hàng chờ, có mạng thì gửi lại theo thứ tự. Giữ tối đa 24 giờ. [Claude]

## 6. Phần 3 (phần lõi báo động): chế độ canh và còi

Phần này nêu trước Phần 2 vì nó quyết định đoạn báo động giữ những gì.

**Chế độ canh cả nhà:** Tắt, Ở nhà, Vắng nhà, Ban đêm. [Claude đề xuất, chủ nhà không phản đối khi xem bản tóm tắt]
- Bật/tắt trên dashboard (cần PIN), theo lịch (ví dụ đóng quán thì sang Vắng nhà), hoặc bằng nút trong Telegram (người đã duyệt).

**Mỗi khu đặt hai điều:**
- **Canh ở những chế độ nào.** Ví dụ sân sau canh ở Vắng nhà và Ban đêm; khu cafe chỉ canh ở Vắng nhà.
- **Khi có sự kiện trong lúc khu đang canh thì làm gì:**
  - chỉ báo trên màn hình và Telegram;
  - **hú còi của các cụm cảm biến liên kết với khu đó**;
  - **hú còi cả nhà**.

  Đây là câu "theo khu liên kết hay báo tất cả" của chủ nhà, chọn riêng cho từng khu. [chủ nhà hỏi, Claude đề xuất cách chọn]

**Nguồn sự kiện của khu:** sự kiện từ các camera thuộc khu: chuyển động, vượt hàng rào ảo, xâm nhập vùng, tuỳ camera hỗ trợ loại nào. Sau này thêm cảm biến cửa, PIR.

**Chỉ khu đang canh mới báo động.** Sự kiện camera ở khu không canh vẫn là sự kiện thường (luật dùng được), nhưng không hú còi, không gửi tin an ninh, không giữ đoạn báo động. [Claude]

**Báo động giữ hình của mọi camera trong khu**, không chỉ camera phát hiện. Mỗi camera giữ các file của chính nó theo quy tắc 30 giây. Sự kiện từ cảm biến cửa hay PIR sau này cũng giữ hình các camera cùng khu. [Claude]

**Thời gian trễ khi ra và vào** (như tủ báo động thông thường) [Claude]:
- **Trễ ra:** chuyển sang Vắng nhà hoặc Ban đêm thì đợi **60 giây** mới bắt đầu canh, để người bật kịp ra khỏi khu. Chỉnh được.
- **Trễ vào:** đặt riêng cho từng khu, mặc định 0. Khu có cửa ra vào thì nên đặt, ví dụ 30 giây.
  - Có sự kiện ở khu có trễ vào: mọi màn hình hiện đếm ngược "Nhập mã để tắt canh".
  - Hết giờ mà chưa tắt canh thì mới hú còi và gửi tin.
  - Ảnh vẫn chụp ngay lúc có sự kiện; mốc 30 giây để giữ hình tính từ lúc có sự kiện.

**Còi:**
- Mỗi cụm cảm biến có còi thì có một thực thể "còi" điều khiển qua MQTT, như các node hiện có.
- Mỗi lần hú tối đa **3 phút**, chỉnh được. [Claude]
- **Tắt còi và tắt canh chỉ bằng PIN nhập trên màn hình, hoặc nút Telegram của người đã duyệt.**
  - Dùng **chung PIN dashboard**, không có mã báo động riêng. [chủ nhà]
  - PIN phải **nhập lại mỗi lần** tắt còi hay tắt canh, không dựa vào phiên đăng nhập 90 ngày của iPad treo tường. Nhập sai nhiều lần thì khoá tạm như màn hình đăng nhập hiện nay. [Claude]
  - **Đổi được PIN trong Cài đặt** (ví dụ khi nhân viên đã biết PIN). [chủ nhà]
    - Đổi PIN phải nhập PIN cũ.
    - PIN lưu dạng băm trong kho cấu hình của hub. `HUBD_PIN` trong `.env` chỉ dùng cho lần chạy đầu.
    - Đổi xong thì đăng xuất mọi màn hình khác.

    [Claude]
- Trợ lý giọng nói **không** tắt được còi an ninh và không tắt được canh: nhận giọng người nhà không phải xác thực, giọng ghi âm phát lại vẫn được nhận. Câu "tắt báo động" của trợ lý hiện xác nhận mọi cảnh báo, nên phải loại báo động an ninh ra. [Claude]

**Chống báo nhầm:** nên dùng hàng rào ảo hoặc xâm nhập vùng của camera thay cho chuyển động thường. Hướng dẫn cài đặt camera ghi rõ điều này. [Claude]

**Phần cứng còi trong cụm cảm biến:** chưa chọn. Còi to (khoảng 100 dB) thường cần 12 V, trong khi node chạy 5 V. Chọn ở danh sách mua; phần mềm chỉ cần một ngõ ra bật/tắt.

## 7. Phần 2: camera không đầu ghi và ghi hình

### 7.1 Danh sách camera

- Nhóm cấu hình mới `cameras` là một danh sách, theo đề xuất ở báo cáo mục 3.7. Mỗi camera có:
  - `id`, tên, khu;
  - kiểu: `hik_cam`, `onvif`, `rtsp`; `hik_nvr` giữ cho đầu thu cũ;
  - host, cổng, tài khoản, mật khẩu (bí mật);
  - URL luồng chính và luồng phụ;
  - chế độ ghi, khung giờ, giới hạn dung lượng.
- Mục đầu thu cũ vẫn nở ra thành các kênh như hiện nay (`cam_ch<N>`). Camera mới có id `cam_<tên>`. [báo cáo]
- **Dò camera:** quét dải LAN của hub tìm camera trả lời ONVIF hoặc ISAPI (xem mục 4: không dùng multicast). Hikvision đọc thêm `/ISAPI/System/deviceInfo` để lấy hãng, model, firmware. Camera đã có trong danh sách thì không báo lại. Vẫn cho **thêm tay** bằng IP hoặc URL RTSP.
- Xem trực tiếp vẫn qua `go2rtc` (luồng phụ, sau PIN), như hiện nay.

### 7.2 Sự kiện từ camera

- Camera tự phát hiện, hub chỉ nhận sự kiện:
  - Hikvision: `alertStream` ISAPI của từng camera;
  - hãng khác: ONVIF PullPoint.
- Sự kiện đưa vào MQTT `cam/<id>/<sự kiện>` (ON/OFF), để luật và Phần 3 dùng như cảm biến.
- Phần này làm trong `hubd` cho camera nối trực tiếp. `hikbridge` chỉ còn dùng cho đầu thu cũ. [Claude]

### 7.3 Ghi hình

**Ổ cứng:**
- Cài đặt → Hệ thống → Lưu trữ hiện thư mục ghi hình (mục 4): có ổ gắn hay không, dung lượng, đã dùng, còn trống.
- Hub chỉ ghi khi thư mục đó là một ổ thật được gắn vào, không phải thư mục nằm trên ổ hệ thống. Không có ổ thì không ghi gì: tin báo động vẫn có ảnh, nhưng không có video. [Claude]
- Nên dùng ổ HDD loại cho camera (dòng surveillance): mọi camera ghi liên tục, nên SSD sẽ nhanh hết tuổi thọ ghi. Ví dụ 10 camera mỗi camera 4 Mbit/s là khoảng 430 GB ghi mỗi ngày.

**Cách ghi:**
- Mỗi camera ghi **luồng chính** liên tục, không chuyển mã, thành **file tối đa 50 MB**, cắt ở khung hình đầy đủ (keyframe). [chủ nhà, cách cắt: Claude]
- Định dạng file phải xem được trên Safari iPad, kể cả H.265. [Claude]
- Mỗi file có thời điểm đầu và cuối, để biết file nào chứa một thời điểm.

**Chế độ ghi** [chủ nhà]:
- **Toàn thời gian**: giữ mọi file.
- **Theo khung giờ**: một hay nhiều khung trong ngày, áp cho mọi ngày. *(Claude: chia theo thứ trong tuần để sau nếu cần.)* File có phần nằm trong khung thì giữ.
- **Không ghi**: không giữ file thường.

**Giữ hay xoá file thường:**
- File không thuộc chế độ ghi (không ghi, hoặc ngoài khung giờ) bị xoá **khi file kế tiếp của camera đó ghi xong**. Như vậy luôn còn file vừa xong, đủ 30 giây trước cho báo động rơi vào đầu file mới. [chủ nhà, Claude thêm phần giữ file vừa xong]
- Camera có giới hạn dung lượng: chạm giới hạn thì xoá file thường cũ nhất **của camera đó**. [chủ nhà]
- Tổng file thường của mọi camera không quá **80% ổ** (chỉnh được). Chạm mức đó thì xoá file thường cũ nhất **toàn ổ**. [chủ nhà; chỉnh được: Claude]

**Đoạn báo động** [chủ nhà]:
- Khi có báo động lúc thời điểm `t`: **giữ mọi file có phần nằm trong khoảng [t − 30 s, t + 30 s]**. Thường là 1 file, nhiều nhất 3 file.
- Các file này chuyển sang khu lưu báo động, kèm thông tin: khu, camera, giờ, loại sự kiện, ảnh chụp.
- Đoạn báo động **không bao giờ tự xoá**. Chỉ chủ nhà xoá, trên dashboard.

**Khi ổ sắp đầy** [Claude]:
- Đoạn báo động dùng hết 20% để dành: lấn sang phần ghi thường (xoá file thường cũ nhất) và gửi nhắc dọn ngay, không đợi hết tuần.
- Ổ còn dưới 2% trống mà không còn file thường để xoá: dừng ghi thường, gửi tin Telegram khẩn và hiện cảnh báo trên dashboard.

**Nhắc dọn:** mỗi tuần một tin (dashboard và Telegram) ghi số đoạn báo động và số GB đang giữ, kèm nút mở danh sách. [chủ nhà]

**Xem lại trên dashboard** [Claude]:
- Trang camera có dòng thời gian của từng camera, gồm phần đã ghi và phần mất hình.
- Có danh sách đoạn báo động: xem, tải về, xoá.
- Cài đặt hiện mỗi camera đã dùng bao nhiêu GB, và với mức ghi hiện tại thì xem lại được khoảng bao nhiêu ngày.

**Camera mất kết nối:** hub tự nối lại; khoảng mất hình đánh dấu trên dòng thời gian. Mất quá 10 phút thì gửi tin loại "Thiết bị". [Claude]

### 7.4 Ảnh chụp

- Lấy ảnh theo thứ tự: ISAPI `picture` của camera; ONVIF `GetSnapshotUri`; nếu không được thì lấy một khung hình từ `go2rtc`.
- Hub giữ ảnh mới nhất của mỗi camera trong một thời gian ngắn, để nhiều người xem không làm camera Wi‑Fi bị gọi liên tục. [báo cáo 3.1]
- Ảnh gửi Telegram lúc báo động phải chụp **ngay khi có sự kiện**, không dùng ảnh cũ trong bộ đệm. [chủ nhà]

## 8. Thứ tự làm

1. **Telegram:** nhỏ, dùng được ngay cho mọi cảnh báo đang có.
2. **Camera nối trực tiếp + sự kiện + ghi hình.**
3. **Chế độ canh, còi, đoạn báo động.**

Mỗi phần chạy được và kiểm được riêng trên máy thử Ubuntu trước khi làm phần sau. [Claude]

## 9. Phải đo trên hub trước khi coi là xong

- **Máy đo:** máy thử Ubuntu (Ryzen 7 5700G), cách đo như mục 15.1. Hub Center 4 GB chưa có, nên ngưỡng đặt theo từng camera để suy ra cho máy đó. [Claude]
- **Tải:** CPU và RAM của `hubd` khi ghi 4 và 10 camera giả lập RTSP. Lấy cả luồng H.264 4 Mbit/s và luồng H.265. **Ngưỡng đạt:** mỗi camera đang ghi làm `hubd` tăng không quá **30 MB RAM** và **5% một nhân CPU**. Mục tiêu 10 camera. [Claude]
- **Đĩa:** tốc độ ghi ra ổ; thời gian xoá file khi chạm giới hạn.
- **Đủ 30 giây trước và sau:** thử báo động rơi vào đầu, giữa và cuối một file, kiểm đoạn giữ lại có đủ 30 giây mỗi bên.
- **Độ trễ:** từ lúc camera báo sự kiện đến lúc ảnh tới Telegram.
- **Không bao giờ xoá đoạn báo động:** quét mọi tình huống đầy ổ (theo camera, theo 80%, ổ gần hết chỗ) và kiểm không đoạn báo động nào bị xoá.

## 10. Để người làm quyết

- Ghi hình bằng thư viện Go thuần hay `ffmpeg -c copy` chạy làm tiến trình con của `hubd`. Báo cáo ước tính cách `ffmpeg` mất 8–12 ngày công.
- Dạng file (MP4 phân mảnh hay MPEG‑TS), miễn đáp ứng mục 7.3.
- Chi tiết giao diện: dòng thời gian, danh sách đoạn báo động, bảng loại tin.
- Cách lưu thông tin file và đoạn báo động: SQLite của hub hay file đi kèm.
- Thư viện ONVIF và WS‑Discovery.

## 11. Chưa biết

- Camera Wi‑Fi ghi liên tục có làm nghẽn mạng 2,4 GHz chung với các node cảm biến không. Báo cáo mục 3.5 cảnh báo ba camera 2 MP đã là giới hạn.
- Camera trong nhà có còn nằm sau cổng PoE của đầu thu không. Nếu còn, phải chuyển sang switch PoE trước khi bỏ đầu thu.
- Xem H.265 trên Chrome máy tính phụ thuộc phần cứng. iPad Safari thì xem được.
