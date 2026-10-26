"""voiced — trợ lý giọng nói iNIX Home (thiết kế mục 15.2).

Âm thanh WAV (nút nói trên iPad; sau này loa vệ tinh ESP32-S3) → STT cục bộ (sherpa-onnx Zipformer tiếng Việt)
→ lệnh cố định khớp cục bộ theo tên thiết bị/kịch bản/số đo trong config (chạy cả khi mất internet)
→ câu tự do gửi Claude (tool use) → mọi thao tác đi qua API trợ lý của hubd bằng token riêng; hubd tự chặn
thiết bị/kịch bản cần PIN → câu trả lời đọc bằng VieNeu-TTS (giọng Hải Đăng), cache theo nội dung câu.
Cảnh báo nghe từ MQTT hubd/alert, đẩy tới loa (iPad) qua SSE; câu cảnh báo cố định tạo sẵn âm thanh lúc khởi động.

HTTP (chỉ trong mạng docker; dashboard đi qua hubd /voice/*, không mở cổng ra LAN):
  POST /voice/ask       body audio/wav (PCM 16-bit, tần số bất kỳ) hoặc JSON {"text": "..."}
                        → {"heard", "reply", "audio": "/voice/tts/<key>.wav"}
  GET  /voice/tts/<key>.wav
  GET  /voice/events    SSE: {"type": "alert", "level", "text", "audio"} | {"type": "ack"}
"""
import hashlib
import io
import json
import os
import queue
import re
import threading
import time
import unicodedata
import urllib.error
import urllib.request
import wave
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import quote, urlencode

import anthropic
import numpy as np

MODEL = "claude-opus-5-5"
VOICE = os.environ.get("VOICED_VOICE", "Hải Đăng")
MODELS = Path(os.environ.get("VOICED_MODELS", "/models"))
VN = timezone(timedelta(hours=7))
OFFLINE = "Không gọi được trợ lý trực tuyến nên tôi chỉ làm được lệnh cố định, như bật đèn kho hay pin còn bao nhiêu."
NOT_HEARD = "Tôi chưa nghe rõ, bạn nói lại nhé."
FIXED_ONLY = "Chưa có khoá Claude nên tôi chỉ làm được lệnh cố định, như bật đèn kho hay pin còn bao nhiêu."
ON_RE = re.compile(r"^\s*(on|1|true)\s*$", re.I)
UNITS = {"%": "phần trăm", "W": "oát", "kW": "ki lô oát", "kWh": "ki lô oát giờ", "V": "vôn", "A": "am pe",
         "°C": "độ C", "ppm": "pi pi em", "µg/m³": "micrô gam trên mét khối", "lux": "lux"}


