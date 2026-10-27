"""Sinh phần Tuya của config.yaml từ tuya/devices.json (scripts/tuya_qr_login.py hoặc `tinytuya wizard`).

Dùng:  python3 tuya-bridge/gen_config.py [tuya/devices.json] [config.yaml]
Chỉ thay đoạn giữa các dòng đánh dấu BEGIN/END tuya-devices và tuya-entities; phần còn lại giữ nguyên.
Không in local key. Sau khi chạy: docker compose restart hubd

devices.json có `mapping` (dp -> code Tuya, scale) thì tự tách công tắc nhiều nút / ổ nhiều lỗ thành
từng kênh và sinh số đo (W, V, A, kWh, nhiệt độ, độ ẩm, CO₂, PM2.5). Không có mapping: mỗi thiết bị
một công tắc, riêng CB (dlq) có W/V/A theo dp chuẩn 18/19/20.

overrides.json theo id: name, zone, protected, kind, area (khu CB cấp điện: số đo hiện ở khu đó),
names ({"2": "Sục khí"}: tên từng kênh).
"""
import json
import re
import sys

BEGIN, END = "  # BEGIN tuya-devices (gen_config.py — đừng sửa tay)", "  # END tuya-devices"
EBEGIN, EEND = "  # BEGIN tuya-entities (gen_config.py — đừng sửa tay)", "  # END tuya-entities"

# category Tuya -> loại thiết bị của dashboard. Công tắc tường (kg) ở nhà này đều điều khiển đèn.
KIND = {**dict.fromkeys(("kg", "dj", "dd", "xdd", "fwd", "dc", "tgq", "tgkg", "gyd"), "light"),
        "tdq": "switch", "cz": "socket", "pc": "socket", "dlq": "breaker",
        **dict.fromkeys(("fs", "fsd", "fskg"), "fan"), **dict.fromkeys(("cl", "clkg", "mc"), "curtain"),
        **dict.fromkeys(("kt", "ktkzq"), "ac"),
        **dict.fromkeys(("wsdcg", "co2bj", "pm2.5", "hjjcy", "ywbj", "rqbj", "sj", "mcs", "pir", "ldcg"), "sensor"),
        "sp": "camera", "sd": "vacuum", "ggq": "water", "sfkzq": "water"}

# code Tuya -> (hậu tố id, tên, đơn vị, hệ số đổi đơn vị). Giá trị thật = giá trị dp × 10^-scale × hệ số.
MEASURE = {"cur_power": ("w", "Công suất", "W", 1), "cur_voltage": ("v", "Điện áp", "V", 1),
           "cur_current": ("a", "Dòng điện", "A", 0.001),  # Tuya báo mA
           "add_ele": ("kwh", "Điện năng", "kWh", 1),  # lượng tăng thêm mỗi lần báo: bridge cộng dồn -> /energy
           "va_temperature": ("temp", "Nhiệt độ", "°C", 1), "temp_current": ("temp", "Nhiệt độ", "°C", 1),
           "va_humidity": ("hum", "Độ ẩm", "%", 1), "humidity_value": ("hum", "Độ ẩm", "%", 1),
           "co2_value": ("co2", "CO₂", "ppm", 1), "pm25_value": ("pm25", "Bụi PM2.5", "µg/m³", 1)}

# CB đo điện chưa có mapping: dp chuẩn Tuya (18 mA, 19 W×10, 20 V×10), đã đối chiếu với CB thật.
STD_METER = {"18": {"code": "cur_current", "values": {"scale": 0}}, "19": {"code": "cur_power", "values": {"scale": 1}},
             "20": {"code": "cur_voltage", "values": {"scale": 1}}}

SWITCH = re.compile(r"^switch(_\d+|_led|_usb\d+)?$")


def _channels(m):
    """[(dp, code)] các công tắc trong mapping, theo thứ tự dp."""
    out = [(dp, v["code"]) for dp, v in m.items() if SWITCH.match(v.get("code", "")) or v.get("code") == "control"]
    return sorted(out, key=lambda x: int(x[0]))


def _label(code, kind, n):
    if code.startswith("switch_usb"):
        return "USB " + code[10:]
    return f"{'Lỗ' if kind == 'socket' else 'Nút'} {n}"


