"""Rate limiting. The default checks are passive (they read the limiter's
headers and spend one request). The burst test is opt-in
(E2E_RATE_LIMIT_BURST=true) because it deliberately trips the per-account
login limiter — on a throwaway, never-registered e-mail — and so also uses up
part of this client IP's login allowance for one window. Runs last."""

import os
import re

import pytest

from harness import out
from harness.config import is_true

DESCRIPTION = "global limiter headers; opt-in login limiter burst"

# Read at import time (conftest has loaded e2e/.env by then), so the burst
# test carries the same name as in the bash suite in both modes.
BURST = is_true(os.environ.get("E2E_RATE_LIMIT_BURST"))


def test_headers(t, e2e):
    """global limiter advertises its budget in X-RateLimit-* headers"""
    r = e2e.raw("GET", "/health")
    if not t.status(r, 200):
        return
    limit, remaining = r.hdr("X-RateLimit-Limit"), r.hdr("X-RateLimit-Remaining")
    if not limit:
        t.skip("no X-RateLimit-* headers: rate limiting is disabled on the target (CALDAV_RATE_LIMIT_ENABLED=false) "
               "or stripped by a proxy")
    if not (re.fullmatch(r"[0-9]+", limit) and re.fullmatch(r"[0-9]+", remaining)):
        t.fail(f"X-RateLimit-Limit/Remaining must be integers (got '{limit}' / '{remaining}')")
        return
    limit_n, before = int(limit), int(remaining)
    if before > limit_n:
        t.fail(f"Remaining ({before}) exceeds Limit ({limit_n})")
    t.header(r, "X-RateLimit-Reset")
    out.info(f"      global limit: {limit_n} requests per window ({before} left for this client)")
    r = e2e.raw("GET", "/health")
    remaining = r.hdr("X-RateLimit-Remaining")
    # Another client on the same IP may have spent some too; only require that
    # it did not go up within the window (a reset in between is tolerated).
    if re.fullmatch(r"[0-9]+", remaining) and int(remaining) > before and int(remaining) != limit_n - 1:
        t.fail(f"X-RateLimit-Remaining went up from {before} to {remaining} without a window reset")


if BURST:
    def test_login_burst(t, e2e):
        """login limiter: repeated failures for one account end in 429"""
        email = f"{e2e.uniq('ratelimit-probe')}@{e2e.domain}"
        got429 = False
        i = 0
        for i in range(1, 31):
            r = e2e.anon("POST", "/auth/login", json={"email": email, "password": "wrong"}, retry=False)
            if r.status == 429:
                got429 = True
                break
            if r.status != 401:
                t.fail(f"unexpected status before the limit (attempt {i})", r)
                return
        if not got429:
            e2e.anon("GET", "/system/settings")
            t.skip("no 429 after 30 failed logins — the login limiter is disabled (CALDAV_RATE_LIMIT_ENABLED=false)")
        out.info(f"      login limiter tripped after {i - 1} failed attempts")
        t.header(r, "Retry-After")
        t.jq(r, lambda j: len(j["message"]) > 0, "the 429 carries a readable message")
else:
    @pytest.mark.skip(reason="opt-in: set E2E_RATE_LIMIT_BURST=true (spends ~11 login attempts of this IP's "
                             "allowance for one window)")
    def test_login_burst():
        """login limiter burst"""
