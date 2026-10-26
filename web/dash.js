// Dashboard Hub Center: mặt cắt nhà theo tầng -> phòng -> thiết bị, trang "Cả nhà" gồm widget kéo thả được.
// Thêm ?demo vào địa chỉ để xem dữ liệu mẫu. Cần app.js (api, esc, LOGIN_HTML) và demo.js (DEMO_DATA).
'use strict';
const $ = (s, el = document) => el.querySelector(s);
const DEMO = new URLSearchParams(location.search).has('demo');
const nf = new Intl.NumberFormat('vi-VN', {maximumFractionDigits: 1});

let cfg = {zones: [], entities: [], devices: [], scenes: []}, state = {};
let view = {floor: 0, room: '', page: ''}, editing = false, drag = null;
const pending = new Set();

// Loại thiết bị: category Tuya (kg, dlq, pc, sd...) hoặc tên tự đặt trong config.
const KIND = {kg: 'light', light: 'light', dj: 'light', pc: 'socket', cz: 'socket', socket: 'socket',
  dlq: 'breaker', breaker: 'breaker', tv: 'tv', speaker: 'speaker', fan: 'fan', fs: 'fan', ac: 'ac', kt: 'ac',
  sd: 'vacuum', vacuum: 'vacuum', router: 'router', wifi: 'router', sp: 'camera', camera: 'camera',
  pump: 'pump', ggq: 'water', sfkzq: 'water', water: 'water', infra: 'infra'};
const IR = new Set(['tv', 'speaker', 'fan', 'ac']);   // hồng ngoại: một chiều, không đọc được trạng thái thật
const RICH = new Set([...IR, 'vacuum']);             // thẻ có nút điều khiển riêng
const PASSIVE = new Set(['infra', 'router', 'camera']);   // chỉ báo trạng thái, không bật tắt
// Số đo inverter Deye + pin JK (node RS485). grid: + mua / − bán; jk.power: + sạc / − xả. *_today: kWh trong ngày.
const ENERGY_IDS = new Set(['deye.pv', 'deye.grid', 'deye.load', 'jk.soc', 'jk.power', 'deye.pv_today', 'deye.load_today',
  'deye.charge_today', 'deye.discharge_today', 'deye.buy_today', 'deye.sell_today']);
const VAC = {cleaning: 'Đang lau', paused: 'Tạm dừng', returning: 'Đang về dock', docked: 'Đang sạc ở dock'};

const ICON = {
  light: '<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.6 10.8c.7.6 1.1 1.3 1.1 2.1V17h5v-1.1c0-.8.4-1.5 1.1-2.1A6 6 0 0 0 12 3z"/>',
  socket: '<rect x="4" y="4" width="16" height="16" rx="5"/><path d="M9.5 9.5v2M14.5 9.5v2M10 15h4"/>',
  breaker: '<rect x="7" y="2.5" width="10" height="19" rx="2"/><rect x="10" y="6" width="4" height="6" rx="1"/><path d="M10 17h4"/>',
  tv: '<rect x="3" y="5" width="18" height="12" rx="2"/><path d="M8 21h8M12 17v4"/>',
  speaker: '<rect x="6" y="2.5" width="12" height="19" rx="3"/><circle cx="12" cy="14" r="3.2"/><circle cx="12" cy="7" r=".9"/>',
  fan: '<circle cx="12" cy="12" r="1.8"/><path d="M12 10.2c-.4-4 1-6.7 3.4-6.2 2.6.6 1.6 4.7-1.9 7M13.6 12.9c3.5 1.9 4.8 4.6 3 6.3-2 1.7-4.6-1.6-4.4-5.6M10.4 12.6c-3.4 2.1-6.3 1.9-6.8-.5-.5-2.6 3.8-3.3 7.4-1.6"/>',
  ac: '<rect x="2.5" y="5" width="19" height="8.5" rx="2"/><path d="M6.5 10.5h11M8 17l-1 3M12 16.5V20M16 17l1 3"/>',
  vacuum: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="9" r="2.3"/><path d="M7.5 15.5h9"/>',
  router: '<path d="M2.5 9a14 14 0 0 1 19 0M5.5 12.5a9.5 9.5 0 0 1 13 0M8.5 16a5 5 0 0 1 7 0M12 19.5h.01"/>',
  camera: '<rect x="2.5" y="6.5" width="13.5" height="11" rx="2.5"/><path d="M16 10.5l5.5-3v9L16 13.5"/>',
  pump: '<path d="M12 3s6 6.5 6 11a6 6 0 0 1-12 0c0-4.5 6-11 6-11z"/>',
  water: '<path d="M12 21v-8M8 13h8M12 9.5V3M7.5 8 5 5.5M16.5 8 19 5.5M5 12H2.5M19 12h2.5"/>',
  infra: '<rect x="3" y="13" width="18" height="6" rx="2"/><path d="M7 16h.01M10.5 16h.01M8 13V9.5M16 13V9.5"/>',
  switch: '<path d="M12 3v8M7 6.5a7 7 0 1 0 10 0"/>',
  power: '<path d="M12 3v8M7 6.5a7 7 0 1 0 10 0"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"/>',
  auto: '<circle cx="12" cy="12" r="8.5"/><path d="M12 3.5v17A8.5 8.5 0 0 0 12 3.5z" fill="currentColor"/>',
  battery: '<rect x="2.5" y="7" width="17" height="10" rx="2.5"/><path d="M22 10.5v3M6 10.5v3M9.5 10.5v3"/>',
  grid: '<path d="M12 3 7 21M12 3l5 18M8.6 9h6.8M7.4 14h9.2M4 9h16"/>',
  home: '<path d="M4 11 12 4l8 7M6 9.5V20h12V9.5M10 20v-5h4v5"/>',
  store: '<path d="M4 10v10h16V10M3 10l1.5-6h15L21 10zM9 20v-5h6v5"/>',
  door: '<path d="M6 21V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v17M3 21h18M14.5 12h.01"/>',
  bolt: '<path d="M13 2.5 5 13.5h6l-1 8 8-11h-6l1-8z"/>',
  lock: '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
  back: '<path d="M15 6l-6 6 6 6"/>',
  play: '<path d="M8 5.5v13l10-6.5z"/>',
  pause: '<path d="M8 5v14M16 5v14"/>',
  dock: '<path d="M4 20h16M7 20v-6a5 5 0 0 1 10 0v6M12 4v4M9.5 6.5 12 9l2.5-2.5"/>',
  warn: '<path d="M12 4 2.5 20h19zM12 10v4M12 17h.01"/>',
  grip: '<path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3.2"/>',
};
const icon = (k, cls = '') => `<svg class="ic ${cls}" viewBox="0 0 24 24" aria-hidden="true">${ICON[k] || ICON.switch}</svg>`;

