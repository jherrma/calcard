"""Assertions for the e2e tests.

Every check returns True/False and, on failure, records a message plus the
request/response dump on the running test — it does NOT stop the test. That
mirrors how the suite was written: a test keeps going after a failed check
unless it explicitly returns (``if not t.status(r, 200): return``), so later
side effects (cleanups, state for the next test) still happen. When the test
function returns, any recorded failure fails the test (see conftest.py).

``known("F03")`` marks a test that fails because of a documented server bug
(e2e/FINDINGS.md): a strict xfail, so the failure is reported as KFAIL and a
pass as XPASS — which fails the run until the marker is removed.
"""

from __future__ import annotations

import re
from typing import Any, Callable

import pytest

from . import config, xml
from .http import NOT_JSON, Resp

FINDINGS = "e2e/FINDINGS.md"


# ---------------------------------------------------------------------------
# Known server bugs
# ---------------------------------------------------------------------------

def known_marks(fid: str) -> list[pytest.MarkDecorator]:
    marks = [pytest.mark.known(fid)]
    if not config.strict_mode():
        marks.append(pytest.mark.xfail(strict=True, reason=f"{fid}: known server bug, see {FINDINGS}"))
    return marks


def known(fid: str):
    """Decorator: the test is expected to fail because of finding FID.
    With E2E_STRICT=true (./run --strict) the marker only labels the test,
    so the bug counts as an ordinary failure."""
    def deco(fn):
        for m in known_marks(fid):
            fn = m(fn)
        return fn
    return deco


# ---------------------------------------------------------------------------
# JSON access (jq-like: a missing key or index yields None)
# ---------------------------------------------------------------------------

_TOKENS = re.compile(r"[^.\[\]]+|\[-?\d+\]")


def jpath(obj: Any, path: str) -> Any:
    """``jpath(body, "data.items[0].name")`` — None when any step is missing."""
    cur = obj
    for tok in _TOKENS.findall(path):
        if tok.startswith("["):
            i = int(tok[1:-1])
            if isinstance(cur, list) and -len(cur) <= i < len(cur):
                cur = cur[i]
            else:
                return None
        elif isinstance(cur, dict) and tok in cur:
            cur = cur[tok]
        else:
            return None
    return cur


def _same(a: Any, b: Any) -> bool:
    if isinstance(a, bool) or isinstance(b, bool):
        return a is b
    return a == b


def _short(v: Any, n: int = 200) -> str:
    s = v if isinstance(v, str) else repr(v)
    return s[:n]


# ---------------------------------------------------------------------------
# Test context
# ---------------------------------------------------------------------------

class SoftFailure(AssertionError):
    """Raised after the test function returned, carrying every failed check."""


