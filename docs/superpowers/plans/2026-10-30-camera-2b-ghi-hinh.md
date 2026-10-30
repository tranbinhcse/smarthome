# Phần 2B: Ghi hình camera — kế hoạch làm

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hub tự ghi luồng chính của mọi camera vào ổ cứng chủ nhà lắp:
- file tối đa 50 MB, cắt ở keyframe;
- giữ hay xoá theo chế độ ghi của từng camera, giới hạn riêng và 80% ổ;
- xem lại trên dashboard theo dòng thời gian;
- báo khi ổ đầy làm dừng ghi.

**Architecture:**
- **Nguồn ghi là `go2rtc`.** hubd mở `GET /api/stream.mp4?name=<id>_rec&src=<RTSP luồng chính>` của go2rtc. Kết quả là MP4 phân mảnh, không chuyển mã, mỗi khung hình một cặp `moof`+`mdat`. hubd chỉ đọc đầu box, cắt file và ghi xuống ổ.
- **Chỉ mục file** nằm trong SQLite của hub, bảng `recordings`. Quy tắc giữ/xoá là một hàm thuần `recPlan`, chạy sau mỗi file ghi xong và mỗi phút.
- **Xem lại bằng HLS** do hubd sinh từ chỉ mục: playlist VOD trỏ thẳng vào từng file bằng byte range, mỗi đoạn là một GOP. Safari phát sẵn, tua được, nối nhiều file liền nhau.
- **Thư mục ghi** là thư mục máy chủ gắn vào container ở `/rec`. hub chỉ ghi khi `/rec` nằm trên thiết bị khác với ổ hệ thống.

**Tech Stack:** Go stdlib (tự đọc box MP4, không thêm thư viện), SQLite (`history.go`), go2rtc 1.9.14 sẵn có, JS thuần (`web/dash.js`, `web/setup-camera.js`, tệp mới `web/setup-storage.js`).

**Spec:** `docs/superpowers/specs/2026-10-29-camera-bao-dong-telegram-design.md`, mục 2 (điểm 3–8), mục 3, mục 4, mục 7.3, mục 9, mục 10.

## Quyết định của người làm (mục 10 đặc tả)

Đã đo trên hub ngày 2026-10-30 với camera thật, luồng phụ H.265 qua go2rtc:
- 20 giây MP4 phân mảnh: `ftyp`, `moov`, rồi 451 cặp `moof`+`mdat`;
- keyframe mỗi 2 giây, đánh dấu bằng `default_sample_flags` trong `tfhd` (`0x02000000` là keyframe);
- nhãn codec là `hev1`;
- go2rtc dùng 1,3% CPU.

1. **Ghi qua go2rtc + Go thuần, không dùng `ffmpeg`.**
   - Không phải thêm `ffmpeg` vào image hubd.
   - Mật khẩu không nằm trên dòng lệnh của tiến trình con.
   - Cắt đúng 50 MB ở keyframe (`ffmpeg -f segment` chỉ cắt theo thời gian).
   - Mỗi camera chỉ có một kết nối RTSP cho việc ghi.
   - Cái giá: go2rtc khởi động lại thì mọi camera mất vài giây hình. hubd tự nối lại.
2. **Dạng file:** MP4 phân mảnh như go2rtc trả. Mỗi file mở đầu bằng `ftyp`+`moov` riêng, đổi nhãn `hev1` thành `hvc1` (Safari chỉ phát `hvc1`), `tfdt` đếm lại từ 0.
3. **Xem lại:** HLS byte range (Safari phát MP4 phân mảnh không có chỉ mục thì không tua được). Chrome máy tính không có HLS sẵn: hiện lời nhắc dùng Safari.
4. **Chỉ mục:** SQLite, bảng `recordings`.
5. **Không có âm thanh.** Luồng go2rtc trả về chỉ có hình (camera Hikvision mặc định G.711, MP4 không chứa).
6. **Đoạn báo động:** 2B có cờ `held` và hàm `KeepAround`, để quy tắc xoá luôn tôn trọng đoạn giữ lại. Danh sách đoạn báo động, nhắc dọn hằng tuần, nhắc khi dùng hết 20% làm ở Phần 3.
7. **Đo tải:** đo với các camera thật đang có (5 camera H.265). Ra số cho mỗi camera rồi suy ra 10 camera. Giả lập 10 luồng RTSP cần thêm dịch vụ, nên để sau.

## Global Constraints

- **Nơi chạy:** mọi phần mới nằm trong `hubd`. Không thêm container, không thêm thư viện Go, không cài `ffmpeg` vào image hubd.
- **Không chuyển mã:** ghi đúng byte go2rtc trả, chỉ sửa đầu box (`hev1`→`hvc1`, `tfdt`).
- **Không ghi vào ổ hệ thống:** chỉ ghi khi `HUBD_REC_DIR` (trong container `/rec`) nằm trên thiết bị (`st_dev`) khác với file cấu hình `HUBD_CONFIG` (gắn từ ổ hệ thống).
- **File:** tối đa `recMaxBytes = 50_000_000` byte. Ngoại lệ duy nhất: một GOP lớn hơn chỗ còn lại thì không cắt đôi được (xem Task 1).
- **Giữ/xoá:** ghi thường dùng tối đa `rec_max_pct` % ổ, mặc định 80, chỉnh 50–95 (nhóm cấu hình `camera`). Còn dưới 2% trống mà không còn file thường để xoá thì dừng ghi. File `held` **không bao giờ** bị xoá tự động.
- **Giờ:** khung giờ và tên thư mục theo múi giờ cố định `tgICT` (`telegram_daily.go`).
- **Bí mật:** URL RTSP có mật khẩu. Không ghi vào log, lỗi trả về, tên file, chỉ mục, API. Lỗi của `http.Client` chứa URL, nên chỉ log mã trạng thái hoặc câu cố định.
- **Mọi API ghi hình nằm sau PIN** (`a.Require`). Đường dẫn file chỉ lấy từ chỉ mục, không bao giờ từ tham số người dùng.
- **Cách làm việc:**
  - test chạy trên máy Ubuntu bằng `scripts/test-on-hub.sh`, không chạy trên Mac;
  - chữ giao diện tiếng Việt có dấu;
  - đổi file web thì nâng `?v=` ở mọi thẻ trong `web/index.html`;
  - conventional commits, không ghi dòng AI attribution.

