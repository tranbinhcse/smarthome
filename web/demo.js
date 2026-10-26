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
  // [id, tên, phòng, loại, bật?]
  const devs = [
    // Sân trước: bơm nước, tưới cây, đèn bảng hiệu
    ['st_bom', 'Bơm nước', 'santruoc', 'pump', 0], ['st_tuoi', 'Tưới cây', 'santruoc', 'water', 0],
    ['st_bh', 'Đèn bảng hiệu', 'santruoc', 'light', 1], ['cam_st', 'Camera sân trước', 'santruoc', 'camera', 1],
    // Khu cafe: 8 công tắc đèn, 2 quạt trần, 2 máy lạnh, 1 TV, robot lau nhà, 3 camera (+ ổ cắm hồ cá đang có thật)
    ['cafe_lanh', 'Đèn phòng lạnh', 'cafe', 'light', 1], ...n(7, 'Đèn', 'cafe', 'light', [1, 2, 5]),
    ['cafe_hoca', 'Ổ cắm hồ cá', 'cafe', 'socket', 1], ...n(3, 'Camera', 'cafe', 'camera', [1, 2, 3]),
    ...n(2, 'Quạt trần', 'cafe', 'fan', [1]), ...n(2, 'Máy lạnh', 'cafe', 'ac', [1]),
    ['cafe_tv', 'TV', 'cafe', 'tv', 1], ['cafe_robot', 'Robot lau nhà', 'cafe', 'vacuum', 0],
    // Bếp: chỉ có CB (thiết bị bếp, tủ lạnh không điều khiển)
    ['cam_bep', 'Camera bếp', 'bep', 'camera', 1],
    // Phòng kho: 1 công tắc, 1 máy lạnh. WC: 3 công tắc
    ['kho_den', 'Đèn kho', 'kho', 'light', 0], ['cam_kho', 'Camera kho', 'kho', 'camera', 1], ['kho_ml', 'Máy lạnh', 'kho', 'ac', 0],
    ...n(3, 'Đèn', 'wc', 'light'),
    ['sansau_den', 'Đèn sân sau', 'sansau', 'light', 1], ['cam_ss', 'Camera sân sau', 'sansau', 'camera', 1],
    ['wifi_t1', 'Router Wi-Fi tầng 1', 'tudien1', 'router', 1],
    // Phòng làm việc: 1 máy lạnh, 1 TV, 1 quạt, 6 công tắc, 1 máy lọc nước, robot lau nhà
    ...n(6, 'Đèn', 'lamviec', 'light', [1, 2]), ['lv_loc', 'Máy lọc nước', 'lamviec', 'socket', 1],
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
    ['lv.tds', 'Nước sau lọc', 'ppm', 'lamviec', '12'],
    ['n1.t', 'Nhiệt độ', '°C', 'ngu1', '28.3'], ['n2.t', 'Nhiệt độ', '°C', 'ngu2', '28'],
    ['ds.t', 'Nhiệt độ', '°C', 'docsach', '26.5'],
    ['gt.mua', 'Mưa', '', 'gieng_troi', 'Không'], ['gt.lux', 'Ánh sáng', 'lx', 'gieng_troi', '18400'],
    ['gt.t', 'Ngoài trời', '°C', 'gieng_troi', '31.6'],
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
    {name: 'Đóng quán', icon: 'door', note: 'Tắt khu cafe rồi cho robot lau',
      steps: [...steps(ids(d => inZ('cafe')(d) && isK('light', 'ac', 'tv', 'fan')(d)), 'off'), {d: 'st_bh', to: 'off'}, {d: 'cafe_robot', act: 'start'}]},
    {name: 'Đi ngủ', icon: 'moon', note: 'Tắt đèn trong nhà, bật máy lạnh phòng ngủ',
      steps: [...steps(ids(d => isK('light')(d) && !inZ('santruoc', 'sansau')(d)), 'off'), {d: 'n1_ml', to: 'on'}, {d: 'n2_ml', to: 'on'}]},
    {name: 'Ra khỏi nhà', icon: 'home', note: 'Tắt hết đèn, máy lạnh, quạt, TV; robot lau',
      steps: [...steps(ids(isK('light', 'ac', 'fan', 'tv', 'pump', 'water')), 'off'), {d: 'cafe_robot', act: 'start'}, {d: 'lv_robot', act: 'start'}]},
  ];

  const cfg = {
    zones, scenes,
    devices: devs.map(([id, name, zone, kind]) => ({id, name, zone, kind, protected: kind === 'breaker'})),
    entities: ents.map(([id, name, unit, zone, , device]) => ({id, name, unit, zone, device})),
  };
  const state = {'online:kho_den': 'offline', 'x:cafe_ac1:temp': '25', 'x:lv_ml:temp': '26',
    'x:cafe_fan1:speed': '2', 'x:ds_quat:speed': '1',
    'x:cafe_robot:status': 'docked', 'x:cafe_robot:battery': '100', 'x:lv_robot:status': 'cleaning', 'x:lv_robot:battery': '72'};
  for (const [id, , , kind, on] of devs) {
    state['dev:' + id] = on ? 'ON' : 'OFF';
    if (!['tv', 'speaker', 'fan', 'ac', 'vacuum'].includes(kind) && id !== 'kho_den') state['online:' + id] = 'online';
  }
  for (const [id, , , , v] of ents) state[id] = v;
  return {cfg, state};
})();
