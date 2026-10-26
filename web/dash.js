// Hub Center: ảnh nền nhà (ngày/đêm) + widget kính mờ. Cả nhà -> tầng -> phòng, và trang Điện năng.
// Cần app.js (api, esc, LOGIN_HTML), scene.js (SCENE: ảnh + vị trí nhãn), demo.js (DEMO_DATA, dùng khi ?demo).
'use strict';
const $ = (s, el = document) => el.querySelector(s);
const Q = new URLSearchParams(location.search);
const DEMO = Q.has('demo'), DEBUG_ZONES = Q.has('vung');
const nf = new Intl.NumberFormat('vi-VN', {maximumFractionDigits: 1});

let cfg = {zones: [], entities: [], devices: [], scenes: []}, state = {};
let view = {floor: 0, room: '', page: ''}, editing = false, drag = null, lastScene = -1;
const pending = new Set();

/* ---------- Loại thiết bị ---------- */
// category Tuya (kg, dlq, pc, sd, sp...) hoặc tên tự đặt trong config.
const KIND = {kg: 'light', light: 'light', dj: 'light', pc: 'socket', cz: 'socket', socket: 'socket',
  dlq: 'breaker', breaker: 'breaker', tv: 'tv', speaker: 'speaker', fan: 'fan', fs: 'fan', ac: 'ac', kt: 'ac',
  sd: 'vacuum', vacuum: 'vacuum', router: 'router', wifi: 'router', sp: 'camera', camera: 'camera',
  pump: 'pump', ggq: 'water', sfkzq: 'water', water: 'water', infra: 'infra'};
const IR = new Set(['tv', 'speaker', 'fan', 'ac']);   // hồng ngoại: một chiều, không đọc được trạng thái thật
const RICH = new Set([...IR, 'vacuum']);             // thẻ có nút điều khiển riêng
const PASSIVE = new Set(['infra', 'router', 'camera']);   // chỉ báo trạng thái, không bật tắt
// Số đo inverter Deye + pin JK. grid: + mua / − bán; jk.power: + sạc / − xả. *_today: kWh trong ngày.
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
  snow: '<path d="M12 2v20M4 6.5l16 11M20 6.5l-16 11M9.5 3.5 12 6l2.5-2.5M9.5 20.5 12 18l2.5 2.5"/>',
  vacuum: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="9" r="2.3"/><path d="M7.5 15.5h9"/>',
  router: '<path d="M2.5 9a14 14 0 0 1 19 0M5.5 12.5a9.5 9.5 0 0 1 13 0M8.5 16a5 5 0 0 1 7 0M12 19.5h.01"/>',
  camera: '<rect x="2.5" y="6.5" width="13.5" height="11" rx="2.5"/><path d="M16 10.5l5.5-3v9L16 13.5"/>',
  pump: '<path d="M12 3s6 6.5 6 11a6 6 0 0 1-12 0c0-4.5 6-11 6-11z"/>',
  drop: '<path d="M12 3s6 6.5 6 11a6 6 0 0 1-12 0c0-4.5 6-11 6-11z"/>',
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
  back: '<path d="M15 6l-6 6 6 6"/>', more: '<path d="M9 6l6 6-6 6"/>',
  play: '<path d="M8 5.5v13l10-6.5z"/>', pause: '<path d="M8 5v14M16 5v14"/>',
  prev: '<path d="M6 5v14M19 5.5v13L9 12z"/>', next: '<path d="M18 5v14M5 5.5v13L15 12z"/>',
  vol: '<path d="M4 9.5h3.5L12 6v12l-4.5-3.5H4zM16 9a4 4 0 0 1 0 6M18.5 6.5a7.5 7.5 0 0 1 0 11"/>',
  music: '<path d="M9 18V5l11-2v13"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="17.5" cy="16" r="2.5"/>',
  up: '<path d="M6 15l6-6 6 6"/>', down: '<path d="M6 9l6 6 6-6"/>', menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>',
  check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>', grow: '<path d="M14 4h6v6M10 20H4v-6M20 4l-6.5 6.5M4 20l6.5-6.5"/>',
  shrink: '<path d="M4 14h6v6M20 10h-6V4M14 10l6.5-6.5M10 14l-6.5 6.5"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  eyeoff: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/><path d="M3 3l18 18"/>',
  undo: '<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11"/>',
  mute: '<path d="M4 9.5h3.5L12 6v12l-4.5-3.5H4zM16 9.5l5 5M21 9.5l-5 5"/>', input: '<path d="M10 18H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v5"/><path d="M14 16h7M18 13l3 3-3 3"/>',
  swing: '<path d="M4 15a8 8 0 0 1 16 0"/><path d="M1.5 12.5 4 15l2.5-2.5M17.5 12.5 20 15l2.5-2.5"/>',
  dock: '<path d="M4 20h16M7 20v-6a5 5 0 0 1 10 0v6M12 4v4M9.5 6.5 12 9l2.5-2.5"/>',
  warn: '<path d="M12 4 2.5 20h19zM12 10v4M12 17h.01"/>',
  bell: '<path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15zM10 20.5a2 2 0 0 0 4 0"/>',
  shield: '<path d="M12 3 4.5 6v5.5c0 4.5 3.2 8.2 7.5 9.5 4.3-1.3 7.5-5 7.5-9.5V6zM9 12l2 2 4-4"/>',
  cube: '<path d="M12 2.5 3.5 7v10l8.5 4.5 8.5-4.5V7zM3.5 7 12 11.5 20.5 7M12 11.5v10"/>',
  cloud: '<path d="M7 18h10a4 4 0 0 0 .5-8 6 6 0 0 0-11.5 1.5A3.3 3.3 0 0 0 7 18z"/>',
  leaf: '<path d="M5 19c0-8 5-13 15-14-1 10-6 15-14 15M5 19c3-4 6-6 10-8"/>',
  spark: '<path d="M12 3c.7 4.3 2.7 6.3 7 7-4.3.7-6.3 2.7-7 7-.7-4.3-2.7-6.3-7-7 4.3-.7 6.3-2.7 7-7z"/>',
  flame: '<path d="M12 21a6 6 0 0 0 6-6c0-4-3-6-4-10-2 2-2 4-2 5-1-1-2-2-2-4-2 2-4 5-4 9a6 6 0 0 0 6 6z"/>',
  layers: '<path d="M12 3 2.5 8 12 13l9.5-5zM2.5 12.5 12 17.5l9.5-5M2.5 16.5 12 21.5l9.5-5"/>',
  tree: '<path d="M12 21v-5M7 16h10l-5-13z"/>', cup: '<path d="M4 8h13v5a5 5 0 0 1-5 5H9a5 5 0 0 1-5-5zM17 9.5h1.5a2.5 2.5 0 0 1 0 5H17M7 3.5v2M10.5 3.5v2"/>',
  utensils: '<path d="M7 3v8M5 3v5a2 2 0 0 0 4 0V3M7 11v10M17 3c-2 2-2.5 5-2.5 8H17v10"/>', box: '<path d="M3.5 7.5 12 3l8.5 4.5v9L12 21l-8.5-4.5zM3.5 7.5 12 12l8.5-4.5M12 12v9"/>',
  toilet: '<path d="M6 3h5v7H6zM4 10h16a8 8 0 0 1-8 7v4H9v-4.5A7 7 0 0 1 4 10z"/>', monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
  bed: '<path d="M3 18v-6h18v6M3 12V7M5 12V9.5a1.5 1.5 0 0 1 1.5-1.5h3A1.5 1.5 0 0 1 11 9.5V12M3 18v2M21 18v2"/>',
  book: '<path d="M4 5a2 2 0 0 1 2-2h13v16H6a2 2 0 0 0-2 2zM4 19V5M19 19v2H6"/>', server: '<rect x="4" y="3" width="16" height="7" rx="2"/><rect x="4" y="14" width="16" height="7" rx="2"/><path d="M8 6.5h.01M8 17.5h.01"/>',
  grip: '<path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01" stroke-width="3.2"/>',
  eye: '<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/>',
};
const icon = (k, cls = '') => `<svg class="ic ${cls}" viewBox="0 0 24 24" aria-hidden="true">${ICON[k] || ICON.switch}</svg>`;

/* ---------- Tra cứu ---------- */
const kindOf = d => KIND[d.kind] || 'switch';
const isOn = d => /^\s*(on|1|true)\s*$/i.test(state['dev:' + d.id] || '');
const known = d => state['dev:' + d.id] !== undefined;
const offline = d => state['online:' + d.id] === 'offline';
const counted = d => !['breaker', ...PASSIVE].includes(kindOf(d));   // tính vào "đang bật"
const zone = id => cfg.zones.find(z => z.id === id);
const floorOf = d => zone(d.zone)?.floor || 0;
const floors = () => [...new Set(cfg.zones.map(z => z.floor).filter(Boolean))].sort((a, b) => a - b);
const floorName = f => SCENE.floorNames[f] || `Tầng ${f}`;
const rooms = f => cfg.zones.filter(z => z.floor === f && z.type !== 'panel');
const devsIn = zid => cfg.devices.filter(d => d.zone === zid);
const devsOnFloor = f => cfg.devices.filter(d => floorOf(d) === f);
const sensorsIn = zid => cfg.entities.filter(e => e.zone === zid && !e.device && !ENERGY_IDS.has(e.id));
const readingsOf = d => cfg.entities.filter(e => e.device === d.id);
const onIn = zid => devsIn(zid).filter(d => counted(d) && isOn(d));
const lit = zid => devsIn(zid).some(d => kindOf(d) === 'light' && isOn(d));
const routerOf = f => cfg.devices.find(d => kindOf(d) === 'router' && floorOf(d) === f);
const where = d => { const z = zone(d.zone); return z ? `${z.floor}${z.type === 'panel' ? '' : '/' + esc(z.id)}` : ''; };
const fmt = v => { const n = Number(v); return v === '' || isNaN(n) ? v : nf.format(n); };
const val = e => state[e.id] === undefined ? '—' : esc(fmt(state[e.id]));
const has = e => state[e.id] !== undefined;
const num = id => state[id] !== undefined && state[id] !== '' && !isNaN(Number(state[id])) ? Number(state[id]) : NaN;
const fw = w => isNaN(w) ? '—' : Math.abs(w) >= 1000 ? `${nf.format(Math.round(Math.abs(w) / 10) / 100)} kW` : `${nf.format(Math.round(Math.abs(w)))} W`;
const xs = (d, key) => state[`x:${d.id}:${key}`];   // trạng thái giả định theo lệnh gần nhất (hồng ngoại, robot)
// Thiết bị trong phạm vi: cả nhà (f = 0) hoặc một tầng; tuỳ chọn lọc theo loại.
const devsOf = (f, kind) => (f ? devsOnFloor(f) : cfg.devices).filter(d => !kind || kindOf(d) === kind);
const sensorsOn = f => cfg.entities.filter(e => !e.device && !ENERGY_IDS.has(e.id) && (!f || zone(e.zone)?.floor === f) && zone(e.zone)?.type !== 'outdoor' && has(e));
// Công suất một khu = tổng số đo W của các MCB gắn vào khu đó (entity có device + zone = khu).
const areaW = zid => {
  const es = cfg.entities.filter(e => e.zone === zid && e.device && e.unit === 'W' && has(e));
  return es.length ? es.reduce((a, e) => a + (Number(state[e.id]) || 0), 0) : NaN;
};
const floorW = f => { const ws = rooms(f).map(z => areaW(z.id)).filter(w => !isNaN(w)); return ws.length ? ws.reduce((a, b) => a + b, 0) : NaN; };
function totalPower() {
  const l = num('deye.load');
  if (!isNaN(l)) return [l, 'Tải cả nhà, đo ở inverter'];
  const ms = cfg.entities.filter(e => e.device && e.unit === 'W' && has(e));
  return ms.length ? [ms.reduce((a, e) => a + (Number(state[e.id]) || 0), 0), 'Tổng các MCB có đo điện'] : [NaN, ''];
}
// Biểu tượng phòng theo mã khu.
const ROOM_IC = [[/san|gieng/, 'tree'], [/cafe/, 'cup'], [/bep/, 'utensils'], [/kho/, 'box'], [/wc/, 'toilet'], [/lamviec/, 'monitor'],
  [/ngu/, 'bed'], [/docsach/, 'book'], [/kythuat/, 'server']];
