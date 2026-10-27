/* ---------- Trang Cài đặt (#/caidat/<mục>/<dòng>): menu mục bên trái, danh sách ở giữa, chi tiết bên phải ----------
   Nạp sau dash.js, trước setup-*.js. Mục tích hợp (Tuya, AI, HTTPS...) vẫn đăng ký qua SETUP và hiện ở mục phù hợp.
   Phòng, thiết bị (tên, loại, tầng, phòng) lưu vào nhóm cấu hình "zones"/"devices": hub áp lúc khởi động nên tự nạp lại. */
Object.assign(ICON, {
  search: '<circle cx="11" cy="11" r="6.5"/><path d="M20 20l-4.2-4.2"/>',
  user: '<circle cx="9" cy="8" r="3.5"/><path d="M2.5 20a6.5 6.5 0 0 1 13 0M16 4.5a3.5 3.5 0 0 1 0 7M18 14a5.5 5.5 0 0 1 3.5 6"/>',
  mic: '<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21"/>',
  download: '<path d="M12 4v11M7.5 10.5 12 15l4.5-4.5M4 20h16"/>',
});
const SECS = [['phong', 'Phòng và thiết bị', 'home'], ['den', 'Chiếu sáng', 'light'], ['dieuhoa', 'Điều hoà', 'snow'], ['camera', 'Camera', 'camera'],
  ['anninh', 'An ninh', 'shield'], ['ngucanh', 'Ngữ cảnh', 'spark'], ['tudong', 'Tự động hoá', 'auto'], null,
  ['taikhoan', 'Tài khoản & chia sẻ', 'user'], ['mang', 'Mạng & kết nối', 'router'], ['thongbao', 'Thông báo', 'bell'],
  ['troly', 'Trợ lý & giọng nói', 'mic'], ['nhac', 'Nhạc', 'music'], ['saoluu', 'Sao lưu & khôi phục', 'cloud'], ['hethong', 'Hệ thống', 'gear']];
// mục tích hợp (SETUP) nằm ở đâu; mục mới chưa khai thì vào Mạng & kết nối
const SETUP_SEC = {camera: 'camera', tuya: 'mang', nodes: 'mang', https: 'mang', ai: 'troly', voice: 'troly', notify: 'thongbao', spotify: 'nhac', music: 'nhac'};
const KIND_LABEL = {light: 'Chiếu sáng', socket: 'Ổ cắm', switch: 'Công tắc', breaker: 'CB', fan: 'Quạt', ac: 'Điều hoà', tv: 'TV', speaker: 'Loa',
  camera: 'Camera', pump: 'Bơm nước', water: 'Tưới cây', vacuum: 'Robot lau nhà', router: 'Wi-Fi', infra: 'Hạ tầng'};
const EDIT_KINDS = ['light', 'socket', 'switch', 'breaker', 'fan', 'ac', 'tv', 'speaker', 'pump', 'water'];
const SET = {sec: 'phong', item: '', sub: '', q: '', hash: ''};
const FD = {};   // bản nháp của các form: id form -> {trường: giá trị}

// Thiết bị nhiều nút (group) là một "thiết bị" gồm nhiều kênh, khoá theo command_topic (tuya/<id>/cmd).
const unitKey = d => d.group ? 'g:' + ((d.command_topic || '').split('/')[1] || d.group) : d.id;
function units(ds = cfg.devices) {
  const m = new Map();
  for (const d of ds) m.set(unitKey(d), [...(m.get(unitKey(d)) || []), d]);
  return m;
}
const unitName = chs => chs[0].group || chs[0].name;
const kindLabel = d => KIND_LABEL[kindOf(d)] || d.kind || 'Thiết bị';
const switchable = d => !PASSIVE.has(kindOf(d)) && !RICH.has(kindOf(d));

function setGo(sec, item = '', sub = '') {
  Object.assign(SET, {sec, item, sub, q: sec === SET.sec ? SET.q : ''});
  SET.hash = '#/caidat/' + [sec, item, sub].filter(Boolean).map(encodeURIComponent).join('/');
  if (location.hash !== SET.hash) history.replaceState(null, '', SET.hash);
  draw();
}
function setSync() {   // mở thẳng / quay lại một địa chỉ #/caidat/... thì lấy mục và dòng từ đó
  if (location.hash === SET.hash || !location.hash.startsWith('#/caidat')) return;
  if (location.hash === '#/caidat' && SET.hash) return history.replaceState(null, '', SET.hash);   // nút Cài đặt: về mục đang xem dở
  const [sec, item = '', sub = ''] = location.hash.slice(9).split('/').map(decodeURIComponent);
  Object.assign(SET, {sec: SECS.some(s => s && s[0] === sec) ? sec : 'phong', item, sub, hash: location.hash});
}