const kindOf = d => KIND[d.kind] || 'switch';
const isOn = d => /^\s*(on|1|true)\s*$/i.test(state['dev:' + d.id] || '');
const known = d => state['dev:' + d.id] !== undefined;
const offline = d => state['online:' + d.id] === 'offline';
const counted = d => !['breaker', ...PASSIVE].includes(kindOf(d));   // thiết bị tính vào "đang bật"
const zone = id => cfg.zones.find(z => z.id === id);
const floorOf = d => zone(d.zone)?.floor || 0;
const floors = () => [...new Set(cfg.zones.map(z => z.floor).filter(Boolean))].sort((a, b) => b - a);
const rooms = f => cfg.zones.filter(z => z.floor === f && z.type !== 'panel');
const panels = f => cfg.zones.filter(z => z.floor === f && z.type === 'panel');
const devsIn = zid => cfg.devices.filter(d => d.zone === zid);
const sensorsIn = zid => cfg.entities.filter(e => e.zone === zid && !e.device && !ENERGY_IDS.has(e.id));
const readingsOf = d => cfg.entities.filter(e => e.device === d.id);
const onIn = zid => devsIn(zid).filter(d => counted(d) && isOn(d));
const lit = zid => devsIn(zid).some(d => kindOf(d) === 'light' && isOn(d));
const routerOf = f => cfg.devices.find(d => kindOf(d) === 'router' && floorOf(d) === f);
const where = d => { const z = zone(d.zone); return z ? `${z.floor}${z.type === 'panel' ? '' : '/' + esc(z.id)}` : ''; };
const fmt = v => { const n = Number(v); return v === '' || isNaN(n) ? v : nf.format(n); };
const val = e => state[e.id] === undefined ? '—' : esc(fmt(state[e.id]));
const has = e => state[e.id] !== undefined;
const sensorBy = (zid, unit) => sensorsIn(zid).find(e => e.unit === unit && has(e));
const plural = (n, word) => `${n} ${word}`;
const num = id => state[id] !== undefined && state[id] !== '' && !isNaN(Number(state[id])) ? Number(state[id]) : NaN;
const fw = w => isNaN(w) ? '—' : Math.abs(w) >= 1000 ? `${nf.format(Math.round(Math.abs(w) / 10) / 100)} kW` : `${nf.format(Math.round(Math.abs(w)))} W`;
// Công suất một khu = tổng số đo W của các MCB gắn vào khu đó (entity có device + zone = khu).
const areaW = zid => {
  const es = cfg.entities.filter(e => e.zone === zid && e.device && e.unit === 'W' && has(e));
  return es.length ? es.reduce((a, e) => a + (Number(state[e.id]) || 0), 0) : NaN;
};

/* ---------- Ngày / đêm: cảm biến ánh sáng ngoài trời nếu có, không thì theo giờ ---------- */
const SKY = ['auto', 'day', 'night'];
let skyMode = 'auto';
try { skyMode = SKY.includes(localStorage.getItem('hubd.sky')) ? localStorage.getItem('hubd.sky') : 'auto'; } catch (_) { /* bộ nhớ bị chặn */ }
function autoSky() {
  const lux = cfg.entities.find(e => e.unit === 'lx' && zone(e.zone)?.type === 'outdoor' && has(e));
  if (lux) return {day: Number(state[lux.id]) > 400, by: 'cảm biến ánh sáng'};
  const now = new Date(), h = now.getHours() + now.getMinutes() / 60;
  return {day: h >= 5.75 && h < 18, by: 'giờ trong ngày'};   // ponytail: giờ cố định 5:45–18:00, đổi sang giờ mặt trời mọc/lặn nếu cần
}
function applySky() {
  const a = autoSky(), day = skyMode === 'auto' ? a.day : skyMode === 'day';
  document.documentElement.dataset.sky = day ? 'day' : 'night';
  const b = $('#sky');
  b.innerHTML = `${icon(skyMode === 'auto' ? 'auto' : day ? 'sun' : 'moon')}${skyMode === 'auto' ? `Tự động: ${day ? 'ngày' : 'đêm'}` : day ? 'Ban ngày' : 'Ban đêm'}`;
  b.title = skyMode === 'auto' ? `Tự đổi theo ${a.by}. Chạm để chọn ban ngày.` : 'Chạm để đổi chế độ trời';
  return {day, by: skyMode === 'auto' ? a.by : 'chọn tay'};
}
$('#sky').addEventListener('click', () => {
  skyMode = SKY[(SKY.indexOf(skyMode) + 1) % SKY.length];
  try { localStorage.setItem('hubd.sky', skyMode); } catch (_) { /* bỏ qua */ }
  draw();
});

/* ---------- Thanh trên + phần chữ trên khung nhà ---------- */
function totalPower() {
  const l = num('deye.load');
  if (!isNaN(l)) return [l, 'Tải cả nhà, đo ở inverter'];
  const ms = cfg.entities.filter(e => e.device && e.unit === 'W' && has(e));
  return ms.length ? [ms.reduce((a, e) => a + (Number(state[e.id]) || 0), 0), 'Tổng các MCB có đo điện'] : [NaN, ''];
}
function renderTop() {
  const now = new Date();
  $('#when').innerHTML = `${now.toLocaleDateString('vi-VN', {weekday: 'long', day: 'numeric', month: 'numeric'})}` +
    `<b>${now.toLocaleTimeString('vi-VN', {hour: '2-digit', minute: '2-digit'})}</b>`;
  applySky();
}
function setConn(s) {
  const c = $('#conn');
  c.querySelector('.dot').className = 'dot ' + (s === 'ok' ? 'ok' : s === 'bad' ? 'bad' : '');
  c.querySelector('span').textContent = s === 'ok' ? 'Trực tuyến' : s === 'bad' ? 'Mất kết nối, đang thử lại' : 'Đang kết nối';
}
function renderHero() {
  const f = view.floor, z = view.room && zone(view.room);
  let h, p;
  if (view.page === 'energy') {
    const [w] = totalPower(), pv = num('deye.pv'), soc = num('jk.soc');
    h = 'Điện năng';
    p = isNaN(w) ? 'Chưa có số đo điện.' : `Nhà đang dùng ${fw(w)}${isNaN(pv) ? '' : `, mặt trời ${fw(pv)}`}${isNaN(soc) ? '' : `, pin ${nf.format(soc)}%`}.`;
  } else if (z) {
    const ds = devsIn(z.id), on = onIn(z.id).length;
    h = z.name; p = `Tầng ${f}` + (ds.length ? `, ${plural(ds.length, 'thiết bị')}${on ? `, ${on} đang bật` : ''}` : '');
  } else if (f) {
    const rs = rooms(f), litN = rs.filter(r => lit(r.id)).length;
    h = `Tầng ${f}`; p = `${plural(rs.length, 'phòng')}${litN ? `, ${litN} phòng sáng đèn` : ''}. Chạm vào một phòng để điều khiển.`;
  } else {
    const hr = new Date().getHours();
    h = hr < 11 ? 'Chào buổi sáng' : hr < 13 ? 'Chào buổi trưa' : hr < 18 ? 'Chào buổi chiều' : 'Chào buổi tối';
    const on = cfg.devices.filter(d => counted(d) && isOn(d)).length, litN = cfg.zones.filter(r => lit(r.id)).length;
    p = on ? `${plural(on, 'thiết bị')} đang bật${litN ? `, ${litN} phòng sáng đèn` : ''}.` : 'Mọi thiết bị trong nhà đang tắt.';
  }
  $('#heroText').innerHTML = `<h1>${esc(h)}</h1><p>${esc(p)}</p>`;
  $('#tabs').innerHTML = [0, ...floors().slice().reverse()].map(x =>
    `<button class="${x === f && !view.page ? 'sel' : ''}" data-go="${x || ''}">${x ? 'Tầng ' + x : 'Cả nhà'}</button>`).join('') +
    `<button class="${view.page === 'energy' ? 'sel' : ''}" data-go="dien">${icon('bolt')}Điện năng</button>`;
  // Chip số liệu ngoài trời + công suất (lux dùng cho ngày/đêm nên không hiện)
  const out = cfg.entities.filter(e => zone(e.zone)?.type === 'outdoor' && e.unit !== 'lx' && has(e)).slice(0, 2);
  const [w, src] = totalPower();
  $('#heroChips').innerHTML = out.map(e => `<span class="chip" title="${esc(e.name)}"><em>${esc(e.name)}</em><b>${val(e)}${esc(e.unit)}</b></span>`).join('') +
    (isNaN(w) ? '' : `<button class="chip" data-go="dien" title="${esc(src)}. Chạm để xem điện năng.">${icon('bolt')}<b>${fw(w)}</b></button>`);
}

