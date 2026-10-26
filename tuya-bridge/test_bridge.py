import json, os, tempfile, time, unittest
import bridge


class FakeDev:
    """Thiết bị Tuya giả: công tắc 1 gang, có thể 'rớt mạng' khi gọi receive."""
    def __init__(self, fail_after=None):
        self.dps = {"1": False, "9": 0}
        self.calls = 0
        self.fail_after = fail_after
        self.closed = False
        self.raw_recv = []
        self.hb_ack = False
        self.refreshed = []
    def status(self): return {"dps": dict(self.dps)}
    def set_value(self, k, v): self.dps[k] = v; return {}
    def heartbeat(self, nowait=False): self.hb_ack = True
    def updatedps(self, index=None, nowait=False): self.refreshed.append((index, nowait))
    def receive(self):
        self.calls += 1
        if self.fail_after and self.calls > self.fail_after:
            return {"Error": "Network Error"}
        time.sleep(0.01)
        self.raw_recv = [9] if self.hb_ack else []  # tinytuya: trả lời heartbeat là tin rỗng, receive() trả None
        self.hb_ack = False
        return None
    def close(self): self.closed = True
    def set_socketTimeout(self, s): self.timeout = s


class SlowRecvDev(FakeDev):
    """Giống tinytuya: receive() khi thiết bị im lặng chặn đủ timeout socket (mặc định 5s) rồi trả None."""
    timeout = 5
    def receive(self):
        time.sleep(self.timeout)
        return None
    def set_value(self, k, v):
        self.set_timeout = self.timeout
        return super().set_value(k, v)


class SlowHbDev(SlowRecvDev):
    """heartbeat(nowait=False) chờ phản hồi tới hết timeout, như khi thiết bị trả lời chậm."""
    hb = 0
    def heartbeat(self, nowait=False):
        self.hb += 1
        if not nowait:
            time.sleep(self.timeout)


class MeterDev(FakeDev):
    """CB đo điện: status() trả ảnh chụp cũ; chỉ khi updatedps thì thiết bị mới báo số đo mới."""
    def __init__(self, pushes=()):
        super().__init__()
        self.dps = {"1": True, "17": 81, "18": 933, "19": 1499, "20": 2394}
        self.pushes = list(pushes)
    def updatedps(self, index=None, nowait=False):
        super().updatedps(index, nowait)
        self.pushes.append({"19": 1600 + len(self.refreshed)})
    def receive(self):
        r = super().receive()
        return {"dps": self.pushes.pop(0)} if self.pushes else r


class SilentDev(FakeDev):
    """Thiết bị mất điện / rớt Wi-Fi: socket còn mở nhưng không trả lời gì, kể cả heartbeat."""
    def heartbeat(self, nowait=False): pass


class Pub:
    def __init__(self): self.msgs = []
    def __call__(self, t, p): self.msgs.append((t, p))
    def last(self, t): return [p for tt, p in self.msgs if tt == t][-1]
    def has(self, t, p): return (t, p) in self.msgs


def wait(cond, t=3):
    end = time.time() + t
    while time.time() < end:
        if cond(): return True
        time.sleep(0.02)
    return False


