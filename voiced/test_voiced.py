import contextlib, io, json, re, socket, tempfile, threading, time, unittest, urllib.error, urllib.request, wave
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from types import SimpleNamespace as NS

import numpy as np

import voiced

# Thiết bị thật: các dòng JSON gen_config.py sinh trong config.yaml (giữa BEGIN/END tuya-devices).
_CFG_TEXT = (Path(__file__).parent.parent / "config.yaml").read_text()
_DEV_BLOCK = _CFG_TEXT[_CFG_TEXT.index("# BEGIN tuya-devices"):_CFG_TEXT.index("# END tuya-devices")]
REAL_DEVICES = [json.loads(l.strip()[2:]) for l in _DEV_BLOCK.splitlines() if l.strip().startswith('- {"id"')]

CFG = {
    "zones": [{"id": "kho", "name": "Phòng kho"}, {"id": "cafe", "name": "Khu cafe"}, {"id": "kythuat", "name": "Tum kỹ thuật"}],
    "devices": REAL_DEVICES,
    "scenes": [{"id": "ra_khoi_nha", "name": "Ra khỏi nhà", "steps": [{"d": "tuya_eba30c56ca208baad0y43a", "to": "off"}]},
               {"id": "di_ngu", "name": "Đi ngủ", "steps": []},
               {"id": "dong_quan", "name": "Đóng quán", "pin": True, "steps": []}],
    "entities": [
        {"id": "cb_phonglanh.w", "name": "Công suất", "unit": "W", "zone": "cafe", "device": "tuya_ebae9da4b1b5eeec58d8ci"},
        {"id": "cb_phonglanh.v", "name": "Điện áp", "unit": "V", "zone": "cafe", "device": "tuya_ebae9da4b1b5eeec58d8ci"},
        {"id": "cb_phonglanh.a", "name": "Dòng điện", "unit": "A", "zone": "cafe", "device": "tuya_ebae9da4b1b5eeec58d8ci"},
        {"id": "deye.pv", "name": "Điện mặt trời", "unit": "W", "zone": "kythuat"},
        {"id": "deye.grid", "name": "Điện lưới", "unit": "W", "zone": "kythuat"},
        {"id": "jk.soc", "name": "Pin lưu trữ", "unit": "%", "zone": "kythuat"},
    ],
    "locked": [d["id"] for d in REAL_DEVICES if d.get("protected")] + ["dong_quan"],
    "phrases": ["Có khói ở bếp"],
}


class FakeHub:
    """Giả API trợ lý của hubd, cùng quy tắc chặn: id trong locked → 403."""
    def __init__(self, values=None, settings=None, cfg=CFG):
        self.calls, self.values, self.alerts, self.settings, self.cfg, self.voice = [], values or {}, [], settings or {}, cfg, {}

    def __call__(self, method, path, body=None):
        self.calls.append((method, path, body))
        if path == "/config":
            return 200, self.cfg
        if path == "/state":
            return 200, {"values": self.values, "alerts": self.alerts}
        if path == "/settings/ai":
            return 200, self.settings
        if path == "/settings/voice":
            return 200, self.voice
        if path.startswith("/history"):
            return 200, [{"t": 1000, "v": 80}, {"t": 2000, "v": 90}, {"t": 3000, "v": 85}]
        target = body["device"] if path == "/cmd" else path.rsplit("/", 1)[-1]
        if target in self.cfg["locked"]:
            return 403, "cần PIN"
        return 204, None


def plain_variant(s):
    return voiced.plain(s)


class TestMatch(unittest.TestCase):
    def test_device_commands_sweep(self):
        # Quét mọi thiết bị thật × 3 động từ × 3 dạng chữ (gốc, CHỮ HOA như STT trả, mất dấu) = 9×3×3.
        n = 0
        for d in REAL_DEVICES:
            for verb, to in (("bật", "on"), ("mở", "on"), ("tắt", "off")):
                for form in (str, str.upper, plain_variant):
                    n += 1
                    text = form(f"{verb} {d['name']} giúp tôi")
                    got = voiced.match(text, CFG)
                    self.assertEqual(got and (got[0], got[1]["id"], got[2]), ("set", d["id"], to), text)
        self.assertEqual(n, len(REAL_DEVICES) * 9)
        self.assertGreaterEqual(len(REAL_DEVICES), 9)

    def test_scene_entity_ack_and_fallthrough(self):
        cases = {
            "RA KHỎI NHA": ("scene", "ra_khoi_nha"),            # STT mất dấu "nhà"
            "đi ngủ thôi": ("scene", "di_ngu"),
            "pin còn bao nhiêu": ("ask_entity", "jk.soc"),
            "ĐIỆN MẶT TRỜI ĐANG PHÁT BAO NHIÊU": ("ask_entity", "deye.pv"),
            "điện lưới bao nhiêu": ("ask_entity", "deye.grid"),
            "công suất phòng lạnh là bao nhiêu": ("ask_entity", "cb_phonglanh.w"),
            "đèn kho đang bật không": ("ask_device", "tuya_eba30c56ca208baad0y43a"),
            "tắt báo động": ("ack", None),
            "tắt còi": ("ack", None),
            "tình hình thế nào": ("brief", None),
            "TÓM TẮT TÌNH HÌNH NHÀ": ("brief", None),
        }
        for text, want in cases.items():
            got = voiced.match(text, CFG)
            self.assertEqual(got and (got[0], got[1] and got[1]["id"]), want, text)
        # Câu tự do / mơ hồ: để Claude xử lý, không đoán.
        for text in ("hôm qua nhà mình dùng hết bao nhiêu điện", "bật đèn", "trời hôm nay thế nào", ""):
            self.assertIsNone(voiced.match(text, CFG), text)
        # Hai thiết bị trùng tên ở hai phòng: không tự chọn một cái.
        twins = {**CFG, "devices": [{"id": "a", "name": "Đèn trần", "zone": "kho"}, {"id": "b", "name": "Đèn trần", "zone": "cafe"}]}
        self.assertIsNone(voiced.match("bật đèn trần", twins))

    def test_question_frame_words_never_name_a_measurement(self):
        # "có bao nhiêu đèn đang mở" từng trả "Có người chưa có số đo": chữ "có" của khung câu hỏi được lấy làm từ đầu tên số đo.
        # Quét mọi từ khung (voiced.FRAME) trên mọi số đo thật của config.yaml: câu chỉ có khung hỏi không chọn số đo nào,
        # còn số đo có tên bắt đầu bằng từ khung vẫn hỏi được khi nói đủ tên.
        import yaml
        real = yaml.safe_load(_CFG_TEXT)
        cfg = {**CFG, "zones": real["zones"], "entities": real["entities"]}
        for f in sorted(voiced.FRAME):
            self.assertIsNone(voiced.match(f"{f} bao nhiêu đèn đang mở", cfg), f)
        for t in ("có bao nhiêu đèn đang mở", "kiểm tra xem có bao nhiêu đèn đang mở"):
            self.assertIsNone(voiced.match(t, cfg), t)
        framed = [e for e in real["entities"] if voiced.words(e["name"])[0] in voiced.FRAME]
        self.assertTrue(framed)
        for e in framed:
            got = voiced.match(f"{e['name']} không", cfg)
            self.assertEqual(got and (got[0], got[1]["id"]), ("ask_entity", e["id"]), e["name"])