const roomIcon = id => (ROOM_IC.find(([re]) => re.test(id)) || [0, 'home'])[1];
// Chỉ số chất lượng không khí từ PM2.5 (thang US EPA 2024).
function aqi(pm) {
  const B = [[0, 9, 0, 50], [9.1, 35.4, 51, 100], [35.5, 55.4, 101, 150], [55.5, 125.4, 151, 200], [125.5, 225.4, 201, 300], [225.5, 325.4, 301, 500]];
  const b = B.find(x => pm <= x[1]) || B.at(-1);
  return Math.round((b[3] - b[2]) / (b[1] - b[0]) * (Math.min(pm, b[1]) - b[0]) + b[2]);
}
const aqiLabel = a => a <= 50 ? ['Tốt', 'good'] : a <= 100 ? ['Trung bình', 'mid'] : a <= 150 ? ['Kém', 'bad'] : ['Xấu', 'bad'];

/* ---------- Ngày / đêm: cảm biến ánh sáng ngoài trời nếu có, không thì theo giờ ---------- */
const SKY = ['auto', 'day', 'night'];
let skyMode = 'auto';
try { skyMode = SKY.includes(localStorage.getItem('hubd.sky')) ? localStorage.getItem('hubd.sky') : 'auto'; } catch (_) { /* bộ nhớ bị chặn */ }
function sky() {
  const lux = cfg.entities.find(e => e.unit === 'lx' && zone(e.zone)?.type === 'outdoor' && has(e));
  const now = new Date(), h = now.getHours() + now.getMinutes() / 60;
  const a = lux ? {day: num(lux.id) > 400, by: 'cảm biến ánh sáng'} : {day: h >= 5.75 && h < 18, by: 'giờ trong ngày'};   // ponytail: giờ cố định 5:45–18:00
  return {day: skyMode === 'auto' ? a.day : skyMode === 'day', by: skyMode === 'auto' ? a.by : 'chọn tay'};
}

/* ---------- Thanh trên ---------- */
const WD = ['Chủ nhật', 'Thứ 2', 'Thứ 3', 'Thứ 4', 'Thứ 5', 'Thứ 6', 'Thứ 7'];
function renderTop() {
  const now = new Date(), s = sky();
  document.documentElement.dataset.sky = s.day ? 'day' : 'night';
  $('#when').innerHTML = `<b>${now.toLocaleTimeString('vi-VN', {hour: '2-digit', minute: '2-digit'})}</b>` +
    `<small>${WD.at(now.getDay())}, ${now.toLocaleDateString('vi-VN', {day: '2-digit', month: '2-digit', year: 'numeric'})}</small>`;
  $('#tabs').innerHTML = [0, ...floors()].map(f =>
    `<button class="${f === view.floor && !view.page ? 'sel' : ''}" data-go="${f || ''}">${f ? esc(floorName(f)) : 'Cả nhà'}</button>`).join('');
  const lost = cfg.devices.filter(offline).length;
  $('#bell').innerHTML = icon('bell') + (lost ? '<i class="badge"></i>' : '');
  $('#bell').title = lost ? `${lost} thiết bị cần chú ý` : 'Không có cảnh báo';
  $('#sky').innerHTML = icon(skyMode === 'auto' ? 'auto' : s.day ? 'sun' : 'moon');
  $('#sky').title = skyMode === 'auto' ? `Ngày/đêm tự động theo ${s.by}` : s.day ? 'Đang để ban ngày' : 'Đang để ban đêm';
}
function setConn(s) {
  const c = $('#conn');
  c.querySelector('.dot').className = 'dot ' + (s === 'ok' ? 'ok' : s === 'bad' ? 'bad' : '');
  c.title = s === 'ok' ? 'Hub trực tuyến' : s === 'bad' ? 'Mất kết nối hub, đang thử lại' : 'Đang kết nối';
}

/* ---------- Ảnh nền phủ kín màn hình + nhãn trên ảnh ---------- */
const imgOf = sc => sky().day ? sc.day : sc.night;
// Ảnh của trang hiện tại. pos = điểm neo khi phủ (như background-position), nhãn tính theo cùng phép phủ đó.
function sceneNow() {
  const H = SCENE.home, home = {src: imgOf(H), w: H.w, h: H.h, chips: {}, pos: H.pos};
  if (view.page) return {...home, blur: true};
  if (!view.floor) return {...home, chips: H.chips};
  const photo = view.room && SCENE.rooms[view.room];
  if (photo) return {src: photo, w: 16, h: 9, chips: {}};
  const fs = SCENE.floors[view.floor];
  return fs ? {src: imgOf(fs), w: fs.w, h: fs.h, chips: fs.chips, pos: fs.pos} : home;
}
let bgKey = '';
function renderBg() {
  const sc = sceneNow(), pos = sc.pos || [50, 50], key = sc.src + !!sc.blur + pos;
  if (key === bgKey) return;
  bgKey = key;
  const box = $('#bgs'), el = document.createElement('div');
  el.className = 'bg' + (sc.blur ? ' blur' : '');
  el.style.backgroundImage = `url("${sc.src}")`;
  el.style.backgroundPosition = `${pos[0]}% ${pos[1]}%`;
  box.append(el);
  requestAnimationFrame(() => requestAnimationFrame(() => el.classList.add('show')));
  setTimeout(() => { while (box.children.length > 1) box.firstElementChild.remove(); }, 900);
}
// Mưa theo cảm biến mưa ở giếng trời: phủ lớp hạt mưa rơi lên ảnh nền (dưới các widget). Hạt tạo một lần, bật tắt bằng class.
const rainE = () => cfg.entities.find(e => /mưa/i.test(e.name));
const raining = () => { const e = rainE(); return !!e && /^(có|mưa|1|true|on)/i.test(state[e.id] || ''); };
function renderRain() {
  const r = $('#rain'), on = raining();
  if (on && !r.childElementCount) r.innerHTML = Array.from({length: 90}, () => `<i style="left:${(Math.random() * 130 - 5).toFixed(1)}%;animation-delay:-${(Math.random() * 1.5).toFixed(2)}s;animation-duration:${(0.6 + Math.random() * 0.5).toFixed(2)}s;opacity:${(0.3 + Math.random() * 0.5).toFixed(2)}"></i>`).join('');
  r.classList.toggle('on', on);
}
const chip = (go, name, ic, dot, x, y, sel) =>
  `<button class="rchip${sel ? ' sel' : ''}" style="left:${x}%;top:${y}%" data-go="${esc(go)}" data-xy="${x}, ${y}">${icon(ic)}<span>${esc(name)}</span><i class="dot ${dot}"></i></button>`;
const roomDot = id => devsIn(id).some(offline) ? 'bad' : lit(id) ? 'lamp' : devsIn(id).length ? 'ok' : '';
function chipsHtml() {
  if (view.page) return '';
  const sc = sceneNow();
  if (!view.floor) {
    return Object.entries(sc.chips).filter(([f]) => floors().includes(+f)).map(([f, [x, y]]) => {
      const bad = devsOnFloor(+f).some(offline), on = rooms(+f).some(z => lit(z.id));
      return chip(f, floorName(+f), 'layers', bad ? 'bad' : on ? 'lamp' : 'ok', x, y);
    }).join('');
  }
  return Object.entries(sc.chips).filter(([id]) => zone(id))
    .map(([id, [x, y]]) => chip(`${view.floor}/${id}`, zone(id).name, roomIcon(id), roomDot(id), x, y, view.room === id)).join('');
}
function stageHtml() {
  if (view.page === 'settings') return settingsBox();
  if (view.page === 'energy') { const fm = flowMap(); return fm ? `<div class="ebox"><div class="wh">${icon('bolt', 'wi amber')}<h3>Dòng điện lúc này</h3></div>${fm}</div>` : ''; }
  const sc = sceneNow(), pos = sc.pos || [50, 50];
  return `<div class="cover" style="--ar:${(sc.w / sc.h).toFixed(4)};--px:${pos[0] / 100};--py:${pos[1] / 100}"><div id="chips"></div></div>`;
}

/* ---------- Khung + nội dung trang ---------- */
const pageType = () => view.page || (view.room ? 'room' : view.floor ? 'floor' : 'home');   // page: energy | settings
let shellKey = '';
function draw() {
  if (drag) return;
  renderTop();
  renderBg();
  renderRain();
  const key = `${pageType()}|${view.floor}|${view.room}|${sceneNow().src}|${editing}`;
  const v = $('#view');
  if (key !== shellKey) {
    shellKey = key;
    v.className = `view v-${pageType()}${DEBUG_ZONES ? ' debug' : ''}${editing ? ' editing' : ''}`;
    v.innerHTML = `<div class="greet" id="greet"></div><div class="stage ${pageType()}" id="stage">${stageHtml()}</div>
      <aside class="side wz" id="side" data-zone="side"></aside><section class="bottom wz" id="bottom" data-zone="bottom"></section>`;
  }
  if (!volDrag) $('#greet').innerHTML = greetHtml();   // đang kéo âm lượng thì không vẽ đè
  const c = $('#chips');
  if (c) c.innerHTML = chipsHtml();
  if (view.page) $('#stage').innerHTML = stageHtml();
  const [side, bottom, scroll] = zonesHtml(), keep = {};
  for (const e of $('#view').querySelectorAll('[data-keep]')) keep[e.dataset.keep] = [e.scrollLeft, e.scrollTop];
  $('#side').innerHTML = side;
  $('#bottom').innerHTML = bottom;
  for (const e of $('#view').querySelectorAll('[data-keep]')) if (keep[e.dataset.keep]) [e.scrollLeft, e.scrollTop] = keep[e.dataset.keep];
  $('#bottom').classList.toggle('scroll', !!scroll);
  if (sheet && $('#modal').open) $('#modal').innerHTML = sheetHtml(sheet);
}
let raf = 0;
const soon = () => { raf = raf || requestAnimationFrame(() => { raf = 0; draw(); }); };

