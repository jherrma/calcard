"""Events over REST: CRUD, validation, conditional updates, all-day events,
recurrence (expand, scope=this / this_and_future), moving between calendars."""

import re
from types import SimpleNamespace

import pytest

from harness.checks import known

DESCRIPTION = "event CRUD, validation, If-Match, all-day, recurrence scopes, move"

pytestmark = pytest.mark.usefixtures("u1", "u2")

WIDE = "start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z"
ZERO = "00000000-0000-0000-0000-000000000000"


@pytest.fixture(scope="module", autouse=True)
def cal(e2e, u1):
    """Two calendars of U1: the events' home and a move target."""
    return SimpleNamespace(ev=u1.mk_calendar(e2e.uniq("events")), ev2=u1.mk_calendar(e2e.uniq("events-target")))


def _list(u, cal_uuid, query):
    return u.api("GET", f"/calendars/{cal_uuid}/events?{query}")


def test_create(t, e2e, u1, cal, st):
    """create returns the event (201)"""
    r = u1.api("POST", f"/calendars/{cal.ev['uuid']}/events", json={
        "summary": f"Planning {e2e.run_id}", "description": "Plan the quarter", "location": "Room 4",
        "start": "2030-01-15T10:00:00Z", "end": "2030-01-15T11:00:00Z", "timezone": "UTC", "all_day": False})
    if not t.status(r, 201):
        return
    st.ev_id, st.ev_uid = r.jdict().get("id"), r.jdict().get("uid")
    t.jq(r, lambda j: re.fullmatch(r"[0-9a-f-]{36}", j["id"]), "id is the event UUID")
    t.nonempty(r, "uid", "an iCalendar UID is generated")
    t.json(r, "summary", f"Planning {e2e.run_id}")
    t.json(r, "location", "Room 4")
    t.json(r, "start", "2030-01-15T10:00:00Z")
    t.json(r, "end", "2030-01-15T11:00:00Z")
    t.json(r, "all_day", False)
    t.json(r, "is_recurring", False)
    t.json(r, "calendar_id", cal.ev["id"])


def test_get(t, u1, cal, st):
    """get returns the stored event"""
    t.need(st, "ev_id")
    r = u1.api("GET", f"/calendars/{cal.ev['uuid']}/events/{st.ev_id}")
    if not t.status(r, 200):
        return
    t.json(r, "id", st.ev_id)
    t.json(r, "uid", st.ev_uid)
    t.json(r, "description", "Plan the quarter")
    t.status(u1.api("GET", f"/calendars/{cal.ev['uuid']}/events/{ZERO}"), 404)


def test_list_window(t, u1, cal, st):
    """list honours the start/end window"""
    t.need(st, "ev_id")
    r = _list(u1, cal.ev["uuid"], "start=2030-01-01T00:00:00Z&end=2030-02-01T00:00:00Z")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([e for e in j["events"] if e["id"] == st.ev_id]) == 1,
         "event inside the window must be listed")
    t.jq(r, lambda j: j["count"] == len(j["events"]), "count must match the list")
    r = _list(u1, cal.ev["uuid"], "start=2031-01-01T00:00:00Z&end=2031-02-01T00:00:00Z")
    t.status(r, 200)
    t.jq(r, lambda j: len([e for e in j["events"] or [] if e["id"] == st.ev_id]) == 0,
         "event outside the window must not be listed")


def test_list_bad(t, u1, cal):
    """list rejects malformed window bounds"""
    t.status(_list(u1, cal.ev["uuid"], "start=yesterday"), 400)
    t.status(_list(u1, cal.ev["uuid"], "end=2030-13-45"), 400)
    t.status(u1.api("GET", f"/calendars/{ZERO}/events?{WIDE}"), 404)


