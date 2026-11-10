"""Alarms / reminders (#169): GET /api/v1/alarms/upcoming (shape, window, the
`until` rules), POST /api/v1/alarms/acknowledge, and the `reminders` field of
events over REST (create, read back, update, VALARMs written by a DAV client).

Every event lies a few hours from *now* (the alarms API is a rolling window, so
nothing fixed in 2030 could appear in it) and is a timed UTC event: no all-day
or floating item, whose trigger would depend on the owner's time zone. The
margins are hours, so the suite gives the same result at any time of day.
Alarms are found by the UID of the event the test created, so other alarms of
the shared users (another suite, an earlier run) never matter."""

import re
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from urllib.parse import urlencode

import pytest

from harness.data import ics_event
from harness.session import SetupFailed

DESCRIPTION = "/alarms/upcoming, /alarms/acknowledge, event `reminders` (REST + DAV VALARMs)"

pytestmark = pytest.mark.usefixtures("u1", "u2")

ALARM_KEYS = {"key", "trigger_at", "uid", "recurrence_id", "alarm_index", "action", "description", "object_type",
              "object_id", "calendar_id", "calendar_uuid", "calendar_name", "calendar_color", "summary", "location",
              "all_day", "start", "start_date", "end", "due", "due_date"}
LIST_KEYS = {"alarms", "count", "from", "until", "server_time", "truncated"}
REMINDER_KEYS = {"index", "action", "related", "offset_seconds", "at", "editable"}
HEX32 = re.compile(r"[0-9a-f]{32}")
INVALID_UNTIL = "Invalid 'until' value"


@pytest.fixture(scope="module", autouse=True)
def al(e2e, u1, u2):
    """Two calendars of U1: one shared read-only with U2, one private; U1's DAV
    password."""
    u1.require_dav()
    shared = u1.mk_calendar(e2e.uniq("alarms-shared"))
    private = u1.mk_calendar(e2e.uniq("alarms-private"))
    r = u1.api("POST", f"/calendars/{shared['uuid']}/shares", json={"user_identifier": u2.email, "permission": "read"})
    if r.status != 201 or not r.jdict().get("id"):
        raise SetupFailed(f"sharing the alarm calendar with {u2.key} failed (HTTP {r.status})", r)
    e2e.defer("revoke alarm calendar share", e2e.cleanup_api, u1, "DELETE",
              f"/calendars/{shared['uuid']}/shares/{r.jdict()['id']}")
    return SimpleNamespace(shared=shared, private=private)


# --- helpers --------------------------------------------------------------------

def now_utc() -> datetime:
    return datetime.now(timezone.utc).replace(microsecond=0)


