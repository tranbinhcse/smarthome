"""Quét QR Smart Life ngay trên dashboard (thay việc chạy tay scripts/tuya_qr_login.py).

hubd gửi tuya/setup/cmd {"action": "start", "user_code"} hoặc {"action": "cancel"}; bridge báo tiến độ ở
tuya/setup/status (retain): qr (kèm mã QR SVG) → fetching → scanning → done (danh sách thiết bị) hoặc error/idle.
Xong thì ghi devices.json (local key, quyền 600, bridge tự nạp lại) và file cấu hình sinh cho hubd
(HUBD_GEN_DIR, hubd thấy file đổi thì tự khởi động lại). Không bao giờ gửi key hay token đăng nhập lên MQTT.
"""
import json
import logging
import os
import socket
import struct
import threading
import time

import gen_config

log = logging.getLogger("qrsetup")
STATUS = "tuya/setup/status"
# Client ID + schema công khai của tích hợp Tuya trong Home Assistant; app sẽ hiện "Home Assistant" khi quét.
CLIENT_ID = "HA_3y9q4ak7g4ephrvke"
SCHEMA = "haauthorize"


def mapping_of(dev):
    """local_strategy của tuya_sharing -> {"<dp>": {"code", "type", "values"}}; rỗng nếu thiết bị không hỗ trợ local."""
    out = {}
    for dp, s in (getattr(dev, "local_strategy", None) or {}).items():
        c = s.get("config_item") or {}
        try:
            values = json.loads(c.get("valueDesc") or "{}")
        except ValueError:
            values = {}
        out[str(dp)] = {"code": s.get("status_code"), "type": c.get("valueType"), "values": values}
    return out


def lan_broadcasts(seconds=20):
    """{device_id: {ip, version}} từ gói broadcast UDP 6666/6667/7000.

    Đọc qua raw socket nên vẫn thấy gói khi ufw chặn các cổng này. Cần CAP_NET_RAW (root, hoặc setcap trên
    trình Python trong image tuya-bridge); thiếu quyền thì ném PermissionError.
    """
    import tinytuya
    s = socket.socket(socket.AF_PACKET, socket.SOCK_DGRAM, socket.htons(0x0800))
    s.settimeout(1)
    found, end = {}, time.time() + seconds
    while time.time() < end:
        try:
            b = s.recv(65535)
        except socket.timeout:
            continue
        if len(b) < 28 or b[9] != 17:  # chỉ UDP
            continue
        ihl = (b[0] & 0x0F) * 4
        if struct.unpack("!H", b[ihl + 2:ihl + 4])[0] not in (6666, 6667, 7000):
            continue
        try:
            data = json.loads(tinytuya.decrypt_udp(b[ihl + 8:]))
        except Exception:
            continue
        if data.get("gwId"):
            found[data["gwId"]] = {"ip": data.get("ip") or socket.inet_ntoa(b[12:16]),
                                   "version": str(data.get("version") or "")}
    s.close()
    return found


def qr_svg(data):
    import qrcode
    import qrcode.image.svg
    return qrcode.make(data, image_factory=qrcode.image.svg.SvgPathImage, border=2).to_string(encoding="unicode")


def _load(path):
    try:
        with open(path) as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def _login_control():
    from tuya_sharing import LoginControl
    return LoginControl()


def _manager(*a):
    from tuya_sharing import Manager
    return Manager(*a)


