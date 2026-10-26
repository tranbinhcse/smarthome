// Dữ liệu mẫu cho /?demo: thiết bị theo danh sách chủ nhà (TV, quạt, máy lạnh hồng ngoại, robot, camera...)
// để dựng và duyệt giao diện trước khi lắp thật. Không gọi API, không gửi lệnh ra thiết bị nào.
const DEMO_DATA = (() => {
  const zones = [
    {id: 'santruoc', name: 'Sân trước', floor: 1, type: 'outdoor'}, {id: 'cafe', name: 'Khu cafe', floor: 1},
    {id: 'bep', name: 'Bếp', floor: 1}, {id: 'kho', name: 'Phòng kho', floor: 1}, {id: 'wc', name: 'WC', floor: 1},
    {id: 'sansau', name: 'Sân sau', floor: 1, type: 'outdoor'},
    {id: 'tudien1', name: 'Tủ điện và Wi-Fi tầng 1', floor: 1, type: 'panel'},
    {id: 'lamviec', name: 'Phòng làm việc', floor: 2}, {id: 'ngu1', name: 'Phòng ngủ 1', floor: 2},
    {id: 'ngu2', name: 'Phòng ngủ 2', floor: 2}, {id: 'docsach', name: 'Phòng đọc sách', floor: 2},
    {id: 'tudien2', name: 'Tủ điện và Wi-Fi tầng 2', floor: 2, type: 'panel'},
    {id: 'kythuat', name: 'Tum kỹ thuật', floor: 3}, {id: 'gieng_troi', name: 'Giếng trời', floor: 3, type: 'outdoor'},
    {id: 'tudien3', name: 'Wi-Fi tầng 3', floor: 3, type: 'panel'},
  ];
  const n = (count, prefix, zone, kind, onIdx = []) =>
    Array.from({length: count}, (_, i) => [`${zone}_${kind}${i + 1}`, `${prefix} ${i + 1}`, zone, kind, onIdx.includes(i + 1) ? 1 : 0]);
  // Ổ cắm nhiều lỗ: mỗi lỗ một thiết bị, chung group (thật: cùng một Tuya, mỗi lỗ một dp).
  const strip = (id, group, zone, on) => ['Ổ 1', 'Ổ 2', 'Ổ 3', 'USB'].map((nm, i) => [`${id}_${i + 1}`, nm, zone, 'socket', on[i], group]);
  // [id, tên, phòng, loại, bật?, nhóm?]
  const devs = [
    // Sân trước: bơm nước, tưới cây, đèn bảng hiệu
    ['st_bom', 'Bơm nước', 'santruoc', 'pump', 0], ['st_tuoi', 'Tưới cây', 'santruoc', 'water', 0],
    ['st_bh', 'Đèn bảng hiệu', 'santruoc', 'light', 1], ['cam_st', 'Camera sân trước', 'santruoc', 'camera', 1],
    // Khu cafe: 8 công tắc đèn, 2 quạt trần, 2 máy lạnh, 1 TV, robot lau nhà, 3 camera (+ ổ cắm hồ cá đang có thật)
    ['cafe_lanh', 'Đèn phòng lạnh', 'cafe', 'light', 1], ...n(7, 'Đèn', 'cafe', 'light', [1, 2, 5]),
    ['cafe_hoca', 'Ổ cắm hồ cá', 'cafe', 'socket', 1], ...strip('cafe_quay', 'Ổ cắm quầy', 'cafe', [1, 1, 0, 1]), ...n(3, 'Camera', 'cafe', 'camera', [1, 2, 3]),
    ...n(2, 'Quạt trần', 'cafe', 'fan', [1]), ...n(2, 'Máy lạnh', 'cafe', 'ac', [1]),
    ['cafe_tv', 'TV', 'cafe', 'tv', 1], ['cafe_loa', 'Loa quán', 'cafe', 'speaker', 1], ['cafe_robot', 'Robot lau nhà', 'cafe', 'vacuum', 0],
    // Bếp: chỉ có CB (thiết bị bếp, tủ lạnh không điều khiển)
    ['cam_bep', 'Camera bếp', 'bep', 'camera', 1],
    // Phòng kho: 1 công tắc, 1 máy lạnh. WC: 3 công tắc
    ['kho_den', 'Đèn kho', 'kho', 'light', 0], ['cam_kho', 'Camera kho', 'kho', 'camera', 1], ['kho_ml', 'Máy lạnh', 'kho', 'ac', 0],
    ...n(3, 'Đèn', 'wc', 'light'),
    ['sansau_den', 'Đèn sân sau', 'sansau', 'light', 1], ['cam_ss', 'Camera sân sau', 'sansau', 'camera', 1],
    ['wifi_t1', 'Router Wi-Fi tầng 1', 'tudien1', 'router', 1],
    // Phòng làm việc: 1 máy lạnh, 1 TV, 1 quạt, 6 công tắc, 1 máy lọc nước, robot lau nhà
    ...n(6, 'Đèn', 'lamviec', 'light', [1, 2]), ['lv_loc', 'Máy lọc nước', 'lamviec', 'socket', 1], ...strip('lv_ban', 'Ổ cắm bàn làm việc', 'lamviec', [1, 0, 0, 1]),
    ['cam_lv', 'Camera phòng làm việc', 'lamviec', 'camera', 1],
    ['lv_ml', 'Máy lạnh', 'lamviec', 'ac', 1], ['lv_tv', 'TV', 'lamviec', 'tv', 0], ['lv_quat', 'Quạt', 'lamviec', 'fan', 0],
    ['lv_robot', 'Robot lau nhà', 'lamviec', 'vacuum', 0],
    // Mỗi phòng ngủ: 1 máy lạnh, 1 công tắc
    ['n1_den', 'Đèn', 'ngu1', 'light', 0], ['n1_ml', 'Máy lạnh', 'ngu1', 'ac', 0],
    ['n2_den', 'Đèn', 'ngu2', 'light', 0], ['n2_ml', 'Máy lạnh', 'ngu2', 'ac', 0],
    // Phòng đọc sách: 1 quạt, 2 công tắc
    ...n(2, 'Đèn', 'docsach', 'light', [1]), ['ds_quat', 'Quạt', 'docsach', 'fan', 1],
    ['wifi_t2', 'Router Wi-Fi tầng 2', 'tudien2', 'router', 1],
    // Tum kỹ thuật: điện, mạng, camera, điều khiển trung tâm
    ['kt_modem', 'Modem Viettel', 'kythuat', 'infra', 1], ['kt_switch', 'Switch mạng', 'kythuat', 'infra', 1],
    ['kt_nvr', 'Đầu ghi camera', 'kythuat', 'infra', 1], ['kt_hub', 'Hub trung tâm', 'kythuat', 'infra', 1],
    ['cam_kt', 'Camera tum', 'kythuat', 'camera', 1], ['cam_gt', 'Camera giếng trời', 'gieng_troi', 'camera', 1],
    ['wifi_t3', 'Router Wi-Fi tầng 3', 'tudien3', 'router', 1],
  ];
  // [id, tên, đơn vị, phòng, giá trị, gắn vào thiết bị]
  const ents = [
    ['cafe.t', 'Nhiệt độ', '°C', 'cafe', '26.8'], ['cafe.h', 'Độ ẩm', '%', 'cafe', '61'],
    ['cafe.co2', 'CO₂', 'ppm', 'cafe', '780'], ['cafe.pm', 'Bụi mịn PM2.5', 'µg/m³', 'cafe', '14'],
    ['lv.t', 'Nhiệt độ', '°C', 'lamviec', '26.1'], ['lv.h', 'Độ ẩm', '%', 'lamviec', '58'], ['lv.co2', 'CO₂', 'ppm', 'lamviec', '690'],
    // Máy lọc nước: TDS nước nguồn và sau lọc, lượng nước đã lọc hôm nay, độ bền lõi lọc
    ['lv.tds_in', 'Nước nguồn', 'ppm', 'lamviec', '185'], ['lv.tds', 'Nước sau lọc', 'ppm', 'lamviec', '12'],
    ['lv.loc_l', 'Đã lọc hôm nay', 'L', 'lamviec', '18.5'], ['lv.loi', 'Lõi lọc còn', '%', 'lamviec', '62'],
    ['cafe.tds_in', 'Nước nguồn', 'ppm', 'cafe', '190'], ['cafe.tds', 'Nước sau lọc', 'ppm', 'cafe', '9'],
    ['cafe.loc_l', 'Đã lọc hôm nay', 'L', 'cafe', '64'], ['cafe.loi', 'Lõi lọc còn', '%', 'cafe', '18'],
    ['n1.t', 'Nhiệt độ', '°C', 'ngu1', '28.3'], ['n2.t', 'Nhiệt độ', '°C', 'ngu2', '28'],
    ['ds.t', 'Nhiệt độ', '°C', 'docsach', '26.5'],
    ['gt.mua', 'Mưa', '', 'gieng_troi', 'Không'], ['gt.lux', 'Ánh sáng', 'lx', 'gieng_troi', '18400'],
    ['gt.t', 'Ngoài trời', '°C', 'gieng_troi', '31.6'], ['gt.h', 'Độ ẩm ngoài trời', '%', 'gieng_troi', '68'],
  ];
  // Ánh sáng giếng trời + điện mặt trời theo giờ thật để chế độ "Tự động" ngày/đêm khớp với bên ngoài.
  const hr = new Date().getHours(), day = hr >= 6 && hr < 18;
  ents.find(e => e[0] === 'gt.lux')[4] = day ? '18400' : '3';

  // Mỗi khu có một MCB đo điện (W) đặt ở tủ điện của tầng. Số đo gắn vào khu (zone) và vào MCB (device).
  const AREA_W = {santruoc: 95, cafe: 620, bep: 310, kho: 40, wc: 0, sansau: 12, lamviec: 260, ngu1: 0, ngu2: 0, docsach: 45, kythuat: 98};
  for (const [z, w] of Object.entries(AREA_W)) {
    const zz = zones.find(x => x.id === z), id = `mcb_${z}`;
    devs.push([id, `MCB ${zz.name}`, `tudien${zz.floor}`, 'breaker', 1]);
    ents.push([`${id}.w`, 'Công suất', 'W', z, String(w), id], [`${id}.v`, 'Điện áp', 'V', z, '229.6', id]);
  }
  // Inverter Deye + pin JK: tải = tổng MCB + phần chưa đo riêng. grid + mua / − bán, jk.power + sạc / − xả.
  const load = Object.values(AREA_W).reduce((a, b) => a + b, 0) + 100, pv = day ? 1840 : 0, bat = day ? 200 : -(load - 60);
  ents.push(['deye.pv', 'Điện mặt trời', 'W', 'kythuat', String(pv)], ['deye.load', 'Tải tiêu thụ', 'W', 'kythuat', String(load)],
    ['deye.grid', 'Điện lưới', 'W', 'kythuat', String(load + bat - pv)], ['jk.power', 'Pin sạc/xả', 'W', 'kythuat', String(bat)],
    ['jk.soc', 'Pin lưu trữ', '%', 'kythuat', day ? '86' : '64'],
    ['deye.pv_today', 'Điện mặt trời hôm nay', 'kWh', 'kythuat', '43.59'], ['deye.charge_today', 'Sạc pin hôm nay', 'kWh', 'kythuat', '22.3'],
    ['deye.sell_today', 'Bán lưới hôm nay', 'kWh', 'kythuat', '0.39'], ['deye.load_today', 'Tiêu thụ hôm nay', 'kWh', 'kythuat', '35.58'],
    ['deye.discharge_today', 'Xả pin hôm nay', 'kWh', 'kythuat', '14.19'], ['deye.buy_today', 'Mua lưới hôm nay', 'kWh', 'kythuat', '0.49']);

  const ids = (pred) => devs.filter(pred).map(d => d[0]);
  const isK = (...ks) => d => ks.includes(d[3]);
  const inZ = (...zs) => d => zs.includes(d[2]);
  const steps = (list, to) => list.map(d => ({d, to}));
  const scenes = [
    {name: 'Mở quán', icon: 'store', note: 'Đèn, máy lạnh, TV khu cafe và đèn bảng hiệu',
      steps: [...steps(ids(d => inZ('cafe')(d) && isK('light', 'ac', 'tv')(d)), 'on'), {d: 'st_bh', to: 'on'}]},
    {name: 'Đóng quán', icon: 'lock', note: 'Tắt khu cafe rồi cho robot lau',
      steps: [...steps(ids(d => inZ('cafe')(d) && isK('light', 'ac', 'tv', 'fan', 'speaker')(d)), 'off'), {d: 'st_bh', to: 'off'}, {d: 'cafe_robot', act: 'start'}]},
    {name: 'Về nhà', icon: 'home', note: 'Bật đèn và máy lạnh phòng làm việc',
      steps: [...steps(ids(d => inZ('lamviec')(d) && isK('light')(d)).slice(0, 2), 'on'), {d: 'lv_ml', to: 'on'}]},
    {name: 'Đi ngủ', icon: 'moon', note: 'Tắt đèn trong nhà, bật máy lạnh phòng ngủ',
      steps: [...steps(ids(d => isK('light')(d) && !inZ('santruoc', 'sansau')(d)), 'off'), {d: 'n1_ml', to: 'on'}, {d: 'n2_ml', to: 'on'}]},
    {name: 'Ra khỏi nhà', icon: 'door', note: 'Tắt hết đèn, máy lạnh, quạt, TV; robot lau',
      steps: [...steps(ids(isK('light', 'ac', 'fan', 'tv', 'pump', 'water')), 'off'), {d: 'cafe_robot', act: 'start'}, {d: 'lv_robot', act: 'start'}]},
  ];

  const cfg = {
    zones, scenes,
    devices: devs.map(([id, name, zone, kind, , group]) => ({id, name, zone, kind, protected: kind === 'breaker', ...(group && {group})})),
    entities: ents.map(([id, name, unit, zone, , device]) => ({id, name, unit, zone, device})),
  };
  const state = {'online:kho_den': 'offline', 'x:cafe_ac1:temp': '25', 'x:lv_ml:temp': '26',
    'x:cafe_fan1:speed': '2', 'x:ds_quat:speed': '1',
    'x:cafe_robot:status': 'docked', 'x:cafe_robot:battery': '100', 'x:lv_robot:status': 'cleaning', 'x:lv_robot:battery': '72',
    'x:cafe_loa:play': '1', 'x:cafe_loa:track': 'Nhạc quán buổi tối', 'x:cafe_loa:vol': '40'};
  for (const [id, , , kind, on] of devs) {
    state['dev:' + id] = on ? 'ON' : 'OFF';
    if (!['tv', 'speaker', 'fan', 'ac', 'vacuum'].includes(kind) && id !== 'kho_den') state['online:' + id] = 'online';
  }
  for (const [id, , , , v] of ents) state[id] = v;
  return {cfg, state};
})();

