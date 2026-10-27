"""voiced — trợ lý giọng nói iNIX Home (thiết kế mục 15.2).

Âm thanh WAV (nút nói trên iPad; sau này loa vệ tinh ESP32-S3) → STT cục bộ (sherpa-onnx Zipformer tiếng Việt)
→ lệnh cố định khớp cục bộ theo tên thiết bị/kịch bản/số đo trong config (chạy cả khi mất internet)
→ câu tự do gửi AI qua API chat completions tương thích OpenAI (tool calling; URL, khoá, model nhập ở trang Cài đặt,
lưu trong hubd nhóm "ai") → mọi thao tác đi qua API trợ lý của hubd bằng token riêng; hubd tự chặn thiết bị/kịch bản
cần PIN → câu trả lời đọc bằng VieNeu-TTS (giọng Nam/Nữ chọn ở Cài đặt), cache theo giọng + câu.
Cảnh báo nghe từ MQTT hubd/alert, đẩy tới loa (iPad) qua SSE; câu cảnh báo cố định tạo sẵn âm thanh lúc khởi động.
Cấu hình đọc qua GET /api/assistant/settings/{ai,voice}, đọc lại khi MQTT hubd/settings/<nhóm> = changed:
nhóm "ai" (URL, khoá, model), nhóm "voice" (giọng, cách gọi chủ nhà, tên trợ lý, tốc độ đọc, giờ yên lặng).

HTTP (chỉ trong mạng docker; dashboard đi qua hubd /voice/*, không mở cổng ra LAN):
  POST /voice/ask       body audio/wav (PCM 16-bit, tần số bất kỳ) hoặc JSON {"text": "..."}
                        → {"heard", "reply", "audio": "/voice/tts/<key>.wav"}
  POST /voice/brief     tóm tắt tình hình (thời tiết, không khí, điện/pin, cảnh báo) → {"reply", "audio"}
  POST /voice/ai/check  JSON {url, api_key, model} (ô trống = giá trị đã lưu) → {"ok", "models", "message"}
  POST /voice/preview   JSON {voice, owner, name} (đang nhập, chưa lưu) → lời chào thử {"reply", "audio"}
  GET  /voice/tts/<key>.wav
  GET  /voice/events    SSE: {"type": "alert", "level", "text", "audio"} | {"type": "ack"}
"""
import hashlib
import io
import json
import math
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
from urllib.parse import quote, urlencode, urlsplit

import numpy as np

# Giọng có sẵn trong gói vieneu (assets/voices_v3_turbo.json, 25 giọng): chạy không cần mạng, không phải tải thêm.
VOICES = {"Hải Đăng": "Nam", "Trúc Ly": "Nữ"}  # cùng kiểu: miền Bắc, đọc tự nhiên
DEFAULT_VOICE = "Hải Đăng"
MODELS = Path(os.environ.get("VOICED_MODELS", "/models"))
VN = timezone(timedelta(hours=7))
# Câu cố định có xưng hô: {me}/{Me} = trợ lý tự xưng, {you} = cách gọi chủ nhà (Persona.fmt).
OFFLINE = "Không gọi được trợ lý trực tuyến nên {me} chỉ làm được lệnh cố định, như bật đèn kho hay pin còn bao nhiêu."
NOT_HEARD = "{Me} chưa nghe rõ, {you} nói lại nhé."
FIXED_ONLY = "Chưa cài AI nên {me} chỉ làm được lệnh cố định, như bật đèn kho hay pin còn bao nhiêu."
AI_KEY = "Khoá API của AI bị từ chối, hãy kiểm tra trong Cài đặt."
AI_ERROR = "Trợ lý trực tuyến đang lỗi, bạn thử lại sau."
ON_RE = re.compile(r"^\s*(on|1|true)\s*$", re.I)
CLOCK = re.compile(r"([01]?\d|2[0-3]):([0-5]\d)")
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


