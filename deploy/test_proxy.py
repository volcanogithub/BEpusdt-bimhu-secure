"""Local deployment contract tests: Python 3, OpenSSL and Nginx >= 1.19.3.

Run from the cloud checkout: NGINX_BIN=/path/to/nginx python3 deploy/test_proxy.py
Certificates and configuration are generated in a private temporary directory.
"""
import http.client
import http.server
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import tempfile
import threading
import time
import unittest
from unittest.mock import patch
import urllib.error
import urllib.request


def port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Echo(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", "0")))
        self.do_GET()

    def do_GET(self):
        self.send_response(500 if self.path == "/failure" else 200)
        self.send_header("Set-Cookie", "session=test-only; Path=/")
        self.end_headers()
        self.wfile.write(json.dumps(dict(self.headers)).encode())

    def log_message(self, *_):
        pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None


class ProxyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory(prefix="bepusdt-proxy-")
        cls.addClassCleanup(cls.directory.cleanup)
        cls.root = Path(cls.directory.name)
        cls.nginx = os.environ.get("NGINX_BIN", "nginx")
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                        "-keyout", str(cls.root / "tls.key"), "-out", str(cls.root / "tls.crt"),
                        "-days", "1", "-subj", "/CN=example.invalid", "-addext",
                        "subjectAltName=DNS:example.invalid"], check=True, capture_output=True)
        (cls.root / "tls.key").chmod(0o600)
        cls.http_port, cls.https_port = port(), port()
        cls.backend = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Echo)
        cls.addClassCleanup(cls.backend.server_close)
        cls.addClassCleanup(cls.backend.shutdown)
        threading.Thread(target=cls.backend.serve_forever, daemon=True).start()
        template = Path(__file__).with_name("nginx.production.conf").read_text()
        cls.config = template.replace("listen 80", f"listen 127.0.0.1:{cls.http_port}")
        cls.config = cls.config.replace("listen 443", f"listen 127.0.0.1:{cls.https_port}")
        cls.config = cls.config.replace("/run/secrets/tls.", str(cls.root / "tls."))
        cls.config = cls.config.replace("127.0.0.1:8080", f"127.0.0.1:{cls.backend.server_port}")
        prefix = f"pid {cls.root}/nginx.pid;\nerror_log {cls.root}/error.log;\nevents {{}}\nhttp {{\naccess_log off;\n"
        for name in ["client_body", "proxy", "fastcgi", "uwsgi", "scgi"]:
            prefix += f"{name}_temp_path {cls.root}/{name};\n"
        cls.config = prefix + cls.config + "\n}\n"
        cls.path = cls.root / "nginx.conf"
        cls.path.write_text(cls.config)
        cls.command = [cls.nginx, "-p", str(cls.root), "-c", str(cls.path)]
        subprocess.run(cls.command + ["-t"], check=True, capture_output=True)
        subprocess.run(cls.command, check=True, capture_output=True)
        cls.addClassCleanup(lambda: subprocess.run(cls.command + ["-s", "quit"], capture_output=True))
        cls.context = ssl.create_default_context(cafile=str(cls.root / "tls.crt"))
        original = socket.getaddrinfo
        cls.resolver = patch("socket.getaddrinfo", side_effect=lambda host, *a, **kw:
                             original("127.0.0.1" if host == "example.invalid" else host, *a, **kw))
        cls.resolver.start()
        cls.addClassCleanup(cls.resolver.stop)
        cls.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                     urllib.request.HTTPSHandler(context=cls.context), NoRedirect())
        for _ in range(50):
            try:
                with socket.create_connection(("127.0.0.1", cls.https_port), timeout=0.1):
                    break
            except OSError:
                time.sleep(0.02)

    def test_https_normal_headers_and_cookie(self):
        response = self.opener.open(f"https://example.invalid:{self.https_port}/", timeout=5)
        self.assertEqual(response.status, 200)
        for name in ["X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy",
                     "Referrer-Policy", "Strict-Transport-Security"]:
            self.assertTrue(response.headers.get(name))
        for flag in ["Secure", "HttpOnly", "SameSite=Strict"]:
            self.assertIn(flag, response.headers["Set-Cookie"])

    def test_forwarded_headers_are_overwritten(self):
        headers = {name: "attacker-controlled" for name in ["X-Forwarded-Proto", "X-Forwarded-For",
                   "X-Real-IP", "X-Forwarded-Ssl", "Front-End-Https", "X-Url-Scheme", "CF-Visitor", "Forwarded"]}
        request = urllib.request.Request(f"https://example.invalid:{self.https_port}/", headers=headers)
        echo = {k.lower(): v for k, v in json.load(self.opener.open(request, timeout=5)).items()}
        self.assertEqual(echo["host"], "example.invalid")
        self.assertEqual(echo["x-forwarded-proto"], "https")
        self.assertEqual(echo["x-forwarded-for"], "127.0.0.1")
        for name in ["x-forwarded-ssl", "front-end-https", "x-url-scheme", "cf-visitor", "forwarded"]:
            self.assertNotIn(name, echo)

    def test_redirect_and_error_headers(self):
        with self.assertRaises(urllib.error.HTTPError) as failure:
            self.opener.open(f"http://example.invalid:{self.http_port}/path", timeout=5)
        self.assertEqual(failure.exception.code, 308)
        self.assertEqual(failure.exception.headers["Location"], "https://example.invalid/path")
        with self.assertRaises(urllib.error.HTTPError) as failure:
            self.opener.open(f"https://example.invalid:{self.https_port}/failure", timeout=5)
        self.assertEqual(failure.exception.code, 500)
        self.assertEqual(failure.exception.headers["Referrer-Policy"], "no-referrer")

    def test_wrong_host_and_missing_tls_key_fail(self):
        request = urllib.request.Request(f"http://127.0.0.1:{self.http_port}/", headers={"Host": "attacker.invalid"})
        with self.assertRaises((urllib.error.URLError, http.client.RemoteDisconnected)):
            self.opener.open(request, timeout=5)
        invalid = self.root / "invalid.conf"
        invalid.write_text(self.config.replace(str(self.root / "tls.key"), str(self.root / "missing.key")))
        result = subprocess.run([self.nginx, "-p", str(self.root), "-c", str(invalid), "-t"], capture_output=True)
        self.assertNotEqual(result.returncode, 0)

    def test_body_limit_normal_and_boundary_failure(self):
        url = f"https://example.invalid:{self.https_port}/"
        with self.opener.open(urllib.request.Request(url, data=b"x" * (1024 * 1024)), timeout=5) as response:
            self.assertEqual(response.status, 200)
        with self.assertRaises(urllib.error.HTTPError) as failure:
            self.opener.open(urllib.request.Request(url, data=b"x" * (1024 * 1024 + 1)), timeout=5)
        self.assertEqual(failure.exception.code, 413)
        self.assertEqual(failure.exception.headers["X-Content-Type-Options"], "nosniff")


if __name__ == "__main__":
    unittest.main(verbosity=2)