class TestRun(unittest.TestCase):
    def test_run_replies_and_pin_block(self):
        hub = FakeHub({"jk.soc": "85", "deye.pv": "1234.5", "dev:tuya_eba30c56ca208baad0y43a": "ON",
                       "online:tuya_eb89a0372bd6a5acf1owhi": "offline"})
        say = lambda t: voiced.run(voiced.match(t, CFG), hub)
        self.assertEqual(say("bật đèn kho"), "Đã bật Đèn Kho.")
        # thiết bị / ngữ cảnh có khoá: hubd chặn (403) → hỏi lại và trả hộp xác nhận cho màn hình; voiced không tự làm
        c = voiced.Conv()
        lock = lambda t: voiced.run(voiced.match(t, CFG), hub, voiced.Persona(), c)
        self.assertEqual(lock("tắt CB tổng bếp"), "CB Tổng Bếp được bảo vệ, bạn xác nhận tắt không?")
        self.assertEqual(hub.calls[-1], ("POST", "/cmd", {"device": "tuya_eb6564a5a81a53bb8ayyzs", "to": "off"}))
        self.assertEqual(c.take(), {"confirm": {"ids": ["tuya_eb6564a5a81a53bb8ayyzs"], "to": "off", "label": "tắt CB Tổng Bếp"}})
        self.assertEqual(say("ra khỏi nhà"), "Đã chạy Ra khỏi nhà.")
        self.assertEqual(lock("đóng quán"), "Đóng quán được bảo vệ, bạn xác nhận chạy không?")
        self.assertEqual(c.take(), {"confirm": {"scene": "dong_quan", "label": "chạy Đóng quán"}})
        self.assertEqual(say("pin còn bao nhiêu"), "Pin lưu trữ 85 phần trăm.")
        self.assertEqual(say("điện mặt trời bao nhiêu"), "Điện mặt trời 1234,5 oát.")
        self.assertEqual(say("đèn kho bật chưa"), "Đèn Kho đang bật.")
        self.assertEqual(say("đèn phòng lạnh đang bật không"), "Đèn phòng lạnh đang mất kết nối.")
        self.assertEqual(say("điện lưới bao nhiêu"), "Điện lưới chưa có số đo.")
        self.assertEqual(say("tắt báo động"), "Đã xác nhận cảnh báo.")
        self.assertEqual(hub.calls[-1], ("POST", "/alarm/ack", None))
        self.assertEqual(say("tình hình thế nào"), voiced.brief(CFG, {"values": hub.values, "alerts": []}))


class FakeOpenAI:
    """Endpoint giả tương thích OpenAI (GET /v1/models, POST /v1/chat/completions) chạy trên 127.0.0.1:
    trả lần lượt các message soạn sẵn, ghi lại (method, path, Authorization, body) của mọi request. Không gọi API thật."""
    def __init__(self, *replies, models=("gpt-b", "gpt-a"), status=200, location=None):
        self.replies, self.models, self.status, self.reqs, self.location = list(replies), models, status, [], location
        fake = self

        class H(BaseHTTPRequestHandler):
            def do_GET(self):
                fake.handle(self, None)

            def do_POST(self):
                fake.handle(self, json.loads(self.rfile.read(int(self.headers["Content-Length"]))))

            def log_message(self, *a):
                pass

        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.srv.server_port}/v1"

    def handle(self, h, body):
        self.reqs.append((h.command, h.path, h.headers.get("Authorization"), body))
        if self.status != 200:
            code, out = self.status, {"error": {"message": "lỗi giả"}}
        elif h.path == "/v1/models" and self.models is not None:
            code, out = 200, {"object": "list", "data": [{"id": m, "object": "model"} for m in self.models]}
        elif h.path == "/v1/chat/completions":
            code, out = 200, {"choices": [{"index": 0, "message": self.replies.pop(0), "finish_reason": "stop"}]}
        else:
            code, out = 404, {"error": {"message": "không có"}}
        b = json.dumps(out).encode()
        h.send_response(code)
        if self.location:
            h.send_header("Location", self.location)
        h.send_header("Content-Type", "application/json")
        h.send_header("Content-Length", str(len(b)))
        h.end_headers()
        h.wfile.write(b)

    def chats(self):
        return [r for r in self.reqs if r[1] == "/v1/chat/completions"]

    def close(self):
        self.srv.shutdown(), self.srv.server_close()


def tc(id, name, args):
    return {"id": id, "type": "function", "function": {"name": name, "arguments": args if isinstance(args, str) else json.dumps(args)}}


def say(text):
    return {"role": "assistant", "content": text}


def closed_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


class AICase(unittest.TestCase):
    def fake(self, *a, **kw):
        f = FakeOpenAI(*a, **kw)
        self.addCleanup(f.close)
        return f


class TestAI(AICase):
    def test_tool_loop_and_pin_refusal(self):
        r1 = {"role": "assistant", "content": None, "reasoning_content": "nghĩ riêng của hãng",
              "tool_calls": [tc("t1", "get_state", {}), tc("t2", "query_history", {"entity": "jk.soc", "hours": 24})]}
        r2 = {"role": "assistant", "content": None, "tool_calls": [tc("t3", "set_device", {"device": "tuya_eb6564a5a81a53bb8ayyzs", "to": "off"})]}
        api, hub = self.fake(r1, r2, say("CB tổng bếp cần PIN, bạn bấm trên màn hình nhé.")), FakeHub({"jk.soc": "85"})
        ai = voiced.AI(api.url + "/", "sk-test", "gpt-test")
        c = voiced.Conv()
        reply = voiced.ask_ai("pin hôm nay thế nào, rồi cắt điện bếp", CFG, hub, ai, voiced.Persona(), c)
        self.assertEqual(reply, "CB tổng bếp cần PIN, bạn bấm trên màn hình nhé.")
        self.assertEqual(c.take(), {"confirm": {"ids": ["tuya_eb6564a5a81a53bb8ayyzs"], "to": "off", "label": "tắt CB Tổng Bếp"}})
        chats = api.chats()
        self.assertEqual(len(chats), 3)
        self.assertTrue(all(c[2] == "Bearer sk-test" for c in chats))
        first = chats[0][3]
        self.assertEqual(first["model"], "gpt-test")
        self.assertEqual({t["function"]["name"] for t in first["tools"] if t["type"] == "function"},
                         {"get_state", "query_history", "set_device", "run_scene", "ack_alarm"})
        self.assertEqual(first["messages"][0]["role"], "system")
        self.assertIn("Đèn Kho", first["messages"][0]["content"])
        # Lượt 2: gửi lại lời gọi tool của trợ lý (bỏ trường riêng của hãng), mỗi tool một message role=tool.
        m = chats[1][3]["messages"]
        self.assertEqual([x["role"] for x in m], ["system", "user", "assistant", "tool", "tool"])
        self.assertNotIn("reasoning_content", m[2])
        self.assertEqual([t["id"] for t in m[2]["tool_calls"]], ["t1", "t2"])
        self.assertEqual([x["tool_call_id"] for x in m[3:]], ["t1", "t2"])
        self.assertIn("Pin lưu trữ (jk.soc): 85 %", m[3]["content"])
        self.assertIn("mới nhất 85", m[4]["content"])
        # Lượt 3: hubd chặn thiết bị có khoá → màn hình hiện hộp xác nhận; dặn model hỏi lại người, không gọi lại tool.
        denied = chats[2][3]["messages"][-1]
        self.assertEqual(denied["tool_call_id"], "t3")
        self.assertIn("xác nhận", denied["content"])
        self.assertIn("không gọi lại", denied["content"])
        self.assertIn(("POST", "/cmd", {"device": "tuya_eb6564a5a81a53bb8ayyzs", "to": "off"}), hub.calls)

    def test_bad_tool_call_reported_not_crashed(self):
        r1 = {"role": "assistant", "content": None, "tool_calls": [tc("t1", "set_device", "không phải json"), tc("t2", "xoa_nha", {})]}
        api = self.fake(r1, say("Xin lỗi, tôi chưa làm được."))
        reply = voiced.ask_ai("làm gì đó", CFG, FakeHub(), voiced.AI(api.url, "", "m"))
        self.assertEqual(reply, "Xin lỗi, tôi chưa làm được.")
        tools = api.chats()[1][3]["messages"][-2:]
        self.assertTrue(all(t["content"].startswith("LỖI") for t in tools), tools)

    def test_errors_become_spoken_replies(self):
        ask = lambda ai: voiced.answer("trời hôm nay thế nào", FakeHub(), ai)
        self.assertEqual(ask(voiced.AI(f"http://127.0.0.1:{closed_port()}/v1", "k", "m")), voiced.Persona().fmt(voiced.OFFLINE))
        self.assertEqual(ask(voiced.AI(self.fake(status=401).url, "sai", "m")), voiced.AI_KEY)
        self.assertEqual(ask(voiced.AI(self.fake(status=500).url, "k", "m")), voiced.AI_ERROR)
        refused = self.fake({"role": "assistant", "content": None, "refusal": "không"})
        self.assertIn("không làm được", ask(voiced.AI(refused.url, "k", "m")))

    def test_no_key_sends_no_authorization(self):
        api = self.fake(say("Chào"))  # Ollama, LM Studio trong LAN: không cần khoá
        self.assertEqual(voiced.answer("kể chuyện cười", FakeHub(), voiced.AI(api.url, "", "llama")), "Chào")
        self.assertIsNone(api.reqs[0][2])

    def test_fixed_commands_never_call_ai(self):
        api = self.fake()
        ai = voiced.AI(api.url, "k", "m")
        self.assertEqual(voiced.answer("BẬT ĐÈN KHO", FakeHub(), ai), "Đã bật Đèn Kho.")
        self.assertEqual(api.reqs, [])
        self.assertEqual(voiced.answer("kể chuyện cười", FakeHub(), None), voiced.Persona().fmt(voiced.FIXED_ONLY))
        wrong_token = lambda *a: (401, "cần token trợ lý")
        self.assertIn("token trợ lý", voiced.answer("bật đèn kho", wrong_token, ai))

    def test_base_url_forms(self):
        for u in ("https://x/v1", "https://x/v1/", " https://x/v1/chat/completions ", "https://x/v1/chat/completions/"):
            self.assertEqual(voiced.AI(u, "", "m").url, "https://x/v1", u)
        self.assertIsNone(voiced.AI.from_settings({"url": "https://x/v1", "api_key": "k"}))  # chưa chọn model
        self.assertIsNone(voiced.AI.from_settings({}))
        self.assertEqual(voiced.AI.from_settings({"url": "https://x/v1", "model": "m"}).key, "")


