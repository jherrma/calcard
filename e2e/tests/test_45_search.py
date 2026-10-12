"""Unified search (GET /api/v1/search): every group, no default date window,
recurring series as one hit, type filters, explicit bounds, limits, isolation."""

import time
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from urllib.parse import urlencode

import pytest

from harness import out
from harness.checks import jpath
from harness.session import SetupFailed

DESCRIPTION = "unified search across events, contacts, calendars, address books"

pytestmark = pytest.mark.usefixtures("u1", "u2")


@pytest.fixture(scope="module", autouse=True)
def sr(e2e, u1):
    """A calendar and an address book named after a run-unique token, seeded
    with events 3 years ahead / back, a weekly series, a decoy and a contact."""
    token = f"Expd{e2e.run_id[-8:]}"
    cal = u1.mk_calendar(f"{token} Calendar", "#abcdef")
    ab = u1.mk_addressbook(f"{token} Contacts")
    now_y = int(time.strftime("%Y"))
    s = SimpleNamespace(token=token, cal=cal, ab=ab, future=now_y + 3, past=now_y - 3,
                        weekly_first=datetime(now_y - 1, 1, 3, 9, 0, tzinfo=timezone.utc))
    seeds = [
        lambda: u1.mk_event(cal["uuid"], f"{token} briefing far ahead", f"{s.future}-06-01T10:00:00Z",
                            f"{s.future}-06-01T11:00:00Z"),
        lambda: u1.mk_event(cal["uuid"], f"{token} debrief long past", f"{s.past}-06-01T10:00:00Z",
                            f"{s.past}-06-01T11:00:00Z"),
        lambda: u1.mk_event(cal["uuid"], f"{token} weekly sync", f"{now_y - 1}-01-03T09:00:00Z",
                            f"{now_y - 1}-01-03T10:00:00Z", recurrence={"frequency": "WEEKLY"}),
        lambda: u1.mk_event(cal["uuid"], "Unrelated dentist", f"{s.future}-01-01T10:00:00Z",
                            f"{s.future}-01-01T11:00:00Z"),
        lambda: u1.mk_contact(ab["UUID"], f"{token} Leader",
                              emails=[{"type": "work", "value": "leader@example.test"}]),
    ]
    for seed in seeds:
        try:
            seed()
        except SetupFailed:
            out.warn("search seed failed")
    return s


def _sr(u1, query):
    return u1.api("GET", f"/search?{query}")


def test_all_groups(t, u1, sr):
    """one query finds events, contacts, calendars and address books"""
    tok = sr.token
    r = _sr(u1, f"q={tok}")
    if not t.status(r, 200):
        return
    t.json(r, "query", tok)
    t.jq(r, lambda j: sorted(i["event"]["summary"] for i in j["events"]["items"])
         == [f"{tok} briefing far ahead", f"{tok} debrief long past", f"{tok} weekly sync"],
         "exactly the three matching events")
    t.jq(r, lambda j: [i["contact"]["formatted_name"] for i in j["contacts"]["items"]] == [f"{tok} Leader"])
    t.jq(r, lambda j: j["contacts"]["items"][0]["addressbook_name"] == f"{tok} Contacts",
         "contact hits carry their book name")
    t.jq(r, lambda j: [i["name"] for i in j["calendars"]["items"]] == [f"{tok} Calendar"])
    t.jq(r, lambda j: [i["Name"] for i in j["addressbooks"]["items"]] == [f"{tok} Contacts"])
    t.jq(r, lambda j: j["events"]["searched"] and j["contacts"]["searched"] and j["calendars"]["searched"]
         and j["addressbooks"]["searched"])
    t.json(r, "max_limit", 100)
    t.jq(r, lambda j: {"calendar_uuid", "calendar_name", "calendar_color"} <= j["events"]["items"][0].keys(),
         "event hits are self-contained")


def test_no_window(t, u1, sr):
    """events are found without a date window (±3 years)"""
    # Queries are substring matches, so search for a prefix of the summary.
    r = _sr(u1, f"q={sr.token}+briefing")
    t.jq(r, lambda j: len(j["events"]["items"]) == 1, "an event 3 years ahead")
    r = _sr(u1, f"q={sr.token}+debrief")
    t.jq(r, lambda j: len(j["events"]["items"]) == 1, "an event 3 years back")