/* ---------- Ảnh nhà: ảnh render thật + vùng chạm từng phòng (vùng khai báo trong scene.js) ---------- */
// Ban đêm ảnh nền tối đi; phòng đang bật đèn lấy lại đúng vùng đó từ ảnh gốc (clip-path), trông như đèn thật đang sáng.
const DEBUG_ZONES = new URLSearchParams(location.search).has('vung');
const clipOf = r => `inset(${r[1]}% ${(100 - r[0] - r[2]).toFixed(2)}% ${(100 - r[1] - r[3]).toFixed(2)}% ${r[0]}%)`;
const boxOf = r => `left:${r[0]}%;top:${r[1]}%;width:${r[2]}%;height:${r[3]}%`;
const isDay = () => document.documentElement.dataset.sky === 'day';
const hasLights = zid => devsIn(zid).some(d => kindOf(d) === 'light');
const dimRoom = zid => !isDay() && hasLights(zid) && !lit(zid);
function pic(p, {focus = 0, sel = '', mini = false, labels = true, floor = 0} = {}) {
  const heat = view.page === 'energy';
  const maxW = Math.max(1, ...cfg.zones.map(z => areaW(z.id)).filter(w => !isNaN(w)));
  const rs = Object.entries(p.rooms).filter(([id]) => zone(id));
  const rt = floor && routerOf(floor);
  return `<div class="pic${mini ? ' mini' : ''}${heat ? ' heat' : ''}" style="aspect-ratio:${p.w}/${p.h}">
    <img class="base" src="${p.src}" alt="" draggable="false">
    ${heat ? '' : rs.filter(([id]) => lit(id)).map(([, r]) => `<img class="glow" src="${p.src}" alt="" draggable="false" style="clip-path:${clipOf(r)}">`).join('')}
    ${focus && p.floors ? Object.entries(p.floors).filter(([f]) => +f !== focus).map(([, [a, b]]) => `<i class="band" style="top:${a}%;height:${b - a}%"></i>`).join('') : ''}
    ${rs.map(([id, r]) => {
      const z = zone(id), on = onIn(id).length, aw = areaW(id), bad = devsIn(id).some(offline);
      const sub = heat ? fw(aw) : bad ? 'mất kết nối' : on ? `${on} đang bật` : '';
      return `<button class="hs${sel === id ? ' sel' : ''}${!heat && lit(id) ? ' lit' : ''}" style="${boxOf(r)};--heat:${isNaN(aw) ? 0 : (aw / maxW).toFixed(2)}"
        data-go="${z.floor}/${esc(id)}" aria-label="${esc(z.name)}">${labels ? `<span class="hl"><b>${esc(z.name)}</b>${sub ? `<small>${esc(sub)}</small>` : ''}</span>` : ''}</button>`;
    }).join('')}
    ${rt && p.wifi ? `<span class="wf${offline(rt) ? ' bad' : ''}" style="left:${p.wifi[0]}%;top:${p.wifi[1]}%" title="${esc(rt.name)}">${icon('router')}</span>` : ''}
  </div>`;
}
// Phòng chưa có ảnh riêng: phóng vùng phòng đó trên ảnh tầng cho vừa khung 16:9.
function roomPic(f, id) {
  const src = SCENE.photos[id], fp = SCENE.floors[f], r = fp && fp.rooms[id];
  if (src) return `<div class="pic photo${dimRoom(id) ? ' off' : ''}"><img src="${src}" alt="" draggable="false"></div>`;
  if (!r) return '';
  return `<div class="pic zoom${dimRoom(id) ? ' off' : ''}"><img src="${fp.src}" alt="" draggable="false"
    style="width:${(10000 / r[2]).toFixed(2)}%;transform:translate(-${r[0]}%,-${(r[1] + r[3] / 2).toFixed(2)}%)"></div>`;
}
function floorCards() {
  return `<div class="fcards">${floors().slice().reverse().map(f => {
    const rs = rooms(f), litN = rs.filter(z => lit(z.id)).length, rt = routerOf(f);
    const cams = cfg.devices.filter(d => kindOf(d) === 'camera' && floorOf(d) === f).length;
    const ws = rs.map(z => areaW(z.id)).filter(w => !isNaN(w)), w = ws.reduce((a, b) => a + b, 0);
    return `<button class="fcard" data-go="${f}"><b>Tầng ${f}</b><span>${litN ? `${litN} phòng sáng đèn` : 'Không phòng nào sáng đèn'}</span>
      <span class="fx">${rt ? `<span class="${offline(rt) ? 'bad' : 'ok'}">${icon('router')}</span>` : ''}${cams ? `<span>${icon('camera')} ${cams}</span>` : ''}${ws.length ? `<span>${icon('bolt')} ${fw(w)}</span>` : ''}</span></button>`;
  }).join('')}</div>`;
}
function renderScene() {
  const s = $('#house'), f = view.floor, fp = SCENE.floors[f];
  s.classList.toggle('debug', DEBUG_ZONES);
  s.innerHTML = view.room && fp ? roomPic(f, view.room) + pic(fp, {sel: view.room, floor: f})
    : f && fp ? pic(fp, {floor: f}) + pic(SCENE.house, {focus: f, mini: true, labels: false})
    : pic(SCENE.house) + floorCards();
}