class TestCheck(AICase):
    """Nút "Thử kết nối" trên trang Cài đặt: trình duyệt → hubd /voice/ai/check → voiced gọi endpoint.
    Ô để trống dùng giá trị đã lưu; kết quả không bao giờ chứa khoá."""
    def check(self, stored, draft):
        r = voiced.check_ai(stored, draft)
        self.assertNotIn("sk-", json.dumps(r, ensure_ascii=False))
        return r

    def test_lists_models_with_stored_or_draft_key(self):
        api = self.fake(say("OK"))
        stored = {"url": api.url, "api_key": "sk-cu", "model": ""}
        r = self.check(stored, {"url": "", "api_key": "", "model": ""})
        self.assertEqual((r["ok"], r["models"]), (True, ["gpt-a", "gpt-b"]))
        self.assertEqual(api.reqs[-1][:3], ("GET", "/v1/models", "Bearer sk-cu"))
        r = self.check(stored, {"api_key": "sk-moi", "model": "gpt-a"})
        self.assertTrue(r["ok"], r)
        self.assertIn("gpt-a", r["message"])
        chat = api.chats()[-1]
        self.assertEqual((chat[2], chat[3]["model"]), ("Bearer sk-moi", "gpt-a"))
        self.assertTrue(chat[3]["tools"])  # thử đúng kiểu gọi thật: có tool

    def test_stored_key_stays_with_stored_url(self):
        # Người có PIN đổi URL sang máy khác mà không nhập lại khoá: khoá đã lưu không được gửi đi.
        home, other = self.fake(), self.fake()
        stored = {"url": home.url, "api_key": "sk-cu"}
        self.check(stored, {"url": other.url})
        self.check(stored, {"url": home.url + "/"})  # cùng địa chỉ, khác dấu / cuối: vẫn dùng khoá đã lưu
        self.assertEqual([r[2] for r in other.reqs], [None])
        self.assertEqual([r[2] for r in home.reqs], ["Bearer sk-cu"])

    def test_only_models_and_chat_paths_no_redirect(self):
        # Chỉ gọi được {url}/models và {url}/chat/completions: URL có query/fragment bị từ chối, không đi theo chuyển hướng
        # (tránh dùng nút Thử để gọi dịch vụ nội bộ như go2rtc, hubd bằng đường dẫn tuỳ ý).
        inner = self.fake()
        hop = self.fake(status=302, location=inner.url + "/models")
        for url in (inner.url + "?x=", inner.url + "#a"):
            r = self.check({}, {"url": url})
            self.assertFalse(r["ok"], url)
        r = self.check({}, {"url": hop.url, "model": "m"})
        self.assertFalse(r["ok"])
        self.assertEqual(inner.reqs, [])
        self.assertNotIn("lỗi giả", r["message"])  # thân lỗi trả về chỉ lấy error.message của API kiểu OpenAI...
        r = self.check({}, {"url": self.fake(status=404).url, "model": "gpt-x"})
        self.assertIn("lỗi giả", r["message"])  # ...như "model không tồn tại"

    def test_endpoint_without_model_list(self):
        api = self.fake(say("OK"), models=None)
        r = self.check({}, {"url": api.url, "model": "qwen2.5"})
        self.assertEqual((r["ok"], r["models"]), (True, []))
        self.assertIn("gõ tên model", r["message"])

    def test_failures(self):
        cases = {
            "key": ({"url": self.fake(status=401).url, "api_key": "sk-sai"}, "bị từ chối"),
            "down": ({"url": f"http://127.0.0.1:{closed_port()}/v1"}, "Không kết nối được"),
            "file": ({"url": "file:///etc/passwd"}, "http://"),
            "ftp": ({"url": "ftp://x/v1"}, "http://"),
            "empty": ({}, "Nhập địa chỉ"),
        }
        for name, (draft, want) in cases.items():
            r = self.check({}, draft)
            self.assertFalse(r["ok"], name)
            self.assertIn(want, r["message"], name)
        r = self.check({}, {"url": self.fake(status=500).url, "model": "x"})  # model không trả lời được
        self.assertFalse(r["ok"])
        self.assertIn("HTTP 500", r["message"])


OUT_CFG = {
    "zones": [{"id": "cafe", "name": "Khu cafe"}, {"id": "lamviec", "name": "Phòng làm việc"},
              {"id": "gieng_troi", "name": "Giếng trời", "type": "outdoor"}, {"id": "kythuat", "name": "Tum kỹ thuật"}],
    "devices": [], "scenes": [], "locked": [], "phrases": [],
    "entities": [
        {"id": "gt.t", "name": "Nhiệt độ", "unit": "°C", "zone": "gieng_troi"},
        {"id": "gt.h", "name": "Độ ẩm", "unit": "%", "zone": "gieng_troi"},
        {"id": "gt.mua", "name": "Mưa", "unit": "", "zone": "gieng_troi"},
        {"id": "cafe.co2", "name": "CO₂", "unit": "ppm", "zone": "cafe"},
        {"id": "lv.co2", "name": "CO₂", "unit": "ppm", "zone": "lamviec"},
        {"id": "ro.tds", "name": "Nước sau lọc", "unit": "ppm", "zone": "cafe"},  # cũng ppm nhưng không phải CO₂
        {"id": "cafe.pm", "name": "Bụi mịn PM2.5", "unit": "µg/m³", "zone": "cafe"},
        {"id": "lv.pm", "name": "Bụi mịn PM2.5", "unit": "µg/m³", "zone": "lamviec"},
        {"id": "jk.soc", "name": "Pin lưu trữ", "unit": "%", "zone": "kythuat"},
        {"id": "jk.power", "name": "Pin sạc/xả", "unit": "W", "zone": "kythuat"},
        {"id": "deye.pv", "name": "Điện mặt trời", "unit": "W", "zone": "kythuat"},
        {"id": "deye.load", "name": "Tải tiêu thụ", "unit": "W", "zone": "kythuat"},
        {"id": "deye.grid", "name": "Điện lưới", "unit": "W", "zone": "kythuat"},
    ],
}


