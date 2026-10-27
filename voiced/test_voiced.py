import io, json, re, tempfile, threading, unittest, urllib.request, wave
from http.server import ThreadingHTTPServer
from pathlib import Path
from types import SimpleNamespace as NS

import anthropic
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
    def __init__(self, values=None):
        self.calls, self.values, self.alerts = [], values or {}, []

    def __call__(self, method, path, body=None):
        self.calls.append((method, path, body))
        if path == "/config":
            return 200, CFG
        if path == "/state":
            return 200, {"values": self.values, "alerts": self.alerts}
        if path.startswith("/history"):
            return 200, [{"t": 1000, "v": 80}, {"t": 2000, "v": 90}, {"t": 3000, "v": 85}]
        target = body["device"] if path == "/cmd" else path.rsplit("/", 1)[-1]
        if target in CFG["locked"]:
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


class TestRun(unittest.TestCase):
    def test_run_replies_and_pin_block(self):
        hub = FakeHub({"jk.soc": "85", "deye.pv": "1234.5", "dev:tuya_eba30c56ca208baad0y43a": "ON",
                       "online:tuya_eb89a0372bd6a5acf1owhi": "offline"})
        say = lambda t: voiced.run(voiced.match(t, CFG), hub)
        self.assertEqual(say("bật đèn kho"), "Đã bật Đèn Kho.")
        self.assertIn("cần PIN", say("tắt CB tổng bếp"))
        self.assertEqual(hub.calls[-1], ("POST", "/cmd", {"device": "tuya_eb6564a5a81a53bb8ayyzs", "to": "off"}))
        self.assertEqual(say("ra khỏi nhà"), "Đã chạy Ra khỏi nhà.")
        self.assertIn("cần PIN", say("đóng quán"))
        self.assertEqual(say("pin còn bao nhiêu"), "Pin lưu trữ 85 phần trăm.")
        self.assertEqual(say("điện mặt trời bao nhiêu"), "Điện mặt trời 1234,5 oát.")
        self.assertEqual(say("đèn kho bật chưa"), "Đèn Kho đang bật.")
        self.assertEqual(say("đèn phòng lạnh đang bật không"), "Đèn phòng lạnh đang mất kết nối.")
        self.assertEqual(say("điện lưới bao nhiêu"), "Điện lưới chưa có số đo.")
        self.assertEqual(say("tắt báo động"), "Đã xác nhận cảnh báo.")
        self.assertEqual(hub.calls[-1], ("POST", "/alarm/ack", None))


def block(**kw):
    return NS(**kw)


class FakeClaude:
    """Trả lần lượt các phản hồi soạn sẵn; ghi lại tham số mỗi lần gọi."""
    def __init__(self, *responses, error=None):
        self.responses, self.error, self.calls = list(responses), error, []
        self.beta = NS(messages=NS(create=self.create))

    def create(self, **kw):
        self.calls.append({**kw, "messages": list(kw["messages"])})
        if self.error:
            raise self.error
        return self.responses.pop(0)