class Hub:
    """API trợ lý của hubd (Bearer token). Trả (mã HTTP, JSON hoặc chữ). hubd không chạy → URLError (OSError)."""
    def __init__(self, url, token):
        self.url, self.token = url.rstrip("/"), token

    def __call__(self, method, path, body=None):
        req = urllib.request.Request(self.url + "/api/assistant" + path, method=method,
                                     data=None if body is None else json.dumps(body).encode(),
                                     headers={"Authorization": "Bearer " + self.token, "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=5) as r:
                code, raw = r.status, r.read()
        except urllib.error.HTTPError as e:
            code, raw = e.code, e.read()
        try:
            return code, json.loads(raw) if raw else None
        except ValueError:
            return code, raw.decode(errors="replace").strip()


# ---------- Lệnh cố định (cục bộ) ----------

def plain(s):
    """Chữ thường, bỏ dấu, đ→d. STT hay sai dấu ("RA KHỎI NHA") nên so tên trên chữ không dấu."""
    s = unicodedata.normalize("NFD", s.lower().replace("đ", "d"))
    return "".join(c for c in s if unicodedata.category(c) != "Mn")


def words(s):
    return re.findall(r"\w+", plain(s))


ON, OFF = {"bat", "mo"}, {"tat"}
QUESTION = {"nhieu", "may", "khong", "chua", "sao", "nao"}


def named(items, ws):
    """Mục có cả tên nằm trọn trong câu; nhiều mục khớp lấy tên dài nhất, hoà thì None (không đoán)."""
    hits = [(len(set(words(x["name"]))), x) for x in items if words(x["name"]) and set(words(x["name"])) <= ws]
    top = [x for n, x in hits if n == max(n for n, _ in hits)]
    return top[0] if len(top) == 1 else None


def measured(cfg, ws):
    """Số đo được hỏi: tên phải có từ đầu (pin, điện, công...) trong câu; điểm = (từ của tên, từ của thiết bị/phòng)."""
    ctx = {d["id"]: d["name"] for d in cfg["devices"]} | {z["id"]: z["name"] for z in cfg["zones"]}
    scored = []
    for e in cfg["entities"]:
        name = words(e["name"])
        if name and name[0] in ws:
            scored.append(((len(set(name) & ws), len(set(words(ctx.get(e.get("device"), "") + " " + ctx.get(e.get("zone"), ""))) & ws)), e))
    top = [e for s, e in scored if s == max(s for s, _ in scored)]
    return top[0] if len(top) == 1 else None


def match(text, cfg):
    """Hiểu câu bằng luật cố định. Trả (việc, đối tượng, on/off) hoặc None để Claude xử lý."""
    w = words(text)
    ws = set(w)
    verb = next((i for i, x in enumerate(w) if x in ON | OFF), None)
    to = None if verb is None else ("on" if w[verb] in ON else "off")
    if (to == "off" or {"xac", "nhan"} <= ws) and ("coi" in ws or {"bao", "dong"} <= ws or {"canh", "bao"} <= ws):
        return ("ack", None, None)
    dev = named(cfg["devices"], ws)
    if dev:
        first = min(w.index(x) for x in words(dev["name"]))
        return ("set", dev, to) if verb is not None and verb < first else ("ask_device", dev, None)
    scene = named(cfg["scenes"], ws)
    if scene and to != "off":
        return ("scene", scene, None)
    ent = measured(cfg, ws) if ws & QUESTION else None
    return ("ask_entity", ent, None) if ent else None


def num(v):
    try:
        return f"{round(float(v), 2):.2f}".rstrip("0").rstrip(".").replace(".", ",")
    except ValueError:
        return v


def run(intent, hub):
    kind, x, to = intent
    name = x and x["name"].strip()
    if kind == "set":
        code, msg = hub("POST", "/cmd", {"device": x["id"], "to": to})
        return {204: f"Đã {'bật' if to == 'on' else 'tắt'} {name}.",
                403: f"{name} cần PIN, hãy bấm trên màn hình."}.get(code, f"Không gửi được lệnh tới {name}.")
    if kind == "scene":
        code, msg = hub("POST", "/scene/" + quote(x["id"], safe=""))
        return {204: f"Đã chạy {name}.", 403: f"Kịch bản {name} cần PIN, hãy bấm trên màn hình."}.get(code, f"{name}: {msg}.")
    if kind == "ack":
        hub("POST", "/alarm/ack")
        return "Đã xác nhận cảnh báo."
    v = hub("GET", "/state")[1]["values"]
    if kind == "ask_device":
        if v.get("online:" + x["id"]) == "offline":
            return f"{name} đang mất kết nối."
        s = v.get("dev:" + x["id"])
        return f"{name} chưa có trạng thái." if s is None else f"{name} đang {'bật' if ON_RE.match(s) else 'tắt'}."
    if x["id"] not in v:
        return f"{name} chưa có số đo."
    return f"{name} {num(v[x['id']])} {UNITS.get(x.get('unit', ''), x.get('unit', ''))}".rstrip() + "."


# ---------- Câu tự do (Claude, tool use) ----------

def tool(name, desc, props=None, required=()):
    return {"name": name, "description": desc, "strict": True,
            "input_schema": {"type": "object", "properties": props or {}, "required": list(required), "additionalProperties": False}}


TOOLS = [
    tool("get_state", "Trạng thái hiện tại của nhà: thiết bị bật/tắt/mất kết nối, số đo cảm biến và năng lượng, cảnh báo chưa xác nhận."),
    tool("query_history", "Lịch sử một số đo trong N giờ gần nhất: nhỏ nhất, lớn nhất, trung bình, mới nhất và vài điểm theo giờ.",
         {"entity": {"type": "string", "description": "id số đo, vd jk.soc"},
          "hours": {"type": "integer", "description": "số giờ nhìn lại, 1 đến 4320"}}, ["entity", "hours"]),
    tool("set_device", "Bật hoặc tắt một thiết bị theo id.",
         {"device": {"type": "string"}, "to": {"type": "string", "enum": ["on", "off"]}}, ["device", "to"]),
    tool("run_scene", "Chạy một kịch bản theo id.", {"scene": {"type": "string"}}, ["scene"]),
    tool("ack_alarm", "Xác nhận đã biết các cảnh báo đang mở (thôi nhắc)."),
]


def system_prompt(cfg):
    zone = {z["id"]: z["name"] for z in cfg["zones"]}
    lock = lambda x: " [cần PIN]" if x["id"] in cfg["locked"] else ""
    dev = {d["id"]: d["name"].strip() for d in cfg["devices"]}
    return "\n".join([
        "Bạn là trợ lý giọng nói của iNIX Home, nhà 3 tầng kiêm quán cafe.",
        "Câu của người dùng do nhận dạng giọng nói chép ra nên có thể sai dấu hoặc sai chữ; hãy hiểu theo tên thiết bị gần nhất.",
        "Câu trả lời sẽ được đọc thành tiếng: tiếng Việt, một hai câu ngắn, không markdown, không liệt kê dài.",
        "Chỉ xem và điều khiển nhà bằng các tool. Tool báo cần PIN thì bảo người dùng bấm trên màn hình, không thử cách khác.",
        "Không bịa số liệu; chưa có dữ liệu thì nói chưa có.",
        f"Bây giờ là {datetime.now(VN):%H:%M ngày %d/%m/%Y} (giờ Việt Nam).",
        "", "Thiết bị (id: tên, phòng):",
        *(f"- {d['id']}: {d['name'].strip()}, {zone.get(d.get('zone'), 'chưa gán phòng')}{lock(d)}" for d in cfg["devices"]),
        "", "Kịch bản (id: tên):",
        *(f"- {s['id']}: {s['name']}{lock(s)}" for s in cfg["scenes"]),
        "", "Số đo (id: tên, đơn vị, thuộc):",
        *(f"- {e['id']}: {e['name']}, {e.get('unit', '')}, {dev.get(e.get('device')) or zone.get(e.get('zone'), '')}" for e in cfg["entities"]),
    ])


def state_text(cfg, st):
    v = st["values"]
    def dev(d):
        if v.get("online:" + d["id"]) == "offline":
            return "mất kết nối"
        s = v.get("dev:" + d["id"])
        return "chưa rõ" if s is None else ("bật" if ON_RE.match(s) else "tắt")
    return "\n".join(["Thiết bị:", *(f"- {d['name'].strip()} ({d['id']}): {dev(d)}" for d in cfg["devices"]),
                      "Số đo:", *(f"- {e['name']} ({e['id']}): {v.get(e['id'], 'chưa có')} {e.get('unit', '')}".rstrip() for e in cfg["entities"]),
                      "Cảnh báo chưa xác nhận: " + ("; ".join(f"{a['level']}: {a['text']}" for a in st["alerts"]) or "không có")])


def history_text(entity, hours, pts):
    if not pts:
        return f"{entity}: không có dữ liệu trong {hours} giờ qua."
    vs = [p["v"] for p in pts]
    step = max(1, len(pts) // 12)
    sample = ", ".join(f"{datetime.fromtimestamp(p['t'], VN):%d/%m %H:%M}={num(p['v'])}" for p in pts[::step])
    return (f"{entity}, {hours} giờ qua: nhỏ nhất {num(min(vs))}, lớn nhất {num(max(vs))}, trung bình {num(sum(vs) / len(vs))}, "
            f"mới nhất {num(vs[-1])}. Mẫu: {sample}")


def verdict(code, msg):
    if code == 204:
        return "đã gửi lệnh", False
    if code == 403:
        return "hubd từ chối: cần PIN. Bảo người dùng bấm trên màn hình, không thử cách khác.", True
    return f"lỗi {code}: {msg}", True


def call_tool(b, hub, cfg):
    a = b.input
    try:
        if b.name == "get_state":
            code, data = hub("GET", "/state")
            out, err = (state_text(cfg, data), False) if code == 200 else (f"lỗi {code}", True)
        elif b.name == "query_history":
            hours = min(max(int(a["hours"]), 1), 4320)
            code, pts = hub("GET", "/history?" + urlencode({"id": a["entity"], "hours": hours}))
            out, err = (history_text(a["entity"], hours, pts), False) if code == 200 else (f"lỗi {code}", True)
        elif b.name == "set_device":
            out, err = verdict(*hub("POST", "/cmd", {"device": a["device"], "to": a["to"]}))
        elif b.name == "run_scene":
            out, err = verdict(*hub("POST", "/scene/" + quote(a["scene"], safe="")))
        elif b.name == "ack_alarm":
            out, err = verdict(*hub("POST", "/alarm/ack"))
        else:
            out, err = f"không có tool {b.name}", True
    except Exception as e:  # hubd mất kết nối, input lạ: báo Claude thay vì làm hỏng cả lượt
        out, err = f"lỗi: {e}", True
    return {"type": "tool_result", "tool_use_id": b.id, "content": out, "is_error": err}


def ask_claude(text, cfg, hub, client):
    msgs = [{"role": "user", "content": text}]
    for _ in range(6):
        r = client.beta.messages.create(
            model=MODEL, max_tokens=4096, system=system_prompt(cfg), tools=TOOLS, messages=msgs,
            output_config={"effort": "low"},
            betas=["server-side-fallback-2026-07-01"], fallbacks="default")  # bị từ chối thì máy chủ tự thử model dự phòng
        if r.stop_reason == "refusal":
            return "Xin lỗi, việc này tôi không làm được."
        msgs.append({"role": "assistant", "content": r.content})  # giữ nguyên cả khối thinking
        calls = [b for b in r.content if b.type == "tool_use"]
        if r.stop_reason != "tool_use" or not calls:
            return " ".join(b.text for b in r.content if b.type == "text").strip() or "Tôi chưa có câu trả lời."
        msgs.append({"role": "user", "content": [call_tool(b, hub, cfg) for b in calls]})
    return "Việc này nhiều bước quá, bạn nói cụ thể hơn giúp tôi."


def answer(text, hub, claude):
    if not text.strip():
        return NOT_HEARD
    try:
        code, cfg = hub("GET", "/config")
        if code != 200:
            return "Trung tâm từ chối trợ lý, cần kiểm tra token trợ lý."
        intent = match(text, cfg)
        if intent:
            return run(intent, hub)
    except OSError:
        return "Không kết nối được trung tâm."
    if claude is None:
        return FIXED_ONLY
    try:
        return ask_claude(text, cfg, hub, claude)
    except anthropic.APIConnectionError:
        return OFFLINE
    except anthropic.APIStatusError as e:
        print(f"Claude lỗi {e.status_code} request_id={e.request_id}", flush=True)
        return "Trợ lý trực tuyến đang lỗi, bạn thử lại sau."


# ---------- Âm thanh ----------

class TTS:
    """Đọc thành WAV, cache theo (giọng, câu): câu cảnh báo và câu lặp lại không phải tổng hợp lại.
    Giữ `keep` file dùng gần nhất (câu trả lời của Claude hầu như không lặp lại)."""
    keep = 500  # ponytail: xoá theo mtime khi ghi file mới, ~0,5 MB/file → tối đa vài trăm MB

    def __init__(self, cache, synth):
        self.cache, self.synth, self.lock = cache, synth, threading.Lock()
        cache.mkdir(parents=True, exist_ok=True)

    def key(self, text):
        return hashlib.sha1(f"{VOICE}\n{text}".encode()).hexdigest()[:16]

    def url(self, text):
        k = self.key(text)
        p = self.cache / f"{k}.wav"
        with self.lock:
            if p.exists():
                os.utime(p)  # đánh dấu vừa dùng
            else:
                tmp = self.cache / f"{k}.part.wav"
                self.synth(text, tmp)
                tmp.replace(p)
                for old in sorted(self.cache.glob("*.wav"), key=lambda f: f.stat().st_mtime_ns)[:-self.keep]:
                    old.unlink(missing_ok=True)
        return f"/voice/tts/{k}.wav"


def vieneu_synth():
    from vieneu import Vieneu
    from vieneu._v3_turbo_engine import onnx_runtime_lite as orl
    # vieneu 3.8.3 không chuyển codec_dir xuống engine: trỏ codec về bản tải sẵn trong image để chạy offline.
    orig = orl.OnnxV3LiteEngine._fetch
    orl.OnnxV3LiteEngine._fetch = staticmethod(
        lambda repo, files, sub: MODELS / "codec" if repo == orl._CODEC_REPO else orig(repo, files, sub))
    v = Vieneu(onnx_dir=str(MODELS / "vieneu" / "onnx_int8"))  # tải sẵn bởi fetch_models.py
    return lambda text, path: v.save(v.infer(text, voice=VOICE), str(path))


def sherpa_stt():
    import sherpa_onnx
    d = MODELS / "stt"
    r = sherpa_onnx.OfflineRecognizer.from_transducer(
        encoder=str(d / "encoder.int8.onnx"), decoder=str(d / "decoder.onnx"), joiner=str(d / "joiner.int8.onnx"),
        tokens=str(d / "tokens.txt"), num_threads=int(os.environ.get("VOICED_THREADS", "4")))
    lock = threading.Lock()
    def stt(rate, samples):
        with lock:
            s = r.create_stream()
            s.accept_waveform(rate, samples)  # sherpa tự đổi tần số về 16 kHz
            r.decode_stream(s)
            return s.result.text
    return stt


def read_wav(b):
    with wave.open(io.BytesIO(b)) as w:
        if w.getsampwidth() != 2 or not 8000 <= w.getframerate() <= 192000:
            raise ValueError("cần WAV PCM 16-bit, 8–192 kHz")
        a = np.frombuffer(w.readframes(w.getnframes()), dtype="<i2").astype(np.float32) / 32768
        return w.getframerate(), a.reshape(-1, w.getnchannels()).mean(axis=1)


# ---------- Dịch vụ ----------

class App:
    def __init__(self, hub, claude, stt, tts):
        self.hub, self.claude, self.stt, self.tts = hub, claude, stt, tts
        self.listeners, self.lock = set(), threading.Lock()

    def speak(self, text):
        try:
            return self.tts.url(text)
        except Exception as e:  # mất tiếng vẫn còn chữ
            print(f"TTS lỗi: {e}", flush=True)
            return None

    def broadcast(self, ev):
        with self.lock:
            for q in self.listeners:
                q.put(ev)

    def on_mqtt(self, topic, payload):
        try:
            if topic == "hubd/alert/ack":
                return self.broadcast({"type": "ack"})
            a = json.loads(payload)
            text = str(a["text"])
            self.broadcast({"type": "alert", "level": str(a.get("level", "info")), "text": text, "audio": self.speak(text)})
        except Exception as e:  # paho 1.6 ném lại lỗi và dừng luồng MQTT: một tin hỏng không được làm câm cảnh báo
            print(f"bỏ qua tin {topic}: {e!r}", flush=True)

    def warm(self):
        """Tạo sẵn âm thanh cho mọi câu cảnh báo cố định trong config (đọc được ngay, cả khi mất internet)."""
        while True:
            try:
                phrases = self.hub("GET", "/config")[1]["phrases"]
                break
            except (OSError, TypeError, KeyError):
                time.sleep(10)
        for p in phrases + [OFFLINE, FIXED_ONLY, NOT_HEARD]:  # câu cố định dài, đọc lần đầu mất vài giây
            self.speak(p)
        print(f"đã tạo sẵn {len(phrases)} câu cảnh báo", flush=True)


def handler(app):
    class H(BaseHTTPRequestHandler):
        def send(self, code, body, ctype="application/json; charset=utf-8"):
            self.send_response(code)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            if self.path != "/voice/ask":
                return self.send_error(404)
            n = self.headers.get("Content-Length", "0")
            if not n.isdigit() or int(n) > 8_000_000:
                return self.send(400, "Content-Length không hợp lệ (tối đa 8 MB)".encode(), "text/plain; charset=utf-8")
            n = int(n)
            body = self.rfile.read(n)
            t0 = time.perf_counter()
            try:
                if self.headers.get("Content-Type", "").startswith("application/json"):
                    heard = str(json.loads(body)["text"])
                else:
                    rate, a = read_wav(body)
                    # chạm rồi gửi ngay: âm thanh quá ngắn làm STT lỗi, coi như chưa nghe gì
                    heard = app.stt(rate, a).strip().lower() if len(a) >= rate * 0.3 else ""
            except Exception:
                return self.send(400, "cần WAV PCM 16-bit hoặc JSON {\"text\": ...}".encode(), "text/plain; charset=utf-8")
            t1 = time.perf_counter()
            try:
                reply = answer(heard, app.hub, app.claude)
            except Exception as e:
                print(f"lỗi khi trả lời {heard!r}: {e!r}", flush=True)
                reply = "Có lỗi, bạn thử lại sau."
            t2 = time.perf_counter()
            audio = app.speak(reply)
            ms = lambda a, b: f"{(b - a) * 1000:.0f} ms"
            print(f"nghe: {heard!r} → {reply!r} (STT {ms(t0, t1)}, xử lý {ms(t1, t2)}, TTS {ms(t2, time.perf_counter())})", flush=True)
            self.send(200, json.dumps({"heard": heard, "reply": reply, "audio": audio}, ensure_ascii=False).encode())

        def do_GET(self):
            if m := re.fullmatch(r"/voice/tts/([0-9a-f]{16})\.wav", self.path):
                p = app.tts.cache / f"{m[1]}.wav"
                return self.send(200, p.read_bytes(), "audio/wav") if p.exists() else self.send_error(404)
            if self.path == "/voice/healthz":
                return self.send(200, b"ok", "text/plain")
            if self.path != "/voice/events":
                return self.send_error(404)
            q = queue.Queue()
            with app.lock:
                app.listeners.add(q)
            try:
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Cache-Control", "no-cache")
                self.end_headers()
                self.wfile.write(b": ok\n\n")
                self.wfile.flush()
                while True:
                    try:
                        self.wfile.write(f"data: {json.dumps(q.get(timeout=25), ensure_ascii=False)}\n\n".encode())
                    except queue.Empty:
                        self.wfile.write(b": ping\n\n")
                    self.wfile.flush()
            except OSError:
                pass
            finally:
                with app.lock:
                    app.listeners.discard(q)

        def log_message(self, fmt, *args):
            pass

    return H


def main():
    import paho.mqtt.client as mqtt
    hub = Hub(os.environ.get("HUBD_URL", "http://hubd:8080"), os.environ["HUBD_ASSISTANT_TOKEN"])
    claude = anthropic.Anthropic(timeout=20.0, max_retries=1) if os.environ.get("ANTHROPIC_API_KEY") else None
    if claude is None:
        print("chưa có ANTHROPIC_API_KEY: chỉ chạy lệnh cố định", flush=True)
    app = App(hub, claude, sherpa_stt(), TTS(Path(os.environ.get("VOICED_CACHE", "/cache")), vieneu_synth()))
    threading.Thread(target=app.warm, daemon=True).start()

    c = mqtt.Client("voiced")
    c.username_pw_set(os.environ.get("MQTT_USER"), os.environ.get("MQTT_PASS"))
    c.on_connect = lambda c, u, f, rc: c.subscribe([("hubd/alert", 1), ("hubd/alert/ack", 1)])
    c.on_message = lambda c, u, m: app.on_mqtt(m.topic, m.payload)
    c.connect_async(os.environ.get("MQTT_HOST", "mosquitto"), 1883)
    c.loop_start()  # tự nối lại khi broker khởi động lại

    srv = ThreadingHTTPServer(("", 8090), handler(app))
    srv.daemon_threads = True
    print("voiced nghe tại :8090", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