def test_patch(t, e2e, u1, cal, st):
    """PATCH updates only the given fields"""
    t.need(st, "ev_id")
    r = u1.api("PATCH", f"/calendars/{cal.ev['uuid']}/events/{st.ev_id}", json={
        "summary": f"Planning (moved) {e2e.run_id}", "start": "2030-01-15T12:00:00Z", "end": "2030-01-15T13:30:00Z"})
    if not t.status(r, 200):
        return
    t.json(r, "summary", f"Planning (moved) {e2e.run_id}")
    t.json(r, "start", "2030-01-15T12:00:00Z")
    t.json(r, "end", "2030-01-15T13:30:00Z")
    t.json(r, "location", "Room 4", "fields not in the PATCH keep their value")
    t.json(r, "uid", st.ev_uid, "the UID never changes")


def test_if_match(t, e2e, u1, cal, st):
    """PATCH with a stale If-Match is 412"""
    t.need(st, "ev_id")
    path = f"/calendars/{cal.ev['uuid']}/events/{st.ev_id}"
    r = u1.api("PATCH", path, json='{"summary":"stale write"}', headers={"If-Match": '"definitely-stale"'})
    if not t.status(r, 412):
        return
    r = u1.api("GET", path)
    t.json(r, "summary", f"Planning (moved) {e2e.run_id}", "a rejected conditional write must not change the event")
    r = u1.api("DELETE", path, headers={"If-Match": '"definitely-stale"'})
    t.status(r, 412, msg="conditional delete with a stale ETag")


def test_invalid_range(t, u1, cal):
    """create rejects end before start"""
    path = f"/calendars/{cal.ev['uuid']}/events"
    t.status(u1.api("POST", path, json='{"summary":"backwards","start":"2030-01-01T10:00:00Z","end":"2030-01-01T09:00:00Z"}'), 400)
    t.status(u1.api("POST", path, json='{"summary":"bad","start":"tomorrow","end":"2030-01-01T09:00:00Z"}'), 400)


@known("F13")
def test_missing_times(t, e2e, u1, cal):
    """create rejects a missing start/end"""
    r = u1.api("POST", f"/calendars/{cal.ev['uuid']}/events", json={"summary": f"no times {e2e.run_id}"})
    if r.status == 201:  # F13: accepted and stored at 0001-01-01; do not keep it
        u1.api("DELETE", f"/calendars/{cal.ev['uuid']}/events/{r.jdict().get('id')}")
    t.bug.status(r, 400, was=201, msg=f"an event without start/end (got start={r.jdict().get('start')})")


@known("F04")
def test_dtstamp(t, u1, cal, st):
    """stored iCalendar has a UTC DTSTAMP (RFC 5545 3.8.7.2)"""
    t.need(st, "ev_id")
    r = u1.api("GET", f"/calendars/{cal.ev['uuid']}/export")
    if not t.status(r, 200):
        return
    line = next((ln for ln in r.text.replace("\r", "").split("\n") if ln.startswith("DTSTAMP")), "")
    if not line:
        return t.fail("exported VEVENT has no DTSTAMP", r)
    # F04: the server writes DTSTAMP;TZID=Local:… (time.Now() instead of UTC).
    t.bug.true(re.fullmatch(r"DTSTAMP:[0-9]{8}T[0-9]{6}Z", line),
               f"DTSTAMP must be a UTC date-time like DTSTAMP:20300101T000000Z, got '{line}'",
               was=line.startswith("DTSTAMP;TZID="))


def test_allday(t, e2e, u1, cal):
    """all-day events round-trip as DATE values"""
    ev = u1.mk_event(cal.ev["uuid"], f"All day {e2e.run_id}", "2030-02-01T00:00:00Z", "2030-02-02T00:00:00Z",
                     all_day=True)
    r = u1.api("GET", f"/calendars/{cal.ev['uuid']}/events/{ev['id']}")
    if not t.json(r, "all_day", True):
        return
    t.jq(r, lambda j: j["start"].startswith("2030-02-01"), "the date must survive")
    r = u1.api("GET", f"/calendars/{cal.ev['uuid']}/export")
    t.match(r, "DTSTART;VALUE=DATE:20300201", "all-day events are stored as DATE values")


