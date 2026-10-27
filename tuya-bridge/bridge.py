"""Cầu nối Tuya (giao thức local, tinytuya) <-> MQTT.

Đọc /tuya/devices.json (scripts/tuya_qr_login.py hoặc `tinytuya wizard`: id, name, key, ip, version,
mapping) và tuỳ chọn /tuya/overrides.json ({"<id>": {"ip": "...", "version": "3.4", "dp": "1"}}) để đặt IP tĩnh.
Không có file / file rỗng -> log rõ ràng và chờ, tự nạp lại khi file đổi (không crash loop).

CB/ổ đo điện không tự báo W/V/A, còn status() trả ảnh chụp cũ: bridge gửi updatedps mỗi REFRESH giây.
Không nhận được gì (kể cả trả lời heartbeat) quá STALE giây -> coi là mất kết nối, báo offline, nối lại.

Topic:
  tuya/<id>/state   ON|OFF  (công tắc chính, retained)
  tuya/<id>/dps     JSON toàn bộ DP (retained)
  tuya/<id>/online  online|offline (retained)
  tuya/<id>/energy  JSON {dp: tổng} cộng dồn các lần báo add_ele (retained, lưu ở TUYA_STATE)
  tuya/<id>/cmd     ON|OFF|TOGGLE hoặc JSON {"2": true}
  tuya/bridge/status online|offline (LWT)
"""
import json
import logging
import os
import queue
import threading
import time

log = logging.getLogger("tuya-bridge")

HEARTBEAT = 9      # giây giữa các heartbeat giữ kết nối
RETRY_MAX = 60     # trần thời gian chờ giữa các lần kết nối lại
RECV_TIMEOUT = 0.5  # giây chờ tin từ thiết bị mỗi vòng: lệnh mới không phải đợi lâu hơn mức này
CONN_TIMEOUT = 5    # timeout mặc định của tinytuya, giữ cho gửi lệnh / heartbeat / kết nối lại
RELOAD = 15        # giây giữa các lần kiểm tra file cấu hình đổi
REFRESH = 10       # giây giữa các lần yêu cầu thiết bị đo điện báo lại W/V/A
STALE = 30         # giây không nhận được gì từ thiết bị (heartbeat 9s/lần) thì coi là mất kết nối
METER = ("cur_current", "cur_power", "cur_voltage")
STD_METER = ("18", "19", "20")  # dp chuẩn Tuya khi devices.json chưa có mapping


def _load_json(path):
    try:
        with open(path) as f:
            txt = f.read().strip()
        return json.loads(txt) if txt else None
    except FileNotFoundError:
        return None
    except (OSError, ValueError) as e:
        log.error("không đọc được %s: %s", path, e)
        return None


def load_devices(devices_path, overrides_path=None):
    """Trả về (list thiết bị hợp lệ, list cảnh báo). Không bao giờ raise."""
    raw = _load_json(devices_path)
    over = _load_json(overrides_path) if overrides_path else None
    over = over if isinstance(over, dict) else {}
    warns, out = [], []
    if raw is None:
        return out, [f"{devices_path} chưa có hoặc rỗng — chờ `tinytuya wizard` (không crash)"]
    if not isinstance(raw, list):
        return out, [f"{devices_path} phải là danh sách thiết bị"]
    for d in raw:
        if not isinstance(d, dict) or not d.get("id"):
            warns.append("bỏ qua mục không có id")
            continue
        o = over.get(d["id"], {})
        ip = o.get("ip") or d.get("ip") or ""
        if ip in ("", "Auto"):
            warns.append(f"{d.get('name') or d['id']}: chưa có IP — thêm vào overrides.json")
            continue
        if not d.get("key"):
            warns.append(f"{d.get('name') or d['id']}: thiếu local key")
            continue
        ver = o.get("version") or d.get("version") or "3.3"
        out.append({"id": d["id"], "name": d.get("name") or d["id"], "key": d["key"],
                    "ip": ip, "version": float(ver), "dp": str(o.get("dp") or "1"),
                    "mapping": d.get("mapping") or {}})
    return out, warns


def parse_cmd(payload, dp="1"):
    """Payload lệnh -> dict {dp: giá trị} hoặc {'toggle': dp}; None nếu không hiểu."""
    s = payload.decode() if isinstance(payload, bytes) else str(payload)
    t = s.strip()
    u = t.upper()
    if u in ("ON", "1", "TRUE"):
        return {dp: True}
    if u in ("OFF", "0", "FALSE"):
        return {dp: False}
    if u == "TOGGLE":
        return {"toggle": dp}
    try:
        j = json.loads(t)
    except ValueError:
        return None
    if isinstance(j, dict) and j and all(isinstance(v, (bool, int, float, str)) for v in j.values()):
        return {str(k): v for k, v in j.items()}
    return None


