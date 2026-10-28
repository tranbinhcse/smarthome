"""voiced — trợ lý giọng nói iNIX Home (thiết kế mục 15.2).

Âm thanh WAV (nút nói trên iPad; sau này loa vệ tinh ESP32-S3) → STT cục bộ (sherpa-onnx Zipformer tiếng Việt)
→ lệnh cố định khớp cục bộ theo tên thiết bị/kịch bản/số đo trong config (chạy cả khi mất internet)
→ câu tự do gửi AI qua API chat completions tương thích OpenAI (tool calling; URL, khoá, model nhập ở trang Cài đặt,
lưu trong hubd nhóm "ai") → mọi thao tác đi qua API trợ lý của hubd bằng token riêng; hubd tự chặn thiết bị/kịch bản
cần PIN → câu trả lời đọc bằng VieNeu-TTS (giọng Nam/Nữ chọn ở Cài đặt), cache theo giọng + câu.
Cảnh báo nghe từ MQTT hubd/alert, đẩy tới loa (iPad) qua SSE; câu cảnh báo cố định tạo sẵn âm thanh lúc khởi động.
Cấu hình đọc qua GET /api/assistant/settings/{ai,voice}, đọc lại khi MQTT hubd/settings/<nhóm> = changed:
nhóm "ai" (URL, khoá, model), nhóm "voice" (giọng, cách gọi chủ nhà, tên trợ lý, tốc độ đọc, giờ yên lặng, chỉ nghe
người nhà, độ khắt khe khi nhận giọng).
Giọng người nhà: mỗi người đọc vài câu ở Cài đặt; câu nói sau đó được nhận ra là của ai (CAM++, Voices) để xưng hô đúng
người, và khi bật "chỉ nghe người nhà" thì bỏ qua giọng lạ (TV, khách). Vân giọng chỉ lưu trên hub (VOICED_DATA).

HTTP (chỉ trong mạng docker; dashboard đi qua hubd /voice/*, không mở cổng ra LAN):
  POST /voice/ask       body audio/wav (PCM 16-bit, tần số bất kỳ) hoặc JSON {"text": "..."}
                        → {"heard", "reply", "audio": ["/voice/tts/<key>.wav", ...]} (từng câu, phát lần lượt)
  POST /voice/ask?listen=1  (nghe gọi tên: iPad/loa vệ tinh gửi từng đoạn có tiếng) câu không mở đầu bằng tên trợ lý
                        → {"ignored": true}, không trả lời, không ghi log; chỉ gọi tên → lời chào + "awaiting": true
  POST /voice/brief     tóm tắt tình hình (thời tiết, không khí, điện/pin, cảnh báo) → {"reply", "audio"}
  POST /voice/ai/check  JSON {url, api_key, model} (ô trống = giá trị đã lưu) → {"ok", "models", "message"}
  POST /voice/preview   JSON {voice, owner, name} (đang nhập, chưa lưu) → lời chào thử {"reply", "audio"}
  GET  /voice/people    → [{"you", "samples"}] (người đã đăng ký giọng; không trả vân giọng)
  POST /voice/people?you=<cách gọi>  body audio/wav (một câu đọc, ≥ 1 giây) → {"you", "samples", "score"}
  POST /voice/people/check  body audio/wav → {"who" | null, "score"} (nút Thử nhận giọng)
  DELETE /voice/people?you=<cách gọi>  → 204, 404 nếu không có
  GET  /voice/tts/<key>.wav
  GET  /voice/events    SSE: {"type": "alert", "level", "text", "audio": [...] | null} | {"type": "ack"}
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
from urllib.parse import parse_qs, quote, urlencode, urlsplit

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
STRANGER = "Xin lỗi, tôi chỉ làm theo giọng người nhà đã đăng ký."
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


# ---------- Hội thoại của một màn hình: nhớ vài lượt cho AI, và lệnh có khoá đang chờ xác nhận ----------

class Conv:
    """Mỗi màn hình một hội thoại (mã gửi qua header X-Voice-Session). Vài lượt gần nhất gửi kèm cho AI để hiểu câu nối tiếp
    ("tắt nó đi"). Lệnh tới thiết bị / ngữ cảnh có khoá bị hubd chặn thì chờ xác nhận WAIT giây: màn hình hiện hộp xác nhận,
    người nói "có" thì trả lệnh cho chính màn hình đó làm bằng phiên PIN của nó; voiced không bao giờ tự làm lệnh có khoá."""
    TURNS, IDLE, WAIT = 6, 300, 60

    def __init__(self, now=time.monotonic):
        self.now, self.turns, self.at, self._pending, self.out = now, [], now(), None, {}
        self._who = None   # (cách gọi, lúc nhận ra): người vừa nói trong hội thoại này

    @property
    def who(self):
        """Người vừa được nhận ra giọng trong hội thoại này, nếu chưa quá WAIT giây."""
        return self._who[0] if self._who and self.now() - self._who[1] <= self.WAIT else None

    @who.setter
    def who(self, you):
        self._who = (you, self.now()) if you else None

    def history(self):
        return [m for u, a in self.turns for m in ({"role": "user", "content": u}, {"role": "assistant", "content": a})]

    def add(self, user, reply):
        self.turns, self.at = (self.turns + [(user, reply)])[-self.TURNS:], self.now()

    def ask(self, act):
        """act: {"ids": [thiết bị], "to"} hoặc {"scene"}, kèm "label" ("tắt CB Tổng Bếp") để màn hình hỏi lại."""
        self._pending, self.out = (act, self.now()), {"confirm": act}

    @property
    def pending(self):
        return self._pending[0] if self._pending and self.now() - self._pending[1] <= self.WAIT else None

    def take(self):
        """Phần gửi kèm câu trả lời cho màn hình ({"confirm": ...} hay {"execute": ...}), lấy một lần."""
        out, self.out = self.out, {}
        return out


# ---------- Giọng người nhà ----------

class Voices:
    """Mỗi người nhà (theo cách gọi, "anh Bình") một vân giọng = tổng các vector mẫu đã chuẩn hoá; lưu ở path trên hub,
    API không trả vân giọng. who() nhận người đang nói: độ giống (cosin) cao nhất và ≥ threshold.
    Đo 28/10 trên hub, 25 giọng VieNeu, CAM++ (mục 15.2.4 thiết kế): câu lệnh 1–2 giây ở ngưỡng 0,55 nhận đúng 50/50, nhận
    nhầm 19/1200; "có" (dưới 1 giây) chỉ nhận đúng 1/25, nên câu ngắn trong hội thoại của người vừa nhận ra chỉ cần giống
    người đó tới threshold − RELAX (0,35: đúng 22/25, nhầm 38/600)."""
    MIN, SHORT, RELAX = 1.0, 1.0, 0.2   # giây: mẫu đăng ký ngắn nhất; câu ngắn; mức nới cho câu ngắn

    def __init__(self, path, embed, threshold=0.55):
        self.path, self.embed, self.threshold, self.lock = Path(path), embed, threshold, threading.Lock()
        try:
            self.db = {k: {"sum": np.array(v["sum"], dtype=float), "n": int(v["n"])} for k, v in json.loads(self.path.read_text()).items()}
        except FileNotFoundError:
            self.db = {}

    def people(self):
        with self.lock:
            return [{"you": k, "samples": v["n"]} for k, v in sorted(self.db.items())]

    def vec(self, rate, samples):
        e = np.asarray(self.embed(rate, samples), dtype=float)
        return e / (np.linalg.norm(e) or 1.0)

    def scores(self, e):
        with self.lock:
            return {k: round(float(e @ v["sum"] / np.linalg.norm(v["sum"])), 3) for k, v in self.db.items()}

    def add(self, you, rate, samples):
        e = self.vec(rate, samples)
        score = self.scores(e).get(you)
        with self.lock:
            cur = self.db.get(you, {"sum": np.zeros_like(e), "n": 0})
            self.db[you] = {"sum": cur["sum"] + e, "n": cur["n"] + 1}
            self.save()
            return {"you": you, "samples": self.db[you]["n"], "score": score}

    def remove(self, you):
        with self.lock:
            if self.db.pop(you, None) is None:
                return False
            self.save()
            return True

    def save(self):  # gọi khi đang giữ lock
        tmp = self.path.with_suffix(".tmp")
        tmp.write_text(json.dumps({k: {"sum": v["sum"].tolist(), "n": v["n"]} for k, v in self.db.items()}))
        tmp.replace(self.path)

    def who(self, rate, samples, talking=None):
        """(người, độ giống) — người là None nếu không ai đủ giống. talking: người vừa nói trong hội thoại này (Conv.who)."""
        s = self.scores(self.vec(rate, samples))
        best = max(s, key=s.get, default=None)
        if best is not None and s[best] >= self.threshold:
            return best, s[best]
        if talking in s and len(samples) < self.SHORT * rate and s[talking] >= self.threshold - self.RELAX:
            return talking, s[talking]
        return None, s.get(best)


YES = ("có", "đồng ý", "xác nhận", "ừ", "ok", "oke", "đúng rồi", "làm đi", "chắc chắn", "được")
NO = ("không", "thôi", "huỷ", "hủy", "đừng", "khỏi")


def yes_no(text):
    """Trả lời có/không cho lệnh đang chờ: True / False, None nếu không phải (câu dài, hay có cả hai như "có ai không").
    Giữ dấu: bỏ dấu thì "đúng" và "đừng" trùng nhau."""
    ws = re.findall(r"\w+", unicodedata.normalize("NFC", text.lower()))
    if not ws or len(ws) > 5:
        return None
    t = f" {' '.join(ws)} "
    y, n = any(f" {w} " in t for w in YES), any(f" {w} " in t for w in NO)
    return y if y != n else None


# ---------- Lệnh cố định (cục bộ) ----------

def plain(s):
    """Chữ thường, bỏ dấu, đ→d. STT hay sai dấu ("RA KHỎI NHA") nên so tên trên chữ không dấu."""
    s = unicodedata.normalize("NFD", s.lower().replace("đ", "d"))
    return "".join(c for c in s if unicodedata.category(c) != "Mn")


def words(s):
    return re.findall(r"\w+", plain(s))


ON, OFF = {"bat", "mo"}, {"tat"}
QUESTION = {"nhieu", "may", "khong", "chua", "sao", "nao"}
# Từ khung câu hỏi ("có … không", "đã … chưa", "còn/là bao nhiêu"): không đủ để đoán tên số đo ("có bao nhiêu đèn" ≠ "Có người").
FRAME = QUESTION | {"co", "da", "dang", "con", "la", "bao"}


def grouped(cfg):
    """Thiết bị theo tên gọi. Thiết bị nhiều nút đã tách kênh (cùng `group`, vd "Nút 1", "Nút 2" của "Đèn phòng lạnh"):
    kênh gọi là "<nhóm> <kênh>", nhóm chỉ một kênh gọi bằng tên nhóm, và thêm một mục cho cả nhóm (id "group:<nhóm>").
    Mỗi mục có `members` = các thiết bị thật nhận lệnh. Gọi lại trên cfg đã gom thì trả nguyên cfg đó."""
    if any("members" in d for d in cfg["devices"]):
        return cfg
    by = {}
    for d in cfg["devices"]:
        if (d.get("group") or "").strip():
            by.setdefault(d["group"].strip(), []).append(d)
    devs = []
    for d in cfg["devices"]:
        g, name = (d.get("group") or "").strip(), d["name"].strip()
        devs.append({**d, "name": (g if len(by[g]) == 1 else f"{g} {name}") if g else name, "members": [d]})
    devs += [{"id": "group:" + g, "name": g, "zone": ms[0].get("zone"), "members": ms} for g, ms in by.items() if len(ms) > 1]
    return {**cfg, "devices": devs}



# Từ khoá ưu tiên cho nhận giọng (hotwords của sherpa-onnx): mô hình hay nghe nhầm đúng các từ này ("sân sau" → "xanh seo",
# "tắt" → "thắp"). Đo 27/10 trên 120 câu lệnh: số lỗi giảm khoảng một nửa (mục 15.2.1 thiết kế).
COMMAND_WORDS = ["BẬT", "TẮT", "BẬT ĐÈN", "TẮT ĐÈN", "TẮT BÁO ĐỘNG", "ĐÈN", "CẦU DAO", "Ổ CẮM"]


def hotwords(cfg):
    """Từ khoá cho create_stream(hotwords=...), ngăn bằng "/": tên phòng, thiết bị (cả tên nhóm), ngữ cảnh và động từ lệnh,
    viết hoa như từ vựng của mô hình. Bỏ tên có chữ số hay ký tự lạ: mô hình viết số bằng chữ ("Nút 1" nghe là "nút một")."""
    cfg = grouped(cfg)
    out = []
    for n in [x.get("name", "") for k in ("zones", "devices", "scenes") for x in cfg.get(k, [])] + COMMAND_WORDS:
        w = " ".join(n.upper().split())
        if w and all(c.isalpha() or c == " " for c in w) and w not in out:
            out.append(w)
    return "/".join(out)


def named(items, ws):
    """Mục có cả tên nằm trọn trong câu; nhiều mục khớp lấy tên dài nhất, hoà thì None (không đoán)."""
    hits = [(len(set(words(x["name"]))), x) for x in items if words(x["name"]) and set(words(x["name"])) <= ws]
    top = [x for n, x in hits if n == max(n for n, _ in hits)]
    return top[0] if len(top) == 1 else None


def measured(cfg, ws):
    """Số đo được hỏi: tên phải có từ đầu (pin, điện, công...) trong câu, từ đầu là từ khung câu hỏi (có, đã...) thì phải
    nói đủ tên; điểm = (từ của tên, từ của thiết bị/phòng)."""
    ctx = {d["id"]: d["name"] for d in cfg["devices"]} | {z["id"]: z["name"] for z in cfg["zones"]}
    scored = []
    for e in cfg["entities"]:
        name = words(e["name"])
        if name and name[0] in ws and (name[0] not in FRAME or set(name) <= ws):
            scored.append(((len(set(name) & ws), len(set(words(ctx.get(e.get("device"), "") + " " + ctx.get(e.get("zone"), ""))) & ws)), e))
    top = [e for s, e in scored if s == max(s for s, _ in scored)]
    return top[0] if len(top) == 1 else None


def match(text, cfg):
    """Hiểu câu bằng luật cố định. Trả (việc, đối tượng, on/off) hoặc None để AI xử lý."""
    cfg = grouped(cfg)
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


def wake(text, p):
    """Câu gọi trợ lý: tên (p.name, chưa đặt thì "trợ lý") ở đầu câu, cho một tiếng đệm phía trước ("à Mây ơi").
    Khớp đúng dấu, hoặc sai dấu nhưng ngay sau là "ơi" (STT hay sai dấu; "máy lạnh" không gọi nhầm "Mây").
    Trả phần lệnh sau tên ("" nếu chỉ gọi tên), không gọi → None."""
    nfc = lambda x: unicodedata.normalize("NFC", x)
    toks, name = re.findall(r"\w+", nfc(text)), re.findall(r"\w+", nfc((p.name or "trợ lý").lower()))
    for i in (0, 1):
        got, after = [t.lower() for t in toks[i:i + len(name)]], toks[i + len(name):]
        oi = bool(after) and plain(after[0]) == "oi"
        if len(got) == len(name) and (got == name or (oi and list(map(plain, got)) == list(map(plain, name)))):
            return " ".join(after[1:] if oi else after)
    return None


def greeting(text, p):
    """Chỉ gọi trợ lý ("chào", "alo", "Mây ơi"): trả lời lời chào."""
    ws = set(words(text))
    return bool(ws) and ws <= GREET | set(words(p.name))


def num(v):
    try:
        return f"{round(float(v), 2):.2f}".rstrip("0").rstrip(".").replace(".", ",")
    except ValueError:
        return v


def switch(x, to, hub, p=None, conv=None):
    """Bật/tắt một thiết bị, một kênh hay cả nhóm (từng kênh một). Kênh có khoá bị hubd chặn (403): hỏi lại người và chờ
    xác nhận trong conv. Trả (câu trả lời, mọi kênh đã nhận lệnh)."""
    ms = x.get("members") or [x]
    done, pin, bad = [], [], []
    for d in ms:
        code, _ = hub("POST", "/cmd", {"device": d["id"], "to": to})
        (done if code == 204 else pin if code == 403 else bad).append(d)
    name, verb = x["name"].strip(), "bật" if to == "on" else "tắt"
    of = (lambda ds: f"{', '.join(d['name'].strip() for d in ds)} của {name}") if sum(map(bool, (done, pin, bad))) > 1 else (lambda ds: name)
    if pin and conv is not None:
        conv.ask({"ids": [d["id"] for d in pin], "to": to, "label": f"{verb} {of(pin)}"})
    you = (p or Persona()).you
    out = ([f"Đã {verb} {of(done)}."] if done else []) + \
          ([f"{of(pin)} được bảo vệ, {you} xác nhận {verb} không?"] if pin else []) + ([f"Không gửi được lệnh tới {of(bad)}."] if bad else [])
    return " ".join(out), not pin and not bad


def status(x, v):
    """Trạng thái một thiết bị/kênh/nhóm; nhóm có kênh khác nhau thì kể từng kênh."""
    def one(d):
        if v.get("online:" + d["id"]) == "offline":
            return "đang mất kết nối"
        s = v.get("dev:" + d["id"])
        return "chưa có trạng thái" if s is None else "đang bật" if ON_RE.match(s) else "đang tắt"
    ss = [(d["name"].strip(), one(d)) for d in x.get("members") or [x]]
    if len({s for _, s in ss}) == 1:
        return f"{x['name'].strip()} {ss[0][1]}."
    return f"{x['name'].strip()}: " + ", ".join(f"{n} {s.removeprefix('đang ')}" for n, s in ss) + "."


def run(intent, hub, p=Persona(), conv=None):
    return summary(hub, p) if intent[0] == "brief" else p.say(reply(intent, hub, p, conv))


def reply(intent, hub, p=Persona(), conv=None):
    kind, x, to = intent
    name = x and x["name"].strip()
    if kind == "set":
        return switch(x, to, hub, p, conv)[0]
    if kind == "scene":
        code, msg = hub("POST", "/scene/" + quote(x["id"], safe=""))
        if code == 403 and conv is not None:
            conv.ask({"scene": x["id"], "label": f"chạy {name}"})
        return {204: f"Đã chạy {name}.", 403: f"{name} được bảo vệ, {p.you} xác nhận chạy không?"}.get(code, f"{name}: {msg}.")
    if kind == "ack":
        hub("POST", "/alarm/ack")
        return "Đã xác nhận cảnh báo."
    v = hub("GET", "/state")[1]["values"]
    if kind == "ask_device":
        return status(x, v)
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
    cfg = grouped(cfg)
    zone = {z["id"]: z["name"] for z in cfg["zones"]}
    def lock(x):
        n = sum(m["id"] in cfg["locked"] for m in x.get("members") or [x])
        return "" if not n else " [cần PIN]" if n == len(x.get("members") or [x]) else " [có kênh cần PIN]"
    group = lambda d: f" (cả nhóm: {', '.join(m['name'].strip() for m in d['members'])})" if len(d.get("members") or []) > 1 else ""
    dev = {d["id"]: d["name"].strip() for d in cfg["devices"]}
    return "\n".join([
        f"Bạn là {p.name + ', ' if p.name else ''}trợ lý giọng nói của iNIX Home, nhà 3 tầng kiêm quán cafe.",
        *([f'Người nói chuyện với bạn là chủ nhà: gọi họ là "{p.owner}", tự xưng "em", nói lễ phép (dạ, ạ).'] if p.owner else []),
        "Câu của người dùng do nhận dạng giọng nói chép ra nên có thể sai dấu hoặc sai chữ; hãy hiểu theo tên thiết bị gần nhất.",
        "Câu trả lời sẽ được đọc thành tiếng: tiếng Việt, một hai câu ngắn, không markdown, không liệt kê dài.",
        "Chỉ xem và điều khiển nhà bằng các tool. Tool báo cần PIN thì bảo người dùng bấm trên màn hình, không thử cách khác.",
        "Không bịa số liệu; chưa có dữ liệu thì nói chưa có.",
        f"Bây giờ là {datetime.now(VN):%H:%M ngày %d/%m/%Y} (giờ Việt Nam).",
        "", "Thiết bị (id: tên, phòng). Mục id group:<nhóm> là cả nhóm kênh của một thiết bị nhiều nút: người dùng nói tên nhóm thì dùng id đó:",
        *(f"- {d['id']}: {d['name'].strip()}, {zone.get(d.get('zone'), 'chưa gán phòng')}{group(d)}{lock(d)}" for d in cfg["devices"]),
        "", "Kịch bản (id: tên):",
        *(f"- {s['id']}: {s['name']}{lock(s)}" for s in cfg["scenes"]),
        "", "Số đo (id: tên, đơn vị, thuộc):",
        *(f"- {e['id']}: {e['name']}, {e.get('unit', '')}, {dev.get(e.get('device')) or zone.get(e.get('zone'), '')}" for e in cfg["entities"]),
    ])


def state_text(cfg, st):
    cfg = grouped(cfg)
    v = st["values"]
    def dev(d):
        if v.get("online:" + d["id"]) == "offline":
            return "mất kết nối"
        s = v.get("dev:" + d["id"])
        return "chưa rõ" if s is None else ("bật" if ON_RE.match(s) else "tắt")
    return "\n".join(["Thiết bị:", *(f"- {d['name'].strip()} ({d['id']}): {dev(d)}" for d in cfg["devices"] if len(d["members"]) == 1),
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


LOCKED_TOOL = ("Thiết bị / kịch bản này được bảo vệ: màn hình đã hiện hộp xác nhận. Hỏi lại người dùng một câu ngắn có muốn "
               "{act} không. Họ đồng ý thì hệ thống tự làm, không gọi lại tool, không thử cách khác.")


def call_tool(name, args, hub, cfg, conv=None):
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
            x = next((d for d in grouped(cfg)["devices"] if d["id"] == a["device"]), None)
            if x is None:
                return f"LỖI: không có thiết bị {a['device']}"
            text, ok = switch(x, a["to"], hub, None, conv)
            if conv is not None and conv.out.get("confirm"):
                return LOCKED_TOOL.format(act=conv.out["confirm"]["label"])
            return text if ok else f"LỖI: {text} Không thử cách khác."
        if name == "run_scene":
            code, msg = hub("POST", "/scene/" + quote(a["scene"], safe=""))
            sc = next((s for s in cfg.get("scenes", []) if s["id"] == a["scene"]), None)
            if code == 403 and conv is not None and sc:
                conv.ask({"scene": sc["id"], "label": f"chạy {sc['name']}"})
                return LOCKED_TOOL.format(act=f"chạy {sc['name']}")
            return verdict(code, msg)
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


def ask_ai(text, cfg, hub, ai, p=Persona(), conv=None):
    msgs = [{"role": "system", "content": system_prompt(cfg, p)}] + (conv.history() if conv else []) + [{"role": "user", "content": text}]
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
        msgs += [{"role": "tool", "tool_call_id": c["id"], "content": call_tool(c["function"]["name"], c["function"]["arguments"], hub, cfg, conv)}
                 for c in calls]
    return "Việc này nhiều bước quá, bạn nói cụ thể hơn giúp tôi."


def answer(text, hub, ai, p=Persona(), conv=None):
    """Câu trả lời cho một câu nói. conv: hội thoại của màn hình (lịch sử cho AI, lệnh có khoá chờ xác nhận); phần gửi
    kèm cho màn hình (hộp xác nhận, lệnh đã được xác nhận) nằm ở conv.take()."""
    conv = conv if conv is not None else Conv()
    conv.out = {}
    out = _answer(text, hub, ai, p, conv)
    if text.strip():
        conv.add(text, out)
    return out


def _answer(text, hub, ai, p, conv):
    if not text.strip():
        return p.fmt(NOT_HEARD)
    act = conv.pending
    if act:
        conv._pending = None   # chỉ một lượt để trả lời có/không; nói sang chuyện khác thì bỏ lệnh đang chờ
        yn = yes_no(text)
        if yn is True:
            conv.out = {"execute": act}
            return p.say(f"Đã xác nhận, {act['label']}.")
        if yn is False:
            return p.say("Đã huỷ.")
    try:
        code, cfg = hub("GET", "/config")
        if code != 200:
            return "Trung tâm từ chối trợ lý, cần kiểm tra token trợ lý."
        intent = match(text, cfg)
        if intent:
            return run(intent, hub, p, conv)
    except OSError:
        return "Không kết nối được trung tâm."
    if greeting(text, p):
        return p.greet()
    if ai is None:
        return p.fmt(FIXED_ONLY)
    try:
        return ask_ai(text, cfg, hub, ai, p, conv)
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

def sentences(text):
    """Cắt câu trả lời để phát câu đầu trong lúc tổng hợp câu sau; mẩu ngắn (dưới 25 ký tự) gộp vào câu kế."""
    out = []
    for part in re.split(r"(?<=[.!?;])\s+", text.strip()):
        if out and len(out[-1]) < 25:
            out[-1] += " " + part
        elif part:
            out.append(part)
    return out


class TTS:
    """Đọc thành WAV, cache theo (giọng, câu): câu cảnh báo và câu lặp lại không phải tổng hợp lại.
    Giữ `keep` file dùng gần nhất (câu trả lời của AI hầu như không lặp lại)."""
    keep = 500  # ponytail: xoá theo mtime khi ghi file mới, ~0,5 MB/file → tối đa vài trăm MB

    def __init__(self, cache, synth):
        self.cache, self.synth, self.lock, self.pending = cache, synth, threading.Lock(), {}
        cache.mkdir(parents=True, exist_ok=True)

    def key(self, text, voice=DEFAULT_VOICE):
        return hashlib.sha1(f"{voice}\n{text}".encode()).hexdigest()[:16]  # cache tách theo giọng

    def later(self, text, voice):
        """Ghi nhận câu sẽ cần (tổng hợp khi make). Trả khoá."""
        k = self.key(text, voice)
        with self.lock:
            self.pending[k] = (text, voice)
            while len(self.pending) > 200:  # câu chờ mà không ai xin (trình phát đã đóng)
                self.pending.pop(next(iter(self.pending)))
        return k

    def make(self, k):
        """Tệp WAV của khoá k, tổng hợp nếu chưa có. Khoá chưa từng ghi nhận → None."""
        p = self.cache / f"{k}.wav"
        if p.exists():  # có sẵn: trả ngay, không chờ máy TTS đang đọc câu khác
            os.utime(p)  # đánh dấu vừa dùng
            return p
        with self.lock:
            if p.exists():  # vừa được luồng khác tổng hợp xong
                return p
            if k not in self.pending:
                return None
            text, voice = self.pending[k]
            tmp = self.cache / f"{k}.part.wav"
            self.synth(text, tmp, voice)
            tmp.replace(p)
            self.pending.pop(k, None)
            for old in sorted(self.cache.glob("*.wav"), key=lambda f: f.stat().st_mtime_ns)[:-self.keep]:
                old.unlink(missing_ok=True)
        return p

    def url(self, text, voice=DEFAULT_VOICE):
        k = self.later(text, voice)
        self.make(k)
        return f"/voice/tts/{k}.wav"

    def urls(self, text, voice=DEFAULT_VOICE):
        """URL từng câu: câu đầu tổng hợp ngay, câu sau tổng hợp nền theo thứ tự (hoặc khi trình phát xin tới trước)."""
        first, *rest = sentences(text) or [text]
        keys = [self.later(t, voice) for t in rest]
        out = [self.url(first, voice)] + [f"/voice/tts/{k}.wav" for k in keys]
        if keys:
            threading.Thread(target=lambda: [self.make(k) for k in keys], daemon=True).start()
        return out


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
    # gipformer 1.5 (68M, MIT, fetch_models.py). modified_beam_search để dùng từ khoá ưu tiên theo từng câu; mức 3,0 tốt nhất
    # trong các mức đã đo (1,5 / 2,0 / 3,0), chỉnh bằng VOICED_HOTWORDS_SCORE.
    r = sherpa_onnx.OfflineRecognizer.from_transducer(
        encoder=str(d / "encoder.int8.onnx"), decoder=str(d / "decoder.onnx"), joiner=str(d / "joiner.int8.onnx"),
        tokens=str(d / "tokens.txt"), num_threads=int(os.environ.get("VOICED_THREADS", "4")),
        decoding_method="modified_beam_search", hotwords_score=float(os.environ.get("VOICED_HOTWORDS_SCORE", "3.0")),
        modeling_unit="bpe", bpe_vocab=str(d / "bpe.vocab"))
    lock = threading.Lock()
    def stt(rate, samples, hot=""):
        with lock:
            s = r.create_stream(hotwords=hot) if hot else r.create_stream()
            s.accept_waveform(rate, samples)  # sherpa tự đổi tần số về 16 kHz
            r.decode_stream(s)
            return s.result.text
    return stt


def sherpa_speaker():
    import sherpa_onnx
    # CAM++ (3D-Speaker, Apache-2.0, fetch_models.py): vector 192 chiều; đo trên hub 22 ms một câu, thêm 83 MB RAM.
    ex = sherpa_onnx.SpeakerEmbeddingExtractor(sherpa_onnx.SpeakerEmbeddingExtractorConfig(
        model=str(MODELS / "speaker" / "campplus.onnx"), num_threads=int(os.environ.get("VOICED_THREADS", "4"))))
    lock = threading.Lock()
    def embed(rate, samples):
        with lock:
            s = ex.create_stream()
            s.accept_waveform(rate, samples)  # sherpa tự đổi tần số về 16 kHz
            s.input_finished()
            return ex.compute(s)
    return embed


def read_wav(b):
    with wave.open(io.BytesIO(b)) as w:
        if w.getsampwidth() != 2 or not 8000 <= w.getframerate() <= 192000:
            raise ValueError("cần WAV PCM 16-bit, 8–192 kHz")
        a = np.frombuffer(w.readframes(w.getnframes()), dtype="<i2").astype(np.float32) / 32768
        return w.getframerate(), a.reshape(-1, w.getnchannels()).mean(axis=1)


# ---------- Dịch vụ ----------

class App:
    def __init__(self, hub, ai, stt, tts, voices=None):
        self.hub, self.ai, self.stt, self.tts, self.voices = hub, ai, stt, tts, voices
        self.voice_cfg, self.only_people = {}, False
        self.listeners, self.lock = set(), threading.Lock()
        self.persona, self.phrases, self.warming = Persona(), [], None
        self.now = lambda: datetime.now(VN)
        self.hot_words, self.hot_at = "", -1e9
        self.convs, self.clock = {}, time.monotonic

    def conv(self, sid):
        """Hội thoại của một màn hình; im lặng quá Conv.IDLE thì bắt đầu lại. Giữ tối đa 50 màn hình (bỏ cái cũ nhất)."""
        sid = (sid or "-")[:64]
        with self.lock:
            c = self.convs.get(sid)
            if c is None or self.clock() - c.at > Conv.IDLE:
                c = self.convs[sid] = Conv(now=lambda: self.clock())
                while len(self.convs) > 50:
                    del self.convs[min(self.convs, key=lambda k: self.convs[k].at)]
            return c

    def hot(self):
        """Từ khoá theo cấu hình hub, giữ 60 giây: đổi tên phòng trên dashboard thì chậm nhất một phút sau có hiệu lực."""
        if time.monotonic() - self.hot_at > 60:
            self.hot_at = time.monotonic()
            try:
                code, cfg = self.hub("GET", "/config")
                if code == 200:
                    self.hot_words = hotwords(cfg)
            except OSError:
                pass   # mất hub: dùng từ khoá cũ, vẫn nhận giọng được
        return self.hot_words

    def load_voice(self):
        """Đọc nhóm "voice" (giọng, xưng hô, giờ yên lặng). Trả True khi giọng hay cách gọi đổi (cần tạo lại câu đọc sẵn)."""
        code, s = self.hub("GET", "/settings/voice")
        if code != 200:
            return print(f"không đọc được cấu hình giọng: HTTP {code}", flush=True) or False
        old, self.persona, self.voice_cfg = self.persona, Persona(s), s
        self.only_people = s.get("only_people") is True
        if self.voices:
            try:
                self.voices.threshold = min(0.9, max(0.2, float(s.get("speaker_threshold") or 0.55)))
            except (TypeError, ValueError):
                self.voices.threshold = 0.55
        return (old.voice, old.owner) != (self.persona.voice, self.persona.owner)

    def persona_for(self, who):
        """Xưng hô theo người đang nói. Đã có người đăng ký giọng mà không nhận ra (giọng lạ) thì gọi "bạn", không gọi nhầm
        tên chủ nhà; chưa ai đăng ký thì như cũ (cách gọi chủ nhà trong Cài đặt)."""
        if who is None and not (self.voices and self.voices.people()):
            return self.persona
        return Persona({**self.voice_cfg, "owner": who or ""})

    def load_ai(self):
        """Đọc cấu hình AI (nhóm "ai" của hubd) bằng token trợ lý. Không in khoá."""
        code, s = self.hub("GET", "/settings/ai")
        if code != 200:
            return print(f"không đọc được cấu hình AI: HTTP {code}", flush=True)
        self.ai = AI.from_settings(s)
        print(f"AI: model {self.ai.model} tại {self.ai.url}" if self.ai else "chưa cài AI: chỉ chạy lệnh cố định", flush=True)

    def speak(self, text, voice=None):
        """Danh sách URL âm thanh theo câu (dashboard phát lần lượt), None nếu TTS lỗi."""
        try:
            return self.tts.urls(text, voice or self.persona.voice)
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
            u = urlsplit(self.path)
            if u.path == "/voice/ask":
                return self.ask(body, parse_qs(u.query).get("listen") == ["1"])
            if u.path == "/voice/people":
                you = self.you()
                if not app.voices or you is None:
                    return self.send(400, "cần ?you=<cách gọi>, 1–40 ký tự".encode(), "text/plain; charset=utf-8")
                try:
                    rate, a = read_wav(body)
                except Exception:
                    return self.send(400, "cần WAV PCM 16-bit".encode(), "text/plain; charset=utf-8")
                if len(a) < Voices.MIN * rate:
                    return self.send(400, "mẫu quá ngắn: đọc trọn câu, ít nhất 1 giây".encode(), "text/plain; charset=utf-8")
                return self.json(app.voices.add(you, rate, a))
            if self.path == "/voice/people/check":
                try:
                    rate, a = read_wav(body)
                except Exception:
                    return self.send(400, "cần WAV PCM 16-bit".encode(), "text/plain; charset=utf-8")
                who, score = app.voices.who(rate, a) if app.voices else (None, None)
                return self.json({"who": who, "score": score})
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

        def you(self):
            v = (parse_qs(urlsplit(self.path).query).get("you") or [""])[0].strip()
            return v if 0 < len(v) <= 40 and v.isprintable() else None

        def do_DELETE(self):
            u = urlsplit(self.path)
            if u.path != "/voice/people" or not app.voices:
                return self.send_error(404)
            you = self.you()
            if you is None or not app.voices.remove(you):
                return self.send_error(404)
            self.send_response(204)
            self.end_headers()

        def json(self, obj):
            self.send(200, json.dumps(obj, ensure_ascii=False).encode())

        def ask(self, body, listen=False):
            t0, wav = time.perf_counter(), None
            try:
                if self.headers.get("Content-Type", "").startswith("application/json"):
                    heard = str(json.loads(body)["text"])
                else:
                    rate, a = wav = read_wav(body)
                    # chạm rồi gửi ngay: âm thanh quá ngắn làm STT lỗi, coi như chưa nghe gì
                    heard = app.stt(rate, a, app.hot()).strip().lower() if len(a) >= rate * 0.3 else ""
            except Exception:
                return self.send(400, "cần WAV PCM 16-bit hoặc JSON {\"text\": ...}".encode(), "text/plain; charset=utf-8")
            conv = app.conv(self.headers.get("X-Voice-Session", ""))
            rest = wake(heard, app.persona) if listen else heard
            if rest is None:  # người trong nhà/quán nói chuyện, không gọi trợ lý: bỏ qua, không ghi log
                return self.json({"ignored": True})
            p = app.persona
            if wav and heard and app.voices and app.voices.people():   # ai đang nói (khi đã có người đăng ký giọng)
                who = app.voices.who(*wav, talking=conv.who)[0]
                if who is None and app.only_people:   # TV, khách: không làm, không ghi lời họ nói
                    print("giọng lạ: bỏ qua", flush=True)
                    return self.json({"ignored": True} if listen else {"heard": "", "reply": STRANGER})
                conv.who, p = who, app.persona_for(who)
            if listen:
                if not rest:
                    reply = p.greet()
                    return self.json({"heard": heard, "reply": reply, "audio": app.speak(reply, p.voice), "awaiting": True})
                heard = rest
            t1 = time.perf_counter()
            try:
                reply = answer(heard, app.hub, app.ai, p, conv)
            except Exception as e:
                print(f"lỗi khi trả lời {heard!r}: {e!r}", flush=True)
                reply = "Có lỗi, bạn thử lại sau."
            t2 = time.perf_counter()
            audio = app.speak(reply, p.voice)
            ms = lambda a, b: f"{(b - a) * 1000:.0f} ms"
            print(f"nghe: {heard!r} → {reply!r} (STT {ms(t0, t1)}, xử lý {ms(t1, t2)}, TTS {ms(t2, time.perf_counter())})", flush=True)
            self.json({"heard": heard, "reply": reply, "audio": audio, **conv.take()})

        def do_GET(self):
            if m := re.fullmatch(r"/voice/tts/([0-9a-f]{16})\.wav", self.path):
                try:
                    p = app.tts.make(m[1])  # câu sau của câu trả lời dài: tổng hợp ngay nếu nền chưa tới
                except Exception as e:
                    print(f"TTS lỗi: {e}", flush=True)
                    p = None
                return self.send(200, p.read_bytes(), "audio/wav") if p else self.send_error(404)
            if self.path == "/voice/people":
                return self.json(app.voices.people() if app.voices else [])
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
    app = App(hub, None, sherpa_stt(), TTS(Path(os.environ.get("VOICED_CACHE", "/cache")), vieneu_synth()),
              Voices(Path(os.environ.get("VOICED_DATA", "/data")) / "voices.json", sherpa_speaker()))
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