class Persona:
    """Giọng và xưng hô (nhóm cấu hình "voice" của hubd). owner = cách gọi chủ nhà ("anh Bình"): có thì trợ lý xưng "em"
    và lễ phép; trống thì nói như cũ (tôi / bạn). quiet_from/quiet_to ("22:00"/"06:00") = giờ không tự đọc cảnh báo.
    speed (tốc độ đọc) do dashboard áp khi phát, không cần ở đây."""
    def __init__(self, s=None):
        get = lambda k: str((s or {}).get(k) or "").strip()
        self.voice = get("voice") if get("voice") in VOICES else DEFAULT_VOICE
        self.owner, self.name = get("owner"), get("name")
        self.me, self.you = ("em", self.owner) if self.owner else ("tôi", "bạn")
        self.window = [CLOCK.fullmatch(get(k)) for k in ("quiet_from", "quiet_to")]

    def fmt(self, template):
        return template.format(me=self.me, Me=self.me.capitalize(), you=self.you)

    def say(self, text):
        """Câu trả lời cố định: có cách gọi chủ nhà thì mở đầu "Dạ anh Bình, ..." (giữ hoa từ viết tắt như CB)."""
        if not self.owner:
            return text
        return f"Dạ {self.owner}, {text[0] if text[1:2].isupper() else text[0].lower()}{text[1:]}"

    def greet(self):
        out = f"{self.me} {'là ' + self.name + ' ' if self.name else ''}đây, {self.you} cần gì{' ạ' if self.owner else ''}?"
        return "Dạ, " + out if self.owner else out[0].upper() + out[1:]

    def quiet(self, now):
        a, b = (int(m[1]) * 60 + int(m[2]) if m else None for m in self.window)
        return a is not None and b is not None and a != b and (now.hour * 60 + now.minute - a) % 1440 < (b - a) % 1440


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
    """Hiểu câu bằng luật cố định. Trả (việc, đối tượng, on/off) hoặc None để AI xử lý."""
    w = words(text)
    ws = set(w)
    verb = next((i for i, x in enumerate(w) if x in ON | OFF), None)
    to = None if verb is None else ("on" if w[verb] in ON else "off")
    if (to == "off" or {"xac", "nhan"} <= ws) and ("coi" in ws or {"bao", "dong"} <= ws or {"canh", "bao"} <= ws):
        return ("ack", None, None)
    if {"tinh", "hinh"} <= ws or {"tom", "tat"} <= ws:
        return ("brief", None, None)
    dev = named(cfg["devices"], ws)
    if dev:
        first = min(w.index(x) for x in words(dev["name"]))
        return ("set", dev, to) if verb is not None and verb < first else ("ask_device", dev, None)
    scene = named(cfg["scenes"], ws)
    if scene and to != "off":
        return ("scene", scene, None)
    ent = measured(cfg, ws) if ws & QUESTION else None
    return ("ask_entity", ent, None) if ent else None


GREET = {"xin", "chao", "alo", "oi", "a", "nhe"}


def greeting(text, p):
    """Chỉ gọi trợ lý ("chào", "alo", "Mây ơi"): trả lời lời chào."""
    ws = set(words(text))
    return bool(ws) and ws <= GREET | set(words(p.name))


def num(v):
    try:
        return f"{round(float(v), 2):.2f}".rstrip("0").rstrip(".").replace(".", ",")
    except ValueError:
        return v


def run(intent, hub, p=Persona()):
    return summary(hub, p) if intent[0] == "brief" else p.say(reply(intent, hub))


def reply(intent, hub):
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


# ---------- Tóm tắt tình hình (nút đọc trên dashboard, "tình hình thế nào"): tính cục bộ, không cần AI ----------

def watt(x):
    return f"{num(round(abs(x) / 1000, 1))} ki lô oát" if abs(x) >= 1000 else f"{round(abs(x))} oát"


