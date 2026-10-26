// Dùng chung: gọi API (hiện ô PIN khi 401) + khung đăng nhập.
async function api(url, opt) {
  const r = await fetch(url, opt);
  if (r.status === 401) { await needLogin(); return api(url, opt); }
  return r;
}
function needLogin() {
  return new Promise(res => {
    const el = document.getElementById('login');
    el.style.display = 'flex';
    const inp = el.querySelector('input'), msg = el.querySelector('p');
    const go = async () => {
      const r = await fetch('/api/login', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({pin: inp.value})});
      if (r.ok) { el.style.display = 'none'; inp.value = ''; msg.textContent = ''; res(); }
      else { msg.textContent = r.status === 429 ? 'Thử lại sau ít phút' : 'Sai PIN'; inp.value = ''; }
    };
    el.querySelector('button').onclick = go;
    inp.onkeydown = e => { if (e.key === 'Enter') go(); };
    inp.focus();
  });
}
const esc = s => String(s).replace(/[&<>"]/g, c => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;'}[c]));
const LOGIN_HTML = '<div id="login"><h2>Nhập PIN</h2><input type="password" inputmode="numeric" autocomplete="current-password"><p></p><button>Vào</button></div>';