class TestClaude(unittest.TestCase):
    def test_tool_loop_and_pin_refusal(self):
        r1 = NS(stop_reason="tool_use", content=[
            block(type="thinking", thinking="", signature="s"),
            block(type="tool_use", id="t1", name="get_state", input={}),
            block(type="tool_use", id="t2", name="query_history", input={"entity": "jk.soc", "hours": 24})])
        r2 = NS(stop_reason="tool_use", content=[block(type="tool_use", id="t3", name="set_device",
                                                        input={"device": "tuya_eb6564a5a81a53bb8ayyzs", "to": "off"})])
        r3 = NS(stop_reason="end_turn", content=[block(type="text", text="CB tổng bếp cần PIN, bạn bấm trên màn hình nhé.")])
        claude, hub = FakeClaude(r1, r2, r3), FakeHub({"jk.soc": "85"})
        reply = voiced.ask_claude("pin hôm nay thế nào, rồi cắt điện bếp", CFG, hub, claude)
        self.assertEqual(reply, "CB tổng bếp cần PIN, bạn bấm trên màn hình nhé.")
        first = claude.calls[0]
        self.assertEqual((first["model"], first["output_config"], first["fallbacks"]), ("claude-opus-5-5", {"effort": "low"}, "default"))
        self.assertEqual({t["name"] for t in first["tools"]}, {"get_state", "query_history", "set_device", "run_scene", "ack_alarm"})
        self.assertIn("Đèn Kho", first["system"])
        # Lượt 2: giữ nguyên nội dung trợ lý (cả thinking), trả đủ 2 tool_result trong một tin.
        m = claude.calls[1]["messages"]
        self.assertIs(m[1]["content"], r1.content)
        res = m[2]["content"]
        self.assertEqual([r["tool_use_id"] for r in res], ["t1", "t2"])
        self.assertIn("Pin lưu trữ (jk.soc): 85 %", res[0]["content"])
        self.assertIn("mới nhất 85", res[1]["content"])
        # Lượt 3: hubd chặn thiết bị cần PIN → tool_result lỗi, Claude không vượt được.
        denied = claude.calls[2]["messages"][-1]["content"][0]
        self.assertTrue(denied["is_error"])
        self.assertIn("cần PIN", denied["content"])
        self.assertIn("bấm trên màn hình", denied["content"])  # dặn Claude chuyển cho người, không thử đường khác
        self.assertIn(("POST", "/cmd", {"device": "tuya_eb6564a5a81a53bb8ayyzs", "to": "off"}), hub.calls)

    def test_refusal_and_offline(self):
        refused = FakeClaude(NS(stop_reason="refusal", content=[]))
        self.assertIn("không làm được", voiced.ask_claude("x", CFG, FakeHub(), refused))
        offline = FakeClaude(error=anthropic.APIConnectionError(request=None))
        self.assertIn("lệnh cố định", voiced.answer("trời hôm nay thế nào", FakeHub(), offline))

    def test_fixed_commands_never_call_claude(self):
        claude = FakeClaude(error=AssertionError("không được gọi Claude"))
        self.assertEqual(voiced.answer("BẬT ĐÈN KHO", FakeHub(), claude), "Đã bật Đèn Kho.")
        self.assertIn("lệnh cố định", voiced.answer("kể chuyện cười", FakeHub(), None))
        wrong_token = lambda *a: (401, "cần token trợ lý")
        self.assertIn("token trợ lý", voiced.answer("bật đèn kho", wrong_token, claude))


def wav_bytes(rate=48000, channels=1, seconds=0.5):
    b = io.BytesIO()
    with wave.open(b, "wb") as w:
        w.setnchannels(channels), w.setsampwidth(2), w.setframerate(rate)
        w.writeframes((np.sin(np.arange(int(rate * seconds) * channels) / 10) * 8000).astype("<i2").tobytes())
    return b.getvalue()


class TestServer(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.heard = []
        def stt(rate, samples):
            self.heard.append((rate, samples.shape))
            return "BẬT ĐÈN KHO"
        tts = voiced.TTS(Path(self.tmp.name), lambda text, path: path.write_bytes(wav_bytes(seconds=0.1)))
        self.app = voiced.App(FakeHub(), None, stt, tts)
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), voiced.handler(self.app))
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.base = f"http://127.0.0.1:{self.srv.server_port}"

    def tearDown(self):
        self.srv.shutdown(), self.srv.server_close(), self.tmp.cleanup()

    def post(self, body, ctype):
        req = urllib.request.Request(self.base + "/voice/ask", data=body, headers={"Content-Type": ctype})
        return json.load(urllib.request.urlopen(req))

    def test_ask_with_wav_and_text(self):
        r = self.post(wav_bytes(channels=2), "audio/wav")
        self.assertEqual((r["heard"], r["reply"]), ("bật đèn kho", "Đã bật Đèn Kho."))
        self.assertEqual(self.heard, [(48000, (24000,))])  # stereo → mono, giữ tần số gốc cho sherpa tự đổi
        self.assertRegex(r["audio"], r"^/voice/tts/[0-9a-f]{16}\.wav$")
        self.assertEqual(urllib.request.urlopen(self.base + r["audio"]).read()[:4], b"RIFF")
        r = self.post(json.dumps({"text": "ra khỏi nhà"}).encode(), "application/json")
        self.assertEqual(r["reply"], "Đã chạy Ra khỏi nhà.")

    def test_too_short_audio_is_not_sent_to_stt(self):
        # Chạm nút rồi gửi ngay: WAV rỗng/ngắn làm sherpa-onnx lỗi ("Invalid input shape {0,80}") → phải trả "chưa nghe rõ".
        for seconds in (0, 0.1):
            r = self.post(wav_bytes(seconds=seconds), "audio/wav")
            self.assertEqual((r["heard"], r["reply"]), ("", voiced.NOT_HEARD))
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
        self.assertRegex(ev[0]["audio"], r"^/voice/tts/[0-9a-f]{16}\.wav$")
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
        self.assertEqual(left, {f"{tts.key(t)}.wav" for t in ("câu 4", "câu 2", "câu 5")})

    def test_warm_prerenders_alert_phrases(self):
        self.app.warm()
        self.assertTrue((Path(self.tmp.name) / f"{self.app.tts.key('Có khói ở bếp')}.wav").exists())


if __name__ == "__main__":
    unittest.main()
