#!/usr/bin/env python3
"""Lấy local key Tuya bằng đăng nhập QR của app Smart Life (không cần Tuya IoT Platform).

Chạy trên máy Ubuntu bằng root (cần đọc gói broadcast trong LAN):
    ~/tuya-venv/bin/python ~/hubd/scripts/tuya_qr_login.py
Ghi tuya/devices.json (chứa local key, quyền 600, chỉ bridge đọc được, đã gitignore), kèm `mapping`
(dp -> code, kiểu, scale theo định dạng `tinytuya wizard`) để gen_config.py tách kênh và sinh số đo.
Thêm thiết bị mới trong app: chạy lại script này rồi gen_config.py.
Không in key ra màn hình. Không lưu token đăng nhập.
"""
import json
import os
import socket
import struct
import sys
import time

# Client ID + schema công khai của tích hợp Tuya trong Home Assistant; app sẽ hiện "Home Assistant" khi quét.
CLIENT_ID = "HA_3y9q4ak7g4ephrvke"
SCHEMA = "haauthorize"
OUT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "tuya", "devices.json"))
BRIDGE_UID = 65534  # user nobody trong container tuya-bridge


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


def login(user_code):
    import qrcode
    from tuya_sharing import LoginControl
    lc = LoginControl()
    r = lc.qr_code(CLIENT_ID, SCHEMA, user_code)
    if not r.get("success"):
        sys.exit(f"Không tạo được mã QR: {r.get('msg') or r.get('code') or 'lỗi không rõ'} (kiểm tra lại User Code)")
    token = r["result"]["qrcode"]
    qr = qrcode.QRCode(border=1)
    qr.add_data(f"tuyaSmart--qrLogin?token={token}")
    qr.print_ascii(invert=True)
    print("App Smart Life → Tôi → icon quét góc trên phải → quét mã trên → Xác nhận. Chờ tối đa 3 phút...")
    for _ in range(90):
        time.sleep(2)
        ok, info = lc.login_result(token, CLIENT_ID, user_code)
        if ok:
            return info
    sys.exit("Hết thời gian chờ quét QR, chạy lại script.")


def lan_broadcasts(seconds=20):
    """{device_id: {ip, version}} từ gói broadcast UDP 6666/6667/7000.

    Đọc qua raw socket nên vẫn thấy gói khi ufw chặn các cổng này.
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


def main():
    if os.geteuid() != 0:
        sys.exit("Chạy bằng root (cần đọc gói broadcast).")
    if sys.argv[1:] == ["--scan"]:
        for i, l in sorted(lan_broadcasts().items(), key=lambda x: x[1]["ip"]):
            print(f"{l['ip']:<16} v{l['version'] or '?':<5} {i}")
        return
    user_code = (sys.argv[1] if len(sys.argv) > 1 else
                 input("User Code (Smart Life → Tôi → ⚙️ → Tài khoản và bảo mật): ")).strip()
    info = login(user_code)
    from tuya_sharing import Manager
    m = Manager(CLIENT_ID, user_code, info["terminal_id"], info["endpoint"], info)
    m.update_device_cache()
    print("Nghe broadcast trong LAN 20 giây để lấy IP...")
    lan = lan_broadcasts()
    out = []
    for d in m.device_map.values():
        l = lan.get(d.id, {})
        out.append({"id": d.id, "name": d.name, "key": d.local_key, "ip": l.get("ip", ""),
                    "version": l.get("version", ""), "category": d.category,
                    "product_name": d.product_name, "sub": d.sub, "mapping": mapping_of(d)})
        print(f"  {d.name:<30} {l.get('ip') or 'CHƯA CÓ IP':<16} v{l.get('version') or '?':<5} {d.category}"
              f"{'' if out[-1]['mapping'] else ' (không có mapping: thiết bị không hỗ trợ local)'}")
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    fd = os.open(OUT, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(out, f, ensure_ascii=False, indent=2)
    os.chmod(OUT, 0o600)  # os.open chỉ đặt quyền khi tạo mới; file cũ (vd từ tinytuya wizard) có thể đang 644
    os.chown(OUT, BRIDGE_UID, BRIDGE_UID)
    print(f"Đã ghi {len(out)} thiết bị vào {OUT} (không in key). Bridge tự nạp lại.")


if __name__ == "__main__":
    main()
