// Mục cài đặt Tuya (Smart Life): nhập User Code, lấy mã QR quét bằng app; quét xong hub tự cập nhật danh sách
// thiết bị (tuya-bridge ghi devices.json + cấu hình sinh, hubd tự nạp lại). Tiến độ: state "setup:tuya" do bridge gửi.
(() => {
  const U = window.SETUP_UI;
  const st = () => { try { return JSON.parse(state['setup:tuya'] || '{}'); } catch (_) { return {}; } };
  const busy = s => ['qr', 'fetching', 'scanning'].includes(s.state);
  const ago = t => { const m = Math.round((Date.now() / 1000 - t) / 60); return m < 1 ? 'vừa xong' : m < 60 ? `${m} phút trước` : `${Math.round(m / 60)} giờ trước`; };
  const svgSrc = svg => 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)));   // ảnh, không chèn SVG làm HTML

  // Sửa từng thiết bị (chạm vào dòng): tên, loại, tầng, phòng; CB thì thêm khu được cấp điện, thiết bị nhiều nút thì
  // thêm tên từng nút. Lưu vào nhóm cấu hình "devices" theo id từng kênh (devoverride.go), hub tự nạp lại vài giây sau.
  const KINDS = [['light', 'Đèn'], ['socket', 'Ổ cắm'], ['switch', 'Công tắc'], ['breaker', 'CB'], ['fan', 'Quạt'], ['ac', 'Điều hoà'],
    ['tv', 'TV'], ['speaker', 'Loa'], ['pump', 'Bơm nước'], ['water', 'Tưới cây']];
  const keyOf = d => (d.command_topic || '').split('/')[1] || d.id;   // tuya/<id thiết bị>/cmd: các kênh chung một khoá
  function units() {
    const m = new Map();
    for (const d of cfg.devices.filter(x => x.id.startsWith('tuya_'))) m.set(keyOf(d), [...(m.get(keyOf(d)) || []), d]);
    return m;
  }
  const multi = chs => chs.length > 1 || !!chs[0].group;
  const cur = chs => ({name: multi(chs) ? chs[0].group || chs[0].name : chs[0].name, kind: chs[0].kind || '', zone: chs[0].zone || '',
    floor: zone(chs[0].zone)?.floor ?? floors()[0], area: cfg.entities.find(e => e.device === chs[0].id)?.zone || ''});
  let open = '', draft = {};
  const val = (chs, f) => draft[f] ?? cur(chs)[f];

  function editor(chs) {
    const sel = (f, opts, label) => `<label class="sf"><span>${label}</span><select data-tf="${f}">${opts.map(([o, l]) =>
      `<option value="${U.esc(o)}"${String(val(chs, f)) === String(o) ? ' selected' : ''}>${U.esc(l)}</option>`).join('')}</select></label>`;
    const k = cur(chs).kind, kinds = KINDS.some(([o]) => o === k) || !k ? KINDS : [[k, k], ...KINDS];
    const zs = cfg.zones.filter(z => z.floor === Number(val(chs, 'floor')));
    return `<div class="tedit">
      <label class="sf"><span>${multi(chs) ? 'Tên thiết bị' : 'Tên'}</span><input data-tf="name" value="${U.esc(val(chs, 'name'))}" autocomplete="off"></label>
      ${sel('kind', kinds, 'Loại')}${sel('floor', floors().map(f => [f, floorName(f)]), 'Tầng')}
      ${sel('zone', [['', 'Chọn phòng...'], ...zs.map(z => [z.id, z.name])], 'Phòng / khu vực')}
      ${val(chs, 'kind') === 'breaker' && cfg.entities.some(e => e.device === chs[0].id)
        ? sel('area', [['', 'Giữ ở tủ điện'], ...cfg.zones.filter(z => z.type !== 'panel').map(z => [z.id, `${floorName(z.floor)} · ${z.name}`])], 'Khu được cấp điện') : ''}
      ${multi(chs) ? chs.map((c, i) => `<label class="sf"><span>Nút ${i + 1}</span><input data-tf="ch:${U.esc(c.id)}" value="${U.esc(draft['ch:' + c.id] ?? c.name)}" autocomplete="off"></label>`).join('') : ''}
      <div class="kv"><button class="pill" data-sact="tuya:close">Huỷ</button><button class="pill amber" data-sact="tuya:save">Lưu</button></div></div>`;
  }
  function devList(s) {
    const scan = Object.fromEntries((s.devices || []).map(d => [d.id, d]));
    return `<div class="list">${[...units()].map(([k, chs]) => {
      const d = chs[0], meta = [KINDS.find(([o]) => o === d.kind)?.[1] || d.kind, zone(d.zone)?.name || 'chưa gán phòng',
        multi(chs) ? `${chs.length} ${d.kind === 'socket' ? 'lỗ' : 'nút'}` : '', scan[k]?.ip || 'chưa có IP'].filter(Boolean).join(' · ');
      return `<button class="setrow${k === open ? ' sel' : ''}" data-sact="tuya:edit:${U.esc(k)}" aria-expanded="${k === open}">
        <span>${U.esc(cur(chs).name)}${scan[k]?.new ? ' <em class="tag good">Mới</em>' : ''}</span><small>${U.esc(meta)}</small></button>${k === open ? editor(chs) : ''}`;
    }).join('')}</div>`;
  }
  function onField(e) {
    const f = e.target.closest?.('[data-tf]');
    if (!f) return;
    draft[f.dataset.tf] = f.value;
    if (f.dataset.tf === 'floor') draft.zone = '';   // đổi tầng thì chọn lại phòng của tầng đó
    if (e.type === 'change' && f.tagName === 'SELECT') { f.blur(); U.soon(); }
  }
  document.addEventListener('input', onField);
  document.addEventListener('change', onField);
  async function save() {
    const chs = units().get(open);
    if (!chs) return;
    const name = String(val(chs, 'name')).trim(), z = val(chs, 'zone'), area = val(chs, 'area');
    if (!name) return U.toast('Nhập tên thiết bị');
    if (!z) return U.toast('Chọn phòng cho thiết bị');
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không lưu');
    const patch = {};
    for (const c of chs) patch[c.id] = {zone: z, kind: val(chs, 'kind'), ...(area ? {area} : {}),
      ...(multi(chs) ? {group: name, name: String(draft['ch:' + c.id] ?? c.name).trim() || c.name} : {name})};
    const r = await U.api('/api/settings/devices', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(patch)});
    if (!r.ok) return U.toast(`Chưa lưu được: ${await r.text()}`);
    open = ''; draft = {};
    U.soon();
    U.toast('Đã lưu, hub đang nạp lại...');
  }

  function body() {
    const s = st();
    let out = U.sinput('tuya', 'user_code', 'User Code', {ph: 'Dán User Code từ app Smart Life'}) +
      `<p class="note">Lấy trong app Smart Life: Tôi → biểu tượng ⚙️ Cài đặt → Tài khoản và bảo mật → User Code.</p>
      <div class="kv">${busy(s) ? '<button class="pill" data-sact="tuya:cancel">Huỷ</button>'
        : '<button class="pill amber" data-sact="tuya:start">Lấy mã QR</button>'}</div>`;
    if (s.state === 'qr' && s.qr_svg) out += `<div class="qrbox"><img class="qr" src="${svgSrc(s.qr_svg)}" alt="Mã QR đăng nhập Smart Life"><p class="note">${U.esc(s.message || '')}</p></div>`;
    else if (busy(s)) out += `<p class="note busy">${U.esc(s.message || 'Đang xử lý...')}</p>`;
    else if (s.state === 'error') out += `<p class="note bad">${U.esc(s.message || 'Có lỗi, thử lại.')}</p>`;
    else if (s.state === 'done') out += `<p class="note">${U.esc(s.message || '')} ${s.at ? '(' + ago(s.at) + ')' : ''}</p>`;
    if (units().size) out += `<p class="note">Chạm vào thiết bị để đổi tên, loại, tầng, phòng.</p>${devList(s)}`;
    return out;
  }

  async function act(what) {
    if (what.startsWith('edit:')) { const k = what.slice(5); open = open === k ? '' : k; draft = {}; return U.soon(); }
    if (what === 'close') { open = ''; draft = {}; return U.soon(); }
    if (what === 'save') return save();
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không quét QR thật');
    const code = String(U.sval('tuya', 'user_code') || '').trim();
    if (what === 'start' && !code) return U.toast('Nhập User Code trước');
    const r = await U.api('/api/setup/tuya', {method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(what === 'start' ? {action: 'start', user_code: code} : {action: 'cancel'})});
    if (!r.ok) return U.toast(`Chưa gửi được: ${await r.text()}`);
    U.toast(what === 'start' ? 'Đang tạo mã QR...' : 'Đã huỷ');
  }

  const n = () => new Set(cfg.devices.filter(d => d.id.startsWith('tuya_')).map(d => d.group || d.id)).size;
  SETUP.push({id: 'tuya', ns: 'tuya', icon: 'socket', c: 'amber', title: 'Tuya (Smart Life)', body, act,
    meta: () => { const s = st(); return busy(s) ? 'Đang quét QR' : `${n()} thiết bị đang kết nối`; }});
})();
