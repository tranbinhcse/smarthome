// Mục cài đặt Spotify: Client ID của app tự tạo trên developer.spotify.com, rồi kết nối tài khoản (OAuth PKCE do hubd làm,
// token nằm ở hub, trình duyệt chỉ biết đã kết nối hay chưa). Trạng thái kết nối lấy từ SP (dash.js hỏi /api/spotify/player).
(() => {
  const U = window.SETUP_UI;
  // Spotify chỉ trả về địa chỉ HTTPS: cổng 8443 của hub, chung chứng chỉ với micro trợ lý (scripts/tls-selfsigned.sh).
  const redirect = () => `https://${location.hostname}:8443/api/spotify/callback`;
  const on = () => SP.connected;
  // Phát Spotify qua hub (spotifyhub.go): librespot trên hub là thiết bị "iNIX Smart"; đăng nhập một lần cho hub.
  let HS = null, hsAt = 0, hsPaste = '';
  const hsLoad = async () => { hsAt = Date.now(); const r = await fetch('/api/spotify/hub'); if (r.ok) { HS = await r.json(); U.soon(); } };
  document.addEventListener('input', e => { if (e.target.matches?.('[data-hsurl]')) hsPaste = e.target.value; });
  function hubPart() {
    if (Date.now() - hsAt > (HS?.state === 'login' || HS?.state === 'starting' ? 2000 : 15000)) hsLoad();
    if (!HS) return '';
    const head = `<h4 class="mh">Phát Spotify qua hub</h4>`;
    if (!HS.available) return `${head}<p class="note">Bản hub này chưa có librespot (cần dựng lại image hubd).</p>`;
    if (HS.state === 'login') return `${head}<p class="note">1. Mở trang đăng nhập, đăng nhập tài khoản Spotify Premium.
      2. Trang sẽ chuyển tới một địa chỉ bắt đầu bằng http://127.0.0.1/login và báo không mở được: chép nguyên địa chỉ đó dán vào ô dưới.</p>
      ${HS.url ? `<div class="kv"><a class="pill amber" href="${U.esc(HS.url)}" target="_blank" rel="noopener">Mở trang đăng nhập Spotify</a></div>` : ''}
      <label class="sf"><span>Địa chỉ trang trả về</span><input data-hsurl type="url" autocomplete="off" placeholder="http://127.0.0.1/login?code=..." value="${U.esc(hsPaste)}"></label>
      <div class="kv"><button class="pill amber" data-sact="spotify:hubcode">Gửi cho hub</button><button class="pill" data-sact="spotify:hubout">Huỷ</button></div>`;
    const run = HS.state === 'running';
    return `${head}<p class="note">${HS.logged_in ? `${run ? 'Đang chạy' : 'Đang khởi động'}: chọn thiết bị <b>${U.esc(HS.name)}</b> trong trình phát Spotify để nghe qua các khu.`
      : 'Cho hub thành một thiết bị Spotify, phát qua các khu (cần Premium). Đăng nhập một lần.'}</p>
      ${HS.error && !run ? `<p class="note bad">${U.esc(HS.error)}</p>` : ''}
      <div class="kv">${HS.logged_in ? '<button class="pill" data-sact="spotify:hubout">Đăng xuất hub</button>' : '<button class="pill amber" data-sact="spotify:hublogin">Đăng nhập cho hub</button>'}</div>`;
  }

  function body() {
    return U.sinput('spotify', 'client_id', 'Client ID', {ph: 'Dán Client ID của app Spotify'}) +
      `<p class="note">Tạo app tại developer.spotify.com → Dashboard → Create app, chọn Web API, điền Redirect URI đúng như dưới (có cả cổng 8443):</p>
      <p class="note"><b>${U.esc(redirect())}</b></p>
      <p class="note">Cần Spotify Premium: tài khoản tạo app và tài khoản phát nhạc (thêm vào User Management của app nếu khác nhau). Hub cần bật HTTPS và iPad đã tin chứng chỉ của hub (như nút nói trợ lý).</p>
      ${SP.error ? `<p class="note bad">${U.esc(SP.error)}</p>` : ''}
      ${SP.reconnect ? '<p class="note bad">Trình phát mới cần thêm quyền xem playlist và bài đã thích: bấm Kết nối lại rồi đồng ý trên Spotify.</p>' : ''}
      <div class="kv">${U.ssave('spotify', 'Lưu Client ID')}${on() && !SP.reconnect ? '' : `<button class="pill amber" data-sact="spotify:login">${SP.error || SP.reconnect ? 'Kết nối lại' : 'Kết nối Spotify'}</button>`}${
        on() || SP.error ? '<button class="pill" data-sact="spotify:off">Ngắt kết nối</button>' : ''}</div>${hubPart()}`;
  }

  async function act(what) {
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không kết nối Spotify');
    if (what.startsWith('hub')) {
      const body = what === 'hublogin' ? {do: 'login'} : what === 'hubcode' ? {do: 'code', url: hsPaste.trim()} : {do: 'logout'};
      if (what === 'hublogin') U.toast('Đang mở librespot trên hub...');
      const r = await U.api('/api/spotify/hub', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
      if (!r.ok) return U.toast(await r.text());
      if (what === 'hubcode') { hsPaste = ''; U.toast('Đã gửi, hub đang đăng nhập Spotify'); }
      if (what === 'hubout') U.toast('Đã đăng xuất Spotify trên hub');
      return hsLoad();
    }
    if (what === 'off') {
      const r = await U.api('/api/settings/spotify', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: '{"refresh_token":null}'});
      if (!r.ok) return U.toast(`Chưa ngắt được: ${await r.text()}`);
      SP = {connected: false};
      await U.loadSetting('spotify');
      return U.toast('Đã ngắt kết nối Spotify');
    }
    if (U.DRAFT.spotify) await U.saveSetting('spotify');   // Client ID vừa dán mà chưa bấm Lưu
    const r = await U.api('/api/spotify/login', {method: 'POST'});
    if (!r.ok) return U.toast(await r.text());
    location.href = (await r.json()).url;   // trang đồng ý của Spotify; xong Spotify trả về hub cổng 8443
  }

  SETUP.push({id: 'spotify', ns: 'spotify', icon: 'music', c: 'green', title: 'Spotify', body, act,
    meta: () => on() && SP.reconnect ? 'Cần kết nối lại' : on() ? `Đã kết nối${SP.device ? ', đang mở ở ' + U.esc(SP.device.name) : ''}` : SP.error ? 'Spotify từ chối' : 'Chưa kết nối'});
})();