## Review Focus

1. **hubd khởi động lại giữa lúc đang ghi** (đổi camera là khởi động lại): file dở không có trong chỉ mục thì không bao giờ bị xoá và làm đầy ổ. Lúc khởi động, hub thêm các file như vậy vào chỉ mục. Test ở Task 4.
2. **Rút ổ hoặc ổ lỗi khi đang ghi:** lỗi ghi làm dừng luồng đó, báo một lần, không thử lại liên tục. Hub thử lại ở lần kiểm ổ kế tiếp. Test ở Task 4.
3. **Luồng go2rtc đứt giữa file** (camera khởi động lại, go2rtc khởi động lại): phần đã ghi phải đóng thành file xem được, có trong chỉ mục. Hub nối lại sau 2 giây, chờ tăng dần tới 60 giây. Test ở Task 1 và Task 4.
4. **GOP dài** (H.265+ của Hikvision có thể 10 giây một keyframe): vẫn cắt ở keyframe, file không vượt 50 MB, bộ đệm mỗi camera có giới hạn. Test ở Task 1.
5. **Safari tải playlist và file bằng cookie PIN:** đường dẫn trong playlist là tương đối, cùng nguồn. Mọi đường xem lại không có cookie thì trả 401. Test ở Task 7.

---

### Task 1: Cắt luồng MP4 phân mảnh thành file

**Files:**
- Create: `recmp4.go`, `recmp4_test.go`

**Interfaces:**
- Produces:
  - `const recMaxBytes = 50_000_000`
  - `type recSeg struct { Path string; Start, End time.Time; Bytes int64 }`
  - `func recSplit(r io.Reader, maxBytes int64, create func(start time.Time) (io.WriteCloser, string, error), now func() time.Time, done func(recSeg)) error`
  - Bộ dựng luồng giả trong test (dùng lại ở Task 4, Task 7), ví dụ `fakeFMP4(codec string, gops [][]int) []byte`: `ftyp`, `moov` có một `trak` hình (`hdlr` = `vide`, `stsd` một mục `hev1` hoặc `avc1`, `mdhd` timescale 90000), mỗi GOP là các cặp `moof`+`mdat`. Mỗi `moof` có `mfhd`, `traf` gồm `tfhd` (cờ `0x020038`: duration, size, default flags; keyframe `0x02000000`, khung thường `0x01010000`), `tfdt` phiên bản 1 và `trun` một mẫu.

**Cách làm (các điểm chữ ký và test chưa định):**
- Đọc lần lượt từng box cấp ngoài: 4 byte độ dài + 4 byte loại; độ dài `1` thì đọc 8 byte độ dài lớn.
- Giữ `ftyp`+`moov` làm phần đầu của mọi file.
  - Trong `moov`: tìm track hình (`hdlr` `vide`) và đổi loại mục mẫu trong `stsd` từ `hev1` thành `hvc1`. Đi đúng cây `trak/mdia/minf/stbl/stsd`, không thay chuỗi mù.
- `moof` là keyframe khi `traf` của track hình có cờ mẫu đầu không bật bit `0x00010000` (không đồng bộ). Cờ lấy theo thứ tự:
  - `first_sample_flags` của `trun` (cờ `0x4`);
  - nếu không có thì cờ của mẫu đầu (cờ `0x400`);
  - nếu không có thì `default_sample_flags` của `tfhd` (cờ `0x20`).
- `moof` không có `traf` của track hình thì không bao giờ là chỗ cắt.
- Bỏ mọi `moof`+`mdat` trước keyframe đầu tiên.
- **Đệm từng GOP:** từ một keyframe tới trước keyframe kế.
  - Tới keyframe kế: nếu file hiện tại đã có dữ liệu và `byte file + byte GOP > maxBytes` thì đóng file, gọi `done`, mở file mới bằng `create(start của GOP)`, rồi ghi GOP.
  - Bộ đệm GOP tối đa 16 MiB. Vượt mức đó thì ghi phần đệm vào file hiện tại luôn (chỉ trường hợp này file mới vượt `maxBytes`).
- Mỗi file viết lại `tfdt` của từng `traf`: trừ `tfdt` đầu tiên của track đó trong file. Hỗ trợ cả phiên bản 0 (32 bit) lẫn 1 (64 bit).
- `Start` của file là `now()` lúc nhận keyframe đầu của file. `End` là `now()` lúc nhận keyframe kế sau GOP cuối, hoặc lúc luồng hết.
- **Luồng hết hay lỗi:**
  - ghi GOP đang đệm (chỉ các cặp `moof`+`mdat` đầy đủ), đóng file, gọi `done`;
  - trả `nil` khi hết sạch ở ranh giới box, `io.ErrUnexpectedEOF` khi hết giữa box, hoặc lỗi đọc/ghi.

