"""Configuration: ``e2e/.env`` (KEY=VALUE) with real environment variables winning.

The file is parsed, never executed: no command substitution, no variable
expansion. A key that is already set in the environment (even to an empty
value) keeps its environment value.
"""

from __future__ import annotations

import os
import random
import re
import time
from dataclasses import dataclass
from pathlib import Path

E2E_DIR = Path(__file__).resolve().parent.parent

_LINE = re.compile(r"^([A-Za-z_][A-Za-z0-9_]*)=(.*)$")


class ConfigError(Exception):
    """The run cannot start (exit code 2)."""


TRUE_WORDS = {"1", "true", "yes", "on", "y"}
FALSE_WORDS = {"0", "false", "no", "off", "n"}

# The longest name the suite derives from the run tag is a DAV credential
# username: the tag spelled in letters without its '-' (at most
# MAX_RUN_TAG - 1 characters) plus a label of at most MAX_CRED_LABEL letters
# (E2E.cred_user). The server caps credential usernames at 50 characters
# (domain/user.ValidateDAVCredentialUsername). Everything else the tag ends up
# in (e-mails, calendar names, UIDs) allows more.
MAX_CRED_LABEL = 12
MAX_RUN_TAG = 50 - MAX_CRED_LABEL + 1


def is_true(value: str | None) -> bool:
    return (value or "").strip().lower() in TRUE_WORDS


def load_env_file() -> tuple[str | None, list[str]]:
    """Load E2E_ENV_FILE (default e2e/.env) into os.environ without
    overriding variables that are already set. Returns (path used, warnings)."""
    explicit = os.environ.get("E2E_ENV_FILE")
    path = Path(explicit) if explicit else E2E_DIR / ".env"
    if not path.is_file():
        if explicit:
            raise ConfigError(f"E2E_ENV_FILE={explicit} does not exist")
        return None, []
    warnings: list[str] = []
    for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = line.rstrip("\r")
        if re.match(r"^\s*(#|$)", line):
            continue
        line = line.lstrip()
        if line.startswith("export "):
            line = line[len("export "):]
        m = _LINE.match(line)
        if not m:
            warnings.append(f"{path}:{n}: ignoring malformed line (expected KEY=VALUE)")
            continue
        key, val = m.group(1), m.group(2)
        if q := re.match(r'^"(.*?)"\s*(?:#.*)?$', val):
            val = q.group(1)
        elif q := re.match(r"^'(.*?)'\s*(?:#.*)?$", val):
            val = q.group(1)
        else:
            val = re.sub(r"\s#.*$", "", val).rstrip()
        # The shell environment always wins over the file.
        if key not in os.environ:
            os.environ[key] = val
    return str(path), warnings


def split_list(value: str | None) -> list[str]:
    """Space or comma separated list."""
    return [x for x in re.split(r"[\s,]+", value or "") if x]


def _env(key: str, default: str = "") -> str:
    v = os.environ.get(key)
    return default if v is None or v == "" else v


def _int(key: str, default: int) -> int:
    try:
        return int(_env(key, str(default)))
    except ValueError:
        return default


