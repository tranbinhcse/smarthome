// Mục cài đặt Camera (đầu ghi Hikvision): IP + tài khoản chỉ-xem, "Thử kết nối" (hubd hỏi đầu ghi danh sách kênh),
// gán kênh vào khu. Lưu ở nhóm "nvr" của hubd; mật khẩu không bao giờ quay lại trình duyệt (chỉ nvr_pass_set).
// Gán kênh đổi thì hubd sinh lại thiết bị camera (camera.yaml) và tự khởi động lại.
// Phần "Camera nối thẳng" (camstore.go, camscan.go): tìm trong mạng, thêm / sửa / xoá camera Hikvision hoặc RTSP.
// Mật khẩu camera KHÔNG đi qua data-sfield/DRAFT (nút "Lưu" của nhóm camera sẽ ghi nó xuống cấu hình): ô thường, đọc lúc bấm, gửi một lần rồi xoá.
(() => {
  const U = window.SETUP_UI, NS = 'nvr';
  let found = null, busy = false, restart = null;   // found: {channels} hoặc {error} của lần thử gần nhất; restart: cfg lúc lưu
  const saved = () => U.SAVED[NS] || {};
  const e = U.esc;
  // ----- camera nối thẳng -----
  const DEMO_CAMS = [{id: 'cam_san', name: 'Camera sân', zone: '', kind: 'rtsp', host: '192.168.1.60', rtsp_port: 554, user: 'xem', pass_set: true}];
  const TPL0 = [{brand: 'Dahua / KBVision / Imou', main_path: '/cam/realmonitor?channel=1&subtype=0', sub_path: '/cam/realmonitor?channel=1&subtype=1', rtsp_port: 554}];
  let PW = '', CAMS = null, TPL = [], SCAN = null, scanTimer = 0, form = null, F = {}, res = {}, tested = {}, camRestart = null;
  const camKey = l => JSON.stringify((l || []).map(c => [c.id, c.name, c.zone]).sort());
  const kindName = k => k === 'hik_cam' ? 'Hikvision' : 'RTSP';
  const json = (m, b) => ({method: m, headers: {'Content-Type': 'application/json'}, body: b === undefined ? undefined : JSON.stringify(b)});
  async function loadCams() {
    if (U.DEMO) { CAMS = DEMO_CAMS; TPL = TPL0; return U.soon(); }
    const r = await U.api('/api/cameras');
    if (!r.ok) return;
    const j = await r.json();
    CAMS = j.cameras || []; TPL = j.templates || []; U.soon();
  }
  async function poll() {
    clearTimeout(scanTimer); scanTimer = 0;
    const r = await U.api('/api/cameras/scan');
    if (!r.ok) { SCAN = null; return U.soon(); }
    const j = await r.json(), was = SCAN;
    SCAN = j;
    if (j.running) scanTimer = setTimeout(poll, 1500);   // dừng khi quét xong
    // chỉ vẽ lại khi có thay đổi: vẽ lại sẽ xoá ô mật khẩu đang gõ dở
    if (!was || was.running !== j.running || (was.found || []).length !== (j.found || []).length || was.error !== j.error) U.soon();
  }
  const zoneName = id => { const z = cfg.zones.find(z => z.id === id); return z ? z.name : ''; };
  const val = k => F[k] ?? '';
  const fin = (k, label, ph = '') => `<label class="sf"><span>${label}</span><input data-cf="${k}" value="${e(val(k))}" placeholder="${e(ph)}" autocomplete="off"></label>`;
  function tplOf(i) { return TPL[Number(i)]; }

  function camForm() {
    const zones = [['', '— Chọn khu —'], ...cfg.zones.filter(z => z.type !== 'panel').map(z => [z.id, `${floorName(z.floor)} · ${z.name}`])];
    const rtsp = F.kind === 'rtsp', other = F.brand === 'other';
    const kinds = [['hik_cam', 'Hikvision'], ['rtsp', 'RTSP (hãng khác)']];
    const brands = [...TPL.map((t, i) => [String(i), t.brand]), ['other', 'Khác']];
    const sel = (k, opts) => `<select data-cf="${k}">${opts.map(([v, l]) => `<option value="${e(v)}"${String(val(k)) === String(v) ? ' selected' : ''}>${e(l)}</option>`).join('')}</select>`;
    const pw = (form === 'edit' ? CAMS.find(c => c.id === F.id)?.pass_set : false);
    return `<div class="list"><h4 class="mh">${form === 'edit' ? 'Sửa camera' : 'Thêm camera'}</h4>
      <label class="sf"><span>Kiểu</span>${sel('kind', kinds)}</label>
      ${rtsp ? `<label class="sf"><span>Hãng</span>${sel('brand', brands)}</label>` : ''}
      ${fin('host', 'IP camera', '192.168.1.60')}
      ${fin('user', 'Tài khoản')}
      <label class="sf"><span>Mật khẩu</span><input id="cam-pass" type="password" autocomplete="off" placeholder="${pw ? 'Đã lưu, để trống nếu giữ nguyên' : ''}" value="${e(PW)}"></label>
      ${rtsp && other ? fin('rtsp_port', 'Cổng RTSP', '554') + fin('main_path', 'Đường dẫn luồng chính', '/stream1') + fin('sub_path', 'Đường dẫn luồng phụ', '/stream2') : ''}
      ${fin('name', 'Tên camera', 'Camera sân')}
      <label class="sf"><span>Khu</span>${sel('zone', zones)}</label>
      <div class="kv"><button class="pill" data-sact="camera:ctest"${F.busy ? ' disabled' : ''}>${F.busy ? 'Đang thử...' : 'Thử kết nối'}</button><button class="pill amber" data-sact="camera:csave"${F.busy ? ' disabled' : ''}>Lưu</button><button class="pill" data-sact="camera:cclose">Đóng</button></div>
      ${F.result ? `<p class="note${F.result.ok ? '' : ' bad'}">${e(F.result.text)}</p>` : ''}
      <p class="note">Mật khẩu chỉ gửi một lần cho hub, ô sẽ tự xoá sau mỗi lần bấm. ${form === 'edit' ? 'Để trống mật khẩu là giữ mật khẩu đã lưu.' : 'Bấm Lưu thì nhập lại mật khẩu.'}</p></div>`;
  }

  function direct() {
    if (CAMS === null) { if (U.DEMO) { CAMS = DEMO_CAMS; TPL = TPL0; } else { CAMS = []; loadCams(); U.loadSetting('camera'); } }   // dải mạng nằm ở nhóm camera
    const found = (SCAN?.found || []).filter(h => !(CAMS || []).some(c => c.host === h.host));
    const list = (CAMS || []).map(c => `<div class="setrow"><span>${U.icon('camera')}${e(c.name)} <small>${e(zoneName(c.zone))} · ${kindName(c.kind)}</small>
        <small class="${res[c.id]?.ok === false ? 'bad' : ''}">${e(res[c.id]?.text || 'Chưa thử kết nối')}</small></span>
      <button class="pill" data-sact="camera:rtest:${e(c.id)}">Thử</button><button class="pill" data-sact="camera:edit:${e(c.id)}">Sửa</button><button class="pill" data-sact="camera:del:${e(c.id)}">Xoá</button></div>`).join('');
    return `<h4 class="mh">Camera nối thẳng</h4>
      <p class="note">Camera nối thẳng vào mạng nhà, không qua đầu ghi.</p>
      ${U.sinput('camera', 'lan_cidr', 'Dải mạng của nhà', {ph: '192.168.1.0/24'})}
      <div class="kv">${U.ssave('camera')}<button class="pill" data-sact="camera:scan"${SCAN?.running ? ' disabled' : ''}>${SCAN?.running ? 'Đang tìm...' : 'Tìm camera'}</button><button class="pill" data-sact="camera:add">Thêm tay</button></div>
      ${SCAN?.error ? `<p class="note bad">${e(SCAN.error)}</p>` : ''}
      ${found.length ? `<h4 class="mh">Tìm thấy camera mới</h4><div class="list">${found.map((h, i) => `<div class="setrow"><span>${e(h.host)} <small>${h.kind === 'hik_cam' ? 'Hikvision' : 'RTSP'}${h.model ? ' · ' + e(h.model) : ''}</small></span><button class="pill" data-sact="camera:pick:${i}">Thêm</button></div>`).join('')}</div>` : SCAN && !SCAN.running && !SCAN.error ? '<p class="note">Không tìm thấy camera mới.</p>' : ''}
      ${form ? camForm() : ''}
      <h4 class="mh">Camera đã thêm</h4>
      ${list ? `<div class="list">${list}</div>` : '<p class="note">Chưa có camera nối thẳng.</p>'}
      ${camRestart === cfg ? '<p class="note busy">Hub đang khởi động lại để cập nhật camera, trang tự cập nhật sau vài giây.</p>' : ''}
      <h4 class="mh">Đầu ghi (cách cũ)</h4>`;
  }

  // gói ô nhập thành body gửi hub; mật khẩu đọc lúc bấm rồi xoá khỏi ô
  function payload(pass) {
    const tp = F.kind === 'rtsp' && F.brand !== 'other' ? tplOf(F.brand) : null;
    const o = {kind: F.kind, host: String(F.host || '').trim(), user: String(F.user || '').trim(), pass,
      http_port: Number(F.http_port) || 0, rtsp_port: Number(F.rtsp_port) || 0};
    if (F.id) o.id = F.id;
    if (F.kind === 'rtsp') { o.main_path = tp ? tp.main_path : String(F.main_path || '').trim(); o.sub_path = tp ? tp.sub_path : String(F.sub_path || '').trim(); }
    return o;
  }
  // mật khẩu chỉ rời module khi gửi: lấy ra rồi xoá ở biến và ô nhập
  const takePass = () => { const p = PW; PW = ''; const b = document.getElementById('cam-pass'); if (b) b.value = ''; return p; };
  const testText = j => j.ok ? `Kết nối được${j.model ? ': ' + j.model : ''}${j.firmware ? ' (' + j.firmware + ')' : ''}` : (j.message || 'Không kết nối được');
  async function runTest(o, into) {
    const r = await U.api('/api/cameras/test', json('POST', o));
    if (r.status === 429) return {ok: false, text: (await r.text()).trim()};
    if (!r.ok) return {ok: false, text: (await r.text()).trim() || `Lỗi HTTP ${r.status}`};
    const j = await r.json();
    return {ok: !!j.ok, text: testText(j)};
  }
  async function camAct(verb, arg) {
    if (verb === 'add') { form = 'new'; PW = ''; F = {kind: 'hik_cam', brand: '0', rtsp_port: TPL[0]?.rtsp_port || ''}; U.soon(); return; }
    if (verb === 'pick') {
      const h = (SCAN?.found || []).filter(h => !(CAMS || []).some(c => c.host === h.host))[Number(arg)];
      if (!h) return;
      form = 'new'; PW = ''; F = {kind: h.kind === 'hik_cam' ? 'hik_cam' : 'rtsp', brand: '0', rtsp_port: TPL[0]?.rtsp_port || '', host: h.host, name: h.model || ''}; return U.soon();
    }
    if (verb === 'edit') {
      const c = CAMS.find(c => c.id === arg);
      if (!c) return;
      const i = TPL.findIndex(t => t.main_path === c.main_path && t.sub_path === c.sub_path);
      form = 'edit'; PW = ''; F = {id: c.id, http_port: c.http_port || '', kind: c.kind, brand: c.kind === 'rtsp' && i < 0 ? 'other' : String(Math.max(i, 0)), host: c.host, user: c.user, name: c.name, zone: c.zone,
        rtsp_port: c.rtsp_port || '', main_path: c.main_path || '', sub_path: c.sub_path || ''};
      return U.soon();
    }
    if (verb === 'cclose') { form = null; F = {}; PW = ''; return U.soon(); }
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không kết nối camera thật');
    if (verb === 'scan') {
      const r = await U.api('/api/cameras/scan', {method: 'POST'});
      if (r.status === 409) { SCAN = {running: true, found: []}; return poll(); }   // đang quét sẵn: theo dõi tiếp
      if (!r.ok) return U.toast((await r.text()).trim() || `Lỗi HTTP ${r.status}`);
      SCAN = {running: true, found: [], error: ''}; U.soon(); return poll();
    }
    if (verb === 'rtest') {
      const c = CAMS.find(c => c.id === arg);
      if (!c) return;
      res[c.id] = {ok: null, text: 'Đang thử...'}; U.soon();
      res[c.id] = await runTest({id: c.id, kind: c.kind, host: c.host, http_port: c.http_port || 0, rtsp_port: c.rtsp_port || 0, user: c.user, pass: ''});
      return U.soon();
    }
    if (verb === 'ctest') {
      if (F.busy) return;
      const o = payload(takePass());
      F.busy = true; F.result = null; U.soon();
      F.result = await runTest(o); F.busy = false; return U.soon();
    }
    if (verb === 'csave') {
      if (F.busy) return;
      if (!F.name?.trim()) return U.toast('Nhập tên camera');   // kiểm tra trước: thiếu tên thì không xoá mật khẩu đang gõ
      F.busy = true; U.soon();
      const o = payload(takePass());
      o.name = F.name.trim(); o.zone = F.zone || '';
      const before = camKey(CAMS);
      const r = await U.api('/api/cameras', json('PUT', o));
      if (!r.ok) { F.busy = false; U.soon(); return U.toast(`Chưa lưu được: ${(await r.text()).trim()}`); }
      form = null; F = {};
      await loadCams();
      if (camKey(CAMS) !== before) { camRestart = cfg; }
      U.toast('Đã lưu camera'); return U.soon();
    }
    if (verb === 'del') {
      const c = CAMS.find(c => c.id === arg);
      if (!c || !(await ask({title: `Xoá ${c.name}?`, text: 'Camera sẽ biến mất khỏi nhà. Có thể thêm lại sau.', ok: 'Xoá', danger: true}))) return;
      const r = await U.api(`/api/cameras/${encodeURIComponent(c.id)}`, {method: 'DELETE'});
      if (!r.ok && r.status !== 404) return U.toast(`Chưa xoá được: ${(await r.text()).trim()}`);
      delete res[c.id];
      await loadCams(); camRestart = cfg; U.toast('Đã xoá camera'); return U.soon();
    }
  }
  // ô thường của form: giữ trong F để vẽ lại không mất; đổi kiểu hoặc hãng thì vẽ lại
  document.addEventListener('input', ev => {
    const f = ev.target.closest?.('[data-cf]');
    if (f) F[f.dataset.cf] = f.value;
    else if (ev.target.id === 'cam-pass') PW = ev.target.value;   // vẽ lại (SSE) thì ô được điền lại từ đây; không vào bản nháp chung
  });
  document.addEventListener('change', ev => {
    const f = ev.target.closest?.('[data-cf]');
    if (!f) return;
    F[f.dataset.cf] = f.value;
    if (f.dataset.cf === 'brand' && tplOf(f.value)) F.rtsp_port = tplOf(f.value).rtsp_port;   // chọn mẫu mới thì lấy cổng RTSP của mẫu
    if (f.dataset.cf === 'kind' || f.dataset.cf === 'brand') { F.result = null; U.soon(); }
  });

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
    return direct() + `<div class="setrow"><span>Bật camera</span><button class="sw amber${on() ? ' on' : ''}" data-sact="camera:toggle" aria-pressed="${on()}" aria-label="Bật/tắt camera"></button></div>` +
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
    if (!['toggle', 'test', 'save'].includes(what)) { const [v, ...a] = what.split(':'); return camAct(v, a.join(':')); }
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

  SETUP.push({id: 'camera', ns: NS, icon: 'camera', c: 'blue', title: 'Camera', body, act,
    meta: () => on() ? `${cfg.devices.filter(d => d.kind === 'camera').length} camera đang hiển thị` : 'Đang tắt'});
})();