def test_wrong_calendar(t, e2e, u1, cal, st):
    """event URL must match its calendar (F09, fixed)"""
    t.need(st, "ev_id")
    unknown = u1.api("GET", f"/calendars/{cal.ev['uuid']}/events/{ZERO}")
    if not t.status(unknown, 404, msg="reference: an unknown event"):
        return
    for wrong, what in ((cal.ev2["uuid"], "a calendar it does not belong to"), (ZERO, "a non-existent calendar")):
        path = f"/calendars/{wrong}/events/{st.ev_id}"
        for method, kw in (("GET", {}), ("PATCH", {"json": '{"summary":"wrong calendar"}'}), ("DELETE", {}),
                           ("POST", {"json": {"target_calendar_id": cal.ev2["uuid"]}})):
            r = u1.api(method, path + ("/move" if method == "POST" else ""), **kw)
            t.status(r, 404, msg=f"{method} of an event addressed through {what}")
            t.eq(r.text, unknown.text, f"{method} through {what} must answer like an unknown event")
    r = u1.api("GET", f"/calendars/{cal.ev['uuid']}/events/{st.ev_id}")
    t.json(r, "summary", f"Planning (moved) {e2e.run_id}", "the event is untouched")
    t.json(r, "calendar_id", cal.ev["id"], "and still in its own calendar")


def test_isolation(t, e2e, u1, u2, cal, st):
    """another user cannot read, change, delete or move it"""
    t.need(st, "ev_id")
    c, ev = cal.ev["uuid"], st.ev_id
    t.status(u2.api("GET", f"/calendars/{c}/events?{WIDE}"), 404, msg="U2 listing U1's calendar")
    t.status(u2.api("GET", f"/calendars/{c}/events/{ev}"), 404, msg="U2 reading U1's event")
    t.status(u2.api("PATCH", f"/calendars/{c}/events/{ev}", json='{"summary":"hijacked"}'), 404,
             msg="U2 patching U1's event")
    t.status(u2.api("DELETE", f"/calendars/{c}/events/{ev}"), 404, msg="U2 deleting U1's event")
    r = u2.api("POST", f"/calendars/{c}/events",
               json='{"summary":"intruder","start":"2030-01-01T10:00:00Z","end":"2030-01-01T11:00:00Z"}')
    t.status(r, 404, msg="U2 creating in U1's calendar")
    r = u2.api("GET", "/calendars/")
    j = r.json()
    u2cal = j["calendars"][0]["uuid"] if isinstance(j, dict) and j.get("calendars") else None
    r = u2.api("POST", f"/calendars/{c}/events/{ev}/move", json={"target_calendar_id": u2cal})
    t.status(r, 404, msg="U2 moving U1's event into their own calendar")
    r = u1.api("GET", f"/calendars/{c}/events/{ev}")
    t.json(r, "summary", f"Planning (moved) {e2e.run_id}", "U1's event must be untouched")


def _rrule_event(t, u1, cal, summary, start, end, recurrence):
    """Create a series in the events calendar → id, or None (failure recorded)."""
    r = u1.api("POST", f"/calendars/{cal.ev['uuid']}/events", json={
        "summary": summary, "start": start, "end": end, "timezone": "UTC", "all_day": False, "recurrence": recurrence})
    if r.status != 201:
        t.fail("create recurring event failed", r)
        return None
    return r.json()["id"]


def _instances(u1, cal, prefix, window):
    """Expanded instances whose summary starts with PREFIX, sorted by start."""
    r = _list(u1, cal.ev["uuid"], f"{window}&expand=true")
    j = r.json()
    try:
        inst = sorted((e for e in j["events"] if e["summary"].startswith(prefix)), key=lambda e: e["start"])
    except (TypeError, KeyError, AttributeError):
        inst = []
    return r, inst


