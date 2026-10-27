// Trợ lý giọng nói (voiced, thiết kế mục 15.2). Cần dash.js (DEMO, toast) và app.js (api).
// Nút nói: chạm để nghe, chạm lần nữa (hoặc sau 8 giây) để gửi WAV lên /voice/ask; đọc câu trả lời.
// Nút loa: đọc tóm tắt tình hình (/voice/brief). Cảnh báo từ /voice/events được đọc thành tiếng.
// Đọc (phát âm thanh) chạy cả trên HTTP; chỉ micro cần HTTPS (cổng 8443, bật trong Cài đặt → HTTPS).
(async () => {
  if (DEMO) return;
  if (!(await fetch('/voice/enabled').then(r => r.ok, () => false))) return;   // trợ lý chưa bật: không hiện nút, không nghe
  const btn = document.createElement('button');
  Object.assign(btn, {className: 'rbtn', id: 'mic', type: 'button', title: 'Nói với trợ lý'});
  btn.setAttribute('aria-label', 'Nói với trợ lý');
  btn.innerHTML = '<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/></svg>';
  document.getElementById('bell').before(btn);
  const say = document.createElement('button');
  Object.assign(say, {className: 'rbtn', id: 'brief', type: 'button', title: 'Đọc tình hình'});
  say.setAttribute('aria-label', 'Đọc tóm tắt tình hình: thời tiết, không khí, điện, cảnh báo');
  say.innerHTML = icon('vol');
  btn.before(say);

  // iOS chỉ cho phát tiếng sau một lần chạm: mở khoá loa ở lần chạm đầu để cảnh báo tự đọc được về sau.
  const player = new Audio();
  const play = url => { if (url) { player.src = url; player.play().catch(() => {}); } };
  addEventListener('pointerup', () => play('data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEAQB8AAIA+AAACABAAZGF0YQAAAAA='), {once: true});

  say.onclick = async () => {
    say.disabled = true; toast('Đang tóm tắt…');
    try {
      const r = await api('/voice/brief', {method: 'POST'});
      if (!r.ok) throw r.status;
      const j = await r.json();
      toast(j.reply); play(j.audio);
    } catch { toast('Trợ lý chưa sẵn sàng'); }
    say.disabled = false;
  };

  let rec = null;
  btn.onclick = async () => {
    if (rec) return rec.stop();
    if (!navigator.mediaDevices) return toast(`Micro cần trang HTTPS: bật trong Cài đặt → HTTPS rồi mở https://${location.hostname}:8443`);
    const ctx = new AudioContext();   // tạo ngay trong cú chạm, iOS mới cho chạy
    let stream;
    try { stream = await navigator.mediaDevices.getUserMedia({audio: true}); }
    catch { ctx.close(); return toast('Chưa được phép dùng micro'); }
    const src = ctx.createMediaStreamSource(stream), node = ctx.createScriptProcessor(4096, 1, 1), chunks = [];
    node.onaudioprocess = e => chunks.push(new Float32Array(e.inputBuffer.getChannelData(0)));
    src.connect(node); node.connect(ctx.destination);
    btn.classList.add('rec'); toast('Đang nghe… chạm lần nữa để gửi');
    const timer = setTimeout(() => rec && rec.stop(), 8000);
    rec = {stop: async () => {
      rec = null; clearTimeout(timer); btn.classList.remove('rec');
      node.disconnect(); src.disconnect(); stream.getTracks().forEach(t => t.stop());
      const body = wav(chunks, ctx.sampleRate);
      ctx.close();
      btn.disabled = true; toast('Đang xử lý…');
      try {
        const r = await api('/voice/ask', {method: 'POST', headers: {'Content-Type': 'audio/wav'}, body});
        if (!r.ok) throw r.status;
        const j = await r.json();
        toast(`“${j.heard}” — ${j.reply}`); play(j.audio);
      } catch { toast('Trợ lý chưa sẵn sàng'); }
      btn.disabled = false;
    }};
  };

  // WAV PCM 16-bit mono, giữ tần số gốc (voiced tự đổi về 16 kHz).
  function wav(chunks, rate) {
    const n = chunks.reduce((a, c) => a + c.length, 0), v = new DataView(new ArrayBuffer(44 + n * 2));
    const str = (o, s) => [...s].forEach((c, i) => v.setUint8(o + i, c.charCodeAt(0)));
    str(0, 'RIFF'); v.setUint32(4, 36 + n * 2, true); str(8, 'WAVEfmt '); v.setUint32(16, 16, true);
    v.setUint16(20, 1, true); v.setUint16(22, 1, true); v.setUint32(24, rate, true); v.setUint32(28, rate * 2, true);
    v.setUint16(32, 2, true); v.setUint16(34, 16, true); str(36, 'data'); v.setUint32(40, n * 2, true);
    let o = 44;
    for (const c of chunks) for (const x of c) { v.setInt16(o, Math.max(-1, Math.min(1, x)) * 0x7fff, true); o += 2; }
    return v.buffer;
  }

  (function listen() {
    const es = new EventSource('/voice/events');
    es.onmessage = ev => {
      const m = JSON.parse(ev.data);
      if (m.type === 'alert') { toast(m.text); play(m.audio); }
      else if (m.type === 'ack') player.pause();
    };
    es.onerror = () => { es.close(); setTimeout(listen, 30000); };   // voiced tắt: thử lại, dashboard vẫn chạy
  })();
})();
