// Trợ lý giọng nói (voiced, thiết kế mục 15.2). Cần dash.js (DEMO, toast) và app.js (api).
// Nút nói: chạm để nghe, chạm lần nữa (hoặc sau 8 giây) để gửi WAV lên /voice/ask; đọc câu trả lời.
// Nút loa: đọc tóm tắt tình hình (/voice/brief). Cảnh báo từ /voice/events được đọc thành tiếng.
// Nghe gọi tên (bật riêng từng màn hình ở Cài đặt → Giọng và xưng hô): micro mở liên tục, gửi từng đoạn có tiếng lên
// /voice/ask?listen=1; voiced chỉ làm theo câu mở đầu bằng tên trợ lý ("Mây ơi, bật đèn kho"), câu khác bỏ qua.
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
  // voiced trả âm thanh theo từng câu (câu đầu có ngay, câu sau đang tổng hợp): phát lần lượt, theo tốc độ trong Cài đặt.
  const player = new Audio();
  // Tốc độ đọc (nhóm "voice"): fetch thường, không qua api() để lúc chưa đăng nhập không mở thêm một khung PIN
  // giành nút "Vào" với lần nạp dashboard. Đã mở trang Cài đặt thì dùng giá trị ở đó (cả khi vừa lưu).
  let voiceCfg = {};
  fetch('/api/settings/voice').then(r => r.ok ? r.json() : {}).then(v => { voiceCfg = v; }, () => {});
  let queue = [];
  const next = () => {
    const url = queue.shift();
    if (!url) return;
    player.src = url;
    player.defaultPlaybackRate = player.playbackRate = Number((SETUP_UI.SAVED.voice || voiceCfg).speed) || 1;
    player.play().catch(() => {});
  };
  player.onended = next;
  const play = urls => { queue = [].concat(urls || []); next(); };
  addEventListener('pointerup', () => {
    play('data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEAQB8AAIA+AAACABAAZGF0YQAAAAA=');
    if (wantEar()) ear(true);   // iOS chỉ cho chạy micro/AudioContext sau một cú chạm
  }, {once: true});

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

  // Nghe gọi tên. Đoạn có tiếng = năng lượng vượt 3 lần nền ồn (tự đo khi im), giữ ~0,25 s trước đó, hết khi im 0,7 s
  // hoặc dài 6 s; đoạn dưới 0,4 s bỏ. Đang gửi, đang đọc, đang bấm nói thì không nghe (khỏi nghe chính loa mình).
  // ponytail: ngưỡng năng lượng cố định, quán ồn/TV to mà gửi nhiều đoạn thừa thì nâng 0.01 hoặc thay bằng VAD có mô hình.
  const EAR = 'hubd.listen';
  const wantEar = () => { try { return localStorage.getItem(EAR) === '1'; } catch (_) { return false; } };
  let earOn = null;
  async function ear(on) {
    try { on ? localStorage.setItem(EAR, '1') : localStorage.removeItem(EAR); } catch (_) { /* bỏ qua */ }
    if (!on) { earOn?.stop(); earOn = null; btn.classList.remove('ear'); return; }
    if (earOn) return;
    if (!navigator.mediaDevices) return toast(`Nghe gọi tên cần trang HTTPS: bật trong Cài đặt → HTTPS rồi mở https://${location.hostname}:8443`);
    const ctx = new AudioContext();
    let stream;
    try { stream = await navigator.mediaDevices.getUserMedia({audio: {echoCancellation: true, noiseSuppression: true}}); }
    catch { ctx.close(); return toast('Chưa được phép dùng micro'); }
    const src = ctx.createMediaStreamSource(stream), node = ctx.createScriptProcessor(4096, 1, 1), step = 4096 / ctx.sampleRate;
    let floor = 0.01, pre = [], seg = [], quiet = 0, busy = false, until = 0;
    node.onaudioprocess = e => {
      const x = new Float32Array(e.inputBuffer.getChannelData(0)), rms = Math.sqrt(x.reduce((a, v) => a + v * v, 0) / x.length);
      if (busy || rec || !player.paused) { seg = []; return; }
      if (!seg.length) {
        floor = floor * 0.95 + rms * 0.05;
        pre = [...pre.slice(-2), x];
        if (rms > Math.max(floor * 3, 0.01)) { seg = pre; quiet = 0; }
        return;
      }
      seg.push(x);
      quiet = rms < Math.max(floor * 2, 0.006) ? quiet + step : 0;
      const len = seg.length * step;
      if (quiet < 0.7 && len < 6) return;
      const s = seg; seg = [];
      if (len - quiet >= 0.4) send(s);
    };
    async function send(chunks) {
      busy = true;
      const k = Math.max(1, Math.round(ctx.sampleRate / 16000)), out = [];   // hạ về ~16 kHz cho nhẹ (voiced tự đổi tần số)
      for (const c of chunks) for (let i = 0; i + k <= c.length; i += k) { let a = 0; for (let j = 0; j < k; j++) a += c[i + j]; out.push(a / k); }
      try {   // ngay sau khi trợ lý hỏi lại ("Dạ, em đây...") thì câu kế không cần gọi tên
        const r = await fetch(`/voice/ask${Date.now() < until ? '' : '?listen=1'}`, {method: 'POST', headers: {'Content-Type': 'audio/wav'},
          body: wav([Float32Array.from(out)], Math.round(ctx.sampleRate / k))});
        if (r.status === 401) { ear(false); toast('Nghe gọi tên đã tắt: cần nhập lại PIN'); }
        const j = r.ok ? await r.json() : {ignored: true};
        if (!j.ignored) { toast(`“${j.heard}” — ${j.reply}`); play(j.audio); until = j.awaiting ? Date.now() + 10000 : 0; }
      } catch (_) { /* voiced tạm vắng: nghe tiếp */ }
      busy = false;
    }
    src.connect(node); node.connect(ctx.destination);
    const wakeUp = () => { if (!document.hidden) ctx.resume(); };   // iPad tắt màn hình rồi mở lại
    document.addEventListener('visibilitychange', wakeUp);
    earOn = {stop: () => { document.removeEventListener('visibilitychange', wakeUp); node.disconnect(); src.disconnect(); stream.getTracks().forEach(t => t.stop()); ctx.close(); }};
    btn.classList.add('ear');
    toast('Đang nghe gọi tên');
  }
  window.voiceEar = {on: () => !!earOn, set: ear};   // cho nút trong Cài đặt (setup-ai.js)

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
      else if (m.type === 'ack') { queue = []; player.pause(); }
    };
    es.onerror = () => { es.close(); setTimeout(listen, 30000); };   // voiced tắt: thử lại, dashboard vẫn chạy
  })();
})();
