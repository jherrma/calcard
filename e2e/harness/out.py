"""Live terminal output that bypasses pytest's output capturing, so progress
notes (429 waits, verbose request logs, the cleanup) show up while a test
runs, like the per-test result lines do."""

from __future__ import annotations

import contextlib
import sys

_tw = None  # pytest TerminalWriter, set by conftest
_capman = None  # pytest CaptureManager, set by conftest
verbose = 0

RED = GREEN = YELLOW = BLUE = MAGENTA = CYAN = DIM = BOLD = RESET = ""


def setup(terminal_writer, capture_manager, verbosity: int) -> None:
    global _tw, _capman, verbose
    global RED, GREEN, YELLOW, BLUE, MAGENTA, CYAN, DIM, BOLD, RESET
    _tw, _capman, verbose = terminal_writer, capture_manager, verbosity
    if terminal_writer is not None and terminal_writer.hasmarkup:
        RED, GREEN, YELLOW, BLUE = "\033[31m", "\033[32m", "\033[33m", "\033[34m"
        MAGENTA, CYAN, DIM, BOLD, RESET = "\033[35m", "\033[36m", "\033[2m", "\033[1m", "\033[0m"


def line(text: str = "") -> None:
    """Write one line to the terminal, even while a test's output is captured."""
    ctx = _capman.global_and_fixture_disabled() if _capman is not None else contextlib.nullcontext()
    with ctx:
        if _tw is not None:
            _tw.line(text)
            _tw.flush()
        else:
            print(text, file=sys.stderr, flush=True)


def info(text: str) -> None:
    line(f"{DIM}{text}{RESET}")


def warn(text: str) -> None:
    line(f"{YELLOW}WARNING:{RESET} {text}")


def vlog(level: int, text: str) -> None:
    """Printed when E2E_VERBOSE >= level."""
    if verbose >= level:
        line(f"{DIM}      {text}{RESET}")