function greetHtml() {
  const hr = new Date().getHours(), hi = hr < 4 ? 'Chào buổi tối!' : hr < 11 ? 'Chào buổi sáng!' : hr < 13 ? 'Chào buổi trưa!' : hr < 18 ? 'Chào buổi chiều!' : 'Chào buổi tối!';
  const lost = cfg.devices.filter(offline).length, t = pageType();
  let h = hi, p = '', extra = '';
  if (t === 'home') {
    p = lost ? `Có ${lost} thiết bị cần chú ý.` : 'Mọi thứ đang hoạt động tốt.';
    extra = player(0);
  } else if (t === 'floor') {
    const f = view.floor, n = rooms(f).filter(z => lit(z.id)).length;
    const temp = sensorsOn(f).find(e => e.unit === '°C');
    p = `${floorName(f)} ${n ? `có ${n} phòng đang sáng đèn` : 'đang tắt đèn'}${temp ? `, ${val(temp)}°C` : ''}.`;
    extra = player(f);
  } else if (t === 'room') {
    const z = zone(view.room), ds = devsIn(z.id), on = onIn(z.id).length;
    const temp = sensorsIn(z.id).find(e => e.unit === '°C' && has(e)), hum = sensorsIn(z.id).find(e => e.unit === '%' && has(e));
    h = esc(z.name);
    p = `${floorName(z.floor)}${ds.length ? `, ${ds.length} thiết bị${on ? `, ${on} đang bật` : ''}` : ''}${temp ? `, ${val(temp)}°C` : ''}${hum ? `, độ ẩm ${val(hum)}%` : ''}.`;
    extra = player(0, z.id);
  } else if (t === 'settings') {
    h = 'Cài đặt';
    p = 'Giao diện, phòng, thiết bị và ngữ cảnh của hub.';
  } else {
    const [w] = totalPower(), pv = num('deye.pv'), soc = num('jk.soc');
    h = 'Điện năng';
    p = isNaN(w) ? 'Chưa có số đo điện.' : `Nhà đang dùng ${fw(w)}${isNaN(pv) ? '' : `, mặt trời ${fw(pv)}`}${isNaN(soc) ? '' : `, pin ${nf.format(soc)}%`}.`;
  }
  const back = t === 'room' ? `<button class="edit" data-go="${view.floor}">${icon('back')}</button>` : view.page ? `<button class="edit" data-go="">${icon('back')}</button>` : '';
  const edit = t === 'home' || t === 'floor'
    ? `${editing ? '<button class="edit" data-wreset>Về mặc định</button>' : ''}<button class="edit${editing ? ' primary' : ''}" data-edit>${editing ? 'Xong' : 'Sắp xếp'}</button>` : '';
  return `<div class="gtop">${back}<div><h1>${h}</h1><p>${esc(p)}</p></div>${edit}</div>${extra}`;
}

/* ---------- Widget theo trang (kéo thả đổi chỗ trong cột phải / hàng dưới, lưu trên thiết bị này) ---------- */
const DEF_LAYOUT = {
  home: {side: ['scenes', 'ac', 'lights', 'weather', 'energy'], bottom: ['climate', 'cameras', 'security', 'devices']},
  floor: {side: ['rooms', 'quick', 'air', 'water', 'energy'], bottom: ['lights', 'ac', 'cameras', 'scenes']},
};
let layout = null;   // nạp trong init(), sau khi W đã được khai báo
function loadLayout() {
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem('hubd.layout3') || '{}') || {}; } catch (_) { saved = {}; }
  const out = {};
  for (const [pg, zs] of Object.entries(DEF_LAYOUT)) {
    out[pg] = {hidden: Array.isArray(saved[pg]?.hidden) ? saved[pg].hidden.filter(id => W[id]) : [],
      opt: saved[pg]?.opt && typeof saved[pg].opt === 'object' ? saved[pg].opt : {}};
    const all = [...zs.side, ...zs.bottom];
    for (const zn of ['side', 'bottom']) {
      const s = Array.isArray(saved[pg]?.[zn]) ? saved[pg][zn].filter(id => all.includes(id)) : [];
      out[pg][zn] = s.length ? s : [...zs[zn]];
    }
    // widget mới thêm sau: đặt về chỗ mặc định nếu chưa có ở đâu
    for (const zn of ['side', 'bottom']) for (const id of zs[zn]) if (![...out[pg].side, ...out[pg].bottom].includes(id)) out[pg][zn].push(id);
  }
  return out;
}
const saveLayout = () => { try { localStorage.setItem('hubd.layout3', JSON.stringify(layout)); } catch (_) { /* bỏ qua */ } };

// Tuỳ chọn từng widget, lưu theo loại trang: size S (nhỏ) / L (lớn), off = các mục người dùng ẩn bớt.
const wOpt = id => layout?.[pageType()]?.opt[id] || {};
const hid = (id, key) => (wOpt(id).off || []).includes(String(key));
const zoneOfW = id => layout[pageType()].side.includes(id) ? 'side' : 'bottom';
const sizeOf = id => wOpt(id).size || (zoneOfW(id) === 'side' && !(W[id].half || []).includes(pageType()) ? 'L' : 'S');
function setOpt(id, patch) {
  const o = layout[pageType()].opt;
  o[id] = {...o[id], ...patch};
  saveLayout();
  draw();
}
function cfgPanel(id) {
  const d = W[id], opts = d.o ? d.o(view.floor) : [], sz = sizeOf(id);
  return `<div class="m-box sheet"><header class="wh">${icon(d.i, 'wi ' + (d.c || ''))}<h3>${d.t}<small>Cấu hình widget</small></h3></header>
    <div class="acrow"><span>Kích thước</span><div class="seg">${[['S', 'Nhỏ'], ['L', 'Lớn']].map(([v, l]) => `<button class="rb${sz === v ? ' sel' : ''}" data-wsize="${id}:${v}">${l}</button>`).join('')}</div></div>
    ${opts.length ? `<div class="acrow"><span>Hiện trong widget</span><div class="picks">${opts.map(([k, l]) =>
      `<button class="pick${hid(id, k) ? '' : ' on'}" data-wpick="${id}:${esc(k)}" aria-pressed="${!hid(id, k)}">${icon('check')}<span>${esc(l)}</span></button>`).join('')}</div></div>` : ''}
    <div class="m-act"><button class="m-btn" data-go="caidat">Cài đặt nâng cao</button><button value="1" class="m-btn ok">Xong</button></div></div>`;
}
function zonesHtml() {
  const t = pageType();
  if (t === 'room') return roomZones(zone(view.room));
  if (t === 'energy') return energyZones();
  if (t === 'settings') return settingsZones();
  const L = layout[t], f = view.floor;
  const one = (id, zn) => {
    const d = W[id], hidden = L.hidden.includes(id);
    const body = d.r(f);
    if (!editing && (hidden || !body)) return '';
    const meta = d.m ? d.m(f) : '';
    const sz = sizeOf(id), tools = editing ? `<div class="wtools"><button class="wt" data-wsize="${id}:${sz === 'L' ? 'S' : 'L'}" aria-label="${sz === 'L' ? 'Thu nhỏ' : 'Phóng to'}">${icon(sz === 'L' ? 'shrink' : 'grow')}</button>
      <button class="wt" data-sheet="cfg:${id}" aria-label="Cấu hình ${d.t}">${icon('gear')}</button>
      <button class="wt" data-whide="${id}" aria-label="${hidden ? 'Hiện' : 'Ẩn'} ${d.t}">${icon(hidden ? 'eyeoff' : 'eye')}</button>
      <span class="grip" data-grip aria-label="Giữ và kéo để đổi chỗ">${icon('grip')}</span></div>` : '';
    const link = !editing && d.go ? `<button class="more" data-go="${typeof d.go === 'function' ? d.go(f) : d.go}" aria-label="Xem chi tiết">${icon('more')}</button>` : '';
    const cls = zn === 'side' ? (sz === 'S' ? ' half' : '') : sz === 'L' ? ' big2' : '';
    return `<section class="w${cls}${hidden ? ' off' : ''}" data-w="${id}"><header class="wh">${icon(d.i, 'wi ' + (d.c || ''))}<h3>${d.t}${meta ? `<small>${meta}</small>` : ''}</h3>${link}</header>${tools}
      ${body || '<p class="empty">Chưa có dữ liệu cho mục này.</p>'}</section>`;
  };
  return [L.side.map(id => one(id, 'side')).join(''), L.bottom.map(id => one(id, 'bottom')).join('')];
}

const sw = (d, cls = '') => IR.has(kindOf(d))
  ? `<button class="sw ${cls}${isOn(d) ? ' on' : ''}" data-act="${esc(d.id)}" data-code="power" data-label="Nguồn" aria-pressed="${isOn(d)}" aria-label="Bật/tắt ${esc(d.name)}"></button>`
  : `<button class="sw ${cls}${isOn(d) ? ' on' : ''}${pending.has(d.id) ? ' pend' : ''}" data-dev="${esc(d.id)}" ${offline(d) ? 'disabled' : ''} aria-pressed="${isOn(d)}" aria-label="Bật/tắt ${esc(d.name)}"></button>`;
// Ảnh nhỏ của một phòng: ảnh riêng nếu có, không thì cắt quanh nhãn phòng trên ảnh tầng.
function thumbBg(z) {
  if (!z) return '';
  if (SCENE.rooms[z.id]) return `background-image:url('${SCENE.rooms[z.id]}')`;
  const fs = SCENE.floors[z.floor], c = fs?.chips[z.id];
  return fs ? `background-image:url('${imgOf(fs)}');background-size:300%;background-position:${c ? `${c[0]}% ${c[1]}%` : 'center'}` : '';
}
// Điều hoà theo phòng: một điều khiển chung cho mọi máy trong phòng; máy đang bật (hoặc máy đầu) làm chuẩn hiển thị.
const acsIn = zid => devsIn(zid).filter(d => kindOf(d) === 'ac');
const acRooms = f => [...new Set(devsOf(f, 'ac').map(d => d.zone))];
const acRef = ds => ds.find(isOn) || ds[0];
const acTemp = d => Number(xs(d, 'temp')) || 26;
const AC_MODE = {auto: 'Tự động', cool: 'Mát', dry: 'Khô', fan: 'Quạt'};
const acg = (zid, code, label, inner = label, cls = 'rb', off = false) =>
  `<button class="${cls}" data-acg="${esc(zid)}" data-code="${code}" data-label="${label}" aria-label="${label}"${off ? ' disabled' : ''}>${inner}</button>`;
