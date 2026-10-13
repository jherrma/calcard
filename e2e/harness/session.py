"""The run: target server, test users, resource factories and the cleanup stack.

Every run registers fresh users named ``<prefix>-<run id>-<key>@<domain>``,
works only with data it created, and undoes everything on the cleanup stack
(LIFO) when the session ends — after failures and Ctrl-C too.
"""

from __future__ import annotations

import base64
import platform
import re
import secrets
import time
from typing import Any, Callable

import httpx
import lxml.etree
import pytest

from . import out
from .config import MAX_CRED_LABEL, Config
from .http import Http, Resp

ZERO_UUID = "00000000-0000-0000-0000-000000000000"

# 401 messages of the REST auth middleware that reject the ACCESS TOKEN itself
# (adapter/http/auth_middleware.go) — as opposed to a handler refusing the
# request (wrong password, …). Only these make User.api re-authenticate.
TOKEN_REJECTED = frozenset({"token revoked", "token expired", "invalid or expired token"})


class SetupFailed(AssertionError):
    """A fixture/factory step failed (fails the test that needed it)."""

    def __init__(self, msg: str, resp: Resp | None = None):
        super().__init__(f"✗ {msg}" + (f"\n{resp.dump()}" if resp is not None else ""))
        self.resp = resp


def abort_run(msg: str):
    """Stop the whole run (exit code 2), like a configuration error."""
    pytest.exit(msg, returncode=2)


def _gen_password() -> str:
    raw = base64.b64encode(secrets.token_bytes(18)).decode()
    return "E2e!" + "".join(c for c in raw if c.isalnum())[:16] + "9a"


