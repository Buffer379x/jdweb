"""Checks the archives of the fixture server with the standard library.

Run: python3 test/fixture/test_archives.py
"""
import http.client
import io
import os
import sys
import threading
import zipfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import server  # noqa: E402

srv = server.ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
PORT = srv.server_address[1]
threading.Thread(target=srv.serve_forever, daemon=True).start()


def fetch(path):
    c = http.client.HTTPConnection("127.0.0.1", PORT)
    c.request("GET", path)
    r = c.getresponse()
    return r.status, r.getheader("Content-Type"), r.read()


ok = True


def check(name, good):
    global ok
    ok &= bool(good)
    print(name, "OK" if good else "FAIL")


status, ctype, body = fetch("/archiv.zip")
check("plain zip", status == 200 and ctype == "application/zip" and zipfile.ZipFile(io.BytesIO(body)).testzip() is None)

status, ctype, body = fetch("/archiv-pw.zip")
z = zipfile.ZipFile(io.BytesIO(body))
check("encrypted zip served", status == 200 and ctype == "application/zip" and all(i.flag_bits & 1 for i in z.infolist()))
z.setpassword(server.ZIP_PASSWORD.encode())
check("right password reads both files", z.testzip() is None and z.read("hello.txt").startswith(b"Hello from"))
z.setpassword(b"wrong")
try:
    z.read("hello.txt")
    check("wrong password is refused", False)
except RuntimeError:
    check("wrong password is refused", True)
check("deterministic", server.build_encrypted_zip() == body)
status, ctype, body = fetch("/archiv-big.zip")
z = zipfile.ZipFile(io.BytesIO(body))
check("big zip is small and holds 768 MiB", status == 200 and len(body) < 4 << 20 and z.infolist()[0].file_size == server.BIG_MIB << 20)


def head(path):
    c = http.client.HTTPConnection("127.0.0.1", PORT)
    c.request("HEAD", path)
    return c.getresponse().status


check("offline link", head("/offline/a.bin") == 404 and fetch("/offline/a.bin")[0] == 404)
check("gone link: online at the check, gone at the download", head("/gone/a.bin") == 200 and fetch("/gone/a.bin")[0] == 404)
print("ALL OK" if ok else "FAILURES")
sys.exit(0 if ok else 1)
