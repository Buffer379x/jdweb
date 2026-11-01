"""Hostile file name checks for the fixture server.

Run: python3 test/fixture/test_hostile_names.py
"""
import http.client
import os
import sys
import threading
from urllib.parse import quote, unquote

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import server  # noqa: E402

srv = server.ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
PORT = srv.server_address[1]
threading.Thread(target=srv.serve_forever, daemon=True).start()
cases = {
    "script": "/" + quote("<script>alert('x')</b>.bin".replace("/", "_"), safe=""),
    "long500": "/" + "a" * 496 + ".bin",
    "rtl": "/" + quote("שלום‮مرحبا.bin", safe=""),
    "emoji": "/" + quote("\U0001f600\U0001f4be.bin", safe=""),
    "crlf": "/a%0d%0aX-Injected:%201.bin",
    "quote": "/a%22b%5c%25c.bin",
}
ok = True
for name, path in cases.items():
    c = http.client.HTTPConnection("127.0.0.1", PORT)
    c.request("GET", path + "?size=100")
    r = c.getresponse(); body = r.read()
    cd = r.getheader("Content-Disposition")
    cdv = r.getheader("Content-Disposition")
    inj = r.getheader("X-Injected")
    fstar = cd.split("filename*=UTF-8''")[1]
    good = r.status == 200 and len(body) == 100 and inj is None and "\r" not in cd and "\n" not in cd and cd.isascii()
    expected = "".join(ch for ch in unquote(path[1:]) if ch >= " " and ch != "\x7f")
    good = good and unquote(fstar) == expected
    # HEAD too
    c.request("HEAD", path + "?size=100"); h = c.getresponse(); h.read()
    good = good and h.status == 200 and h.getheader("Content-Length") == "100"
    ok &= good
    print(name, r.status, len(body), "OK" if good else "FAIL", cd[:110])
print("ALL OK" if ok else "FAILURES")
sys.exit(0 if ok else 1)