class TestBrief(unittest.TestCase):
    """Nút đọc tóm tắt: thời tiết, không khí, điện/pin, cảnh báo đang mở; tính cục bộ, không cần AI hay internet."""
    def test_full(self):
        st = {"values": {"gt.t": "31.5", "gt.h": "70", "gt.mua": "1", "cafe.co2": "1350", "lv.co2": "700", "ro.tds": "1500",
                         "cafe.pm": "12", "lv.pm": "18.4", "jk.soc": "85", "jk.power": "1200", "deye.pv": "2300",
                         "deye.load": "800", "deye.grid": "-300", "alert:pin_yeu": "Pin lưu trữ dưới 20%", "alert:cu": ""},
              "alerts": [{"level": "warn", "text": "Pin lưu trữ dưới 20%"},
                         {"level": "warn", "text": "CO2 ở khu cafe đang cao, nên mở cửa cho thoáng."}]}
        self.assertEqual(voiced.brief(OUT_CFG, st),
                         "Ngoài trời 31,5 độ, độ ẩm 70 phần trăm, đang mưa. "
                         "Trong nhà CO2 cao nhất 1350 pi pi em ở Khu cafe, bụi mịn cao nhất 18,4 micrô gam ở Phòng làm việc. "
                         "Pin 85 phần trăm, đang sạc 1,2 ki lô oát; điện mặt trời 2,3 ki lô oát; nhà dùng 800 oát; đang bán lưới 300 oát. "
                         "Có 2 cảnh báo: Pin lưu trữ dưới 20%; CO2 ở khu cafe đang cao, nên mở cửa cho thoáng.")

    def test_partial_and_empty(self):
        self.assertEqual(voiced.brief(OUT_CFG, {"values": {}, "alerts": []}),
                         "Chưa có số đo thời tiết, không khí hay điện. Không có cảnh báo nào.")
        # ESPHome gửi "nan" khi mất cảm biến: coi như chưa có; trời tạnh; pin đang xả; lưới gần 0 thì thôi nói.
        st = {"values": {"gt.t": "nan", "gt.mua": "0", "jk.soc": "40", "jk.power": "-450", "deye.grid": "3"}, "alerts": []}
        self.assertEqual(voiced.brief(OUT_CFG, st), "Ngoài trời không mưa. Pin 40 phần trăm, đang xả 450 oát. Không có cảnh báo nào.")


def wav_bytes(rate=48000, channels=1, seconds=0.5):
    b = io.BytesIO()
    with wave.open(b, "wb") as w:
        w.setnchannels(channels), w.setsampwidth(2), w.setframerate(rate)
        w.writeframes((np.sin(np.arange(int(rate * seconds) * channels) / 10) * 8000).astype("<i2").tobytes())
    return b.getvalue()


class TestConversation(AICase):
    def test_yes_no_sweep(self):
        # Chỉ câu ngắn mới là trả lời có/không; có cả "có" lẫn "không" ("có ai không") là câu hỏi, không phải đồng ý.
        for t in ("có", "Có.", "đồng ý", "ừ làm đi", "ok", "xác nhận", "đúng rồi", "chắc chắn rồi", "được em"):
            self.assertIs(voiced.yes_no(t), True, t)
        for t in ("không", "thôi", "huỷ đi", "hủy", "đừng", "khỏi"):
            self.assertIs(voiced.yes_no(t), False, t)
        for t in ("", "có ai ở nhà không", "bật đèn kho", "có thể cho tôi biết pin còn bao nhiêu phần trăm"):
            self.assertIsNone(voiced.yes_no(t), t)

    def test_confirm_yes_no_other_and_expiry(self):
        hub, t = FakeHub(), [0.0]
        c = voiced.Conv(now=lambda: t[0])
        ask = lambda text: voiced.answer(text, hub, None, voiced.Persona(), c)
        ask("tắt CB tổng bếp")
        self.assertIn("confirm", c.take())
        n = len(hub.calls)
        self.assertEqual(ask("đồng ý"), "Đã xác nhận, tắt CB Tổng Bếp.")
        self.assertEqual(c.take(), {"execute": {"ids": ["tuya_eb6564a5a81a53bb8ayyzs"], "to": "off", "label": "tắt CB Tổng Bếp"}})
        self.assertEqual([x for x in hub.calls[n:] if x[1] == "/cmd"], [])   # màn hình (có PIN) làm, voiced không gửi
        self.assertEqual(ask("đồng ý"), voiced.Persona().fmt(voiced.FIXED_ONLY))   # không còn gì chờ: câu thường
        ask("tắt CB tổng bếp"), c.take()
        self.assertEqual(ask("thôi"), "Đã huỷ.")
        self.assertEqual(c.take(), {})
        ask("tắt CB tổng bếp"), c.take()
        self.assertEqual(ask("bật đèn kho"), "Đã bật Đèn Kho.")   # nói sang chuyện khác: bỏ lệnh đang chờ
        self.assertEqual((ask("có"), c.take()), (voiced.Persona().fmt(voiced.FIXED_ONLY), {}))
        ask("tắt CB tổng bếp"), c.take()
        t[0] += voiced.Conv.WAIT + 1   # quá hạn xác nhận
        self.assertEqual((ask("có"), c.take()), (voiced.Persona().fmt(voiced.FIXED_ONLY), {}))

    def test_history_sent_to_ai_per_conversation(self):
        api = self.fake(say("Đèn kho đang bật."), say("Đã ghi nhận."), say("Chào."))
        ai, hub = voiced.AI(api.url + "/", "", "m"), FakeHub()
        a, b = voiced.Conv(), voiced.Conv()
        voiced.answer("bật đèn kho", hub, ai, voiced.Persona(), a)   # lệnh cố định cũng vào lịch sử
        voiced.answer("vừa rồi em làm gì", hub, ai, voiced.Persona(), a)
        voiced.answer("còn nó thì sao", hub, ai, voiced.Persona(), a)
        voiced.answer("còn nó thì sao", hub, ai, voiced.Persona(), b)
        m = [c[3]["messages"] for c in api.chats()]
        self.assertEqual([x["content"] for x in m[0][1:]], ["bật đèn kho", "Đã bật Đèn Kho.", "vừa rồi em làm gì"])
        self.assertEqual([x["role"] for x in m[1][1:]], ["user", "assistant", "user", "assistant", "user"])
        self.assertEqual(m[1][-2]["content"], "Đèn kho đang bật.")
        self.assertEqual([x["role"] for x in m[2]], ["system", "user"])   # màn hình khác: không thấy lịch sử của màn hình kia
        for i in range(10):
            a.add(f"câu {i}", "ừ")
        self.assertEqual(len(a.history()), 2 * voiced.Conv.TURNS)

    def test_app_conversation_per_screen_and_idle_reset(self):
        app, t = voiced.App(FakeHub(), None, None, None), [0.0]
        app.clock = lambda: t[0]
        x = app.conv("man-hinh-1")
        self.assertIs(app.conv("man-hinh-1"), x)
        self.assertIsNot(app.conv("man-hinh-2"), x)
        x.add("a", "b")
        t[0] += voiced.Conv.IDLE + 1
        self.assertEqual(app.conv("man-hinh-1").history(), [])


class TestHotwords(unittest.TestCase):
    def test_names_from_config(self):
        cfg = {"zones": [{"id": "kho", "name": "Phòng kho"}, {"id": "sau", "name": "Sân  sau"}, {"id": "t1", "name": "Tủ điện tầng 1"}],
               "devices": [{"id": "a_1", "name": "Nút 1", "group": "Đèn phòng lạnh"}, {"id": "a_2", "name": "Nút 2", "group": "Đèn phòng lạnh"},
                           {"id": "k", "name": "Đèn Kho"}, {"id": "k2", "name": "đèn kho"}, {"id": "c", "name": "Camera 01"},
                           {"id": "cb", "name": "CB Tổng Bảng Hiệu"}],
               "scenes": [{"id": "x", "name": "Ra khỏi nhà"}]}
        h = voiced.hotwords(cfg).split("/")
        for w in ("PHÒNG KHO", "SÂN SAU", "ĐÈN PHÒNG LẠNH", "ĐÈN KHO", "CB TỔNG BẢNG HIỆU", "RA KHỎI NHÀ", "BẬT", "TẮT", "BẬT ĐÈN", "TẮT ĐÈN"):
            self.assertIn(w, h)
        # tên có chữ số (Nút 1, Camera 01, Tủ điện tầng 1) bỏ: mô hình viết số bằng chữ; viết hoa như từ vựng của mô hình, không trùng
        self.assertEqual([w for w in h if re.search(r"\d", w) or w != w.upper()], [])
        self.assertEqual(len(h), len(set(h)))
        self.assertEqual(voiced.hotwords({"zones": [], "devices": []}).split("/"), voiced.COMMAND_WORDS)