const acSw = (zid, on) => acg(zid, on ? 'off' : 'on', on ? 'Tắt điều hoà' : 'Bật điều hoà', '', `sw blue${on ? ' on' : ''}`);
const acTempCtl = (zid, t, cls = '') => `<span class="temp${cls}">${acg(zid, `temp_${t - 1}`, 'Giảm nhiệt độ', '−', 'rb', t <= 16)}<b>${t}°</b>${acg(zid, `temp_${t + 1}`, 'Tăng nhiệt độ', '+', 'rb', t >= 30)}</span>`;
const acSub = (ds, r, on) => [ds.length > 1 && `${ds.length} máy`, on && AC_MODE[xs(r, 'mode') || 'cool']].filter(Boolean).join(', ');
function acRow(zid) {
  const ds = acsIn(zid), r = acRef(ds), on = ds.some(isOn);
  return `<div class="acm${on ? ' on' : ''}"><button class="acn" data-sheet="ac:${esc(zid)}" aria-label="Mở toàn bộ điều khiển điều hoà ${esc(zone(zid)?.name || '')}">
    <b>${esc(zone(zid)?.name || r.name)}${icon('more')}</b>${acSub(ds, r, on) ? `<small>${acSub(ds, r, on)}</small>` : ''}</button>${acSw(zid, on)}${acTempCtl(zid, acTemp(r))}</div>`;
}
function acCard(zid) {
  const ds = acsIn(zid), r = acRef(ds), on = ds.some(isOn);
  return `<div class="dv k-ac${on ? ' on' : ''}" title="Điều khiển hồng ngoại: không đọc được trạng thái thật"><div class="dvh"><button class="dvo" data-sheet="ac:${esc(zid)}" aria-label="Mở toàn bộ điều khiển điều hoà">
    <span class="dvi">${icon('ac')}</span><div><b>Điều hoà</b>${ds.length > 1 ? `<small>${ds.length} máy</small>` : ''}</div></button>${acSw(zid, on)}</div>
    <div class="remote">${acTempCtl(zid, acTemp(r), ' wide')}</div></div>`;
}
// Bảng mở rộng: đủ các nút của remote máy lạnh, áp dụng cho cả phòng; phòng nhiều máy thì bật/tắt riêng từng máy.
function acPanel(zid) {
  const ds = acsIn(zid), r = acRef(ds), on = ds.some(isOn), t = acTemp(r);
  const seg = (key, opts, def) => `<div class="seg">${opts.map(([v, l]) => acg(zid, `${key}_${v}`, l, l, 'rb' + ((xs(r, key) || def) === v ? ' sel' : ''))).join('')}</div>`;
  const tog = (key, l) => acg(zid, key, l, l, 'rb' + (xs(r, key) === '1' ? ' sel' : ''));
  return `<div class="m-box sheet"><header class="wh">${icon('snow', 'wi blue')}<h3>Điều hoà ${esc(zone(zid)?.name || '')}<small>${ds.length > 1 ? `${ds.length} máy, điều khiển cùng lúc` : esc(r.name)}</small></h3>${acSw(zid, on)}</header>
    <div class="acdial">${acg(zid, `temp_${t - 1}`, 'Giảm nhiệt độ', '−', 'rb big', t <= 16)}<b>${t}<small>°C</small></b>${acg(zid, `temp_${t + 1}`, 'Tăng nhiệt độ', '+', 'rb big', t >= 30)}</div>
    <div class="acrow"><span>Chế độ</span>${seg('mode', Object.entries(AC_MODE), 'cool')}</div>
    <div class="acrow"><span>Tốc độ gió</span>${seg('fan', [['auto', 'Tự động'], ['1', 'Nhẹ'], ['2', 'Vừa'], ['3', 'Mạnh']], 'auto')}</div>
    <div class="acrow"><span>Tiện ích</span><div class="remote">${tog('swing', 'Đảo gió')}${tog('sleep', 'Ngủ đêm')}${tog('turbo', 'Làm lạnh nhanh')}</div></div>
    ${ds.length > 1 ? `<div class="acrow"><span>Từng máy</span><div class="lights">${ds.map(d => `<div class="lt${isOn(d) ? ' on' : ''}">${icon('ac')}<span>${esc(d.name)} · ${acTemp(d)}°</span>${sw(d, 'blue')}</div>`).join('')}</div></div>` : ''}
    <p class="note">Hồng ngoại chỉ gửi lệnh một chiều: trạng thái hiện theo lệnh gần nhất.</p>
    <div class="m-act"><button value="1" class="m-btn ok">Xong</button></div></div>`;
}
// Remote TV đầy đủ: âm lượng, kênh, phím điều hướng, quay lại, trang chủ, menu, tắt tiếng, nguồn vào.
function tvPanel(id) {
  const d = cfg.devices.find(x => x.id === id), b = (code, label, inner, cls = 'rb') => btn(id, code, label, inner, cls);
  const vr = (dn, up, label, mid) => `<div class="vrock">${b(up, label + ' tăng', '+')}<span>${mid}</span>${b(dn, label + ' giảm', '−')}</div>`;
  return `<div class="m-box sheet"><header class="wh">${icon('tv', 'wi blue')}<h3>${esc(d.name)}<small>${esc(zone(d.zone)?.name || '')}</small></h3>${sw(d, 'blue')}</header>
    <div class="tvpad">${vr('vol_down', 'vol_up', 'Âm lượng', icon('vol'))}
      <div class="dpad">${b('up', 'Lên', icon('up'), 'rb up')}${b('left', 'Trái', icon('back'), 'rb lf')}${b('ok', 'Chọn', 'OK', 'rb ok')}${b('right', 'Phải', icon('more'), 'rb rt')}${b('down', 'Xuống', icon('down'), 'rb dn')}</div>
      ${vr('ch_down', 'ch_up', 'Kênh', 'CH')}</div>
    <div class="remote">${[['back', 'Quay lại', 'undo'], ['home', 'Trang chủ', 'home'], ['menu', 'Menu', 'menu'], ['mute', 'Tắt tiếng', 'mute'], ['input', 'Nguồn vào', 'input']].map(([c, l, i]) => b(c, l, icon(i), 'rb f')).join('')}</div>
    <p class="note">Hồng ngoại chỉ gửi lệnh một chiều: trạng thái hiện theo lệnh gần nhất.</p>
    <div class="m-act"><button value="1" class="m-btn ok">Xong</button></div></div>`;
}
// Bảng mở rộng đang mở ('ac:<phòng>', 'tv:<thiết bị>', 'cam:<camera>'); draw() vẽ lại khi trạng thái đổi.
let sheet = '';
const SHEETS = {ac: acPanel, tv: tvPanel, cam: camPanel, cfg: cfgPanel};
const sheetHtml = key => SHEETS[key.slice(0, key.indexOf(':'))](key.slice(key.indexOf(':') + 1));
function openSheet(key) {
  const m = $('#modal');
  sheet = key;
  m.innerHTML = sheetHtml(key);
  m.onclick = e => { if (e.target === m || e.target.closest('.m-act button, [data-go]')) m.close(); };
  m.onclose = () => { sheet = ''; };
  m.showModal();
}
// on/off chỉ bấm nguồn máy đang khác trạng thái; lệnh khác gửi tới mọi máy (bỏ qua máy mất kết nối).
async function acGroup(zid, code, label) {
  let bad = 0;
  for (const d of acsIn(zid).filter(x => !offline(x))) {
    if ((code === 'on' || code === 'off') && isOn(d) === (code === 'on')) continue;
    if (!(await act(d.id, code === 'on' || code === 'off' ? 'power' : code, label, true))) bad++;
  }
  draw();
  if (bad) toast(`${bad} máy lạnh chưa nhận lệnh “${label}”`);
}
const QUICK = [['light', 'Đèn'], ['fan', 'Quạt'], ['ac', 'Điều hoà'], ['socket', 'Ổ cắm'], ['speaker', 'Loa'], ['tv', 'TV'], ['pump', 'Bơm nước'], ['water', 'Tưới cây']];
const W = {
  rooms: {t: 'Các phòng', i: 'layers', c: 'amber', o: f => rooms(f).map(z => [z.id, z.name]), r: f => f ? `<div class="wfs">${rooms(f).filter(z => !hid('rooms', z.id)).map(z => `<button class="wfp" data-go="${f}/${esc(z.id)}">${icon(roomIcon(z.id))}${esc(z.name)}<i class="dot ${roomDot(z.id)}"></i></button>`).join('')}</div>` : ''},
  scenes: {t: 'Ngữ cảnh', i: 'spark', c: 'amber', o: () => (cfg.scenes || []).map(s => [s.name, s.name]), r: () => (cfg.scenes || []).length
    ? `<div class="scn">${cfg.scenes.map((s, i) => hid('scenes', s.name) ? '' : `<button class="sc${i === lastScene ? ' on' : ''}" data-scene="${i}" title="${esc(s.note || '')}">${icon(s.icon || 'spark')}<span>${esc(s.name)}</span></button>`).join('')}</div>` : ''},
  energy: {t: 'Điện năng', i: 'bolt', c: 'amber', half: ['home'], go: 'dien', r: f => {
    const soc = num('jk.soc'), bat = num('jk.power'), grid = num('deye.grid'), w = f ? floorW(f) : totalPower()[0];
    if (isNaN(soc) && isNaN(w)) return '';
    if (f) {
      const rows = rooms(f).map(z => [z, areaW(z.id)]).filter(([, x]) => !isNaN(x)).sort((a, b) => b[1] - a[1]);
      return `<div class="big">${fw(w)}<small>${esc(floorName(f))} đang dùng</small></div>${areaBars(rows, w, 3)}`;
    }
    const st = isNaN(bat) ? '' : bat > 5 ? 'đang sạc' : bat < -5 ? 'đang xả' : 'đang nghỉ';
    return `${isNaN(soc) ? '' : `<div class="big">${nf.format(soc)}%<small>Pin ${st}</small></div><div class="bar"><i style="width:${Math.min(100, soc)}%"></i></div>`}
      <div class="kv"><span>Nhà dùng <b>${fw(w)}</b></span>${isNaN(grid) ? '' : `<span>${grid > 5 ? `Mua lưới <b>${fw(grid)}</b>` : grid < -5 ? `Bán lưới <b>${fw(grid)}</b>` : 'Lưới không dùng'}</span>`}</div>`;
  }},
  weather: {t: 'Thời tiết', i: 'cloud', c: 'cyan', half: ['home'], r: () => {
    const out = cfg.entities.filter(e => zone(e.zone)?.type === 'outdoor' && has(e));
    if (!out.length) return '';
    const t = out.find(e => e.unit === '°C'), h = out.find(e => e.unit === '%'), rain = out.find(e => /mưa/i.test(e.name)), lux = out.find(e => e.unit === 'lx');
    const rainy = raining();
    const desc = rainy ? 'Đang mưa' : lux ? (num(lux.id) > 20000 ? 'Trời nắng, không mưa' : num(lux.id) > 400 ? 'Trời sáng, không mưa' : 'Trời tối, không mưa') : 'Không mưa';
    return `${t ? `<div class="big">${val(t)}°</div>` : ''}${DEMO && rain ? `<button class="sub" data-rain title="Dữ liệu mẫu: bấm để thử trời mưa">${desc}</button>` : `<div class="sub">${desc}</div>`}
      <div class="kv">${h ? `<span>${icon('drop')}Độ ẩm <b>${val(h)}%</b></span>` : ''}${lux ? `<span>${icon('sun')}<b>${val(lux)}</b> lx</span>` : ''}</div>`;
  }},
  air: {t: 'Không khí', i: 'leaf', c: 'green', half: ['floor'], r: f => {
    const es = sensorsOn(f), pm = es.find(e => e.unit === 'µg/m³'), co2 = es.find(e => /co/i.test(e.name) && e.unit === 'ppm');
    if (!pm && !co2) return '';
    const a = pm ? aqi(num(pm.id)) : NaN, [lb, cls] = isNaN(a) ? ['', ''] : aqiLabel(a);
    // thang AQI 0–300 chia màu theo mốc EPA 50/100/150/200; chấm trắng là mức hiện tại
    return `${isNaN(a) ? '' : `<div class="aq"><b class="${cls}">${a}</b><span>${lb}<small>Chỉ số AQI</small></span></div>
      <div class="aqbar"><i style="left:${Math.min(100, a / 3).toFixed(1)}%"></i></div>`}
      <div class="kv">${pm ? `<span>PM2.5 <b>${val(pm)}</b></span>` : ''}${co2 ? `<span>CO₂ <b>${val(co2)}</b></span>` : ''}</div>`;
  }},
  water: {t: 'Nước lọc', i: 'drop', c: 'cyan', half: ['floor'], r: f => {
    const tds = sensorsOn(f).find(e => /nước|tds/i.test(e.name));
    if (!tds) return '';
    const v = num(tds.id), [lb, cls] = v <= 50 ? ['Tốt', 'good'] : v <= 150 ? ['Đạt', 'mid'] : ['Thay lõi lọc', 'bad'];
    return `<div class="aq"><b class="${cls}">${val(tds)}</b><span>${lb}<small>TDS, ppm</small></span></div>`;
  }},
  ac: {t: 'Điều hoà', i: 'snow', c: 'blue', o: f => acRooms(f).map(z => [z, zone(z)?.name || z]), m: f => { const r = acRooms(f); return r.length ? `${r.filter(z => acsIn(z).some(isOn)).length}/${r.length} phòng đang bật` : ''; }, r: f => {
    const rs = acRooms(f).filter(z => !hid('ac', z)).sort((a, b) => acsIn(b).some(isOn) - acsIn(a).some(isOn));
    return rs.length ? `<div class="acs" data-keep="ac">${rs.map(acRow).join('')}</div>` : '';
  }},
  lights: {t: 'Chiếu sáng', i: 'light', c: 'amber', o: f => f ? [] : cfg.zones.filter(z => devsIn(z.id).some(d => kindOf(d) === 'light')).map(z => [z.id, z.name]), m: f => { const l = devsOf(f, 'light'); return l.length ? `${l.filter(isOn).length}/${l.length} đang bật` : ''; }, r: f => {
    const ls = devsOf(f, 'light');
    if (!ls.length) return '';
    if (f) return `<div class="btn2"><button class="pill amber" data-lights="${f}:on">${icon('sun')}Bật tất cả</button><button class="pill" data-lights="${f}:off">${icon('moon')}Tắt tất cả</button></div>`;
    const lr = cfg.zones.filter(z => lit(z.id) && !hid('lights', z.id));
    return lr.length ? `<div class="thumbs" data-keep="lights">${lr.map(z => `<button class="th" data-go="${z.floor}/${esc(z.id)}" style="${thumbBg(z)}"><span>${esc(z.name)}</span></button>`).join('')}</div>`
      : '<p class="empty">Không phòng nào đang bật đèn.</p>';
  }},
  climate: {t: 'Khí hậu trong nhà', i: 'leaf', c: 'green', o: () => [['t', 'Nhiệt độ'], ['h', 'Độ ẩm'], ['pm', 'Bụi PM2.5'], ['co2', 'CO₂'], ['tds', 'Nước lọc']], r: f => {
    // nhiệt độ, độ ẩm lấy trung bình các phòng; bụi, CO₂, nước lấy chỗ tệ nhất
    const es = sensorsOn(f).filter(e => !/ngoài trời/i.test(e.name)), vs = test => es.filter(test).map(e => num(e.id)).filter(v => !isNaN(v));
    const avg = a => a.reduce((x, y) => x + y, 0) / a.length, t = vs(e => e.unit === '°C'), h = vs(e => e.unit === '%' && /ẩm/i.test(e.name)),
      pm = vs(e => e.unit === 'µg/m³'), co2 = vs(e => /co/i.test(e.name) && e.unit === 'ppm'), tds = vs(e => /nước|tds/i.test(e.name));
    const lvl = (v, ok, mid) => v <= ok ? 'good' : v <= mid ? 'mid' : 'bad';
    const tiles = [
      t.length && ['t', 'Nhiệt độ', nf.format(avg(t)), '°', ''],
      h.length && ['h', 'Độ ẩm', Math.round(avg(h)), '%', avg(h) >= 40 && avg(h) <= 70 ? 'good' : 'mid'],
      pm.length && ['pm', 'Bụi PM2.5', Math.max(...pm), 'µg/m³', aqiLabel(aqi(Math.max(...pm)))[1]],
      co2.length && ['co2', 'CO₂', Math.max(...co2), 'ppm', lvl(Math.max(...co2), 800, 1200)],
      tds.length && ['tds', 'Nước lọc', Math.max(...tds), 'ppm', lvl(Math.max(...tds), 50, 150)],
    ].filter(x => x && !hid('climate', x[0])).map(x => x.slice(1));
    return tiles.length ? `<div class="clim">${tiles.map(([l, v, u, cls]) => `<div class="cl ${cls}"><small>${l}</small><b>${v}<small>${u}</small></b></div>`).join('')}</div>` : '';
  }},
  cameras: {t: 'Camera', i: 'camera', o: f => devsOf(f, 'camera').map(d => [d.id, d.name]), m: f => { const c = devsOf(f, 'camera'); return c.length ? `${c.filter(d => !offline(d)).length}/${c.length} đang ghi hình` : ''; }, r: f => {
    const cs = devsOf(f, 'camera').filter(d => !hid('cameras', d.id));
    return cs.length ? `<div class="cams" data-keep="cameras">${cs.map(d => `<button class="cam" data-sheet="cam:${esc(d.id)}" style="${thumbBg(zone(d.zone))}" title="Ảnh minh hoạ phòng, chưa có hình trực tiếp">
      ${icon('camera', 'camic')}<span><i class="dot ${offline(d) ? 'bad' : 'ok'}"></i>${esc(zone(d.zone)?.name || d.name)}</span></button>`).join('')}</div>` : '';
  }},
  security: {t: 'An ninh', i: 'shield', c: 'green', r: () => {
    const cs = cfg.devices.filter(d => kindOf(d) === 'camera'), lost = cfg.devices.filter(offline);
    return `<div class="sec ${lost.length ? 'warn' : 'ok'}">${lost.length ? `${lost.length} thiết bị mất kết nối` : 'Bình thường'}</div>
      <div class="ics"><button data-alerts><span class="ib">${icon('camera')}</span>Camera<span class="n">${cs.length - cs.filter(offline).length}</span></button>
      <button data-alerts><span class="ib">${icon('warn')}</span>Cảnh báo<span class="n">${lost.length}</span></button>
      <span title="Hệ báo cháy CBX chưa nối vào hub"><span class="ib">${icon('flame')}</span>Báo cháy</span></div>`;
  }},
  devices: {t: 'Thiết bị', i: 'cube', o: () => [['light', 'Đèn'], ['socket', 'Ổ cắm'], ['ac', 'Điều hoà'], ['fan', 'Quạt'], ['other', 'Khác']], m: f => { const ds = devsOf(f); return `${ds.length} thiết bị, ${ds.filter(d => !offline(d)).length} trực tuyến`; }, r: f => {
    const ds = devsOf(f), G = [['light', 'Đèn'], ['socket', 'Ổ cắm'], ['ac', 'Điều hoà'], ['fan', 'Quạt']];
    const other = ds.filter(d => !G.some(([k]) => kindOf(d) === k)).length;
    return `<div class="ics">${G.map(([k, n]) => { const c = ds.filter(d => kindOf(d) === k).length; return c && !hid('devices', k) ? `<span><span class="ib">${icon(k)}</span>${n}<span class="n">${c}</span></span>` : ''; }).join('')}
      ${other && !hid('devices', 'other') ? `<span><span class="ib">${icon('cube')}</span>Khác<span class="n">${other}</span></span>` : ''}</div>`;
  }},
  quick: {t: 'Thao tác nhanh', i: 'bolt', c: 'amber', o: f => QUICK.filter(([k]) => devsOf(f, k).length), r: f => {
    const rows = QUICK.map(([k, n]) => [k, n, devsOf(f, k).filter(d => !d.protected)]).filter(([k, , ds]) => ds.length && !hid('quick', k));
    return rows.length ? `<div class="qa">${rows.map(([k, n, ds]) => { const on = ds.some(isOn); return `<div class="q${on ? ' on' : ''}">${icon(k)}<span>${n}<small>${ds.length} thiết bị</small></span>
      <button class="sw ${k === 'ac' || k === 'fan' ? 'blue' : k === 'light' ? 'amber' : ''}${on ? ' on' : ''}" data-group="${f}:${k}:${on ? 'off' : 'on'}" aria-label="${on ? 'Tắt' : 'Bật'} ${n}"></button></div>`; }).join('')}</div>` : '';
  }},
};