def iso(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def basic(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def instant(value) -> datetime | None:
    """An RFC 3339 timestamp with a Z (fraction ignored) as an aware datetime, None if it is not one."""
    m = re.fullmatch(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.\d+)?Z", value or "")
    return datetime.fromisoformat(m.group(1) + "+00:00") if m else None


def upcoming(u, **params):
    """GET /alarms/upcoming (query values percent-encoded: '+' → %2B)."""
    return u.api("GET", "/alarms/upcoming" + ("?" + urlencode(params) if params else ""))


def alarms_of(r, uid) -> list:
    return [a for a in r.jdict().get("alarms") or [] if a.get("uid") == uid]


def mk_ev(u, cal, label, start: datetime, minutes=60, **extra) -> dict:
    """A timed UTC event of CAL starting at START (a SetupFailed unless 201)."""
    return u.mk_event(cal["uuid"], label, iso(start), iso(start + timedelta(minutes=minutes)), **extra)


def ev_url(cal, ev) -> str:
    return f"/calendars/{cal['uuid']}/events/{ev['id']}"


def dav_put(u, cal, uid, start: datetime, *extra) -> object:
    """PUT an event with the given extra content lines (VALARM blocks) over CalDAV."""
    return u.dav("PUT", f"{u.home_cal}{cal['path']}/{uid}.ics",
                 ics=ics_event(uid, f"DAV {uid}", basic(start), basic(start + timedelta(hours=1)), *extra))


def valarm(*lines) -> list:
    return ["BEGIN:VALARM", *lines, "END:VALARM"]


# --- GET /alarms/upcoming ---------------------------------------------------------

def test_auth_required(t, e2e):
    """both alarm routes require authentication (401)"""
    t.status(e2e.anon("GET", "/alarms/upcoming"), 401)
    t.status(e2e.anon("POST", "/alarms/acknowledge", json={"keys": ["0" * 32]}), 401)
    t.status(e2e.anon("GET", "/alarms/upcoming", bearer="not-a-token"), 401)


def test_shape_and_defaults(t, u1):
    """upcoming: raw JSON shape, from = now - 24 h, until = now + 24 h, no-store"""
    r = upcoming(u1)
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "application/json")
    t.header(r, "Cache-Control", "no-store")
    j = r.jdict()
    t.eq(set(j), LIST_KEYS, "the answer has exactly these keys (raw JSON, no status/data envelope)")
    t.jq(r, lambda j: isinstance(j["alarms"], list), "alarms is an array, never null")
    t.jq(r, lambda j: j["count"] == len(j["alarms"]), "count matches the list")
    t.jq(r, lambda j: j["truncated"] is False, "truncated is false for a small list")
    srv, frm, until = instant(j.get("server_time")), instant(j.get("from")), instant(j.get("until"))
    if not t.true(srv and frm and until, f"server_time/from/until are RFC 3339 in UTC: {j.get('server_time')!r}, "
                                          f"{j.get('from')!r}, {j.get('until')!r}", r):
        return
    t.true(abs((srv - now_utc()).total_seconds()) < 300, "server_time is the current time (clocks within 5 min)", r)
    t.eq(srv - frm, timedelta(hours=24), "from = server_time - 24 h")
    t.eq(until - srv, timedelta(hours=24), "until defaults to server_time + 24 h")
    for a in j["alarms"]:
        t.eq(set(a), ALARM_KEYS, "every alarm carries exactly the documented keys")


def test_until_rules(t, u1):
    """upcoming: until is honoured, clamped to now + 7 d, an earlier one gives an empty list"""
    r = upcoming(u1, until=iso(now_utc() + timedelta(hours=48)))
    if t.status(r, 200):
        srv, until = instant(r.jdict().get("server_time")), instant(r.jdict().get("until"))
        t.true(srv and until and abs((until - srv - timedelta(hours=48)).total_seconds()) < 300,
               "a 48 h until is echoed (within the clock difference)", r)
    # An offset other than Z: +05:00 sent as %2B05:00 means the same instant.
    inst = now_utc() + timedelta(hours=10)
    local = inst.astimezone(timezone(timedelta(hours=5))).isoformat()
    r = upcoming(u1, until=local)
    if t.status(r, 200):
        t.eq(instant(r.jdict().get("until")), inst, "an until with a +05:00 offset is the same instant, shown in UTC")
    for far in (now_utc() + timedelta(days=30), now_utc() + timedelta(days=7, minutes=10)):
        r = upcoming(u1, until=iso(far))
        if t.status(r, 200):
            srv, until = instant(r.jdict().get("server_time")), instant(r.jdict().get("until"))
            t.eq(until - srv if srv and until else None, timedelta(days=7),
                 f"until {iso(far)} is clamped to server_time + 7 d, and the answer shows the value used")
    r = upcoming(u1, until="2000-01-01T00:00:00Z")
    if t.status(r, 200, msg="an until before from is not an error"):
        t.json(r, "alarms", [], "an until before from is an empty list")
        t.json(r, "count", 0)
        t.json(r, "until", "2000-01-01T00:00:00Z")


def test_until_invalid(t, e2e, u1):
    """upcoming: an until that is not RFC 3339 with an offset is 400 Invalid 'until' value"""
    for bad in ("2030-01-01T00:00:00", "2030-01-01", "tomorrow", "1700000000", "2030-01-01T00:00:00+0100"):
        r = upcoming(u1, until=bad)
        if t.status(r, 400, msg=f"until={bad!r}"):
            t.eq(r.jmsg(), INVALID_UNTIL, f"the 400 text for until={bad!r}")
    # A '+' that is not percent-encoded arrives as a space.
    r = u1.api("GET", "/alarms/upcoming?until=2030-01-01T00:00:00+01:00")
    if t.status(r, 400, msg="an unencoded '+' offset"):
        t.eq(r.jmsg(), INVALID_UNTIL)
    r = u1.api("GET", "/alarms/upcoming?until=")
    t.status(r, 200, msg="an empty until is the default")


def test_rest_reminder_in_upcoming(t, e2e, u1, al, st):
    """an event created with an offset reminder appears in upcoming with the expected trigger and fields"""
    start = now_utc() + timedelta(hours=3)
    summary = f"Alarm {e2e.run_id}"
    r = u1.api("POST", f"/calendars/{al.shared['uuid']}/events", json={
        "summary": summary, "location": "Room 7", "start": iso(start), "end": iso(start + timedelta(hours=1)),
        "timezone": "UTC", "all_day": False, "reminders": [{"offset_seconds": -900}]})
    if not t.status(r, 201):
        return
    ev = r.jdict()
    st.ev, st.uid, st.start = ev, ev["uid"], start
    r = upcoming(u1)
    if not t.status(r, 200):
        return
    found = alarms_of(r, st.uid)
    if not t.eq(len(found), 1, "exactly one alarm for the new event"):
        return
    a = found[0]
    st.key = a["key"]
    t.true(isinstance(a["key"], str) and HEX32.fullmatch(a["key"]), f"key is 32 lowercase hex characters: {a['key']!r}", r)
    t.eq(a["trigger_at"], iso(start - timedelta(minutes=15)), "trigger_at = start - 15 min (UTC, Z)")
    t.eq(a["object_type"], "event")
    t.eq(a["object_id"], ev["id"], "object_id is the event's id")
    t.eq(a["calendar_uuid"], al.shared["uuid"])
    t.eq(a["calendar_id"], al.shared["id"])
    t.eq(a["calendar_name"], al.shared["name"])
    t.eq(a["calendar_color"], al.shared["color"])
    t.eq(a["summary"], summary)
    t.eq(a["location"], "Room 7")
    t.eq(a["all_day"], False)
    t.eq(a["start"], iso(start), "start is the event's start")
    t.eq(a["end"], iso(start + timedelta(hours=1)), "end is the event's end")
    t.eq((a["start_date"], a["due"], a["due_date"], a["recurrence_id"]), (None, None, None, None),
         "start_date, due, due_date and recurrence_id are null for a timed, non-series event")
    t.eq(a["alarm_index"], 0)
    t.eq(a["action"], "display")
    t.eq(a["description"], "Reminder", "the server writes DESCRIPTION:Reminder")
    # The same alarm has the same key on every request.
    again = alarms_of(upcoming(u1), st.uid)
    t.eq([x["key"] for x in again], [st.key], "the key is stable across requests")


def test_reminders_read_back(t, u1, al, st):
    """reminders: the create answer, GET and the listing carry the entry {index, action, related, offset_seconds, at, editable}"""
    t.need(st, "ev")
    want = {"index": 0, "action": "display", "related": "start", "offset_seconds": -900, "at": None, "editable": True}
    r = u1.api("GET", ev_url(al.shared, st.ev))
    if t.status(r, 200):
        t.json(r, "reminders", [want])
        t.jq(r, lambda j: set(j["reminders"][0]) == REMINDER_KEYS, "a reminder carries exactly the documented keys")
    t.eq(st.ev.get("reminders"), [want], "the create answer already carries it")
    r = u1.api("GET", f"/calendars/{al.shared['uuid']}/events?start={iso(st.start - timedelta(hours=1))}"
                      f"&end={iso(st.start + timedelta(hours=2))}")
    if t.status(r, 200):
        t.jq(r, lambda j: [e["reminders"] for e in j["events"] if e["id"] == st.ev["id"]] == [[want]],
             "the event listing carries the reminders")


def test_reminders_never_null(t, e2e, u1, al):
    """an event without alarms reports reminders as [] (never null)"""
    ev = mk_ev(u1, al.private, f"No alarm {e2e.run_id}", now_utc() + timedelta(hours=5))
    t.eq(ev.get("reminders"), [], "create answer")
    r = u1.api("GET", ev_url(al.private, ev))
    if t.status(r, 200):
        t.json(r, "reminders", [])
    t.eq(alarms_of(upcoming(u1), ev["uid"]), [], "an event without VALARM has no upcoming alarm")


def test_reminder_absolute_and_multiple(t, e2e, u1, al):
    """reminders: an absolute `at` and several offsets are stored and listed; upcoming is sorted by trigger_at then key"""
    start = now_utc() + timedelta(hours=6)
    at = start - timedelta(hours=1)
    ev = mk_ev(u1, al.private, f"Absolute {e2e.run_id}", start,
               reminders=[{"offset_seconds": -1800}, {"at": at.astimezone(timezone(timedelta(hours=2))).isoformat()},
                          {"offset_seconds": -600}])
    r = u1.api("GET", ev_url(al.private, ev))
    if not t.status(r, 200):
        return
    rem = {(x["offset_seconds"], x["at"]): x for x in r.jdict().get("reminders") or []}
    t.eq(len(rem), 3, "three reminders")
    abs_r = rem.get((None, iso(at)))
    if t.true(abs_r is not None, f"the absolute reminder is reported as its UTC instant: {r.jdict().get('reminders')}", r):
        t.eq((abs_r["related"], abs_r["offset_seconds"], abs_r["editable"]), (None, None, True))
    t.jq(r, lambda j: sorted(x["index"] for x in j["reminders"]) == [0, 1, 2], "indexes are 0..2")
    found = alarms_of(upcoming(u1), ev["uid"])
    t.eq(sorted(a["trigger_at"] for a in found),
         sorted(iso(x) for x in (start - timedelta(minutes=30), at, start - timedelta(minutes=10))),
         "upcoming has one alarm per reminder at the right instant")
    t.eq(len({a["key"] for a in found}), len(found), "the keys of different alarms differ")
    whole = upcoming(u1).jdict().get("alarms") or []
    t.eq([(a["trigger_at"], a["key"]) for a in whole], sorted((a["trigger_at"], a["key"]) for a in whole),
         "the list is sorted by trigger_at, then key")


def test_reminder_validation(t, e2e, u1, al):
    """reminders: bad input is a 400 with the documented text and creates nothing"""
    base = {"summary": f"Bad reminder {e2e.run_id}", "start": iso(now_utc() + timedelta(hours=7)),
            "end": iso(now_utc() + timedelta(hours=8)), "timezone": "UTC", "all_day": False}
    cases = [
        ([{"offset_seconds": -60, "at": iso(now_utc() + timedelta(hours=6))}], "a reminder needs exactly one of offset_seconds and at"),
        ([{}], "a reminder needs exactly one of offset_seconds and at"),
        ([{"offset_seconds": -60}] * 11, "reminders must contain at most 10 entries"),
        ([{"offset_seconds": -400 * 86400 - 1}], "reminder offset out of range"),
        ([{"at": "2030-01-01T10:00:00"}], "Invalid reminder time"),
        ([{"at": iso(now_utc() + timedelta(days=500))}], "reminder time out of range"),
    ]
    for reminders, text in cases:
        r = u1.api("POST", f"/calendars/{al.private['uuid']}/events", json={**base, "reminders": reminders})
        if t.status(r, 400, msg=f"reminders {reminders!r:.80}"):
            t.eq(r.jmsg(), text, f"the 400 text for {reminders!r:.60}")
    r = u1.api("GET", f"/calendars/{al.private['uuid']}/events?start={base['start']}&end={base['end']}")
    if t.status(r, 200):
        t.jq(r, lambda j: not [e for e in j["events"] or [] if e["summary"] == base["summary"]],
             "a refused create stores nothing")


def test_update_reminders(t, e2e, u1, al):
    """PATCH reminders: absent keeps, an array replaces, [] and null remove the editable ones, upcoming follows"""
    start = now_utc() + timedelta(hours=4)
    ev = mk_ev(u1, al.private, f"Patch {e2e.run_id}", start, reminders=[{"offset_seconds": -900}])
    url = ev_url(al.private, ev)

    r = u1.api("PATCH", url, json={"summary": f"Patch renamed {e2e.run_id}"})
    if t.status(r, 200):
        t.jq(r, lambda j: [x["offset_seconds"] for x in j["reminders"]] == [-900], "no `reminders` key leaves the alarms")
    etag = u1.api("GET", url).hdr("ETag")

    r = u1.api("PATCH", url, json={"reminders": [{"offset_seconds": -900}]})
    if t.status(r, 200):
        t.jq(r, lambda j: [x["offset_seconds"] for x in j["reminders"]] == [-900], "the same list again keeps the alarm")
        t.eq(u1.api("GET", url).hdr("ETag"), etag, "an unchanged reminders list writes nothing (ETag kept)")

    r = u1.api("PATCH", url, json={"reminders": [{"offset_seconds": -900}, {"offset_seconds": -3600}]})
    if t.status(r, 200):
        t.eq(sorted(x["offset_seconds"] for x in r.jdict()["reminders"]), [-3600, -900], "an array is the desired set")
    t.eq(sorted(a["trigger_at"] for a in alarms_of(upcoming(u1), ev["uid"])),
         [iso(start - timedelta(hours=1)), iso(start - timedelta(minutes=15))], "upcoming has both alarms")

    r = u1.api("PATCH", url, json={"reminders": [{"offset_seconds": -3600}]})
    if t.status(r, 200):
        t.eq([x["offset_seconds"] for x in r.jdict()["reminders"]], [-3600], "an editable reminder left out is removed")

    r = u1.api("PATCH", url, json={"reminders": []})
    if t.status(r, 200):
        t.json(r, "reminders", [], "reminders: [] removes the editable reminder")
    t.json(u1.api("GET", url), "reminders", [], "stored")
    t.eq(alarms_of(upcoming(u1), ev["uid"]), [], "the alarm is gone from upcoming")

    r = u1.api("PATCH", url, json={"reminders": [{"offset_seconds": -300}]})
    t.status(r, 200)
    r = u1.api("PATCH", url, json={"reminders": None})
    if t.status(r, 200):
        t.json(r, "reminders", [], "reminders: null removes the editable reminder too")

    r = u1.api("PATCH", url, json={"reminders": [{"offset_seconds": 1}, {"offset_seconds": 2, "at": iso(start)}]})
    t.status(r, 400, msg="one bad entry refuses the whole PATCH")
    t.json(u1.api("GET", url), "reminders", [], "a refused PATCH changes nothing")


def test_foreign_alarms(t, e2e, u1, al):
    """VALARMs written over DAV: listed (non-editable ones flagged), and `reminders: []` never removes the non-editable ones"""
    start = now_utc() + timedelta(hours=5)
    uid = f"alarm-foreign-{e2e.run_id}"
    r = dav_put(u1, al.private, uid, start,
                *valarm("ACTION:DISPLAY", "DESCRIPTION:editable", "TRIGGER:-PT20M"),
                *valarm("ACTION:DISPLAY", "DESCRIPTION:from the end", "TRIGGER;RELATED=END:-PT5M"),
                *valarm("ACTION:EMAIL", "SUMMARY:mail", "DESCRIPTION:mail", "ATTENDEE:mailto:nobody@example.test",
                        "TRIGGER:-PT1H"),
                *valarm("ACTION:DISPLAY", "DESCRIPTION:repeats", "TRIGGER:-PT30M", "REPEAT:2", "DURATION:PT5M"))
    if not t.status(r, 201, msg="PUT the event with four VALARMs"):
        return
    lst = u1.api("GET", f"/calendars/{al.private['uuid']}/events?start={iso(start - timedelta(hours=1))}"
                        f"&end={iso(start + timedelta(hours=2))}")
    if not t.status(lst, 200):
        return
    evs = [e for e in lst.jdict().get("events") or [] if e.get("uid") == uid]
    if not t.eq(len(evs), 1, "the DAV event is listed over REST"):
        return
    ev = evs[0]
    rem = ev["reminders"]
    t.eq(len(rem), 4, "all four VALARMs are reported")
    t.jq(lst, lambda j: all(set(x) == REMINDER_KEYS for x in rem), "every entry has the documented keys")
    by = {x["index"]: x for x in rem}
    t.eq((by[0]["editable"], by[0]["related"], by[0]["offset_seconds"]), (True, "start", -1200), "plain relative alarm")
    t.eq((by[1]["editable"], by[1]["related"], by[1]["offset_seconds"]), (False, "end", -300), "RELATED=END is read-only")
    t.eq((by[2]["editable"], by[2]["action"]), (False, "email"), "an EMAIL alarm is read-only, action lower-case")
    t.eq(by[3]["editable"], False, "an alarm with REPEAT is read-only")

    ups = upcoming(u1)
    got = {a["trigger_at"]: a for a in alarms_of(ups, uid)}
    for trig in (start - timedelta(minutes=20), start + timedelta(hours=1) - timedelta(minutes=5),
                 start - timedelta(minutes=30)):
        t.true(iso(trig) in got, f"upcoming lists the DAV alarm triggering at {iso(trig)}", ups)
    t.eq(sorted(a["alarm_index"] for a in got.values() if a["trigger_at"] in
                (iso(start - timedelta(minutes=20)), iso(start + timedelta(minutes=55)))), [0, 1],
         "alarm_index is the VALARM's position")

    url = ev_url(al.private, ev)
    r = u1.api("PATCH", url, json={"reminders": []})
    if t.status(r, 200):
        t.eq([x["editable"] for x in r.jdict()["reminders"]], [False, False, False],
             "only the editable reminder is removed; the foreign ones stay")
        t.eq(sorted(x["action"] for x in r.jdict()["reminders"]), ["display", "display", "email"])
    # The foreign VALARM bytes survive in the stored component.
    d = u1.dav("GET", f"{u1.home_cal}{al.private['path']}/{uid}.ics")
    if t.status(d, 200):
        t.contains(d, "TRIGGER;RELATED=END:-PT5M", "the RELATED=END alarm is still in the stored iCalendar")
        t.contains(d, "ACTION:EMAIL", "the EMAIL alarm is still in the stored iCalendar")
        t.contains(d, "REPEAT:2", "the repeating alarm is still in the stored iCalendar")
        t.not_contains(d, "DESCRIPTION:editable", "the editable alarm is gone")
    # A reminders array that leaves the foreign ones out does not remove them either.
    r = u1.api("PATCH", url, json={"reminders": [{"offset_seconds": -120}]})
    if t.status(r, 200):
        t.eq(len(r.jdict()["reminders"]), 4, "a new reminder is appended next to the three foreign ones")


def test_window_and_look_back(t, e2e, u1, al):
    """upcoming: alarms from the last 24 h are listed, older ones and ones beyond until are not"""
    n = now_utc()
    recent = mk_ev(u1, al.private, f"Recent {e2e.run_id}", n - timedelta(hours=2), reminders=[{"offset_seconds": -900}])
    old = mk_ev(u1, al.private, f"Old {e2e.run_id}", n - timedelta(hours=30), reminders=[{"offset_seconds": -900}])
    later = mk_ev(u1, al.private, f"Later {e2e.run_id}", n + timedelta(hours=30), reminders=[{"offset_seconds": -900}])
    far = mk_ev(u1, al.private, f"Far {e2e.run_id}", n + timedelta(days=10), reminders=[{"offset_seconds": -900}])
    r = upcoming(u1)
    if not t.status(r, 200):
        return
    t.eq(len(alarms_of(r, recent["uid"])), 1, "an alarm that fired 2 h ago is still listed (24 h look-back)")
    t.eq(len(alarms_of(r, old["uid"])), 0, "an alarm older than 24 h is not listed")
    t.eq(len(alarms_of(r, later["uid"])), 0, "an alarm 30 h ahead is outside the default window")
    r = upcoming(u1, until=iso(n + timedelta(hours=48)))
    t.eq(len(alarms_of(r, later["uid"])), 1, "it is listed with until = now + 48 h")
    t.eq(len(alarms_of(r, far["uid"])), 0, "an alarm 10 days ahead is not")
    r = upcoming(u1, until=iso(n + timedelta(days=30)))
    t.eq(len(alarms_of(r, far["uid"])), 0, "an alarm beyond now + 7 d is out of reach even with a 30-day until")
    r = upcoming(u1, until=iso(n + timedelta(days=9)))
    t.eq(len(alarms_of(r, far["uid"])), 0, "until is clamped to now + 7 d, a 9-day until does not reach it")
    r = upcoming(u1, until=iso(n + timedelta(hours=1)))
    t.eq(len(alarms_of(r, recent["uid"])), 1, "a short until still lists the past alarm")
    t.eq(len(alarms_of(r, later["uid"])), 0)


def test_scope_and_isolation(t, e2e, u1, u2, al):
    """upcoming covers the caller's readable calendars: a shared one is visible to the sharee, a private one is not"""
    start = now_utc() + timedelta(hours=8)
    pub = mk_ev(u1, al.shared, f"Shared alarm {e2e.run_id}", start, reminders=[{"offset_seconds": -600}])
    priv = mk_ev(u1, al.private, f"Private alarm {e2e.run_id}", start, reminders=[{"offset_seconds": -600}])
    r1, r2 = upcoming(u1), upcoming(u2)
    t.status(r2, 200)
    t.eq(len(alarms_of(r1, pub["uid"])), 1, "owner sees the shared calendar's alarm")
    t.eq(len(alarms_of(r1, priv["uid"])), 1, "owner sees the private calendar's alarm")
    mine, theirs = alarms_of(r1, pub["uid"]), alarms_of(r2, pub["uid"])
    if t.eq(len(theirs), 1, "the read-only sharee sees the alarm of the shared calendar") and mine:
        t.eq(theirs[0]["key"], mine[0]["key"], "the sharee gets the same key as the owner")
        t.eq(theirs[0]["trigger_at"], mine[0]["trigger_at"])
        t.eq(theirs[0]["calendar_uuid"], al.shared["uuid"])
    t.eq(len(alarms_of(r2, priv["uid"])), 0, "the other user does not see an alarm of an unshared calendar")


def test_acknowledge(t, e2e, u1, u2, al):
    """acknowledge: 204, the alarm leaves the caller's upcoming only, a repeat is 204 too"""
    start = now_utc() + timedelta(hours=9)
    ev = mk_ev(u1, al.shared, f"Ack {e2e.run_id}", start, reminders=[{"offset_seconds": -900}, {"offset_seconds": -1800}])
    found = alarms_of(upcoming(u1), ev["uid"])
    if not t.eq(len(found), 2, "two alarms before the acknowledgement"):
        return
    acked, other = sorted(found, key=lambda a: a["trigger_at"])[0]["key"], sorted(found, key=lambda a: a["trigger_at"])[1]["key"]
    r = u1.api("POST", "/alarms/acknowledge", json={"keys": [acked]})
    if not t.status(r, 204):
        return
    t.eq(r.content, b"", "204 has no body")
    t.eq([a["key"] for a in alarms_of(upcoming(u1), ev["uid"])], [other],
         "the acknowledged alarm is gone, the other one stays")
    t.eq(sorted(a["key"] for a in alarms_of(upcoming(u2), ev["uid"])), sorted([acked, other]),
         "the second user's list is unaffected (acknowledgements are per user)")
    t.status(u1.api("POST", "/alarms/acknowledge", json={"keys": [acked, acked]}), 204,
             msg="acknowledging again, with a duplicate in the batch, is 204")
    # The other user acknowledges independently.
    t.status(u2.api("POST", "/alarms/acknowledge", json={"keys": [acked]}), 204)
    t.eq([a["key"] for a in alarms_of(upcoming(u2), ev["uid"])], [other], "u2's own acknowledgement works")
    t.eq([a["key"] for a in alarms_of(upcoming(u1), ev["uid"])], [other], "and changes nothing for u1")
    t.status(u1.api("POST", "/alarms/acknowledge", json={"keys": [other]}), 204)
    t.eq(alarms_of(upcoming(u1), ev["uid"]), [], "both alarms acknowledged")
    # A moved event has new keys and is listed again.
    r = u1.api("PATCH", ev_url(al.shared, ev), json={"start": iso(start + timedelta(minutes=30)),
                                                     "end": iso(start + timedelta(minutes=90))})
    if t.status(r, 200):
        again = alarms_of(upcoming(u1), ev["uid"])
        t.eq(len(again), 2, "after moving the event its alarms are listed again")
        t.true(not {a["key"] for a in again} & {acked, other}, "with new keys")


def test_acknowledge_rejections(t, e2e, u1, al):
    """acknowledge: missing, empty, malformed, uppercase or non-string keys are 400 and write nothing"""
    start = now_utc() + timedelta(hours=11)
    ev = mk_ev(u1, al.private, f"Ack bad {e2e.run_id}", start, reminders=[{"offset_seconds": -900}])
    found = alarms_of(upcoming(u1), ev["uid"])
    if not t.eq(len(found), 1, "one alarm"):
        return
    good = found[0]["key"]
    ack = lambda body: u1.api("POST", "/alarms/acknowledge", json=body)  # noqa: E731
    cases = [
        ({}, "keys is required"),
        ({"keys": []}, "keys is required"),
        ({"keys": None}, "keys is required"),
        ({"keys": ["abc"]}, "Invalid alarm key at index 0"),
        ({"keys": [""]}, "Invalid alarm key at index 0"),
        ({"keys": ["A" * 32]}, "Invalid alarm key at index 0"),
        ({"keys": ["g" * 32]}, "Invalid alarm key at index 0"),
        ({"keys": ["0" * 31]}, "Invalid alarm key at index 0"),
        ({"keys": ["0" * 33]}, "Invalid alarm key at index 0"),
        ({"keys": [" " + "0" * 31]}, "Invalid alarm key at index 0"),
        ({"keys": [good, "nope"]}, "Invalid alarm key at index 1"),
        ({"keys": [good, good, good.upper()]}, "Invalid alarm key at index 2"),
        ({"keys": "0" * 32}, "Invalid request body"),
        ({"keys": [123]}, "Invalid request body"),
        ({"keys": {"a": 1}}, "Invalid request body"),
    ]
    for body, text in cases:
        r = ack(body)
        if t.status(r, 400, msg=f"body {body!r:.70}"):
            t.eq(r.jmsg(), text, f"the 400 text for {body!r:.70}")
    r = u1.api("POST", "/alarms/acknowledge", json="{not json")
    if t.status(r, 400, msg="malformed JSON"):
        t.eq(r.jmsg(), "Invalid request body")
    t.eq([a["key"] for a in alarms_of(upcoming(u1), ev["uid"])], [good],
         "a refused batch writes nothing: the valid key in it is still listed")


def test_acknowledge_limit(t, e2e, u1):
    """acknowledge: 501 keys are refused (400), 500 are accepted, a key that belongs to no alarm is accepted"""
    keys = [f"{i:032x}" for i in range(1, 502)]
    r = u1.api("POST", "/alarms/acknowledge", json={"keys": keys})
    if t.status(r, 400, msg="501 keys"):
        t.eq(r.jmsg(), "Too many keys: at most 500 per request")
    r = u1.api("POST", "/alarms/acknowledge", json={"keys": keys[:500]})
    t.status(r, 204, msg="500 keys, none of them an alarm of the caller")
    r = u1.api("POST", "/alarms/acknowledge", json={"keys": ["0" * 32]})
    t.status(r, 204, msg="an unknown but well-formed key is accepted (no existence probe)")
    # An oversized batch with a bad key reports the size, not the key.
    r = u1.api("POST", "/alarms/acknowledge", json={"keys": ["x"] + keys[:500]})
    t.status(r, 400)


def test_delete_event_removes_alarm(t, e2e, u1, al):
    """deleting the event removes its alarm from upcoming"""
    ev = mk_ev(u1, al.private, f"Delete {e2e.run_id}", now_utc() + timedelta(hours=12), reminders=[{"offset_seconds": -300}])
    t.eq(len(alarms_of(upcoming(u1), ev["uid"])), 1, "listed before")
    if t.status(u1.api("DELETE", ev_url(al.private, ev)), 204, 200):
        t.eq(alarms_of(upcoming(u1), ev["uid"]), [], "gone after the delete")
