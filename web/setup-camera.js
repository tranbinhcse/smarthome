// Mục cài đặt Camera (đầu ghi Hikvision): IP + tài khoản chỉ-xem, "Thử kết nối" (hubd hỏi đầu ghi danh sách kênh),
// gán kênh vào khu. Lưu ở nhóm "nvr" của hubd; mật khẩu không bao giờ quay lại trình duyệt (chỉ nvr_pass_set).
// Gán kênh đổi thì hubd sinh lại thiết bị camera (camera.yaml) và tự khởi động lại.
(() => {
  const U = window.SETUP_UI, NS = 'nvr';
  let found = null, busy = false, restart = null;   // found: {channels} hoặc {error} của lần thử gần nhất; restart: cfg lúc lưu
  const saved = () => U.SAVED[NS] || {};
  const on = () => (U.DRAFT[NS]?.enabled ?? saved().enabled) !== false;
  // danh sách camera hubd sẽ sinh (camDevices trong camera.go): đổi thì hubd khởi động lại
  const gen = s => s.enabled === false ? '[]' : JSON.stringify(Object.keys(s).filter(k => /^ch\d+_zone$/.test(k) && s[k]).sort()
    .map(k => [k, s[k], String(s[k.replace('_zone', '_name')] || '').trim()]));

  // kênh hiện ra: kênh đầu ghi vừa trả về + kênh đã gán trước đó (để sửa mà không cần thử lại)
  function channels() {
    const m = new Map((found?.channels || []).map(c => [c.id, c.name]));
    for (const [k, v] of Object.entries(saved())) {
      const n = Number(/^ch(\d+)_zone$/.exec(k)?.[1]);
      if (n && v && !m.has(n)) m.set(n, '');
    }
    return [...m].sort((a, b) => a[0] - b[0]);
  }

  function body() {
    const zones = [['', '— Không dùng —'], ...cfg.zones.filter(z => z.type !== 'panel').map(z => [z.id, `${floorName(z.floor)} · ${z.name}`])];
    const chs = channels();
    return `<div class="setrow"><span>Bật camera</span><button class="sw amber${on() ? ' on' : ''}" data-sact="camera:toggle" aria-pressed="${on()}" aria-label="Bật/tắt camera"></button></div>` +
      U.sinput(NS, 'nvr_host', 'IP đầu ghi', {ph: '192.168.1.64'}) +
      U.sinput(NS, 'nvr_user', 'Tài khoản chỉ xem', {ph: 'Tài khoản riêng trên đầu ghi'}) +
      U.sinput(NS, 'nvr_pass', 'Mật khẩu', {secret: true, ph: 'Chỉ gồm chữ và số'}) +
      `<p class="note">Tạo trên đầu ghi một tài khoản chỉ có quyền xem. Để trống IP thì hub dùng NVR_HOST/NVR_USER/NVR_PASS trong .env (cách cũ).</p>
      <div class="kv"><button class="pill" data-sact="camera:test"${busy ? ' disabled' : ''}>${busy ? 'Đang thử...' : 'Thử kết nối'}</button><button class="pill amber" data-sact="camera:save">Lưu</button></div>` +
      (found?.error ? `<p class="note bad">${U.esc(found.error)}</p>` : found ? `<p class="note">Tìm thấy ${found.channels.length} kênh${found.channels.length ? ': ' + U.esc(found.channels.map(c => c.name || `Kênh ${c.id}`).join(', ')) : ''}.</p>` : '') +
      (chs.length ? `<p class="note">Gán kênh vào khu (camera mới hiện trên trang chủ và trang khu đó):</p>` + chs.map(([n, name]) =>
        `<div class="setrow"><span>${U.icon('camera')}Kênh ${n}${name ? ' · ' + U.esc(name) : ''}</span></div>
        <div style="display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:8px">${U.sselect(NS, `ch${n}_zone`, 'Khu', zones)}${U.sinput(NS, `ch${n}_name`, 'Tên hiển thị', {ph: name || `Camera kênh ${n}`})}</div>`).join('') : '') +
      (restart === cfg ? '<p class="note busy">Hub đang khởi động lại để cập nhật camera, trang tự cập nhật sau vài giây.</p>' : '');   // dash.js nạp cfg mới khi nối lại
  }

  async function act(what) {
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không kết nối đầu ghi thật');
    if (what === 'toggle') {
      const r = await U.api(`/api/settings/${NS}`, {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({enabled: !on()})});
      if (!r.ok) return U.toast(`Chưa lưu được: ${await r.text()}`);
      if (U.DRAFT[NS]) delete U.DRAFT[NS].enabled;
      const before = gen(saved());
      await U.loadSetting(NS);
      if (gen(saved()) !== before) { restart = cfg; U.soon(); }
      return U.toast(on() ? 'Đã bật camera' : 'Đã tắt camera');
    }
    const host = String(U.sval(NS, 'nvr_host')).trim();
    if (host && !/^[A-Za-z0-9.-]+(:\d{1,5})?$/.test(host)) return U.toast('IP đầu ghi chỉ ghi IP (vd 192.168.1.64), không có http://');
    // hubd bỏ mật khẩu đã lưu khi đổi IP (không gửi mật khẩu cũ tới máy khác): nhắc nhập lại thay vì lưu mất mật khẩu
    if (what === 'save' && host !== String(saved().nvr_host || '').trim() && saved().nvr_pass_set && !U.sval(NS, 'nvr_pass')) return U.toast('Đổi IP đầu ghi thì nhập lại mật khẩu');
    if (what === 'test') {
      busy = true; U.soon();
      const r = await U.api('/api/setup/nvr/test', {method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({nvr_host: host, nvr_user: String(U.sval(NS, 'nvr_user')).trim(), nvr_pass: String(U.sval(NS, 'nvr_pass'))})});
      found = r.ok ? await r.json() : {error: (await r.text()).trim() || `Lỗi HTTP ${r.status}`};
      busy = false; U.soon();
      return;
    }
    if (what === 'save') {
      // kênh vừa gán khu mà chưa đặt tên: lấy tên trên đầu ghi
      for (const [n, name] of channels()) {
        if (name && U.sval(NS, `ch${n}_zone`) && !U.sval(NS, `ch${n}_name`)) (U.DRAFT[NS] ||= {})[`ch${n}_name`] = name;
      }
      const before = gen(saved());
      await U.saveSetting(NS);
      if (gen(saved()) !== before) { restart = cfg; U.soon(); }
    }
  }

  SETUP.push({id: 'camera', ns: NS, icon: 'camera', c: 'blue', title: 'Camera (đầu ghi Hikvision)', body, act,
    meta: () => on() ? `${cfg.devices.filter(d => d.kind === 'camera').length} camera đang hiển thị` : 'Đang tắt'});
})();