def state_of(dps, dp="1"):
    v = (dps or {}).get(dp)
    if isinstance(v, bool):
        return "ON" if v else "OFF"
    return None


def meter_dps(mapping, dps):
    """Các dp W/V/A cần updatedps: theo mapping nếu có, không thì dp chuẩn 18/19/20 mà thiết bị đang báo."""
    ids = [k for k, v in mapping.items() if v.get("code") in METER] if mapping else STD_METER
    return sorted(int(k) for k in ids if k in dps)


class Energy:
    """Tổng điện năng cộng dồn từ các lần thiết bị báo add_ele (mỗi lần là lượng tăng thêm), lưu file JSON."""

    def __init__(self, path):
        self.path, self.lock = path, threading.Lock()
        t = _load_json(path)
        self.tot = t if isinstance(t, dict) else {}

    def get(self, dev_id):
        with self.lock:
            return dict(self.tot.get(dev_id, {}))

    def add(self, dev_id, dp, v):
        with self.lock:
            d = self.tot.setdefault(dev_id, {})
            d[dp] = round(d.get(dp, 0) + v, 6)
            try:
                with open(self.path + ".tmp", "w") as f:
                    json.dump(self.tot, f)
                os.replace(self.path + ".tmp", self.path)
            except OSError as e:
                log.error("không ghi được %s: %s", self.path, e)
            return dict(d)


class DeviceWorker(threading.Thread):
    """Một luồng cho mỗi thiết bị: kết nối bền, heartbeat, nhận cập nhật, chạy lệnh."""

    def __init__(self, dev, publish, factory, energy=None):
        super().__init__(daemon=True, name="tuya-" + dev["id"])
        self.dev, self.publish, self.factory, self.energy = dev, publish, factory, energy
        self.cmds = queue.Queue()
        self.stop_evt = threading.Event()
        self.dps = {}
        self.online = None

    def send(self, payload):
        self.cmds.put(payload)

    def _set_online(self, v):
        if v != self.online:
            self.online = v
            self.publish(f"tuya/{self.dev['id']}/online", "online" if v else "offline")

    def _update(self, dps):
        if not isinstance(dps, dict) or not dps:
            return
        self.dps.update(dps)
        i = self.dev["id"]
        self.publish(f"tuya/{i}/dps", json.dumps(self.dps, ensure_ascii=False))
        s = state_of(self.dps, self.dev["dp"])
        if s:
            self.publish(f"tuya/{i}/state", s)

    def _count(self, dps, sums):
        # ponytail: tin add_ele đẩy về được coi là lượng tăng thêm (report_type "sum" của Tuya); đối chiếu với app sau vài ngày
        for k in sums:
            v = dps.get(k)
            if isinstance(v, (int, float)) and not isinstance(v, bool) and v > 0:
                tot = self.energy.add(self.dev["id"], k, v)
                self.publish(f"tuya/{self.dev['id']}/energy", json.dumps(tot))

    def _apply(self, d, payload):
        c = parse_cmd(payload, self.dev["dp"])
        if c is None:
            log.warning("%s: lệnh không hiểu: %r", self.dev["name"], payload)
            return
        if "toggle" in c:
            c = {c["toggle"]: not bool(self.dps.get(c["toggle"]))}
        for k, v in c.items():
            r = d.set_value(k, v)
            if isinstance(r, dict) and r.get("Error"):
                log.warning("%s: set %s lỗi: %s", self.dev["name"], k, r["Error"])
        self._update(c)

    def _session(self, d):
        r = d.status()
        if not isinstance(r, dict) or "dps" not in r:
            raise ConnectionError(f"status lỗi: {r.get('Error') if isinstance(r, dict) else r}")
        self._set_online(True)
        self._update(r["dps"])  # số đo trong status() có thể cũ: không cộng add_ele, updatedps ngay sau đây
        mapping = self.dev.get("mapping") or {}
        meter = meter_dps(mapping, self.dps)
        sums = [k for k, v in mapping.items() if v.get("code") == "add_ele"] if self.energy else []
        if sums and self.energy.get(self.dev["id"]):
            self.publish(f"tuya/{self.dev['id']}/energy", json.dumps(self.energy.get(self.dev["id"])))
        last_hb = last_rx = time.time()
        last_ref = 0
        while not self.stop_evt.is_set():
            try:
                self._apply(d, self.cmds.get_nowait())
                continue
            except queue.Empty:
                pass
            if time.time() - last_hb >= HEARTBEAT:
                d.heartbeat(nowait=True)  # không chờ phản hồi: vòng receive() đọc nó, lệnh mới không bị kẹt
                last_hb = time.time()
            if meter and time.time() - last_ref >= REFRESH:
                d.updatedps(meter, nowait=True)  # thiết bị trả số mới qua receive()
                last_ref = time.time()
            d.set_socketTimeout(RECV_TIMEOUT)
            try:
                r = d.receive()
            finally:
                d.set_socketTimeout(CONN_TIMEOUT)
            # trả lời heartbeat là tin rỗng: receive() trả None nhưng raw_recv có tin
            if r or getattr(d, "raw_recv", None):
                last_rx = time.time()
            elif time.time() - last_rx > STALE:
                raise ConnectionError(f"không phản hồi {STALE}s")
            if isinstance(r, dict):
                if r.get("Error"):
                    raise ConnectionError(r["Error"])
                if "dps" in r:
                    self._count(r["dps"], sums)
                    self._update(r["dps"])

    def run(self):
        delay = 2
        while not self.stop_evt.is_set():
            d = None
            try:
                d = self.factory(self.dev)
                log.info("%s: kết nối %s", self.dev["name"], self.dev["ip"])
                self._session(d)
            except Exception as e:  # noqa: BLE001 — mọi lỗi mạng đều dẫn tới kết nối lại
                log.warning("%s: mất kết nối (%s), thử lại sau %ss", self.dev["name"], e, delay)
                self._set_online(False)
                if self.stop_evt.wait(delay):
                    break
                delay = min(delay * 2, RETRY_MAX)
            else:
                delay = 2
            finally:
                try:
                    if d:
                        d.close()
                except Exception:  # noqa: BLE001
                    pass

    def stop(self):
        self.stop_evt.set()