- [ ] **Step 1: Viết test (đỏ)**
  - `TestRecSplitCutsAtKeyframesUnderLimit`: 30 GOP, mỗi GOP 50 khung × 2000 byte, `maxBytes` 1 000 000.
    - Ít nhất 3 file, file nào cũng `<= maxBytes`.
    - Mỗi file bắt đầu `ftyp`, `moov`, rồi một `moof` keyframe.
    - Nối mọi `mdat` của các file ra, đúng thứ tự và byte như đầu vào.
  - `TestRecSplitRenamesHev1`: `moov` ở đầu ra có `hvc1`, không còn `hev1`. Đầu vào `avc1` thì giữ nguyên.
  - `TestRecSplitRebasesTfdt`: `tfdt` của `moof` đầu mỗi file bằng 0. Trong một file, `tfdt` tăng dần.
  - `TestRecSplitDropsUntilFirstKeyframe`: 3 khung thường rồi mới tới keyframe. File đầu bắt đầu ở keyframe; 3 khung kia không có trong đầu ra.
  - `TestRecSplitTruncatedStream`: luồng cắt ngang giữa một `mdat`.
    - Các file đã ghi đều được báo qua `done`.
    - Đọc lại từng file thấy toàn box đầy đủ.
    - Hàm trả `io.ErrUnexpectedEOF`.
  - `TestRecSplitLongGOP`:
    - GOP 300 khung × 10 000 byte (3 MB), `maxBytes` 5 000 000: mỗi file đúng một GOP, không file nào vượt.
    - Một GOP 20 MB: ghi đủ, không panic, bộ đệm không quá 16 MiB.
  - `TestRecSplitAudioMoofNotCut`: `moof` chỉ có `traf` của track âm thanh (track_ID 2) đứng giữa các khung hình thì không thành chỗ cắt.
- [ ] **Step 2:** `scripts/test-on-hub.sh` → các test trên FAIL (`recSplit` chưa có).
- [ ] **Step 3: Viết `recSplit`** theo các điểm ở trên.
- [ ] **Step 4:** `scripts/test-on-hub.sh` → TẤT CẢ ĐẠT.
- [ ] **Step 5: Commit** `feat(rec): split go2rtc fragmented MP4 into files cut at keyframes`

### Task 2: Chỉ mục file và quy tắc giữ/xoá

**Files:**
- Create: `recstore.go`, `recstore_test.go`
- Modify: `history.go` (thêm bảng vào `OpenDB`)

**Interfaces:**
- Produces:
  - Bảng: `CREATE TABLE IF NOT EXISTS recordings(id INTEGER PRIMARY KEY, cam TEXT NOT NULL, path TEXT NOT NULL UNIQUE, start INTEGER NOT NULL, end INTEGER NOT NULL, bytes INTEGER NOT NULL, held INTEGER NOT NULL DEFAULT 0); CREATE INDEX IF NOT EXISTS recordings_cam_start ON recordings(cam, start);` (thời gian tính bằng mili giây Unix)
  - `type recFile struct { ID int64; Cam, Path string; Start, End time.Time; Bytes int64; Held bool }`
  - `type recWindow struct { From string `json:"from"`; To string `json:"to"` }` (`"HH:MM"`; `To` nhỏ hơn `From` là qua đêm)
  - `type recPolicy struct { Mode string; Windows []recWindow; LimitBytes int64 }`
  - `var recModes = []string{"always", "schedule", "off"}`: tập đóng, Task 5 kiểm hợp lệ và test quét đều sinh từ đây
  - `type recDisk struct { OK bool; Reason string; Total, Free int64 }`
  - `type recDecision struct { Delete []int64; Stop bool }`
  - `func recAdd(db *DB, f recFile) (int64, error)`
  - `func recAll(db *DB) ([]recFile, error)` (theo `start` tăng dần)
  - `func recRange(db *DB, cam string, from, to time.Time) ([]recFile, error)`: file của camera có phần nằm trong `[from, to]`
  - `func recRemove(db *DB, ids []int64) error`: chỉ xoá dòng, xoá file là việc của bên gọi
  - `func recHoldRange(db *DB, cam string, from, to time.Time) (int, error)`: đặt `held = 1` cho file có phần nằm trong khoảng, trả số file
  - `func recWanted(p recPolicy, f recFile) bool`
  - `func recPlan(files []recFile, pol map[string]recPolicy, d recDisk, normalPct int) recDecision`

**Quy tắc `recWanted`:**
- `always` → true; `off` → false.
- `schedule` → true khi `[Start, End]` của file có phần nằm trong một khung nào đó, ngày nào cũng vậy, theo `tgICT`.
- Camera không có trong `pol` (camera đã xoá, kênh đầu thu) tính như `always`.

**Quy tắc `recPlan`** (theo thứ tự; "file thường" là `!Held`; "file mới nhất" là file có `Start` lớn nhất của camera đó):
1. Với từng camera: mọi file thường không phải file mới nhất và `!recWanted` → xoá.
2. Camera có `LimitBytes > 0`: khi tổng file thường còn lại lớn hơn `LimitBytes`, xoá file thường cũ nhất của camera đó, trừ file mới nhất.
3. Khi tổng file thường còn lại lớn hơn `normalPct`% × `Total`, xoá file thường cũ nhất toàn ổ, trừ file mới nhất của mỗi camera.
4. Khi `Free` cộng số byte đã xoá vẫn nhỏ hơn 2% `Total`, xoá tiếp file thường cũ nhất, lần này kể cả file mới nhất.
   - Xoá hết file thường mà vẫn dưới 2% thì `Stop = true`.