@dataclass
class Config:
    base_url: str
    scheme: str
    host: str
    insecure: bool
    timeout: float
    connect_timeout: float
    health_wait: int
    verbose: int
    user_prefix: str
    email_domain: str
    password: str
    keep_data: bool
    strict: bool
    rate_limit_retries: int
    rate_limit_max_wait: int
    rate_limit_burst: bool
    body_preview: int
    feed: str
    feed_host: str
    feed_bind: str
    feed_port: str
    run_id: str
    run_tag: str
    warnings: list[str]

    @classmethod
    def from_env(cls) -> Config:
        """Derive the target from CALCARD_URL, or from CALCARD_HOST
        (+ CALCARD_PORT, CALCARD_SCHEME). CALCARD_HOST may itself be a full
        URL or "host:port"."""
        warnings: list[str] = []
        url = os.environ.get("CALCARD_URL", "")
        host = os.environ.get("CALCARD_HOST", "")
        port = os.environ.get("CALCARD_PORT", "")
        scheme = os.environ.get("CALCARD_SCHEME", "")
        if not url:
            if not host:
                raise ConfigError(
                    "No target configured. Set CALCARD_URL (e.g. http://192.168.1.10:8080) or "
                    "CALCARD_HOST in e2e/.env or the environment — see e2e/.env.example."
                )
            if "://" in host:
                url = host
            else:
                if re.search(r":.*:", host) and not host.startswith("["):
                    host = f"[{host}]"  # bare IPv6 literal
                if not scheme:
                    scheme = "https" if port == "443" else "http"
                url = f"{scheme}://{host}"
                if port:
                    if re.search(r":[0-9]+$", host):
                        warnings.append(f"CALCARD_HOST already carries a port; ignoring CALCARD_PORT={port}")
                    else:
                        url += f":{port}"
        url = url.rstrip("/")
        m = re.match(r"^(https?)://([^/]+)(/.*)?$", url)
        if not m:
            raise ConfigError(f"Cannot parse target URL '{url}' (expected http(s)://host[:port])")
        authority = m.group(2).rsplit("@", 1)[-1]
        if authority.startswith("["):
            target_host = authority[1:].split("]", 1)[0]
        else:
            target_host = authority.split(":", 1)[0]
        if m.group(3):
            warnings.append(
                f"Target URL has a path component ({m.group(3)}). CalCard serves /api, /dav and "
                "/.well-known from the root; sub-path deployments are not supported by the "
                "server's absolute redirects."
            )

        prefix = _env("E2E_USER_PREFIX", "e2e")
        if not re.match(r"^[a-z0-9][a-z0-9-]*$", prefix):
            raise ConfigError(f"E2E_USER_PREFIX must be lowercase letters, digits and '-' (got '{prefix}')")
        verbose = _env("E2E_VERBOSE", "0")
        # One id per run: every name the suite creates carries it, so runs
        # never collide with each other or with real data.
        run_id = os.environ.get("E2E_RUN_ID") or (
            time.strftime("%Y%m%d%H%M%S") + f"{random.randrange(10000):04d}"
        )
        if not re.fullmatch(r"[a-z0-9]+", run_id):
            raise ConfigError(f"E2E_RUN_ID must be lowercase letters and digits (got '{run_id}')")
        run_tag = f"{prefix}-{run_id}"
        if len(run_tag) > MAX_RUN_TAG:
            raise ConfigError(
                f"E2E_USER_PREFIX '{prefix}' is too long: names derived from '<prefix>-<run id>' must stay "
                f"within the server's 50-character credential usernames, so the prefix may have at most "
                f"{MAX_RUN_TAG - 1 - len(run_id)} characters (the run id has {len(run_id)})")
        feed = _env("E2E_FEED", "auto").strip().lower()
        if feed in TRUE_WORDS:
            feed = "auto"
        elif feed in FALSE_WORDS:
            feed = "off"
        elif feed != "auto":
            raise ConfigError(f"E2E_FEED must be auto (or true/on/1) or off (or false/no/0), got '{feed}'")
        return cls(
            base_url=url,
            scheme=m.group(1),
            host=target_host,
            insecure=is_true(_env("E2E_INSECURE", "false")),
            timeout=float(_int("E2E_TIMEOUT", 30)),
            connect_timeout=float(_int("E2E_CONNECT_TIMEOUT", 10)),
            health_wait=_int("E2E_HEALTH_WAIT", 60),
            verbose=int(verbose) if verbose.isdigit() else 0,
            user_prefix=prefix,
            email_domain=_env("E2E_EMAIL_DOMAIN", "example.test"),
            password=os.environ.get("E2E_PASSWORD", ""),
            keep_data=is_true(_env("E2E_KEEP_DATA", "false")),
            strict=strict_mode(),
            rate_limit_retries=_int("E2E_RATE_LIMIT_RETRIES", 6),
            rate_limit_max_wait=_int("E2E_RATE_LIMIT_MAX_WAIT", 90),
            rate_limit_burst=is_true(_env("E2E_RATE_LIMIT_BURST", "false")),
            body_preview=_int("E2E_BODY_PREVIEW", 1200),
            feed=feed,
            feed_host=os.environ.get("E2E_FEED_HOST", ""),
            feed_bind=os.environ.get("E2E_FEED_BIND", ""),
            feed_port=os.environ.get("E2E_FEED_PORT", ""),
            run_id=run_id,
            run_tag=run_tag,
            warnings=warnings,
        )


def strict_mode() -> bool:
    """E2E_STRICT: known server bugs count as ordinary failures."""
    return is_true(os.environ.get("E2E_STRICT"))