/* ---------- Trang phòng ---------- */
function roomZones(z) {
  const ds = devsIn(z.id), ss = sensorsIn(z.id), aw = areaW(z.id), ls = ds.filter(d => kindOf(d) === 'light');
  const card = (i, c, t, meta, body) => `<section class="w"><header class="wh">${icon(i, 'wi ' + c)}<h3>${t}${meta ? `<small>${meta}</small>` : ''}</h3></header>${body}</section>`;
  const side = [];
  if (ls.length) {
    side.push(card('light', 'amber', 'Chiếu sáng', `${ls.filter(isOn).length}/${ls.length} đang bật`,
      `<div class="lights">${ls.map(d => `<div class="lt${isOn(d) ? ' on' : ''}${offline(d) ? ' lost' : ''}">${icon('light')}<span title="${esc(d.name)}">${esc(d.name)}</span>${sw(d, 'amber')}</div>`).join('')}</div>
      <div class="kv" style="gap:8px"><button class="pill amber" data-lights="r:${esc(z.id)}:on">Bật tất cả</button><button class="pill" data-lights="r:${esc(z.id)}:off">Tắt tất cả</button></div>`));
  }
  if (ss.length) {
    const pm = ss.find(e => e.unit === 'µg/m³' && has(e)), a = pm ? aqi(num(pm.id)) : NaN, [lb, cls] = isNaN(a) ? ['', ''] : aqiLabel(a);
    side.push(card('leaf', 'green', 'Khí hậu', isNaN(a) ? '' : `Không khí: ${lb.toLowerCase()}`,
      `<div class="kv">${ss.map(e => `<span>${esc(e.name)} <b>${val(e)}</b>${e.unit ? ' ' + esc(e.unit) : ''}</span>`).join('')}</div>${isNaN(a) ? '' : `<div><span class="tag ${cls}">Chỉ số ${a}</span></div>`}`));
  }
  if (!isNaN(aw)) side.push(card('bolt', 'amber', 'Điện', 'Đo tại MCB của phòng', `<div class="big">${fw(aw)}<small>đang dùng</small></div>
    ${readingsFor(z.id)}<button class="pill" data-go="dien">${icon('bolt')}Xem điện năng cả nhà</button>`));
  if (!side.length) side.push(card('home', '', 'Phòng này chưa có thiết bị', '', '<p class="empty">Khi thêm thiết bị vào hub và gán vào phòng này, chúng sẽ hiện ở đây.</p>'));
  // Hàng dưới: thiết bị có nút điều khiển trước, rồi ổ cắm / bơm, camera, hạ tầng.
  const rank = d => RICH.has(kindOf(d)) ? 0 : PASSIVE.has(kindOf(d)) ? 2 : 1;
  const rest = ds.filter(d => !['light', 'ac'].includes(kindOf(d))).sort((a, b) => rank(a) - rank(b));
  const cards = [], seen = new Set();
  for (const d of rest) {
    if (!d.group) cards.push(devCard(d));
    else if (!seen.has(d.group)) { seen.add(d.group); cards.push(stripCard(rest.filter(x => x.group === d.group))); }
  }
  return [side.join(''), (acsIn(z.id).length ? acCard(z.id) : '') + cards.join(''), true];
}
const readingsFor = zid => {
  const r = cfg.entities.filter(e => e.zone === zid && e.device && e.unit !== 'W' && has(e));
  return r.length ? `<div class="reads">${r.map(e => `<span>${esc(e.name)} <b>${val(e)} ${esc(e.unit)}</b></span>`).join('')}</div>` : '';
};
const btn = (id, code, label, inner = label, cls = 'rb') =>
  `<button class="${cls}" data-act="${esc(id)}" data-code="${code}" data-label="${label}" aria-label="${label}">${inner}</button>`;
