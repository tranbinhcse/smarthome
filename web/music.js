// Trình phát đầy đủ (bảng "music:" trong SHEETS của dash.js), hai nguồn: "Trên hub" (file MP3 trong bộ nhớ hub, USB, phát tới
// các khu qua /api/music/*, music.go) và Spotify (bìa lớn, tiến độ, điều khiển, âm lượng, nơi phát; tìm bài/playlist/album/nghệ sĩ,
// playlist của tài khoản, bài đã thích, hàng đợi qua /api/spotify/*).
// Bảng không vẽ lại cả khối theo mỗi tin SSE (mất chữ đang gõ, mất chỗ cuộn): draw() chỉ gọi musicLive() để vẽ lại phần
// đang phát khi có đổi; danh sách chỉ vẽ lại khi chính nó đổi (tìm xong, mở playlist...). Cần dash.js (SP, spDevs, spCmd...).
'use strict';
Object.assign(ICON, {
  search: '<circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/>',
  shuffle: '<path d="M16 3h5v5M4 20 21 3M21 16v5h-5M15 15l6 6M4 4l5 5"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
});
const LIKED = {uri: 'liked', name: 'Bài hát đã thích', sub: 'Thư viện của bạn'};
const M = {tab: 'lists', q: '', res: null, lists: null, list: null, queue: null, cur: null, sig: '', drag: false,
  src: '', lib: null, folder: null, hq: '', hcur: null};   // src: hub | spotify; lib/folder/hq/hcur: thư viện nhạc trên hub
