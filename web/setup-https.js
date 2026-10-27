// Mục cài đặt HTTPS (cổng 8443, tls.go): một chứng chỉ dùng chung cho micro của trợ lý (Safari chỉ mở micro trên HTTPS)
// và địa chỉ trả về khi đăng nhập Spotify. Hub tự tạo CA riêng của nhà + chứng chỉ cho IP đang mở dashboard;
// mỗi máy cài CA một lần theo các bước bên dưới.
(() => {
  const U = window.SETUP_UI;
  let st = null, busy = false;
  async function load() {
    st = {};
    if (U.DEMO) return;
    const r = await U.api('/api/tls');
    if (r.ok) { st = await r.json(); U.soon(); }
  }
  const day = t => new Date(t * 1000).toLocaleDateString('vi-VN');

  function body() {
    if (!st) load();
    const s = st || {}, host = location.hostname, url = `https://${(s.ips || [])[0] || host}:${s.port || 8443}`;
    const ca = `${location.origin}/api/tls/ca.crt`, name = U.esc(s.ca_name || 'iNIX Home CA');
    const here = location.protocol === 'https:' ? '<p class="note">Bạn đang mở dashboard bằng HTTPS.</p>' : '';
    if (s.mode === 'file') return `<div class="kv"><span>Địa chỉ <b><a href="${url}">${url}</a></b></span><span>Hết hạn <b>${day(s.expires)}</b></span></div>${here}
      <p class="note">Đang dùng chứng chỉ riêng (HUBD_TLS_CERT trong .env của hub). Muốn hub tự cấp thì bỏ HUBD_TLS_CERT/KEY.</p>`;
    if (s.mode !== 'ca') return `<p class="note">Safari chỉ cho dùng micro (nút nói) trên trang HTTPS; đăng nhập Spotify cũng cần HTTPS.
        Hub tự tạo chứng chỉ cho địa chỉ ${U.esc(host)}, dùng cổng 8443. Đọc thông báo không cần HTTPS.</p>
      <div class="kv"><button class="pill amber" data-sact="https:on"${busy ? ' disabled' : ''}>${busy ? 'Đang tạo...' : 'Bật HTTPS'}</button></div>`;
    return `<div class="kv"><span>Địa chỉ <b><a href="${url}">${url}</a></b></span><span>Hết hạn <b>${day(s.expires)}</b></span></div>${here}
      <p class="note">Cài chứng chỉ của nhà lên iPad (một lần):</p>
      <ol class="note">
        <li>Mở <b>Safari</b> (không mở từ biểu tượng trên màn hình chính), vào <b>${U.esc(ca)}</b> hoặc chạm <a href="/api/tls/ca.crt">Tải chứng chỉ</a> → <b>Cho phép</b> → <b>Đóng</b>.</li>
        <li>Mở <b>Cài đặt</b> → dòng <b>Đã tải về hồ sơ</b> ở đầu (hoặc Cài đặt chung → Quản lý VPN &amp; thiết bị) → <b>${name}</b> → <b>Cài đặt</b>, nhập mật mã iPad → <b>Cài đặt</b> → <b>Xong</b>.</li>
        <li><b>Cài đặt chung → Giới thiệu → Cài đặt tin cậy chứng nhận</b> → bật <b>${name}</b> → <b>Tiếp tục</b>.</li>
        <li>Mở <a href="${url}">${url}</a> trong Safari, nhập PIN. Chia sẻ → <b>Thêm vào MH chính</b> để thay biểu tượng cũ.</li>
      </ol>
      <p class="note">Mã SHA-256 của chứng chỉ (so với Chi tiết trong hồ sơ trên iPad): ${U.esc(s.ca_sha256 || '')}</p>
      <p class="note">Android: Cài đặt → Bảo mật → Cài đặt chứng chỉ → Chứng chỉ CA. Mac: mở tệp tải về, trong Keychain chọn Luôn tin cậy.
        Chứng chỉ chỉ dùng được cho IP trong nhà, không giả được trang web nào ngoài internet.</p>
      <div class="kv"><button class="pill" data-sact="https:on"${busy ? ' disabled' : ''}>Cấp lại cho ${U.esc(host)}</button></div>`;
  }

  async function act() {
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không tạo chứng chỉ');
    busy = true; U.soon();
    const r = await U.api('/api/tls', {method: 'POST'});
    busy = false;
    if (!r.ok) { U.soon(); return U.toast(await r.text()); }
    st = await r.json(); U.soon();
    U.toast('Đã bật HTTPS. Cài chứng chỉ lên iPad theo các bước trong mục HTTPS.');
  }

  SETUP.push({id: 'https', icon: 'lock', c: 'green', title: 'HTTPS', body, act,
    meta: () => st?.mode === 'ca' || st?.mode === 'file' ? `Cổng ${st.port}, hết hạn ${day(st.expires)}` : 'Chưa bật'});
})();