def brief(cfg, st, p=Persona()):
    v = st["values"]
    def val(i):
        try:
            x = float(v[i])
        except (KeyError, TypeError, ValueError):
            return None
        return None if math.isnan(x) else x
    zone = {z["id"]: z for z in cfg["zones"]}
    outdoor = lambda e: zone.get(e.get("zone"), {}).get("type") == "outdoor"
    sensors = [(e, val(e["id"])) for e in cfg["entities"] if not e.get("device") and val(e["id"]) is not None]
    def top(pick):  # số đo lớn nhất trong nhà (cùng cách dashboard lấy phòng tệ nhất)
        xs = [(x, e) for e, x in sensors if not outdoor(e) and pick(e)]
        return max(xs, key=lambda p: p[0]) if xs else (None, None)
    parts = []

    out = [(e, x) for e, x in sensors if outdoor(e)]
    first = lambda pick: next((x for e, x in out if pick(e)), None)
    t, h, rain = first(lambda e: e.get("unit") == "°C"), first(lambda e: e.get("unit") == "%"), first(lambda e: "mua" in words(e["name"]))
    w = [f"{num(t)} độ"] if t is not None else []
    w += [f"độ ẩm {num(h)} phần trăm"] if h is not None else []
    w += ["đang mưa" if rain >= 0.5 else "không mưa"] if rain is not None else []
    if w:
        parts.append("Ngoài trời " + ", ".join(w) + ".")

    name = lambda e: zone.get(e.get("zone"), {}).get("name", "")
    co2, ce = top(lambda e: re.search(r"co[2₂]", e["name"], re.I))
    pm, pe = top(lambda e: e.get("unit") == "µg/m³")
    air = [f"CO2 cao nhất {num(co2)} pi pi em ở {name(ce)}"] if ce else []
    air += [f"bụi mịn cao nhất {num(pm)} micrô gam ở {name(pe)}"] if pe else []
    if air:
        parts.append("Trong nhà " + ", ".join(air) + ".")

    soc, bat, pv, load, grid = (val(i) for i in ("jk.soc", "jk.power", "deye.pv", "deye.load", "deye.grid"))
    pw = []
    if soc is not None:
        state = "" if bat is None or abs(bat) <= 5 else f", đang {'sạc' if bat > 0 else 'xả'} {watt(bat)}"
        pw.append(f"pin {num(soc)} phần trăm{state}")
    pw += [f"điện mặt trời {watt(pv)}"] if pv is not None else []
    pw += [f"nhà dùng {watt(load)}"] if load is not None else []
    pw += [f"đang {'mua' if grid > 0 else 'bán'} lưới {watt(grid)}"] if grid is not None and abs(grid) > 5 else []
    if pw:
        e = "; ".join(pw)
        parts.append(e[0].upper() + e[1:] + ".")

    if not parts:
        parts.append("Chưa có số đo thời tiết, không khí hay điện.")
    alerts = list(dict.fromkeys([a["text"] for a in st["alerts"]] + [x for k, x in v.items() if k.startswith("alert:") and x]))
    parts.append(f"Có {len(alerts)} cảnh báo: " + "; ".join(x.rstrip(". ") for x in alerts) + "." if alerts else "Không có cảnh báo nào.")
    return " ".join(([f"Chào {p.owner}."] if p.owner else []) + parts)


def summary(hub, p=Persona()):
    return brief(hub("GET", "/config")[1], hub("GET", "/state")[1], p)


# ---------- Câu tự do (AI qua API tương thích OpenAI, tool calling) ----------

def tool(name, desc, props=None, required=()):
    return {"type": "function", "function": {"name": name, "description": desc,
            "parameters": {"type": "object", "properties": props or {}, "required": list(required), "additionalProperties": False}}}


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


