import json
import unittest
import gen_config as g

class T(unittest.TestCase):
    def test_idempotent_and_no_key(self):
        devs = [{"id": "x1", "name": "Đèn \"bếp\"", "key": "SECRET"}]
        a = g.apply("mqtt: {}\n\ndevices: []\n", devs)
        self.assertNotIn("SECRET", a)
        self.assertIn('"tuya/x1/cmd"', a)
        self.assertEqual(g.apply(a, devs), a)
        self.assertEqual(g.apply(a, []).count("tuya_x1"), 0)
        self.assertIn("devices:", g.apply(a, []))

    def test_yaml_valid(self):
        out = g.apply("devices: []\n", [{"id": "x1", "name": "Đèn: bếp #1"}])
        line = [l for l in out.splitlines() if l.startswith("  - {")][0]
        self.assertEqual(json.loads(line[4:])["name"], "Đèn: bếp #1")

class T2(unittest.TestCase):
    def test_overrides_zone_protected_name(self):
        out = g.apply("devices: []\n", [{"id": "x1", "name": "A"}], {"x1": {"zone": "khu1", "protected": True, "name": "Tủ lạnh"}})
        self.assertIn('"zone": "khu1"', out)
        self.assertIn('"protected": true', out)
        self.assertIn("Tủ lạnh", out)

    def test_kind_from_category_and_override(self):
        out = g.apply("devices: []\n", [{"id": "x1", "name": "A", "category": "dlq"},
                                         {"id": "x2", "name": "B", "category": "kg"},
                                         {"id": "x3", "name": "C"}], {"x2": {"kind": "socket"}})
        self.assertIn('"kind": "breaker"', out)
        self.assertIn('"kind": "socket"', out)
        self.assertEqual(out.count('"kind"'), 2)  # không có category thì không ghi kind


def sw(n, code=None):
    return {str(n): {"code": code or f"switch_{n}", "type": "Boolean", "values": {}}}


def num(dp, code, unit, scale):
    return {str(dp): {"code": code, "type": "Integer", "values": {"unit": unit, "min": 0, "max": 99999, "scale": scale, "step": 1}}}