- `d.OK == false` → trả quyết định rỗng.
- `held` không bao giờ nằm trong `Delete`.

- [ ] **Step 1: Viết test (đỏ)**
  - `TestRecWantedScheduleOvernight`: khung `22:00–06:00`.
    - File 05:58–06:01 được giữ; 06:01–06:03 không; 21:59–22:00 được giữ.
    - Chế độ `always` → true, `off` → false.
  - `TestRecPlanRules`: mỗi quy tắc 1–4 một ca, kiểm đúng `Delete` và thứ tự xoá (cũ trước).
  - `TestRecPlanSweepNeverDeletesHeld` — quét tích các tập đóng:
    - các tập: `recModes` × `held ∈ {false, true}` × `LimitBytes ∈ {0, 3 file}` × trạng thái ổ `{dư, vượt normalPct, dưới 2%}` × vị trí file `{duy nhất, cũ nhất, giữa, mới nhất}`;
    - mỗi ca 2 camera, mỗi camera 4 file;
    - kiểm tra cho mọi ca:
      - (a) không id `held` nào trong `Delete`;
      - (b) file mới nhất của camera chỉ bị xoá khi ổ dưới 2%;
      - (c) `Stop` chỉ khi mọi file thường đã nằm trong `Delete` mà vẫn dưới 2%;
    - in số ca đã chạy, phải bằng tích kích thước các tập.
  - `TestRecHoldRange`: ba file A [0,100 s], B [100,200 s], C [200,300 s].
    - Báo động ở 105 s (đầu file B): giữ A, B.
    - Ở 150 s (giữa): chỉ giữ B.
    - Ở 195 s (cuối): giữ B, C.
- [ ] **Step 2:** `scripts/test-on-hub.sh` → FAIL.
- [ ] **Step 3: Viết** bảng và các hàm trên.
- [ ] **Step 4:** `scripts/test-on-hub.sh` → TẤT CẢ ĐẠT.
- [ ] **Step 5: Commit** `feat(rec): recording index and keep/delete rules`

### Task 3: Ổ ghi hình và API dung lượng

**Files:**
- Create: `recdisk.go`, `recdisk_test.go`
- Modify: `docker-compose.yml` (dịch vụ `hubd`), `main.go`

**Interfaces:**
- Consumes: `recDisk`, `recAll`, `recPolicy` (Task 2)
- Produces:
  - `func recDiskCheck(dir, sysRef string) recDisk`: dùng `syscall.Stat_t.Dev` và `syscall.Statfs`
  - `func recNormalPct(db *DB) int`: đọc `camera.rec_max_pct`, ép vào 50–95, mặc định 80
  - `GET /api/rec/storage` (sau PIN) trả `{"ok", "reason", "total", "free", "normal_bytes", "held_bytes", "max_pct", "cams": [{"id", "bytes", "days"}]}`
    - `days`:
      - `rate` = số byte file của camera đó kết thúc trong 24 giờ qua;
      - ngân sách = `LimitBytes` nếu có, nếu không = `max_pct`% × `total` × `rate` / tổng `rate` mọi camera;
      - `days` = ngân sách / `rate`, làm tròn 1 chữ số; `rate` = 0 thì `null`.

**Lý do không đạt** (chữ hiển thị, dùng đúng):
- `dir` trống hoặc không tồn tại → `"Chưa gắn thư mục ghi hình"`
- cùng thiết bị với `sysRef` → `"Thư mục ghi hình nằm trên ổ hệ thống: gắn ổ cứng riêng vào /srv/hubd-video"`
- tạo/xoá file `.hubd-write-test` lỗi → `"Không ghi được vào ổ ghi hình"`

**docker-compose.yml**, dịch vụ `hubd`:
- thêm `HUBD_REC_DIR: /rec`;
- thêm một volume:
  ```yaml
  - type: bind
    source: ${HUBD_REC_HOST:-/srv/hubd-video}
    target: /rec
    bind: {create_host_path: true}
  ```
- ghi chú ngay đó: gắn ổ xong thì chạy lại `docker compose up -d hubd`;
- thư mục tự tạo trên ổ hệ thống bị `recDiskCheck` từ chối.

- [ ] **Step 1: Viết test (đỏ)**
  - `TestRecDiskSameDeviceRefused`: `dir` và `sysRef` cùng trong `t.TempDir()` → `OK=false`, đúng lý do ổ hệ thống.
  - `TestRecDiskMissing`: `dir` không tồn tại → lý do "Chưa gắn".
  - `TestRecDiskOtherDevice`: `dir` trong `/dev/shm` (tmpfs), `sysRef` trong `t.TempDir()`.
    - `OK=true`, `Total > 0`, file thử đã xoá.
    - Không có `/dev/shm` thì `t.Skip`.
  - `TestRecStorageAPI`: 2 camera, file giả trong chỉ mục.
    - Đúng `normal_bytes`, `held_bytes`, `max_pct` mặc định 80, `days` theo công thức.
    - Không cookie → 401.
  - `TestRecNormalPctClamp`: 30 → 50, 99 → 95, thiếu → 80.
- [ ] **Step 2:** `scripts/test-on-hub.sh` → FAIL.
- [ ] **Step 3: Viết** `recdisk.go`, gắn route trong `main.go`, sửa compose.
- [ ] **Step 4:** `scripts/test-on-hub.sh` → TẤT CẢ ĐẠT.
- [ ] **Step 5: Commit** `feat(rec): recording disk check and storage API`

