import json, os, socket, threading, unittest
import gia_lap_inverter as g

FIXTURE = os.path.join(os.path.dirname(__file__), "..", "firmware", "inverter", "fixtures", "deye-1pha.json")


def broker(srv, got):
    """Broker MQTT giả: nhận CONNECT, trả CONNACK, gom các PUBLISH tới khi DISCONNECT."""
    c, _ = srv.accept()
    f = c.makefile("rb")
    while True:
        head = f.read(1)
        if not head:
            break
        n, mul = 0, 1
        while True:
            b = f.read(1)[0]
            n += (b & 0x7F) * mul
            mul *= 128
            if b < 0x80:
                break
        body = f.read(n)
        if head[0] == 0x10:
            got["connect"] = body
            c.sendall(b"\x20\x02\x00\x00")
        elif head[0] & 0xF0 == 0x30:
            tl = int.from_bytes(body[:2], "big")
            got[body[2:2 + tl].decode()] = body[2 + tl:].decode()
        elif head[0] == 0xE0:
            break
    c.close()


class TestGiaLap(unittest.TestCase):
    def test_remaining_length(self):
        self.assertEqual(g.varint(0), b"\x00")
        self.assertEqual(g.varint(127), b"\x7f")
        self.assertEqual(g.varint(321), b"\xc1\x02")

    def test_publishes_whole_fixture(self):
        srv = socket.socket()
        srv.bind(("127.0.0.1", 0))
        srv.listen(1)
        got = {}
        t = threading.Thread(target=broker, args=(srv, got))
        t.start()
        expect = json.load(open(FIXTURE))["expect"]
        g.publish_all("127.0.0.1", srv.getsockname()[1], "node", "bí-mật", g.messages(expect))
        t.join(5)
        self.assertIn(b"node", got["connect"])
        self.assertEqual(got["nangluong/status"], "online")
        for k, v in expect.items():
            self.assertEqual(float(got["nangluong/inverter/" + k]), v, k)
        self.assertEqual(got["nangluong/inverter/grid_power"], "-350")


if __name__ == "__main__":
    unittest.main()