class TestServer(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.heard, self.hot = [], []
        def stt(rate, samples, hot=""):
            self.heard.append((rate, samples.shape))
            self.hot.append(hot)
            return "BẬT ĐÈN KHO"
        self.spoken = []
        def synth(text, path, voice):
            self.spoken.append((voice, text))
            path.write_bytes(wav_bytes(seconds=0.1))
        tts = voiced.TTS(Path(self.tmp.name), synth)
        self.app = voiced.App(FakeHub(), None, stt, tts)
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), voiced.handler(self.app))
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.base = f"http://127.0.0.1:{self.srv.server_port}"

    def tearDown(self):
        self.srv.shutdown(), self.srv.server_close(), self.tmp.cleanup()

    def post(self, body, ctype, path="/voice/ask"):
        req = urllib.request.Request(self.base + path, data=body, headers={"Content-Type": ctype})
        return json.load(urllib.request.urlopen(req))

    def fake(self, *a, **kw):
        f = FakeOpenAI(*a, **kw)
        self.addCleanup(f.close)
        return f

    def test_ai_settings_load_and_reload_on_mqtt(self):
        api = self.fake(say("Mèo đi học về."))
        self.app.hub.settings = {"url": api.url, "api_key": "sk-bi-mat-1", "model": "m1"}
        log = io.StringIO()
        with contextlib.redirect_stdout(log):
            self.app.load_ai()
        self.assertNotIn("sk-bi-mat-1", log.getvalue())  # không in khoá ra log
        self.assertEqual((self.app.ai.model, self.app.ai.key), ("m1", "sk-bi-mat-1"))
        r = self.post(json.dumps({"text": "kể chuyện cười"}).encode(), "application/json")
        self.assertEqual(r["reply"], "Mèo đi học về.")
        # Lưu trên trang Cài đặt → hubd publish hubd/settings/ai = changed → voiced đọc lại qua API trợ lý.
        self.app.hub.settings = {"url": api.url, "model": "m2"}
        self.app.on_mqtt("hubd/settings/ai", "changed")
        self.assertEqual((self.app.ai.model, self.app.ai.key), ("m2", ""))
        self.app.hub.settings = {}
        self.app.on_mqtt("hubd/settings/ai", "changed")
        self.assertIsNone(self.app.ai)
        self.assertEqual(self.post(json.dumps({"text": "kể chuyện cười"}).encode(), "application/json")["reply"], voiced.Persona().fmt(voiced.FIXED_ONLY))

    def test_ai_check_endpoint(self):
        api = self.fake()
        self.app.hub.settings = {"url": api.url, "api_key": "sk-da-luu", "model": ""}
        r = self.post(json.dumps({"url": "", "api_key": "", "model": ""}).encode(), "application/json", "/voice/ai/check")
        self.assertEqual((r["ok"], r["models"]), (True, ["gpt-a", "gpt-b"]))
        self.assertEqual(api.reqs[-1][2], "Bearer sk-da-luu")

    def test_brief_endpoint(self):
        r = self.post(b"", "application/json", "/voice/brief")
        self.assertEqual(r["reply"], voiced.brief(CFG, {"values": {}, "alerts": []}))
        self.assertRegex(r["audio"][0], r"^/voice/tts/[0-9a-f]{16}\.wav$")

    def test_ask_with_wav_and_text(self):
        r = self.post(wav_bytes(channels=2), "audio/wav")
        self.assertEqual((r["heard"], r["reply"]), ("bật đèn kho", "Đã bật Đèn Kho."))
        self.assertEqual(self.heard, [(48000, (24000,))])  # stereo → mono, giữ tần số gốc cho sherpa tự đổi
        self.assertRegex(r["audio"][0], r"^/voice/tts/[0-9a-f]{16}\.wav$")
        self.assertEqual(urllib.request.urlopen(self.base + r["audio"][0]).read()[:4], b"RIFF")
        r = self.post(json.dumps({"text": "ra khỏi nhà"}).encode(), "application/json")
        self.assertEqual(r["reply"], "Đã chạy Ra khỏi nhà.")

    def test_confirm_round_trip_per_screen(self):
        talk = lambda text, sid: json.load(urllib.request.urlopen(urllib.request.Request(self.base + "/voice/ask",
            data=json.dumps({"text": text}).encode(), headers={"Content-Type": "application/json", "X-Voice-Session": sid})))
        r = talk("tắt CB tổng bếp", "ipad")
        self.assertEqual(r["confirm"]["ids"], ["tuya_eb6564a5a81a53bb8ayyzs"])
        self.assertNotIn("execute", talk("có", "dien-thoai"))   # màn hình khác nói "có": không làm lệnh của iPad
        r = talk("có", "ipad")
        self.assertEqual(r["execute"], {"ids": ["tuya_eb6564a5a81a53bb8ayyzs"], "to": "off", "label": "tắt CB Tổng Bếp"})
        self.assertNotIn("confirm", r)

    def test_stt_gets_hotwords_from_hub_config(self):
        # Từ khoá ưu tiên lấy từ cấu hình hub ở mỗi câu (giữ 60 giây), để mô hình nghe đúng tên trong nhà.
        self.post(wav_bytes(), "audio/wav")
        self.assertIn("ĐÈN KHO", self.hot[0].split("/"))
        self.assertIn("BẬT", self.hot[0].split("/"))
        n = sum(1 for c in self.app.hub.calls if c[1] == "/config")
        self.post(wav_bytes(), "audio/wav")
        self.assertEqual(self.hot[1], self.hot[0])
        self.assertEqual(sum(1 for c in self.app.hub.calls if c[1] == "/config"), n + 1)  # câu sau chỉ gọi /config cho answer()

    def test_too_short_audio_is_not_sent_to_stt(self):
        # Chạm nút rồi gửi ngay: WAV rỗng/ngắn làm sherpa-onnx lỗi ("Invalid input shape {0,80}") → phải trả "chưa nghe rõ".
        for seconds in (0, 0.1):
            r = self.post(wav_bytes(seconds=seconds), "audio/wav")
            self.assertEqual((r["heard"], r["reply"]), ("", voiced.Persona().fmt(voiced.NOT_HEARD)))
        self.assertEqual(self.heard, [])

    def test_bad_input_and_path(self):
        for body, ctype in ((b"khong phai wav", "audio/wav"), (b"{}", "application/json")):
            with self.assertRaises(urllib.error.HTTPError) as e:
                self.post(body, ctype)
            self.assertEqual(e.exception.code, 400)
        with self.assertRaises(urllib.error.HTTPError) as e:
            urllib.request.urlopen(self.base + "/voice/tts/../../etc/passwd")
        self.assertEqual(e.exception.code, 404)

    def test_alert_from_mqtt_reaches_sse(self):
        resp = urllib.request.urlopen(self.base + "/voice/events")
        self.assertEqual(resp.readline(), b": ok\n")
        resp.readline()
        self.app.on_mqtt("hubd/alert", '{"level":"urgent","text":"Có khói ở bếp"}')
        self.app.on_mqtt("hubd/alert/ack", "{}")
        ev = [json.loads(resp.readline()[6:]), resp.readline(), json.loads(resp.readline()[6:])]
        self.assertEqual((ev[0]["type"], ev[0]["level"], ev[0]["text"]), ("alert", "urgent", "Có khói ở bếp"))
        self.assertRegex(ev[0]["audio"][0], r"^/voice/tts/[0-9a-f]{16}\.wav$")
        self.assertEqual(ev[2], {"type": "ack"})

    def test_bad_mqtt_payload_does_not_raise(self):
        # paho 1.6.1 ném lại lỗi của on_message và làm chết luồng MQTT: một tin hỏng không được làm câm cảnh báo về sau.
        for payload in ("không phải json", '{"level":"warn"}', "[1]", b"\xff\xfe"):
            self.app.on_mqtt("hubd/alert", payload)

    def test_bad_wav_header_and_length(self):
        for rate, seconds in ((1, 1), (8_000_000, 0.35)):  # tần số vô lý, đủ dài để qua ngưỡng 0,3 s
            with self.assertRaises(urllib.error.HTTPError) as e:
                self.post(wav_bytes(rate=rate, seconds=seconds), "audio/wav")
            self.assertEqual(e.exception.code, 400)
        self.assertEqual(self.heard, [])
        import http.client
        c = http.client.HTTPConnection("127.0.0.1", self.srv.server_port, timeout=5)
        c.putrequest("POST", "/voice/ask"), c.putheader("Content-Length", "-1"), c.endheaders()
        self.assertEqual(c.getresponse().status, 400)

    def test_tts_cache_keeps_newest(self):
        tts = self.app.tts
        tts.keep = 3
        for i in range(5):
            tts.url(f"câu {i}")
        tts.url("câu 2")  # dùng lại: thành mới nhất
        tts.url("câu 5")
        left = {p.name for p in Path(self.tmp.name).glob("*.wav")}
        self.assertEqual(left, {f"{tts.key(t, 'Hải Đăng')}.wav" for t in ("câu 4", "câu 2", "câu 5")})

    def test_voice_settings_reload_and_rerender(self):
        # Cài đặt → Trợ lý: đổi giọng / xưng hô → hubd publish hubd/settings/voice → voiced áp dụng ngay,
        # câu cảnh báo cố định tạo lại theo giọng mới; cache tách theo giọng (cùng câu, hai giọng = hai tệp).
        self.app.warm()
        self.assertIn(("Hải Đăng", "Có khói ở bếp"), self.spoken)
        self.app.hub.voice = {"voice": "Trúc Ly", "owner": "anh Bình", "name": "Mây"}
        self.app.on_mqtt("hubd/settings/voice", "changed")
        self.app.warming.join(10)
        self.assertEqual((self.app.persona.voice, self.app.persona.owner), ("Trúc Ly", "anh Bình"))
        self.assertIn(("Trúc Ly", "Có khói ở bếp"), self.spoken)
        self.assertIn(("Trúc Ly", "Em chưa nghe rõ, anh Bình nói lại nhé."), self.spoken)
        tts = self.app.tts
        self.assertNotEqual(tts.key("Có khói ở bếp", "Hải Đăng"), tts.key("Có khói ở bếp", "Trúc Ly"))
        self.assertTrue((Path(self.tmp.name) / f"{tts.key('Có khói ở bếp', 'Hải Đăng')}.wav").exists())
        self.assertTrue((Path(self.tmp.name) / f"{tts.key('Có khói ở bếp', 'Trúc Ly')}.wav").exists())
        r = self.post(json.dumps({"text": "bật đèn kho"}).encode(), "application/json")
        self.assertEqual(r["reply"], "Dạ anh Bình, đã bật Đèn Kho.")
        self.assertEqual(self.spoken[-1], ("Trúc Ly", "Dạ anh Bình, đã bật Đèn Kho."))
        # Giọng lạ (sửa tay, giọng bị bỏ) → giọng mặc định, không làm hỏng TTS.
        self.app.hub.voice = {"voice": "Không có"}
        self.app.on_mqtt("hubd/settings/voice", "changed")
        self.app.warming.join(10)
        self.assertEqual(self.app.persona.voice, "Hải Đăng")

    def test_quiet_hours_mute_alert_audio_not_text(self):
        resp = urllib.request.urlopen(self.base + "/voice/events")
        resp.readline(), resp.readline()
        self.app.hub.voice = {"quiet_from": "22:00", "quiet_to": "06:00"}
        self.app.load_voice()
        self.app.now = lambda: voiced.datetime(2026, 10, 27, 23, 30, tzinfo=voiced.VN)
        self.app.on_mqtt("hubd/alert", '{"level":"warn","text":"Trời bắt đầu mưa"}')
        self.app.on_mqtt("hubd/alert", '{"level":"urgent","text":"Có khói ở bếp"}')
        self.app.now = lambda: voiced.datetime(2026, 10, 27, 7, 0, tzinfo=voiced.VN)
        self.app.on_mqtt("hubd/alert", '{"level":"warn","text":"CO2 cao"}')
        got = []
        while len(got) < 3:
            line = resp.readline()
            if line.startswith(b"data: "):
                got.append(json.loads(line[6:]))
        self.assertEqual([(e["text"], e["audio"] is not None) for e in got],
                         [("Trời bắt đầu mưa", False), ("Có khói ở bếp", True), ("CO2 cao", True)])
        self.assertNotIn(("Hải Đăng", "Trời bắt đầu mưa"), self.spoken)  # không tổng hợp, đỡ tốn CPU ban đêm

    def test_long_reply_plays_first_sentence_while_rest_synthesizes(self):
        # Đọc nhanh hơn: câu trả lời dài trả về ngay khi xong câu đầu; câu sau tổng hợp nền, hoặc ngay khi trình phát xin tới.
        self.assertEqual(voiced.sentences("Chào anh Bình. Ngoài trời 31 độ, đang mưa. Pin 85 phần trăm; nhà dùng 800 oát. Không có cảnh báo nào."),
                         ["Chào anh Bình. Ngoài trời 31 độ, đang mưa.", "Pin 85 phần trăm; nhà dùng 800 oát.", "Không có cảnh báo nào."])
        self.assertEqual(voiced.sentences("Đã bật Đèn Kho."), ["Đã bật Đèn Kho."])
        gate = threading.Event()
        synth = self.app.tts.synth
        def slow(text, path, voice):  # câu sau chờ tới khi được thả, để thấy phản hồi không đợi chúng
            if not text.startswith("Ngoài"):
                gate.wait(5)
            synth(text, path, voice)
        self.app.tts.synth = slow
        self.app.hub.values = {"gt.t": "31", "gt.mua": "1", "jk.soc": "85", "deye.load": "800"}
        self.app.hub.cfg = OUT_CFG
        t0 = time.perf_counter()
        r = self.post(b"", "application/json", "/voice/brief")
        self.assertLess(time.perf_counter() - t0, 3)
        self.assertGreater(len(r["audio"]), 1)
        first = Path(self.tmp.name) / r["audio"][0].rsplit("/", 1)[1]
        self.assertTrue(first.exists())
        # Câu đầu đã có: tải ngay dù luồng nền đang giữ máy TTS cho câu sau (đo trên hub: từng phải chờ 4,7 s).
        t0 = time.perf_counter()
        self.assertEqual(urllib.request.urlopen(self.base + r["audio"][0], timeout=3).read()[:4], b"RIFF")
        self.assertLess(time.perf_counter() - t0, 1)
        gate.set()
        for u in r["audio"]:
            self.assertEqual(urllib.request.urlopen(self.base + u).read()[:4], b"RIFF")
        self.assertEqual([t for v, t in self.spoken][-len(r["audio"]):], voiced.sentences(r["reply"]))
        with self.assertRaises(urllib.error.HTTPError):  # khoá lạ (chưa từng đăng ký) thì 404, không tổng hợp gì
            urllib.request.urlopen(self.base + "/voice/tts/0123456789abcdef.wav")

    def test_listen_mode_wake_name(self):
        # Nghe gọi tên (iPad để micro mở, gửi từng đoạn có tiếng): chỉ câu mở đầu bằng tên trợ lý mới được xử lý.
        self.app.hub.voice = {"name": "Mây", "owner": "anh Bình"}
        self.app.load_voice()
        heard = {"v": ""}
        self.app.stt = lambda rate, a, hot="": heard["v"]
        listen = lambda text, q="?listen=1": (heard.__setitem__("v", text), self.post(wav_bytes(), "audio/wav", "/voice/ask" + q))[1]
        log = io.StringIO()
        with contextlib.redirect_stdout(log):
            r = listen("KHÁCH BÀN BA GỌI MỘT LY CÀ PHÊ SỮA")
        self.assertEqual(r, {"ignored": True})
        self.assertNotIn("CÀ PHÊ", log.getvalue().upper())  # không ghi lại câu nói không gọi trợ lý
        n = len(self.spoken)
        self.assertEqual(listen("MÁY LẠNH CÒN BẬT KHÔNG"), {"ignored": True})  # "máy" không phải "Mây"
        self.assertEqual(len(self.spoken), n)
        r = listen("MÂY ƠI BẬT ĐÈN KHO")
        self.assertEqual((r["heard"], r["reply"]), ("bật đèn kho", "Dạ anh Bình, đã bật Đèn Kho."))
        self.assertIn(("POST", "/cmd", {"device": "tuya_eba30c56ca208baad0y43a", "to": "on"}), self.app.hub.calls)
        r = listen("MÂY ƠI")
        self.assertEqual((r["reply"], r["awaiting"]), ("Dạ, em là Mây đây, anh Bình cần gì ạ?", True))
        r = listen("TẮT ĐÈN KHO", "")  # câu ngay sau khi trợ lý hỏi lại: gửi như nút nói, không cần gọi tên
        self.assertEqual(r["reply"], "Dạ anh Bình, đã tắt Đèn Kho.")

    def test_preview_endpoint(self):
        # Nút Nghe thử: dùng giá trị đang nhập (chưa lưu), không đổi cấu hình đang chạy.
        r = self.post(json.dumps({"voice": "Trúc Ly", "owner": "anh Bình", "name": "Mây"}).encode(), "application/json", "/voice/preview")
        self.assertEqual(r["reply"], "Dạ, em là Mây đây, anh Bình cần gì ạ?")
        self.assertEqual(self.spoken[-1], ("Trúc Ly", r["reply"]))
        self.assertRegex(r["audio"][0], r"^/voice/tts/[0-9a-f]{16}\.wav$")
        self.assertEqual(self.app.persona.voice, "Hải Đăng")
        r = self.post(b"{}", "application/json", "/voice/preview")
        self.assertEqual(r["reply"], "Tôi đây, bạn cần gì?")

    def test_warm_prerenders_alert_phrases(self):
        self.app.warm()
        self.assertTrue((Path(self.tmp.name) / f"{self.app.tts.key('Có khói ở bếp', 'Hải Đăng')}.wav").exists())