/* ---------- Ô nhập trong các form: chữ đang gõ giữ trong FD nên màn hình vẽ lại không mất ---------- */
const fv = (form, k, def) => FD[form]?.[k] ?? def;
function onSetField(e) {
  const f = e.target.closest?.('[data-sf]');
  if (!f) return;
  const [form, k] = f.dataset.sf.split('|');
  (FD[form] ||= {})[k] = f.value;
  if (k === 'floor' && !form.startsWith('z:') && form !== 'newroom') FD[form].zone = '';   // đổi tầng thì chọn lại phòng
  if (e.type === 'change' && f.tagName === 'SELECT') { f.blur(); soon(); }
}
document.addEventListener('input', onSetField);
document.addEventListener('change', onSetField);
const opt = (v, l, cur) => `<option value="${esc(v)}"${String(v) === String(cur) ? ' selected' : ''}>${esc(l)}</option>`;
const field = (label, html) => `<label class="sf"><span>${label}</span>${html}</label>`;
const input = (form, k, v, ph = '') => `<input data-sf="${esc(form)}|${k}" value="${esc(fv(form, k, v))}" placeholder="${esc(ph)}" autocomplete="off">`;
const select = (form, k, cur, opts) => `<select data-sf="${esc(form)}|${k}">${opts.map(([v, l]) => opt(v, l, fv(form, k, cur))).join('')}</select>`;
const floorOpts = () => floors().map(f => [f, floorName(f)]);

async function putSettings(ns, patch, done) {
  if (DEMO) return toast('Chế độ dữ liệu mẫu không lưu cấu hình');
  const r = await api(`/api/settings/${ns}`, {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(patch)});
  if (!r.ok) return toast(`Chưa lưu được: ${await r.text()}`);
  done?.();
  toast('Đã lưu, hub đang nạp lại...');
  draw();
}

/* ---------- Thiết bị: chi tiết, điều khiển, form tên / loại / tầng / phòng ---------- */
function devStatus(chs) {
  const d = chs[0];
  if (chs.every(offline)) return '<span class="st bad"><i class="dot bad"></i>Mất kết nối</span>';
  if (chs.length > 1) return `<span class="st">${chs.filter(isOn).length}/${chs.length} đang bật</span>`;
  if (switchable(d)) return `<button class="sw${isOn(d) ? ' on' : ''}" data-dev="${esc(d.id)}" aria-pressed="${isOn(d)}" aria-label="Bật/tắt ${esc(d.name)}"></button>`;
  return '<span class="st good"><i class="dot ok"></i>Online</span>';
}
const devRow = (key, chs, sec, sub = false) => `<div class="drow"><span class="dic k-${kindOf(chs[0])}">${icon(kindOf(chs[0]))}</span>
  <button class="dname" data-setitem="${esc(sub ? SET.item : key)}"${sub ? ` data-setsub="${esc(key)}"` : ''} data-setsec="${sec}"><b>${esc(unitName(chs))}</b>
  <small>${esc(kindLabel(chs[0]))}${sub ? '' : ` · ${esc(zone(chs[0].zone)?.name || 'Chưa gán phòng')}`}</small></button>${devStatus(chs)}
  <button class="go" data-setitem="${esc(sub ? SET.item : key)}"${sub ? ` data-setsub="${esc(key)}"` : ''} data-setsec="${sec}" aria-label="Chi tiết ${esc(unitName(chs))}">${icon('more')}</button></div>`;

