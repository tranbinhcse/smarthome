#!/usr/bin/env python3
"""Tài khoản MQTT riêng cho một node ESP32 (chạy trên PC build firmware).

    python3 scripts/node_mqtt_user.py node-cafe

- firmware/secrets.yaml chưa có mqtt_pass_node_cafe (hoặc còn "..." của file mẫu) thì sinh ngẫu nhiên và ghi vào.
  Không in mật khẩu ra màn hình.
- In ra dòng cho mosquitto/passwd: chỉ có bản băm PBKDF2-SHA512 (định dạng $7$ của Mosquitto 2), không có mật khẩu gốc,
  nên gửi qua ssh lên trung tâm được. Cách đưa lên: firmware/README.md.
"""
import base64
import hashlib
import os
import re
import secrets
import sys

SECRETS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "firmware", "secrets.yaml")


def mosquitto_hash(user, password):
    salt = os.urandom(12)
    dk = hashlib.pbkdf2_hmac("sha512", password.encode(), salt, 101)
    return f"{user}:$7$101${base64.b64encode(salt).decode()}${base64.b64encode(dk).decode()}"


def main():
    node = sys.argv[1] if len(sys.argv) == 2 else ""
    if not re.fullmatch(r"node-[a-z0-9-]+", node):
        sys.exit("cách dùng: node_mqtt_user.py node-<khu>   (vd node-cafe)")
    key = "mqtt_pass_" + node.replace("-", "_")
    lines = open(SECRETS, encoding="utf-8").read().splitlines() if os.path.exists(SECRETS) else []
    old = [l.split(":", 1)[1].strip().strip('"') for l in lines if l.startswith(key + ":")]
    password = old[0] if old and old[0] not in ("", "...") else ""
    if not password:
        password = secrets.token_urlsafe(18)
        lines = [l for l in lines if not l.startswith(key + ":")] + [f'{key}: "{password}"']
        fd = os.open(SECRETS, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")
        os.chmod(SECRETS, 0o600)  # file chép từ secrets.example.yaml thường là 644
        print(f"đã ghi {key} vào firmware/secrets.yaml", file=sys.stderr)
    print(mosquitto_hash(node, password))


if __name__ == "__main__":
    main()
