// Mục cài đặt Tuya (Smart Life): nhập User Code, lấy mã QR quét bằng app; quét xong hub tự cập nhật danh sách
// thiết bị (tuya-bridge ghi devices.json + cấu hình sinh, hubd tự nạp lại). Tiến độ: state "setup:tuya" do bridge gửi.
(() => {
  const U = window.SETUP_UI;
  const st = () => { try { return JSON.parse(state['setup:tuya'] || '{}'); } catch (_) { return {}; } };
  const busy = s => ['qr', 'fetching', 'scanning'].includes(s.state);
  const ago = t => { const m = Math.round((Date.now() / 1000 - t) / 60); return m < 1 ? 'vừa xong' : m < 60 ? `${m} phút trước` : `${Math.round(m / 60)} giờ trước`; };
  const svgSrc = svg => 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)));   // ảnh, không chèn SVG làm HTML

  function body() {
    const s = st();
    let out = U.sinput('tuya', 'user_code', 'User Code', {ph: 'Dán User Code từ app Smart Life'}) +
      `<p class="note">Lấy trong app Smart Life: Tôi → biểu tượng ⚙️ Cài đặt → Tài khoản và bảo mật → User Code.</p>
      <div class="kv">${busy(s) ? '<button class="pill" data-sact="tuya:cancel">Huỷ</button>'
        : '<button class="pill amber" data-sact="tuya:start">Lấy mã QR</button>'}</div>`;
    if (s.state === 'qr' && s.qr_svg) out += `<div class="qrbox"><img class="qr" src="${svgSrc(s.qr_svg)}" alt="Mã QR đăng nhập Smart Life"><p class="note">${U.esc(s.message || '')}</p></div>`;
    else if (busy(s)) out += `<p class="note busy">${U.esc(s.message || 'Đang xử lý...')}</p>`;
    else if (s.state === 'error') out += `<p class="note bad">${U.esc(s.message || 'Có lỗi, thử lại.')}</p>`;
    else if (s.state === 'done') {
      out += `<p class="note">${U.esc(s.message || '')} ${s.at ? '(' + ago(s.at) + ')' : ''}</p><div class="list">${(s.devices || []).map(d =>
        `<div class="setrow"><span>${U.esc(d.name)}${d.new ? ' <em class="tag good">Mới</em>' : ''}</span>
        <small>${d.channels > 1 ? `${d.channels} nút · ` : ''}${d.ip ? U.esc(d.ip) : 'chưa có IP'}</small></div>`).join('')}</div>`;
    }
    return out;
  }

  async function act(what) {
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
