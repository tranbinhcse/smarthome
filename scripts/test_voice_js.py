"""Hội thoại với trợ lý trên dashboard (web/voice.js): chạy đúng tệp JS trong QuickJS, micro, loa, mạng, DOM đều giả.

Từng có lỗi: trợ lý hỏi xác nhận ("Ổ cắm Hồ Cá được bảo vệ, xác nhận bật không?") rồi im. Chạm nút nói chỉ ghi một câu,
hộp xác nhận che nút; nghe gọi tên thì câu kế sau một câu trả lời thường lại phải gọi tên. Người phải chạm hay gọi lại.
"""
import json
import unittest
from pathlib import Path

import quickjs

SRC = (Path(__file__).parent.parent / "web" / "voice.js").read_text()

# Một khung micro 4096 mẫu ở 16 kHz = 0,256 s. say(): 3 khung có tiếng + 3 khung im (0,77 s) là một câu, gửi ngay.
PRELUDE = r"""
var T = 1e6, LOG = [], FAILS = [], RESULT = null, REPLIES = [], ASKS = [], SETS = [], DIALOGS = [], PLAYED = [], TRACKS = [];
var NODE = null, PLAYER = null, EL = {};
globalThis.Date = {now: () => T};
var el = () => { var c = new Set(); return {classList: {add: x => c.add(x), remove: x => c.delete(x), toggle: (x, on) => on ? c.add(x) : c.delete(x),
  contains: x => c.has(x)}, setAttribute() {}, before(x) { EL[x.id] = x; }}; };
var bell = el();
globalThis.document = {createElement: el, getElementById: id => id === 'bell' ? bell : null, addEventListener() {}, removeEventListener() {}, hidden: false};
globalThis.window = globalThis;
globalThis.addEventListener = () => {};
globalThis.setTimeout = () => 0; globalThis.clearTimeout = () => {};
var store = () => { var m = {}; return Object.assign(m, {getItem: k => k in m ? m[k] : null, setItem: (k, v) => { m[k] = String(v); }, removeItem: k => { delete m[k]; }}); };
globalThis.localStorage = store(); globalThis.sessionStorage = store();
globalThis.DEMO = false; globalThis.SETUP_UI = {SAVED: {}}; globalThis.icon = () => '';
globalThis.toast = m => LOG.push('toast ' + m);
globalThis.ask = o => { DIALOGS.push(o.title); return new Promise(() => {}); };   // hộp xác nhận đang mở, người chưa bấm
globalThis.$ = () => ({close: v => LOG.push('đóng hộp ' + v)});
globalThis.cfg = {devices: [{id: 'oc', name: 'Ổ cắm Hồ Cá'}, {id: 'kho', name: 'Đèn Kho'}], scenes: []};
globalThis.setDev = async (d, to) => { SETS.push(d.id + ' ' + to); return true; };
globalThis.draw = () => {}; globalThis.runScene = () => {};
globalThis.EventSource = class { close() {} };
globalThis.Audio = class { constructor() { PLAYER = this; this.paused = true; } play() { PLAYED.push(this.src); return Promise.resolve(); } pause() { this.paused = true; } };
globalThis.AudioContext = class { constructor() { this.sampleRate = 16000; this.destination = {}; }
  createMediaStreamSource() { return {connect() {}, disconnect() {}}; } createScriptProcessor() { return NODE = {connect() {}, disconnect() {}}; }
  close() {} resume() {} };
globalThis.navigator = {mediaDevices: {getUserMedia: async () => { var t = {live: true, stop() { t.live = false; }}; TRACKS.push(t); return {getTracks: () => [t]}; }}};
var net = via => async url => {
  if (url.startsWith('/voice/ask')) { ASKS.push(via + ' ' + url); var j = REPLIES.shift() || {ignored: true}; return {ok: true, status: 200, json: async () => j}; }
  return {ok: true, status: 200, json: async () => ({})};
};
globalThis.fetch = net('fetch'); globalThis.api = net('api');
var frame = a => NODE.onaudioprocess({inputBuffer: {getChannelData: () => new Float32Array(4096).fill(a)}});
var tick = async () => { for (var i = 0; i < 50; i++) await null; };
var say = async () => { for (var a of [0.2, 0.2, 0.2, 0, 0, 0]) frame(a); await tick(); };
var check = (ok, msg) => { if (!ok) FAILS.push(msg); };
var live = () => TRACKS.some(t => t.live);
var mic = () => EL.mic;
"""


def run(scenario):
    ctx = quickjs.Context()
    ctx.eval(PRELUDE)
    ctx.eval(SRC)
    ctx.eval("(async () => { await tick();\n" + scenario + "\n})().then(() => { RESULT = 'ok'; }, e => { RESULT = String(e && e.stack || e); });")
    while ctx.execute_pending_job():
        pass
    return json.loads(ctx.eval("JSON.stringify({result: RESULT, fails: FAILS, asks: ASKS})"))