// Ổ cắm nhiều lỗ: công tắc tổng + nút từng lỗ (mỗi lỗ là một thiết bị cùng group).
function stripCard(ds) {
  const on = ds.some(isOn), name = ds[0].group, lost = ds.every(offline);
  return `<div class="dv k-socket${on ? ' on' : ''}${lost ? ' lost' : ''}"><div class="dvh"><span class="dvi">${icon('socket')}</span><div><b>${esc(name)}</b>${lost ? '<small>Mất kết nối</small>' : ''}</div>
    <button class="sw${on ? ' on' : ''}" data-strip="${esc(ds[0].id)}" ${lost ? 'disabled' : ''} aria-pressed="${on}" aria-label="${on ? 'Tắt' : 'Bật'} cả ${esc(name)}"></button></div>
    <div class="outs">${ds.map(d => `<button class="out${isOn(d) ? ' on' : ''}${pending.has(d.id) ? ' pend' : ''}" data-dev="${esc(d.id)}" ${offline(d) ? 'disabled' : ''} aria-pressed="${isOn(d)}" aria-label="Bật/tắt ${esc(d.name)}">${esc(d.name)}</button>`).join('')}</div></div>`;
}
// Camera: thẻ vuông, chạm để xem lớn. Chưa nối đầu thu nên dùng ảnh phòng làm hình minh hoạ.
const camCard = d => `<button class="dv cam1${offline(d) ? ' lost' : ''}" data-sheet="cam:${esc(d.id)}" style="${thumbBg(zone(d.zone))}" aria-label="Xem ${esc(d.name)}">
  <i class="dot ${offline(d) ? 'bad' : 'ok'}"></i><b>${esc(d.name)}</b></button>`;
function camPanel(id) {
  const d = cfg.devices.find(x => x.id === id), z = zone(d.zone);
  return `<div class="m-box sheet camsheet"><header class="wh">${icon('camera', 'wi blue')}<h3>${esc(d.name)}<small>${esc(z?.name || '')}</small></h3>
    <span class="tag ${offline(d) ? 'bad' : 'good'}">${offline(d) ? 'Mất kết nối' : 'Đang ghi hình'}</span></header>
    <div class="camview" style="${thumbBg(z)}"></div>
    <p class="note">Hình trực tiếp sẽ hiện khi hub nối với đầu thu camera ở tum kỹ thuật; hiện là ảnh minh hoạ của phòng.</p>
    <div class="m-act"><button value="1" class="m-btn ok">Xong</button></div></div>`;
}
// Mức gió quạt: ba vạch tăng dần, vạch vượt mức thì mờ.
const spd = n => `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true">${[15, 11, 7].map((y, i) => `<path d="M${6 + i * 6} 19V${y}" stroke-width="3"${i >= n ? ' opacity=".3"' : ''}/>`).join('')}</svg>`;
function devCard(d) {
  const k = kindOf(d), on = k === 'vacuum' ? xs(d, 'status') === 'cleaning' : isOn(d), lost = offline(d);
  const head = (st, right, open = '') => {
    const who = `<span class="dvi">${icon(k)}</span><div><b>${esc(d.name)}${d.protected ? icon('lock', 'lock') : ''}</b>${st ? `<small>${st}</small>` : ''}</div>`;
    return `<div class="dvh">${open ? `<button class="dvo" data-sheet="${open}" aria-label="Mở toàn bộ điều khiển ${esc(d.name)}">${who}</button>` : who}${right}</div>`;
  };
  const b = (...a) => btn(d.id, ...a);
  let body = '', st = '', right = '';
  if (k === 'breaker') return `<div class="dv${on ? ' on' : ''}">${head(lost ? 'Mất kết nối' : '', sw(d))}${reads(d)}</div>`;
  if (k === 'camera') return camCard(d);
  if (PASSIVE.has(k)) return `<div class="dv k-${k}${lost ? ' lost' : ''}">${head(lost ? 'Mất kết nối' : '', '')}</div>`;
  if (k === 'vacuum') {
    const vs = xs(d, 'status') || 'docked';
    st = VAC[vs] || esc(vs);
    right = xs(d, 'battery') ? `<span class="batt">${icon('battery')} ${esc(xs(d, 'battery'))}%</span>` : '';
    body = [['start', 'Bắt đầu lau', 'play', 'cleaning'], ['pause', 'Tạm dừng', 'pause', 'paused'], ['dock', 'Về dock', 'dock', 'returning']]
      .map(([c, l, i, s]) => b(c, l, icon(i), 'rb f' + (vs === s ? ' sel' : ''))).join('');
  } else if (IR.has(k)) {
    right = sw(d, k === 'fan' ? 'blue' : '');
    const vol = `<span class="wide">${b('vol_down', 'Giảm âm lượng', '−')}<span>${icon('vol')}</span>${b('vol_up', 'Tăng âm lượng', '+')}</span>`;
    if (k === 'fan') body = `<span class="seg wide">${[1, 2, 3].map(n => b('speed_' + n, `Tốc độ ${n}`, spd(n), 'rb' + (xs(d, 'speed') === String(n) ? ' sel' : ''))).join('')}</span>` +
      b('swing', 'Đảo gió', icon('swing'), 'rb sq' + (xs(d, 'swing') === '1' ? ' sel' : ''));
    if (k === 'tv') body = vol;
    if (k === 'speaker') body = b('play', xs(d, 'play') === '1' ? 'Dừng' : 'Phát', icon(xs(d, 'play') === '1' ? 'pause' : 'play'), 'rb sq') + vol;
  } else {
    st = lost ? 'Mất kết nối' : !known(d) ? 'Chưa rõ trạng thái' : '';
    right = sw(d);
  }
  return `<div class="dv k-${k}${on ? ' on' : ''}${lost ? ' lost' : ''}"${IR.has(k) ? ' title="Điều khiển hồng ngoại: không đọc được trạng thái thật"' : ''}>${head(st, right, k === 'tv' ? 'tv:' + esc(d.id) : '')}${body ? `<div class="remote">${body}</div>` : ''}${reads(d)}</div>`;
}
function reads(d) {
  const r = readingsOf(d).filter(has);
  return r.length ? `<div class="reads">${r.map(e => `<span>${esc(e.name)} <b>${val(e)} ${esc(e.unit)}</b></span>`).join('')}</div>` : '';
}
// Nhạc: loa đang phát trong phạm vi (cả nhà, một tầng, một phòng). Âm lượng: bấm biểu tượng để mở thanh trượt dọc.
let volOpen = '';
// Thu gọn chỉ còn đĩa nhạc (quay khi đang phát); bấm đĩa để trượt mở / thu lại.
let playerOpen = false, playerSlide = false;
function player(f, zid) {
  const sps = cfg.devices.filter(d => kindOf(d) === 'speaker' && (zid ? d.zone === zid : !f || floorOf(d) === f));
  const sp = sps.find(d => xs(d, 'play') === '1') || sps[0];
  if (!sp) return '';
  const playing = xs(sp, 'play') === '1', b = (...a) => btn(sp.id, ...a), tr = xs(sp, 'track'), vol = Number(xs(sp, 'vol') ?? 50);
  const slide = playerSlide;
  playerSlide = false;
  // độ trễ âm theo đồng hồ để đĩa không giật về 0° mỗi lần vẽ lại
  const disc = `<button class="disc${playing ? ' spin' : ''}" data-pl style="animation-delay:-${(Date.now() % 4000) / 1000}s" aria-expanded="${playerOpen}" aria-label="${playerOpen ? 'Thu gọn' : 'Mở'} trình phát nhạc"></button>`;
  if (!playerOpen) return `<div class="player mini">${disc}</div>`;
  return `<div class="player${slide ? ' slide' : ''}">${disc}
    <span class="pt"><b>${esc(tr || sp.name)}</b><small>${esc(tr ? sp.name + (zone(sp.zone) ? ', ' + zone(sp.zone).name : '') : 'Điều khiển hồng ngoại')}</small></span>
    ${b('play', playing ? 'Dừng' : 'Phát', icon(playing ? 'pause' : 'play'), 'rb big')}${b('next', 'Bài sau', icon('next'))}
    <span class="volw"><button class="rb" data-vol="${esc(sp.id)}" aria-label="Âm lượng" aria-expanded="${volOpen === sp.id}">${icon('vol')}</button>
    ${volOpen === sp.id ? `<span class="volpop"><span class="vtrack" data-volt="${esc(sp.id)}" style="--v:${vol}%" role="slider" tabindex="0" aria-label="Âm lượng ${esc(sp.name)}" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${vol}"><i></i><b></b></span><small>${vol}</small></span>` : ''}</span></div>`;
}
// Thanh âm lượng dọc tự vẽ bằng Pointer Events: input range xoay dọc không kéo được bằng cảm ứng trên Safari iPad.
let volDrag = null;
const volAt = (t, e) => { const r = t.getBoundingClientRect(); return Math.round(Math.min(1, Math.max(0, (r.bottom - e.clientY) / r.height)) * 20) * 5; };
const volShow = (t, v) => { t.style.setProperty('--v', v + '%'); t.setAttribute('aria-valuenow', v); t.nextElementSibling.textContent = v; };
document.addEventListener('pointerdown', e => {
  const t = e.target.closest('[data-volt]');
  if (!t) return;
  e.preventDefault();
  t.setPointerCapture(e.pointerId);
  volDrag = t;
  volShow(t, volAt(t, e));
});
document.addEventListener('pointermove', e => { if (volDrag) volShow(volDrag, volAt(volDrag, e)); });
const volEnd = () => {
  if (!volDrag) return;
  const t = volDrag;
  volDrag = null;
  // gửi sau sự kiện click của lần thả tay, để click không rơi ra ngoài thanh (đã vẽ lại) và đóng thanh
  setTimeout(() => setVolume(t.dataset.volt, Number(t.getAttribute('aria-valuenow'))));
};
document.addEventListener('pointerup', volEnd);
document.addEventListener('pointercancel', volEnd);
document.addEventListener('keydown', e => {
  const t = e.target.closest?.('[data-volt]'), dir = {ArrowUp: 5, ArrowRight: 5, ArrowDown: -5, ArrowLeft: -5}[e.key];
  if (!t || !dir) return;
  e.preventDefault();
  setVolume(t.dataset.volt, Math.min(100, Math.max(0, Number(t.getAttribute('aria-valuenow')) + dir)));
});
// Loa hồng ngoại không đặt được mức tuyệt đối: đổi mức thành số lần tăng/giảm (mỗi nấc 5%).
async function setVolume(id, nv) {
  const d = cfg.devices.find(x => x.id === id), k = `x:${d.id}:vol`, old = Number(state[k] ?? 50);
  const steps = Math.round((nv - old) / 5);
  for (let i = 0; i < Math.abs(steps); i++) if (!(await act(d.id, steps > 0 ? 'vol_up' : 'vol_down', 'Âm lượng', true))) { draw(); return toast(`Hub chưa gửi được lệnh tới ${d.name}`); }
  state[k] = String(nv);
  draw();
}

