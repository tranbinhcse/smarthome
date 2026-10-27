// Mục cài đặt Spotify: Client ID của app tự tạo trên developer.spotify.com, rồi kết nối tài khoản (OAuth PKCE do hubd làm,
// token nằm ở hub, trình duyệt chỉ biết đã kết nối hay chưa). Trạng thái kết nối lấy từ SP (dash.js hỏi /api/spotify/player).
(() => {
  const U = window.SETUP_UI;
  // Spotify chỉ trả về địa chỉ HTTPS: cổng 8443 của hub, chung chứng chỉ với micro trợ lý (scripts/tls-selfsigned.sh).
  const redirect = () => `https://${location.hostname}:8443/api/spotify/callback`;
  const on = () => SP.connected;

  function body() {
    return U.sinput('spotify', 'client_id', 'Client ID', {ph: 'Dán Client ID của app Spotify'}) +
      `<p class="note">Tạo app tại developer.spotify.com → Dashboard → Create app, chọn Web API, điền Redirect URI đúng như dưới (có cả cổng 8443):</p>
      <p class="note"><b>${U.esc(redirect())}</b></p>
      <p class="note">Cần Spotify Premium: tài khoản tạo app và tài khoản phát nhạc (thêm vào User Management của app nếu khác nhau). Hub cần bật HTTPS và iPad đã tin chứng chỉ của hub (như nút nói trợ lý).</p>
      <div class="kv">${U.ssave('spotify', 'Lưu Client ID')}${on() ? '<button class="pill" data-sact="spotify:off">Ngắt kết nối</button>'
        : '<button class="pill amber" data-sact="spotify:login">Kết nối Spotify</button>'}</div>`;
  }

  async function act(what) {
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không kết nối Spotify');
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
    meta: () => on() ? `Đã kết nối${SP.device ? ', đang mở ở ' + U.esc(SP.device.name) : ''}` : 'Chưa kết nối'});
})();