/* ---------- Thẻ thiết bị ---------- */
const lockIc = d => d.protected ? icon('lock') : '';
function devTile(d) {
  const k = kindOf(d);
  if (RICH.has(k)) return richCard(d);
  if (k === 'breaker') return brk(d);
  const on = isOn(d), lost = offline(d);
  if (PASSIVE.has(k)) {
    return `<div class="tile t-infra${lost ? ' lost' : ''}"><span class="t-ic">${icon(k)}</span><span class="t-name">${esc(d.name)}</span>
      <span class="t-st">${lost ? 'Mất kết nối' : k === 'camera' ? 'Đang ghi hình' : 'Hoạt động'}</span></div>`;
  }
  const st = lost ? 'Mất kết nối' : !known(d) ? 'Chưa rõ trạng thái' : on ? 'Đang bật' : 'Đang tắt';
  return `<button class="tile t-${k}${on ? ' on' : ''}${lost ? ' lost' : ''}${pending.has(d.id) ? ' pend' : ''}" data-dev="${esc(d.id)}"
      ${lost ? 'disabled' : ''} aria-pressed="${on}">
    <span class="t-ic">${icon(k)}</span><span class="sw"></span>
    <span class="t-name">${esc(d.name)}${lockIc(d)}</span><span class="t-st">${st}</span>${reads(d)}</button>`;
}
function reads(d) {
  const r = readingsOf(d).filter(has);
  return r.length ? `<span class="read">${r.map(e => `<span>${val(e)} ${esc(e.unit)}</span>`).join('')}</span>` : '';
}
function brk(d) {
  const on = isOn(d), lost = offline(d);
  const st = lost ? 'Mất kết nối' : !known(d) ? 'Chưa rõ' : on ? 'Đang đóng điện' : 'Đang ngắt';
  return `<button class="brk${on ? ' on' : ''}${lost ? ' lost' : ''}${pending.has(d.id) ? ' pend' : ''}" data-dev="${esc(d.id)}"
      ${lost ? 'disabled' : ''} aria-pressed="${on}">
    <span class="lever"><i></i></span><span class="t-name">${esc(d.name)}${lockIc(d)}</span><span class="t-st">${st}</span>${reads(d)}</button>`;
}
function panelItem(d) {
  if (kindOf(d) === 'breaker') return brk(d);
  const lost = offline(d);
  return `<div class="brk net${lost ? ' lost' : ''}"><span class="t-ic">${icon(kindOf(d))}</span>
    <span class="t-name">${esc(d.name)}</span><span class="t-st ${lost ? '' : 'ok'}">${lost ? 'Mất kết nối' : 'Hoạt động'}</span></div>`;
}
// Thẻ có nút riêng: TV, loa, quạt, máy lạnh (hồng ngoại) và robot lau nhà.
const btn = (id, code, label, inner = label, cls = 'rb') =>
  `<button class="${cls}" data-act="${esc(id)}" data-code="${code}" data-label="${label}" aria-label="${label}">${inner}</button>`;
const xs = (d, key) => state[`x:${d.id}:${key}`];   // trạng thái giả định theo lệnh gần nhất
function richCard(d) {
  const k = kindOf(d), b = (...a) => btn(d.id, ...a);
  const rocker = (label, dn, up) => `<span class="rocker">${b(dn, label + ' giảm', '−')}<span>${label}</span>${b(up, label + ' tăng', '+')}</span>`;
  const seg = (opts, cur) => `<span class="seg">${opts.map(([code, label]) =>
    b(code, label, label, 'rb' + (cur === code.split('_')[1] ? ' sel' : ''))).join('')}</span>`;
  let on = isOn(d), st, pw = b('power', 'Nguồn', icon('power'), 'rb pw'), body = '', extra = '';
  if (k === 'vacuum') {
    const vs = xs(d, 'status') || 'docked';
    on = vs === 'cleaning'; st = VAC[vs] || vs; pw = '';
    extra = xs(d, 'battery') ? `<span class="batt">${icon('battery')} ${esc(xs(d, 'battery'))}%</span>` : '';
    body = b('start', 'Bắt đầu lau', `${icon('play')}Lau`) + b('pause', 'Tạm dừng', `${icon('pause')}Dừng`) + b('dock', 'Về dock', `${icon('dock')}Về dock`);
  } else {
    st = `${on ? 'Bật' : 'Tắt'} theo lệnh gần nhất`;
    if (k === 'tv') body = rocker('Âm lượng', 'vol_down', 'vol_up') + b('mute', 'Tắt tiếng') + b('input', 'Nguồn vào');
    if (k === 'speaker') body = b('play', xs(d, 'play') === '1' ? 'Dừng' : 'Phát', icon(xs(d, 'play') === '1' ? 'pause' : 'play')) + rocker('Âm lượng', 'vol_down', 'vol_up');
    if (k === 'fan') body = seg([['speed_1', 'Nhẹ'], ['speed_2', 'Vừa'], ['speed_3', 'Mạnh']], xs(d, 'speed')) + b('swing', 'Xoay');
    if (k === 'ac') body = `<span class="temp">${b('temp_down', 'Giảm nhiệt độ', '−')}<b>${esc(xs(d, 'temp') || '26')}°</b>${b('temp_up', 'Tăng nhiệt độ', '+')}</span>` +
      seg([['mode_cool', 'Mát'], ['mode_dry', 'Khô'], ['mode_fan', 'Quạt']], xs(d, 'mode') || 'cool');
  }
  return `<div class="tile ir t-${k}${on ? ' on' : ''}"${IR.has(k) ? ' title="Điều khiển hồng ngoại: không đọc được trạng thái thật của thiết bị"' : ''}>
    <div class="ir-head"><span class="t-ic">${icon(k)}</span><div><div class="t-name">${esc(d.name)}</div><div class="t-st">${esc(st)}</div></div>${extra}${pw}</div>
    <div class="remote">${body}</div></div>`;
}

/* ---------- Thẻ phòng, điện năng ---------- */
function roomCard(z) {
  const ds = devsIn(z.id), on = onIn(z.id), ss = sensorsIn(z.id), t = sensorBy(z.id, '°C');
  const lights = ds.filter(d => kindOf(d) === 'light' && isOn(d) && !d.protected);
  const aw = areaW(z.id);
  const sub = (ds.length ? `${plural(ds.length, 'thiết bị')}${on.length ? `, ${on.length} đang bật` : ''}` : ss.length ? plural(ss.length, 'cảm biến') : 'Chưa có thiết bị') +
    (isNaN(aw) ? '' : `, đang dùng ${fw(aw)}`);
  return `<div class="rcard${lit(z.id) ? ' lit' : ''}${z.type === 'outdoor' ? ' out' : ''}" data-go="${z.floor}/${esc(z.id)}" role="link" tabindex="0">
    <div class="rc-top"><span class="rc-name">${esc(z.name)}</span>${t ? `<span class="rc-temp">${val(t)}°</span>` : ''}</div>
    <span class="rc-sub">${sub}</span>
    <div class="rc-foot"><span class="rc-icons">${ds.filter(counted).slice(0, 8).map(d => icon(kindOf(d), isOn(d) ? 'on' : '')).join('')}</span>
      ${lights.length ? `<button class="offbtn" data-off="${esc(z.id)}">Tắt ${lights.length > 1 ? plural(lights.length, 'đèn') : 'đèn'}</button>` : ''}</div>
  </div>`;
}
/* ---------- Điện năng: sơ đồ dòng điện, tiêu thụ theo khu vực, tổng trong ngày ---------- */
// Đường chảy đi từ điểm đầu tới điểm cuối của path; rev = chảy ngược (bán lên lưới, sạc pin).
function flowMap(mini = false) {
  const pv = num('deye.pv'), grid = num('deye.grid'), load = num('deye.load'), soc = num('jk.soc'), bat = num('jk.power');
  if ([pv, grid, load, soc, bat].every(isNaN)) return '';
  const fl = (d, w, c, rev) => `<path class="fl${isNaN(w) || Math.abs(w) < 5 ? ' idle' : rev ? ' rev' : ''}" d="${d}" style="--c:${c}"/>`;
  const node = (x, y, cls, ic, value, label) =>
    `<div class="fnode ${cls}" style="left:${x}%;top:${y}%"><span class="fi">${icon(ic)}</span><b>${value}</b><em>${label}</em></div>`;
  const gridLbl = isNaN(grid) ? 'Điện lưới' : grid > 5 ? 'Đang mua từ lưới' : grid < -5 ? 'Đang bán lên lưới' : 'Lưới, không dùng';
  const batLbl = isNaN(bat) ? 'Pin lưu trữ' : bat > 5 ? `Đang sạc ${fw(bat)}` : bat < -5 ? `Đang xả ${fw(bat)}` : 'Pin nghỉ';
  return `<div class="flowmap${mini ? ' mini' : ''}"><svg viewBox="0 0 100 62.5" aria-hidden="true">
      ${fl('M16 13 C36 13 50 18 50 31', pv, 'var(--lamp)', false)}
      ${fl('M84 13 C64 13 50 18 50 31', grid, 'var(--cool)', grid < 0)}
      ${fl('M16 50 C36 50 50 44 50 31', bat, 'var(--ok)', bat > 0)}
      ${fl('M50 31 C50 44 64 50 84 50', load, 'var(--ink)', false)}</svg>
    ${node(16, 21, 'n-sun', 'sun', fw(pv), 'Mặt trời')}
    ${node(84, 21, 'n-grid', 'grid', fw(grid), gridLbl)}
    ${node(50, 50, 'n-inv', 'bolt', 'Deye', 'Inverter')}
    ${node(16, 80, 'n-bat', 'battery', isNaN(soc) ? '—' : `${nf.format(soc)}%`, batLbl)}
    ${node(84, 80, 'n-load', 'home', fw(load), 'Nhà đang dùng')}
  </div>`;
}
function donut(title, total, segs, ic) {
  const C = 2 * Math.PI * 40;
  let off = 0;
  const arcs = segs.map(([, v, c]) => {
    const len = total > 0 ? Math.max(0, v) / total * C : 0;
    const s = `<circle r="40" cx="50" cy="50" fill="none" stroke="${c}" stroke-width="11" stroke-dasharray="${len.toFixed(2)} ${(C - len).toFixed(2)}" stroke-dashoffset="${(-off).toFixed(2)}"/>`;
    off += len;
    return s;
  }).join('');
  return `<div class="donut"><div class="dz"><svg viewBox="0 0 100 100" aria-hidden="true">
      <circle r="40" cx="50" cy="50" fill="none" stroke="rgba(203,224,245,.08)" stroke-width="11"/><g transform="rotate(-90 50 50)">${arcs}</g></svg>
      <div class="dc">${icon(ic)}<b>${nf.format(total)}</b><small>kWh</small></div></div>
    <div class="dl"><h3>${title}</h3>${segs.map(([l, v, c]) => `<div class="dli"><i style="background:${c}"></i><span>${l}</span>
      <b>${nf.format(Math.max(0, v))}</b><small>${total > 0 ? Math.round(Math.max(0, v) / total * 100) : 0}%</small></div>`).join('')}</div></div>`;
}
const areaRows = () => cfg.zones.filter(z => z.type !== 'panel').map(z => [z, areaW(z.id)])
  .filter(([, w]) => !isNaN(w)).sort((a, b) => b[1] - a[1]);