class T(unittest.TestCase):
    def tmp(self, devs=None, over=None):
        d = tempfile.mkdtemp()
        p, o = os.path.join(d, "devices.json"), os.path.join(d, "overrides.json")
        if devs is not None: open(p, "w").write(devs if isinstance(devs, str) else json.dumps(devs))
        if over is not None: open(o, "w").write(json.dumps(over))
        return p, o

    def test_missing_or_empty_file_does_not_crash(self):
        p, o = self.tmp()
        self.assertEqual(bridge.load_devices(p, o)[0], [])
        p, o = self.tmp("")
        d, w = bridge.load_devices(p, o)
        self.assertEqual(d, []); self.assertTrue(w)
        p, o = self.tmp("{hỏng")
        self.assertEqual(bridge.load_devices(p, o)[0], [])

    def test_wizard_format_and_static_ip(self):
        raw = [{"id": "a1", "name": "Đèn", "key": "k", "ip": "", "version": "3.4"},
               {"id": "a2", "name": "Quạt", "key": "k", "ip": "192.168.1.14", "version": "3.3"},
               {"id": "a3", "name": "Không key", "ip": "192.168.1.15"}]
        p, o = self.tmp(raw, {"a1": {"ip": "192.168.1.11", "dp": "20"}})
        d, w = bridge.load_devices(p, o)
        self.assertEqual([x["id"] for x in d], ["a1", "a2"])
        self.assertEqual(d[0]["ip"], "192.168.1.11"); self.assertEqual(d[0]["dp"], "20")
        self.assertEqual(d[0]["version"], 3.4)
        self.assertEqual(len(w), 1)  # a3 thiếu key

    def test_parse_cmd(self):
        self.assertEqual(bridge.parse_cmd(b"ON"), {"1": True})
        self.assertEqual(bridge.parse_cmd("off", "2"), {"2": False})
        self.assertEqual(bridge.parse_cmd("toggle"), {"toggle": "1"})
        self.assertEqual(bridge.parse_cmd('{"2": true}'), {"2": True})
        self.assertIsNone(bridge.parse_cmd("bậy"))
        self.assertIsNone(bridge.parse_cmd("[1]"))

    def test_state_of(self):
        self.assertEqual(bridge.state_of({"1": True}), "ON")
        self.assertEqual(bridge.state_of({"1": False}), "OFF")
        self.assertIsNone(bridge.state_of({"9": 1}))

    def run_worker(self, fake, mapping=None, energy=None):
        pub = Pub()
        dev = {"id": "a1", "name": "Đèn", "key": "k", "ip": "1.2.3.4", "version": 3.3, "dp": "1", "mapping": mapping or {}}
        w = bridge.DeviceWorker(dev, pub, lambda _d: fake, energy)
        w.start()
        self.addCleanup(w.stop)
        return w, pub

    def test_worker_publishes_state_and_executes_cmd(self):
        fake = FakeDev()
        w, pub = self.run_worker(fake)
        self.assertTrue(wait(lambda: pub.has("tuya/a1/state", "OFF")))
        self.assertTrue(pub.has("tuya/a1/online", "online"))
        w.send(b"ON")
        self.assertTrue(wait(lambda: pub.has("tuya/a1/state", "ON")))
        self.assertTrue(fake.dps["1"])
        w.send(b"TOGGLE")
        self.assertTrue(wait(lambda: fake.dps["1"] is False))
        w.send(b"rac")  # lệnh rác không làm sập luồng
        w.send(b"ON")
        self.assertTrue(wait(lambda: fake.dps["1"] is True))

    def test_worker_reconnects_after_network_error(self):
        bridge.RETRY_MAX = 1
        devs = [FakeDev(fail_after=2), FakeDev()]
        it = iter(devs)
        pub = Pub()
        dev = {"id": "a1", "name": "Đèn", "key": "k", "ip": "1.2.3.4", "version": 3.3, "dp": "1"}
        orig = bridge.DeviceWorker.run
        w = bridge.DeviceWorker(dev, pub, lambda _d: next(it))
        w.start(); self.addCleanup(w.stop)
        self.assertTrue(wait(lambda: pub.has("tuya/a1/online", "offline")))
        self.assertTrue(wait(lambda: pub.msgs.count(("tuya/a1/online", "online")) >= 2, 8))
        self.assertTrue(devs[0].closed)

    def test_bridge_routes_cmd_and_reloads_without_restart(self):
        p, o = self.tmp("")  # chưa có thiết bị
        class C:
            def publish(self, *a, **k): pass
        fake = FakeDev()
        b = bridge.Bridge(C(), p, o, factory=lambda _d: fake)
        b.tick(); self.assertEqual(b.workers, {})
        time.sleep(0.05)
        open(p, "w").write(json.dumps([{"id": "a1", "name": "Đèn", "key": "k", "ip": "1.2.3.4"}]))
        os.utime(p, (time.time() + 5, time.time() + 5))
        b.tick(); self.assertEqual(list(b.workers), ["a1"])
        class M: topic = "tuya/a1/cmd"; payload = b"ON"
        b.on_message(None, None, M())
        self.assertTrue(wait(lambda: fake.dps["1"] is True))
        for w in b.workers.values(): w.stop()


    def test_cmd_applied_fast_while_device_idle(self):
        # Lệnh từ dashboard không được nằm chờ hết timeout của receive() (trước đây trễ tới ~5s).
        fake = SlowRecvDev()
        w, pub = self.run_worker(fake)
        self.assertTrue(wait(lambda: pub.has("tuya/a1/state", "OFF")))
        time.sleep(0.2)  # để luồng đang nằm trong receive()
        t0 = time.time()
        w.send(b"ON")
        self.assertTrue(wait(lambda: fake.dps["1"] is True, 1.5), "lệnh phải chạy trong 1,5s")
        self.assertLess(time.time() - t0, 1.5)
        self.assertEqual(fake.set_timeout, 5, "gửi lệnh/heartbeat/kết nối lại vẫn dùng timeout 5s")


    def test_cmd_not_blocked_by_heartbeat(self):
        # Lệnh đến đúng lúc heartbeat đang chờ phản hồi từng bị trễ tới ~5s.
        old = bridge.HEARTBEAT
        bridge.HEARTBEAT = 0.3
        self.addCleanup(setattr, bridge, "HEARTBEAT", old)
        fake = SlowHbDev()
        w, pub = self.run_worker(fake)
        self.assertTrue(wait(lambda: fake.hb >= 1, 3))
        w.send(b"ON")
        self.assertTrue(wait(lambda: fake.dps["1"] is True, 1.5), "lệnh không được chờ heartbeat")