class User:
    """A test account. Acts through ``api`` (REST with its JWT) and ``dav``
    (Basic auth with an app password created on first use)."""

    def __init__(self, e2e: E2E, key: str, display: str):
        self.e2e = e2e
        self.key = key
        self.display = display
        self.email = f"{e2e.cfg.run_tag}-{key.lower()}@{e2e.cfg.email_domain}"
        self.password = e2e.cfg.password or _gen_password()
        self.id = ""
        self.username = ""
        self.token = ""
        self.refresh = ""
        self.exp = 0
        self.apppw = ""
        self.apppw_id = ""
        self.deleted = False

    def __repr__(self) -> str:
        return f"<User {self.key} {self.email}>"

    # --- paths ----------------------------------------------------------------
    @property
    def home_cal(self) -> str:
        return f"/dav/{self.username}/calendars/"

    @property
    def home_ab(self) -> str:
        return f"/dav/{self.username}/addressbooks/"

    # --- session --------------------------------------------------------------
    def login(self, quiet: bool = False) -> bool:
        """Password login; stores the tokens."""
        r = self.e2e.anon("POST", "/auth/login", json={"email": self.email, "password": self.password})
        if r.status != 200 or not self._store_tokens(r):
            if not quiet:
                out.warn(f"login for {self.email} failed with HTTP {r.status}\n{r.dump()}")
            return False
        return True

    def _store_tokens(self, r: Resp) -> bool:
        """Take the tokens of a login/refresh response; False when the body
        carries no access token (not JSON, unexpected shape)."""
        data = r.jdict().get("data")
        data = data if isinstance(data, dict) else {}
        token = data.get("access_token")
        if not isinstance(token, str) or not token:
            return False
        self.token = token
        refresh = data.get("refresh_token")
        self.refresh = refresh if isinstance(refresh, str) else ""
        exp = data.get("expires_at")
        self.exp = int(exp) if isinstance(exp, (int, float)) else 0
        return True

    def ensure_token(self) -> None:
        """Keep the access token fresh (refresh 60s before expiry, fall back
        to a password login). Runs before every ``api`` call."""
        if self.deleted:
            return
        if self.token and time.time() < self.exp - 60:
            return
        self.reauth()

    def reauth(self) -> bool:
        """Get a new access token: /auth/refresh with the stored refresh
        token, else a password login."""
        if self.refresh:
            r = self.e2e.anon("POST", "/auth/refresh", json={"refresh_token": self.refresh})
            if r.status == 200 and self._store_tokens(r):
                out.vlog(1, f"refreshed access token for {self.key}")
                return True
        return self.login(quiet=self.e2e.in_cleanup)

    def ensure_dav(self) -> bool:
        """Create the user's DAV app password on first use."""
        if self.apppw:
            return True
        r = self.api("POST", "/app-passwords/", json={"name": self.e2e.uniq("dav"), "scopes": ["caldav", "carddav"]})
        data = r.jdict().get("data")
        if r.status in (200, 201) and isinstance(data, dict) and data.get("id") and data.get("password"):
            self.apppw = data["password"]
            self.apppw_id = data["id"]
            self.e2e.defer(f"revoke app password of {self.key}",
                           self.e2e.cleanup_api, self, "DELETE", f"/app-passwords/{self.apppw_id}")
            return True
        out.warn(f"could not create a DAV app password for {self.key} (HTTP {r.status})\n{r.dump()}")
        return False

    def require_dav(self) -> None:
        """ensure_dav for suite setup: raises SetupFailed when no app password
        can be created (every test of the suite then fails)."""
        if not self.ensure_dav():
            raise SetupFailed(f"could not create a DAV app password for {self.key}")

    def delete_account(self) -> bool:
        """DELETE /users/me (password + "DELETE" confirmation).

        Never guesses: an account counts as deleted only after this call (or
        a test that deleted it and set ``deleted``) saw the 200/204. A 401
        that survives ``api``'s re-login, a 429, a 5xx or a network error is
        a failed cleanup — reported, so leftovers are never silent."""
        if self.deleted:
            return True
        r = self.api("DELETE", "/users/me", json={"password": self.password, "confirmation": "DELETE"})
        self.e2e._last_cleanup_status = r.status
        if r.status in (200, 204):
            self.deleted = True
            return True
        return False

    # --- requests -------------------------------------------------------------
    def api(self, method: str, path: str, **kw) -> Resp:
        """REST call under /api/v1 with this user's JWT.

        Recovers ONCE from an access token the server rejects as such (401
        with one of TOKEN_REJECTED): since P02 (#189) a logout, password
        change or reset revokes every access token of the account, while the
        refresh token survives. Any other 401 — a wrong password in the body,
        a missing token — is returned unchanged, so authentication tests see
        exactly what the server said."""
        self.ensure_token()
        url = f"{self.e2e.base}/api/v1{path}"
        r = self.e2e.http.request(method, url, bearer=self.token, **kw)
        if r.status == 401 and not self.deleted and r.jmsg() in TOKEN_REJECTED:
            stale = self.token
            if self.reauth() and self.token != stale:
                self.e2e.reauths += 1
                out.vlog(1, f"access token of {self.key} was rejected ({r.jmsg()}); re-authenticated, retrying")
                r = self.e2e.http.request(method, url, bearer=self.token, **kw)
        return r

    def dav(self, method: str, path: str, **kw) -> Resp:
        """DAV call with Basic auth (e-mail + app password)."""
        self.ensure_dav()
        return self.e2e.http.request(method, f"{self.e2e.base}{path}", basic=(self.email, self.apppw), **kw)

    # --- factories (each registers its own cleanup) ---------------------------
    def mk_calendar(self, name: str, color: str = "#3366cc") -> dict:
        """POST /calendars/ → the raw calendar JSON (uuid, id, path, …)."""
        r = self.api("POST", "/calendars/", json={"name": name, "color": color,
                                                  "description": f"e2e {self.e2e.cfg.run_tag}"})
        cal = r.jdict()
        if r.status != 201 or not cal.get("uuid"):
            raise SetupFailed(f"creating calendar '{name}' as {self.key} failed (HTTP {r.status})", r)
        self.e2e.defer(f"delete calendar '{name}'", self.e2e.cleanup_api, self, "DELETE",
                       f"/calendars/{cal['uuid']}", json={"confirmation": "DELETE"})
        return cal

    def mk_addressbook(self, name: str) -> dict:
        """POST /addressbooks/ → the raw address book JSON (UUID, ID, Path, …)."""
        r = self.api("POST", "/addressbooks/", json={"name": name, "description": f"e2e {self.e2e.cfg.run_tag}"})
        ab = r.jdict()
        if r.status != 201 or not ab.get("UUID"):
            raise SetupFailed(f"creating address book '{name}' as {self.key} failed (HTTP {r.status})", r)
        self.e2e.defer(f"delete address book '{name}'", self.e2e.cleanup_api, self, "DELETE",
                       f"/addressbooks/{ab['UUID']}", json={"confirmation": "DELETE"})
        return ab

    def mk_event(self, cal_uuid: str, summary: str, start: str, end: str, **extra: Any) -> dict:
        """POST /calendars/{uuid}/events → the raw event JSON. Deleted with its calendar."""
        body = {"summary": summary, "start": start, "end": end, "timezone": "UTC", "all_day": False, **extra}
        r = self.api("POST", f"/calendars/{cal_uuid}/events", json=body)
        if r.status != 201 or not r.jdict().get("id"):
            raise SetupFailed(f"creating event '{summary}' failed (HTTP {r.status})", r)
        return r.jdict()

    def mk_contact(self, ab_uuid: str, formatted_name: str, **extra: Any) -> dict:
        """POST /addressbooks/{uuid}/contacts → the raw contact JSON."""
        r = self.api("POST", f"/addressbooks/{ab_uuid}/contacts", json={"formatted_name": formatted_name, **extra})
        if r.status != 201 or not r.jdict().get("id"):
            raise SetupFailed(f"creating contact '{formatted_name}' failed (HTTP {r.status})", r)
        return r.jdict()


