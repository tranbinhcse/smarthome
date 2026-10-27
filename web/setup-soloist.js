// Mục cài đặt Spotify trên hub (spotifyhub.go): hub chạy Soloist, trình nhận Spotify Connect chính thức, thành thiết bị
// "iNIX Smart"; chọn thiết bị này trong app Spotify thì nhạc ra mọi khu. Khoá API Soloist là bí mật: chỉ lưu ở hub, trình duyệt
// chỉ biết đã lưu hay chưa (api_key_set). Tình trạng lấy từ /api/spotify/hub.
(() => {
  const U = window.SETUP_UI;
  let S = null, at = 0;
  const load = async () => { at = Date.now(); const r = await fetch('/api/spotify/hub'); if (r.ok) { S = await r.json(); U.soon(); } };
  const TXT = {nokey: 'Chưa có khoá API', downloading: 'Đang tải Soloist', starting: 'Đang khởi động', expired: 'Bản Soloist hết hạn', error: 'Lỗi'};
  const label = () => !S ? '' : S.state === 'running' ? (S.logged_in ? 'Đang chạy' : 'Chờ ghép đôi') : TXT[S.state] || S.state;

  function body() {
    // hỏi lại nhanh khi đang tải/khởi động hoặc vừa lưu, xoá khoá (hub chưa kịp đổi tình trạng)
    const busy = ['downloading', 'starting'].includes(S?.state) || !!U.SAVED.soloist?.api_key_set !== !!S?.key_set;
    if (!U.DEMO && Date.now() - at > (busy ? 1500 : 10000)) load();
    const name = U.esc(S?.name || 'iNIX Smart');
    return `<p class="note">Cho hub thành loa Spotify Connect <b>${name}</b>: chọn thiết bị này trong app Spotify thì nhạc ra mọi khu. Cần Spotify Premium.</p>
      ${S ? `<p class="note">Tình trạng: <b>${U.esc(label())}</b>${S.version ? ` · Soloist ${U.esc(S.version)}${S.days ? `, bản dựng còn ${S.days} ngày` : ''}` : ''}</p>` : ''}
      ${S?.error ? `<p class="note bad">${U.esc(S.error)}</p>` : ''}
      ${S?.state === 'running' && !S.logged_in ? `<p class="note">Ghép đôi một lần: điện thoại nối cùng Wi-Fi với hub, mở app Spotify, chạm biểu tượng thiết bị phát, chọn <b>${name}</b> rồi phát một bài. Hub nhớ phiên này cho các lần sau.</p>` : ''}
      ${S?.logged_in ? `<p class="note">Đã ghép đôi. Trong app Spotify chọn <b>${name}</b> để nghe qua các khu.</p>` : ''}
      <h4 class="mh">Khoá API Soloist</h4>
      <p class="note">1. Mở developer.spotify.com, đăng nhập bằng tài khoản Spotify Premium của bạn.<br>
        2. Vào Dashboard, mở mục Spotify Soloist API Key; đọc điều khoản và đồng ý nếu được hỏi.<br>
        3. Bấm tạo khoá (Generate), chép khoá, dán vào ô dưới rồi bấm Lưu khoá.<br>
        Khoá gắn với tài khoản của bạn: không gửi cho người khác. Hub chỉ lưu khoá ở hub, không hiện lại.</p>
      ${U.sinput('soloist', 'api_key', 'Khoá API', {secret: true, ph: 'Dán khoá API Soloist'})}
      <div class="kv">${U.ssave('soloist', 'Lưu khoá')}${S?.key_set ? '<button class="pill" data-sact="soloist:update">Cập nhật Soloist</button><button class="pill" data-sact="soloist:remove">Xoá khoá</button>' : ''}</div>
      <p class="note">Hub tự tải Soloist từ Spotify (không đóng gói sẵn). Mỗi bản dùng được 90 ngày; hết hạn hub tự tải bản mới. Cập nhật Soloist: kiểm bản mới ngay và chạy lại.</p>`;
  }

  async function act(what) {
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không chạy Soloist');
    const r = what === 'update'
      ? await U.api('/api/spotify/hub', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{"do":"update"}'})
      : await U.api('/api/settings/soloist', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: '{"api_key":null}'});
    if (!r.ok) return U.toast(await r.text());
    if (what === 'remove') await U.loadSetting('soloist');
    U.toast(what === 'update' ? 'Hub đang kiểm bản Soloist mới và chạy lại' : 'Đã xoá khoá; Spotify trên hub đã dừng');
    setTimeout(load, 500);
  }

  SETUP.push({id: 'soloist', ns: 'soloist', icon: 'music', c: 'green', title: 'Spotify trên hub', body, act, meta: label});
})();
