import json, os, tempfile, time, unittest
import bridge


class FakeDev:
    """Thiết bị Tuya giả: công tắc 1 gang, có thể 'rớt mạng' khi gọi receive."""
    def __init__(self, fail_after=None):
        self.dps = {"1": False, "9": 0}
        self.calls = 0
        self.fail_after = fail_after
        self.closed = False
    def status(self): return {"dps": dict(self.dps)}
    def set_value(self, k, v): self.dps[k] = v; return {}
    def heartbeat(self, nowait=False): pass
    def receive(self):
        self.calls += 1
        if self.fail_after and self.calls > self.fail_after:
            return {"Error": "Network Error"}
        time.sleep(0.01)
        return None
    def close(self): self.closed = True


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

    def run_worker(self, fake):
        pub = Pub()
        dev = {"id": "a1", "name": "Đèn", "key": "k", "ip": "1.2.3.4", "version": 3.3, "dp": "1"}
        w = bridge.DeviceWorker(dev, pub, lambda _d: fake)
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


if __name__ == "__main__":
    unittest.main()
