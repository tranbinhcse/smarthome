"""Sinh phần `devices:` của config.yaml từ tuya/devices.json (kết quả `tinytuya wizard`).

Dùng:  python3 tuya-bridge/gen_config.py [tuya/devices.json] [config.yaml]
Chỉ thay đoạn giữa hai dòng đánh dấu BEGIN/END tuya-devices; phần còn lại giữ nguyên.
Không in local key. Sau khi chạy: docker compose restart hubd
"""
import json
import re
import sys

BEGIN, END = "  # BEGIN tuya-devices (gen_config.py — đừng sửa tay)", "  # END tuya-devices"


def render(devs, over=None):
    over = over or {}
    lines = [BEGIN]
    for d in devs:
        if not isinstance(d, dict) or not d.get("id"):
            continue
        i = d["id"]
        item = {"id": "tuya_" + i, "name": d.get("name") or i,
                "command_topic": f"tuya/{i}/cmd", "state_topic": f"tuya/{i}/state",
                "online_topic": f"tuya/{i}/online"}
        o = over.get(i, {})
        if o.get("name"):
            item["name"] = o["name"]
        for k in ("zone", "protected"):
            if k in o:
                item[k] = o[k]
        lines.append("  - " + json.dumps(item, ensure_ascii=False))  # JSON flow là YAML hợp lệ
    lines.append(END)
    return "\n".join(lines)


def apply(cfg, devs, over=None):
    block = render(devs, over)
    pat = re.compile(re.escape(BEGIN) + r".*?" + re.escape(END), re.S)
    if pat.search(cfg):
        return pat.sub(lambda _m: block, cfg)
    if not re.search(r"^devices:.*$", cfg, re.M):
        return cfg.rstrip("\n") + "\ndevices:\n" + block + "\n"
    return re.sub(r"^devices:.*$", lambda _m: "devices:\n" + block, cfg, count=1, flags=re.M)


if __name__ == "__main__":
    src = sys.argv[1] if len(sys.argv) > 1 else "tuya/devices.json"
    dst = sys.argv[2] if len(sys.argv) > 2 else "config.yaml"
    try:
        raw = open(src).read().strip()
        devs = json.loads(raw) if raw else []
    except FileNotFoundError:
        devs = []
    try:
        over = json.load(open(src.replace("devices.json", "overrides.json")))
    except (FileNotFoundError, ValueError):
        over = {}
    cfg = open(dst).read()
    open(dst, "w").write(apply(cfg, devs, over))
    print(f"{dst}: {len(devs)} thiết bị Tuya")