/* ---------- Điện năng ---------- */
// Sơ đồ vẽ trọn trong một SVG tỉ lệ cố định nên đường nối luôn khớp nút và không đè lên chữ.
// Mỗi path đi từ nguồn tới đích (vào/ra tâm inverter, bị vòng tròn che); rev = chảy ngược (bán lên lưới, sạc pin).
function flowMap() {
  const pv = num('deye.pv'), grid = num('deye.grid'), load = num('deye.load'), soc = num('jk.soc'), bat = num('jk.power');
  if ([pv, grid, load, soc, bat].every(isNaN)) return '';
  const live = w => !isNaN(w) && Math.abs(w) >= 5;
  const fl = (d, w, c, rev) => `<path class="trk" d="${d}"/>${live(w) ? `<path class="fl${rev ? ' rev' : ''}" d="${d}" style="--c:${c}"/>` : ''}`;
  const node = (x, y, ic, c, on, value, label) => `<g class="fn${on ? ' on' : ''}" style="--c:${c}"><circle cx="${x}" cy="${y}" r="34"/>
    <g class="fic" transform="translate(${x - 13} ${y - 13}) scale(1.0833)">${ICON[ic]}</g>
    <text class="v" x="${x}" y="${y + 64}">${value}</text><text class="l" x="${x}" y="${y + 86}">${label}</text></g>`;
  const gridLbl = isNaN(grid) ? 'Điện lưới' : grid > 5 ? 'Đang mua từ lưới' : grid < -5 ? 'Đang bán lên lưới' : 'Lưới, không dùng';
  const batLbl = isNaN(bat) ? 'Pin lưu trữ' : bat > 5 ? `Đang sạc ${fw(bat)}` : bat < -5 ? `Đang xả ${fw(bat)}` : 'Pin nghỉ';
  return `<svg class="flowmap" viewBox="0 0 640 410" role="img" aria-label="Dòng điện giữa mặt trời, lưới, pin và nhà">
    ${fl('M120 90 H276 Q296 90 296 110 V200', pv, 'var(--amber)')}${fl('M520 90 H364 Q344 90 344 110 V200', grid, '#4cc9f0', grid < 0)}
    ${fl('M120 310 H276 Q296 310 296 290 V200', bat, 'var(--green)', bat > 0)}${fl('M344 200 V290 Q344 310 364 310 H520', load, '#e8eef4')}
    ${node(120, 90, 'sun', 'var(--amber)', live(pv), fw(pv), 'Mặt trời')}${node(520, 90, 'grid', '#4cc9f0', live(grid), fw(Math.abs(grid)), gridLbl)}
    ${node(120, 310, 'battery', 'var(--green)', live(bat), isNaN(soc) ? '—' : `${nf.format(soc)}%`, batLbl)}${node(520, 310, 'home', '#e8eef4', live(load), fw(load), 'Nhà đang dùng')}
    <g class="fn on" style="--c:#e8eef4"><circle cx="320" cy="200" r="34"/><g class="fic" transform="translate(307 187) scale(1.0833)">${ICON.bolt}</g></g>
    <text class="v s st" x="368" y="196">Deye</text><text class="l st" x="368" y="216">Inverter</text></svg>`;
}
// Thanh xếp chồng: tổng trong ngày chia theo nguồn / nơi dùng.
function split(title, total, segs) {
  const pc = v => total > 0 ? Math.max(0, v) / total * 100 : 0;
  return `<div class="split"><div class="sh"><span>${title}</span><b>${nf.format(total)}<small> kWh</small></b></div>
    <div class="sbar">${segs.filter(([, v]) => v > 0).map(([, v, c]) => `<i style="width:${pc(v).toFixed(1)}%;background:${c}"></i>`).join('')}</div>
    ${segs.map(([l, v, c]) => `<div class="dli"><i style="background:${c}"></i><span>${l}</span><b>${nf.format(Math.max(0, v))}</b><small>${Math.round(pc(v))}%</small></div>`).join('')}</div>`;
}
function areaBars(rows, total, limit = 99) {
  const max = Math.max(1, ...rows.map(([, w]) => w));
  const bar = (name, sub, w, go) => `<${go ? `button data-go="${go}"` : 'div'} class="bar2"><span>${name}<small>${sub}</small></span>
    <span class="bt"><i style="width:${Math.min(100, w / max * 100).toFixed(1)}%"></i></span><b>${fw(w)}</b><small class="bp">${total > 0 ? Math.round(w / total * 100) : 0}%</small></${go ? 'button' : 'div'}>`;
  const rest = num('deye.load') - rows.reduce((a, [, w]) => a + w, 0);
  return `<div class="bars">${rows.slice(0, limit).map(([z, w]) => bar(esc(z.name), esc(floorName(z.floor)), w, `${z.floor}/${esc(z.id)}`)).join('')}
    ${limit >= rows.length && rest > 5 ? bar('Chưa đo riêng', 'Chênh giữa inverter và MCB', rest, '') : ''}</div>`;
}
function energyZones() {
  const [total] = totalPower(), t = id => num('deye.' + id), today = [];
  const rows = cfg.zones.filter(z => z.type !== 'panel').map(z => [z, areaW(z.id)]).filter(([, w]) => !isNaN(w)).sort((a, b) => b[1] - a[1]);
  const pvT = t('pv_today'), loadT = t('load_today');
  if (!isNaN(pvT)) { const ch = t('charge_today') || 0, se = t('sell_today') || 0; today.push(split('Điện mặt trời', pvT, [['Sạc pin', ch, 'var(--green)'], ['Nhà dùng', pvT - ch - se, 'var(--amber)'], ['Bán lên lưới', se, '#4cc9f0']])); }
  if (!isNaN(loadT)) { const dc = t('discharge_today') || 0, by = t('buy_today') || 0; today.push(split('Nhà tiêu thụ', loadT, [['Từ pin', dc, 'var(--green)'], ['Từ mặt trời', loadT - dc - by, 'var(--amber)'], ['Mua từ lưới', by, '#4cc9f0']])); }
  return [`<section class="w"><header class="wh">${icon('sun', 'wi amber')}<h3>Hôm nay</h3></header>${today.join('') || '<p class="empty">Chưa có số liệu trong ngày từ inverter Deye. Sẽ hiện khi lắp node RS485 ở tum kỹ thuật.</p>'}</section>
    <section class="w"><header class="wh">${icon('bolt', 'wi amber')}<h3>Theo khu vực<small>Đo tại MCB từng khu</small></h3></header>
    ${rows.length ? areaBars(rows, total) : '<p class="empty">Chưa có MCB đo điện nào được gán vào khu vực.</p>'}</section>`, ''];
}

/* ---------- Cài đặt ---------- */
// Chỉnh được ngay: ngày/đêm, bố cục widget. Phòng, thiết bị, ngữ cảnh: xem tại đây, sửa trong config.yaml trên hub.
function settingsBox() {
  const byFloor = floors().map(f => [f, cfg.zones.filter(z => z.floor === f)]);
  return `<div class="ebox set"><div class="wh">${icon('layers', 'wi amber')}<h3>Phòng và thiết bị<small>${cfg.zones.length} khu vực, ${cfg.devices.length} thiết bị, ${cfg.entities.length} số đo</small></h3></div>
    <div class="setlist">${byFloor.map(([f, zs]) => `<h4>${esc(floorName(f))}</h4>${zs.map(z => { const ds = devsIn(z.id), ks = [...new Set(ds.map(kindOf))];
      return `<button class="setrow" data-go="${f}/${esc(z.id)}"><span>${esc(z.name)}</span><span class="ks">${ks.map(k => `<i title="${k}">${icon(k)}<b>${ds.filter(d => kindOf(d) === k).length}</b></i>`).join('')}</span>${icon('more')}</button>`; }).join('')}`).join('')}</div>
    <p class="note">Thêm, đổi tên, gán phòng cho thiết bị: sửa config.yaml trên hub rồi khởi động lại hubd.</p></div>`;
}
function settingsZones() {
  const card = (i, c, t, meta, body) => `<section class="w"><header class="wh">${icon(i, 'wi ' + c)}<h3>${t}${meta ? `<small>${meta}</small>` : ''}</h3></header>${body}</section>`;
  const s = sky();
  return [card('auto', 'amber', 'Ngày và đêm', skyMode === 'auto' ? `Tự động theo ${s.by}` : 'Chọn tay', `<div class="seg">${[['auto', 'Tự động'], ['day', 'Ban ngày'], ['night', 'Ban đêm']]
      .map(([v, l]) => `<button class="rb${skyMode === v ? ' sel' : ''}" data-skyset="${v}">${l}</button>`).join('')}</div>`) +
    card('grip', '', 'Bố cục widget', 'Lưu trên thiết bị này', `<div class="kv" style="gap:8px"><button class="pill" data-go="" data-editgo>Sắp xếp trang chủ</button><button class="pill" data-wreset>Về mặc định</button></div>`) +
    card('spark', 'amber', 'Ngữ cảnh', `${(cfg.scenes || []).length} ngữ cảnh`, `<div class="list">${(cfg.scenes || []).map(sc => `<div class="setrow"><span>${icon(sc.icon || 'spark')}${esc(sc.name)}</span><small>${(sc.steps || []).length} bước</small></div>`).join('')}</div>`),
    ''];
}

/* ---------- Kéo thả widget (Pointer Events: chạy được với cảm ứng iPad) ---------- */
document.addEventListener('pointerdown', e => {
  const grip = editing && e.target.closest('[data-grip]');
  if (!grip) return;
  e.preventDefault();
  const el = grip.closest('.w'), r = el.getBoundingClientRect(), ph = document.createElement('div');
  ph.className = el.className + ' ph';
  ph.style.height = `${r.height}px`;
  el.after(ph);
  Object.assign(el.style, {position: 'fixed', left: `${r.left}px`, top: `${r.top}px`, width: `${r.width}px`, height: `${r.height}px`});
  el.classList.add('dragging');
  drag = {el, ph, zone: el.parentElement, dx: e.clientX - r.left, dy: e.clientY - r.top};
});
document.addEventListener('pointermove', e => {
  if (!drag) return;
  drag.el.style.left = `${e.clientX - drag.dx}px`;
  drag.el.style.top = `${e.clientY - drag.dy}px`;
  const over = document.elementFromPoint(e.clientX, e.clientY)?.closest('.wz .w:not(.ph):not(.dragging)');
  if (!over || over.parentElement !== drag.zone) return;   // chỉ đổi chỗ trong cùng một vùng
  const r = over.getBoundingClientRect(), horiz = drag.zone.id === 'bottom' || over.classList.contains('half');
  const after = horiz ? e.clientX > r.left + r.width / 2 : e.clientY > r.top + r.height / 2;
  if (after ? over.nextElementSibling !== drag.ph : over.previousElementSibling !== drag.ph) over[after ? 'after' : 'before'](drag.ph);
});
function endDrag() {
  if (!drag) return;
  const {el, ph, zone: z} = drag;
  drag = null;
  ph.replaceWith(el);
  el.removeAttribute('style');
  el.classList.remove('dragging');
  layout[pageType()][z.dataset.zone] = [...z.children].map(w => w.dataset.w).filter(Boolean);
  saveLayout();
  shellKey = '';
  draw();
}
addEventListener('pointerup', endDrag);
addEventListener('pointercancel', endDrag);