def test_rrule_expand(t, e2e, u1, cal):
    """recurrence: expand=true yields every instance"""
    p = f"Weekly A {e2e.run_id}"
    id_ = _rrule_event(t, u1, cal, p, "2033-05-02T10:00:00Z", "2033-05-02T11:00:00Z", {"frequency": "WEEKLY", "count": 4})
    if id_ is None:
        return
    r, inst = _instances(u1, cal, p, "start=2033-05-01T00:00:00Z&end=2033-06-30T00:00:00Z")
    if not t.status(r, 200):
        return
    if not t.eq(len(inst), 4, "COUNT=4 must expand to 4 instances"):
        return
    t.eq(",".join(str(e.get("recurrence_id")) for e in inst),
         "20330502T100000Z,20330509T100000Z,20330516T100000Z,20330523T100000Z", "recurrence ids of the instances")
    t.eq(all(e.get("is_recurring") for e in inst), True, "instances are flagged is_recurring")
    r = _list(u1, cal.ev["uuid"], "start=2033-05-01T00:00:00Z&end=2033-06-30T00:00:00Z&expand=false")
    t.jq(r, lambda j: len([e for e in j["events"] if e["id"] == id_]) == 1, "expand=false returns the series once")


def test_rrule_byday(t, e2e, u1, cal):
    """recurrence: BYDAY rule lands on the right weekdays"""
    p = f"Byday {e2e.run_id}"
    if _rrule_event(t, u1, cal, p, "2033-09-05T08:00:00Z", "2033-09-05T08:30:00Z",
                    {"frequency": "WEEKLY", "by_day": ["MO", "WE"], "count": 4}) is None:
        return
    _, inst = _instances(u1, cal, p, "start=2033-09-01T00:00:00Z&end=2033-10-01T00:00:00Z")
    t.eq(",".join(e["start"][0:10] for e in inst), "2033-09-05,2033-09-07,2033-09-12,2033-09-14",
         "Mondays and Wednesdays, 4 in total")


def test_rrule_delete_this(t, e2e, u1, cal):
    """recurrence: delete scope=this removes one instance"""
    p = f"Weekly B {e2e.run_id}"
    id_ = _rrule_event(t, u1, cal, p, "2033-07-04T14:00:00Z", "2033-07-04T14:30:00Z", {"frequency": "WEEKLY", "count": 4})
    if id_ is None:
        return
    r = u1.api("DELETE", f"/calendars/{cal.ev['uuid']}/events/{id_}?scope=this&recurrence_id=20330711T140000Z")
    if not t.status(r, 204):
        return
    _, inst = _instances(u1, cal, p, "start=2033-07-01T00:00:00Z&end=2033-08-31T00:00:00Z")
    t.eq(",".join(str(e.get("recurrence_id")) for e in inst),
         "20330704T140000Z,20330718T140000Z,20330725T140000Z", "exactly the second instance is excluded (EXDATE)")


def test_rrule_update_this(t, e2e, u1, cal):
    """recurrence: update scope=this edits one instance"""
    p = f"Weekly C {e2e.run_id}"
    id_ = _rrule_event(t, u1, cal, p, "2033-08-01T09:00:00Z", "2033-08-01T10:00:00Z", {"frequency": "WEEKLY", "count": 4})
    if id_ is None:
        return
    r = u1.api("PATCH", f"/calendars/{cal.ev['uuid']}/events/{id_}?scope=this&recurrence_id=20330808T090000Z",
               json={"summary": f"{p} edited"})
    if not t.status(r, 200):
        return
    _, inst = _instances(u1, cal, p, "start=2033-07-25T00:00:00Z&end=2033-09-15T00:00:00Z")
    t.eq(len(inst), 4, "scope=this keeps the instance count")
    target = [e for e in inst if e.get("recurrence_id") == "20330808T090000Z"]
    others = [e for e in inst if e.get("recurrence_id") != "20330808T090000Z"]
    t.eq("".join(e["summary"] for e in target), f"{p} edited", "the targeted instance carries the edit")
    t.eq("".join(sorted({e["summary"] for e in others})), p, "every other instance keeps the series summary")
    t.eq("".join(e["start"] for e in target), "2033-08-08T09:00:00Z", "the edited instance keeps its own start")