class Live(unittest.TestCase):
    """Số đo không đứng, online/offline đúng, kWh cộng dồn."""
    def setUp(self):
        for k, v in {"HEARTBEAT": 0.1, "STALE": 0.6, "REFRESH": 0.1, "RETRY_MAX": 60}.items():
            self.addCleanup(setattr, bridge, k, getattr(bridge, k))
            setattr(bridge, k, v)
    run_worker = T.run_worker

    def test_meter_dps(self):
        self.assertEqual(bridge.meter_dps({}, {"1": True, "18": 1, "19": 2, "20": 3}), [18, 19, 20])
        self.assertEqual(bridge.meter_dps({}, {"1": True, "9": 0}), [])
        m = {"1": {"code": "switch_1"}, "4": {"code": "cur_current"}, "5": {"code": "cur_power"}, "6": {"code": "cur_voltage"},
             "18": {"code": "countdown"}}
        self.assertEqual(bridge.meter_dps(m, {"1": True, "4": 0, "5": 0, "6": 0, "18": 0}), [4, 5, 6])

    def test_meter_values_refreshed_not_stale(self):
        fake = MeterDev()
        w, pub = self.run_worker(fake)
        self.assertTrue(wait(lambda: len(fake.refreshed) >= 2))
        self.assertEqual(fake.refreshed[0], ([18, 19, 20], True))
        self.assertTrue(wait(lambda: json.loads(pub.last("tuya/a1/dps"))["19"] >= 1601), pub.last("tuya/a1/dps"))

    def test_plain_switch_not_refreshed(self):
        fake = FakeDev()
        w, pub = self.run_worker(fake)
        time.sleep(0.5)
        self.assertEqual(fake.refreshed, [])

    def test_offline_when_device_stops_answering(self):
        fake = SilentDev()
        w, pub = self.run_worker(fake)
        self.assertTrue(wait(lambda: pub.has("tuya/a1/online", "online")))
        self.assertTrue(wait(lambda: pub.has("tuya/a1/online", "offline"), 3), "phải báo offline khi không còn trả lời")
        self.assertTrue(wait(lambda: fake.closed, 3))

    def test_stays_online_while_heartbeat_answered(self):
        fake = FakeDev()
        w, pub = self.run_worker(fake)
        time.sleep(1.5)
        self.assertEqual([p for t, p in pub.msgs if t == "tuya/a1/online"], ["online"])

    def test_energy_counts_pushed_increments_only(self):
        path = os.path.join(tempfile.mkdtemp(), "energy.json")
        fake = MeterDev(pushes=[{"17": 5}, {"17": 3}])
        fake.updatedps = lambda *a, **k: None
        w, pub = self.run_worker(fake, mapping={"17": {"code": "add_ele"}}, energy=bridge.Energy(path))
        self.assertTrue(wait(lambda: pub.has("tuya/a1/energy", '{"17": 8}')), pub.msgs)  # 81 trong status() là số cũ: không cộng
        w.stop()
        self.assertEqual(json.load(open(path)), {"a1": {"17": 8}})
        fake2 = MeterDev(pushes=[{"17": 2}])
        fake2.updatedps = lambda *a, **k: None
        w2, pub2 = self.run_worker(fake2, mapping={"17": {"code": "add_ele"}}, energy=bridge.Energy(path))
        self.assertTrue(wait(lambda: pub2.has("tuya/a1/energy", '{"17": 10}')), pub2.msgs)  # nối tiếp sau khởi động lại

    def test_load_devices_keeps_mapping(self):
        d = tempfile.mkdtemp()
        p = os.path.join(d, "devices.json")
        m = {"19": {"code": "cur_power", "type": "Integer", "values": {"scale": 1}}}
        open(p, "w").write(json.dumps([{"id": "a1", "key": "k", "ip": "1.2.3.4", "mapping": m}, {"id": "a2", "key": "k", "ip": "1.2.3.5"}]))
        devs, _ = bridge.load_devices(p)
        self.assertEqual([x["mapping"] for x in devs], [m, {}])


if __name__ == "__main__":
    unittest.main()