function areaBars(rows, total, limit = 99) {
  const max = Math.max(1, ...rows.map(([, w]) => w));
  const bar = (name, sub, w, go) => `<${go ? `button data-go="${go}"` : 'div'} class="bar"><span class="bn">${name}<small>${sub}</small></span>
    <span class="bt"><i style="width:${Math.min(100, w / max * 100).toFixed(1)}%"></i></span><b>${fw(w)}</b>
    <small class="bp">${total > 0 ? Math.round(w / total * 100) : 0}%</small></${go ? 'button' : 'div'}>`;
  const measured = rows.reduce((a, [, w]) => a + w, 0), rest = num('deye.load') - measured;
  return `<div class="bars">${rows.slice(0, limit).map(([z, w]) => bar(esc(z.name), `Tầng ${z.floor}`, w, `${z.floor}/${esc(z.id)}`)).join('')}
    ${limit >= rows.length && rest > 5 ? bar('Chưa đo riêng', 'Chênh lệch giữa inverter và các MCB', rest, '') : ''}</div>`;
}
function energyView() {
  const [total] = totalPower(), rows = areaRows(), fm = flowMap(false), t = id => num('deye.' + id);
  const donuts = [];
  const pvT = t('pv_today'), loadT = t('load_today');
  if (!isNaN(pvT)) {
    const ch = t('charge_today') || 0, se = t('sell_today') || 0;
    donuts.push(donut('Điện mặt trời', pvT, [['Sạc pin', ch, 'var(--ok)'], ['Nhà dùng', pvT - ch - se, 'var(--lamp)'], ['Bán lên lưới', se, 'var(--cool)']], 'sun'));
  }
  if (!isNaN(loadT)) {
    const dc = t('discharge_today') || 0, by = t('buy_today') || 0;
    donuts.push(donut('Nhà tiêu thụ', loadT, [['Từ pin', dc, 'var(--ok)'], ['Từ mặt trời', loadT - dc - by, 'var(--lamp)'], ['Mua từ lưới', by, 'var(--cool)']], 'home'));
  }
  return `<div class="rowhead"><button class="back" data-go="">${icon('back')}Cả nhà</button></div>
    <section class="card"><h2>Dòng điện lúc này</h2>${fm || '<p class="empty">Chưa có dữ liệu từ inverter Deye và pin JK. Sơ đồ sẽ hiện khi lắp node RS485 ở tum kỹ thuật.</p>'}</section>
    <section class="card"><h2>Tiêu thụ theo khu vực</h2>${rows.length ? areaBars(rows, total) : '<p class="empty">Chưa có MCB đo điện nào được gán vào khu vực.</p>'}</section>
    ${donuts.length ? `<section class="card"><h2>Hôm nay</h2><div class="donuts">${donuts.join('')}</div></section>` : ''}`;
}

/* ---------- Widget trang "Cả nhà": kéo thả đổi chỗ, đổi cỡ, ẩn; lưu trên thiết bị này ---------- */
const WNAME = {scenes: 'Cảnh', energy: 'Điện năng', rooms: 'Phòng đang dùng', climate: 'Khí hậu trong nhà',
  outdoor: 'Ngoài trời', cameras: 'Camera giám sát', network: 'Wi-Fi các tầng', robots: 'Robot lau nhà', alerts: 'Cần chú ý'};
const WDEF = [['scenes', 2], ['energy', 2], ['rooms', 2], ['climate', 1], ['outdoor', 1], ['cameras', 1], ['network', 1],
  ['robots', 1], ['alerts', 1]];
let layout = loadLayout();
function loadLayout() {
  let saved = [];
  try { saved = JSON.parse(localStorage.getItem('hubd.widgets') || '[]'); } catch (_) { saved = []; }
  const def = new Map(WDEF.map(([id, span]) => [id, {id, span, hidden: false}]));
  const out = (Array.isArray(saved) ? saved : []).filter(w => w && def.has(w.id))
    .map(w => ({id: w.id, span: w.span === 1 ? 1 : 2, hidden: !!w.hidden}));
  for (const [id] of WDEF) if (!out.some(w => w.id === id)) out.push(def.get(id));   // widget mới thêm sau
  return out;
}
const saveLayout = () => { try { localStorage.setItem('hubd.widgets', JSON.stringify(layout)); } catch (_) { /* bỏ qua */ } };