def test_rrule_update_future(t, e2e, u1, cal):
    """recurrence: update scope=this_and_future splits the series"""
    p = f"Weekly D {e2e.run_id}"
    id_ = _rrule_event(t, u1, cal, p, "2033-10-03T15:00:00Z", "2033-10-03T15:30:00Z", {"frequency": "WEEKLY", "count": 4})
    if id_ is None:
        return
    r = u1.api("PATCH", f"/calendars/{cal.ev['uuid']}/events/{id_}?scope=this_and_future&recurrence_id=20331017T150000Z",
               json={"summary": f"{p} renamed"})
    if not t.status(r, 200):
        return
    _, inst = _instances(u1, cal, p, "start=2033-09-20T00:00:00Z&end=2033-11-15T00:00:00Z")
    if not t.eq(len(inst), 4, "a split must preserve the total instance count"):
        return
    t.eq("|".join(e["summary"] for e in inst), f"{p}|{p}|{p} renamed|{p} renamed",
         "instances before the split keep the old summary, the rest carry the new one")


def test_rrule_delete_future(t, e2e, u1, cal):
    """recurrence: delete scope=this_and_future truncates"""
    p = f"Weekly E {e2e.run_id}"
    id_ = _rrule_event(t, u1, cal, p, "2033-11-07T07:00:00Z", "2033-11-07T07:30:00Z", {"frequency": "WEEKLY", "count": 4})
    if id_ is None:
        return
    r = u1.api("DELETE", f"/calendars/{cal.ev['uuid']}/events/{id_}?scope=this_and_future&recurrence_id=20331121T070000Z")
    if not t.status(r, 204):
        return
    _, inst = _instances(u1, cal, p, "start=2033-11-01T00:00:00Z&end=2033-12-31T00:00:00Z")
    t.eq(",".join(str(e.get("recurrence_id")) for e in inst), "20331107T070000Z,20331114T070000Z",
         "only the instances before the split remain (UNTIL)")
    r = u1.api("DELETE", f"/calendars/{cal.ev['uuid']}/events/{id_}?scope=bogus&recurrence_id=20331107T070000Z")
    t.status(r, 400, msg="an unknown scope is a client error")


def test_move(t, e2e, u1, cal):
    """move to another own calendar keeps the UID"""
    ev = u1.mk_event(cal.ev["uuid"], f"Mover {e2e.run_id}", "2031-08-01T10:00:00Z", "2031-08-01T11:00:00Z")
    id_, uid = ev["id"], ev["uid"]
    r = u1.api("POST", f"/calendars/{cal.ev['uuid']}/events/{id_}/move", json={"target_calendar_id": cal.ev2["uuid"]})
    if not t.status(r, 200):
        return
    t.json(r, "uid", uid, "the UID survives a move")
    t.json(r, "calendar_id", cal.ev2["id"])
    r = _list(u1, cal.ev["uuid"], f"{WIDE}&expand=false")
    t.jq(r, lambda j: len([e for e in j["events"] or [] if e["uid"] == uid]) == 0, "gone from the source calendar")
    r = _list(u1, cal.ev2["uuid"], f"{WIDE}&expand=false")
    t.jq(r, lambda j: len([e for e in j["events"] if e["uid"] == uid]) == 1, "present in the target calendar")
    r = u1.api("POST", f"/calendars/{cal.ev2['uuid']}/events/{id_}/move",
               json='{"target_calendar_id":"00000000-0000-0000-0000-000000000000"}')
    t.status(r, 404, msg="moving into an unknown calendar")


def test_delete(t, u1, cal, st):
    """delete is 204, then the event is gone"""
    t.need(st, "ev_id")
    path = f"/calendars/{cal.ev['uuid']}/events/{st.ev_id}"
    if not t.status(u1.api("DELETE", path), 204):
        return
    t.status(u1.api("GET", path), 404)
    t.status(u1.api("DELETE", path), 404, msg="deleting twice")