class TestVoiceConversation(unittest.TestCase):
    def assertRan(self, out):
        self.assertEqual(out["result"], "ok", out)
        self.assertEqual(out["fails"], [], out)

    def test_tap_then_confirm_by_voice_and_follow_up(self):
        # Chạm nút nói một lần: "bật ổ cắm hồ cá" → hỏi xác nhận → "có" → "tắt nó đi", không chạm lại, không gọi tên.
        self.assertRan(run(r"""
            mic().onclick(); await tick();
            check(live() && mic().classList.contains('rec'), 'chạm nút nói: micro mở, đang hội thoại');
            REPLIES.push({heard: 'bật ổ cắm hồ cá', reply: 'Ổ cắm Hồ Cá được bảo vệ, bạn xác nhận bật không?', audio: ['hoi'],
                          confirm: {ids: ['oc'], to: 'on', label: 'bật Ổ cắm Hồ Cá'}});
            await say();
            check(ASKS[0] === 'api /voice/ask', 'câu đầu sau khi chạm gửi đi không cần gọi tên: ' + ASKS[0]);
            check(DIALOGS[0] === 'Bật Ổ cắm Hồ Cá?', 'hiện hộp xác nhận: ' + DIALOGS);
            PLAYER.paused = false; T += 20000; frame(0); PLAYER.paused = true;   // trợ lý đọc câu hỏi (20 giây cho chắc)
            T += 7000; frame(0);
            check(live() && mic().classList.contains('rec'), 'đọc xong 7 giây vẫn nghe tiếp: lượt tính từ lúc đọc xong');
            REPLIES.push({heard: 'có', reply: 'Đã xác nhận, bật Ổ cắm Hồ Cá.', execute: {ids: ['oc'], to: 'on', label: 'bật Ổ cắm Hồ Cá'}});
            await say();
            check(ASKS[1] === 'api /voice/ask', 'trả lời "có" không cần gọi tên, không cần chạm: ' + ASKS[1]);
            check(SETS.join() === 'oc on', 'nói "có" thì màn hình gửi lệnh: ' + SETS);
            check(LOG.includes('đóng hộp 0'), 'nói "có" thì đóng hộp xác nhận');
            T += 3000;
            REPLIES.push({heard: 'tắt nó đi', reply: 'Ổ cắm Hồ Cá được bảo vệ, bạn xác nhận tắt không?',
                          confirm: {ids: ['oc'], to: 'off', label: 'tắt Ổ cắm Hồ Cá'}});
            await say();
            check(ASKS[2] === 'api /voice/ask', 'câu nối tiếp vẫn trong hội thoại: ' + ASKS[2]);
            T += 1000;
            var played = PLAYED.length;
            REPLIES.push({heard: '', reply: 'Tôi chưa nghe rõ, bạn nói lại nhé.', audio: ['on']});
            await say();
            check(PLAYED.length === played, 'tiếng ồn (không nghe ra chữ) thì không đọc');
            T += 8001; frame(0);
            check(!live() && !mic().classList.contains('rec'), 'im quá 8 giây (tiếng ồn không tính) thì tắt micro');
        """))

    def test_wake_word_then_follow_up_without_name(self):
        self.assertRan(run(r"""
            voiceEar.set(true); await tick();
            check(live() && mic().classList.contains('ear'), 'bật nghe gọi tên: micro mở, chờ gọi tên');
            await say();
            check(ASKS[0] === 'fetch /voice/ask?listen=1', 'ngoài hội thoại cần gọi tên: ' + ASKS[0]);
            T += 30000; frame(0);
            check(live(), 'nghe gọi tên thì micro mở mãi');
            REPLIES.push({heard: 'bật đèn kho', reply: 'Đã bật Đèn Kho.'});
            await say();
            check(ASKS[1] === 'fetch /voice/ask?listen=1', 'câu có gọi tên: ' + ASKS[1]);
            check(mic().classList.contains('rec'), 'trả lời xong thì vào hội thoại');
            T += 2000;
            REPLIES.push({heard: 'tắt đi', reply: 'Đã tắt Đèn Kho.'});
            await say();
            check(ASKS[2] === 'api /voice/ask', 'câu kế sau một câu trả lời thường không cần gọi tên: ' + ASKS[2]);
            T += 8001; frame(0);
            check(live() && mic().classList.contains('ear') && !mic().classList.contains('rec'), 'hết lượt: chờ gọi tên, micro vẫn mở');
            await say();
            check(ASKS[3] === 'fetch /voice/ask?listen=1', 'hết lượt thì lại cần gọi tên: ' + ASKS[3]);
        """))


if __name__ == "__main__":
    unittest.main()
