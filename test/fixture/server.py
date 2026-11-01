"""Deterministic file server used as a download source in JDWeb tests.

Python standard library only. Listens on port 8000.

* Any path returns a file of ``?size=<bytes>`` bytes (default 1 MiB). The
  content is a pure function of the path and the size, so equal requests
  return equal bytes. Content-Length and HEAD are supported.
* Paths below ``/slow/`` are throttled to about 2 MiB/s.
* ``/archiv.zip`` returns a genuine, valid ZIP archive built in memory.
* The file name derives from the path, so unique paths give unique links.
"""
import hashlib
import io
import posixpath
import time
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, quote, unquote, urlsplit

PORT = 8000
DEFAULT_SIZE = 1024 * 1024
MAX_SIZE = 1024 * 1024 * 1024
CHUNK = 64 * 1024
SLOW_RATE = 2 * 1024 * 1024  # bytes per second
ZIP_DATE = (2026, 1, 1, 0, 0, 0)


def build_zip():
    """Return a small valid ZIP archive with deterministic content."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        for name, text in (
            ("hello.txt", "Hello from the JDWeb fixture.\n" * 20),
            ("docs/readme.txt", "Second file inside the fixture archive.\n" * 20),
        ):
            info = zipfile.ZipInfo(name, ZIP_DATE)
            info.compress_type = zipfile.ZIP_DEFLATED
            zf.writestr(info, text)
    return buf.getvalue()


ZIP_BYTES = build_zip()


def block_for(path):
    """A 1 KiB block derived from the path; the file is this block repeated."""
    seed = hashlib.sha256(path.encode("utf-8")).hexdigest().encode("ascii")
    return (seed * 16)[:1024]


def file_name(path):
    name = posixpath.basename(path.rstrip("/"))
    # Drop control characters (CR, LF, NUL, ...) so a name can never inject headers.
    name = "".join(ch for ch in name if ch >= " " and ch != "\x7f")
    return name or "index.bin"


def content_disposition(name):
    """Build a header value that is safe for any file name.

    ``filename`` is an ASCII fallback (quotes, backslashes, percent signs and
    non-ASCII characters become ``_``); ``filename*`` carries the exact name as
    percent-encoded UTF-8 (RFC 6266 / RFC 8187).
    """
    fallback = "".join(
        ch if " " <= ch < "\x7f" and ch not in '"\\%' else "_" for ch in name
    )
    return "attachment; filename=\"%s\"; filename*=UTF-8''%s" % (fallback, quote(name, safe=""))


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _target(self):
        """Return (path, body_length, content_type, slow, chunk_iterator_factory).

        body_length is None when the request is invalid.
        """
        parts = urlsplit(self.path)
        path = unquote(parts.path)
        if path == "/archiv.zip":
            data = ZIP_BYTES
            return path, len(data), "application/zip", False, lambda: iter((data,))
        query = parse_qs(parts.query)
        try:
            size = int(query.get("size", [DEFAULT_SIZE])[0])
        except ValueError:
            size = -1
        if size < 0 or size > MAX_SIZE:
            return path, None, None, False, None
        block = block_for(path)

        def chunks():
            left = size
            full = (block * (CHUNK // len(block) + 1))[:CHUNK]
            while left > 0:
                n = min(left, CHUNK)
                yield full[:n]
                left -= n

        return path, size, "application/octet-stream", path.startswith("/slow/"), chunks

    def _send_head(self):
        path, length, ctype, slow, chunks = self._target()
        if length is None:
            self.send_error(400, "invalid size")
            return None
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(length))
        self.send_header("Content-Disposition", content_disposition(file_name(path)))
        self.send_header("Accept-Ranges", "none")
        self.end_headers()
        return slow, chunks

    def do_HEAD(self):
        self._send_head()

    def do_GET(self):
        res = self._send_head()
        if res is None:
            return
        slow, chunks = res
        try:
            for chunk in chunks():
                self.wfile.write(chunk)
                if slow:
                    time.sleep(len(chunk) / SLOW_RATE)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("", PORT), Handler).serve_forever()
