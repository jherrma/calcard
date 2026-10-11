"""HTTP layer: one ``Http`` per run (connection pooling, TLS settings and the
single place that handles HTTP 429) and ``Resp``, a raw view of one exchange.

Responses are never decoded into typed models: tests read ``status``,
``headers``, ``content`` (bytes), ``text`` and ``json()`` (plain
``json.loads``) so nothing the server sends can be filtered away.
"""

from __future__ import annotations

import base64
import json as _json
import ssl
import time
from urllib.parse import urlsplit

import httpcore
import httpx

from . import out
from .config import Config

NOT_JSON = object()

_DUMP_HEADERS = ("Location", "ETag", "WWW-Authenticate", "DAV", "Allow", "Retry-After", "Mcp-Session-Id")


class Resp:
    """One request/response exchange. ``status`` is 0 when the request
    itself failed (connection refused, timeout, TLS error); ``error`` then
    says why."""

    def __init__(self, *, method: str, url: str, auth_desc: str, req_body: str,
                 status: int = 0, headers: list[tuple[str, str]] | None = None,
                 content: bytes = b"", error: str = "", preview: int = 1200):
        self.method = method
        self.url = url
        self.auth_desc = auth_desc
        self.req_body = req_body
        self.status = status
        self.header_list = headers or []
        self.content = content
        self.error = error
        self._preview = preview
        self._json = None
        self._parsed = False

    # --- body -------------------------------------------------------------
    @property
    def text(self) -> str:
        """Body as text (UTF-8, undecodable bytes replaced, NUL bytes dropped)."""
        return self.content.decode("utf-8", errors="replace").replace("\x00", "")

    def json(self):
        """The body parsed with json.loads — or NOT_JSON."""
        if not self._parsed:
            self._parsed = True
            try:
                self._json = _json.loads(self.content)
            except ValueError:
                self._json = NOT_JSON
        return self._json

    # --- headers ----------------------------------------------------------
    def hdr(self, name: str) -> str:
        """Value of a response header (case-insensitive, last occurrence), '' if absent."""
        val = ""
        lname = name.lower()
        for k, v in self.header_list:
            if k.lower() == lname:
                val = v
        return val

    # --- diagnostics ------------------------------------------------------
    @property
    def request_line(self) -> str:
        return f"{self.method} {self.url}" + (f"  [{self.auth_desc}]" if self.auth_desc else "")

    def dump(self) -> str:
        """Human-readable summary of the exchange (printed for failures)."""
        d, r = out.DIM, out.RESET
        lines = [f"          {d}> {self.request_line}{r}"]
        if self.req_body:
            rb = self.req_body[:300].replace("\r", "").replace("\n", "\\n")
            lines.append(f"          {d}> body: {rb}{r}")
        if self.status == 0:
            lines.append(f"          < request failed: {self.error or 'unknown error'}")
            return "\n".join(lines)
        ct = self.hdr("Content-Type")
        lines.append(f"          < HTTP {self.status}" + (f"  ({ct})" if ct else ""))
        for h in _DUMP_HEADERS:
            if v := self.hdr(h):
                lines.append(f"          < {h}: {v}")
        body = self.text
        if body:
            if len(body) > self._preview:
                body = f"{body[:self._preview]}… ({len(self.text)} chars, truncated)"
            body = body.replace("\r", "").replace("\n", "\n                  ")
            lines.append(f"          < body: {body}")
        return "\n".join(lines)

    def __repr__(self) -> str:
        return f"<Resp {self.request_line} -> {self.status}>"


def _basic(user: str, password: str) -> str:
    return "Basic " + base64.b64encode(f"{user}:{password}".encode()).decode()


