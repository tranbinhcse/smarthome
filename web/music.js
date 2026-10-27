// Trình phát Spotify đầy đủ (bảng "music:" trong SHEETS của dash.js): bìa lớn, tiến độ, điều khiển, âm lượng, nơi phát;
// tìm bài/playlist/album/nghệ sĩ, playlist của tài khoản, bài đã thích, hàng đợi. Mọi dữ liệu qua hub /api/spotify/*.
// Bảng không vẽ lại cả khối theo mỗi tin SSE (mất chữ đang gõ, mất chỗ cuộn): draw() chỉ gọi musicLive() để vẽ lại phần
// đang phát khi có đổi; danh sách chỉ vẽ lại khi chính nó đổi (tìm xong, mở playlist...). Cần dash.js (SP, spDevs, spCmd...).
'use strict';
Object.assign(ICON, {
  search: '<circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/>',
  shuffle: '<path d="M16 3h5v5M4 20 21 3M21 16v5h-5M15 15l6 6M4 4l5 5"/>',
});
const LIKED = {uri: 'liked', name: 'Bài hát đã thích', sub: 'Thư viện của bạn'};
const M = {tab: 'lists', q: '', res: null, lists: null, list: null, queue: null, cur: null, sig: '', drag: false};
const mmss = ms => `${Math.floor(ms / 60000)}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}`;
const artBg = u => /^https:\/\/[^"'()\\\s]+$/.test(u || '') ? ` style="background-image:url('${esc(u)}')"` : '';
let spSeen = null, spAt = 0;   // lúc nhận SP hiện tại: tính tiến độ giữa hai lần hỏi Spotify
const spPos = () => { if (SP !== spSeen) { spSeen = SP; spAt = Date.now(); } return Math.min(SP.duration_ms || 0, (SP.progress_ms || 0) + (SP.playing ? Date.now() - spAt : 0)); };
const spTarget = () => SP.device?.id || spDevs?.[0]?.id || '';   // chưa có nơi phát: phát ở thiết bị đầu danh sách

function openMusic() {
  $('#modal').innerHTML = '';   // vẽ mới cả bảng
  openSheet('music:');
  if (!spDevs) spLoadDevs();
  if (!M.lists) mLoadLists();
}
SHEETS.music = () => `<div class="m-box sheet widesheet musicsheet" data-music>
  <div class="mgrid"><section class="mnow" data-mnow data-uri="${esc(SP.uri || '')}">${nowHtml()}</section>
  <section class="mbrowse"><label class="msearch">${icon('search')}<input type="search" data-mq value="${esc(M.q)}" placeholder="Tìm bài hát, playlist, album, nghệ sĩ"
    enterkeyhint="search" autocomplete="off" autocorrect="off" aria-label="Tìm trên Spotify"></label>
  <div class="seg mtabs" data-mtabs>${tabsHtml()}</div><div class="mlist" data-mbody>${bodyHtml()}</div></section></div>
  <div class="m-act"><button class="m-btn" data-mcollapse>Thu gọn trình phát</button><button value="1" class="m-btn ok">Xong</button></div></div>`;

function nowHtml() {
  const d = SP.device, b = (v, l, ic, cls = 'rb sq') => `<button class="${cls}" data-sp="${esc(v)}" aria-label="${esc(l)}" title="${esc(l)}">${ic}</button>`;
  M.sig = sig();
  return `<div class="mart"${artBg(SP.art)}></div>
    <div class="mtitle"><b>${esc(SP.track || 'Chưa phát bài nào')}</b><small>${esc(SP.artist || (d ? d.name : 'Chọn thiết bị phát bên dưới'))}</small></div>
    ${SP.duration_ms ? `<div class="mprog"><input type="range" data-mseek min="0" max="${SP.duration_ms}" step="1000" value="${spPos()}" aria-label="Tua bài">
      <span><small data-mpos>${mmss(spPos())}</small><small>${mmss(SP.duration_ms)}</small></span></div>` : ''}
    ${d ? `<div class="mctl"><button class="rb sq${SP.shuffle ? ' sel' : ''}" data-mshuf aria-pressed="${!!SP.shuffle}" aria-label="Trộn bài" title="Trộn bài">${icon('shuffle')}</button>
      ${b('prev', 'Bài trước', icon('prev'))}${b(SP.playing ? 'pause' : 'play', SP.playing ? 'Dừng' : 'Phát', icon(SP.playing ? 'pause' : 'play'), 'rb big pp')}${b('next', 'Bài sau', icon('next'))}</div>` : ''}
    ${d?.supports_volume ? `<label class="mvol">${icon('vol')}<input type="range" data-mvol min="0" max="100" step="5" value="${d.volume_percent ?? 50}" aria-label="Âm lượng ${esc(d.name)}"></label>` : ''}
    <div class="mdevs">${(spDevs || []).map(x => `<button class="rb${x.is_active ? ' sel' : ''}" data-sp="transfer:${esc(x.id)}">${icon('cast')}${esc(x.name)}</button>`).join('')
      || '<small class="note">Không thấy thiết bị nào. Bật đầu thu Spotify, hoặc mở app Spotify trên điện thoại cùng tài khoản.</small>'}</div>
    ${SP.reconnect ? '<p class="note">Cần cấp thêm quyền để xem playlist và bài đã thích. <button class="sub" data-sact="spotify:login">Kết nối lại Spotify</button></p>' : ''}`;
}
const sig = () => [SP.uri, SP.playing, SP.shuffle, SP.reconnect, SP.duration_ms, SP.device?.id, SP.device?.volume_percent, (spDevs || []).map(x => x.id + x.is_active).join()].join('|');
const tabsHtml = () => M.q ? '<button class="rb sel">Kết quả tìm</button>'
  : [['lists', 'Playlist'], ['queue', 'Hàng đợi']].map(([v, l]) => `<button class="rb${M.tab === v ? ' sel' : ''}" data-mtab="${v}">${l}</button>`).join('');

// Một hàng: bài (chạm = phát, nút + = thêm vào hàng đợi) hoặc playlist/album (chạm = xem bài), nghệ sĩ (chạm = phát).
function rowHtml(x, i, how) {
  const kind = x.uri.split(':')[1], on = x.uri === SP.uri || x.uri === SP.context;
  const hit = kind === 'track' ? `data-mplay="${how}:${i}"` : kind === 'artist' ? `data-mctx="${esc(x.uri)}"` : `data-mopen="${esc(x.uri)}"`;
  const sub = x === LIKED ? x.sub : kind === 'track' ? `${x.sub || ''}${x.ms ? ' · ' + mmss(x.ms) : ''}` : kind === 'playlist' ? `${x.sub ? x.sub + ' · ' : ''}${x.total ?? ''} bài`
    : kind === 'album' ? `Album · ${x.sub || ''}` : 'Nghệ sĩ';
  return `<div class="mrow${on ? ' on' : ''}"><button class="mrhit" ${hit}><i class="mthumb${kind === 'artist' ? ' round' : x === LIKED ? ' liked' : ''}"${artBg(x.art)}></i>
    <span><b>${esc(x.name)}</b><small>${esc(sub)}</small></span></button>${kind === 'track'
    ? `<button class="rb sq" data-mqadd="${esc(x.uri)}" aria-label="Thêm ${esc(x.name)} vào hàng đợi" title="Thêm vào hàng đợi">${icon('plus')}</button>` : ''}</div>`;
}
const rows = (xs, how) => xs.map((x, i) => rowHtml(x, i, how)).join('');
const wait = '<p class="empty">Đang tải…</p>';

function bodyHtml() {
  M.cur = null;
  if (M.list) {
    const l = M.list.item, tr = M.list.tracks;
    if (tr) M.cur = {ctx: l.uri === 'liked' ? '' : l.uri, tracks: tr};
    return `<div class="mhead"><button class="rb sq" data-mback aria-label="Quay lại">${icon('back')}</button><i class="mthumb"${artBg(l.art)}></i>
      <span><b>${esc(l.name)}</b><small>${esc(l.sub || '')}</small></span>${l.uri === 'liked' ? (tr?.length ? '<button class="pill amber" data-mplay="list:0">Phát</button>' : '')
      : `<button class="pill amber" data-mctx="${esc(l.uri)}">Phát</button>`}</div>
      ${M.list.err ? `<p class="note bad">${esc(M.list.err)}</p>` : !tr ? wait : tr.length ? rows(tr, l.uri === 'liked' ? 'list' : 'ctx') : '<p class="empty">Không có bài nào phát được.</p>'}`;
  }
  if (M.q) {
    const r = M.res;
    if (!r) return wait;
    if (r.err) return `<p class="note bad">${esc(r.err)}</p>`;
    M.cur = {ctx: '', tracks: r.tracks};
    const sec = (t, xs, how) => xs.length ? `<h4 class="mh">${t}</h4>${rows(xs, how)}` : '';
    return sec('Bài hát', r.tracks, 'list') + sec('Playlist', r.playlists) + sec('Album', r.albums) + sec('Nghệ sĩ', r.artists) || '<p class="empty">Không tìm thấy gì.</p>';
  }
  if (M.tab === 'queue') {
    if (!M.queue) return wait;
    if (M.queue.err) return `<p class="note bad">${esc(M.queue.err)}</p>`;
    M.cur = {ctx: '', tracks: M.queue};   // chạm một bài trong hàng đợi: phát từ bài đó
    return M.queue.length ? rows(M.queue, 'list') : '<p class="empty">Hàng đợi trống. Bấm + ở một bài để thêm.</p>';
  }
  if (!M.lists) return wait;
  if (M.lists.err) return `<p class="note bad">${esc(M.lists.err)}</p>`;
  return rows([LIKED, ...M.lists]);
}

// Vẽ lại danh sách (giữ chỗ cuộn khi vẫn là danh sách đó) và nhãn tab.
function mBody(keepScroll = true) {
  const m = $('#modal'), box = m.querySelector('[data-mbody]');
  if (!box || !sheet.startsWith('music:')) return;
  const y = box.scrollTop;
  box.innerHTML = bodyHtml();
  m.querySelector('[data-mtabs]').innerHTML = tabsHtml();
  box.scrollTop = keepScroll ? y : 0;
}
// draw() (mỗi tin SSE, mỗi lần hỏi Spotify) gọi vào đây thay vì vẽ lại cả bảng.
function musicLive(m) {
  if (M.drag || sig() === M.sig) return;
  const now = m.querySelector('[data-mnow]');
  const changed = SP.uri !== now.dataset.uri;
  now.innerHTML = nowHtml();
  now.dataset.uri = SP.uri || '';
  if (changed) mBody();   // tô bài / playlist đang phát
}
setInterval(() => {   // tiến độ chạy giữa hai lần hỏi Spotify (5 giây)
  if (!sheet.startsWith('music:') || M.drag) return;
  const r = $('#modal [data-mseek]'), t = $('#modal [data-mpos]');
  if (r) r.value = spPos();
  if (t) t.textContent = mmss(spPos());
}, 1000);

async function mGet(url) {
  const r = await api(url);
  return r.ok ? r.json() : {err: await r.text()};
}
async function mLoadLists() { const j = await mGet('/api/spotify/playlists'); M.lists = j.err ? j : j.playlists; mBody(); }
async function mLoadQueue() { const j = await mGet('/api/spotify/queue'); M.queue = j.err ? j : j.queue; mBody(); }
let mTimer = 0;
document.addEventListener('input', e => {
  if (e.target.matches?.('[data-mq]')) {
    clearTimeout(mTimer);
    mTimer = setTimeout(async () => {
      const q = e.target.value.trim();
      if (q === M.q) return;
      M.q = q; M.res = null; M.list = null;
      mBody(false);
      if (!q) return;
      const j = await mGet(`/api/spotify/search?q=${encodeURIComponent(q)}`);
      if (M.q === q) { M.res = j; mBody(false); }   // bỏ kết quả của câu tìm cũ về muộn
    }, 350);
  }
});
// Kéo thanh tua / âm lượng: không vẽ lại phần đang phát giữa chừng; thả tay mới gửi lệnh.
document.addEventListener('pointerdown', e => { if (e.target.matches?.('[data-mseek], [data-mvol]')) M.drag = true; });
document.addEventListener('change', e => {
  const t = e.target;
  if (!t.matches?.('[data-mseek], [data-mvol]')) return;
  M.drag = false;
  if (t.matches('[data-mvol]')) { if (SP.device) SP.device.volume_percent = Number(t.value); spCmd({do: 'volume', volume: Number(t.value)}); }
  else { SP.progress_ms = Number(t.value); spSeen = null; spCmd({do: 'seek', position_ms: Number(t.value)}); }
});
addEventListener('pointerup', () => { if (M.drag) setTimeout(() => { M.drag = false; }, 300); });

document.addEventListener('click', async e => {
  const el = e.target.closest?.('[data-mfull], [data-mopen], [data-mback], [data-mtab], [data-mplay], [data-mctx], [data-mqadd], [data-mshuf], [data-mcollapse]');
  if (!el) return;
  const ds = el.dataset;
  if ('mfull' in ds) return openMusic();
  if ('mcollapse' in ds) { playerOpen = false; volOpen = ''; spDevOpen = false; return draw(); }
  if ('mback' in ds) { M.list = null; return mBody(false); }
  if ('mtab' in ds) { M.tab = ds.mtab; mBody(false); return M.tab === 'queue' ? mLoadQueue() : M.lists?.err && mLoadLists(); }
  if ('mshuf' in ds) { SP.shuffle = !SP.shuffle; draw(); return spCmd({do: 'shuffle', state: SP.shuffle}); }
  if ('mqadd' in ds) { if (await spCmd({do: 'queue', uri: ds.mqadd, device: spTarget()})) { toast('Đã thêm vào hàng đợi'); M.queue = null; if (M.tab === 'queue' && !M.q) mLoadQueue(); } return; }
  if ('mopen' in ds) {
    const all = [...(M.lists?.length ? M.lists : []), ...(M.res?.playlists || []), ...(M.res?.albums || [])];
    const item = ds.mopen === 'liked' ? LIKED : all.find(x => x.uri === ds.mopen);
    M.list = {item, tracks: null};
    mBody(false);
    const j = await mGet(`/api/spotify/tracks?of=${encodeURIComponent(ds.mopen)}`);
    if (M.list?.item === item) { M.list = j.err ? {item, err: j.err} : {item, tracks: j.tracks}; mBody(false); }
    return;
  }
  const dev = spTarget();
  if ('mctx' in ds) return spCmd({do: 'play', context: ds.mctx, device: dev});
  const [how, i] = ds.mplay.split(':'), c = M.cur, x = c?.tracks[+i];
  if (!x) return;
  // trong playlist/album: phát cả danh sách từ bài được chọn; còn lại (tìm, bài đã thích, hàng đợi): phát từ bài đó trở đi
  if (how === 'ctx' && c.ctx) spCmd({do: 'play', context: c.ctx, offset: x.uri, device: dev});
  else spCmd({do: 'play', uris: c.tracks.slice(+i, +i + 50).map(t => t.uri), device: dev});
  SP.uri = x.uri; SP.track = x.name; SP.artist = x.sub; SP.art = x.art || SP.art; SP.playing = true;   // vẽ ngay, lần hỏi sau xác nhận
  draw();
});