def tinytuya_factory(dev):
    import tinytuya
    d = tinytuya.Device(dev["id"], dev["ip"], dev["key"], version=dev["version"])
    d.set_socketPersistent(True)
    d.set_socketTimeout(5)
    d.set_socketRetryLimit(1)
    return d


class Bridge:
    def __init__(self, client, devices_path, overrides_path, factory=tinytuya_factory, energy=None):
        self.client, self.dp, self.op, self.factory, self.energy = client, devices_path, overrides_path, factory, energy
        self.workers = {}
        self.sig = None
        self.warned = set()

    def publish(self, topic, payload):
        self.client.publish(topic, payload, qos=1, retain=True)

    def on_message(self, _c, _u, msg):
        parts = msg.topic.split("/")
        if len(parts) == 3 and parts[2] == "cmd" and parts[1] in self.workers:
            self.workers[parts[1]].send(msg.payload)

    def _mtimes(self):
        return tuple(os.path.getmtime(p) if os.path.exists(p) else None for p in (self.dp, self.op))

    def reload(self):
        devs, warns = load_devices(self.dp, self.op)
        for w in warns:
            if w not in self.warned:
                log.warning(w)
                self.warned.add(w)
        want = {d["id"]: d for d in devs}
        for i in list(self.workers):
            w = self.workers[i]
            if i not in want or w.dev != want[i]:
                w.stop()
                del self.workers[i]
        for i, d in want.items():
            if i not in self.workers:
                w = DeviceWorker(d, self.publish, self.factory, self.energy)
                self.workers[i] = w
                w.start()
        log.info("đang giữ %d thiết bị Tuya", len(self.workers))

    def tick(self):
        s = self._mtimes()
        if s != self.sig:
            self.sig = s
            self.warned.clear()
            self.reload()


def main():
    import paho.mqtt.client as mqtt
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    tdir = os.environ.get("TUYA_DIR", "/tuya")
    c = mqtt.Client(client_id="tuya-bridge")
    if os.environ.get("MQTT_USER"):
        c.username_pw_set(os.environ["MQTT_USER"], os.environ.get("MQTT_PASS", ""))
    c.will_set("tuya/bridge/status", "offline", qos=1, retain=True)
    b = Bridge(c, os.path.join(tdir, "devices.json"), os.path.join(tdir, "overrides.json"),
               energy=Energy(os.environ.get("TUYA_STATE", "/state/energy.json")))
    c.on_message = b.on_message
    c.on_connect = lambda cl, *_: (cl.subscribe("tuya/+/cmd", 1),
                                   cl.publish("tuya/bridge/status", "online", 1, True),
                                   log.info("MQTT connected"))
    c.on_disconnect = lambda *_: log.warning("MQTT mất kết nối, paho tự nối lại")
    host = os.environ.get("MQTT_HOST", "127.0.0.1")
    while True:  # broker có thể chưa sẵn sàng khi khởi động
        try:
            c.connect(host, int(os.environ.get("MQTT_PORT", "1883")), 30)
            break
        except OSError as e:
            log.warning("chưa nối được MQTT %s: %s", host, e)
            time.sleep(3)
    c.loop_start()
    while True:
        b.tick()
        time.sleep(RELOAD)


if __name__ == "__main__":
    main()
