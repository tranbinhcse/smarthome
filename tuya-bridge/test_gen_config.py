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
        import json
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
                                         {"id": "x3", "name": "C"}], {"x2": {"kind": "light"}})
        self.assertIn('"kind": "dlq"', out)
        self.assertIn('"kind": "light"', out)
        self.assertEqual(out.count('"kind"'), 2)  # không có category thì không ghi kind

if __name__ == "__main__":
    unittest.main()