### Task 4: Bộ ghi

**Files:**
- Create: `recorder.go`, `recorder_test.go`
- Modify: `camera.go` (tách hàm URL RTSP của kênh đầu ghi), `camstore.go` (`camPolicies`), `main.go`

**Interfaces:**
- Consumes: `recSplit`, `recMaxBytes` (Task 1); bảng và hàm chỉ mục, `recPlan` (Task 2); `recDiskCheck`, `recNormalPct` (Task 3); `camList`, `camRec.rtsp(false)` (2A).
- Produces:
  - `type recSource struct { ID, URL string }`: `URL` có mật khẩu, không bao giờ log
  - `func nvrRTSP(n nvrConf, ch int, sub bool) string`
    - tách từ `camera.go`, hàm `live` (đoạn dựng `src` của luồng phụ đầu ghi, gần dòng 205);
    - luồng chính `/Streaming/Channels/<ch>01`, luồng phụ `<ch>02`;
    - chỗ cũ gọi lại hàm này.
  - `func camPolicies(db *DB) map[string]recPolicy`: camera nối thẳng theo cấu hình (Task 5 thêm trường; trước đó mọi camera là `always`)
  - `func newRecorder(db *DB, go2rtc *url.URL, dir string, disk func() recDisk, sources func() []recSource, policies func() map[string]recPolicy, notify func(level, kind, text string)) *recorder`
  - `func (r *recorder) run(ctx context.Context)`
  - `func (r *recorder) KeepAround(cam string, t time.Time)`: giữ `[t−30s, t+30s]`. File đang ghi có phần trong khoảng đó được giữ khi ghi xong. Phần 3 sẽ gọi hàm này.

**Cách chạy:**
- **Khởi động** (`run`):
  - quét `dir/*/*/*.mp4`;
  - file chưa có trong chỉ mục thì thêm vào:
    - `Cam` lấy từ thư mục cấp 1;
    - `Start` lấy từ đường dẫn `<YYYYMMDD>/<HHMMSS>.mp4` theo `tgICT`;
    - `End` là mtime, `Bytes` là kích thước.
- **Mỗi 60 giây:**
  - kiểm ổ (`disk()`);
  - so `sources()` với các luồng đang chạy: mở thêm, đóng luồng thừa;
  - chạy `recPlan(recAll, policies(), disk, recNormalPct)`, xoá file trong `Delete` rồi `recRemove`.
- **Mỗi nguồn một goroutine:**
  - gọi `GET <go2rtc>/api/stream.mp4?name=<id>_rec&src=<URL>`, rồi `recSplit(body, recMaxBytes, …)`;
  - file nằm ở `dir/<id>/<YYYYMMDD>/<HHMMSS>.mp4` theo `tgICT`; trùng tên thì thêm `-1`, `-2`;
  - mỗi file xong (`done`):
    - `recAdd`;
    - nếu có lệnh giữ đang chờ phủ file này thì `recHoldRange`;
    - chạy `recPlan` như trên.
  - Luồng hết hay lỗi thì chờ rồi nối lại: 2 giây, gấp đôi mỗi lần, tối đa 60 giây. Ghi xong một file thì về lại 2 giây.
- **Lỗi ghi file** (ổ bị rút, ổ đầy):
  - đóng luồng đó, đánh dấu ổ hỏng tới lần kiểm kế tiếp;
  - báo một lần mỗi lần chuyển trạng thái: `notify("warn", "luu_tru", "Không ghi được vào ổ ghi hình, đã dừng ghi.")`.
- **Ổ chuyển từ đạt sang không đạt** lúc đang chạy: dừng mọi luồng, báo `notify("warn", "luu_tru", <lý do>)` một lần. Lúc khởi động mà ổ không đạt thì không báo, vì hubd khởi động lại mỗi khi đổi camera.
- **`recPlan` trả `Stop`:**
  - dừng mọi luồng;
  - báo một lần `notify("urgent", "luu_tru", "Ổ ghi hình gần đầy: đã dừng ghi hình thường, chỉ còn giữ đoạn báo động.")`;
  - ghi lại khi còn trống từ 3% trở lên, báo `notify("info", "luu_tru", "Ổ ghi hình đã có chỗ, ghi hình lại.")`.
- **`main.go`:**
  - `HUBD_REC_DIR` khác rỗng thì tạo recorder và `go rec.run(ctx)`;
  - `disk` = `recDiskCheck(HUBD_REC_DIR, HUBD_CONFIG)`;
  - `sources`:
    - camera nối thẳng: `camList` → `rec.rtsp(false)`;
    - kênh đầu ghi: `camDevices` của nhóm `nvr` → `nvrRTSP(c.conf(), ch, false)`; chưa có IP đầu ghi thì bỏ qua.
- **Log** chỉ có dạng `ghi hình <id>: <câu cố định hoặc mã HTTP>`.

