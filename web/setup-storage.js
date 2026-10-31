// Mục cài đặt Lưu trữ (ghi hình camera): tình trạng ổ ghi hình, mức dùng tối đa cho đoạn thường, dung lượng và số ngày mỗi camera.
// Dữ liệu từ GET /api/rec/storage (recdisk.go). Mức tối đa lưu ở nhóm "camera", khoá rec_max_pct (số, hubd kẹp 50-95).
(() => {
  const U = window.SETUP_UI, e = U.esc;
  const DEMO = {ok: true, reason: '', total: 2e12, free: 5.2e11, normal_bytes: 1.2e12, held_bytes: 1.5e10, max_pct: 80,
    cams: [{id: 'cam_san', bytes: 5.1e11, days: 12.4}, {id: 'cam_cong', bytes: 4.3e11, days: 9}, {id: 'cam_kho', bytes: 2.6e11, days: null}]};
  let st = null, names = {};
  // 1 GB = 1e9 byte (khớp đổi GB↔byte của hubd); dấu phẩy thập phân kiểu Việt
  const vn = n => String(n).replace('.', ',');
  const size = b => b >= 1e12 ? `${vn((b / 1e12).toFixed(1))} TB` : `${Math.round(b / 1e9)} GB`;
  async function load() {
    st = {ok: false, reason: 'Đang đọc tình trạng ổ ghi hình...'}; names = {};
    if (U.DEMO) { st = DEMO; names = {cam_san: 'Camera sân', cam_cong: 'Camera cổng', cam_kho: 'Camera kho'}; return U.soon(); }
    U.loadSetting('camera');
    for (const d of cfg.devices) if (d.kind === 'camera') names[d.id] = d.name;
    const [a, c] = await Promise.all([U.api('/api/rec/storage'), U.api('/api/cameras')]);
    if (c.ok) for (const x of (await c.json()).cameras || []) names[x.id] = x.name;
    st = a.ok ? await a.json() : {ok: false, reason: `Không đọc được tình trạng ổ (lỗi ${a.status})`};
    U.soon();
  }
  const PCTS = [50, 55, 60, 65, 70, 75, 80, 85, 90, 95];
  function body() {
    if (!st) load();
    const s = st || {};
    const line = s.ok ? `Ổ ghi hình: ${size(s.total)}, đã dùng ${size(s.total - s.free)}, còn trống ${size(s.free)}` : s.reason;
    const rows = (s.cams || []).map(c => `<div class="setrow"><span>${U.icon('camera')}${e(names[c.id] || c.id)}</span>
      <small>${size(c.bytes)} · ${c.days == null ? 'chưa đủ dữ liệu' : `khoảng ${Math.round(c.days)} ngày`}</small></div>`).join('');
    // ô số (không dùng <select>): dash.js lưu select thành chuỗi, mà hubd chỉ nhận số cho rec_max_pct
    const cur = Number(U.sval('camera', 'rec_max_pct')) || s.max_pct || 80;
    return `<p class="note${s.ok ? '' : ' bad'}">${e(line)}</p>
      <label class="sf"><span>Ghi hình thường dùng tối đa (%)</span><input data-sfield="camera.rec_max_pct" type="number" min="50" max="95" step="5" value="${cur}" list="rec-pcts"></label>
      <datalist id="rec-pcts">${PCTS.map(p => `<option value="${p}">`).join('')}</datalist>
      <p class="note">Đoạn báo động được giữ lại không tính vào mức này và không bị xoá tự động.</p>
      <div class="kv">${U.ssave('camera')}</div>
      <h4 class="mh">Mỗi camera</h4>
      ${rows ? `<div class="list">${rows}</div>` : '<p class="note">Chưa có đoạn ghi hình.</p>'}`;
  }
  SETUP.push({id: 'storage', ns: 'camera', icon: 'cloud', c: 'amber', title: 'Lưu trữ', body,
    meta: () => st?.ok ? `Còn trống ${size(st.free)}` : ''});
})();
