// Mục cài đặt Loa nối vào hub (hubout.go): hub phát thẳng ra loa cắm jack 3,5 mm/HDMI của hub hoặc loa Bluetooth, thành một
// khu như các đầu nhận (tên, âm lượng, tắt tiếng ở trình phát → Trên hub → Khu phát). Chọn ngõ ra, độ trễ loa; mục Loa
// Bluetooth: tìm, ghép đôi (ghép đôi + tin cậy + nối), nối, ngắt. Ghép đôi là việc người dùng bấm, hub không tự ghép.
(() => {
  const U = window.SETUP_UI;
  let S = null, at = 0, busy = '', delay = null;   // busy: địa chỉ loa đang ghép đôi/nối; delay: số đang gõ
  const load = async () => { at = Date.now(); const r = await fetch('/api/music/out'); if (r.ok) { S = await r.json(); U.soon(); } };
  const post = body => U.api('/api/music/out', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
  const TXT = {off: 'Tắt tiếng', idle: 'Sẵn sàng, chờ nhạc', playing: 'Đang phát', error: 'Lỗi'};
  const label = () => !S ? '' : S.out ? TXT[S.state] || S.state : 'Không dùng';
  document.addEventListener('input', e => { if (e.target.matches?.('[data-spkdelay]')) delay = e.target.value; });

  function body() {
    if (!U.DEMO && Date.now() - at > (S?.bt?.scanning || busy ? 2000 : 10000)) load();
    if (!S) return '<p class="note">Đang đọc...</p>';
    const bt = S.bt || {}, devs = bt.devices || [], esc = U.esc;
    const name = id => id.startsWith('bt:') ? `Bluetooth: ${devs.find(d => 'bt:' + d.mac === id)?.name || id.slice(3)}` : S.outputs.find(o => o.id === id)?.name || id;
    const outs = [['', 'Không dùng'], ...S.outputs.map(o => [o.id, o.name]), ...devs.filter(d => d.paired).map(d => ['bt:' + d.mac, 'Bluetooth: ' + d.name])];
    if (S.out && !outs.some(([id]) => id === S.out)) outs.push([S.out, name(S.out)]);   // loa đã chọn nhưng hub chưa thấy
    const st = d => d.connected ? 'Đang nối' : d.paired ? 'Đã ghép đôi' : 'Chưa ghép đôi';
    const btn = (what, mac, l, cls = 'pill sm') => `<button class="${cls}" data-sact="speaker:${what}" data-mac="${esc(mac)}"${busy ? ' disabled' : ''}>${l}</button>`;
    return `<p class="note">Hub phát thẳng ra loa cắm vào hub hoặc loa Bluetooth, cùng lúc và khớp nhịp với các khu khác. Khu này hiện ở trình phát → Trên hub → Khu phát
      (tên, âm lượng, tắt tiếng).</p>
      <p class="note">Tình trạng: <b>${esc(label())}</b>${S.out ? ` · ${esc(name(S.out))}` : ''}</p>
      ${S.error ? `<p class="note bad">${esc(S.error)}</p>` : ''}
      <h4 class="mh">Ngõ ra</h4>
      <div class="list">${outs.map(([id, n]) => `<button class="setrow${S.out === id ? ' sel' : ''}" data-sact="speaker:out" data-out="${esc(id)}"><span>${esc(n)}</span>${S.out === id ? '<small>Đang dùng</small>' : ''}</button>`).join('')}</div>
      ${S.outputs.length ? '' : '<p class="note">Hub chưa thấy ngõ ra có dây: container hubd cần /dev/snd (docker-compose.yml).</p>'}
      <label class="sf"><span>Độ trễ của loa (ms)</span><input data-spkdelay type="number" min="0" max="700" step="10" value="${esc(delay ?? S.delay_ms)}"></label>
      <p class="note">Loa nghe sớm hay muộn hơn các khu khác thì chỉnh ở đây: tăng khi loa này muộn hơn. Loa Bluetooth thường cần khoảng 150–250 ms, loa cắm dây 0.</p>
      <div class="kv"><button class="pill amber" data-sact="speaker:delay">Lưu độ trễ</button></div>
      <h4 class="mh">Loa Bluetooth</h4>
      ${!bt.available ? `<p class="note">Hub chưa nói chuyện được với Bluetooth của máy: máy cần có Bluetooth, container hubd cần /run/dbus/system_bus_socket,
        và trên Ubuntu cần cho phép D-Bus qua AppArmor (xem tài liệu, mục 18.3).</p>${bt.error ? `<p class="note bad">${esc(bt.error)}</p>` : ''}`
      : `<p class="note">Bật loa, chuyển loa sang chế độ ghép đôi (thường giữ nút Bluetooth hoặc Pair tới khi đèn nháy nhanh), rồi bấm Tìm loa. Thấy tên loa thì bấm Ghép đôi.
        Lần sau bật loa, hub tự nối lại khi có nhạc.</p>
        <div class="kv"><button class="pill amber" data-sact="speaker:scan"${bt.scanning ? ' disabled' : ''}>${bt.scanning ? 'Đang tìm…' : 'Tìm loa'}</button></div>
        <div class="list">${devs.map(d => `<div class="setrow"><span>${esc(d.name)}</span><small>${busy === d.mac ? 'Đang làm…' : st(d)}</small>
          ${!d.paired ? btn('pair', d.mac, 'Ghép đôi', 'pill sm amber') : d.connected ? btn('disconnect', d.mac, 'Ngắt') : btn('connect', d.mac, 'Nối')}</div>`).join('')
          || `<p class="note">${bt.scanning ? 'Đang tìm loa ở gần hub…' : 'Chưa thấy loa nào.'}</p>`}</div>`}`;
  }

  async function act(what, el) {
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không điều khiển loa');
    if (what === 'out' || what === 'delay') {
      const d = Number(delay ?? S.delay_ms);
      const r = await post({do: 'set', out: what === 'out' ? el.dataset.out : S.out, delay_ms: d});
      if (!r.ok) return U.toast(await r.text());
      delay = null;
      U.toast(what === 'out' ? 'Đã đổi ngõ ra' : 'Đã lưu độ trễ');
      return load();
    }
    if (what === 'scan') {
      const r = await post({do: 'scan'});
      if (!r.ok) return U.toast(await r.text());
      U.toast('Đang tìm loa trong 30 giây');
      return load();
    }
    busy = el.dataset.mac;   // ghép đôi, nối: có thể mất tới nửa phút
    U.soon();
    if (what === 'pair') U.toast('Đang ghép đôi, giữ loa ở chế độ ghép đôi');
    const r = await post({do: what, mac: busy});
    busy = '';
    if (!r.ok) U.toast(await r.text());
    else U.toast({pair: 'Đã ghép đôi và nối loa', connect: 'Đã nối loa', disconnect: 'Đã ngắt loa'}[what]);
    load();
  }

  SETUP.push({id: 'speaker', icon: 'speaker', c: 'cyan', title: 'Loa nối vào hub', body, act, meta: label});
})();