- [ ] **Step 1: Viết test (đỏ).** Dùng go2rtc giả (`httptest`) trả `fakeFMP4` của Task 1, `disk` giả luôn đạt, thư mục tạm.
  - `TestRecorderWritesAndIndexes`: một nguồn, luồng 6 GOP rồi đóng.
    - File nằm đúng `dir/<id>/<ngày>/<giờ>.mp4`.
    - Chỉ mục có đủ file, tổng `Bytes` bằng kích thước trên đĩa.
    - Query gửi go2rtc có `name=<id>_rec` và `src` đúng.
  - `TestRecorderReconnects`: go2rtc giả đóng luồng 2 lần → có ít nhất 3 yêu cầu, mỗi lần đứt đều để lại file đọc được.
  - `TestRecorderNeverLogsURL`:
    - nguồn có URL `rtsp://u:p%40ss@10.0.0.9:554/x`, go2rtc giả trả 500 rồi đóng kết nối;
    - log (bắt bằng `log.SetOutput`) không chứa `rtsp://`, `p%40ss`, `p@ss`.
  - `TestRecorderKeepAroundPending`: gọi `KeepAround` khi file đang ghi → file đó có `held=1` sau khi xong.
  - `TestRecorderAdoptsOrphans`: đặt sẵn `dir/cam_x/20261030/101500.mp4` không có trong chỉ mục → sau khởi động có dòng với `Start` 10:15:00 ICT.
  - `TestRecorderDiskWriteError`: `create` trả lỗi ghi.
    - Luồng dừng; `notify` gọi đúng 1 lần với kind `luu_tru`.
    - Không mở yêu cầu go2rtc mới trước lần kiểm ổ kế tiếp.
  - `TestRecorderStopAndResume`: `disk` giả còn 1% trống, không file thường → `notify("urgent", …)` 1 lần, luồng dừng. Đổi thành 4% → ghi lại, `notify("info", …)` 1 lần.
  - `TestRecorderDiskNotOKNoRequests`: `disk` không đạt → go2rtc giả không nhận yêu cầu nào.
- [ ] **Step 2:** `scripts/test-on-hub.sh` → FAIL.
- [ ] **Step 3: Viết** `recorder.go`, `nvrRTSP`, `camPolicies`, gắn vào `main.go`.
- [ ] **Step 4:** `scripts/test-on-hub.sh` → TẤT CẢ ĐẠT (gồm các test camera cũ).
- [ ] **Step 5: Commit** `feat(rec): record every camera through go2rtc`

### Task 5: Chế độ ghi của từng camera

**Files:**
- Modify: `camstore.go`, `camstore_test.go`, `web/setup-camera.js`, `web_selectors_test.go`, `web/index.html` (`?v=`)

**Interfaces:**
- Consumes: `recModes`, `recWindow`, `recPolicy` (Task 2); `camPolicies` (Task 4)
- Produces: `camRec` thêm trường
  - `RecMode string` (json `rec_mode`): trống nghĩa là `always`
  - `RecWindows []recWindow` (json `rec_windows`)
  - `RecLimitGB float64` (json `rec_limit_gb`): 0 = theo giới hạn chung

**Kiểm hợp lệ trong `camSave`** (sai thì 400, chữ tiếng Việt):
- `RecMode` thuộc `recModes`, hoặc trống (lưu thành `always`).
- `schedule` cần 1–6 khung.
  - Mỗi khung `From`/`To` đúng `HH:MM` (00:00–23:59) và `From != To`.
  - Chế độ khác thì bỏ `RecWindows`.
- `0 <= RecLimitGB <= 100000`.
- `view()` trả 3 trường này.
- `camPolicies` đổi GB thành byte: `int64(RecLimitGB * 1e9)`.

**Giao diện** (form thêm/sửa camera):
- Ô chọn **Chế độ ghi** với các lựa chọn:
  - `Toàn thời gian` (`always`);
  - `Theo khung giờ` (`schedule`);
  - `Không ghi (vẫn giữ đoạn báo động)` (`off`).
- Ô **Khung giờ ghi**: chỉ hiện khi chọn theo khung giờ, gợi ý `22:00-06:00, 12:00-13:30`. Trình duyệt tách thành `[{from, to}]`; sai định dạng thì báo ngay, không gửi.
- Ô số **Giới hạn dung lượng (GB)**, gợi ý `0 = theo giới hạn chung`.
- Danh sách camera hiện tên chế độ ở mỗi dòng. Dữ liệu mẫu (`DEMO_CAMS`) có đủ 3 chế độ.
- Các trường mới đi theo cách vẽ lại hiện có (`F`, không nằm trong `data-sfield` bí mật). Mật khẩu vẫn theo ruling R9 của 2A.

- [ ] **Step 1: Viết test (đỏ)**
  - `TestCamSaveRecMode`:
    - mode lạ → 400;
    - `schedule` không khung → 400;
    - khung `25:00-06:00` → 400;
    - `From == To` → 400;
    - limit âm → 400;
    - trống → lưu `always`;
    - `schedule` 2 khung → lưu đúng; đổi sang `always` thì `rec_windows` bị bỏ.
  - `TestCamPolicies`: GB → byte; camera chưa có trường → `always`.
  - `TestCameraSetupWired` thêm: có đủ 3 nhãn chế độ đúng chữ trên, ô khung giờ, ô giới hạn GB.
- [ ] **Step 2:** `scripts/test-on-hub.sh` → FAIL.
- [ ] **Step 3: Viết** phần máy chủ và giao diện; nâng `?v=` mọi thẻ.
- [ ] **Step 4:** `scripts/test-on-hub.sh` → TẤT CẢ ĐẠT.
- [ ] **Step 5: Commit** `feat(rec): per-camera recording mode, windows and size limit`

### Task 6: Cài đặt → Hệ thống → Lưu trữ, loại tin "Lưu trữ"

**Files:**
- Create: `web/setup-storage.js`
- Modify: `web/settings.js` (`SETUP_SEC`: `storage: 'hethong'`), `web/index.html` (thẻ script mới, `?v=` mọi thẻ), `notify.go`, `notify_test.go`, `web_selectors_test.go`