class E2E:
    """Session object (the ``e2e`` fixture)."""

    def __init__(self, cfg: Config, env_file: str | None):
        self.cfg = cfg
        self.base = cfg.base_url
        self.env_file = env_file
        self.http = Http(cfg)
        self.users: dict[str, User] = {}
        self.cleanups: list[tuple[str, Callable[..., Any], tuple, dict]] = []
        self.cleanup_warnings: list[str] = []
        self.in_cleanup = False
        self._last_cleanup_status: int | None = None
        self.reauths = 0  # User.api re-logins after a revoked/expired access token
        self.server_registration = None
        self._feed = None

    # --- helpers --------------------------------------------------------------
    def uniq(self, label: str) -> str:
        """Run-unique name, e.g. "e2e-202610111015004821-LABEL"."""
        return f"{self.cfg.run_tag}-{label}"

    def cred_user(self, label: str) -> str:
        """Run-unique CalDAV/CardDAV credential username, e.g.
        "ececacgbabdbabfaaeicbcalro" for run "e2e-202610131015004821" and label
        "calro". New credential usernames may only hold lowercase letters a-z,
        so the run tag is spelled in letters: digits become a-j and '-' is
        dropped. The label is lowercase letters too."""
        if not re.fullmatch(r"[a-z]+", label) or len(label) > MAX_CRED_LABEL:
            raise ValueError(f"credential label must be 1-{MAX_CRED_LABEL} lowercase letters a-z (got {label!r})")
        tag = "".join(chr(ord("a") + int(c)) if c.isdigit() else c for c in self.cfg.run_tag if c != "-")
        return f"{tag}{label}"

    @property
    def run_id(self) -> str:
        return self.cfg.run_id

    @property
    def domain(self) -> str:
        return self.cfg.email_domain

    def req(self, method: str, url: str, **kw) -> Resp:
        """Any absolute URL, credentials only as given."""
        return self.http.request(method, url, **kw)

    def raw(self, method: str, path: str, **kw) -> Resp:
        """Any path on the server, no credentials."""
        return self.http.request(method, f"{self.base}{path}", **kw)

    def anon(self, method: str, path: str, **kw) -> Resp:
        """REST call under /api/v1 without credentials."""
        return self.http.request(method, f"{self.base}/api/v1{path}", **kw)

    # --- start-up -------------------------------------------------------------
    def start(self) -> None:
        cfg = self.cfg
        out.line(f"{out.BOLD}CalCard e2e{out.RESET} — target {out.BOLD}{cfg.base_url}{out.RESET}  run {cfg.run_tag}"
                 + (f"  {out.DIM}(config: {self.env_file}){out.RESET}" if self.env_file else ""))
        out.info(f"python {platform.python_version()}, pytest {pytest.__version__}, httpx {httpx.__version__}, "
                 f"lxml {'.'.join(map(str, lxml.etree.LXML_VERSION[:3]))}")
        for w in cfg.warnings:
            out.warn(w)
        self.wait_for_health()
        self.preflight()

    def wait_for_health(self) -> None:
        deadline = time.time() + self.cfg.health_wait
        shown = False
        while True:
            r = self.raw("GET", "/health", retry=False)
            body = r.json()
            if r.status == 200 and isinstance(body, dict) and body.get("status") == "ok":
                out.info("server healthy (GET /health)")
                return
            if time.time() >= deadline:
                abort_run(
                    f"server at {self.base} did not become healthy within {self.cfg.health_wait}s "
                    f"(last: HTTP {r.status or '000'}{', ' + r.error if r.error else ''}).\n"
                    "       Check CALCARD_URL / E2E_INSECURE (self-signed TLS) / network reachability."
                )
            if not shown:
                out.info(f"waiting for {self.base}/health (up to {self.cfg.health_wait}s)…")
                shown = True
            time.sleep(2)

    def preflight(self) -> None:
        """Registration must be possible without e-mail verification."""
        r = self.anon("GET", "/system/settings")
        data = r.jdict().get("data")
        if r.status != 200 or not isinstance(data, dict) or "registration_enabled" not in data:
            abort_run(f"GET {self.base}/api/v1/system/settings answered HTTP {r.status} without the CalCard "
                      f"settings JSON — is CALCARD_URL pointing at a CalCard server?\n{r.dump()}")
        self.server_registration = data.get("registration_enabled")
        if self.server_registration is not True:
            abort_run(
                "Self-registration is disabled on the target (registration_enabled=false).\n"
                "       The suite registers a fresh, uniquely named test user on every run and needs it enabled:\n"
                "       set CALDAV_REGISTRATION_DISABLED=false (registration.disabled: false) on the server, "
                "restart it, and re-run."
            )

    # --- users ----------------------------------------------------------------
    def create_user(self, key: str, display: str | None = None) -> User:
        """Register + log in a fresh user; its deletion is registered as a
        cleanup. Raises SetupFailed on error."""
        u = User(self, key, display or f"E2E {key} {self.cfg.run_id}")
        r = self.anon("POST", "/auth/register",
                      json={"email": u.email, "password": u.password, "display_name": u.display})
        if r.status == 403:
            abort_run(f"Registration was refused (HTTP 403: {r.jmsg()}). "
                      "Enable self-registration on the target (CALDAV_REGISTRATION_DISABLED=false).")
        if r.status not in (200, 201):
            raise SetupFailed(f"registering {u.email} failed with HTTP {r.status}", r)
        data = r.jdict().get("data")
        if not isinstance(data, dict) or not data.get("email"):
            raise SetupFailed(f"registering {u.email} answered HTTP {r.status} without the account JSON", r)
        u.id = data.get("id") or ""
        u.username = data.get("username") or ""
        # Deleting the account is the very last cleanup for this user.
        self.defer(f"delete test account {u.email}", u.delete_account)
        if not u.login():
            raise SetupFailed(f"login for {u.email} failed right after registration")
        self.users[key] = u
        return u

    def ensure_user(self, key: str) -> User:
        """The shared users U1 / U2 (created once per run). A failure stops the run."""
        if key in self.users:
            return self.users[key]
        try:
            return self.create_user(key)
        except SetupFailed as e:
            abort_run(f"could not create test user {key}:\n{e}")

    # --- cleanup --------------------------------------------------------------
    def defer(self, desc: str, fn: Callable[..., Any], *args, **kwargs) -> None:
        """Register an undo action (run LIFO at the end of the session). It
        must return a truthy value on success."""
        self.cleanups.append((desc, fn, args, kwargs))

    def cleanup_api(self, user: User, method: str, path: str, **kw) -> bool:
        """Cleanup helper: 2xx or 404 (already gone) count as success."""
        if user.deleted:
            return True
        r = user.api(method, path, **kw)
        self._last_cleanup_status = r.status
        if 200 <= r.status < 300 or r.status == 404:
            return True
        # Some handlers answer "not found" with a 400 (see FINDINGS.md F12).
        return r.status == 400 and "not found" in r.text.lower()

    def run_cleanups(self) -> None:
        n = len(self.cleanups)
        if n == 0:
            return
        if self.cfg.keep_data:
            out.warn(f"E2E_KEEP_DATA=true: skipping {n} cleanup action(s); test data stays on the server.")
            self.cleanups = []
            return
        out.line()
        out.line(f"{out.BOLD}== cleanup{out.RESET} {out.DIM}({n} action(s)){out.RESET}")
        self.in_cleanup = True
        while self.cleanups:
            desc, fn, args, kwargs = self.cleanups.pop()
            out.vlog(1, f"cleanup: {desc}")
            self._last_cleanup_status = None
            try:
                ok = fn(*args, **kwargs)
            except Exception as e:  # never let one cleanup stop the others
                ok = False
                desc = f"{desc} ({type(e).__name__}: {e})"
            if not ok:
                status = self._last_cleanup_status
                self.cleanup_warnings.append(f"{desc} (last status {status or '?'})")
                out.warn(f"cleanup failed: {desc} (last HTTP status {status or '?'})")
        self.in_cleanup = False
        if not self.cleanup_warnings:
            out.info("  all test data removed")

    # --- feed server ------------------------------------------------------------
    def feed(self):
        """The local iCalendar feed server (started on first use). Raises
        FeedUnavailable with the reason when it cannot run here."""
        from .feed import FeedServer

        if self._feed is None:
            self._feed = FeedServer(self.cfg)
        self._feed.start()
        return self._feed

    def close(self) -> None:
        if self._feed is not None:
            self._feed.stop()
        self.http.close()
