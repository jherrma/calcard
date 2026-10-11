"""pytest wiring for the CalCard end-to-end suite.

- configuration (.env + environment) and the server preflight, once per run
- the shared fixtures: ``e2e`` (session), ``u1``/``u2`` (shared users),
  ``t`` (soft checks of one test), ``st`` (state shared by one suite's tests)
- suite selection (E2E_SUITES / E2E_SKIP_SUITES), result lines, the summary
  and the exit codes 0 / 1 / 2 / 130 (see README.md)
"""

from __future__ import annotations

import os
import re
import signal
import time
from pathlib import Path
from types import SimpleNamespace

import pytest

from harness import config as cfgmod
from harness import out
from harness.checks import SoftFailure, TestCtx
from harness.session import E2E, SetupFailed

E2E_DIR = Path(__file__).resolve().parent

try:
    ENV_FILE, ENV_WARNINGS = cfgmod.load_env_file()
    ENV_ERROR = ""
except cfgmod.ConfigError as _e:
    ENV_FILE, ENV_WARNINGS, ENV_ERROR = None, [], str(_e)

CTX_KEY = pytest.StashKey[TestCtx]()
DETAIL_KEY = pytest.StashKey[str]()


class _State:
    def __init__(self):
        self.counts = {"PASS": 0, "FAIL": 0, "SKIP": 0, "KFAIL": 0, "XPASS": 0}
        self.lists: dict[str, list[str]] = {k: [] for k in self.counts}
        self.results: list[tuple[str, str]] = []
        self.current_suite = ""
        self.interrupted = False
        self.e2e: E2E | None = None
        self.started = 0.0
        self.last_detail = ""


STATE = _State()


# ---------------------------------------------------------------------------
# Suites
# ---------------------------------------------------------------------------

def suite_of(path: str | os.PathLike) -> str:
    """tests/test_15_credentials.py → credentials."""
    m = re.match(r"test_\d\d_(.+)\.py$", Path(path).name)
    return m.group(1) if m else Path(path).stem


def all_suites() -> list[str]:
    return [suite_of(p) for p in sorted((E2E_DIR / "tests").glob("test_[0-9][0-9]_*.py"))]


def display_name(item: pytest.Item) -> str:
    """suite/description — the description is the first docstring line."""
    doc = (getattr(item, "function", None).__doc__ or item.name).strip().splitlines()[0]
    return f"{suite_of(item.path)}/{doc}"


# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

@pytest.hookimpl(trylast=True)
def pytest_configure(config: pytest.Config) -> None:
    config.addinivalue_line("markers", "known(fid): fails because of a documented server bug (e2e/FINDINGS.md)")
    # SIGTERM ends the run like Ctrl-C: cleanup runs, exit code 130.
    def _term(signum, frame):
        raise KeyboardInterrupt
    try:
        signal.signal(signal.SIGTERM, _term)
    except ValueError:  # not the main thread
        pass
    verbose = os.environ.get("E2E_VERBOSE", "0")
    out.setup(config.get_terminal_writer(), config.pluginmanager.getplugin("capturemanager"),
              int(verbose) if verbose.isdigit() else 0)


def pytest_collection_modifyitems(session: pytest.Session, config: pytest.Config, items: list[pytest.Item]) -> None:
    """Suite selection: E2E_SUITES (in the given order; ./run sets it from its
    arguments) minus E2E_SKIP_SUITES — before contacting the server, so a
    typo fails fast."""
    for item in items:
        fn = getattr(item, "function", None)
        if fn is None or not (fn.__doc__ or "").strip():
            raise pytest.UsageError(f"{item.nodeid}: every test needs a docstring (its name in the results)")
    known = all_suites()
    wanted = cfgmod.split_list(os.environ.get("E2E_SUITES"))
    skipped = cfgmod.split_list(os.environ.get("E2E_SKIP_SUITES"))
    for s in wanted + skipped:
        if s not in known:
            pytest.exit(f"unknown suite '{s}' — available: {' '.join(known)}", returncode=2)
    if not wanted and not skipped:
        return
    order = {s: i for i, s in enumerate(wanted or known)}
    keep, drop = [], []
    for item in items:
        s = suite_of(item.path)
        (keep if s in order and s not in skipped else drop).append(item)
    keep.sort(key=lambda it: order[suite_of(it.path)])  # stable: file order inside a suite
    if drop:
        config.hook.pytest_deselected(items=drop)
    if not keep and items:
        pytest.exit("nothing to run (all selected suites are in E2E_SKIP_SUITES)", returncode=2)
    items[:] = keep


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

