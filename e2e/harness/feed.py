"""A throwaway local iCalendar feed for the subscription tests: an
``http.server`` in a background thread serving files from memory. The
CalCard server must be able to reach it, so the advertised URL uses an
address of THIS machine that the server can route to (auto-detected, or
E2E_FEED_HOST)."""

from __future__ import annotations

import socket
import threading
from collections import Counter
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from . import out
from .config import Config


class FeedUnavailable(Exception):
    """The feed cannot be served from here; the message is the skip reason."""


def _is_loopback(host: str) -> bool:
    return host == "localhost" or host.startswith("127.") or host in ("::1", "[::1]")


class FeedServer:
    def __init__(self, cfg: Config):
        self.cfg = cfg
        self.files: dict[str, tuple[bytes, str]] = {}
        self.hits: Counter[str] = Counter()
        self.url = ""
        self.unavailable = ""
        self._httpd: ThreadingHTTPServer | None = None
        self._thread: threading.Thread | None = None
        self._lock = threading.Lock()

    # --- lifecycle --------------------------------------------------------------
    def _detect_host(self) -> str:
        if self.cfg.feed_host:
            return self.cfg.feed_host
        target = self.cfg.host
        if _is_loopback(target):
            return "127.0.0.1"
        try:
            ip = socket.getaddrinfo(target, None, socket.AF_INET)[0][4][0]
            # The source address of the route to the target (no packet is sent).
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
                s.connect((ip, 9))
                return s.getsockname()[0]
        except OSError:
            raise FeedUnavailable(
                "cannot work out which local address the server could reach (set E2E_FEED_HOST)") from None

    def start(self) -> None:
        if self._httpd is not None:
            return
        if self.unavailable:
            raise FeedUnavailable(self.unavailable)
        try:
            if self.cfg.feed.lower() == "off":
                raise FeedUnavailable("disabled by E2E_FEED=off")
            host = self._detect_host()
            bind = self.cfg.feed_bind or ("127.0.0.1" if _is_loopback(host) else "0.0.0.0")
            port = int(self.cfg.feed_port) if self.cfg.feed_port else 0
            try:
                self._httpd = ThreadingHTTPServer((bind, port), self._handler())
            except (OSError, ValueError) as e:
                raise FeedUnavailable(f"could not start the feed server on {bind}:{port or 'random'} ({e})") from None
        except FeedUnavailable as e:
            self.unavailable = str(e)
            raise
        self._httpd.daemon_threads = True
        self._thread = threading.Thread(target=self._httpd.serve_forever, name="e2e-feed", daemon=True)
        self._thread.start()
        actual_port = self._httpd.server_address[1]
        h = f"[{host}]" if ":" in host else host
        self.url = f"http://{h}:{actual_port}"
        out.info(f"      local feed server on {bind}:{actual_port}, advertised as {self.url}")

    def stop(self) -> None:
        if self._httpd is not None:
            self._httpd.shutdown()
            self._httpd.server_close()
            self._httpd = None

    # --- content ----------------------------------------------------------------
    def put(self, name: str, content: str, ctype: str = "text/calendar; charset=utf-8") -> None:
        """Publish (or replace) a file."""
        with self._lock:
            self.files[name] = (content.encode(), ctype)

    def rm(self, name: str) -> None:
        """Make the file 404."""
        with self._lock:
            self.files.pop(name, None)

    def hit_count(self, name: str) -> int:
        """How many GETs the server received for NAME."""
        with self._lock:
            return self.hits[name]

    def _handler(self):
        feed = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):  # noqa: N802
                name = self.path.split("?", 1)[0].lstrip("/")
                with feed._lock:
                    feed.hits[name] += 1
                    entry = feed.files.get(name)
                if entry is None:
                    self.send_error(404)
                    return
                body, ctype = entry
                self.send_response(200)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):  # keep the terminal quiet
                pass

        return Handler