def _next_weekly(first: datetime, duration: timedelta, at: datetime) -> datetime:
    """Start of the first occurrence of a weekly series still under way or
    ahead at AT (the server's pivot rule: overlap, so end > AT)."""
    k = max(0, (at - duration - first) // timedelta(weeks=1) + 1)
    return first + k * timedelta(weeks=1)


def test_recurring(t, u1, sr):
    """a recurring series is one hit at its next occurrence"""
    before = datetime.now(timezone.utc)
    r = _sr(u1, f"q={sr.token}+weekly&types=events")
    after = datetime.now(timezone.utc)
    if not t.status(r, 200):
        return
    if not t.jq(r, lambda j: len(j["events"]["items"]) == 1, "one hit per series, not one per occurrence"):
        return
    t.json(r, "events.items[0].event.is_recurring", True)
    t.nonempty(r, "events.items[0].event.recurrence_id")
    # The seeded series: weekly from Jan 3 of last year, 09:00–10:00 UTC. The
    # hit must be exactly the next occurrence (not the last past one, which
    # is at most 7 days back). Computed for both ends of the request.
    first, dur = sr.weekly_first, timedelta(hours=1)
    want = {_next_weekly(first, dur, at).strftime("%Y-%m-%dT%H:%M:%SZ") for at in (before, after)}
    start = jpath(r.json(), "events.items[0].event.start")
    t.true(start in want, f"expected the upcoming occurrence {' or '.join(sorted(want))}, got {start}", r)
    t.json(r, "events.items[0].calendar_uuid", sr.cal["uuid"])
    t.json(r, "events.items[0].calendar_color", "#abcdef")


def test_types(t, u1, sr):
    """types= narrows and marks skipped groups searched=false"""
    r = _sr(u1, f"q={sr.token}&types=contacts")
    if not t.status(r, 200):
        return
    t.json(r, "contacts.searched", True)
    t.json(r, "events.searched", False, "an unsearched group must say so")
    t.jq(r, lambda j: len(j["events"]["items"]) == 0)
    r = _sr(u1, f"q={sr.token}&types=calendars,addressbooks")
    t.jq(r, lambda j: j["calendars"]["searched"] and j["addressbooks"]["searched"]
         and not j["events"]["searched"] and not j["contacts"]["searched"])


def test_case_insensitive(t, e2e, u1, sr):
    """search ignores case, also for non-ASCII letters, on /search and /contacts/search"""
    # Its own token, so the exact-result checks of the other tests stay valid.
    tok = f"MixCase{e2e.run_id[-8:]}"
    try:
        u1.mk_event(sr.cal["uuid"], f"Übergabe {tok}", f"{sr.future}-03-01T10:00:00Z",
                    f"{sr.future}-03-01T11:00:00Z")
        u1.mk_contact(sr.ab["UUID"], f"Ärztin {tok}")
    except SetupFailed as exc:
        t.abort(f"seeding failed: {exc}")
    # SQLite's LIKE used to fold ASCII only and PostgreSQL's nothing (F26), so a
    # query that differs from the stored text only in case missed it.
    for q in (tok, tok.lower(), tok.upper(), f"übergabe {tok.lower()}", f"ÜBERGABE {tok}"):
        r = _sr(u1, urlencode({"q": q, "types": "events"}))
        if t.status(r, 200):
            t.jq(r, lambda j: [i["event"]["summary"] for i in j["events"]["items"]] == [f"Übergabe {tok}"],
                 f"events q={q!r} must find 'Übergabe {tok}'")
    for q in (tok.lower(), tok.upper(), f"ärztin {tok.lower()}", f"ÄRZTIN {tok}"):
        r = _sr(u1, urlencode({"q": q, "types": "contacts"}))
        if t.status(r, 200):
            t.jq(r, lambda j: [i["contact"]["formatted_name"] for i in j["contacts"]["items"]] == [f"Ärztin {tok}"],
                 f"/search contacts q={q!r} must find 'Ärztin {tok}'")
        r = u1.api("GET", "/contacts/search?" + urlencode({"q": q}))
        if t.status(r, 200):
            t.jq(r, lambda j: [c["formatted_name"] for c in j["contacts"]] == [f"Ärztin {tok}"],
                 f"/contacts/search q={q!r} must find 'Ärztin {tok}'")


def test_bounds(t, u1, sr):
    """explicit start/end bound the event search"""
    r = _sr(u1, f"q={sr.token}&types=events&start={sr.future}-01-01T00:00:00Z&end={sr.future}-12-31T00:00:00Z")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: f"{sr.token} briefing far ahead" in [i["event"]["summary"] for i in j["events"]["items"]],
         "in-range event found")
    t.jq(r, lambda j: f"{sr.token} debrief long past" not in [i["event"]["summary"] for i in j["events"]["items"]],
         "out-of-range event excluded")


def test_limit(t, u1, sr):
    """limit caps each group and reports has_more / max_limit"""
    r = _sr(u1, f"q={sr.token}&types=events&limit=1")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len(j["events"]["items"]) == 1)
    t.json(r, "events.has_more", True)
    r = _sr(u1, f"q={sr.token}&limit=1000")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: j["limit"] <= j["max_limit"], "limit is clamped to max_limit")


def test_invalid(t, e2e, u1, sr):
    """short query is 400, anonymous is 401"""
    t.status(_sr(u1, "q=e"), 400, msg="queries need at least 2 characters")
    t.status(_sr(u1, ""), 400)
    t.status(e2e.anon("GET", f"/search?q={sr.token}"), 401)


def test_isolation(t, u2, sr):
    """another user finds none of it"""
    r = u2.api("GET", f"/search?q={sr.token}")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: all(len(j[g]["items"]) == 0 for g in ("events", "contacts", "calendars", "addressbooks")),
         "U2 must not see any of U1's data")