class TestCtx:
    """The ``t`` fixture: records failed checks of one test."""

    __test__ = False  # not a test class

    def __init__(self, node: pytest.Item):
        self.node = node
        self.failures: list[str] = []

    # --- flow ---------------------------------------------------------------
    def fail(self, msg: str, resp: Resp | None = None) -> bool:
        entry = f"✗ {msg}"
        if resp is not None:
            entry += "\n" + resp.dump()
        self.failures.append(entry)
        return False

    def abort(self, msg: str, resp: Resp | None = None):
        """Record a failure and stop the test."""
        self.fail(msg, resp)
        raise SoftFailure(self.report())

    def skip(self, reason: str):
        """Skip the running test (it still FAILs if a check already failed)."""
        pytest.skip(reason)

    def need(self, state: Any, *names: str) -> None:
        """Precondition guard: skip when a value an earlier step should have
        produced (attribute of the module state) is empty."""
        for name in names:
            if not getattr(state, name, None):
                pytest.skip(f"precondition not met: {name} is empty (an earlier step failed)")

    def known(self, fid: str) -> None:
        """Mark the running test as a known server bug from here on (for
        bugs that only apply under a runtime condition)."""
        for m in known_marks(fid):
            self.node.add_marker(m)

    def report(self) -> str:
        return "\n".join(self.failures)

    # --- generic ------------------------------------------------------------
    def true(self, cond: Any, msg: str, resp: Resp | None = None) -> bool:
        return True if cond else self.fail(msg, resp)

    def eq(self, actual: Any, expected: Any, msg: str = "values differ") -> bool:
        if _same(actual, expected):
            return True
        return self.fail(f"{msg}: expected '{_short(expected)}', got '{_short(actual)}'")

    def ne(self, actual: Any, unexpected: Any, msg: str = "values must differ") -> bool:
        if not _same(actual, unexpected):
            return True
        return self.fail(f"{msg}: both are '{_short(actual)}'")

    # --- status -------------------------------------------------------------
    def status(self, r: Resp, *codes: int, msg: str = "") -> bool:
        if r.status in codes:
            return True
        want = " or ".join(str(c) for c in codes)
        return self.fail(f"{msg + ': ' if msg else ''}expected HTTP {want}, got {r.status or '000'}", r)

    def status_class(self, r: Resp, cls: str, msg: str = "") -> bool:
        """``cls`` like '4xx'."""
        if str(r.status)[:1] == cls[:1]:
            return True
        return self.fail(f"{msg + ': ' if msg else ''}expected a {cls[:1]}xx status, got {r.status or '000'}", r)

    def status_not(self, r: Resp, *codes: int, msg: str = "") -> bool:
        if r.status not in codes:
            return True
        return self.fail(f"{msg + ': ' if msg else ''}status must not be {r.status}", r)

    # --- JSON ---------------------------------------------------------------
    def json(self, r: Resp, path: str, expected: Any, msg: str = "") -> bool:
        """The JSON value at PATH equals EXPECTED (type-strict for booleans)."""
        body = r.json()
        if body is NOT_JSON:
            return self.fail(msg or "response is not JSON", r)
        got = jpath(body, path)
        if _same(got, expected):
            return True
        return self.fail(f"{msg or 'JSON .' + path}: expected '{_short(expected)}', got '{_short(got)}'", r)

    def jq(self, r: Resp, pred: Callable[[Any], Any], msg: str = "expected JSON condition to hold") -> bool:
        """PRED(parsed body) is truthy. A predicate that raises (missing key,
        wrong type) counts as false, like a failing ``jq -e``."""
        body = r.json()
        ok = False
        if body is not NOT_JSON:
            try:
                ok = bool(pred(body))
            except (KeyError, IndexError, TypeError, AttributeError, ValueError):
                ok = False
        return True if ok else self.fail(msg, r)

    def nonempty(self, r: Resp, path: str, msg: str = "") -> bool:
        body = r.json()
        got = jpath(body, path) if body is not NOT_JSON else None
        if not (got is None or got is False or got == ""):
            return True
        return self.fail(msg or f"expected non-empty .{path}", r)

    # --- body ---------------------------------------------------------------
    def contains(self, r: Resp, needle: str, msg: str = "") -> bool:
        if needle in r.text:
            return True
        return self.fail(msg or f"body should contain '{needle}'", r)

    def not_contains(self, r: Resp, needle: str, msg: str = "") -> bool:
        if needle not in r.text:
            return True
        return self.fail(msg or f"body must not contain '{needle}'", r)

    def match(self, r: Resp, regex: str, msg: str = "") -> bool:
        if re.search(regex, r.text):
            return True
        return self.fail(msg or f"body should match /{regex}/", r)

    # --- headers ------------------------------------------------------------
    def header(self, r: Resp, name: str, substring: str = "", msg: str = "") -> bool:
        v = r.hdr(name)
        if not v:
            return self.fail(msg or f"missing response header {name}", r)
        if substring and substring.lower() not in v.lower():
            return self.fail(msg or f"header {name} should contain '{substring}', got '{v}'", r)
        return True

    def header_eq(self, r: Resp, name: str, value: str, msg: str = "") -> bool:
        v = r.hdr(name)
        if v == value:
            return True
        return self.fail(msg or f"header {name}: expected '{value}', got '{v}'", r)

    def header_absent(self, r: Resp, name: str, msg: str = "") -> bool:
        if not r.hdr(name):
            return True
        return self.fail(msg or f"header {name} must be absent, got '{r.hdr(name)}'", r)

    # --- XML ----------------------------------------------------------------
    def xml_wellformed(self, r: Resp) -> bool:
        if xml.parse(r) is not None:
            return True
        return self.fail("response body is not well-formed XML", r)

    def xpath(self, r: Resp, expr: str, msg: str = "XPath does not hold") -> bool:
        if xml.xbool(r, expr):
            return True
        return self.fail(f"{msg}: {expr}", r)

    def xpath_count(self, r: Resp, expr: str, n: int, msg: str = "") -> bool:
        got = xml.xcount(r, expr)
        if got == n:
            return True
        return self.fail(f"{msg or 'XPath count of ' + expr}: expected {n}, got {got if got >= 0 else '?'}", r)
