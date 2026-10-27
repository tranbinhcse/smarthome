// Biểu đồ đường + vùng tô (SVG thuần), kiểu "RAM / CPU / Dung lượng": hàng tab chọn chỉ số, mỗi tab có
// giá trị hiện tại và cao nhất; lưới nét đứt, vạch ngưỡng đỏ, rê / chạm để xem từng điểm. Dùng chung cho
// trang Điện năng (dash.js) và trang Biểu đồ (history.html).
// metric: {label, unit, color, pts: [{t (ms), v}], zero?: trục từ 0, limit?: {v, label}, fmt?: v => chuỗi}
const CHARTS = {};   // id -> {metrics, sel, hours}
const chNf = new Intl.NumberFormat('vi-VN', {maximumFractionDigits: 1});
const chFmt = (m, v) => isNaN(v) ? '—' : m.fmt ? m.fmt(v) : m.unit === 'W' && Math.abs(v) >= 1000 ? `${chNf.format(v / 1000)} kW` : `${chNf.format(v)}${m.unit ? ' ' + m.unit : ''}`;
// Màu theo loại số đo; ngưỡng tham khảo: PM2.5 trung bình 24 giờ theo EPA, CO₂ quá 1.000 ppm nên thông gió.
const CH_COLOR = {'°C': '#ff9f5a', '%': '#4cc9f0', 'ppm': '#34d17a', 'µg/m³': '#a78bfa', 'W': '#f6b93b', 'lx': '#facc15', 'L': '#4cc9f0', 'V': '#94a3b8'};
const chLimit = e => e.unit === 'µg/m³' ? {v: 35, label: 'Ngưỡng 24 giờ (EPA) 35 µg/m³'} : /co₂|co2/i.test(e.name) ? {v: 1000, label: 'Nên thông gió từ 1.000 ppm'} : undefined;
const chMetric = (e, label, pts, now) => ({label, unit: e.unit, color: CH_COLOR[e.unit] || '#4cc9f0', pts, now, limit: chLimit(e), zero: ['W', 'lx', 'L'].includes(e.unit)});
const chEsc = s => String(s).replace(/[&<>"]/g, c => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;'}[c]));
// Lịch sử một số đo cho biểu đồ. API lịch sử trả t theo giây (voiced cũng đọc như vậy), biểu đồ vẽ theo mili giây:
// mọi trang lấy lịch sử qua hàm này (web_selectors_test.go kiểm).
function chHistory(id, hours) { return api(`/api/history?id=${encodeURIComponent(id)}&hours=${hours}`).then(r => r.json()).then(ps => (ps || []).map(p => ({t: p.t * 1e3, v: p.v}))); }
const CW = 640, PL = 52, PR = 12, PT = 18, PB = 26;   // chiều cao vùng vẽ (CH) tuỳ từng biểu đồ

// Mốc tròn cho trục: bước 1 / 2 / 2,5 / 5 × 10^n, khoảng 4 khoảng.
function chNice(lo, hi, n = 4) {
  if (hi === lo) { hi += Math.abs(hi) * 0.1 || 1; lo -= Math.abs(lo) * 0.1 || 1; }
  const raw = (hi - lo) / n, p = 10 ** Math.floor(Math.log10(raw)), step = [1, 2, 2.5, 5, 10].map(m => m * p).find(s => s >= raw);
  return [Math.floor(lo / step) * step, Math.ceil(hi / step) * step, step];
}
function chPlot(id, m, hours, CH) {
  const pts = m.pts || [];
  if (pts.length < 2) return '<p class="empty ch-empty">Chưa đủ dữ liệu để vẽ.</p>';
  const t1 = Date.now(), t0 = t1 - hours * 3600e3, vs = pts.map(p => p.v);
  let lo = Math.min(...vs), hi = Math.max(...vs);
  if (m.limit) { lo = Math.min(lo, m.limit.v); hi = Math.max(hi, m.limit.v); }
  if (m.zero) lo = Math.min(0, lo);
  const [y0, y1, step] = chNice(lo, hi);
  const X = t => PL + (t - t0) / (t1 - t0) * (CW - PL - PR), Y = v => CH - PB - (v - y0) / (y1 - y0) * (CH - PT - PB);
  const grid = [];
  for (let v = y0; v <= y1 + step / 2; v += step) {
    const tk = m.tick ? m.tick(v) : m.unit === 'W' && Math.abs(v) >= 1000 ? `${chNf.format(v / 1000)}k` : chNf.format(v);
    grid.push(`<line class="ch-grid" x1="${PL}" x2="${CW - PR}" y1="${Y(v).toFixed(1)}" y2="${Y(v).toFixed(1)}"/><text x="${PL - 8}" y="${(Y(v) + 4).toFixed(1)}" text-anchor="end">${chEsc(tk)}</text>`);
  }
  // nhãn thời gian ở các mốc giờ tròn, tối đa ~6 nhãn
  const hs = [1, 2, 3, 4, 6, 12, 24, 48, 72, 168].find(h => hours / h <= 6) || 168, xs = [];
  const first = new Date(t0); first.setMinutes(0, 0, 0);
  for (let t = first.getTime() + 3600e3; t < t1; t += 3600e3) {
    const d = new Date(t);
    if (hs < 24 ? d.getHours() % hs : d.getHours() || Math.round((t - t0) / 864e5) % (hs / 24)) continue;
    const lb = hs < 24 ? `${String(d.getHours()).padStart(2, '0')}:00` : `${d.getDate()}/${d.getMonth() + 1}`;
    xs.push(`<text x="${X(t).toFixed(1)}" y="${CH - 6}" text-anchor="middle">${lb}</text>`);
  }
  const line = pts.map((p, i) => `${i ? 'L' : 'M'}${X(p.t).toFixed(1)} ${Y(p.v).toFixed(1)}`).join(' ');
  const base = Y(Math.max(y0, Math.min(0, y1)));
  const area = `${line} L${X(pts.at(-1).t).toFixed(1)} ${base.toFixed(1)} L${X(pts[0].t).toFixed(1)} ${base.toFixed(1)}Z`;
  const lim = m.limit ? `<line class="ch-lim" x1="${PL}" x2="${CW - PR}" y1="${Y(m.limit.v).toFixed(1)}" y2="${Y(m.limit.v).toFixed(1)}"/>
    <text class="ch-limt" x="${CW - PR}" y="${(Y(m.limit.v) - 6).toFixed(1)}">${chEsc(m.limit.label)}</text>` : '';
  return `<div class="ch-plot" data-chart="${chEsc(id)}" style="--c:${m.color}">
    <svg viewBox="0 0 ${CW} ${CH}" role="img" aria-label="${chEsc(m.label)} trong ${hours} giờ qua">
      <defs><linearGradient id="chg-${chEsc(id)}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${m.color}" stop-opacity=".22"/><stop offset="1" stop-color="${m.color}" stop-opacity="0"/></linearGradient></defs>
      ${grid.join('')}${xs.join('')}<path d="${area}" fill="url(#chg-${chEsc(id)})"/><path class="ch-line" d="${line}"/>${lim}
      <line class="ch-x ch-hover" y1="${PT}" y2="${CH - PB}"/><circle class="ch-dot ch-hover" r="5"/></svg><div class="ch-tip"><b></b><small></small></div></div>`;
}
// Thẻ biểu đồ đầy đủ. Gọi lại với cùng id sẽ giữ tab đang chọn.
function chartHtml(id, metrics, hours = 24, h = 210, initSel = 0) {
  const sel = Math.max(0, Math.min(CHARTS[id]?.sel ?? initSel, metrics.length - 1));
  CHARTS[id] = {metrics, sel, hours, h};
  if (!metrics.length) return '';
  const tabs = metrics.map((m, i) => {
    const vs = (m.pts || []).map(p => p.v), now = m.now ?? vs.at(-1), max = vs.length ? Math.max(...vs) : NaN;
    return `<button class="ch-tab${i === sel ? ' on' : ''}" role="tab" aria-selected="${i === sel}" data-chtab="${chEsc(id)}:${i}" style="--c:${m.color}">
      <span>${chEsc(m.label)}</span><b>${chEsc(chFmt(m, now))}</b><small>Cao nhất ${chEsc(chFmt(m, max))}</small></button>`;
  }).join('');
  return `<div class="chart" data-chartbox="${chEsc(id)}"><div class="ch-tabs" role="tablist">${tabs}</div>${chPlot(id, metrics[sel], hours, h)}</div>`;
}
const chRedraw = id => { const box = document.querySelector(`[data-chartbox="${CSS.escape(id)}"]`); if (box) box.outerHTML = chartHtml(id, CHARTS[id].metrics, CHARTS[id].hours, CHARTS[id].h); };

document.addEventListener('click', e => {
  const b = e.target.closest('[data-chtab]');
  if (!b) return;
  const v = b.dataset.chtab, id = v.slice(0, v.lastIndexOf(':'));
  CHARTS[id].sel = Number(v.slice(v.lastIndexOf(':') + 1));
  chRedraw(id);
});
// Vạch dọc bám điểm gần nhất theo trục thời gian; chú thích ghi giá trị + giờ (textContent, không chèn HTML).
document.addEventListener('pointermove', e => {
  const plot = e.target.closest?.('[data-chart]');
  for (const p of document.querySelectorAll('.ch-plot.hov')) if (p !== plot) p.classList.remove('hov');
  if (!plot) return;
  const c = CHARTS[plot.dataset.chart], m = c.metrics[c.sel], svg = plot.querySelector('svg'), r = svg.getBoundingClientRect();
  const t1 = Date.now(), t0 = t1 - c.hours * 3600e3, x = (e.clientX - r.left) / r.width * CW;
  const t = t0 + (x - PL) / (CW - PL - PR) * (t1 - t0);
  const p = m.pts.reduce((a, b) => Math.abs(b.t - t) < Math.abs(a.t - t) ? b : a);
  const px = PL + (p.t - t0) / (t1 - t0) * (CW - PL - PR), path = plot.querySelector('.ch-line');
  // toạ độ y lấy lại từ chính đường đã vẽ, khỏi tính lại thang đo
  const seg = path.getAttribute('d').split(/[ML]/).filter(Boolean)[m.pts.indexOf(p)].trim().split(' ');
  plot.querySelector('.ch-x').setAttribute('x1', px); plot.querySelector('.ch-x').setAttribute('x2', px);
  plot.querySelector('.ch-dot').setAttribute('cx', px); plot.querySelector('.ch-dot').setAttribute('cy', seg[1]);
  const tip = plot.querySelector('.ch-tip'), d = new Date(p.t);
  tip.querySelector('b').textContent = chFmt(m, p.v);
  tip.querySelector('small').textContent = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}, ${d.getDate()}/${d.getMonth() + 1}`;
  // đặt cạnh vạch dọc, trong vùng vẽ; nửa phải thì lật sang trái
  const lx = px / CW * r.width, right = lx > r.width * 0.6;
  tip.style.left = `${lx + (right ? -10 : 10)}px`;
  tip.style.transform = right ? 'translateX(-100%)' : 'none';
  plot.classList.add('hov');
});
document.addEventListener('pointerleave', e => { if (e.target.matches?.('[data-chart]')) e.target.classList.remove('hov'); }, true);