**Interfaces:**
- Consumes: `GET /api/rec/storage` (Task 3); nhóm cấu hình `camera`, khoá `rec_max_pct`.

**Thẻ** `SETUP.push({id: 'storage', ns: 'camera', …, title: 'Lưu trữ'})`:
- **Dòng trạng thái:**
  - đạt: `Ổ ghi hình: <tổng>, đã dùng <đã dùng>, còn trống <trống>` (GB/TB, dấu phẩy thập phân);
  - không đạt: đúng chữ `reason`.
- **Ô chọn** "Ghi hình thường dùng tối đa": 50–95%, bước 5. Lưu bằng cách thẻ Camera đang lưu `lan_cidr` (`U.sinput`/nhóm `camera`).
- **Bảng mỗi camera:** tên (lấy từ `/api/cameras` và thiết bị `cam_ch*`), GB đã dùng, `khoảng N ngày` hoặc `chưa đủ dữ liệu`.
- **Dữ liệu mẫu** cho `?demo`.

**`notify.go`:** danh sách `/api/notify` thêm `{Kind: "luu_tru", Texts: ["Ổ ghi hình gần đầy, đã dừng ghi", "Không ghi được vào ổ ghi hình"]}` sau mục `thiet_bi`. Thẻ Đọc thông báo và Telegram có công tắc cho loại này, mặc định bật.

- [ ] **Step 1: Viết test (đỏ)**
  - `notify_test.go`: danh sách mong đợi có `luu_tru` ngay sau `thiet_bi`.
  - `TestStorageSetupWired`:
    - `setup-storage.js` có `title: 'Lưu trữ'`, gọi `/api/rec/storage`, có ô `rec_max_pct`;
    - `settings.js` có `storage: 'hethong'`;
    - `index.html` nạp `setup-storage.js`.
- [ ] **Step 2:** `scripts/test-on-hub.sh` → FAIL.
- [ ] **Step 3: Viết** thẻ, khai `SETUP_SEC`, thêm loại tin; nâng `?v=` mọi thẻ. Kiểm cú pháp bằng `node --check web/setup-storage.js`.
- [ ] **Step 4:** `scripts/test-on-hub.sh` → TẤT CẢ ĐẠT.
- [ ] **Step 5: Commit** `feat(rec): storage settings card and storage alert kind`

### Task 7: Xem lại trên dashboard

**Files:**
- Create: `recplay.go`, `recplay_test.go`
- Modify: `main.go` (gắn route), `web/dash.js` (bảng camera `camSheet` ~dòng 767, `liveFill` ~dòng 521), `web/demo.js`, `web_selectors_test.go`, `web/index.html` (`?v=`)

**Interfaces:**
- Consumes: `recRange` (Task 2); `fakeFMP4` (Task 1, cho test).
- Produces:
  - `type recGOP struct { Off, Len int64; Dur float64 }`
  - `func recScan(path string) (initLen int64, gops []recGOP, err error)`:
    - đọc đầu box, dừng ở box cuối còn nguyên;
    - thời lượng = tổng thời lượng mẫu (`trun` từng mẫu, hoặc `default_sample_duration` × số mẫu) / timescale `mdhd` của track hình.
  - Route (đều `a.Require`):
    - `GET /api/cam/{id}/rec?day=YYYY-MM-DD` → `{"spans": [[startMs, endMs], …]}`: file trong ngày đó theo `tgICT`, gộp hai file liền nếu cách nhau ≤ 10 giây.
    - `GET /api/cam/{id}/rec.m3u8?from=<ms>&to=<ms>` → playlist VOD.
      - `to - from` tối đa 24 giờ, sai thì 400.
      - Kiểu `application/vnd.apple.mpegurl`.
      - Bắt đầu từ GOP chứa `from`, hết ở GOP chứa `to`.
    - `GET /api/cam/{id}/rec/{file}.mp4` → `http.ServeContent`:
      - `{file}` là id trong chỉ mục; phải thuộc camera `{id}`, không thì 404;
      - đường dẫn trên đĩa chỉ lấy từ chỉ mục.

**Playlist (định dạng, URI tương đối):**
```
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:<làm tròn lên GOP dài nhất>
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="rec/<fileID>.mp4",BYTERANGE="<initLen>@0"
#EXTINF:<dur 3 chữ số>,
#EXT-X-BYTERANGE:<len>@<off>
rec/<fileID>.mp4
…   (sang file kế: #EXT-X-DISCONTINUITY rồi #EXT-X-MAP của file đó)
#EXT-X-ENDLIST
```

**Giao diện, bảng camera (`camSheet`)** — dưới khung xem trực tiếp thêm khối **Xem lại**:
- `<input type="date">`, mặc định hôm nay.
- Một thanh 24 giờ: các đoạn đã ghi tô màu, đoạn mất hình để trống, có mốc giờ 0, 6, 12, 18, 24.
- Chạm vào thanh:
  - nếu thời điểm đó nằm trong một đoạn: phát `<video controls playsinline>` với `src` là `/api/cam/<id>/rec.m3u8?from=<t>&to=<cuối đoạn>`;
  - nếu không: báo `Giờ này không có hình`.
- Trình duyệt không phát được HLS (`video.canPlayType('application/vnd.apple.mpegurl') === ''`) thì hiện `Xem lại chạy trên Safari (iPad, iPhone, Mac).`
- Khung `<video>` xem lại có thuộc tính `data-replay`. `liveFill` giữ nguyên phần tử này khi vẽ lại, như đang giữ `[data-live]`, để video không phát lại từ đầu mỗi lần trạng thái đổi.
- `?demo`: đoạn ghi giả, không gọi API.