class QRSetup:
    def __init__(self, publish, devices_path, overrides_path, gen_path, login=_login_control, manager=_manager,
                 scan=lan_broadcasts, poll=2.0, timeout=180):
        self.publish, self.dp, self.op, self.gen = publish, devices_path, overrides_path, gen_path
        self.login, self.manager, self.scan, self.poll, self.timeout = login, manager, scan, poll, timeout
        self.thread, self.cancel_ev = None, threading.Event()

    def handle(self, payload):
        try:
            cmd = json.loads(payload)
        except ValueError:
            return
        if not isinstance(cmd, dict):
            return
        if cmd.get("action") == "cancel":
            self.cancel_ev.set()
        elif cmd.get("action") == "start" and str(cmd.get("user_code") or "").strip():
            self.cancel_ev.set()  # đang chờ quét mã cũ thì bỏ
            if self.thread:
                self.thread.join(5)
            self.cancel_ev = threading.Event()
            self.thread = threading.Thread(target=self._run, args=(str(cmd["user_code"]).strip(), self.cancel_ev), daemon=True)
            self.thread.start()

    def _status(self, state, **kw):
        self.publish(STATUS, json.dumps({"state": state, "at": int(time.time()), **kw}, ensure_ascii=False), 1, True)

    def _run(self, user_code, cancel):
        try:
            lc = self.login()
            r = lc.qr_code(CLIENT_ID, SCHEMA, user_code)
            if not r.get("success"):
                return self._status("error", message=f"Không tạo được mã QR ({r.get('msg') or r.get('code') or 'lỗi không rõ'}). Kiểm tra lại User Code.")
            token = r["result"]["qrcode"]
            self._status("qr", qr_svg=qr_svg(f"tuyaSmart--qrLogin?token={token}"),
                         message="Mở app Smart Life → Tôi → biểu tượng quét ở góc trên phải → quét mã này → Xác nhận.")
            deadline = time.monotonic() + self.timeout
            while True:
                if cancel.wait(self.poll):
                    return self._status("idle", message="Đã huỷ quét QR")
                ok, info = lc.login_result(token, CLIENT_ID, user_code)
                if ok:
                    break
                if time.monotonic() > deadline:
                    return self._status("error", message="Hết thời gian chờ quét. Bấm lấy mã QR mới.")
            self._status("fetching", message="Đã đăng nhập, đang lấy danh sách thiết bị...")
            m = self.manager(CLIENT_ID, user_code, info["terminal_id"], info["endpoint"], info)
            m.update_device_cache()
            self._status("scanning", message="Đang dò IP thiết bị trong mạng LAN...")
            try:
                lan = self.scan(15)
            except (PermissionError, OSError) as e:
                log.warning("không dò được broadcast LAN (%s): giữ IP cũ", type(e).__name__)
                lan = {}
            old = {d.get("id"): d for d in (_load(self.dp) or []) if isinstance(d, dict)}
            out = []
            for d in m.device_map.values():
                o, l = old.get(d.id, {}), lan.get(d.id, {})
                out.append({"id": d.id, "name": d.name, "key": d.local_key, "ip": l.get("ip") or o.get("ip", ""),
                            "version": l.get("version") or o.get("version", ""), "category": d.category,
                            "product_name": getattr(d, "product_name", ""), "sub": getattr(d, "sub", False),
                            "mapping": mapping_of(d)})
            self._write_devices(out)
            self._write_gen(out, _load(self.op) or {})
            self._status("done", message=f"Đã cập nhật {len(out)} thiết bị Tuya.",
                         devices=[{"id": d["id"], "name": d["name"], "category": d["category"], "ip": d["ip"],
                                   "channels": sum(1 for v in d["mapping"].values() if str(v.get("code", "")).startswith("switch")),
                                   "new": d["id"] not in old} for d in out])
        except Exception as e:  # không đưa nội dung lỗi lên MQTT: có thể chứa token
            log.error("quét QR lỗi: %s", type(e).__name__)
            self._status("error", message=f"Lỗi khi quét QR ({type(e).__name__}). Thử lại sau ít phút.")

    def _write_devices(self, out):
        # ghi đè tại chỗ: thư mục /tuya thuộc root, bridge (nobody) chỉ sửa được file sẵn có của mình
        fd = os.open(self.dp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as f:
            json.dump(out, f, ensure_ascii=False, indent=2)
        os.chmod(self.dp, 0o600)

    def _write_gen(self, out, over):
        tmp = self.gen + ".tmp"
        with open(tmp, "w") as f:
            f.write(gen_config.render_file(out, over))
        os.replace(tmp, self.gen)  # hubd không bao giờ đọc phải file ghi dở
