// Mục cài đặt Trợ lý AI (voiced gọi API chat completions tương thích OpenAI) và Đọc thông báo (bật/tắt từng loại notify).
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
      return `<div class="list">${kinds.map(k => `<label class="setrow"><span>${U.esc(LABEL[k.kind] || k.kind)}
          <small>${k.texts.length > 1 ? `${k.texts.length} chỗ đo` : U.esc(k.texts[0])}</small></span>
          <input type="checkbox" data-sfield="notify.${U.esc(k.kind)}"${on(k) ? ' checked' : ''}></label>`).join('')}</div>
        <div class="kv">${U.ssave('notify')}</div>
        <p class="note">Loại bị tắt thì không đọc thành tiếng, luật vẫn bật tắt thiết bị như cũ. Cảnh báo pin và an toàn luôn đọc.
          Đọc trên mọi màn hình đang mở dashboard (HTTP cũng được, chạm màn hình một lần sau khi mở).</p>`;
    }});
})();