def system_prompt(cfg, p=Persona()):
    zone = {z["id"]: z["name"] for z in cfg["zones"]}
    lock = lambda x: " [cần PIN]" if x["id"] in cfg["locked"] else ""
    dev = {d["id"]: d["name"].strip() for d in cfg["devices"]}
    return "\n".join([
        f"Bạn là {p.name + ', ' if p.name else ''}trợ lý giọng nói của iNIX Home, nhà 3 tầng kiêm quán cafe.",
        *([f'Người nói chuyện với bạn là chủ nhà: gọi họ là "{p.owner}", tự xưng "em", nói lễ phép (dạ, ạ).'] if p.owner else []),
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
        return "đã gửi lệnh"
    if code == 403:
        return "LỖI: hubd từ chối, cần PIN. Bảo người dùng bấm trên màn hình, không thử cách khác."
    return f"LỖI {code}: {msg}"


def call_tool(name, args, hub, cfg):
    """Chạy một lời gọi tool, trả chữ cho message role=tool. Lỗi bắt đầu bằng "LỖI" (API OpenAI không có cờ lỗi)."""
    try:
        a = json.loads(args or "{}")
        if name == "get_state":
            code, data = hub("GET", "/state")
            return state_text(cfg, data) if code == 200 else f"LỖI {code}"
        if name == "query_history":
            hours = min(max(int(a["hours"]), 1), 4320)
            code, pts = hub("GET", "/history?" + urlencode({"id": a["entity"], "hours": hours}))
            return history_text(a["entity"], hours, pts) if code == 200 else f"LỖI {code}"
        if name == "set_device":
            return verdict(*hub("POST", "/cmd", {"device": a["device"], "to": a["to"]}))
        if name == "run_scene":
            return verdict(*hub("POST", "/scene/" + quote(a["scene"], safe="")))
        if name == "ack_alarm":
            return verdict(*hub("POST", "/alarm/ack"))
        return f"LỖI: không có tool {name}"
    except Exception as e:  # hubd mất kết nối, tham số hỏng: báo model thay vì làm hỏng cả lượt
        return f"LỖI: {e!r}"


def base_url(url):
    return url.strip().rstrip("/").removesuffix("/chat/completions")


def url_problem(url):
    """Địa chỉ AI do người có PIN nhập: chỉ http(s), không ? hay # nên chỉ gọi được {url}/models và {url}/chat/completions
    (không dùng được để gọi đường dẫn tuỳ ý của dịch vụ nội bộ như go2rtc, hubd)."""
    u = urlsplit(url)
    if u.scheme not in ("http", "https") or not u.hostname or "?" in url or "#" in url:
        return "Địa chỉ API phải dạng http:// hoặc https://máy/đường-dẫn, không có ? hay #."


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **kw):
        return None  # 3xx thành HTTPError: endpoint không chuyển hướng được sang dịch vụ nội bộ


OPEN = urllib.request.build_opener(NoRedirect).open


def api_error(e):
    """error.message của API kiểu OpenAI (vd model không tồn tại); không trả nguyên thân phản hồi."""
    try:
        return ": " + str(json.loads(e.read())["error"]["message"])[:200]
    except Exception:
        return ""


class AI:
    """API chat completions tương thích OpenAI (OpenAI, OpenRouter, Groq, Gemini, Ollama/LM Studio trong LAN...),
    chỉ dùng thư viện chuẩn. Lỗi mạng → OSError; HTTP lỗi → urllib.error.HTTPError (cũng là OSError)."""
    def __init__(self, url, key, model, timeout=20):
        self.url, self.key, self.model, self.timeout = base_url(url), key or "", model, timeout

    @classmethod
    def from_settings(cls, s):
        """Nhóm cấu hình "ai" của hubd. Chưa có URL hoặc model → None (chỉ lệnh cố định); khoá được trống (Ollama)."""
        return cls(s["url"], s.get("api_key"), s["model"]) if s.get("url") and s.get("model") else None

    def call(self, path, body=None):
        if problem := url_problem(self.url):  # urllib còn mở được file://
            raise ValueError(problem)
        headers = {"Content-Type": "application/json"} | ({"Authorization": "Bearer " + self.key} if self.key else {})
        req = urllib.request.Request(self.url + path, data=None if body is None else json.dumps(body).encode(), headers=headers)
        with OPEN(req, timeout=self.timeout) as r:
            return json.load(r)

    def chat(self, messages, tools):
        return self.call("/chat/completions", {"model": self.model, "messages": messages, "tools": tools})["choices"][0]["message"]

    def models(self):
        return sorted(m["id"] for m in self.call("/models")["data"])


def ask_ai(text, cfg, hub, ai, p=Persona()):
    msgs = [{"role": "system", "content": system_prompt(cfg, p)}, {"role": "user", "content": text}]
    for _ in range(6):
        m = ai.chat(msgs, TOOLS)
        calls = [{"id": c["id"], "type": "function", "function": {"name": c["function"]["name"], "arguments": c["function"].get("arguments") or "{}"}}
                 for c in m.get("tool_calls") or []]
        if not calls:
            if m.get("refusal") and not m.get("content"):
                return "Xin lỗi, việc này tôi không làm được."
            return (m.get("content") or "").strip() or "Tôi chưa có câu trả lời."
        # Gửi lại đúng lời gọi tool; bỏ trường riêng của từng hãng (reasoning_content...) mà hãng khác không nhận.
        msgs.append({"role": "assistant", "content": m.get("content"), "tool_calls": calls})
        msgs += [{"role": "tool", "tool_call_id": c["id"], "content": call_tool(c["function"]["name"], c["function"]["arguments"], hub, cfg)}
                 for c in calls]
    return "Việc này nhiều bước quá, bạn nói cụ thể hơn giúp tôi."


def answer(text, hub, ai, p=Persona()):
    if not text.strip():
        return p.fmt(NOT_HEARD)
    try:
        code, cfg = hub("GET", "/config")
        if code != 200:
            return "Trung tâm từ chối trợ lý, cần kiểm tra token trợ lý."
        intent = match(text, cfg)
        if intent:
            return run(intent, hub, p)
    except OSError:
        return "Không kết nối được trung tâm."
    if greeting(text, p):
        return p.greet()
    if ai is None:
        return p.fmt(FIXED_ONLY)
    try:
        return ask_ai(text, cfg, hub, ai, p)
    except urllib.error.HTTPError as e:
        print(f"AI lỗi HTTP {e.code} ({ai.url}, model {ai.model})", flush=True)  # không in thân lỗi: có hãng nhắc lại một phần khoá
        return AI_KEY if e.code in (401, 403) else AI_ERROR
    except OSError:
        return p.fmt(OFFLINE)
    except (ValueError, KeyError, IndexError, TypeError) as e:  # endpoint trả dạng lạ
        print(f"AI trả về không hợp lệ: {e!r}", flush=True)
        return AI_ERROR


def check_ai(stored, draft):
    """Nút "Thử kết nối": ô để trống dùng giá trị đã lưu. Lấy danh sách model; có model thì hỏi thử một câu kèm tool
    (đúng kiểu gọi thật). Kết quả không chứa khoá."""
    s = {**stored, **{k: str(draft[k]).strip() for k in ("url", "api_key", "model") if str(draft.get(k) or "").strip()}}
    if base_url(s.get("url", "")) != base_url(stored.get("url", "")) and not str(draft.get("api_key") or "").strip():
        s["api_key"] = ""  # khoá đã lưu chỉ gửi tới đúng địa chỉ đã lưu cùng nó
    def done(ok, models, *notes):
        return {"ok": ok, "models": models, "message": " ".join(notes)}
    if not s.get("url"):
        return done(False, [], "Nhập địa chỉ API trước.")
    ai = AI(s["url"], s.get("api_key"), s.get("model", ""), timeout=10)
    if problem := url_problem(ai.url):
        return done(False, [], problem)
    models, notes = [], []
    try:
        models = ai.models()
        notes.append(f"Kết nối được, có {len(models)} model.")
    except urllib.error.HTTPError as e:
        if e.code in (401, 403):
            return done(False, [], f"Khoá API bị từ chối (HTTP {e.code}).")
        notes.append(f"Endpoint không trả danh sách model (HTTP {e.code}), hãy gõ tên model.")
    except OSError as e:
        return done(False, [], f"Không kết nối được {ai.url}: {getattr(e, 'reason', e)}")
    except (ValueError, KeyError, TypeError):
        notes.append("Endpoint không trả danh sách model, hãy gõ tên model.")
    if not ai.model:
        return done(True, models, *notes, "Chọn model rồi bấm Thử kết nối lần nữa.")
    t0 = time.perf_counter()
    try:
        ai.chat([{"role": "user", "content": "Trả lời đúng một chữ: OK"}], TOOLS)
    except urllib.error.HTTPError as e:
        why = api_error(e) if e.code >= 400 and e.code not in (401, 403) else ""
        return done(False, models, *notes, f"Model {ai.model} lỗi HTTP {e.code}{why}")
    except (OSError, ValueError, KeyError, IndexError, TypeError) as e:
        return done(False, models, *notes, f"Model {ai.model} chưa trả lời được: {e}")
    return done(True, models, *notes, f"Model {ai.model} trả lời được trong {num(round(time.perf_counter() - t0, 1))} giây.")


# ---------- Âm thanh ----------

class TTS:
    """Đọc thành WAV, cache theo (giọng, câu): câu cảnh báo và câu lặp lại không phải tổng hợp lại.
    Giữ `keep` file dùng gần nhất (câu trả lời của AI hầu như không lặp lại)."""
    keep = 500  # ponytail: xoá theo mtime khi ghi file mới, ~0,5 MB/file → tối đa vài trăm MB

    def __init__(self, cache, synth):
        self.cache, self.synth, self.lock = cache, synth, threading.Lock()
        cache.mkdir(parents=True, exist_ok=True)

    def key(self, text, voice=DEFAULT_VOICE):
        return hashlib.sha1(f"{voice}\n{text}".encode()).hexdigest()[:16]  # cache tách theo giọng

    def url(self, text, voice=DEFAULT_VOICE):
        k = self.key(text, voice)
        p = self.cache / f"{k}.wav"
        with self.lock:
            if p.exists():
                os.utime(p)  # đánh dấu vừa dùng
            else:
                tmp = self.cache / f"{k}.part.wav"
                self.synth(text, tmp, voice)
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
    return lambda text, path, voice: v.save(v.infer(text, voice=voice), str(path))


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
    def __init__(self, hub, ai, stt, tts):
        self.hub, self.ai, self.stt, self.tts = hub, ai, stt, tts
        self.listeners, self.lock = set(), threading.Lock()
        self.persona, self.phrases, self.warming = Persona(), [], None
        self.now = lambda: datetime.now(VN)

    def load_voice(self):
        """Đọc nhóm "voice" (giọng, xưng hô, giờ yên lặng). Trả True khi giọng hay cách gọi đổi (cần tạo lại câu đọc sẵn)."""
        code, s = self.hub("GET", "/settings/voice")
        if code != 200:
            return print(f"không đọc được cấu hình giọng: HTTP {code}", flush=True) or False
        old, self.persona = self.persona, Persona(s)
        return (old.voice, old.owner) != (self.persona.voice, self.persona.owner)

    def load_ai(self):
        """Đọc cấu hình AI (nhóm "ai" của hubd) bằng token trợ lý. Không in khoá."""
        code, s = self.hub("GET", "/settings/ai")
        if code != 200:
            return print(f"không đọc được cấu hình AI: HTTP {code}", flush=True)
        self.ai = AI.from_settings(s)
        print(f"AI: model {self.ai.model} tại {self.ai.url}" if self.ai else "chưa cài AI: chỉ chạy lệnh cố định", flush=True)

    def speak(self, text, voice=None):
        try:
            return self.tts.url(text, voice or self.persona.voice)
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
            if topic == "hubd/settings/ai":  # lưu trên trang Cài đặt; tin không kèm giá trị
                return self.load_ai()
            if topic == "hubd/settings/voice":
                if self.load_voice():  # giọng hay cách gọi đổi: đọc sẵn lại câu cảnh báo, câu cố định (chạy nền)
                    self.warming = threading.Thread(target=self.prerender, daemon=True)
                    self.warming.start()
                return
            a = json.loads(payload)
            text, level = str(a["text"]), str(a.get("level", "info"))
            # Giờ yên lặng: vẫn hiện chữ, không đọc (trừ khẩn); không tổng hợp nên đêm cũng đỡ tốn CPU.
            audio = None if level != "urgent" and self.persona.quiet(self.now()) else self.speak(text)
            self.broadcast({"type": "alert", "level": level, "text": text, "audio": audio})
        except Exception as e:  # paho 1.6 ném lại lỗi và dừng luồng MQTT: một tin hỏng không được làm câm cảnh báo
            print(f"bỏ qua tin {topic}: {e!r}", flush=True)

    def warm(self):
        """Tạo sẵn âm thanh cho mọi câu cảnh báo cố định trong config (đọc được ngay, cả khi mất internet)."""
        while True:
            try:
                self.phrases = self.hub("GET", "/config")[1]["phrases"]
                self.load_ai()
                self.load_voice()
                break
            except (OSError, TypeError, KeyError):
                time.sleep(10)
        self.prerender()

    def prerender(self):
        p = self.persona
        for t in self.phrases + [p.fmt(x) for x in (OFFLINE, FIXED_ONLY, NOT_HEARD)]:  # câu cố định dài, đọc lần đầu mất vài giây
            self.speak(t, p.voice)
        print(f"đã tạo sẵn {len(self.phrases)} câu cảnh báo, giọng {p.voice}", flush=True)


def handler(app):
    class H(BaseHTTPRequestHandler):
        def send(self, code, body, ctype="application/json; charset=utf-8"):
            self.send_response(code)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            n = self.headers.get("Content-Length", "0")
            if not n.isdigit() or int(n) > 8_000_000:
                return self.send(400, "Content-Length không hợp lệ (tối đa 8 MB)".encode(), "text/plain; charset=utf-8")
            body = self.rfile.read(int(n))
            if self.path == "/voice/ask":
                return self.ask(body)
            if self.path == "/voice/brief":
                try:
                    reply = summary(app.hub, app.persona)
                except Exception as e:  # hubd mất kết nối hoặc trả dạng lạ
                    print(f"tóm tắt lỗi: {e!r}", flush=True)
                    reply = "Không lấy được số liệu từ trung tâm."
                return self.json({"reply": reply, "audio": app.speak(reply)})
            if self.path == "/voice/preview":  # nút Nghe thử: giá trị đang nhập, không đổi cấu hình đang chạy
                try:
                    draft = json.loads(body or b"{}")
                except ValueError:
                    draft = {}
                p = Persona(draft if isinstance(draft, dict) else {})
                return self.json({"reply": p.greet(), "audio": app.speak(p.greet(), p.voice)})
            if self.path == "/voice/ai/check":
                try:
                    draft = json.loads(body)
                    code, stored = app.hub("GET", "/settings/ai")
                    return self.json(check_ai(stored if code == 200 else {}, draft if isinstance(draft, dict) else {}))
                except (ValueError, OSError) as e:
                    return self.json({"ok": False, "models": [], "message": f"Lỗi: {e}"})
            self.send_error(404)

        def json(self, obj):
            self.send(200, json.dumps(obj, ensure_ascii=False).encode())

        def ask(self, body):
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
                reply = answer(heard, app.hub, app.ai, app.persona)
            except Exception as e:
                print(f"lỗi khi trả lời {heard!r}: {e!r}", flush=True)
                reply = "Có lỗi, bạn thử lại sau."
            t2 = time.perf_counter()
            audio = app.speak(reply)
            ms = lambda a, b: f"{(b - a) * 1000:.0f} ms"
            print(f"nghe: {heard!r} → {reply!r} (STT {ms(t0, t1)}, xử lý {ms(t1, t2)}, TTS {ms(t2, time.perf_counter())})", flush=True)
            self.json({"heard": heard, "reply": reply, "audio": audio})

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
    app = App(hub, None, sherpa_stt(), TTS(Path(os.environ.get("VOICED_CACHE", "/cache")), vieneu_synth()))
    threading.Thread(target=app.warm, daemon=True).start()

    c = mqtt.Client("voiced")
    c.username_pw_set(os.environ.get("MQTT_USER"), os.environ.get("MQTT_PASS"))
    def on_connect(c, u, f, rc):
        c.subscribe([("hubd/alert", 1), ("hubd/alert/ack", 1), ("hubd/settings/ai", 1), ("hubd/settings/voice", 1)])
        app.on_mqtt("hubd/settings/ai", "changed")  # nối lại broker: có thể đã lỡ tin đổi cấu hình
        app.on_mqtt("hubd/settings/voice", "changed")
    c.on_connect = on_connect
    c.on_message = lambda c, u, m: app.on_mqtt(m.topic, m.payload)
    c.connect_async(os.environ.get("MQTT_HOST", "mosquitto"), 1883)
    c.loop_start()  # tự nối lại khi broker khởi động lại

    srv = ThreadingHTTPServer(("", 8090), handler(app))
    srv.daemon_threads = True
    print("voiced nghe tại :8090", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
