// Mục cài đặt Trợ lý AI (voiced gọi API chat completions tương thích OpenAI), Giọng và xưng hô (nhóm "voice"),
// Đọc thông báo (bật/tắt từng loại notify).
// URL, khoá, model lưu trong hubd nhóm "ai"; trình duyệt chỉ thấy api_key_set. "Thử kết nối" và danh sách model đi qua
// hubd → voiced (/voice/ai/check): trình duyệt không gọi thẳng endpoint AI.
(() => {
  const U = window.SETUP_UI;
  let check = null, busy = false, manual = false, kinds = null;

  function body() {
    const models = check?.models || [], m = String(U.sval('ai', 'model') || '');
    const model = models.length && !manual
      ? U.sselect('ai', 'model', 'Model', [['', '— Chọn model —'], ...(m && !models.includes(m) ? [m] : []).concat(models).map(x => [x, x])])
      : U.sinput('ai', 'model', 'Model', {ph: 'vd gpt-4o-mini, qwen2.5:7b'});
    return U.sinput('ai', 'url', 'Địa chỉ API (base URL)', {ph: 'https://api.openai.com/v1'}) +
      U.sinput('ai', 'api_key', 'API key', {secret: true, ph: 'Để trống nếu máy AI trong nhà không cần khoá'}) + model +
      `<div class="kv"><button class="pill" data-sact="ai:check"${busy ? ' disabled' : ''}>${busy ? 'Đang thử...' : 'Thử kết nối'}</button>${U.ssave('ai')}
        ${models.length ? `<button class="pill" data-sact="ai:manual">${manual ? 'Chọn từ danh sách' : 'Gõ tên model'}</button>` : ''}</div>` +
      (check ? `<p class="note${check.ok ? '' : ' bad'}">${U.esc(check.message)}</p>` : '') +
      `<p class="note">Dùng được mọi API tương thích OpenAI (OpenAI, OpenRouter, Groq, Gemini, Ollama, LM Studio...). Câu hỏi tự do và trạng thái
        thiết bị được gửi tới địa chỉ này. Đổi địa chỉ thì nhập lại khoá (khoá cũ không gửi sang máy khác).
        Lệnh cố định (bật đèn kho, pin còn bao nhiêu, tình hình thế nào) vẫn chạy trong nhà khi chưa cài.</p>`;
  }

  async function act(what) {
    if (what === 'manual') { manual = !manual; return U.soon(); }
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không thử kết nối thật');
    busy = true; U.soon();
    try {   // khoá chỉ gửi khi vừa gõ (ô trống = dùng khoá đã lưu trong hub)
      const r = await U.api('/voice/ai/check', {method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({url: U.sval('ai', 'url'), api_key: U.DRAFT.ai?.api_key || '', model: U.sval('ai', 'model')})});
      check = r.ok ? await r.json() : {ok: false, models: [], message: r.status === 404
        ? 'Trợ lý giọng nói (voiced) chưa chạy: thêm voice vào COMPOSE_PROFILES (README mục Trợ lý).' : `Lỗi ${r.status}`};
    } catch (_) { check = {ok: false, models: [], message: 'Không gọi được hub'}; }
    busy = false; U.soon();
  }

  SETUP.push({id: 'ai', ns: 'ai', icon: 'spark', c: 'amber', title: 'Trợ lý AI', body, act,
    meta: () => U.SAVED.ai?.model ? `Model ${U.esc(U.SAVED.ai.model)}` : 'Chưa cài, chỉ lệnh cố định'});

  // Giọng và xưng hô (nhóm "voice"): voiced đọc lại ngay khi lưu (MQTT hubd/settings/voice). Tốc độ đọc do dashboard áp
  // khi phát (voice.js, giữ cao độ). Nghe thử dùng giá trị đang nhập, chưa cần lưu.
  const VOICES = [['Hải Đăng', 'Nam · Hải Đăng'], ['Trúc Ly', 'Nữ · Trúc Ly']];
  const SPEEDS = [['1', 'Vừa'], ['0.85', 'Chậm'], ['1.15', 'Nhanh'], ['1.3', 'Rất nhanh']];
  const sample = new Audio();
  SETUP.push({id: 'voice', ns: 'voice', icon: 'speaker', c: 'cyan', title: 'Giọng và xưng hô',
    meta: () => { const v = U.SAVED.voice || {}; return `${U.esc(v.voice || 'Hải Đăng')}${v.owner ? ` · gọi ${U.esc(v.owner)}` : ''}`; },
    body: () => U.sselect('voice', 'voice', 'Giọng đọc', VOICES) +
      U.sinput('voice', 'owner', 'Gọi chủ nhà là', {ph: 'vd anh Bình, chị Lan (để trống: bạn)'}) +
      U.sinput('voice', 'name', 'Tên trợ lý', {ph: 'vd Mây: gọi "Mây ơi" để ra lệnh'}) +
      U.sselect('voice', 'speed', 'Tốc độ đọc', SPEEDS) +
      `<div class="kv">${U.sinput('voice', 'quiet_from', 'Giờ yên lặng từ', {type: 'time'})}${U.sinput('voice', 'quiet_to', 'đến', {type: 'time'})}</div>
      <div class="kv"><button class="pill" data-sact="voice:preview">Nghe thử</button>${U.ssave('voice')}
        <button class="pill${window.voiceEar?.on() ? ' amber' : ''}" data-sact="voice:ear">${window.voiceEar?.on() ? 'Tắt nghe gọi tên ở màn hình này' : 'Nghe gọi tên ở màn hình này'}</button></div>
      <p class="note">Có cách gọi chủ nhà thì trợ lý xưng "em" và mở đầu câu trả lời bằng "Dạ ...", cả khi hỏi AI. Đã đăng ký
        Giọng người nhà thì trợ lý gọi theo người đang nói. Trong giờ yên lặng
        cảnh báo chỉ hiện chữ, cảnh báo khẩn vẫn đọc; để trống hai ô giờ thì luôn đọc.</p>
      <p class="note">Nghe gọi tên: micro của màn hình này mở liên tục (cần HTTPS, để màn hình sáng); gọi "${U.esc(U.sval('voice', 'name') || 'trợ lý')} ơi"
        rồi nói lệnh. Tiếng nói được nhận dạng ngay trên hub, câu không gọi tên bị bỏ qua và không lưu lại.</p>`,
    act: async what => {
      if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không đọc thử');
      if (what === 'ear') {
        if (!window.voiceEar) return U.toast('Trợ lý giọng nói (voiced) chưa chạy');
        await window.voiceEar.set(!window.voiceEar.on());
        return U.soon();
      }
      const v = k => String(U.sval('voice', k) || '');
      const r = await U.api('/voice/preview', {method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({voice: v('voice'), owner: v('owner'), name: v('name')})});
      if (!r.ok) return U.toast(r.status === 404 ? 'Trợ lý giọng nói (voiced) chưa chạy' : `Lỗi ${r.status}`);
      const j = await r.json();
      U.toast(j.reply);
      if (!j.audio) return;
      sample.src = j.audio[0];
      sample.defaultPlaybackRate = sample.playbackRate = Number(v('speed')) || 1;
      sample.play().catch(() => {});
    }});

  // Giọng người nhà (voiced /voice/people): mỗi người đọc vài câu để trợ lý nhận ra ai đang nói, xưng hô đúng người; bật
  // "chỉ làm theo giọng người nhà" (nhóm "voice") thì bỏ qua giọng lạ như TV, khách. Vân giọng chỉ lưu trên hub.
  // Câu mẫu không chứa lệnh (micro nghe lệnh có thể đang mở; voice.js cũng tạm ngừng nghe khi ghi).
  const READ = ['Xin chào, đây là giọng của tôi, hãy nhớ giọng nói này nhé.',
    'Sáng nay trời trong xanh, gió nhẹ, tôi đi dạo quanh hồ một vòng.', 'Một, hai, ba, bốn, năm, sáu, bảy, tám, chín, mười.'];
  const LEVELS = [['0.55', 'Vừa (mặc định)'], ['0.45', 'Thoáng: người nhà ít bị báo giọng lạ'], ['0.65', 'Chặt: người lạ khó lọt']];
  const num = x => String(x).replace('.', ',');
  let people = null, recBusy = '';
  async function loadPeople() {
    people = people || [];
    if (U.DEMO) { people = [{you: 'anh Bình', samples: 3}]; return U.soon(); }
    const r = await U.api('/voice/people');
    if (r.ok) { people = await r.json(); U.soon(); }
  }
  SETUP.push({id: 'people', ns: 'voice', icon: 'shield', c: 'cyan', title: 'Giọng người nhà',
    meta: () => people?.length ? `${people.length} người${U.SAVED.voice?.only_people ? ' · chỉ nghe người nhà' : ''}` : 'Chưa đăng ký',
    body: () => {
      if (!people) loadPeople();
      const you = String(U.sval('people', 'you') || '').trim(), n = people?.find(p => p.you === you)?.samples || 0;
      return `<p class="note">Mỗi người đọc ${READ.length} câu dưới đây, mỗi lần bấm ghi 5 giây; nói như lúc ra lệnh, đứng cách màn hình như thường ngày.
          Trợ lý nhận ra ai đang nói để xưng hô đúng người. Vân giọng chỉ lưu trên hub.</p>
        ${U.sinput('people', 'you', 'Cách gọi người này', {ph: 'vd anh Bình, chị Lan'})}
        <p class="note">Câu ${n % READ.length + 1}: <b>“${U.esc(READ[n % READ.length])}”</b></p>
        <div class="kv"><button class="pill amber" data-sact="people:rec"${recBusy ? ' disabled' : ''}>${recBusy === 'rec' ? 'Đang ghi… đọc câu trên'
            : n ? `Ghi thêm (đã có ${n} mẫu)` : 'Ghi câu 1'}</button>
          <button class="pill" data-sact="people:check"${recBusy || !people?.length ? ' disabled' : ''}>${recBusy === 'check' ? 'Đang nghe… nói một câu' : 'Thử nhận giọng'}</button></div>
        ${people?.length ? `<div class="list">${people.map(p => `<div class="setrow"><span>${U.esc(p.you)}<small>${p.samples} mẫu${p.samples < READ.length
            ? `, nên ghi đủ ${READ.length}` : ''}</small></span><button class="pill sm" data-sact="people:del" data-you="${U.esc(p.you)}">Xoá</button></div>`).join('')}</div>` : ''}
        <label class="setrow"><span>Chỉ nghe người nhà<small>bỏ qua TV, khách</small></span>
          <input type="checkbox" data-sfield="voice.only_people"${U.sval('voice', 'only_people') === true ? ' checked' : ''}></label>
        ${U.sselect('voice', 'speaker_threshold', 'Độ khắt khe khi nhận giọng', LEVELS)}
        <div class="kv">${U.ssave('voice')}</div>
        <p class="note">Thử nhận giọng cho biết độ giống (0 đến 1, từ 0,55 là cùng người ở mức Vừa). Người nhà hay bị báo giọng lạ thì ghi thêm mẫu
          hoặc chọn Thoáng; người lạ vẫn lọt thì chọn Chặt. Câu rất ngắn như "có" được tính cho người vừa nói trong cùng cuộc trò chuyện.
          Bật Chỉ nghe người nhà: giọng lạ gọi tên thì trợ lý im, bấm nút nói thì trợ lý chỉ báo bằng chữ.</p>`;
    },
    act: async (what, el) => {
      if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không ghi âm');
      if (what === 'del') {
        if (!(await ask({title: `Xoá giọng của ${el.dataset.you}?`, ok: 'Xoá', danger: true}))) return;
        const r = await U.api('/voice/people?you=' + encodeURIComponent(el.dataset.you), {method: 'DELETE'});
        if (!r.ok) return U.toast(`Lỗi ${r.status}`);
        people = null; return loadPeople();
      }
      if (recBusy) return;
      if (!window.voiceRecord) return U.toast('Trợ lý giọng nói (voiced) chưa chạy');
      const you = String(U.sval('people', 'you') || '').trim();
      if (what === 'rec' && !you) return U.toast('Nhập cách gọi người này trước, vd anh Bình');
      recBusy = what; U.soon();
      let body;
      try { body = await window.voiceRecord(what === 'rec' ? 5 : 3); }   // gọi ngay trong cú chạm: iOS mới cho mở micro
      catch (e) { recBusy = ''; U.soon(); return U.toast(e.message); }
      recBusy = ''; U.soon();
      const r = await U.api(what === 'rec' ? '/voice/people?you=' + encodeURIComponent(you) : '/voice/people/check',
        {method: 'POST', headers: {'Content-Type': 'audio/wav'}, body});
      if (!r.ok) return U.toast(r.status === 404 ? 'Trợ lý giọng nói (voiced) chưa chạy' : await r.text());
      const j = await r.json();
      if (what === 'check') return U.toast(j.who ? `Nhận ra ${j.who} (độ giống ${num(j.score)})`
        : `Chưa nhận ra giọng${j.score != null ? ` (giống nhất ${num(j.score)})` : ''}`);
      U.toast(`Đã ghi mẫu ${j.samples} của ${j.you}${j.score != null ? `, giống các mẫu trước ${num(j.score)}` : ''}`);
      people = null; loadPeople();
    }});

  // Đọc thông báo: mỗi loại (kind của notify trong config.yaml) một công tắc, lưu nhóm "notify" ({"<kind>": false} = tắt).
  const LABEL = {co2: 'CO₂ cao', bui: 'Bụi mịn PM2.5', mua: 'Trời bắt đầu mưa', nong: 'Nóng bất thường'};
  async function loadKinds() {
    kinds = [];
    if (U.DEMO) return;
    const r = await U.api('/api/notify');
    if (r.ok) { kinds = await r.json(); U.soon(); }
  }
  const on = k => U.sval('notify', k.kind) !== false;
  SETUP.push({id: 'notify', ns: 'notify', icon: 'vol', c: 'cyan', title: 'Đọc thông báo',
    meta: () => kinds?.length ? `${kinds.filter(on).length}/${kinds.length} loại đang bật` : '',
    body: () => {
      if (!kinds) loadKinds();
      if (!kinds?.length) return '<p class="note">Chưa có luật notify nào có kind trong config.yaml.</p>';
      const tg = (key, name) => `<label class="tgk">Telegram <input type="checkbox" data-sfield="notify.tg:${U.esc(key)}"${U.sval('notify', 'tg:' + key) !== false ? ' checked' : ''}></label>`;
      return `<div class="list">${kinds.map(k => `<div class="setrow"><span>${U.esc(LABEL[k.kind] || k.kind)}
          <small>${k.texts.length > 1 ? `${k.texts.length} chỗ đo` : U.esc(k.texts[0])}</small></span>
          <label class="tgk">Đọc <input type="checkbox" data-sfield="notify.${U.esc(k.kind)}"${on(k) ? ' checked' : ''}></label>${tg(k.kind)}</div>`).join('')}
          <div class="setrow"><span>Pin và cảnh báo khác (khói, rò nước…)<small>Cảnh báo không thuộc loại nào ở trên, gồm cả pin</small></span>${tg('khac')}</div></div>
        <div class="kv">${U.ssave('notify')}</div>
        <p class="note">Loại bị tắt thì không đọc thành tiếng, luật vẫn bật tắt thiết bị như cũ. Cảnh báo pin và an toàn luôn đọc.
          Đọc trên mọi màn hình đang mở dashboard (HTTP cũng được, chạm màn hình một lần sau khi mở).</p>`;
    }});
})();