function devForm(key) {
  const chs = units().get(key);
  if (!chs) return '';
  const d = chs[0], form = 'u:' + key, multi = chs.length > 1 || !!d.group, k = d.kind || '';
  const floor = fv(form, 'floor', zone(d.zone)?.floor ?? floors()[0]), meters = cfg.entities.some(e => e.device === d.id);
  const kinds = (EDIT_KINDS.includes(k) || !k ? EDIT_KINDS : [k, ...EDIT_KINDS]).map(x => [x, KIND_LABEL[x] || x]);
  return `<div class="setform">
    ${field(multi ? 'Tên thiết bị' : 'Tên', input(form, 'name', unitName(chs)))}
    ${field('Loại', select(form, 'kind', k, kinds))}
    ${field('Tầng', select(form, 'floor', floor, floorOpts()))}
    ${field('Phòng / khu vực', select(form, 'zone', d.zone, [['', 'Chọn phòng...'], ...cfg.zones.filter(z => z.floor === Number(floor)).map(z => [z.id, z.name])]))}
    ${fv(form, 'kind', k) === 'breaker' && meters ? field('Khu được cấp điện', select(form, 'area', cfg.entities.find(e => e.device === d.id)?.zone || '',
      [['', 'Giữ ở tủ điện'], ...cfg.zones.filter(z => z.type !== 'panel').map(z => [z.id, `${floorName(z.floor)} · ${z.name}`])])) : ''}
    ${multi ? chs.map((c, i) => field(`Nút ${i + 1}`, input(form, 'ch:' + c.id, c.name))).join('') : ''}
    <div class="kv"><button class="pill acc" data-setsave="${esc(form)}">Lưu thay đổi</button></div></div>`;
}
function devSave(key) {
  const chs = units().get(key), form = 'u:' + key;
  if (!chs) return;
  const d = chs[0], multi = chs.length > 1 || !!d.group, name = String(fv(form, 'name', unitName(chs))).trim(), z = fv(form, 'zone', d.zone), area = fv(form, 'area', '');
  if (!name) return toast('Nhập tên thiết bị');
  if (!z) return toast('Chọn phòng cho thiết bị');
  const patch = {};
  for (const c of chs) patch[c.id] = {zone: z, kind: fv(form, 'kind', d.kind || 'switch'), ...(area ? {area} : {}),
    ...(multi ? {group: name, name: String(fv(form, 'ch:' + c.id, c.name)).trim() || c.name} : {name})};
  putSettings('devices', patch, () => delete FD[form]);
}
function devDetail(key, back = '') {
  const chs = units().get(key);
  if (!chs) return '<p class="empty">Không tìm thấy thiết bị này.</p>';
  const d = chs[0], k = kindOf(d), z = zone(d.zone), ents = cfg.entities.filter(e => chs.some(c => c.id === e.device) && has(e));
  const open = k === 'ac' ? `ac:${d.zone}` : k === 'tv' ? `tv:${d.id}` : RICH.has(k) ? `dev:${d.id}` : '';
  return `${back}<div class="dhero"><span class="dic big k-${k}">${icon(k)}</span><div><h2>${esc(unitName(chs))}</h2>
      <p>${esc(kindLabel(d))} · ${esc(z ? `${floorName(z.floor)}, ${z.name}` : 'Chưa gán phòng')}</p></div>${chs.length === 1 ? devStatus(chs) : ''}</div>
    ${k === 'camera' && !DEMO ? `<div class="camview" style="${camBg(d)}" data-live="/api/cam/${encodeURIComponent(d.id)}/ws"></div>` : ''}
    ${chs.length > 1 ? `<section class="setcard"><h3>Từng nút</h3>${chs.map(c => `<div class="drow"><span class="dic k-${kindOf(c)}">${icon(kindOf(c))}</span><span class="dname"><b>${esc(c.name)}</b></span>${devStatus([c])}</div>`).join('')}</section>` : ''}
    ${open ? `<div class="kv"><button class="pill" data-sheet="${esc(open)}">${icon('menu')}Mở bảng điều khiển</button></div>` : ''}
    ${ents.length ? `<section class="setcard"><h3>Số đo</h3><div class="kvs">${ents.map(e => `<span>${esc(e.name)}<b>${esc(fmt(e))}</b></span>`).join('')}</div></section>` : ''}
    <section class="setcard"><h3>${icon('gear')}Thông tin thiết bị</h3>${devForm(key)}</section>
    ${d.protected ? '<p class="note">Thiết bị được bảo vệ: bật/tắt phải xác nhận, trợ lý giọng nói không được điều khiển.</p>' : ''}`;
}

