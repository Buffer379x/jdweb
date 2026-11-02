"""Deterministic file server used as a download source in JDWeb tests.

Python standard library only. Listens on port 8000.

* Any path returns a file of ``?size=<bytes>`` bytes (default 1 MiB). The
  content is a pure function of the path and the size, so equal requests
  return equal bytes. Content-Length and HEAD are supported.
* Paths below ``/slow/`` are throttled to about 2 MiB/s.
* ``/archiv.zip`` returns a genuine, valid ZIP archive built in memory.
* ``/archiv-pw.zip`` returns the same files in a ZIP archive that is encrypted
  with the password ``jdweb-pw`` (traditional ZipCrypto, which every extractor
  reads; the standard library can only decrypt it, so it is written here).
* The file name derives from the path, so unique paths give unique links.
"""
import hashlib
import io
import posixpath
import struct
import time
import zlib
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

ZIP_PASSWORD = "jdweb-pw"
ZIP_FILES = (
    ("hello.txt", "Hello from the JDWeb fixture.\n" * 20),
    ("docs/readme.txt", "Second file inside the fixture archive.\n" * 20),
)


class _ZipCrypto:
    """The traditional PKWARE stream cipher (APPNOTE 6.1)."""

    def __init__(self, password):
        self.k = [0x12345678, 0x23456789, 0x34567890]
        for ch in password.encode("utf-8"):
            self._update(ch)

    @staticmethod
    def _crc(old, byte):
        return zlib.crc32(bytes((byte,)), old ^ 0xFFFFFFFF) ^ 0xFFFFFFFF

    def _update(self, byte):
        k = self.k
        k[0] = self._crc(k[0], byte)
        k[1] = ((k[1] + (k[0] & 0xFF)) * 134775813 + 1) & 0xFFFFFFFF
        k[2] = self._crc(k[2], k[1] >> 24)

    def encrypt(self, data):
        out = bytearray()
        for byte in data:
            temp = (self.k[2] | 2) & 0xFFFF
            key = ((temp * (temp ^ 1)) >> 8) & 0xFF
            out.append(byte ^ key)
            self._update(byte)
        return bytes(out)


def build_encrypted_zip(password=ZIP_PASSWORD):
    """A valid ZIP with ZipCrypto-encrypted, deflated entries (deterministic)."""
    out = io.BytesIO()
    central = []
    for name, text in ZIP_FILES:
        raw = text.encode("utf-8")
        crc = zlib.crc32(raw) & 0xFFFFFFFF
        comp = zlib.compressobj(9, zlib.DEFLATED, -15)
        packed = comp.compress(raw) + comp.flush()
        # 12 byte header: eleven fixed bytes and the high byte of the CRC as check.
        header = bytes((i * 7 + 3) & 0xFF for i in range(11)) + bytes((crc >> 24,))
        cipher = _ZipCrypto(password)
        body = cipher.encrypt(header + packed)
        nm = name.encode("utf-8")
        dos_time, dos_date = 0, ((2026 - 1980) << 9) | (1 << 5) | 1
        flags = 0x0001 | 0x0800  # encrypted, UTF-8 names
        offset = out.tell()
        out.write(struct.pack("<IHHHHHIIIHH", 0x04034B50, 20, flags, 8, dos_time, dos_date, crc, len(body), len(raw), len(nm), 0))
        out.write(nm)
        out.write(body)
        central.append((nm, flags, dos_time, dos_date, crc, len(body), len(raw), offset))
    start = out.tell()
    for nm, flags, t, d, crc, csize, usize, offset in central:
        out.write(struct.pack("<IHHHHHHIIIHHHHHII", 0x02014B50, 20, 20, flags, 8, t, d, crc, csize, usize, len(nm), 0, 0, 0, 0, 0, offset))
        out.write(nm)
    size = out.tell() - start
    out.write(struct.pack("<IHHHHIIH", 0x06054B50, 0, 0, len(central), len(central), size, start, 0))
    return out.getvalue()


ZIP_PW_BYTES = build_encrypted_zip()


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
        if path in ("/archiv.zip", "/archiv-pw.zip"):
            data = ZIP_BYTES if path == "/archiv.zip" else ZIP_PW_BYTES
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
