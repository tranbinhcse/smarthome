import json
import unittest
from types import SimpleNamespace

import tuya_qr_login as q


class T(unittest.TestCase):
    def test_mapping_from_local_strategy(self):
        dev = SimpleNamespace(local_strategy={
            1: {"status_code": "switch_1", "config_item": {"valueType": "Boolean", "valueDesc": "{}"}},
            19: {"status_code": "cur_power", "config_item": {"valueType": "Integer",
                                                             "valueDesc": '{"unit":"W","min":0,"max":99999,"scale":1,"step":1}'}},
            38: {"status_code": "relay_status", "config_item": {"valueType": "Enum", "valueDesc": ""}}})
        self.assertEqual(q.mapping_of(dev), {
            "1": {"code": "switch_1", "type": "Boolean", "values": {}},
            "19": {"code": "cur_power", "type": "Integer", "values": {"unit": "W", "min": 0, "max": 99999, "scale": 1, "step": 1}},
            "38": {"code": "relay_status", "type": "Enum", "values": {}}})
        json.dumps(q.mapping_of(dev))  # ghi được vào devices.json

    def test_no_local_strategy(self):
        self.assertEqual(q.mapping_of(SimpleNamespace(local_strategy={})), {})
        self.assertEqual(q.mapping_of(SimpleNamespace()), {})


if __name__ == "__main__":
    unittest.main()