/* ---------- Điều hướng: hướng chuyển động theo lên/xuống tầng hay vào/ra phòng ---------- */
function parseHash() {
  if (location.hash.startsWith('#/dien')) return {floor: 0, room: '', page: 'energy'};
  if (location.hash.startsWith('#/caidat')) return {floor: 0, room: '', page: 'settings'};
  const m = location.hash.match(/^#\/t(\d+)(?:\/([\w-]+))?/);
  const f = m ? +m[1] : 0, r = m && m[2] || '';
  if (!floors().includes(f)) return {floor: 0, room: '', page: ''};
  return {floor: f, room: rooms(f).some(z => z.id === r) ? r : '', page: ''};
}
function animate(el, cls) {
  el.classList.remove('go-up', 'go-down', 'go-in', 'go-out', 'go-side');
  void el.offsetWidth;   // chạy lại animation
  el.classList.add(cls);
}
function go(f, room = '', from = null, page = '') {
  if (f === view.floor && room === view.room && page === view.page) return;
  const depth = (ff, r, p) => r ? 2 : ff || p ? 1 : 0, a = depth(view.floor, view.room, view.page), b = depth(f, room, page);
  const dir = f && view.floor && f !== view.floor ? (f > view.floor ? 'up' : 'down') : b > a ? 'in' : b < a ? 'out' : 'side';
  const v = $('#view');
  if (from) { const r = from.getBoundingClientRect(); v.style.setProperty('--ox', `${r.left + r.width / 2}px`); v.style.setProperty('--oy', `${r.top + r.height / 2}px`); }
  view = {floor: f, room, page};
  editing = false;
  const h = '#/' + (page ? {energy: 'dien', settings: 'caidat'}[page] : f ? `t${f}` + (room ? `/${room}` : '') : '');
  if (location.hash !== h) history.pushState(null, '', h);
  draw();
  animate(v, 'go-' + dir);
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
// Modal thay cho confirm()/alert() của trình duyệt. Trả về true khi chọn nút hành động.
function ask({title, text, ok, danger = false, html = ''}) {
  return new Promise(res => {
    const m = $('#modal');
    m.innerHTML = `<div class="m-box"><h3>${esc(title)}</h3>${text ? `<p>${esc(text)}</p>` : ''}${html}
      <div class="m-act">${ok ? '<button value="0" class="m-btn">Huỷ</button>' : ''}<button value="1" class="m-btn ${danger ? 'danger' : 'ok'}">${esc(ok || 'Đóng')}</button></div></div>`;
    m.onclick = e => {
      const g = e.target.closest('[data-go]');
      if (g) { m.close('0'); return; }   // điều hướng do trình xử lý chung làm
      const b = e.target.closest('.m-act button');
      if (b) m.close(b.value); else if (e.target === m) m.close('0');
    };
    m.onclose = () => res(m.returnValue === '1');
    m.returnValue = '';
    m.showModal();
    m.querySelector('.m-btn').focus();   // mặc định vào nút đầu (Huỷ) để Enter không bấm nhầm
  });
}
function showAlerts() {
  const lost = cfg.devices.filter(offline);
  ask({title: lost.length ? `${lost.length} thiết bị cần chú ý` : 'Không có cảnh báo', text: lost.length ? 'Các thiết bị dưới đây đang mất kết nối với hub.' : 'Mọi thiết bị đang kết nối bình thường.',
    html: lost.length ? `<div class="list">${lost.map(d => `<button class="li" data-go="${where(d)}"><span class="lic bad">${icon(kindOf(d))}</span>
      <span>${esc(d.name)}<small>${esc(zone(d.zone)?.name || 'Chưa gán phòng')}</small></span></button>`).join('')}</div>` : ''});
}
const post = body => api('/api/cmd', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
async function setDev(d, to) {
  if (DEMO) { state['dev:' + d.id] = to.toUpperCase(); return true; }
  pending.add(d.id);
  const r = await post({device: d.id, to});
  setTimeout(() => { if (pending.delete(d.id)) soon(); }, r.ok ? 5000 : 0);   // trạng thái thật về qua SSE
  return r.ok;
}
async function toggle(id) {
  const d = cfg.devices.find(x => x.id === id);
  if (!d || offline(d)) return;
  const to = isOn(d) ? 'off' : 'on';
  if (d.protected && !(await ask({title: `${to === 'off' ? 'Tắt' : 'Bật'} ${d.name}?`,
    text: `Thiết bị này được bảo vệ để tránh bấm nhầm.${kindOf(d) === 'breaker' && to === 'off' ? ' Tắt CB sẽ ngắt điện cả khu vực.' : ''}`,
    ok: to === 'off' ? 'Tắt' : 'Bật', danger: to === 'off'}))) return;
  const p = setDev(d, to);
  draw();
  if (!(await p)) toast(`Không gửi được lệnh tới ${d.name}`);
  draw();
}
// Bật/tắt cả nhóm thiết bị (bỏ qua thiết bị được bảo vệ, đang mất kết nối). Hồng ngoại: chỉ bấm nguồn khi khác trạng thái giả định.
async function setMany(ds, to) {
  let n = 0, bad = 0;
  for (const d of ds.filter(x => !x.protected && !offline(x) && isOn(x) !== (to === 'on'))) {
    const ok = IR.has(kindOf(d)) ? await act(d.id, 'power', 'Nguồn', true) : await setDev(d, to);
    ok ? n++ : bad++;
  }
  draw();
  return [n, bad];
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
  else if (/^(speed|mode|fan|temp)_/.test(code)) {   // đặt giá trị: speed_2, mode_cool, fan_auto, temp_24
    const [key, v] = code.split('_');
    state[k(key)] = key === 'temp' ? String(Math.min(30, Math.max(16, Number(v)))) : v;
    if (key === 'speed') state['dev:' + id] = 'ON';
  }
  else if (['play', 'swing', 'sleep', 'turbo'].includes(code)) state[k(code)] = state[k(code)] === '1' ? '0' : '1';
  else if (code === 'start') state[k('status')] = 'cleaning';
  else if (code === 'pause') state[k('status')] = 'paused';
  else if (code === 'dock') state[k('status')] = 'returning';
  if (!quiet) { draw(); toast(`Đã gửi “${label}” tới ${d.name}`); }
  return true;
}
// Cảnh: chuỗi bước {d, to: on|off} hoặc {d, act}.
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
  lastScene = i;
  draw();
  toast(`Đã chạy “${sc.name}”: ${n} lệnh${bad ? `, ${bad} lệnh lỗi` : ''}`);
}

document.addEventListener('click', async e => {
  const t = e.target, pick = sel => t.closest(sel);
  let el;
  if ((el = pick('[data-vol]'))) { volOpen = volOpen === el.dataset.vol ? '' : el.dataset.vol; return draw(); }
  if (volOpen && !pick('.volw')) { volOpen = ''; draw(); }
  if ((el = pick('[data-pl]'))) {
    if (!playerOpen) { playerOpen = playerSlide = true; return draw(); }
    el.parentElement.classList.add('closing');
    return setTimeout(() => { playerOpen = false; volOpen = ''; draw(); }, 260);
  }
  if (DEBUG_ZONES && !pick('button') && (el = pick('.stage:not(.energy)'))) {   // chạm ảnh để lấy toạ độ % đặt nhãn
    const box = el.querySelector('.cover').getBoundingClientRect();
    return toast(`x: ${((e.clientX - box.left) / box.width * 100).toFixed(1)}, y: ${((e.clientY - box.top) / box.height * 100).toFixed(1)}`);
  }
  if ((el = pick('[data-sheet]'))) return openSheet(el.dataset.sheet);
  if ((el = pick('[data-acg]'))) return acGroup(el.dataset.acg, el.dataset.code, el.dataset.label);
  if ((el = pick('[data-act]'))) return act(el.dataset.act, el.dataset.code, el.dataset.label);
  if ((el = pick('[data-scene]'))) return runScene(+el.dataset.scene);
  if ((el = pick('[data-strip]'))) {
    const d0 = cfg.devices.find(x => x.id === el.dataset.strip), ds = cfg.devices.filter(x => x.group === d0.group && x.zone === d0.zone);
    const [, bad] = await setMany(ds, ds.some(isOn) ? 'off' : 'on');
    return bad ? toast(`${bad} lỗ của ${d0.group} chưa nhận lệnh`) : undefined;
  }
  if ((el = pick('[data-rain]')) && DEMO) { state[rainE().id] = raining() ? 'Không' : 'Có'; return draw(); }
  if ((el = pick('[data-dev]'))) return toggle(el.dataset.dev);
  if ((el = pick('[data-group]'))) {
    const [f, k, to] = el.dataset.group.split(':'), [n, bad] = await setMany(devsOf(+f, k), to);
    return toast(`${to === 'on' ? 'Đã bật' : 'Đã tắt'} ${n} thiết bị${bad ? `, ${bad} lỗi` : ''}`);
  }
  if ((el = pick('[data-lights]'))) {
    const p = el.dataset.lights.split(':'), to = p.at(-1);
    const ds = p[0] === 'r' ? devsIn(p[1]).filter(d => kindOf(d) === 'light') : devsOf(+p[0], 'light');
    const [n, bad] = await setMany(ds, to);
    return toast(`${to === 'on' ? 'Đã bật' : 'Đã tắt'} ${n} đèn${bad ? `, ${bad} đèn không nhận lệnh` : ''}`);
  }
  if (pick('[data-alerts]') || pick('#bell')) return showAlerts();
  if (pick('#sky')) {
    skyMode = SKY[(SKY.indexOf(skyMode) + 1) % SKY.length];
    try { localStorage.setItem('hubd.sky', skyMode); } catch (_) { /* bỏ qua */ }
    toast(skyMode === 'auto' ? 'Ngày/đêm: tự động' : skyMode === 'day' ? 'Ngày/đêm: luôn ban ngày' : 'Ngày/đêm: luôn ban đêm');
    shellKey = '';
    return draw();
  }
  if (pick('[data-edit]')) { editing = !editing; return draw(); }
  if (pick('[data-editgo]')) { go(0); editing = true; return draw(); }
  if (pick('[data-wreset]')) {
    try { localStorage.removeItem('hubd.layout3'); } catch (_) { /* bỏ qua */ }
    layout = loadLayout();
    shellKey = '';
    toast('Bố cục widget đã về mặc định');
    return draw();
  }
  if ((el = pick('[data-wsize]'))) { const [id, v] = el.dataset.wsize.split(':'); return setOpt(id, {size: v}); }
  if ((el = pick('[data-wpick]'))) {
    const v = el.dataset.wpick, id = v.slice(0, v.indexOf(':')), k = v.slice(v.indexOf(':') + 1), off = wOpt(id).off || [];
    return setOpt(id, {off: off.includes(k) ? off.filter(x => x !== k) : [...off, k]});
  }
  if ((el = pick('[data-skyset]'))) { skyMode = el.dataset.skyset; try { localStorage.setItem('hubd.sky', skyMode); } catch (_) { /* bỏ qua */ } return draw(); }
  if ((el = pick('[data-whide]'))) {
    const L = layout[pageType()], id = el.dataset.whide;
    L.hidden = L.hidden.includes(id) ? L.hidden.filter(x => x !== id) : [...L.hidden, id];
    saveLayout();
    return draw();
  }
  if (pick('[data-grip]')) return;
  if ((el = pick('[data-go]'))) {
    if (el.dataset.go === 'dien') return go(0, '', el, 'energy');
    if (el.dataset.go === 'caidat') return go(0, '', el, 'settings');
    const [f, r] = el.dataset.go.split('/');
    return go(Number(f) || 0, r || '', el);
  }
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
  es.onerror = () => { setConn('bad'); es.close(); setTimeout(connect, retry); retry = Math.min(retry * 2, 15000); };
}
document.addEventListener('visibilitychange', () => { if (!DEMO && !document.hidden && es && es.readyState === 2) connect(); });

async function init() {
  $('#loginbox').innerHTML = LOGIN_HTML;
  if (DEMO) {
    cfg = DEMO_DATA.cfg; state = {...DEMO_DATA.state};
    $('#conn').hidden = true;
    $('#demo-chip').innerHTML = '<a class="chipdemo" href="/" title="Về dữ liệu thật của nhà">Dữ liệu mẫu</a>';
  } else {
    cfg = await (await api('/api/config')).json();
    state = await (await api('/api/state')).json();
    connect();
  }
  layout = loadLayout();
  view = parseHash();
  draw();
  // tải trước mọi ảnh nền để chuyển tầng / phòng không bị nền đen lúc ảnh đang tải
  for (const sc of [SCENE.home, ...Object.values(SCENE.floors)]) for (const u of [sc.day, sc.night]) new Image().src = u;
  for (const u of Object.values(SCENE.rooms)) new Image().src = u;
  setInterval(draw, 30000);   // đồng hồ, chuyển ngày/đêm
}
init();