class Http:
    """All requests of a run go through ``request``. On HTTP 429 it waits
    Retry-After (capped by E2E_RATE_LIMIT_MAX_WAIT, at most
    E2E_RATE_LIMIT_RETRIES times) and repeats the request; ``rate_limit_hits``
    counts those retries for the summary."""

    def __init__(self, cfg: Config):
        self.cfg = cfg
        self.rate_limit_hits = 0
        self.client = httpx.Client(
            verify=not cfg.insecure,
            timeout=httpx.Timeout(cfg.timeout, connect=cfg.connect_timeout),
            follow_redirects=False,
            # curl-like defaults: no content negotiation, so what the server
            # sends is what the tests see.
            headers={"User-Agent": "calcard-e2e", "Accept": "*/*", "Accept-Encoding": "identity"},
        )
        self._raw_pool: httpcore.ConnectionPool | None = None

    def close(self) -> None:
        self.client.close()
        if self._raw_pool is not None:
            self._raw_pool.close()

    # ------------------------------------------------------------------
    def request(self, method: str, url: str, *, bearer: str | None = None,
                basic: tuple[str, str] | None = None, json=None, xml: str | None = None,
                ics: str | None = None, vcf: str | None = None, data: str | bytes | None = None,
                ctype: str | None = None, depth: int | str | None = None,
                headers: dict[str, str] | None = None, files: dict | None = None,
                retry: bool = True, path_as_is: bool = False) -> Resp:
        """Send one request.

        Body (at most one): ``json`` (object → serialized, str → sent as is),
        ``xml``/``ics``/``vcf`` (str, with the matching Content-Type),
        ``data`` (raw str/bytes, Content-Type only via ``ctype``) or ``files``
        (multipart, httpx format). ``path_as_is`` sends the path without
        normalizing ``..`` segments (traversal tests).
        """
        hdrs: dict[str, str] = {}
        body: bytes | None = None
        req_preview = ""
        auth_desc = ""
        ct = None
        if bearer is not None:
            hdrs["Authorization"] = f"Bearer {bearer}"
            auth_desc = "bearer"
        if basic is not None:
            hdrs["Authorization"] = _basic(*basic)
            auth_desc = f"basic {basic[0]}"
        if json is not None:
            text = json if isinstance(json, str) else _json.dumps(json, separators=(",", ":"), ensure_ascii=False)
            body, ct, req_preview = text.encode(), "application/json", text
        elif xml is not None:
            body, ct, req_preview = xml.encode(), "application/xml; charset=utf-8", xml
        elif ics is not None or vcf is not None:
            text = ics if ics is not None else vcf
            if text.endswith("\r"):
                text += "\n"
            ct = "text/calendar; charset=utf-8" if ics is not None else "text/vcard; charset=utf-8"
            body, req_preview = text.encode(), text
        elif data is not None:
            body = data if isinstance(data, bytes) else data.encode()
            req_preview = data if isinstance(data, str) else f"<{len(data)} bytes>"
        elif files is not None:
            req_preview = "<multipart " + ", ".join(files) + ">"
        if ctype is not None:
            ct = ctype
        if ct is not None:
            hdrs["Content-Type"] = ct
        if depth is not None:
            hdrs["Depth"] = str(depth)
        if headers:
            hdrs.update(headers)

        attempt = 0
        while True:
            resp = self._send(method, url, hdrs, body, files, path_as_is)
            resp.auth_desc, resp.req_body = auth_desc, req_preview
            out.vlog(1, f"{resp.request_line} -> {resp.status}")
            if out.verbose >= 2:
                if req_preview:
                    out.vlog(2, f">> {req_preview[:600]}")
                out.vlog(2, f"<< {resp.text[:600]}")
            if resp.status == 429 and retry and attempt < self.cfg.rate_limit_retries:
                attempt += 1
                self.rate_limit_hits += 1
                ra = resp.hdr("Retry-After")
                wait = int(ra) if ra.isdigit() else 10
                wait = min(max(wait, 1), self.cfg.rate_limit_max_wait)
                path = url[len(self.cfg.base_url):] if url.startswith(self.cfg.base_url) else url
                out.info(f"      rate limited (429) on {method} {path}; waiting {wait}s "
                         f"(retry {attempt}/{self.cfg.rate_limit_retries})")
                time.sleep(wait)
                continue
            return resp

    def _send(self, method, url, hdrs, body, files, path_as_is) -> Resp:
        base = dict(method=method, url=url, auth_desc="", req_body="", preview=self.cfg.body_preview)
        if path_as_is:
            return self._send_raw(method, url, hdrs, body, base)
        try:
            r = self.client.request(method, url, headers=hdrs, content=body, files=files)
            content = r.content  # HEAD: empty
            return Resp(status=r.status_code, headers=list(r.headers.multi_items()), content=content, **base)
        except httpx.HTTPError as e:
            return Resp(error=f"{type(e).__name__}: {e}", **base)

    def _send_raw(self, method, url, hdrs, body, base) -> Resp:
        """httpx always removes dot segments from paths; httpcore sends the
        request target verbatim."""
        if self._raw_pool is None:
            ctx = ssl.create_default_context()
            if self.cfg.insecure:
                ctx.check_hostname = False
                ctx.verify_mode = ssl.CERT_NONE
            self._raw_pool = httpcore.ConnectionPool(ssl_context=ctx)
        parts = urlsplit(url)
        target = parts.path + (f"?{parts.query}" if parts.query else "")
        port = parts.port or (443 if parts.scheme == "https" else 80)
        headers = {"Host": parts.netloc, "User-Agent": "calcard-e2e", "Accept": "*/*",
                   "Accept-Encoding": "identity", **hdrs}
        if body is not None:
            headers["Content-Length"] = str(len(body))
        timeout = {"connect": self.cfg.connect_timeout, "read": self.cfg.timeout,
                   "write": self.cfg.timeout, "pool": self.cfg.timeout}
        try:
            r = self._raw_pool.request(
                method,
                httpcore.URL(scheme=parts.scheme, host=parts.hostname, port=port, target=target),
                headers=list(headers.items()),
                content=body,
                extensions={"timeout": timeout},
            )
            hl = [(k.decode("latin-1"), v.decode("latin-1")) for k, v in r.headers]
            return Resp(status=r.status, headers=hl, content=r.content, **base)
        except httpcore.TimeoutException as e:
            return Resp(error=f"timeout: {e}", **base)
        except (httpcore.NetworkError, httpcore.ProtocolError) as e:
            return Resp(error=f"{type(e).__name__}: {e}", **base)