class Describe(unittest.TestCase):
    def test_category_to_kind(self):
        want = {"kg": "light", "dj": "light", "tdq": "switch", "pc": "socket", "cz": "socket", "dlq": "breaker",
                "fs": "fan", "cl": "curtain", "kt": "ac", "sp": "camera", "sd": "vacuum", "ggq": "water", "lạ": "lạ"}
        for cat, kind in want.items():
            devs, _ = g.describe({"id": "x", "name": "A", "category": cat})
            self.assertEqual(devs[0]["kind"], kind, cat)
        devs, _ = g.describe({"id": "x", "name": "A", "category": "kg"}, {"kind": "fan"})
        self.assertEqual(devs[0]["kind"], "fan")

    def test_multi_outlet_split_into_channels(self):
        m = {**sw(1), **sw(2), **sw(3), "9": {"code": "countdown_1", "type": "Integer", "values": {}}}
        devs, _ = g.describe({"id": "x", "name": "Ổ cắm Hồ Cá", "category": "pc", "mapping": m},
                             {"zone": "cafe", "protected": True, "names": {"2": "Sục khí"}})
        self.assertEqual([d["id"] for d in devs], ["tuya_x_1", "tuya_x_2", "tuya_x_3"])
        self.assertEqual([d["name"] for d in devs], ["Lỗ 1", "Sục khí", "Lỗ 3"])
        d = devs[1]
        self.assertEqual(d["state_topic"], "tuya/x/dps")
        self.assertEqual(d["field"], "2")
        self.assertEqual(json.loads(d["on"]), {"2": True})
        self.assertEqual(json.loads(d["off"]), {"2": False})
        self.assertEqual(d["command_topic"], "tuya/x/cmd")
        self.assertEqual(d["online_topic"], "tuya/x/online")
        for d in devs:
            self.assertEqual((d["group"], d["zone"], d["protected"], d["kind"]), ("Ổ cắm Hồ Cá", "cafe", True, "socket"))

    def test_multi_gang_switch_names(self):
        devs, _ = g.describe({"id": "x", "name": "Công tắc bếp", "category": "kg", "mapping": {**sw(1), **sw(2)}})
        self.assertEqual([d["name"] for d in devs], ["Nút 1", "Nút 2"])

    def test_single_switch_keeps_legacy_state_topic(self):
        for d in ({"id": "x", "name": "Đèn", "category": "kg", "mapping": sw(1)}, {"id": "x", "name": "Đèn", "category": "kg"}):
            devs, _ = g.describe(d)
            self.assertEqual(len(devs), 1)
            self.assertEqual(devs[0]["id"], "tuya_x")
            self.assertEqual(devs[0]["state_topic"], "tuya/x/state")
            self.assertNotIn("field", devs[0])
            self.assertNotIn("on", devs[0])

    def test_light_main_switch_not_dp1(self):
        m = {**sw(20, "switch_led"), **num(22, "bright_value", "", 0)}
        devs, ents = g.describe({"id": "x", "name": "Đèn trần", "category": "dj", "mapping": m})
        self.assertEqual(len(devs), 1)
        self.assertEqual(devs[0]["id"], "tuya_x")
        self.assertEqual((devs[0]["state_topic"], devs[0]["field"]), ("tuya/x/dps", "20"))
        self.assertEqual(json.loads(devs[0]["on"]), {"20": True})
        self.assertEqual(ents, [])  # độ sáng đèn không phải cảm biến

    def test_curtain_open_close(self):
        m = {"1": {"code": "control", "type": "Enum", "values": {"range": ["open", "stop", "close"]}}}
        devs, _ = g.describe({"id": "x", "name": "Rèm", "category": "cl", "mapping": m})
        self.assertEqual((devs[0]["kind"], devs[0]["field"]), ("curtain", "1"))
        self.assertEqual(json.loads(devs[0]["on"]), {"1": "open"})
        self.assertEqual(json.loads(devs[0]["off"]), {"1": "close"})

    def test_meter_entities_from_mapping(self):
        m = {**sw(1), **num(17, "add_ele", "kwh", 3), **num(18, "cur_current", "mA", 0),
             **num(19, "cur_power", "W", 1), **num(20, "cur_voltage", "V", 1)}
        _, ents = g.describe({"id": "x", "name": "Ổ đo điện", "category": "cz", "mapping": m}, {"zone": "tudien1", "area": "cafe"})
        by = {e["id"]: e for e in ents}
        self.assertEqual(set(by), {"tuya_x.w", "tuya_x.v", "tuya_x.a", "tuya_x.kwh"})
        self.assertEqual(by["tuya_x.w"], {"id": "tuya_x.w", "name": "Công suất", "unit": "W", "topic": "tuya/x/dps",
                                          "field": "19", "scale": 0.1, "zone": "cafe", "device": "tuya_x"})
        self.assertEqual((by["tuya_x.v"]["field"], by["tuya_x.v"]["scale"], by["tuya_x.v"]["unit"]), ("20", 0.1, "V"))
        self.assertEqual((by["tuya_x.a"]["field"], by["tuya_x.a"]["scale"], by["tuya_x.a"]["unit"]), ("18", 0.001, "A"))
        # add_ele là lượng tăng thêm mỗi lần báo: bridge cộng dồn rồi gửi tuya/<id>/energy
        self.assertEqual((by["tuya_x.kwh"]["topic"], by["tuya_x.kwh"]["field"], by["tuya_x.kwh"]["scale"], by["tuya_x.kwh"]["unit"]),
                         ("tuya/x/energy", "17", 0.001, "kWh"))

    def test_meter_entities_attach_to_first_channel(self):
        m = {**sw(1), **sw(2), **num(19, "cur_power", "W", 1)}
        _, ents = g.describe({"id": "x", "name": "Ổ", "category": "pc", "mapping": m}, {"zone": "bep"})
        self.assertEqual([(e["device"], e["zone"]) for e in ents], [("tuya_x_1", "bep")])

    def test_breaker_without_mapping_uses_standard_meter_dps(self):
        _, ents = g.describe({"id": "x", "name": "CB", "category": "dlq"}, {"zone": "tudien1"})
        got = {e["id"]: (e["field"], e["scale"], e["unit"], e["zone"]) for e in ents}
        self.assertEqual(got, {"tuya_x.w": ("19", 0.1, "W", "tudien1"), "tuya_x.v": ("20", 0.1, "V", "tudien1"),
                               "tuya_x.a": ("18", 0.001, "A", "tudien1")})  # không đoán kWh khi chưa biết scale
        _, ents = g.describe({"id": "x", "name": "Ổ", "category": "pc"})
        self.assertEqual(ents, [])  # ổ không có mapping: không biết có đo điện hay không

    def test_wifi_sensor_becomes_entities_only(self):
        m = {**num(1, "va_temperature", "℃", 1), **num(2, "va_humidity", "%", 0), **num(4, "battery_percentage", "%", 0),
             **num(5, "co2_value", "ppm", 0), **num(6, "pm25_value", "ug/m3", 0)}
        devs, ents = g.describe({"id": "x", "name": "Cảm biến bếp", "category": "wsdcg", "mapping": m}, {"zone": "bep"})
        self.assertEqual(devs, [])  # không có công tắc: không hiện nút bật/tắt
        by = {e["id"]: e for e in ents}
        self.assertEqual(set(by), {"tuya_x.temp", "tuya_x.hum", "tuya_x.co2", "tuya_x.pm25"})
        self.assertEqual(by["tuya_x.temp"], {"id": "tuya_x.temp", "name": "Nhiệt độ", "unit": "°C", "topic": "tuya/x/dps",
                                             "field": "1", "scale": 0.1, "zone": "bep"})
        self.assertEqual((by["tuya_x.hum"]["unit"], by["tuya_x.hum"].get("scale")), ("%", None))
        self.assertIn("ẩm", by["tuya_x.hum"]["name"])  # dashboard nhận độ ẩm theo tên
        self.assertRegex(by["tuya_x.co2"]["name"], "(?i)co")  # dashboard nhận CO₂ theo tên chứa "CO"
        self.assertEqual(by["tuya_x.pm25"]["unit"], "µg/m³")


class Apply(unittest.TestCase):
    CB = {"id": "x", "name": "CB", "category": "dlq", "key": "SECRET"}

    def test_entities_block_written_and_idempotent(self):
        cfg = "entities:\n  - { id: deye.pv, topic: nangluong/deye/pv_power }\n\ndevices: []\n"
        out = g.apply(cfg, [self.CB])
        self.assertIn("deye.pv", out)  # số đo viết tay giữ nguyên
        self.assertNotIn("SECRET", out)
        ents = out.split("entities:")[1].split("devices:")[0]
        self.assertIn('"tuya_x.w"', ents)
        self.assertIn(g.EBEGIN, ents)
        self.assertEqual(g.apply(out, [self.CB]), out)
        self.assertNotIn("tuya_x.w", g.apply(out, []))

    def test_entities_section_added_when_missing(self):
        out = g.apply("devices: []\n", [self.CB])
        self.assertIn("\nentities:\n" + g.EBEGIN, out)
        self.assertEqual(g.apply(out, [self.CB]), out)


if __name__ == "__main__":
    unittest.main()