const W = {   // mỗi widget trả nội dung, hoặc '' khi chưa có dữ liệu (lúc đó tự ẩn)
  scenes: () => (cfg.scenes || []).length ? `<div class="scenes">${cfg.scenes.map((s, i) =>
    `<button class="scene" data-scene="${i}">${icon(s.icon || 'auto')}<span>${esc(s.name)}</span><small>${esc(s.note || '')}</small></button>`).join('')}</div>` : '',
  energy: () => {
    const fm = flowMap(true), rows = areaRows();
    if (!fm && !rows.length) return '';
    return `${fm}${rows.length ? `<h3 class="sub">Khu dùng nhiều nhất</h3>${areaBars(rows, totalPower()[0], 3)}` : ''}
      <button class="more" data-go="dien">Xem chi tiết điện năng</button>`;
  },
  rooms: () => {
    const active = cfg.zones.filter(z => z.type !== 'panel' && onIn(z.id).length).sort((a, b) => onIn(b.id).length - onIn(a.id).length);
    return active.length ? `<div class="grid2">${active.map(roomCard).join('')}</div>` : '<p class="empty">Không phòng nào đang bật thiết bị.</p>';
  },
  climate: () => {
    const rs = cfg.zones.filter(z => z.type !== 'outdoor' && sensorBy(z.id, '°C'));
    return rs.length ? `<div class="list">${rs.map(z => { const hm = sensorBy(z.id, '%');
      return `<button class="lrow" data-go="${z.floor}/${esc(z.id)}"><span>${esc(z.name)}</span><small>${hm ? `${val(hm)}%` : ''}</small><b>${val(sensorBy(z.id, '°C'))}°</b></button>`; }).join('')}</div>` : '';
  },
  outdoor: () => {
    const es = cfg.entities.filter(e => zone(e.zone)?.type === 'outdoor' && has(e));
    if (!es.length) return '';
    const sky = applySky();
    return `<div class="list">${es.map(e => `<div class="lrow"><span>${esc(e.name)}</span><small></small><b>${val(e)}${e.unit ? ' ' + esc(e.unit) : ''}</b></div>`).join('')}</div>
      <p class="note">Giao diện đang ở chế độ ${sky.day ? 'ban ngày' : 'ban đêm'}, theo ${sky.by}.</p>`;
  },
  cameras: () => {
    const cs = cfg.devices.filter(d => kindOf(d) === 'camera'), lost = cs.filter(offline);
    return cs.length ? `<div class="big"><b>${cs.length - lost.length}</b><span>/ ${plural(cs.length, 'camera')} đang ghi hình</span></div>
      ${lost.length ? `<div class="list">${lost.map(d => `<button class="lrow" data-go="${where(d)}"><span>${esc(d.name)}<small>${esc(zone(d.zone)?.name || '')}</small></span><i class="dot bad"></i></button>`).join('')}</div>`
        : '<p class="note">Đầu ghi Hikvision ở tum kỹ thuật. Xem hình trực tiếp sẽ có khi hub kết nối đầu ghi.</p>'}` : '';
  },
  network: () => {
    const rs = cfg.devices.filter(d => kindOf(d) === 'router').sort((a, b) => floorOf(a) - floorOf(b));
    return rs.length ? `<div class="list">${rs.map(d => `<button class="lrow" data-go="${floorOf(d) || ''}"><span>${esc(d.name)}</span>
      <small>${offline(d) ? 'Mất kết nối' : 'Hoạt động'}</small><i class="dot ${offline(d) ? 'bad' : 'ok'}"></i></button>`).join('')}</div>` : '';
  },
  robots: () => {
    const vs = cfg.devices.filter(d => kindOf(d) === 'vacuum');
    return vs.length ? `<div class="list">${vs.map(d => { const s = xs(d, 'status') || 'docked';
      return `<div class="lrow robot"><span>${esc(zone(d.zone)?.name || d.name)}<small>${VAC[s] || esc(s)}${xs(d, 'battery') ? `, pin ${esc(xs(d, 'battery'))}%` : ''}</small></span>
        ${s === 'cleaning' ? btn(d.id, 'dock', 'Về dock', icon('dock'), 'rb') : btn(d.id, 'start', 'Bắt đầu lau', icon('play'), 'rb')}</div>`; }).join('')}</div>` : '';
  },
  alerts: () => {
    const lost = cfg.devices.filter(offline);
    return lost.length ? `<div class="alerts">${lost.map(d => `<button class="alert" data-go="${where(d)}">${icon('warn')}
      <span>${esc(d.name)}<small>${esc(zone(d.zone)?.name || 'Chưa gán phòng')}, mất kết nối</small></span></button>`).join('')}</div>`
      : '<p class="empty">Mọi thiết bị đang kết nối.</p>';
  },
};
function homeView() {
  const items = layout.map(w => {
    const body = W[w.id]();
    if (!editing && (w.hidden || !body)) return '';
    const tools = editing ? `<span class="w-tools">
        <button class="wt" data-wsize="${w.id}">${w.span === 2 ? 'Hẹp' : 'Rộng'}</button>
        <button class="wt" data-whide="${w.id}">${w.hidden ? 'Hiện' : 'Ẩn'}</button>
        <span class="grip" data-grip role="button" aria-label="Giữ và kéo để đổi chỗ ${WNAME[w.id]}">${icon('grip')}</span></span>` : '';
    return `<section class="widget${w.span === 2 ? ' s2' : ''}${w.hidden ? ' off' : ''}${w.id === 'rooms' ? ' bare' : ''}" data-w="${w.id}">
      <div class="w-head"><h2>${WNAME[w.id]}</h2>${tools}</div>${body || '<p class="empty">Chưa có dữ liệu cho mục này.</p>'}</section>`;
  }).join('');
  const unassigned = cfg.devices.filter(d => !zone(d.zone));
  return `<div class="rowhead"><span class="rh">${editing ? 'Giữ biểu tượng chấm để kéo đổi chỗ.' : ''}</span><span class="sp"></span>
      ${editing ? '<button class="back" data-wreset>Về mặc định</button>' : ''}
      <button class="back${editing ? ' primary' : ''}" data-edit>${editing ? 'Xong' : 'Sắp xếp'}</button></div>
    <div class="widgets${editing ? ' editing' : ''}" id="widgets">${items}</div>
    ${unassigned.length ? `<section class="card" style="margin-top:14px"><h2>Chưa gán phòng</h2><div class="devs">${unassigned.map(devTile).join('')}</div></section>` : ''}
    ${DEMO ? '' : '<p class="note"><a href="/?demo" style="text-decoration:underline">Xem giao diện mẫu với đủ thiết bị</a></p>'}`;
}

/* Kéo thả bằng Pointer Events (chạy được với cảm ứng iPad; HTML5 drag-and-drop thì không). */
document.addEventListener('pointerdown', e => {
  const grip = editing && e.target.closest('[data-grip]');
  if (!grip) return;
  e.preventDefault();
  const el = grip.closest('.widget'), r = el.getBoundingClientRect();
  const ph = document.createElement('div');
  ph.className = 'widget ph' + (el.classList.contains('s2') ? ' s2' : '');
  ph.style.height = `${r.height}px`;
  el.after(ph);
  Object.assign(el.style, {position: 'fixed', left: `${r.left}px`, top: `${r.top}px`, width: `${r.width}px`, height: `${r.height}px`});
  el.classList.add('dragging');
  drag = {el, ph, dx: e.clientX - r.left, dy: e.clientY - r.top};
});
document.addEventListener('pointermove', e => {
  if (!drag) return;
  drag.el.style.left = `${e.clientX - drag.dx}px`;
  drag.el.style.top = `${e.clientY - drag.dy}px`;
  const st = $('#stage'), sr = st.getBoundingClientRect();   // tự cuộn khi kéo sát mép
  if (e.clientY < sr.top + 60) st.scrollTop -= 14; else if (e.clientY > sr.bottom - 60) st.scrollTop += 14;
  const over = document.elementFromPoint(e.clientX, e.clientY)?.closest('#widgets .widget:not(.ph):not(.dragging)');
  if (!over) return;
  const r = over.getBoundingClientRect();
  const after = over.classList.contains('s2') ? e.clientY > r.top + r.height / 2 : e.clientX > r.left + r.width / 2;
  if (after ? over.nextElementSibling !== drag.ph : over.previousElementSibling !== drag.ph) over[after ? 'after' : 'before'](drag.ph);
});
function endDrag() {
  if (!drag) return;
  const {el, ph} = drag;
  drag = null;
  ph.replaceWith(el);
  el.removeAttribute('style');
  el.classList.remove('dragging');
  const order = [...document.querySelectorAll('#widgets .widget')].map(w => w.dataset.w);
  layout.sort((a, b) => (order.indexOf(a.id) + 1 || 99) - (order.indexOf(b.id) + 1 || 99));
  saveLayout();
  draw();
}
addEventListener('pointerup', endDrag);
addEventListener('pointercancel', endDrag);