/* ---------- Mục Phòng và thiết bị ---------- */
function roomCard(z) {
  const us = units(devsIn(z.id)), ks = [...new Set([...us.values()].map(c => kindOf(c[0])))].slice(0, 4);
  return `<button class="rcard${SET.item === z.id ? ' sel' : ''}" data-setitem="${esc(z.id)}"><span class="rthumb" style="${thumbBg(z)}"></span>
    <span class="ric">${icon(roomIcon(z.id))}</span><span class="rname"><b>${esc(z.name)}</b><small>${esc(floorName(z.floor))}</small></span>
    <span class="rcount">${icon('cube')}<small>${us.size} thiết bị</small></span>
    <span class="rkinds">${ks.map(k => `<i>${icon(k)}<b>${[...us.values()].filter(c => kindOf(c[0]) === k).length}</b></i>`).join('')}</span>${icon('more', 'chev')}</button>`;
}
function roomDetail(zid) {
  if (zid === '+new') {
    const f = 'newroom';
    return `<section class="setcard"><h3>${icon('plus')}Thêm phòng</h3><div class="setform">
      ${field('Tên phòng', input(f, 'name', '', 'Ví dụ: Phòng khách'))}${field('Tầng', select(f, 'floor', floors()[0], floorOpts()))}
      ${field('Mô tả', `<textarea data-sf="${f}|desc" rows="3" placeholder="Không bắt buộc">${esc(fv(f, 'desc', ''))}</textarea>`)}
      <div class="kv"><button class="pill" data-setitem="">Huỷ</button><button class="pill acc" data-setsave="newroom">Thêm phòng</button></div></div></section>`;
  }
  const z = zone(zid);
  if (!z) return '<p class="empty">Chọn một phòng ở danh sách.</p>';
  if (SET.sub && SET.sub !== '+pick') return devDetail(SET.sub, `<button class="back" data-setitem="${esc(z.id)}">${icon('back')}${esc(z.name)}</button>`);
  const us = units(devsIn(z.id)), f = 'z:' + z.id;
  if (SET.sub === '+pick') {
    const other = [...units()].filter(([, c]) => c[0].zone !== z.id);
    return `<button class="back" data-setitem="${esc(z.id)}">${icon('back')}${esc(z.name)}</button><section class="setcard"><h3>Thêm thiết bị vào ${esc(z.name)}</h3>
      <p class="note">Chọn thiết bị đang ở phòng khác hoặc chưa gán phòng để chuyển vào đây. Thiết bị mới thì kết nối ở Mạng & kết nối (Tuya, node ESP32).</p>
      ${other.map(([k, c]) => `<div class="drow"><span class="dic k-${kindOf(c[0])}">${icon(kindOf(c[0]))}</span><span class="dname"><b>${esc(unitName(c))}</b>
        <small>${esc(zone(c[0].zone)?.name || 'Chưa gán phòng')}</small></span><button class="pill" data-setmove="${esc(k)}">Chuyển vào</button></div>`).join('') || '<p class="empty">Mọi thiết bị đã ở phòng này.</p>'}</section>`;
  }
  return `<div class="rhero" style="${thumbBg(z)}"></div><h2 class="rtitle">${esc(z.name)}</h2>
    <p class="rmeta">${esc(floorName(z.floor))} · ${us.size} thiết bị</p>${z.desc ? `<p class="rdesc">${esc(z.desc)}</p>` : ''}
    <section class="setcard"><h3>${icon('home')}Thông tin phòng</h3><div class="setform">
      ${field('Tên phòng', input(f, 'name', z.name))}${field('Vị trí', select(f, 'floor', z.floor, floorOpts()))}
      ${field('Mô tả', `<textarea data-sf="${esc(f)}|desc" rows="3" placeholder="Mô tả ngắn về phòng">${esc(fv(f, 'desc', z.desc || ''))}</textarea>`)}
      <div class="kv"><button class="pill acc" data-setsave="${esc(f)}">Lưu thay đổi</button></div></div></section>
    <section class="setcard"><div class="sch"><h3>${icon('cube')}Thiết bị trong phòng (${us.size})</h3>
      <button class="pill acc sm" data-setitem="${esc(z.id)}" data-setsub="+pick">${icon('plus')}Thêm thiết bị</button></div>
      ${[...us].map(([k, c]) => devRow(k, c, 'phong', true)).join('') || '<p class="empty">Phòng chưa có thiết bị.</p>'}</section>`;
}
function roomSave(form) {
  if (form === 'newroom') {
    const name = String(fv(form, 'name', '')).trim();
    if (!name) return toast('Nhập tên phòng');
    let id = fold(name).replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 28) || 'phong';
    for (let i = 2; zone(id); i++) id = id.replace(/-\d+$/, '') + '-' + i;
    return putSettings('zones', {[id]: {name, floor: Number(fv(form, 'floor', floors()[0])), desc: String(fv(form, 'desc', '')).trim()}},
      () => { delete FD[form]; SET.item = ''; });
  }
  const z = zone(form.slice(2)), name = String(fv(form, 'name', z.name)).trim();
  if (!name) return toast('Nhập tên phòng');
  putSettings('zones', {[z.id]: {name, floor: Number(fv(form, 'floor', z.floor)), desc: String(fv(form, 'desc', z.desc || '')).trim()}}, () => delete FD[form]);
}

/* ---------- Mục tích hợp (SETUP), tài khoản, sao lưu, hệ thống ---------- */
function setupCard(id) {
  const m = SETUP.find(x => x.id === id);
  if (!m) return '';
  if (m.ns && !(m.ns in SAVED)) loadSetting(m.ns);
  return `<section class="setcard" data-setup="${esc(m.id)}"><h3>${icon(m.icon || 'gear', 'wi ' + (m.c || ''))}${m.title}${m.meta ? `<small>${m.meta()}</small>` : ''}</h3>${m.body()}</section>`;
}
const setupsOf = sec => SETUP.filter(m => (SETUP_SEC[m.id] || 'mang') === sec);
let SESS = null;
async function loadSessions() {
  SESS = SESS || [];
  if (DEMO) { SESS = [{id: 'a1b2c3d4e5f6', created: Date.now() / 1000 - 3600, current: true}, {id: '0f9e8d7c6b5a', created: Date.now() / 1000 - 86400 * 3}]; return soon(); }
  const r = await api('/api/sessions');
  if (r.ok) { SESS = await r.json(); soon(); }
}
const when = t => new Date(t * 1000).toLocaleString('vi-VN', {hour: '2-digit', minute: '2-digit', day: '2-digit', month: '2-digit', year: 'numeric'});

/* ---------- Từng mục: tiêu đề, danh sách giữa, chi tiết phải ---------- */
const row = (id, ic, title, sub, right = '', cls = '') => `<button class="lrow${SET.item === id ? ' sel' : ''}${cls}" data-setitem="${esc(id)}">
  <span class="dic">${ic}</span><span class="dname"><b>${title}</b>${sub ? `<small>${sub}</small>` : ''}</span>${right}${icon('more', 'chev')}</button>`;
