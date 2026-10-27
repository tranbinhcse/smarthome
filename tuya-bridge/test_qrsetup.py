import json
import os
import stat
import tempfile
import threading
import unittest
from types import SimpleNamespace

import yaml

import qrsetup


def put(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f)


def get(path):
    with open(path) as f:
        return f.read()

KEY = "khoa-local-bi-mat"


def dev(id_, name, category="kg", key=KEY):
    return SimpleNamespace(id=id_, name=name, local_key=key, category=category, product_name="p", sub=False,
                           local_strategy={1: {"status_code": "switch_1", "config_item": {"valueType": "Boolean", "valueDesc": "{}"}},
                                           2: {"status_code": "switch_2", "config_item": {"valueType": "Boolean", "valueDesc": "{}"}}})


class FakeLogin:
    def __init__(self, ok_after=2, qr_ok=True):
        self.n, self.ok_after, self.qr_ok = 0, ok_after, qr_ok

    def qr_code(self, client_id, schema, user_code):
        if not self.qr_ok:
            return {"success": False, "msg": "user code sai"}
        return {"success": True, "result": {"qrcode": "TOKEN-QR"}}

    def login_result(self, token, client_id, user_code):
        self.n += 1
        if self.n >= self.ok_after:
            return True, {"terminal_id": "t", "endpoint": "e", "access_token": "tk-dang-nhap"}
        return False, {}


class FakeManager:
    devices = []

    def __init__(self, *a):
        self.device_map = {}

    def update_device_cache(self):
        self.device_map = {d.id: d for d in FakeManager.devices}


class QRSetupTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.dp, self.op = os.path.join(self.dir, "devices.json"), os.path.join(self.dir, "overrides.json")
        self.gen = os.path.join(self.dir, "gen", "tuya.yaml")
        os.makedirs(os.path.dirname(self.gen))
        put(self.dp, [{"id": "cu", "name": "Đèn cũ", "key": "k-cu", "ip": "192.168.1.15", "version": "3.3"}])
        put(self.op, {"cu": {"zone": "kho"}})
        self.sent, self.done = [], threading.Event()

    def publish(self, topic, payload, qos=0, retain=False):
        self.sent.append((topic, json.loads(payload), retain))
        if json.loads(payload)["state"] in ("done", "error", "idle"):
            self.done.set()

    def make(self, login, scan=lambda s: {"moi": {"ip": "192.168.1.30", "version": "3.4"}}):
        return qrsetup.QRSetup(self.publish, self.dp, self.op, self.gen, login=lambda: login, manager=FakeManager,
                               scan=scan, poll=0.01, timeout=1)

    def states(self):
        return [p["state"] for _, p, _ in self.sent]

    def test_full_flow_writes_devices_and_generated_config(self):
        FakeManager.devices = [dev("cu", "Đèn kho"), dev("moi", "Công tắc 2 nút")]
        s = self.make(FakeLogin(ok_after=2))
        s.handle(b'{"action":"start","user_code":"abc"}')
        self.assertTrue(self.done.wait(5))
        self.assertEqual(self.states(), ["qr", "fetching", "scanning", "done"])
        topic, qr, retain = self.sent[0]
        self.assertEqual((topic, retain), ("tuya/setup/status", True))
        self.assertIn("<svg", qr["qr_svg"])
        # devices.json: key mới, IP dò được cho máy mới, máy cũ giữ IP cũ; quyền 600
        devs = {d["id"]: d for d in json.loads(get(self.dp))}
        self.assertEqual(devs["moi"]["ip"], "192.168.1.30")
        self.assertEqual(devs["cu"]["ip"], "192.168.1.15")
        self.assertEqual(devs["moi"]["key"], KEY)
        self.assertEqual(stat.S_IMODE(os.stat(self.dp).st_mode), 0o600)
        self.assertIn("1", devs["moi"]["mapping"])
        # file cấu hình sinh cho hubd: đủ thiết bị, tách kênh, override zone vẫn áp dụng
        g = yaml.safe_load(get(self.gen))
        ids = [d["id"] for d in g["devices"]]
        self.assertIn("tuya_moi_1", ids)
        self.assertIn("tuya_moi_2", ids)
        self.assertTrue(all(d.get("zone") == "kho" for d in g["devices"] if d["id"].startswith("tuya_cu")))
        # danh sách báo về dashboard: có thiết bị mới, không bao giờ có key hay token đăng nhập
        done = self.sent[-1][1]
        self.assertEqual({d["id"]: d["new"] for d in done["devices"]}, {"cu": False, "moi": True})
        blob = json.dumps([p for _, p, _ in self.sent])
        self.assertNotIn(KEY, blob)
        self.assertNotIn("tk-dang-nhap", blob)
        self.assertNotIn(KEY, get(self.gen))

    def test_bad_user_code_and_timeout_and_cancel(self):
        self.make(FakeLogin(qr_ok=False)).handle(b'{"action":"start","user_code":"sai"}')
        self.assertTrue(self.done.wait(5))
        self.assertEqual(self.states()[-1], "error")
        self.assertIn("User Code", self.sent[-1][1]["message"])

        self.sent.clear(); self.done.clear()
        self.make(FakeLogin(ok_after=10 ** 9)).handle(b'{"action":"start","user_code":"abc"}')
        self.assertTrue(self.done.wait(5))   # timeout=1 giây
        self.assertEqual(self.states(), ["qr", "error"])

        self.sent.clear(); self.done.clear()
        s = self.make(FakeLogin(ok_after=10 ** 9))
        s.handle(b'{"action":"start","user_code":"abc"}')
        s.handle(b'{"action":"cancel"}')
        self.assertTrue(self.done.wait(5))
        self.assertEqual(self.states()[-1], "idle")
        before = get(self.dp)
        self.assertEqual(json.loads(get(self.dp))[0]["id"], "cu", before)  # huỷ thì không đụng devices.json

    def test_scan_without_permission_keeps_old_ips(self):
        FakeManager.devices = [dev("cu", "Đèn kho")]

        def no_perm(_s):
            raise PermissionError("cần CAP_NET_RAW")
        self.make(FakeLogin(ok_after=1), scan=no_perm).handle(b'{"action":"start","user_code":"abc"}')
        self.assertTrue(self.done.wait(5))
        self.assertEqual(self.states()[-1], "done")
        self.assertEqual(json.loads(get(self.dp))[0]["ip"], "192.168.1.15")

    def test_garbage_command_ignored(self):
        s = self.make(FakeLogin())
        for p in (b"", b"{", b'{"action":"start"}', b'{"action":"xoa"}', b"[]"):
            s.handle(p)
        self.assertEqual(self.sent, [])


if __name__ == "__main__":
    unittest.main()