/* ---------- Màn hình tầng và phòng ---------- */
function floorView(f) {
  const rt = routerOf(f);
  return `<div class="rowhead"><button class="back" data-go="">${icon('back')}Cả nhà</button><span class="sp"></span>
      ${rt ? `<span class="chip${offline(rt) ? ' bad' : ''}">${icon('router')}Wi-Fi tầng ${f}: ${offline(rt) ? 'mất kết nối' : 'hoạt động'}</span>` : ''}</div>
    <div class="grid2">${rooms(f).map(roomCard).join('')}</div>
    ${panels(f).map(p => `<section class="card" style="margin-top:14px"><h2>${esc(p.name)}</h2>${devsIn(p.id).length
      ? `<div class="brks">${devsIn(p.id).map(panelItem).join('')}</div>` : '<p class="empty">Chưa có thiết bị nào được gán vào đây.</p>'}</section>`).join('')}`;
}
function roomView(f, zid) {
  const z = zone(zid);
  if (!z) return homeView();
  const ds = devsIn(zid), ss = sensorsIn(zid);
  const lights = ds.filter(d => kindOf(d) === 'light' && isOn(d) && !d.protected);
  const eb = cfg.entities.some(e => ENERGY_IDS.has(e.id) && e.zone === zid) ? flowMap(false) : '';
  const aw = areaW(zid);
  // Ô bật tắt trước, rồi camera/hạ tầng, thẻ có nút điều khiển sau cùng để lưới không bị lỗ
  const rank = d => RICH.has(kindOf(d)) ? 2 : PASSIVE.has(kindOf(d)) ? 1 : 0;
  const order = [0, 1, 2].flatMap(r => ds.filter(d => rank(d) === r));
  return `<div class="rowhead"><button class="back" data-go="${f}">${icon('back')}Tầng ${f}</button><span class="sp"></span>
      ${lights.length ? `<button class="offbtn" data-off="${esc(zid)}">Tắt ${lights.length > 1 ? plural(lights.length, 'đèn') : 'đèn'}</button>` : ''}</div>
    ${eb ? `<section class="card" style="margin-bottom:14px"><h2>Dòng điện lúc này</h2>${eb}<button class="more" data-go="dien">Xem chi tiết điện năng</button></section>` : ''}
    ${ss.length || !isNaN(aw) ? `<div class="sensors">${isNaN(aw) ? '' : `<button class="sn pw" data-go="dien"><b>${fw(aw)}</b><em>Điện đang dùng</em></button>`}${ss.map(e =>
      `<div class="sn"><b>${val(e)}</b><span>${esc(e.unit)}</span><em>${esc(e.name)}</em></div>`).join('')}</div>` : ''}
    ${ds.length ? `<div class="devs">${order.map(devTile).join('')}</div>` : ss.length || eb ? ''
      : `<p class="empty">Phòng này chưa có thiết bị điều khiển được. Khi thêm thiết bị vào hub, gán nó vào “${esc(z.name)}” để hiện ở đây.</p>`}`;
}
function renderStage() {
  const f = view.floor;
  $('#stage').innerHTML = view.page === 'energy' ? energyView() : view.room ? roomView(f, view.room) : f ? floorView(f) : homeView();
}
function draw() { if (drag) return; renderTop(); renderHero(); renderScene(); renderStage(); }
let raf = 0;
const soon = () => { raf = raf || requestAnimationFrame(() => { raf = 0; draw(); }); };