const mmss = ms => `${Math.floor(ms / 60000)}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}`;
const artBg = u => /^https:\/\/[^"'()\\\s]+$/.test(u || '') ? ` style="background-image:url('${esc(u)}')"` : '';
let spSeen = null, spAt = 0;   // lúc nhận SP hiện tại: tính tiến độ giữa hai lần hỏi Spotify
const spPos = () => { if (SP !== spSeen) { spSeen = SP; spAt = Date.now(); } return Math.min(SP.duration_ms || 0, (SP.progress_ms || 0) + (SP.playing ? Date.now() - spAt : 0)); };
const spTarget = () => SP.device?.id || spDevs?.[0]?.id || '';   // chưa có nơi phát: phát ở thiết bị đầu danh sách

function openMusic() {
  // nguồn đang phát; không bên nào phát thì giữ nguồn lần trước
  M.src = HM.playing ? 'hub' : SP.playing ? 'spotify' : M.src || (SP.connected || !HM.ok ? 'spotify' : 'hub');
  $('#modal').innerHTML = '';   // vẽ mới cả bảng
  openSheet('music:');
  hLoadLib();
  if (SP.connected && !spDevs) spLoadDevs();
  if (SP.connected && !M.lists) mLoadLists();
}
const hub = () => M.src === 'hub';
SHEETS.music = () => `<div class="m-box sheet widesheet musicsheet" data-music>
  <div class="seg msrc">${[['hub', 'Trên hub'], ['spotify', 'Spotify']].map(([v, l]) => `<button class="rb${M.src === v ? ' sel' : ''}" data-msrc="${v}">${l}</button>`).join('')}</div>
  <div class="mgrid"><section class="mnow" data-mnow data-uri="${esc(nowKey())}">${hub() ? hNowHtml() : nowHtml()}</section>
  <section class="mbrowse">${hub() ? `<label class="msearch">${icon('search')}<input type="search" data-hq value="${esc(M.hq)}" placeholder="Tìm bài trong thư viện nhạc"
    enterkeyhint="search" autocomplete="off" autocorrect="off" aria-label="Tìm bài trên hub"></label>`
    : `<label class="msearch">${icon('search')}<input type="search" data-mq value="${esc(M.q)}" placeholder="Tìm bài hát, playlist, album, nghệ sĩ"
    enterkeyhint="search" autocomplete="off" autocorrect="off" aria-label="Tìm trên Spotify"></label><div class="seg mtabs" data-mtabs>${tabsHtml()}</div>`}
  <div class="mlist" data-mbody>${bodyHtml()}</div></section></div>
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
const nowKey = () => (hub() ? HM.file?.id : SP.uri) || '';
const sig = () => hub() ? [HM.file?.id, HM.playing, HM.dur_ms, HM.source, SP.track, (HM.zones || []).map(z => [z.id, z.name, z.connected, z.volume, z.muted].join()).join('|')].join('|') : [SP.uri, SP.playing, SP.shuffle, SP.reconnect, SP.duration_ms, SP.device?.id, SP.device?.volume_percent, (spDevs || []).map(x => x.id + x.is_active).join()].join('|');
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
  if (hub()) return hBodyHtml();
  M.cur = null;
  if (!SP.connected) return '<p class="note">Chưa kết nối Spotify Premium. <button class="sub" data-go="caidat">Mở Cài đặt, mục Spotify</button></p>';
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
  const tabs = m.querySelector('[data-mtabs]');
  if (tabs) tabs.innerHTML = tabsHtml();
  box.scrollTop = keepScroll ? y : 0;
}
// draw() (mỗi tin SSE, mỗi lần hỏi Spotify) gọi vào đây thay vì vẽ lại cả bảng.
function musicLive(m) {
  if (M.drag || sig() === M.sig) return;
  const now = m.querySelector('[data-mnow]');
  const changed = nowKey() !== now.dataset.uri;
  now.innerHTML = hub() ? hNowHtml() : nowHtml();
  now.dataset.uri = nowKey();
  if (changed) mBody();   // tô bài / playlist đang phát
}
setInterval(() => {   // tiến độ chạy giữa hai lần hỏi Spotify (5 giây)
  if (!sheet.startsWith('music:') || M.drag) return;
  const r = $('#modal [data-mseek]'), t = $('#modal [data-mpos]'), p = $('#modal [data-hprog]'), pos = hub() ? hmPos() : spPos();
  if (r) r.value = pos;
  if (t) t.textContent = mmss(pos);
  if (p && HM.dur_ms) p.style.width = `${100 * pos / HM.dur_ms}%`;
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
document.addEventListener('pointerdown', e => { if (e.target.matches?.('[data-mseek], [data-mvol], [data-hzvol]')) M.drag = true; });
document.addEventListener('change', e => {
  const t = e.target;
  if (!t.matches?.('[data-mseek], [data-mvol], [data-hzvol]')) return;
  M.drag = false;
  if (t.matches('[data-hzvol]')) return hmCmd({do: 'zone', id: t.dataset.hzvol, volume: Number(t.value)});
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

/* ---------- Nhạc trên hub (music.go): file MP3 trong bộ nhớ hub, USB; phát tới các khu (đầu nhận Snapcast cạnh amply) ----------
   Hỏi /api/music/state 2 giây một lần khi đang phát hoặc đang mở bảng, 10 giây khi không. Tìm bài lọc ngay trên danh sách. */
let hmSeen = null, hmAt = 0;
const hmPos = () => { if (HM !== hmSeen) { hmSeen = HM; hmAt = Date.now(); } return Math.min(HM.dur_ms || 0, (HM.pos_ms || 0) + (HM.playing ? Date.now() - hmAt : 0)); };
async function hmFetch() {
  try { const r = await fetch('/api/music/state'); if (r.ok) { HM = {...await r.json(), ok: true}; soon(); } } catch (_) { /* mất mạng: giữ trạng thái cũ */ }
}
(function hmLoop() { if (!DEMO && !document.hidden) hmFetch(); setTimeout(hmLoop, HM.playing || sheet.startsWith('music:') ? 2000 : 10000); })();
async function hmCmd(body) {
  const r = await api('/api/music/cmd', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
  if (!r.ok) toast(await r.text());
  hmFetch();
  return r.ok;
}
async function hLoadLib() { M.lib = await mGet('/api/music/library'); mBody(); }
const hzOn = () => (HM.zones || []).filter(z => z.connected && !z.muted).length;
const hb = (v, l, ic, cls = 'rb sq') => `<button class="${cls}" data-hm="${v}" aria-label="${l}" title="${l}">${ic}</button>`;

// Thanh nhạc thu gọn (player() trong dash.js) khi nguồn là nhạc trên hub.
function hmBar() {
  return `<button class="pt" data-mfull aria-label="Mở trình phát đầy đủ"><b>${esc(HM.file?.title || 'Nhạc trên hub')}</b>
    <small>${esc(HM.file ? `${HM.file.folder || 'Nhạc trên hub'} · ${hzOn()} khu đang nghe` : 'Chạm để chọn bài')}</small></button>
    ${HM.file ? hb(HM.playing ? 'pause' : 'play', HM.playing ? 'Dừng' : 'Phát', icon(HM.playing ? 'pause' : 'play'), 'rb big') + hb('next', 'Bài sau', icon('next'), 'rb') : ''}`;
}

function hNowHtml() {
  M.sig = sig();
  const f = HM.file, zs = HM.zones || [];
  return `<div class="mart"></div>
    <div class="mtitle"><b>${esc(f?.title || 'Chưa phát bài nào')}</b><small>${esc(f ? f.folder || 'Nhạc trên hub' : 'Chọn bài trong thư viện bên cạnh')}</small></div>
    ${HM.source === 'spotify' ? `<p class="note">Các khu đang phát Spotify qua hub${SP.track ? `: ${esc(SP.track)}` : ''}. Chọn bài bên cạnh để chuyển sang nhạc trên hub.</p>` : ''}
    ${HM.dur_ms ? `<div class="mprog"><div class="bar"><i data-hprog style="width:${100 * hmPos() / HM.dur_ms}%"></i></div>
      <span><small data-mpos>${mmss(hmPos())}</small><small>${mmss(HM.dur_ms)}</small></span></div>` : ''}
    ${f ? `<div class="mctl">${hb('prev', 'Bài trước', icon('prev'))}${hb(HM.playing ? 'pause' : 'play', HM.playing ? 'Dừng' : 'Phát', icon(HM.playing ? 'pause' : 'play'), 'rb big pp')}${hb('next', 'Bài sau', icon('next'))}</div>` : ''}
    <h4 class="mh">Khu phát</h4>
    ${zs.length ? zs.map(z => { const n = esc(z.name || z.id); return `<div class="hz${z.connected ? '' : ' lost'}">
      <button class="sw${z.muted ? '' : ' on'}" data-hzmute="${esc(z.id)}" aria-pressed="${!z.muted}" aria-label="${z.muted ? 'Bật' : 'Tắt'} tiếng ${n}"${z.connected ? '' : ' disabled'}></button>
      <button class="hzn" data-hzname="${esc(z.id)}" title="Chạm để đổi tên khu"><b>${n}</b><small>${z.connected ? z.muted ? 'Tắt tiếng' : 'Bật tiếng' : 'Mất kết nối'}</small></button>
      <input type="range" data-hzvol="${esc(z.id)}" min="0" max="100" step="5" value="${z.volume}" aria-label="Âm lượng ${n}"></div>`; }).join('')
      : '<p class="note">Chưa có đầu nhận nào nối vào hub. Đầu nhận (ESP32 hoặc Pi chạy snapclient) cắm AUX vào amply, cùng mạng với hub, sẽ tự hiện ở đây.</p>'}`;
}

// Thư viện: danh sách thư mục → bài trong thư mục; ô tìm lọc theo tên bài và thư mục. Chạm một bài: phát danh sách đó từ bài này.
function hBodyHtml() {
  const lib = M.lib;
  M.hcur = null;
  if (!lib) return wait;
  if (lib.err) return `<p class="note bad">${esc(lib.err)}</p>`;
  const fs = lib.files, row = (x, i) => `<div class="mrow${x.id === HM.file?.id ? ' on' : ''}"><button class="mrhit" data-hplay="${i}"><i class="mthumb ic2">${icon('music')}</i>
    <span><b>${esc(x.title)}</b><small>${esc(x.folder || 'Thư mục gốc')}</small></span></button></div>`;
  if (!fs.length) return `<p class="note">Chưa có file MP3. Chép nhạc vào thư mục nhạc của hub hoặc cắm USB có nhạc.</p>
    ${lib.roots.map(r => `<p class="note">${esc(r.path)}: ${r.ok ? 'đang đọc' : 'chưa có'}</p>`).join('')}`;
  if (M.hq) {
    const q = fold(M.hq);
    M.hcur = fs.filter(x => fold(`${x.title} ${x.folder}`).includes(q)).slice(0, 300);
    return M.hcur.map(row).join('') || '<p class="empty">Không tìm thấy bài nào.</p>';
  }
  if (M.folder !== null) {
    M.hcur = fs.filter(x => x.folder === M.folder);
    return `<div class="mhead"><button class="rb sq" data-hback aria-label="Quay lại">${icon('back')}</button><i class="mthumb ic2">${icon('folder')}</i>
      <span><b>${esc(M.folder || 'Thư mục gốc')}</b><small>${M.hcur.length} bài</small></span><button class="pill amber" data-hplay="0">Phát</button></div>${M.hcur.map(row).join('')}`;
  }
  const n = new Map();
  for (const x of fs) n.set(x.folder, (n.get(x.folder) || 0) + 1);
  return [...n].map(([d, c]) => `<div class="mrow${HM.file?.folder === d ? ' on' : ''}"><button class="mrhit" data-hfolder="${esc(d)}"><i class="mthumb ic2">${icon('folder')}</i>
    <span><b>${esc(d || 'Thư mục gốc')}</b><small>${c} bài</small></span></button></div>`).join('');
}

document.addEventListener('input', e => { if (e.target.matches?.('[data-hq]')) { M.hq = e.target.value.trim(); mBody(false); } });
document.addEventListener('click', e => {
  const el = e.target.closest?.('[data-msrc], [data-hm], [data-hfolder], [data-hback], [data-hplay], [data-hzmute], [data-hzname]');
  if (!el) return;
  const ds = el.dataset, z = id => (HM.zones || []).find(x => x.id === id);
  if ('msrc' in ds) {
    M.src = ds.msrc;
    $('#modal').innerHTML = SHEETS.music();
    if (hub()) hLoadLib(); else { if (!spDevs) spLoadDevs(); if (!M.lists) mLoadLists(); }
    return;
  }
  if ('hm' in ds) { if (ds.hm === 'play' || ds.hm === 'pause') { HM.playing = ds.hm === 'play'; draw(); } return hmCmd({do: ds.hm}); }
  if ('hfolder' in ds) { M.folder = ds.hfolder; return mBody(false); }
  if ('hback' in ds) { M.folder = null; return mBody(false); }
  if ('hplay' in ds) {
    const list = M.hcur || [], i = Number(ds.hplay);
    if (!list[i]) return;
    HM.file = list[i]; HM.playing = true; draw();   // vẽ ngay, lần hỏi sau xác nhận
    return hmCmd({do: 'play', files: list.slice(0, 2000).map(x => x.id), index: Math.min(i, 1999)});
  }
  if ('hzmute' in ds) { const x = z(ds.hzmute); if (x) { x.muted = !x.muted; draw(); hmCmd({do: 'zone', id: x.id, muted: x.muted}); } return; }
  if ('hzname' in ds) {
    const x = z(ds.hzname), name = x && prompt('Tên khu (vd. Khu cafe, Phòng ngủ 1)', x.name || '');
    if (name !== null && name !== undefined && name.trim()) hmCmd({do: 'zone', id: x.id, name: name.trim()});
  }
});