def describe(d, o=None):
    """Một thiết bị Tuya -> (danh sách device, danh sách entity) cho config.yaml."""
    o = o or {}
    i, name = d["id"], o.get("name") or d.get("name") or d["id"]
    kind = o.get("kind") or KIND.get(d.get("category"), d.get("category"))
    m = d.get("mapping") or {}
    base = {"id": "tuya_" + i, "name": name, "command_topic": f"tuya/{i}/cmd", "state_topic": f"tuya/{i}/state",
            "online_topic": f"tuya/{i}/online"}
    if kind:
        base["kind"] = kind
    base.update({k: o[k] for k in ("zone", "protected") if k in o})

    chans = _channels(m) if m else ([] if kind == "sensor" else [("1", "switch")])
    devs = []
    for n, (dp, code) in enumerate(chans, 1):
        item = dict(base)
        if code == "control":  # rèm: open/close thay cho bật/tắt
            on, off = "open", "close"
        else:
            on, off = True, False
        if len(chans) > 1:
            item.update(id=f"tuya_{i}_{dp}", name=o.get("names", {}).get(dp) or _label(code, kind, n), group=name)
        if len(chans) > 1 or dp != "1" or code == "control":  # đọc dp trong JSON dps, gửi lệnh JSON theo dp
            item.update(state_topic=f"tuya/{i}/dps", field=dp, on=json.dumps({dp: on}), off=json.dumps({dp: off}))
        devs.append(item)

    meters = m or (STD_METER if kind == "breaker" else {})
    ents = []
    for dp, v in sorted(meters.items(), key=lambda x: int(x[0])):
        if v.get("code") not in MEASURE:
            continue
        suf, label, unit, f = MEASURE[v["code"]]
        e = {"id": f"tuya_{i}.{suf}", "name": label, "unit": unit,
             "topic": f"tuya/{i}/energy" if v["code"] == "add_ele" else f"tuya/{i}/dps", "field": dp}
        scale = round(10 ** -int((v.get("values") or {}).get("scale") or 0) * f, 10)
        if scale != 1:
            e["scale"] = scale
        if o.get("area") or o.get("zone"):
            e["zone"] = o.get("area") or o["zone"]
        if devs:
            e["device"] = devs[0]["id"]
        ents.append(e)
    return devs, ents


def render(devs, over=None):
    over = over or {}
    dl, el = [BEGIN], [EBEGIN]
    for d in devs:
        if not isinstance(d, dict) or not d.get("id"):
            continue
        ds, es = describe(d, over.get(d["id"]))
        dl += ["  - " + json.dumps(x, ensure_ascii=False) for x in ds]  # JSON flow là YAML hợp lệ
        el += ["  - " + json.dumps(x, ensure_ascii=False) for x in es]
    return "\n".join(dl + [END]), "\n".join(el + [EEND])


def render_file(devs, over=None):
    """File cấu hình sinh cho hubd (HUBD_GEN_DIR): thiết bị + số đo Tuya, thay mục cùng id trong config.yaml."""
    dl, el = render(devs, over)
    items = lambda block: "\n".join(block.splitlines()[1:-1])  # bỏ dòng BEGIN/END
    d, e = items(dl), items(el)
    return ("# Sinh bởi tuya-bridge sau khi quét QR trên dashboard. Đừng sửa tay: quét lại sẽ ghi đè.\n"
            f"devices:{chr(10) + d if d else ' []'}\nentities:{chr(10) + e if e else ' []'}\n")


def _put(cfg, key, begin, end, block):
    pat = re.compile(re.escape(begin) + r".*?" + re.escape(end), re.S)
    if pat.search(cfg):
        return pat.sub(lambda _m: block, cfg)
    if not re.search(rf"^{key}:.*$", cfg, re.M):
        return cfg.rstrip("\n") + f"\n{key}:\n" + block + "\n"
    return re.sub(rf"^{key}:.*$", lambda _m: f"{key}:\n" + block, cfg, count=1, flags=re.M)


def apply(cfg, devs, over=None):
    dblock, eblock = render(devs, over)
    return _put(_put(cfg, "devices", BEGIN, END, dblock), "entities", EBEGIN, EEND, eblock)


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