const byRoom = list => {   // [key, chs] -> nhóm theo phòng, có tiêu đề phòng
  const zs = [...new Set(list.map(([, c]) => c[0].zone))];
  return zs.map(z => `<h4>${esc(zone(z)?.name || 'Chưa gán phòng')}</h4>${list.filter(([, c]) => c[0].zone === z).map(([k, c]) => devRow(k, c, SET.sec)).join('')}`).join('');
};
const match = s => !SET.q || fold(s).includes(fold(SET.q));
const kindUnits = ks => [...units()].filter(([, c]) => ks.includes(kindOf(c[0])) && match(unitName(c)));

const PAGES = {
  phong: () => {
    const zs = cfg.zones.filter(z => match(z.name) || devsIn(z.id).some(d => match(d.name) || match(d.group || '')));
    return {t: 'Phòng và thiết bị', meta: `${cfg.zones.length} phòng | ${units().size} thiết bị`, search: 'Tìm thiết bị, phòng...',
      btn: `<button class="pill acc" data-setitem="+new">${icon('plus')}Thêm phòng</button>`,
      list: zs.map(roomCard).join('') || '<p class="empty">Không có phòng nào khớp.</p>', first: zs[0]?.id, detail: roomDetail};
  },
  den: () => ({t: 'Chiếu sáng', meta: `${kindUnits(['light']).length} thiết bị`, search: 'Tìm đèn...', list: byRoom(kindUnits(['light'])) || '<p class="empty">Chưa có đèn.</p>',
    first: kindUnits(['light'])[0]?.[0], detail: devDetail}),
  dieuhoa: () => ({t: 'Điều hoà', meta: `${kindUnits(['ac']).length} máy`, search: 'Tìm máy lạnh...', list: byRoom(kindUnits(['ac'])) || '<p class="empty">Chưa có máy lạnh (lắp module hồng ngoại trên node ESP32).</p>',
    first: kindUnits(['ac'])[0]?.[0], detail: devDetail}),
  camera: () => {
    const cs = kindUnits(['camera']);
    return {t: 'Camera', meta: `${cs.filter(([, c]) => !offline(c[0])).length}/${cs.length} đang ghi hình`, search: 'Tìm camera...',
      btn: `<button class="pill" data-go="camera">${icon('camera')}Xem tất cả</button>`,
      list: row('nvr', icon('server'), 'Đầu ghi Hikvision', 'Địa chỉ, tài khoản, chọn kênh') + byRoom(cs), first: 'nvr',
      detail: id => id === 'nvr' ? setupCard('camera') : devDetail(id)};
  },
  anninh: () => {
    const al = alerts(), lost = [...units()].filter(([, c]) => c.every(offline)), prot = [...units()].filter(([, c]) => c[0].protected);
    return {t: 'An ninh', meta: al.length ? `${al.length} cảnh báo đang mở` : 'Bình thường',
      list: `<h4>Cảnh báo đang mở</h4>${al.map(k => row(k, icon('warn'), esc(state[k]), 'Luật ' + esc(k.slice(6)), '', ' warn')).join('') || '<p class="empty">Không có cảnh báo.</p>'}
        <h4>Mất kết nối</h4>${lost.map(([k, c]) => devRow(k, c, 'anninh')).join('') || '<p class="empty">Mọi thiết bị đang kết nối.</p>'}
        <h4>Được bảo vệ (cần xác nhận khi bật/tắt)</h4>${prot.map(([k, c]) => devRow(k, c, 'anninh')).join('') || '<p class="empty">Không có.</p>'}`,
      first: al[0] || lost[0]?.[0] || prot[0]?.[0],
      detail: id => id.startsWith('alert:') ? `<section class="setcard"><h3>${icon('warn')}Cảnh báo</h3><p class="big">${esc(state[id] || 'Đã hết')}</p>
        <div class="kv"><button class="pill" data-setgo="tudong|${esc(id.slice(6))}">Xem luật gây cảnh báo</button></div></section>` : devDetail(id)};
  },
  ngucanh: () => {
    const sc = (cfg.scenes || []).map((s, i) => [String(i), s]).filter(([, s]) => match(s.name));
    return {t: 'Ngữ cảnh', meta: `${(cfg.scenes || []).length} ngữ cảnh`, search: 'Tìm ngữ cảnh...',
      list: sc.map(([i, s]) => row(i, icon(s.icon || 'spark'), esc(s.name), `${(s.steps || []).length} bước${s.pin ? ' · cần PIN' : ''}`)).join('') || '<p class="empty">Chưa có ngữ cảnh.</p>',
      first: sc[0]?.[0], detail: i => {
        const s = cfg.scenes[+i];
        if (!s) return '';
        return `<div class="dhero"><span class="dic big">${icon(s.icon || 'spark')}</span><div><h2>${esc(s.name)}</h2><p>${esc(s.note || `${(s.steps || []).length} bước`)}</p></div></div>
          <div class="kv"><button class="pill acc" data-scene="${+i}">${icon('play')}Chạy ngữ cảnh</button></div>
          <section class="setcard"><h3>Các bước</h3>${(s.steps || []).map((st, n) => { const d = cfg.devices.find(x => x.id === st.d);
            return `<div class="drow"><span class="step">${n + 1}</span><span class="dname"><b>${esc(d ? unitName([d]) + (d.group ? ' ' + d.name : '') : st.d)}</b><small>${esc(zone(d?.zone)?.name || '')}</small></span><span class="st">${st.to === 'off' ? 'Tắt' : 'Bật'}</span></div>`; }).join('')}</section>
          <p class="note">Sửa các bước ngữ cảnh trong config.yaml trên hub.</p>`;
      }};
  },
  tudong: () => {
    const rs = (cfg.rules || []).filter(r => match(r.alert || r.id) || match(cfg.entities.find(e => e.id === r.entity)?.name || ''));
    const cond = r => { const e = cfg.entities.find(x => x.id === r.entity), u = e?.unit || '';
      return `${esc(e?.name || r.entity)}${e?.zone ? ` (${esc(zone(e.zone)?.name || '')})` : ''} ${r.above != null ? `trên ${nf.format(r.above)} ${u}` : ''}${r.below != null ? `dưới ${nf.format(r.below)} ${u}` : ''}${r.for ? ` trong ${Math.round(r.for / 6e10)} phút` : ''}`; };
    const acts = as => (as || []).map(a => a.set ? `<div class="drow"><span class="dname"><b>${esc(cfg.devices.find(d => d.id === a.set)?.name || a.set)}</b></span><span class="st">${a.to === 'off' ? 'Tắt' : 'Bật'}</span></div>`
      : `<div class="drow"><span class="dname"><b>Thông báo</b><small>${esc(a.notify || '')}</small></span></div>`).join('');
    return {t: 'Tự động hoá', meta: `${(cfg.rules || []).length} luật`, search: 'Tìm luật...',
      list: rs.map(r => row(r.id, icon(state['alert:' + r.id] ? 'warn' : 'auto'), esc(r.alert || r.id), cond(r), state['alert:' + r.id] ? '<span class="st bad">Đang kích hoạt</span>' : '')).join('') || '<p class="empty">Chưa có luật tự động.</p>',
      first: rs[0]?.id, detail: id => { const r = (cfg.rules || []).find(x => x.id === id);
        return r ? `<div class="dhero"><span class="dic big">${icon('auto')}</span><div><h2>${esc(r.alert || r.id)}</h2><p>${state['alert:' + r.id] ? 'Đang kích hoạt' : 'Đang chờ điều kiện'}</p></div></div>
          <section class="setcard"><h3>Khi</h3><p>${cond(r)}</p></section>
          ${r.then?.length ? `<section class="setcard"><h3>Thì</h3>${acts(r.then)}</section>` : ''}${r.on_clear?.length ? `<section class="setcard"><h3>Khi hết</h3>${acts(r.on_clear)}</section>` : ''}
          <p class="note">Sửa luật trong config.yaml trên hub; hub tự nạp lại khi file đổi.</p>` : ''; }};
  },
  taikhoan: () => ({t: 'Tài khoản & chia sẻ', meta: 'Mã PIN chung của nhà',
    list: row('phien', icon('monitor'), 'Thiết bị đã đăng nhập', SESS ? `${SESS.length} thiết bị` : 'Máy nào đang mở dashboard') + row('chiase', icon('user'), 'Chia sẻ truy cập', 'Mở dashboard trên máy khác'),
    first: 'phien', detail: id => {
      if (id === 'chiase') return `<section class="setcard"><h3>${icon('user')}Chia sẻ truy cập</h3>
        <p class="note">Mở một trong hai địa chỉ trên máy khác trong mạng nhà rồi nhập mã PIN của nhà. Micro và Spotify cần địa chỉ HTTPS.</p>
        <div class="kvs"><span>Thường<b>http://${esc(location.hostname)}:8080</b></span><span>HTTPS<b>https://${esc(location.hostname)}:8443</b></span></div>
        <p class="note">Mã PIN đặt trong .env trên hub (HUBD_PIN). Đổi PIN xong thì đăng xuất mọi máy để ai cũng phải nhập lại.</p></section>`;
      if (!SESS) loadSessions();
      return `<section class="setcard"><h3>${icon('monitor')}Thiết bị đã đăng nhập</h3>${(SESS || []).map(s => `<div class="drow"><span class="dic">${icon('monitor')}</span>
        <span class="dname"><b>${s.current ? 'Máy này' : 'Máy khác'} <em class="tag">${esc(s.id.slice(0, 6))}</em></b><small>Đăng nhập ${esc(when(s.created))}</small></span>
        ${s.current ? '<span class="st good">Đang dùng</span>' : `<button class="pill sm" data-setact="sess-del|${esc(s.id)}">Đăng xuất</button>`}</div>`).join('') || '<p class="empty">Đang tải...</p>'}
        <div class="kv"><button class="pill" data-setact="sess-all">Đăng xuất mọi máy khác</button></div>
        <p class="note">Mỗi lần nhập PIN là một phiên, giữ 90 ngày. Máy bị đăng xuất phải nhập lại PIN.</p></section>`;
    }}),
  mang: () => sectionOfSetups('mang', 'Mạng & kết nối', `${cfg.devices.filter(d => !offline(d)).length}/${cfg.devices.length} thiết bị đang kết nối`,
    row('hub', icon('server'), 'Hub Center', $('#conn')?.title || 'Kết nối máy chủ'),
    id => id === 'hub' ? `<section class="setcard"><h3>${icon('server')}Hub Center</h3><div class="kvs"><span>Địa chỉ<b>${esc(location.host)}</b></span>
      <span>Kết nối trực tiếp<b>${esc($('#conn')?.title || '—')}</b></span><span>Phòng<b>${cfg.zones.length}</b></span><span>Thiết bị<b>${cfg.devices.length}</b></span>
      <span>Số đo<b>${cfg.entities.length}</b></span><span>Mất kết nối<b>${cfg.devices.filter(offline).length}</b></span></div></section>` : ''),
  thongbao: () => sectionOfSetups('thongbao', 'Thông báo', alerts().length ? `${alerts().length} cảnh báo đang mở` : 'Không có cảnh báo',
    row('canhbao', icon('warn'), 'Cảnh báo đang mở', `${alerts().length} cảnh báo`),
    id => id === 'canhbao' ? `<section class="setcard"><h3>${icon('warn')}Cảnh báo đang mở</h3>${alerts().map(k => `<div class="drow"><span class="dic">${icon('warn')}</span><span class="dname"><b>${esc(state[k])}</b><small>${esc(k.slice(6))}</small></span></div>`).join('') || '<p class="empty">Không có cảnh báo.</p>'}</section>` : ''),
  troly: () => sectionOfSetups('troly', 'Trợ lý & giọng nói', 'AI, giọng đọc, xưng hô'),
  nhac: () => sectionOfSetups('nhac', 'Nhạc', 'Spotify và loa'),
  saoluu: () => ({t: 'Sao lưu & khôi phục', meta: 'Cài đặt nhập trên dashboard',
    list: row('saoluu', icon('download'), 'Tải bản sao lưu', 'Một tệp .json') + row('khoiphuc', icon('undo'), 'Khôi phục', 'Từ tệp đã tải'),
    first: 'saoluu', detail: id => id === 'khoiphuc' ? `<section class="setcard"><h3>${icon('undo')}Khôi phục</h3>
      <p class="note">Chọn tệp sao lưu iNIX Home. Phòng, thiết bị, đầu ghi, Tuya, AI... ghi đè theo tệp; khoá API và mật khẩu đang có giữ nguyên (tệp không chứa chúng). Hub tự nạp lại sau khi khôi phục.</p>
      <input type="file" accept="application/json,.json" data-setfile><div class="kv"><button class="pill acc" data-setact="restore">Khôi phục</button></div></section>`
      : `<section class="setcard"><h3>${icon('download')}Tải bản sao lưu</h3>
      <p class="note">Gồm sắp xếp phòng và thiết bị, đầu ghi, Tuya, Spotify, AI, giọng đọc, thông báo. <b>Không</b> gồm khoá API, mật khẩu, token: khôi phục sang hub mới thì nhập lại các ô đó. Cấu hình gốc (config.yaml) và dữ liệu lịch sử nằm trên hub, không có trong tệp.</p>
      <div class="kv"><a class="pill acc" href="/api/backup" download>${icon('download')}Tải bản sao lưu</a></div></section>`}),
  hethong: () => ({t: 'Hệ thống', meta: 'Giao diện và thông tin hub',
    list: row('giaodien', icon('sun'), 'Giao diện', skyMode === 'auto' ? 'Ngày/đêm tự động' : 'Ngày/đêm chọn tay') + row('thongtin', icon('server'), 'Thông tin hub', `${cfg.devices.length} thiết bị, ${cfg.entities.length} số đo`),
    first: 'giaodien', detail: id => id === 'thongtin' ? PAGES.mang().detail('hub') : `<section class="setcard"><h3>${icon('auto')}Ngày và đêm</h3>
      <p class="note">${skyMode === 'auto' ? `Tự động theo ${esc(sky().by)}` : 'Đang chọn tay'}</p><div class="seg">${[['auto', 'Tự động'], ['day', 'Ban ngày'], ['night', 'Ban đêm']]
        .map(([v, l]) => `<button class="rb${skyMode === v ? ' sel' : ''}" data-skyset="${v}">${l}</button>`).join('')}</div></section>
      <section class="setcard"><h3>${icon('grip')}Bố cục widget</h3><p class="note">Lưu trên thiết bị này.</p>
      <div class="kv"><button class="pill" data-go="" data-editgo>Sắp xếp trang chủ</button><button class="pill" data-wreset>Về mặc định</button></div></section>`}),
};
function sectionOfSetups(sec, t, meta, extra = '', extraDetail = () => '') {
  const ms = setupsOf(sec);
  return {t, meta, list: extra + ms.map(m => row(m.id, icon(m.icon || 'gear', 'wi ' + (m.c || '')), m.title, m.meta ? m.meta() : '')).join(''),
    first: ms[0]?.id || (extra ? extra.match(/data-setitem="([^"]+)"/)?.[1] : ''), detail: id => extraDetail(id) || setupCard(id)};
}

function settingsPage() {
  setSync();
  let p = PAGES[SET.sec]();
  if (!SET.item && innerWidth > 900 && p.first) { SET.item = p.first; p = PAGES[SET.sec](); }   // màn rộng: mở sẵn dòng đầu
  const nav = SECS.map(s => s ? `<button class="snav${s[0] === SET.sec ? ' sel' : ''}" data-setsec="${s[0]}" data-setitem="">${icon(s[2])}<span>${s[1]}</span></button>` : '<i class="sgap"></i>').join('');
  const detail = SET.item ? p.detail(SET.item) : '';
  return `<div class="setpage${SET.item ? ' has-item' : ''}">
    <nav class="setnav glass" data-keep="set-nav"><h1>Cài đặt</h1><p>Quản lý hệ thống hub thông minh</p><div class="snavs">${nav}</div></nav>
    <section class="setmain glass"><header class="seth"><span class="sic">${icon(SECS.find(s => s && s[0] === SET.sec)[2])}</span><div><h2>${p.t}</h2><small>${esc(p.meta || '')}</small></div>
      ${p.search ? `<label class="ssearch">${icon('search')}<input data-setq value="${esc(SET.q)}" placeholder="${esc(p.search)}" aria-label="${esc(p.search)}"></label>` : ''}${p.btn || ''}</header>
      <div class="setlist" data-keep="set-list:${SET.sec}">${p.list}</div></section>
    <aside class="setdetail glass" data-keep="set-detail:${esc(SET.sec)}:${esc(SET.item)}:${esc(SET.sub)}"><button class="back mob" data-setitem="">${icon('back')}${p.t}</button>${detail || '<p class="empty">Chọn một mục ở danh sách.</p>'}</aside></div>`;
}

document.addEventListener('input', e => { if (e.target.matches?.('[data-setq]')) { SET.q = e.target.value; soon(); } });
document.addEventListener('click', async e => {
  const t = e.target.closest?.('[data-setsec], [data-setitem], [data-setsave], [data-setmove], [data-setact], [data-setgo]');
  if (!t || !t.closest('.setpage')) return;
  if (t.dataset.setsave !== undefined) {
    const f = t.dataset.setsave;
    return f.startsWith('u:') ? devSave(f.slice(2)) : roomSave(f);
  }
  if (t.dataset.setmove) {
    const zid = SET.item, chs = units().get(t.dataset.setmove) || [];
    return putSettings('devices', Object.fromEntries(chs.map(c => [c.id, {zone: zid}])), () => { SET.sub = ''; });
  }
  if (t.dataset.setgo) { const [s, i] = t.dataset.setgo.split('|'); return setGo(s, i); }
  if (t.dataset.setact) {
    const [a, id] = t.dataset.setact.split('|');
    if (DEMO) return toast('Chế độ dữ liệu mẫu không đổi được');
    if (a === 'sess-del' || a === 'sess-all') {
      if (a === 'sess-all' && !(await ask({title: 'Đăng xuất mọi máy khác?', text: 'Các máy đó phải nhập lại mã PIN.', ok: 'Đăng xuất'}))) return;
      await api(a === 'sess-all' ? '/api/sessions' : `/api/sessions/${encodeURIComponent(id)}`, {method: 'DELETE'});
      SESS = null;
      return toast('Đã đăng xuất');
    }
    if (a === 'restore') {
      const file = document.querySelector('[data-setfile]')?.files?.[0];
      if (!file) return toast('Chọn tệp sao lưu trước');
      if (!(await ask({title: 'Khôi phục từ tệp này?', text: `${file.name}: cài đặt hiện tại được ghi đè theo tệp, hub tự nạp lại.`, ok: 'Khôi phục'}))) return;
      const r = await api('/api/restore', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: await file.text()});
      return toast(r.ok ? 'Đã khôi phục, hub đang nạp lại...' : `Chưa khôi phục được: ${await r.text()}`);
    }
    return;
  }
  setGo(t.dataset.setsec || SET.sec, t.dataset.setitem ?? SET.item, t.dataset.setsub || '');
});
Object.assign(window.SETUP_UI, {unitKey, units, devForm, devSave});
