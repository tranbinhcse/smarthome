#!/usr/bin/env python3
"""Giả lập node năng lượng khi chưa lắp inverter: đăng lược đồ MQTT chung (nangluong/inverter/<khoá>)
lấy từ fixture của firmware. Chỉ dùng thư viện chuẩn (tự gói MQTT 3.1.1, QoS 0, không retain).

  MQTT_USER=node MQTT_PASS=... python3 scripts/gia_lap_inverter.py --host 127.0.0.1 --port 1884
  python3 scripts/gia_lap_inverter.py --dry-run                 # chỉ in, không gửi
  python3 scripts/gia_lap_inverter.py --loop 10                 # gửi lại mỗi 10 s

Đừng trỏ vào broker thật (192.168.1.254) khi không cần: số giả sẽ vào lịch sử của hubd.
"""
import argparse, json, os, socket, time

PREFIX = "nangluong/inverter/"
DEFAULT_FIXTURE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "firmware", "inverter", "fixtures", "deye-1pha.json")


def varint(n):
    """Remaining Length của MQTT: 7 bit mỗi byte, bit cao = còn tiếp."""
    out = bytearray()
    while True:
        n, b = divmod(n, 128)
        out.append(b | (0x80 if n else 0))
        if not n:
            return bytes(out)


def mstr(s):
    b = s.encode()
    return len(b).to_bytes(2, "big") + b


def messages(expect):
    out = [("nangluong/status", "online")]
    for k, v in expect.items():
        out.append((PREFIX + k, f"{v:g}"))
    return out


def publish_all(host, port, user, password, msgs):
    with socket.create_connection((host, port), timeout=5) as s:
        flags = 0x02 | (0x80 if user else 0) | (0x40 if password else 0)  # clean session
        var = mstr("MQTT") + bytes([4, flags]) + (30).to_bytes(2, "big")
        payload = mstr("gia-lap-inverter") + (mstr(user) if user else b"") + (mstr(password) if password else b"")
        s.sendall(b"\x10" + varint(len(var) + len(payload)) + var + payload)
        ack = s.recv(4)
        if len(ack) != 4 or ack[0] != 0x20 or ack[3] != 0:
            raise SystemExit(f"broker từ chối kết nối (CONNACK {ack.hex()}): kiểm tra MQTT_USER/MQTT_PASS")
        for topic, msg in msgs:
            body = mstr(topic) + msg.encode()
            s.sendall(b"\x30" + varint(len(body)) + body)
        s.sendall(b"\xe0\x00")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=1883)
    ap.add_argument("--fixture", default=DEFAULT_FIXTURE, help="firmware/inverter/fixtures/<gói>.json")
    ap.add_argument("--loop", type=float, default=0, help="gửi lại sau mỗi N giây (0 = một lần)")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()
    msgs = messages(json.load(open(a.fixture))["expect"])
    while True:
        if a.dry_run:
            for t, m in msgs:
                print(t, m)
        else:
            publish_all(a.host, a.port, os.environ.get("MQTT_USER", ""), os.environ.get("MQTT_PASS", ""), msgs)
            print(f"đã gửi {len(msgs)} tin tới {a.host}:{a.port}")
        if not a.loop:
            return
        time.sleep(a.loop)


if __name__ == "__main__":
    main()