def _start_session() -> E2E:
    """Configuration, banner, health wait, preflight — exit code 2 on failure."""
    if ENV_ERROR:
        pytest.exit(ENV_ERROR, returncode=2)
    try:
        cfg = cfgmod.Config.from_env()
    except cfgmod.ConfigError as e:
        pytest.exit(str(e), returncode=2)
    cfg.warnings[:0] = ENV_WARNINGS
    session = E2E(cfg, ENV_FILE)
    STATE.e2e = session
    STATE.started = time.time()
    session.start()
    return session


@pytest.hookimpl(tryfirst=True)
def pytest_runtestloop(session: pytest.Session):
    # Start (and preflight) before the first suite header is printed.
    if not session.config.option.collectonly and session.items and STATE.e2e is None:
        _start_session()


@pytest.fixture(scope="session", autouse=True)
def e2e():
    """The run (harness/session.py); runs the cleanup stack at the end —
    also after failures and Ctrl-C."""
    session = STATE.e2e or _start_session()
    try:
        yield session
    finally:
        try:
            session.run_cleanups()
        finally:
            session.close()


@pytest.fixture(scope="session")
def u1(e2e):
    """The main test user."""
    return e2e.ensure_user("U1")


@pytest.fixture(scope="session")
def u2(e2e):
    """The peer user for sharing / isolation tests."""
    return e2e.ensure_user("U2")


@pytest.fixture(scope="module")
def st():
    """State one suite's tests hand to each other (ids, tokens, ETags).
    Read it through ``t.need(st, "name")`` so a test whose predecessor
    failed is SKIPPED with a reason instead of failing for no reason."""
    return SimpleNamespace()


@pytest.fixture
def t(request):
    """Soft checks for the running test (see harness/checks.py)."""
    ctx = TestCtx(request.node)
    request.node.stash[CTX_KEY] = ctx
    return ctx


# ---------------------------------------------------------------------------
# Running a test: soft failures → test failure
# ---------------------------------------------------------------------------

@pytest.hookimpl(wrapper=True)
def pytest_runtest_call(item: pytest.Item):
    __tracebackhide__ = True
    try:
        result = yield
    except pytest.skip.Exception:
        ctx = item.stash.get(CTX_KEY, None)
        if ctx is not None and ctx.failures:  # a skip never hides a failed check
            raise SoftFailure(ctx.report()) from None
        raise
    except (SoftFailure, KeyboardInterrupt, pytest.exit.Exception):
        raise
    except Exception as e:
        ctx = item.stash.get(CTX_KEY, None)
        if ctx is not None and ctx.failures:
            raise SoftFailure(ctx.report() + f"\n✗ {type(e).__name__}: {e}") from e
        raise
    ctx = item.stash.get(CTX_KEY, None)
    if ctx is not None and ctx.failures:
        raise SoftFailure(ctx.report())
    return result


@pytest.hookimpl(wrapper=True, tryfirst=True)
def pytest_runtest_makereport(item: pytest.Item, call: pytest.CallInfo):
    rep = yield
    exc = call.excinfo
    if call.when == "setup" and hasattr(rep, "wasxfail") and exc is not None \
            and not exc.errisinstance((pytest.skip.Exception, pytest.xfail.Exception)):
        # A broken suite fixture is a failure, not the known server bug.
        del rep.wasxfail
        rep.outcome = "failed"
    if exc is not None and not exc.errisinstance(pytest.skip.Exception):
        if exc.errisinstance((SoftFailure, SetupFailed)):
            detail = str(exc.value)
        else:
            detail = str(exc.getrepr(style="short", chain=True))
        rep.e2e_detail = detail
    known_marker = item.get_closest_marker("known")
    rep.e2e_known = known_marker.args[0] if known_marker else ""
    rep.e2e_name = display_name(item)
    return rep