- [ ] **Step 1: Viết test (đỏ)**
  - `TestRecScan`: file từ `recSplit(fakeFMP4)` → `initLen` đúng độ dài `ftyp`+`moov`, số GOP đúng, tổng `Len` + `initLen` = kích thước file, `Dur` đúng theo timescale.
  - `TestRecScanTruncated`: cắt cụt file giữa một `mdat` → GOP cuối không đủ bị bỏ, không lỗi.
  - `TestRecPlaylist`:
    - 2 file liền nhau;
    - playlist có đúng 1 `#EXT-X-DISCONTINUITY`, 2 `#EXT-X-MAP`;
    - mỗi `BYTERANGE` khớp `recScan`;
    - kết thúc `#EXT-X-ENDLIST`;
    - không chứa `http`, `/rec/` đầu tuyệt đối, hay đường dẫn đĩa.
  - `TestRecSpansMerge`: 3 file, khoảng cách 5 giây và 30 giây → 2 đoạn.
  - `TestRecPlaybackAuth`: cả 3 route không cookie → 401.
  - `TestRecFileOtherCamera`: id file của camera khác → 404.
  - `TestRecFileRange`: `Range: bytes=0-99` → 206, 100 byte.
  - `TestDashReplayWired`: `dash.js` có `data-replay`, có chữ `Giờ này không có hình`, có `liveFill` giữ `[data-replay]`.
- [ ] **Step 2:** `scripts/test-on-hub.sh` → FAIL.
- [ ] **Step 3: Viết** `recplay.go` và giao diện; nâng `?v=` mọi thẻ; `node --check web/dash.js`.
- [ ] **Step 4:** `scripts/test-on-hub.sh` → TẤT CẢ ĐẠT.
- [ ] **Step 5: Commit** `feat(rec): playback timeline and HLS playlists`

### Task 8: Chạy thật trên hub, đo, tài liệu

**Files:**
- Modify: `README.md` (mục mới **Ghi hình** sau "Camera nối thẳng"; mục "Chưa làm / giới hạn"), `docs/2026-10-26-trung-tam-dieu-khien-nha-design.md` (§15.1, một đoạn trỏ tới kế hoạch này)

- [ ] **Step 1: Ổ trên máy thử.** Máy thử chỉ có ổ NVMe hệ thống (đã xem bằng `lsblk` ngày 2026-10-30). **Hỏi chủ nhà, chỉ làm khi chủ nhà đồng ý một trong hai cách:**
  - (a) Chủ nhà cắm ổ HDD. Claude gắn vào `/srv/hubd-video` bằng UUID trong `/etc/fstab`. Không định dạng ổ nếu chủ nhà chưa đồng ý riêng.
  - (b) Ổ thử bằng file 20 GB `/srv/hubd-video.img`, gắn loop vào `/srv/hubd-video`. Ổ này nằm trên NVMe, chỉ dùng để thử; ghi liên tục làm mòn SSD.
- [ ] **Step 2: Triển khai** theo quy trình bundle, `docker compose up -d --build hubd`. Settings → Hệ thống → Lưu trữ báo ổ đạt.
- [ ] **Step 3: Kiểm** với các camera đang có:
  - mỗi camera có file trong `/srv/hubd-video/<id>/<ngày>/`;
  - không file nào quá 50 000 000 byte;
  - `ffprobe` (trong container `hub-go2rtc`) thấy khung đầu là keyframe, nhãn `hvc1`;
  - chỉ mục khớp số file trên đĩa.
- [ ] **Step 4: Chủ nhà xem lại trên iPad** (Safari): chọn một camera, chạm thanh thời gian, video phát và tua được.
- [ ] **Step 5: Giữ/xoá:**
  - đặt `rec_max_pct` 50 trên ổ thử: file cũ nhất bị xoá, tổng file thường dưới mức;
  - đặt một camera `off`: chỉ còn file mới nhất của camera đó;
  - đo thời gian xoá.
- [ ] **Step 6: Đo tải.**
  - RAM và CPU của `hubd` và `hub-go2rtc` trước khi ghi và khi ghi mọi camera thật, lấy 3 mẫu cách 5 giây; tốc độ ghi đĩa.
  - Ra số cho mỗi camera; ngưỡng đạt: hubd tăng ≤ 30 MB RAM và ≤ 5% một nhân CPU mỗi camera.
  - Ghi số đo, worktree và commit vào README.
- [ ] **Step 7: Tài liệu README** mục Ghi hình:
  - cách gắn ổ, nên dùng HDD dòng cho camera (surveillance) vì ghi liên tục làm mòn SSD;
  - chế độ ghi;
  - giữ/xoá, 80%;
  - xem lại trên Safari;
  - chưa có âm thanh;
  - đoạn báo động làm ở Phần 3.
- [ ] **Step 8: Commit** `docs(rec): recording setup and measured load`

## Ngoài kế hoạch này

- **Phần 3:**
  - danh sách đoạn báo động (xem, tải, xoá);
  - thông tin khu/sự kiện/ảnh của đoạn báo động;
  - nhắc dọn hằng tuần;
  - nhắc khi đoạn báo động dùng hết 20%.
- **2C:** sự kiện từ camera, ONVIF.
- **Để sau:**
  - âm thanh;
  - giả lập 10 luồng RTSP để đo;
  - tự nhận ổ khi cắm, định dạng ổ trong Cài đặt;
  - sao lưu/khôi phục danh sách camera.