/* ---------- Điều hướng: hướng chuyển động theo việc lên/xuống tầng hay vào/ra phòng ---------- */
function parseHash() {
  if (location.hash.startsWith('#/dien')) return {floor: 0, room: '', page: 'energy'};
  const m = location.hash.match(/^#\/t(\d+)(?:\/([\w-]+))?/);
  const f = m ? +m[1] : 0, r = m && m[2] || '';
  if (!floors().includes(f)) return {floor: 0, room: '', page: ''};
  return {floor: f, room: rooms(f).some(z => z.id === r) ? r : '', page: ''};
}
function animate(el, cls) {
  el.classList.remove('go-up', 'go-down', 'go-in', 'go-out', 'go-side', 'swap');
  void el.offsetWidth;   // chạy lại animation
  el.classList.add(cls);
}
function go(f, room = '', from = null, page = '') {
  if (f === view.floor && room === view.room && page === view.page) return;
  const depth = (ff, r, p) => r ? 2 : ff || p ? 1 : 0, a = depth(view.floor, view.room, view.page), b = depth(f, room, page);
  const dir = f && view.floor && f !== view.floor ? (f > view.floor ? 'up' : 'down') : b > a ? 'in' : b < a ? 'out' : 'side';
  const st = $('#stage');
  if (from && st.contains(from)) {   // phóng to từ đúng chỗ vừa chạm
    const r = from.getBoundingClientRect(), s = st.getBoundingClientRect();
    st.style.setProperty('--ox', `${r.left + r.width / 2 - s.left}px`);
    st.style.setProperty('--oy', `${r.top + r.height / 2 - s.top}px`);
  } else { st.style.removeProperty('--ox'); st.style.removeProperty('--oy'); }
  view = {floor: f, room, page};
  editing = false;
  const h = '#/' + (page === 'energy' ? 'dien' : f ? `t${f}` + (room ? `/${room}` : '') : '');
  if (location.hash !== h) history.pushState(null, '', h);
  draw();
  st.scrollTop = 0;
  animate(st, 'go-' + dir);
  animate($('#heroText'), 'swap');
  animate($('#house'), 'go-' + dir);
}
addEventListener('popstate', () => { const v = parseHash(); go(v.floor, v.room, null, v.page); });

/* ---------- Lệnh ---------- */
let tt = 0;
function toast(msg) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(tt);
  tt = setTimeout(() => t.classList.remove('show'), 2600);
}
const post = body => api('/api/cmd', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
async function setDev(d, to) {
  if (DEMO) { state['dev:' + d.id] = to.toUpperCase(); return true; }
  pending.add(d.id);
  const r = await post({device: d.id, to});
  setTimeout(() => { if (pending.delete(d.id)) soon(); }, r.ok ? 5000 : 0);   // trạng thái thật về qua SSE
  return r.ok;
}
// Modal xác nhận thay cho confirm() của trình duyệt. Trả về true khi chọn nút hành động.
function ask({title, text, ok, danger = false}) {
  return new Promise(res => {
    const m = $('#modal');
    m.innerHTML = `<div class="m-box"><h3>${esc(title)}</h3><p>${esc(text)}</p>
      <div class="m-act"><button value="0" class="m-btn">Huỷ</button><button value="1" class="m-btn ${danger ? 'danger' : 'ok'}">${esc(ok)}</button></div></div>`;
    m.onclick = e => { const b = e.target.closest('button'); if (b) m.close(b.value); else if (e.target === m) m.close('0'); };
    m.onclose = () => res(m.returnValue === '1');
    m.returnValue = '';
    m.showModal();
    m.querySelector('.m-btn').focus();   // mặc định vào "Huỷ" để Enter không tắt nhầm
  });
}
async function toggle(id) {
  const d = cfg.devices.find(x => x.id === id);
  if (!d || offline(d)) return;
  const to = isOn(d) ? 'off' : 'on';
  if (d.protected && !(await ask({
    title: `${to === 'off' ? 'Tắt' : 'Bật'} ${d.name}?`,
    text: `Thiết bị này được bảo vệ để tránh bấm nhầm.${kindOf(d) === 'breaker' && to === 'off' ? ' Tắt CB sẽ ngắt điện cả khu vực.' : ''}`,
    ok: to === 'off' ? 'Tắt' : 'Bật', danger: to === 'off'}))) return;
  const p = setDev(d, to);
  draw();
  if (!(await p)) toast(`Không gửi được lệnh tới ${d.name}`);
  draw();
}
async function lightsOff(zid) {
  const ds = devsIn(zid).filter(d => kindOf(d) === 'light' && isOn(d) && !d.protected && !offline(d));
  const ok = await Promise.all(ds.map(d => setDev(d, 'off')));
  draw();
  const bad = ok.filter(x => !x).length;
  toast(bad ? `Có ${bad} đèn không nhận lệnh` : `Đã tắt ${plural(ds.length, 'đèn')} ở ${zone(zid).name}`);
}
async function act(id, code, label, quiet = false) {
  const d = cfg.devices.find(x => x.id === id);
  if (!d) return false;
  if (!DEMO) {
    // Lệnh hồng ngoại / robot đi qua hub khi đã lắp module phát hồng ngoại hoặc nối robot vào hub.
    const r = await post({device: id, to: code});
    if (!r.ok) { if (!quiet) toast(`Hub chưa gửi được lệnh tới ${d.name}`); return false; }
  }
  const k = key => `x:${id}:${key}`;
  if (code === 'power') state['dev:' + id] = isOn(d) ? 'OFF' : 'ON';
  else if (code.startsWith('speed_')) { state[k('speed')] = code.slice(6); state['dev:' + id] = 'ON'; }
  else if (code.startsWith('mode_')) state[k('mode')] = code.slice(5);
  else if (code === 'temp_up' || code === 'temp_down') {
    state[k('temp')] = String(Math.min(30, Math.max(16, (Number(state[k('temp')]) || 26) + (code === 'temp_up' ? 1 : -1))));
  } else if (code === 'play') state[k('play')] = state[k('play')] === '1' ? '0' : '1';
  else if (code === 'start') state[k('status')] = 'cleaning';
  else if (code === 'pause') state[k('status')] = 'paused';
  else if (code === 'dock') state[k('status')] = 'returning';
  if (!quiet) { draw(); toast(`Đã gửi “${label}” tới ${d.name}`); }
  return true;
}
// Cảnh: chuỗi bước {d, to: on|off} hoặc {d, act}. Thiết bị hồng ngoại chỉ bấm nguồn khi trạng thái giả định khác.
async function runScene(i) {
  const sc = cfg.scenes[i];
  let n = 0, bad = 0;
  for (const s of sc.steps || []) {
    const d = cfg.devices.find(x => x.id === s.d);
    if (!d || offline(d)) continue;
    let ok = true;
    if (s.act) ok = await act(d.id, s.act, s.act, true);
    else if (isOn(d) === (s.to === 'on')) continue;
    else if (IR.has(kindOf(d))) ok = await act(d.id, 'power', 'Nguồn', true);
    else ok = await setDev(d, s.to);
    ok ? n++ : bad++;
  }
  draw();
  toast(`Đã chạy “${sc.name}”: ${plural(n, 'lệnh')}${bad ? `, ${bad} lệnh lỗi` : ''}`);
}
document.addEventListener('click', e => {
  const t = e.target;
  const pick = sel => t.closest(sel);
  let el;
  if ((el = pick('[data-off]'))) return lightsOff(el.dataset.off);
  if ((el = pick('[data-act]'))) return act(el.dataset.act, el.dataset.code, el.dataset.label);
  if ((el = pick('[data-scene]'))) return runScene(+el.dataset.scene);
  if ((el = pick('[data-dev]'))) return toggle(el.dataset.dev);
  if (pick('[data-edit]')) { editing = !editing; return draw(); }
  if (pick('[data-wreset]')) {
    try { localStorage.removeItem('hubd.widgets'); } catch (_) { /* bỏ qua */ }
    layout = loadLayout();
    return draw();
  }
  if ((el = pick('[data-wsize]'))) { const w = layout.find(x => x.id === el.dataset.wsize); w.span = w.span === 2 ? 1 : 2; saveLayout(); return draw(); }
  if ((el = pick('[data-whide]'))) { const w = layout.find(x => x.id === el.dataset.whide); w.hidden = !w.hidden; saveLayout(); return draw(); }
  if (pick('[data-grip]')) return;
  if ((el = pick('[data-go]'))) {
    if (el.dataset.go === 'dien') return go(0, '', el, 'energy');
    const [f, r] = el.dataset.go.split('/');
    return go(Number(f) || 0, r || '', el);
  }
});
document.addEventListener('keydown', e => {   // thẻ phòng là div role=link: Enter/Space để mở
  if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('.rcard')) { e.preventDefault(); e.target.click(); }
});

/* ---------- Dữ liệu ---------- */
let es, retry = 1000;
function connect() {
  es = new EventSource('/events');
  es.onopen = async () => {   // nạp lại để bù tin lỡ khi mất mạng
    retry = 1000; setConn('ok');
    state = await (await api('/api/state')).json(); soon();
  };
  es.onmessage = ev => {
    const m = JSON.parse(ev.data);
    state[m.id] = m.value;
    if (m.id.startsWith('dev:')) pending.delete(m.id.slice(4));
    soon();
  };
  es.onerror = () => {
    setConn('bad'); es.close();
    setTimeout(connect, retry); retry = Math.min(retry * 2, 15000);
  };
}
document.addEventListener('visibilitychange', () => { if (!DEMO && !document.hidden && es && es.readyState === 2) connect(); });

async function init() {
  $('#loginbox').innerHTML = LOGIN_HTML;
  if (DEMO) {
    cfg = DEMO_DATA.cfg; state = {...DEMO_DATA.state};
    $('#conn').hidden = true;
    $('#demo-chip').innerHTML = '<a class="chip demo" href="/" title="Về dữ liệu thật của nhà">Dữ liệu mẫu<b>Thoát</b></a>';
  } else {
    cfg = await (await api('/api/config')).json();
    state = await (await api('/api/state')).json();
    connect();
  }
  view = parseHash();
  draw();
  setInterval(renderTop, 15000);
}
init();