# ---------------------------------------------------------------------------
# Output: one line per test, like "  PASS  suite/description (12ms)"
# ---------------------------------------------------------------------------

def pytest_report_teststatus(report: pytest.TestReport, config: pytest.Config):
    """Keep pytest's own per-test letters quiet; we print our own lines."""
    if hasattr(report, "wasxfail"):
        cat = "xfailed" if report.skipped else "xpassed"
    elif report.when in ("setup", "teardown") and report.failed:
        cat = "error"
    elif report.when != "call" and report.passed:
        cat = ""
    else:
        cat = report.outcome
    return cat, "", ""


def pytest_runtest_logstart(nodeid: str, location) -> None:
    suite = suite_of(location[0])
    if suite != STATE.current_suite:
        STATE.current_suite = suite
        desc = _suite_description(E2E_DIR / location[0]) if (E2E_DIR / location[0]).exists() else ""
        out.line()
        out.line(f"{out.BOLD}{out.BLUE}== {suite}{out.RESET}{out.BOLD}: {desc}{out.RESET}")


def _suite_description(path: Path) -> str:
    import ast

    try:
        tree = ast.parse(path.read_text(encoding="utf-8"))
    except (OSError, SyntaxError):
        return ""
    for node in tree.body:
        if isinstance(node, ast.Assign) and any(getattr(t, "id", "") == "DESCRIPTION" for t in node.targets):
            if isinstance(node.value, ast.Constant):
                return str(node.value.value)
    return ""


def pytest_runtest_logreport(report: pytest.TestReport) -> None:
    if report.when == "call" or (report.when == "setup" and not report.passed):
        pass
    elif report.when == "teardown" and report.failed:
        name = getattr(report, "e2e_name", report.nodeid)
        out.line(f"  {out.RED}ERROR{out.RESET} {name} — teardown failed")
        out.line(_indent(getattr(report, "e2e_detail", report.longreprtext)))
        STATE.counts["FAIL"] += 1
        STATE.lists["FAIL"].append(f"{name} (teardown)")
        return
    else:
        return
    name = getattr(report, "e2e_name", report.nodeid)
    fid = getattr(report, "e2e_known", "")
    extra, detail = "", ""
    if hasattr(report, "wasxfail"):
        if report.skipped:
            status, color = "KFAIL", out.MAGENTA
            extra = f" [known server bug {fid}, see e2e/FINDINGS.md]"
            detail = getattr(report, "e2e_detail", "")
        else:  # non-strict xpass (not used by this suite)
            status, color = "XPASS", out.CYAN
    elif report.skipped:
        status, color = "SKIP", out.YELLOW
        reason = report.longrepr[2] if isinstance(report.longrepr, tuple) else str(report.longrepr)
        reason = reason.removeprefix("Skipped: ")
        extra = f" — {reason}"
        name_for_list = f"{name} — {reason}"
    elif report.failed and str(report.longrepr).startswith("[XPASS(strict)]"):
        status, color = "XPASS", out.CYAN
        extra = (f" [expected failure {fid} no longer reproduces — remove the known(\"{fid}\") marker "
                 "and update FINDINGS.md]")
    elif report.failed:
        status, color = "FAIL", out.RED
        detail = getattr(report, "e2e_detail", "") or report.longreprtext
        if fid and cfgmod.strict_mode():  # --strict: a known bug counted as failure
            extra = f" [known server bug {fid}, see e2e/FINDINGS.md]"
    else:
        status, color = "PASS", out.GREEN
    ms = int(report.duration * 1000)
    out.line(f"  {color}{status:<5}{out.RESET} {name}{extra} {out.DIM}({ms}ms){out.RESET}")
    if detail and (status in ("FAIL", "KFAIL") or out.verbose >= 1):
        if detail == STATE.last_detail:  # a failed suite fixture fails every test the same way
            out.line(f"        {out.DIM}(same failure as the previous test){out.RESET}")
        else:
            out.line(_indent(detail))
            STATE.last_detail = detail
    STATE.counts[status] += 1
    if status == "SKIP":
        STATE.lists["SKIP"].append(name_for_list)
    elif status in ("KFAIL", "XPASS") or (status == "FAIL" and fid and cfgmod.strict_mode()):
        STATE.lists[status].append(f"{name} [{fid}]")
    elif status == "FAIL":
        STATE.lists["FAIL"].append(name)
    STATE.results.append((status, name))