class TestWake(unittest.TestCase):
    def test_wake_name(self):
        # Tên có dấu khớp đúng, hoặc không dấu nhưng ngay sau là "ơi" (STT hay sai dấu); cho phép một tiếng đệm phía trước.
        mây, plain = voiced.Persona({"name": "Mây"}), voiced.Persona()
        cases = [(mây, "MÂY ƠI BẬT ĐÈN KHO", "BẬT ĐÈN KHO"), (mây, "mây ơi", ""), (mây, "Mây, tắt đèn", "tắt đèn"),
                 (mây, "MÁY ƠI TẮT ĐÈN", "TẮT ĐÈN"), (mây, "à mây ơi tắt đèn", "tắt đèn"),
                 (mây, "máy lạnh bật chưa", None), (mây, "bật đèn kho", None), (mây, "bật đèn kho mây ơi", None), (mây, "", None),
                 (plain, "TRỢ LÝ ƠI BẬT ĐÈN", "BẬT ĐÈN"), (plain, "trợ lý", ""), (plain, "bật đèn kho", None)]
        for p, text, want in cases:
            self.assertEqual(voiced.wake(text, p), want, (p.name, text))


class TestPersona(unittest.TestCase):
    """Xưng hô (nhóm cấu hình "voice"): owner = cách gọi chủ nhà ("anh Bình"); có owner thì trợ lý xưng "em".
    Trống thì nói như trước (tôi / bạn)."""
    def test_defaults_unchanged(self):
        p = voiced.Persona()
        self.assertEqual((p.voice, p.me, p.you), ("Hải Đăng", "tôi", "bạn"))
        self.assertEqual(p.fmt(voiced.NOT_HEARD), "Tôi chưa nghe rõ, bạn nói lại nhé.")
        self.assertEqual(p.say("Đã bật Đèn Kho."), "Đã bật Đèn Kho.")
        self.assertNotIn("Gọi người dùng", voiced.system_prompt(CFG, p))

    def test_owner_in_fixed_replies_brief_and_prompt(self):
        p = voiced.Persona({"owner": "anh Bình", "name": "Mây", "voice": "Trúc Ly"})
        self.assertEqual(p.fmt(voiced.NOT_HEARD), "Em chưa nghe rõ, anh Bình nói lại nhé.")
        self.assertEqual(p.fmt(voiced.FIXED_ONLY)[:30], "Chưa cài AI nên em chỉ làm đượ")
        self.assertEqual(p.say("Đã bật Đèn Kho."), "Dạ anh Bình, đã bật Đèn Kho.")
        self.assertEqual(p.say("CB tổng bếp cần PIN."), "Dạ anh Bình, CB tổng bếp cần PIN.")  # chữ viết tắt giữ nguyên hoa
        hub = FakeHub({"jk.soc": "85"})
        self.assertEqual(voiced.run(voiced.match("bật đèn kho", CFG), hub, p), "Dạ anh Bình, đã bật Đèn Kho.")
        self.assertEqual(voiced.run(voiced.match("tắt CB tổng bếp", CFG), hub, p), "Dạ anh Bình, CB Tổng Bếp được bảo vệ, anh Bình xác nhận tắt không?")
        self.assertTrue(voiced.brief(OUT_CFG, {"values": {}, "alerts": []}, p).startswith("Chào anh Bình. Chưa có số đo"))
        sp = voiced.system_prompt(CFG, p)
        self.assertIn("Bạn là Mây", sp)
        self.assertIn('gọi họ là "anh Bình", tự xưng "em"', sp)

    def test_greeting(self):
        cases = {(): ("chào", "Tôi đây, bạn cần gì?"),
                 ("anh Bình", ""): ("xin chào", "Dạ, em đây, anh Bình cần gì ạ?"),
                 ("anh Bình", "Mây"): ("MÂY ƠI", "Dạ, em là Mây đây, anh Bình cần gì ạ?"),
                 ("", "Mây"): ("alo", "Tôi là Mây đây, bạn cần gì?")}
        for k, (text, want) in cases.items():
            p = voiced.Persona(dict(zip(("owner", "name"), k)))
            self.assertEqual(voiced.answer(text, FakeHub(), None, p), want, k)
        # Gọi tên rồi ra lệnh: vẫn là lệnh, không phải lời chào.
        self.assertEqual(voiced.answer("Mây ơi bật đèn kho", FakeHub(), None, voiced.Persona({"name": "Mây"})), "Đã bật Đèn Kho.")

    def test_quiet_window_sweep(self):
        # Quét đủ 1440 phút trong ngày × các dạng khung giờ (qua nửa đêm, trong ngày, trống, bằng nhau, sai định dạng),
        # so với cách đếm độc lập: phút m yên lặng khi đi từ giờ bắt đầu tới m (theo vòng 24 giờ) chưa chạm giờ kết thúc.
        windows = [("22:00", "06:00"), ("13:00", "14:30"), ("", ""), ("22:00", ""), ("07:00", "07:00"), ("25:00", "06:00"), ("0:05", "23:59")]
        n = 0
        for a, b in windows:
            p = voiced.Persona({"quiet_from": a, "quiet_to": b})
            ok = all(re.fullmatch(r"([01]?\d|2[0-3]):[0-5]\d", x) for x in (a, b)) and a != b
            mins = lambda x: int(x.split(":")[0]) * 60 + int(x.split(":")[1])
            quiet = set()
            if ok:  # đi từng phút từ giờ bắt đầu tới giờ kết thúc (không tính phút kết thúc)
                m = mins(a)
                while m != mins(b):
                    quiet.add(m)
                    m = (m + 1) % 1440
            for m in range(1440):
                n += 1
                want = m in quiet
                now = voiced.datetime(2026, 10, 27, m // 60, m % 60, tzinfo=voiced.VN)
                self.assertEqual(p.quiet(now), want, (a, b, m))
        self.assertEqual(n, len(windows) * 1440)


# Thiết bị nhiều nút sau khi quét QR Tuya: tuya.yaml tách mỗi nút/lỗ thành một thiết bị tên "Nút 1", "Lỗ 2"... cùng `group`,
# hubd bỏ thiết bị gốc (b3e097f). Dữ liệu giống trên hub: đèn 2 nút, ổ 3 lỗ có một lỗ protected, nhóm một kênh.
GROUP_CFG = {
    "zones": [{"id": "cafe", "name": "Khu cafe"}, {"id": "sansau", "name": "Sân sau"}],
    "devices": [
        {"id": "tuya_lanh_1", "name": "Nút 1", "group": "Đèn phòng lạnh", "zone": "cafe"},
        {"id": "tuya_lanh_2", "name": "Nút 2", "group": "Đèn phòng lạnh", "zone": "cafe"},
        {"id": "tuya_ca_1", "name": "Lỗ 1", "group": "Ổ cắm Hồ Cá", "zone": "sansau"},
        {"id": "tuya_ca_2", "name": "Lỗ 2", "group": "Ổ cắm Hồ Cá", "zone": "sansau"},
        {"id": "tuya_ca_3", "name": "Lỗ 3", "group": "Ổ cắm Hồ Cá", "zone": "sansau", "protected": True},
        {"id": "tuya_san_1", "name": "Kênh 1", "group": "Đèn sân", "zone": "sansau"},
        {"id": "tuya_kho", "name": "Đèn Kho", "zone": "cafe"},
    ],
    "scenes": [], "entities": [], "locked": ["tuya_ca_3"], "phrases": [],
}


class TestGroups(AICase):
    """Kênh gọi bằng "<nhóm> <kênh>", nói tên nhóm thì áp dụng mọi kênh (lệnh cố định lẫn tool AI); kênh cần PIN vẫn bị hubd chặn."""
    def test_fixed_commands(self):
        hub = FakeHub({"dev:tuya_lanh_1": "ON", "dev:tuya_lanh_2": "OFF"}, cfg=GROUP_CFG)
        say = lambda t: voiced.run(voiced.match(t, GROUP_CFG), hub)
        cmds = lambda: [(c[2]["device"], c[2]["to"]) for c in hub.calls if c[1] == "/cmd"]
        self.assertEqual(say("bật đèn phòng lạnh"), "Đã bật Đèn phòng lạnh.")
        self.assertEqual(cmds(), [("tuya_lanh_1", "on"), ("tuya_lanh_2", "on")])
        hub.calls.clear()
        self.assertEqual(say("tắt đèn phòng lạnh nút 2"), "Đã tắt Đèn phòng lạnh Nút 2.")
        self.assertEqual(cmds(), [("tuya_lanh_2", "off")])
        hub.calls.clear()
        self.assertEqual(say("TẮT Ổ CẮM HỒ CÁ"), "Đã tắt Lỗ 1, Lỗ 2 của Ổ cắm Hồ Cá. Lỗ 3 của Ổ cắm Hồ Cá được bảo vệ, bạn xác nhận tắt không?")
        self.assertEqual(cmds(), [("tuya_ca_1", "off"), ("tuya_ca_2", "off"), ("tuya_ca_3", "off")])  # lỗ 3: hubd trả 403
        self.assertEqual(say("tắt ổ cắm hồ cá lỗ 3"), "Ổ cắm Hồ Cá Lỗ 3 được bảo vệ, bạn xác nhận tắt không?")
        # nhóm có một phần được bảo vệ: kênh thường làm ngay, hộp xác nhận chỉ hỏi kênh có khoá
        c = voiced.Conv()
        voiced.run(voiced.match("tắt ổ cắm hồ cá", GROUP_CFG), FakeHub(cfg=GROUP_CFG), voiced.Persona(), c)
        self.assertEqual(c.take(), {"confirm": {"ids": ["tuya_ca_3"], "to": "off", "label": "tắt Lỗ 3 của Ổ cắm Hồ Cá"}})
        self.assertEqual(say("bật đèn sân"), "Đã bật Đèn sân.")  # nhóm một kênh: gọi bằng tên nhóm
        self.assertEqual(say("đèn phòng lạnh đang bật không"), "Đèn phòng lạnh: Nút 1 bật, Nút 2 tắt.")
        self.assertEqual(say("đèn phòng lạnh nút 1 bật chưa"), "Đèn phòng lạnh Nút 1 đang bật.")
        self.assertIsNone(voiced.match("bật nút 1", GROUP_CFG))  # "Nút 1" trần không còn là tên gọi: không đoán

    def test_every_name_sweep(self):
        # Quét mọi tên gọi do grouped() sinh (kênh, nhóm, nhóm một kênh, thiết bị thường) × 3 động từ × 3 dạng chữ.
        targets = voiced.grouped(GROUP_CFG)["devices"]
        self.assertEqual(sorted(d["name"] for d in targets), sorted([
            "Đèn phòng lạnh Nút 1", "Đèn phòng lạnh Nút 2", "Ổ cắm Hồ Cá Lỗ 1", "Ổ cắm Hồ Cá Lỗ 2", "Ổ cắm Hồ Cá Lỗ 3",
            "Đèn sân", "Đèn Kho", "Đèn phòng lạnh", "Ổ cắm Hồ Cá"]))
        n = 0
        for d in targets:
            for verb, to in (("bật", "on"), ("mở", "on"), ("tắt", "off")):
                for form in (str, str.upper, voiced.plain):
                    n += 1
                    got = voiced.match(form(f"{verb} {d['name']} giúp tôi"), GROUP_CFG)
                    self.assertEqual(got and (got[0], got[1]["id"], got[2]), ("set", d["id"], to), (verb, d["name"]))
        self.assertEqual(n, 9 * 9)
        g = voiced.grouped(GROUP_CFG)
        self.assertIs(voiced.grouped(g), g)  # gom lại cfg đã gom: giữ nguyên, không thành "Đèn phòng lạnh Đèn phòng lạnh Nút 1"

    def test_ai_context_and_group_tool(self):
        sp = voiced.system_prompt(GROUP_CFG)
        self.assertIn("- tuya_lanh_1: Đèn phòng lạnh Nút 1, Khu cafe", sp)
        self.assertIn("- group:Đèn phòng lạnh: Đèn phòng lạnh, Khu cafe (cả nhóm: Nút 1, Nút 2)", sp)
        self.assertIn("- group:Ổ cắm Hồ Cá: Ổ cắm Hồ Cá, Sân sau (cả nhóm: Lỗ 1, Lỗ 2, Lỗ 3) [có kênh cần PIN]", sp)
        self.assertIn("- tuya_san_1: Đèn sân, Sân sau", sp)
        self.assertNotIn("- tuya_lanh_1: Nút 1,", sp)  # không còn dòng thiết bị tên trần "Nút 1"
        st = voiced.state_text(GROUP_CFG, {"values": {"dev:tuya_lanh_1": "ON"}, "alerts": []})
        self.assertIn("- Đèn phòng lạnh Nút 1 (tuya_lanh_1): bật", st)
        self.assertNotIn("group:", st)  # trạng thái liệt kê từng kênh, không lặp nhóm
        r1 = {"role": "assistant", "content": None, "tool_calls": [
            tc("t1", "set_device", {"device": "group:Đèn phòng lạnh", "to": "on"}),
            tc("t2", "set_device", {"device": "group:Ổ cắm Hồ Cá", "to": "off"}),
            tc("t3", "set_device", {"device": "tuya_lanh_2", "to": "off"})]}
        api, hub = self.fake(r1, say("Xong.")), FakeHub(cfg=GROUP_CFG)
        self.assertEqual(voiced.ask_ai("bật đèn phòng lạnh, tắt ổ cắm hồ cá", GROUP_CFG, hub, voiced.AI(api.url, "", "m")), "Xong.")
        tools = api.chats()[1][3]["messages"][-3:]
        self.assertEqual(tools[0]["content"], "Đã bật Đèn phòng lạnh.")
        self.assertTrue(tools[1]["content"].startswith("LỖI"), tools[1])
        self.assertIn("Lỗ 3 của Ổ cắm Hồ Cá được bảo vệ", tools[1]["content"])
        self.assertEqual(tools[2]["content"], "Đã tắt Đèn phòng lạnh Nút 2.")
        self.assertEqual([c[2]["device"] for c in hub.calls if c[1] == "/cmd"],
                         ["tuya_lanh_1", "tuya_lanh_2", "tuya_ca_1", "tuya_ca_2", "tuya_ca_3", "tuya_lanh_2"])


if __name__ == "__main__":
    unittest.main()
