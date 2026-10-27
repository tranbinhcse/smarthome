// Mục cài đặt Tuya (Smart Life): nhập User Code, lấy mã QR quét bằng app; quét xong hub tự cập nhật danh sách
// thiết bị (tuya-bridge ghi devices.json + cấu hình sinh, hubd tự nạp lại). Tiến độ: state "setup:tuya" do bridge gửi.
(() => {
  const U = window.SETUP_UI;
  const st = () => { try { return JSON.parse(state['setup:tuya'] || '{}'); } catch (_) { return {}; } };
  const busy = s => ['qr', 'fetching', 'scanning'].includes(s.state);
  const ago = t => { const m = Math.round((Date.now() / 1000 - t) / 60); return m < 1 ? 'vừa xong' : m < 60 ? `${m} phút trước` : `${Math.round(m / 60)} giờ trước`; };
  const svgSrc = svg => 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)));   // ảnh, không chèn SVG làm HTML

  // Chạm vào thiết bị để sửa tên, loại, tầng, phòng: form dùng chung của trang Cài đặt (settings.js, nhóm cấu hình "devices").
  let open = '';
  function devList(s) {
    const scan = Object.fromEntries((s.devices || []).map(d => [d.id, d]));
    return `<div class="list">${[...U.units(cfg.devices.filter(x => x.id.startsWith('tuya_')))].map(([k, chs]) => {
      const d = chs[0], raw = (d.command_topic || '').split('/')[1] || d.id, n = chs.length > 1 || d.group ? chs.length : 0;
      const meta = [kindLabel(d), zone(d.zone)?.name || 'chưa gán phòng', n ? `${n} ${kindOf(d) === 'socket' ? 'lỗ' : 'nút'}` : '', scan[raw]?.ip || 'chưa có IP'].join(' · ');
      return `<button class="setrow${k === open ? ' sel' : ''}" data-sact="tuya:edit:${U.esc(k)}" aria-expanded="${k === open}">
        <span>${U.esc(d.group || d.name)}${scan[raw]?.new ? ' <em class="tag good">Mới</em>' : ''}</span><small>${U.esc(meta)}</small></button>${k === open ? U.devForm(k) : ''}`;
    }).join('')}</div>`;
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
    if (cfg.devices.some(x => x.id.startsWith('tuya_'))) out += `<p class="note">Chạm vào thiết bị để đổi tên, loại, tầng, phòng.</p>${devList(s)}`;
    return out;
  }

  async function act(what) {
    if (what.startsWith('edit:')) { const k = what.slice(5); open = open === k ? '' : k; return U.soon(); }
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
