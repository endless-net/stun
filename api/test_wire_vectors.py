"""Offline standard-wire fixtures, without a production STUN server."""
import ipaddress
import json
import struct
import unittest
from pathlib import Path


class BindingVectors(unittest.TestCase):
    def test_supported_wire_subset(self):
        profile = json.loads(Path(__file__).with_name("vectors.json").read_text())
        self.assertEqual(profile["max_datagram_bytes"], 1500)
        for vector in profile["vectors"]:
            with self.subTest(vector=vector["id"]):
                request = bytes.fromhex(vector["request_hex"]) + bytes(vector.get("append_zero_bytes", 0))
                accepted = 20 <= len(request) <= profile["max_datagram_bytes"]
                if accepted:
                    kind, length, cookie = struct.unpack("!HHI", request[:8])
                    accepted = kind == 1 and cookie == 0x2112A442 and length == len(request) - 20 and length % 4 == 0
                offset = 20
                while accepted and offset < len(request):
                    if offset + 4 > len(request):
                        accepted = False
                        break
                    kind, length = struct.unpack("!HH", request[offset:offset + 4])
                    offset += 4 + (length + 3) // 4 * 4
                    if kind < 0x8000 or offset > len(request):
                        accepted = False
                self.assertEqual(accepted, vector["outcome"] == "success")
                if not accepted:
                    self.assertNotIn("response_hex", vector)
                    continue
                response = bytes.fromhex(vector["response_hex"])
                kind, length, cookie = struct.unpack("!HHI", response[:8])
                self.assertEqual((kind, length, cookie), (0x0101, len(response) - 20, 0x2112A442))
                self.assertEqual(response[8:20], request[8:20])
                attr, length = struct.unpack("!HH", response[20:24])
                self.assertEqual(attr, 0x0020)
                self.assertEqual(length, len(response) - 24)
                address = ipaddress.ip_address(vector["source_address"])
                self.assertEqual(response[24:26], bytes([0, 1 if address.version == 4 else 2]))
                self.assertEqual(struct.unpack("!H", response[26:28])[0] ^ 0x2112, vector["source_port"])
                mask = bytes.fromhex("2112a442") + request[8:20]
                self.assertEqual(bytes(a ^ b for a, b in zip(response[28:], mask)), address.packed)


if __name__ == "__main__":
    unittest.main()
