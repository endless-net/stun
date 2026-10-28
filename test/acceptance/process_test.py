#!/usr/bin/env python3
"""Real executable acceptance; run separately from the short CI gate."""
import os
import signal
import socket
import struct
import subprocess
import time
import unittest
import urllib.request


def free_port(family, kind):
    with socket.socket(family, kind) as sock:
        sock.bind(("127.0.0.1" if family == socket.AF_INET else "::1", 0))
        return sock.getsockname()[1]


class ExecutableAcceptance(unittest.TestCase):
    binary = os.environ["STUN_BINARY"]

    def test_config_and_version_do_not_bind(self):
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as occupied:
            occupied.bind(("127.0.0.1", 0))
            address = "127.0.0.1:" + str(occupied.getsockname()[1])
            for mode in ("--version", "--check-config"):
                result = subprocess.run([self.binary, "--listen", address, mode], capture_output=True, timeout=2)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_process_binding_and_sigterm(self):
        udp = free_port(socket.AF_INET, socket.SOCK_DGRAM)
        http = free_port(socket.AF_INET, socket.SOCK_STREAM)
        process = subprocess.Popen([self.binary, "--listen", f"0.0.0.0:{udp},[::]:{udp}",
                                    "--http-listen", f"127.0.0.1:{http}"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 5
            while True:
                try:
                    with urllib.request.urlopen(f"http://127.0.0.1:{http}/readyz", timeout=0.2) as response:
                        self.assertEqual(response.status, 200)
                        break
                except OSError:
                    self.assertIsNone(process.poll(), "process failed during startup")
                    self.assertLess(time.monotonic(), deadline)
                    time.sleep(0.01)
            transaction = b"independent!"
            request = struct.pack("!HHI", 1, 0, 0x2112A442) + transaction
            for family, host in ((socket.AF_INET, "127.0.0.1"), (socket.AF_INET6, "::1")):
                with socket.socket(family, socket.SOCK_DGRAM) as client:
                    client.settimeout(1)
                    client.connect((host, udp))
                    client.send(request)
                    response = client.recv(1501)
                    self.assertEqual(len(response), 32 if family == socket.AF_INET else 44)
                    self.assertEqual(response[8:20], transaction)
                    self.assertEqual(struct.unpack("!H", response[26:28])[0] ^ 0x2112, client.getsockname()[1])
                    mask = struct.pack("!I", 0x2112A442) + transaction
                    self.assertEqual(bytes(a ^ b for a, b in zip(response[28:], mask)), socket.inet_pton(family, client.getsockname()[0]))
            started = time.monotonic()
            process.send_signal(signal.SIGTERM)
            stdout, stderr = process.communicate(timeout=5)
            self.assertEqual(process.returncode, 0, stderr)
            self.assertLess(time.monotonic() - started, 5)
            self.assertNotIn(b"127.0.0.1", stderr)
            self.assertNotIn(transaction, stdout + stderr)
            for family, host in ((socket.AF_INET, "0.0.0.0"), (socket.AF_INET6, "::")):
                with socket.socket(family, socket.SOCK_DGRAM) as reused:
                    if family == socket.AF_INET6:
                        reused.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
                    reused.bind((host, udp))
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate(timeout=2)

    def test_failed_second_listener_releases_first(self):
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as occupied:
            occupied.bind(("127.0.0.1", 0))
            first = free_port(socket.AF_INET, socket.SOCK_DGRAM)
            result = subprocess.run([self.binary, "--listen", f"127.0.0.1:{first},127.0.0.1:{occupied.getsockname()[1]}"], capture_output=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn(b"127.0.0.1", result.stderr)
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as reused:
                reused.bind(("127.0.0.1", first))


if __name__ == "__main__":
    unittest.main()