def _indent(text: str) -> str:
    return "\n".join(("        " + ln) if not ln.startswith("          ") else ln for ln in text.splitlines())


# ---------------------------------------------------------------------------
# Summary and exit code
# ---------------------------------------------------------------------------

def pytest_keyboard_interrupt(excinfo) -> None:
    if excinfo.errisinstance(KeyboardInterrupt):
        STATE.interrupted = True
        out.line()
        out.warn("interrupted — cleaning up")


def pytest_terminal_summary(terminalreporter, exitstatus, config) -> None:
    c, e2e = STATE.counts, STATE.e2e
    if e2e is None or (sum(c.values()) == 0 and not e2e.cleanup_warnings):
        return  # aborted before any test ran: nothing to summarize
    line = out.line
    line(f"{out.BOLD}== summary{out.RESET} {out.DIM}(target {e2e.base}, run {e2e.cfg.run_tag}, "
         f"{time.time() - STATE.started:.0f}s){out.RESET}")
    sections = [
        ("SKIP", out.YELLOW, "Skipped:"),
        ("KFAIL", out.MAGENTA, "Known server bugs (KFAIL, see e2e/FINDINGS.md):"),
        ("XPASS", out.CYAN, "Unexpected passes (XPASS — finding fixed? remove the known() marker, update FINDINGS.md):"),
        ("FAIL", out.RED, "Failed:"),
    ]
    for key, color, title in sections:
        if STATE.lists[key]:
            line(f"{color}{title}{out.RESET}")
            for x in STATE.lists[key]:
                line(f"  - {x}")
    if e2e.cleanup_warnings:
        line(f"{out.YELLOW}Cleanup problems (data may be left on the server):{out.RESET}")
        for x in e2e.cleanup_warnings:
            line(f"  - {x}")
    line()
    line(f"{out.GREEN}PASS {c['PASS']}{out.RESET}  {out.RED}FAIL {c['FAIL']}{out.RESET}  "
         f"{out.YELLOW}SKIP {c['SKIP']}{out.RESET}  {out.MAGENTA}KFAIL {c['KFAIL']}{out.RESET}  "
         f"{out.CYAN}XPASS {c['XPASS']}{out.RESET}  {out.DIM}(429 retries: {e2e.http.rate_limit_hits}){out.RESET}")
    results_file = os.environ.get("E2E_RESULTS_FILE")
    if results_file:
        Path(results_file).write_text("".join(f"{s}\t{n}\n" for s, n in STATE.results), encoding="utf-8")


@pytest.hookimpl(trylast=True)
def pytest_sessionfinish(session: pytest.Session, exitstatus) -> None:
    """0 all good · 1 a test FAILed (or a known bug XPASSed) · 2 the run could
    not start · 130 interrupted."""
    if STATE.interrupted:
        session.exitstatus = 130
    elif exitstatus in (pytest.ExitCode.INTERNAL_ERROR, pytest.ExitCode.USAGE_ERROR,
                        pytest.ExitCode.NO_TESTS_COLLECTED):
        session.exitstatus = 2
    elif exitstatus == pytest.ExitCode.OK and STATE.counts["FAIL"] + STATE.counts["XPASS"] > 0:
        session.exitstatus = 1
