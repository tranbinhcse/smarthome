// Mục cài đặt Nhạc trên hub (music.go, nas.go): tình trạng các nguồn nhạc (thư mục trên hub, USB, NAS) và chọn thư mục nhạc
// trên NAS trong nhà (chia sẻ SMB): nhập địa chỉ, tài khoản, mật khẩu (chỉ lưu ở hub) → duyệt chia sẻ → thư mục → dùng thư mục đó.
(() => {
  const U = window.SETUP_UI;
  let LIB = null, libAt = 0, NB = null;   // NB: đang duyệt NAS {share, path, shares | dirs, mp3}
  const libLoad = async (rescan = false) => { libAt = Date.now(); const r = await fetch(`/api/music/library${rescan ? '?rescan=1' : ''}`); if (r.ok) { LIB = await r.json(); U.soon(); } };
  const nasBody = () => ({host: String(U.sval('music', 'nas_host')).trim(), user: String(U.sval('music', 'nas_user')).trim(), pass: U.DRAFT.music?.nas_pass || ''});

  function body() {
    if (!U.DEMO && Date.now() - libAt > 15000) libLoad();
    const roots = LIB?.roots || [], n = i => (LIB?.files || []).filter(f => f.id.startsWith(i + '/')).length;
    const saved = U.SAVED.music || {};
    let out = `<div class="list">${roots.map((r, i) => `<div class="setrow"><span>${U.esc(r.path)}</span><small class="${r.ok ? '' : 'bad'}">${r.ok ? `${n(i)} bài` : U.esc(r.error || 'chưa có')}</small></div>`).join('')
      || '<p class="note">Đang đọc...</p>'}</div>
      <p class="note">Chép file MP3 vào thư mục music của hub, hoặc cắm USB có nhạc. Thêm thư mục nhạc trên NAS trong nhà ở dưới.</p>
      <h4 class="mh">Thư mục nhạc trên NAS</h4>
      ${U.sinput('music', 'nas_host', 'Địa chỉ NAS', {ph: '192.168.1.50 hoặc nas.local'})}
      ${U.sinput('music', 'nas_user', 'Tài khoản', {ph: 'Tài khoản có quyền đọc thư mục nhạc'})}
      ${U.sinput('music', 'nas_pass', 'Mật khẩu', {secret: true})}
      <p class="note">${saved.nas_share ? `Đang dùng: //${U.esc(saved.nas_host)}/${U.esc(saved.nas_share)}${saved.nas_path ? '/' + U.esc(saved.nas_path) : ''}` : 'Chưa chọn thư mục NAS.'}</p>
      <div class="kv"><button class="pill amber" data-sact="music:browse">Duyệt NAS</button>${saved.nas_share ? '<button class="pill" data-sact="music:unset">Bỏ thư mục NAS</button>' : ''}</div>`;
    if (NB?.err) out += `<p class="note bad">${U.esc(NB.err)}</p>`;
    else if (NB?.shares) out += `<div class="list">${NB.shares.map(s => `<button class="setrow" data-sact="music:share" data-nas="${U.esc(s)}"><span>${U.icon('folder')} ${U.esc(s)}</span>${U.icon('more')}</button>`).join('') || '<p class="note">NAS không có chia sẻ nào cho tài khoản này.</p>'}</div>`;
    else if (NB?.dirs) out += `<p class="note">//${U.esc(nasBody().host)}/${U.esc(NB.share)}/${U.esc(NB.path)} · ${NB.mp3} bài MP3 ngay trong thư mục này</p>
      <div class="list"><button class="setrow" data-sact="music:up"><span>${U.icon('back')} Lên một cấp</span></button>
      ${NB.dirs.map(d => `<button class="setrow" data-sact="music:dir" data-nas="${U.esc(d)}"><span>${U.icon('folder')} ${U.esc(d)}</span>${U.icon('more')}</button>`).join('')}</div>
      <div class="kv"><button class="pill amber" data-sact="music:use">Dùng thư mục này</button></div>`;
    return out;
  }

  async function browse(share, path) {
    const r = await U.api('/api/music/nas', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({...nasBody(), share, path})});
    NB = r.ok ? {share, path, ...(await r.json())} : {err: await r.text()};
    U.soon();
  }

  async function act(what, el) {
    if (U.DEMO) return U.toast('Chế độ dữ liệu mẫu không nối NAS');
    if (what === 'browse') { if (!nasBody().host) return U.toast('Nhập địa chỉ NAS trước'); NB = {dirs: null}; return browse('', ''); }
    if (what === 'share') return browse(el.dataset.nas, '');
    if (what === 'dir') return browse(NB.share, NB.path ? `${NB.path}/${el.dataset.nas}` : el.dataset.nas);
    if (what === 'up') return NB.path ? browse(NB.share, NB.path.split('/').slice(0, -1).join('/')) : browse('', '');
    // lưu qua /api/music/nas: hub ràng buộc mật khẩu với đúng địa chỉ, tài khoản (mật khẩu trống = mật khẩu đã lưu cho đúng máy đó)
    const body = what === 'use' ? {do: 'save', ...nasBody(), share: NB.share, path: NB.path} : {do: 'unset'};
    const r = await U.api('/api/music/nas', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
    if (!r.ok) return U.toast(`Chưa lưu được: ${await r.text()}`);
    delete U.DRAFT.music;
    NB = null;
    await U.loadSetting('music');
    await libLoad(true);
    U.toast(what === 'use' ? 'Đã thêm thư mục NAS vào thư viện nhạc' : 'Đã bỏ thư mục NAS');
  }

  SETUP.push({id: 'music', ns: 'music', icon: 'music', c: 'amber', title: 'Nhạc trên hub', body, act,
    meta: () => LIB ? `${(LIB.files || []).length} bài` : ''});
})();