// Lịch sử mẫu cho biểu đồ (/?demo): dạng ngày/đêm theo loại số đo, điểm cuối gần giá trị hiện tại.
// Nhiễu dùng bộ sinh số cố định theo id + giờ nên vẽ lại không nhảy lung tung.
function demoHist(id, unit, hours, cur) {
  const n = 240, t1 = Date.now(), dt = hours * 3600e3 / (n - 1);
  const noise = t => { let x = Math.floor(t / 300e3) ^ [...id].reduce((a, c) => (a * 31 + c.charCodeAt(0)) >>> 0, 7); x = (x ^ (x >>> 13)) * 1274126177 >>> 0; return (x % 1000) / 1000; };
  const hr = t => { const d = new Date(t); return d.getHours() + d.getMinutes() / 60; }, day = h => Math.max(0, Math.sin((h - 6) / 12 * Math.PI));
  const busy = h => (h > 6.5 && h < 9 ? 0.7 : 0) + (h > 17.5 && h < 22.5 ? 1 : 0) + (h > 9 && h < 17.5 ? 0.4 : 0);
  let f, shift = true;
  if (id === 'deye.pv') { f = t => 3200 * day(hr(t)) ** 1.5 * (0.75 + 0.25 * noise(t)); shift = false; }
  else if (id === 'deye.load' || unit === 'W') f = t => (cur || 300) * (0.45 + 0.6 * busy(hr(t)) + 0.15 * noise(t));
  else if (id === 'deye.grid') f = t => (hr(t) > 18 && hr(t) < 23 ? 180 : 25) * noise(t);
  else if (id === 'jk.soc') { f = t => { const h = hr(t); return h < 7 ? 72 - h * 3.5 : h < 15 ? 47 + (h - 7) * 5.8 : h < 18 ? 93 : 93 - (h - 18) * 4.3; }; shift = false; }
  else if (unit === 'lx') { f = t => 42000 * day(hr(t)) * (0.7 + 0.3 * noise(t)); shift = false; }
  else if (unit === '°C') f = t => (/ngoài/i.test(id) || id.startsWith('gt') ? 4 : 1.4) * Math.sin((hr(t) - 9) / 24 * 2 * Math.PI) + 0.3 * noise(t);
  else if (unit === '%' && /\.h$/.test(id)) f = t => -8 * Math.sin((hr(t) - 9) / 24 * 2 * Math.PI) + 2 * noise(t);
  else if (/co2/.test(id)) f = t => 480 + 420 * busy(hr(t)) + 60 * noise(t);
  else if (unit === 'µg/m³') f = t => 9 + 8 * busy(hr(t)) + 4 * noise(t);
  else if (unit === 'L') { f = t => cur * Math.min(1, hr(t) / Math.max(hr(t1), 1)); shift = false; }
  else f = t => cur * (1 + 0.02 * Math.sin(t / 5.4e6 + id.length));   // TDS, lõi lọc, điện áp...: trôi nhẹ quanh giá trị hiện tại
  const pts = Array.from({length: n}, (_, i) => { const t = t1 - (n - 1 - i) * dt; return {t, v: f(t)}; });
  const off = shift && !isNaN(cur) ? cur - pts.at(-1).v : 0;
  return pts.map(p => ({t: p.t, v: Math.round((p.v + off) * 10) / 10}));
}
