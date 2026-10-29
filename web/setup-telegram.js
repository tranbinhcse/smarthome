// Mục cài đặt Telegram (telegram.go): hub gửi cảnh báo và báo cáo hằng ngày qua bot Telegram của chủ nhà.
// Token bot là bí mật: chỉ lưu ở hub, trình duyệt chỉ biết đã lưu hay chưa (bot_token_set). Người nhận phải được duyệt bằng PIN hub.
// PIN không đi qua data-sfield/DRAFT (nút "Lưu" sẽ ghi nó vào cấu hình): ô PIN chỉ được đọc lúc bấm Duyệt rồi xoá ngay.
(() => {
  const U = window.SETUP_UI;
  let T = null, at = 0, pinFor = null;
  const DEMO_T = {token_set: true, bot: 'inix_demo_bot', chats: [{id: 1, name: 'Mẹ'}], pending: [{id: 2, name: 'Khách mới'}], error: ''};
  const load = async () => {
    at = Date.now();
    if (U.DEMO) { T = DEMO_T; return U.soon(); }
    const r = await fetch('/api/telegram');
    if (r.ok) { T = await r.json(); U.soon(); }
  };
  const meta = () => !T ? '' : !T.token_set ? 'Chưa có bot' : T.error ? 'Lỗi' : `${T.chats.length} người nhận`;

  function body() {
    // không nạp lại khi đang nhập PIN: màn hình vẽ lại sẽ xoá ô PIN
    if (!pinFor && Date.now() - at > 10000) load();
    const id = c => Number(c.id);
    const chats = (T?.chats || []).map(c => `<div class="setrow"><span>${U.esc(c.name)}</span>
      <button class="pill" data-sact="telegram:remove:${id(c)}">Bỏ</button></div>`).join('');
    const pend = (T?.pending || []).map(c => `<div class="setrow"><span>${U.esc(c.name)}</span>
      ${pinFor === id(c)
        ? `<input id="tg-pin" type="password" inputmode="numeric" autocomplete="off" placeholder="PIN hub" style="width:110px">
           <button class="pill amber" data-sact="telegram:approve:${id(c)}">Xác nhận</button>`
        : `<button class="pill" data-sact="telegram:ask:${id(c)}">Duyệt</button>`}</div>`).join('');
    return `<p class="note">Nhận cảnh báo và báo cáo của nhà qua Telegram. Tạo bot riêng:<br>
        1. Mở <b>@BotFather</b> trong Telegram.<br>
        2. Gửi <b>/newbot</b>, rồi chọn tên cho bot.<br>
        3. Chép token BotFather trả về, dán vào ô dưới và bấm Lưu.<br>
        Token chỉ lưu ở hub, không hiện lại.</p>
      ${U.sinput('telegram', 'bot_token', 'Token bot', {secret: true, ph: 'Dán token từ BotFather'})}
      ${U.sinput('telegram', 'daily_at', 'Báo cáo hằng ngày lúc (để trống là tắt)', {type: 'time'})}
      <div class="kv">${U.ssave('telegram')}<button class="pill" data-sact="telegram:test">Gửi thử</button></div>
      ${T?.error ? `<p class="note bad">${U.esc(T.error)}</p>` : T?.bot ? `<p class="note">Bot: <b>@${U.esc(T.bot)}</b>. Mở bot trong Telegram và gửi một tin bất kỳ, người đó sẽ hiện ở danh sách chờ duyệt.</p>` : ''}
      <h4 class="mh">Người nhận</h4>
      ${chats ? `<div class="list">${chats}</div>` : '<p class="note">Chưa có người nhận.</p>'}
      ${pend ? `<h4 class="mh">Chờ duyệt</h4><div class="list">${pend}</div><p class="note">Duyệt cần PIN của hub.</p>` : ''}
      <p class="note">Chọn loại cảnh báo gửi qua Telegram trong mục Đọc thông báo (cột Telegram).</p>`;
  }

  const json = (m, b) => ({method: m, headers: {'Content-Type': 'application/json'}, body: JSON.stringify(b)});
  async function act(what, el) {
    const [verb, arg] = what.split(':'), cid = Number(arg);
    if (verb === 'ask') { pinFor = cid; U.soon(); return setTimeout(() => document.getElementById('tg-pin')?.focus(), 50); }
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không gọi Telegram');
    if (verb === 'approve') {
      const box = document.getElementById('tg-pin'), pin = box?.value || '';
      if (box) box.value = '';
      if (!pin) return U.toast('Nhập PIN của hub');
      // fetch thường, không qua U.api: api() coi 401 là phiên hết hạn, hiện ô đăng nhập rồi gửi lại cùng PIN sai
      const r = await fetch('/api/telegram/approve', json('POST', {id: cid, pin}));
      if (r.status === 401) return U.toast('Sai PIN');
      if (r.status === 429) return U.toast('Nhập sai nhiều lần, thử lại sau ít phút');
      if (!r.ok) return U.toast(await r.text());
      pinFor = null; U.toast('Đã duyệt'); return load();
    }
    if (verb === 'remove') {
      const r = await U.api(`/api/telegram/chats/${cid}`, {method: 'DELETE'});
      if (!r.ok) return U.toast(await r.text());
      U.toast('Đã bỏ người nhận'); return load();
    }
    if (verb === 'test') {
      const r = await U.api('/api/telegram/test', {method: 'POST'});
      U.toast(r.status === 409 ? 'Chưa có người nhận nào' : r.ok ? 'Đã gửi tin thử' : await r.text());
    }
  }

  SETUP.push({id: 'telegram', ns: 'telegram', icon: 'bell', c: 'blue', title: 'Telegram', body, act, meta});
})();
