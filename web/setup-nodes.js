// Mục cài đặt Node ESP32: danh sách node (khu, online, lần gửi cuối), tài khoản MQTT riêng từng node (hubd quản lý qua
// plugin dynamic security của Mosquitto, nodes.go), hãng máy lạnh cho climate_ir, gán mã hồng ngoại vừa học vào nút.
// Mật khẩu node chỉ hiện một lần ngay sau khi tạo/đổi: hubd không lưu, quên thì đổi mật khẩu mới.
(() => {
  const U = window.SETUP_UI;
  // Hãng có sẵn trong ESPHome, cùng danh sách ở đầu firmware/ir.yaml (nodes_test.go kiểm khớp). Đổi hãng phải nạp lại firmware.
  const AC = [['', 'Chưa chọn (đang coolix)'], ['coolix', 'Coolix: Midea, Kaysun, nhiều hãng Trung Quốc'],
    ['daikin', 'Daikin'], ['daikin_arc', 'Daikin (remote ARC)'], ['daikin_brc', 'Daikin (remote BRC)'], ['mitsubishi', 'Mitsubishi'],
    ['toshiba', 'Toshiba'], ['gree', 'Gree'], ['fujitsu_general', 'Fujitsu General'], ['hitachi_ac344', 'Hitachi'],
    ['climate_ir_lg', 'LG'], ['midea_ir', 'Midea'], ['whirlpool', 'Whirlpool'], ['tcl112', 'TCL']];
  // Nút remote của dashboard (web/dash.js) theo loại thiết bị, đúng mã hubd nhận (ir.go).
  const BTN = {
    tv: [['power', 'Nguồn'], ['vol_up', 'Tăng âm lượng'], ['vol_down', 'Giảm âm lượng'], ['mute', 'Tắt tiếng'], ['input', 'Nguồn vào'],
      ['ch_up', 'Kênh tăng'], ['ch_down', 'Kênh giảm'], ['up', 'Lên'], ['down', 'Xuống'], ['left', 'Trái'], ['right', 'Phải'],
      ['ok', 'OK'], ['back', 'Quay lại'], ['home', 'Trang chủ'], ['menu', 'Menu']],
    fan: [['power', 'Nguồn'], ['speed_1', 'Tốc độ 1'], ['speed_2', 'Tốc độ 2'], ['speed_3', 'Tốc độ 3'], ['swing', 'Đảo gió'],
      ['timer_0', 'Bỏ hẹn giờ'], ['timer_1', 'Hẹn giờ 1 giờ'], ['timer_2', 'Hẹn giờ 2 giờ'], ['timer_4', 'Hẹn giờ 4 giờ'],
      ['mode_normal', 'Gió thường'], ['mode_natural', 'Gió tự nhiên'], ['mode_sleep', 'Gió ngủ']],
    speaker: [['power', 'Nguồn'], ['play', 'Phát / dừng'], ['next', 'Bài sau'], ['prev', 'Bài trước'], ['vol_up', 'Tăng âm lượng'],
      ['vol_down', 'Giảm âm lượng'], ['mute', 'Tắt tiếng'], ['src_bt', 'Nguồn Bluetooth'], ['src_aux', 'Nguồn AUX'], ['src_usb', 'Nguồn USB']],
  };
  const ACC = {active: ['good', 'Đang dùng'], disabled: ['bad', 'Đã khoá'], none: ['mid', 'Chưa có']};
  const DONE = {create: 'Đã tạo tài khoản', reset: 'Đã đổi mật khẩu', disable: 'Đã khoá, node bị ngắt ngay', enable: 'Đã mở khoá', delete: 'Đã xoá tài khoản'};
  const WARN = {
    reset: ['Đổi mật khẩu', 'Node đang chạy với mật khẩu cũ sẽ không vào lại được tới khi nạp firmware có mật khẩu mới.'],
    disable: ['Khoá', 'Node bị ngắt ngay, không gửi được số đo tới khi mở khoá.'],
    delete: ['Xoá', 'Node bị ngắt ngay. Tạo lại sẽ có mật khẩu mới.'],
  };
  let data = null, at = 0, loading = false, open = '', newName = '', secret = null;
  const pick = {};   // thiết bị/nút đang chọn để gán mã, theo node (giữ qua các lần vẽ lại)

  const demo = () => { const t = Date.now() / 1000; return {manage: true, nodes: [
    {name: 'node-cafe', zone: 'cafe', online: true, last: t - 40, wifi: '-58', account: 'active', ir: ['cafe_tv', 'cafe_loa']},
    {name: 'node-gieng-troi', zone: 'gieng_troi', online: true, last: t - 95, wifi: '-71', account: 'active', ir: []},
    {name: 'node-lamviec', zone: 'lamviec', online: true, last: t - 20, wifi: '-63', account: 'active', ir: ['lv_ml', 'lv_tv', 'lv_quat']},
    {name: 'node-ngu1', zone: 'ngu1', online: false, last: t - 3 * 3600, wifi: '-80', account: 'disabled', ir: []},
    {name: 'node-ro', zone: 'cafe', online: false, last: 0, wifi: '', account: 'none', ir: []}]}; };
  async function load(force) {
    if (U.DEMO) { data ||= demo(); return; }
    if (loading || !force && Date.now() - at < 20000) return;   // trang Cài đặt vẽ lại theo mỗi tin SSE: hỏi lại tối đa 20 giây một lần
    loading = true; at = Date.now();
    try { const r = await U.api('/api/nodes'); if (r.ok) { data = await r.json(); U.soon(); } } finally { loading = false; }
  }

  const isOn = n => { const s = state['node:' + n.name]; return s ? s === 'online' : n.online; };
  const ago = t => { if (!t) return 'chưa gửi gì'; const m = Math.round((Date.now() / 1000 - t) / 60);
    return m < 1 ? 'vừa gửi' : m < 60 ? `${m} phút trước` : m < 1440 ? `${Math.round(m / 60)} giờ trước` : `${Math.round(m / 1440)} ngày trước`; };
  const learned = n => (state['irlearn:' + n] || (U.DEMO && n === 'node-cafe' ? '0000 006D 0022 0002 0157 00AC 0015 0016 0015 0041' : '')).trim();
  // Thiết bị hồng ngoại của node (trừ máy lạnh: đi climate_ir) và nút đang chọn.
  function target(n) {
    const node = data.nodes.find(x => x.name === n), devs = cfg.devices.filter(d => node?.ir.includes(d.id) && BTN[d.kind]);
    const p = pick[n] || {}, dev = devs.find(d => d.id === p.dev) || devs[0], btns = dev ? BTN[dev.kind] : [];
    return {devs, dev, btns, btn: btns.find(([v]) => v === p.btn) || btns[0]};
  }

  function body() {
    load();
    if (!data) return '<p class="note busy">Đang tải danh sách node...</p>';
    const head = !data.manage ? `<p class="note">Chưa bật quản lý tài khoản MQTT. Trên hub: <code>scripts/init-secrets.sh</code> rồi
        <code>docker compose up -d mosquitto hubd</code> (Mosquitto khởi động lại vài giây). Danh sách và mã hồng ngoại vẫn dùng được.</p>`
      : data.error ? `<p class="note bad">Không hỏi được tài khoản MQTT: ${U.esc(data.error)}</p>` : '';
    return head + `<div class="list">${data.nodes.map(row).join('') || '<p class="empty">Chưa thấy node nào.</p>'}</div>` + (data.manage ? `<h4 class="nh">Thêm node mới</h4>
      <label class="sf"><span>Tên khu: tài khoản và tiền tố topic sẽ là node-&lt;khu&gt;</span><input data-newnode autocomplete="off" autocapitalize="off"
        placeholder="vd kho, ngu-2" value="${U.esc(newName)}"></label>
      <div class="kv"><button class="pill amber" data-sact="nodes:new">Tạo tài khoản MQTT</button></div>
      <p class="note">Mỗi node một tài khoản, chỉ gửi và nhận được topic của chính nó. Mật khẩu hiện một lần để chép vào firmware/secrets.yaml.</p>` : '');
  }
  function row(n) {
    const on = isOn(n), w = Number(n.wifi), z = zone(n.zone)?.name || n.zone || 'Chưa gán khu';
    return `<button class="setrow" data-sact="nodes:open:${U.esc(n.name)}" aria-expanded="${open === n.name}">
      <span><i class="ndot${on ? ' on' : ''}"></i><span class="nn"><b>${U.esc(n.name)}</b><small>${U.esc(z)}</small></span></span>
      <small>${on ? 'Đang kết nối' : n.last ? 'Mất kết nối' : 'Chưa thấy'} · ${ago(n.last)}${n.wifi && isFinite(w) ? ` · ${Math.round(w)} dBm` : ''}</small></button>
      ${open === n.name ? detail(n) : ''}`;
  }
  function detail(n) {
    const a = n.account, ac = String(U.sval('nodes', 'ac:' + n.name));
    const acts = !data.manage || !a ? [] : a === 'none' ? [['create', 'Tạo tài khoản MQTT', 'amber']]
      : [['reset', 'Đổi mật khẩu'], a === 'active' ? ['disable', 'Khoá'] : ['enable', 'Mở khoá'], ['delete', 'Xoá tài khoản']];
    return `<div class="ndetail">${a ? `<p class="note">Tài khoản MQTT <b>${U.esc(n.name)}</b>: <em class="tag ${ACC[a][0]}">${ACC[a][1]}</em></p>` : ''}
      ${acts.length ? `<div class="kv">${acts.map(([x, l, c]) => `<button class="pill ${c || ''}" data-sact="nodes:${x}:${U.esc(n.name)}">${l}</button>`).join('')}</div>` : ''}
      <h4 class="nh">Máy lạnh</h4>${U.sselect('nodes', 'ac:' + n.name, 'Hãng máy lạnh (climate_ir, chỉ node có mắt hồng ngoại)', AC)}
      <p class="note">${ac ? `Đổi hãng phải nạp lại firmware: <code>cd firmware &amp;&amp; esphome -s ac_platform ${U.esc(ac)} run ${U.esc(n.name)}.yaml</code>`
        : 'Hãng không có trong danh sách: bỏ gói ir.yaml khỏi máy lạnh và học mã như TV (firmware/README.md).'}</p>
      <div class="kv">${U.ssave('nodes', 'Lưu hãng máy lạnh')}</div>${irBox(n)}</div>`;
  }
  function irBox(n) {
    const code = learned(n.name), {devs, dev, btns, btn} = target(n.name);
    let out = `<h4 class="nh">Mã hồng ngoại</h4>` + (code
      ? `<p class="note">Mã vừa thu (${code.split(/\s+/).length} số): <code>${U.esc(code.slice(0, 40))}${code.length > 40 ? '…' : ''}</code></p>`
      : '<p class="note">Chĩa remote gốc vào mắt thu của node rồi bấm một nút: mã thu được hiện ở đây.</p>');
    if (!dev) return out + `<p class="note">Chưa có TV, loa, quạt nào phát qua node này: khai báo trong config.yaml với command_topic ${U.esc(n.name)}/ir/send.</p>`;
    const sel = (k, label, opts, cur) => `<label class="sf"><span>${label}</span><select data-irpick="${U.esc(n.name)}:${k}">${opts.map(([v, l]) =>
      `<option value="${U.esc(v)}"${v === cur ? ' selected' : ''}>${U.esc(l)}</option>`).join('')}</select></label>`;
    out += sel('dev', 'Thiết bị', devs.map(d => [d.id, d.name]), dev.id) + sel('btn', 'Nút', btns, btn[0]) +
      `<div class="kv"><button class="pill amber" data-sact="nodes:learn:${U.esc(n.name)}"${code ? '' : ' disabled'}>Gán mã vừa thu vào nút này</button></div>`;
    const saved = devs.flatMap(d => BTN[d.kind].filter(([v]) => U.SAVED.nodes?.[`ir:${d.id}:${v}`]).map(([v, l]) => [d, v, l]));
    return out + (saved.length ? `<div class="list">${saved.map(([d, v, l]) => `<div class="setrow"><span>${U.esc(d.name)} · ${U.esc(l)}</span>
      <button class="pill" data-sact="nodes:forget:${U.esc(d.id)}:${v}">Xoá mã</button></div>`).join('')}</div>` : '');
  }

  async function account(name, action) {
    const w = WARN[action];
    if (w && !(await ask({title: `${w[0]} tài khoản ${name}?`, text: w[1], ok: w[0], danger: action !== 'reset'}))) return;
    const r = await U.api(`/api/nodes/${encodeURIComponent(name)}`, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({action})});
    if (!r.ok) return U.toast(`Chưa được: ${await r.text()}`);
    load(true);
    if (r.status !== 200) return U.toast(DONE[action]);
    const j = await r.json();
    if (action === 'create') { newName = ''; open = name; }
    secret = {name, line: `${j.secret}: "${j.password}"`};
    await ask({title: `Mật khẩu MQTT của ${name}`, html: `<p>Chỉ hiện lần này, hub không lưu. Thay dòng <b>${U.esc(j.secret)}</b> trong firmware/secrets.yaml
      bằng dòng dưới rồi nạp firmware: <code>cd firmware &amp;&amp; esphome run ${U.esc(name)}.yaml</code></p>
      <textarea class="nsecret" readonly rows="2">${U.esc(secret.line)}</textarea>
      <div class="kv"><button class="pill amber" data-sact="nodes:copy">Chép</button><button class="pill" data-sact="nodes:download">Tải file secrets</button></div>`});
    secret = null;
  }
  async function putNodes(patch, msg) {
    const r = await U.api('/api/settings/nodes', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(patch)});
    if (!r.ok) return U.toast(`Chưa lưu được: ${await r.text()}`);
    await U.loadSetting('nodes');
    U.toast(msg);
  }
  async function copy() {
    try { await navigator.clipboard.writeText(secret.line); } catch (_) { document.querySelector('.nsecret').select(); document.execCommand('copy'); }   // http trong LAN: không có clipboard API
    U.toast('Đã chép');
  }
  function download() {
    const txt = `# Mật khẩu MQTT của ${secret.name}, tạo trên dashboard ${new Date().toLocaleString('vi-VN')}. Không commit.\n` +
      `# Thay dòng cùng tên trong firmware/secrets.yaml rồi: cd firmware && esphome run ${secret.name}.yaml\n${secret.line}\n`;
    const a = Object.assign(document.createElement('a'), {href: URL.createObjectURL(new Blob([txt], {type: 'text/yaml'})), download: `secrets-${secret.name}.yaml`});
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  async function act(what) {
    const [x, a, b] = what.split(':');
    if (x === 'open') { open = open === a ? '' : a; return U.soon(); }
    if (x === 'copy' || x === 'download') return secret && (x === 'copy' ? copy() : download());
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không đổi cấu hình thật');
    if (x === 'learn') {
      const {dev, btn} = target(a), code = learned(a);
      return dev && code && putNodes({[`ir:${dev.id}:${btn[0]}`]: code}, `Đã gán mã vào ${dev.name} · ${btn[1]}`);
    }
    if (x === 'forget') return putNodes({[`ir:${a}:${b}`]: null}, 'Đã xoá mã');
    if (x === 'new') {
      const name = 'node-' + newName.trim().toLowerCase();
      if (!/^node-[a-z0-9]+(-[a-z0-9]+)*$/.test(name) || name.length > 31) return U.toast('Tên khu: chữ thường không dấu, số, gạch ngang (vd kho, ngu-2)');
      return account(name, 'create');
    }
    return account(a, x);
  }

  document.addEventListener('input', e => { if (e.target.matches?.('[data-newnode]')) newName = e.target.value; });
  document.addEventListener('change', e => {
    const s = e.target.closest?.('[data-irpick]');
    if (!s) return;
    const [n, k] = s.dataset.irpick.split(':');
    (pick[n] ||= {})[k] = s.value;
    s.blur();   // đang chọn trong ô thì trang không vẽ lại: đổi thiết bị phải hiện ngay danh sách nút của nó
    U.soon();
  });
  document.head.insertAdjacentHTML('beforeend', `<style>
    .nn { display: flex; flex-direction: column; min-width: 0; } .nn b { white-space: nowrap; } [data-setup="nodes"] .setrow > small { max-width: 50%; text-align: right; }
    .ndot { flex: none; width: 10px; height: 10px; border-radius: 50%; background: var(--ink3); } .ndot.on { background: var(--green); }
    .ndetail { display: flex; flex-direction: column; gap: 10px; padding: 2px 4px 14px; }
    [data-setup="nodes"] .nh { margin: 8px 0 0; font-size: 13px; font-weight: 600; color: var(--ink2); }
    [data-setup="nodes"] code, .nsecret { font: 12.5px ui-monospace, Menlo, monospace; word-break: break-all; }
    .nsecret { width: 100%; margin: 4px 0 10px; padding: 8px 10px; border-radius: 10px; border: 1px solid var(--line); background: var(--glass2); color: var(--ink); resize: none; }
  </style>`);

  SETUP.push({id: 'nodes', ns: 'nodes', icon: 'router', c: 'blue', title: 'Node ESP32', body, act,
    meta: () => data ? `${data.nodes.filter(isOn).length}/${data.nodes.length} node đang kết nối` : ''});
})();
