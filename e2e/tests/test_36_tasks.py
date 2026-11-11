"""Tasks (#170) over REST and CalDAV: the per-list task routes (create, read,
list filters, PATCH, complete / uncomplete, the hard DELETE), their refusals,
repeating tasks, sharing, the cross-list GET /tasks and GET /tasks/summary,
plain text, and the round trip between REST and a DAV client (fields, ETags,
foreign properties, sync).

Every date lies in 2030 or later, so nothing depends on the wall clock.
Query strings are built with urlencode, so a UTC offset's '+' goes out as
%2B; the tests that send a raw '+' do so on purpose."""

import re
from datetime import datetime
from types import SimpleNamespace
from urllib.parse import urlencode

import pytest

from harness.data import VTIMEZONE_BERLIN, ical_lines, ics_calendar, ics_todo_due, ics_todo_rrule, ics_vtodo, sync_body
from harness.session import ZERO_UUID, SetupFailed
from harness.xml import el, lp, xcount, xml_text

DESCRIPTION = "task routes, refusals, sharing, /tasks, /tasks/summary, REST <-> DAV round trip"

pytestmark = pytest.mark.usefixtures("u1", "u2")

RECURRING = "Repeating tasks cannot be completed here yet. Complete it in your task app."
READ_ONLY = "You have read-only access to this calendar"
TASK_NOT_FOUND = "Task not found"
CALENDAR_NOT_FOUND = "Calendar not found"

# Every key of a TaskResponse; the nullable ones are always present.
TASK_KEYS = {"id", "calendar_id", "calendar_uuid", "uid", "summary", "description", "status", "completed",
             "completed_at", "percent_complete", "priority", "due", "due_date", "due_all_day", "start",
             "start_date", "is_recurring", "rrule", "etag", "created_at", "updated_at", "reminders"}


@pytest.fixture(scope="module", autouse=True)
def tk(e2e, u1):
    """Feature detection, then U1's DAV password and one task list for the DAV
    round-trip tests (the other tests make their own lists)."""
    r = u1.api("GET", "/tasks/summary?as_of=2030-01-01T00:00:00Z")
    if r.status == 404:
        pytest.skip(f"server predates #170: GET /api/v1/tasks/summary is not routed (HTTP {r.status})")
    u1.require_dav()
    lst = u1.mk_calendar(e2e.uniq("tasks"), kind="tasks")
    return SimpleNamespace(list=lst, col=f"{u1.home_cal}{lst['path']}/")


# --- helpers --------------------------------------------------------------------

def qs(**params) -> str:
    """'?k=v&…' with every value percent-encoded ('+' → %2B, ':' → %3A)."""
    return "?" + urlencode(params)


def tasks_url(cal) -> str:
    return f"/calendars/{cal['uuid']}/tasks"


def task_url(cal, task_id: str) -> str:
    return f"{tasks_url(cal)}/{task_id}"


def mk_task(u, cal, **body) -> dict:
    """POST a task → its raw JSON (deleted with its list). SetupFailed unless 201."""
    r = u.api("POST", tasks_url(cal), json=body)
    if r.status != 201 or not r.jdict().get("id"):
        raise SetupFailed(f"creating task '{body.get('summary')}' failed (HTTP {r.status})", r)
    return r.jdict()


def page(t, u, path) -> dict:
    """GET a task listing → its JSON ({} after a recorded failure)."""
    r = u.api("GET", path)
    if not t.status(r, 200, msg=f"GET {path}"):
        return {}
    return r.jdict()


def summaries(body) -> list:
    return [x.get("summary") for x in body.get("tasks") or []]


def by_uid(t, u, cal, uid) -> dict:
    """The task with UID in CAL (all statuses), {} with a failure recorded."""
    for task in page(t, u, tasks_url(cal) + "?status=all").get("tasks") or []:
        if task.get("uid") == uid:
            return task
    t.fail(f"no task with UID {uid} in calendar {cal['uuid']}")
    return {}


def instant(value):
    """An RFC 3339 timestamp as an aware datetime (fraction cut to µs: Go
    writes up to nanoseconds, PostgreSQL keeps microseconds). None if unparseable."""
    m = re.fullmatch(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d+))?(Z|[+-]\d\d:\d\d)", value or "")
    if not m:
        return None
    frac = (m.group(2) or "0")[:6].ljust(6, "0")
    return datetime.fromisoformat(f"{m.group(1)}.{frac}{'+00:00' if m.group(3) == 'Z' else m.group(3)}")


def same_task(t, want: dict, got: dict, msg: str) -> bool:
    """Two TaskResponses are equal; created_at/updated_at as instants (±1 ms)."""
    ok = True
    for key in ("created_at", "updated_at"):
        w, g = instant(want.get(key)), instant(got.get(key))
        if w is None or g is None or abs((w - g).total_seconds()) > 0.001:
            ok = t.fail(f"{msg}: {key} differs: '{want.get(key)}' vs '{got.get(key)}'") and ok
    def strip(d):
        return {k: v for k, v in d.items() if k not in ("created_at", "updated_at")}
    return t.eq(strip(got), strip(want), msg) and ok


def utc_z(t, task: dict, msg: str) -> None:
    """Every timed value of a TaskResponse is in UTC with a Z."""
    for key in ("due", "start", "completed_at", "created_at", "updated_at"):
        v = task.get(key)
        if v is not None and not (isinstance(v, str) and v.endswith("Z") and instant(v) is not None):
            t.fail(f"{msg}: {key} must be RFC 3339 in UTC (Z), got '{v}'")


def share(t, owner, cal, user, permission) -> str:
    """Share CAL with USER → the share id ('' after a recorded failure)."""
    r = owner.api("POST", f"/calendars/{cal['uuid']}/shares",
                  json={"user_identifier": user.email, "permission": permission})
    if not t.status(r, 201, msg=f"sharing {cal['name']} ({permission})"):
        return ""
    return r.jdict().get("id") or ""


def unquote_etag(etag: str) -> str:
    return etag[1:-1] if len(etag) >= 2 and etag[0] == etag[-1] == '"' else etag


def dav_get(t, u, href):
    """DAV GET → (body text, ETag header); ('', '') with a failure recorded."""
    r = u.dav("GET", href)
    if not t.status(r, 200, msg=f"DAV GET {href}"):
        return "", ""
    return r.text, r.hdr("ETag")


# --- E1: create, get, list ------------------------------------------------------

def test_create_get_list(t, e2e, u1):
    """create answers 201 with every key (UTC due, all-day due_date); get equals it; list filters and pages"""
    cal = u1.mk_calendar(e2e.uniq("tasks-crud"), kind="tasks")
    r = u1.api("POST", tasks_url(cal), json={"summary": "Plan trip", "due": "2030-03-10T09:30:00+01:00"})
    if not t.status(r, 201):
        return
    timed = r.jdict()
    t.eq(set(timed), TASK_KEYS, "a TaskResponse carries exactly its keys, the nullable ones included")
    t.true(re.fullmatch(r"[0-9a-f-]{36}", timed.get("id") or ""), f"id is the object UUID, got '{timed.get('id')}'")
    t.eq(timed.get("uid"), f"{timed.get('id')}@calcard.io", "the server writes UID <id>@calcard.io")
    t.eq(timed.get("calendar_uuid"), cal["uuid"])
    t.eq(timed.get("calendar_id"), cal["id"])
    t.eq(timed.get("due"), "2030-03-10T08:30:00Z", "a +01:00 due is answered in UTC")
    for key, want in (("due_date", None), ("due_all_day", False), ("start", None), ("start_date", None),
                      ("status", "needs-action"), ("completed", False), ("completed_at", None),
                      ("percent_complete", None), ("priority", None), ("description", ""),
                      ("is_recurring", False), ("rrule", None)):
        t.eq(timed.get(key), want, f"new timed task: {key}")
    t.true(timed.get("etag"), "a new task has an ETag")
    utc_z(t, timed, "create (timed)")

    allday = mk_task(u1, cal, summary="Pay rent", due="2030-03-12", due_all_day=True, priority=1)
    t.eq((allday.get("due"), allday.get("due_date"), allday.get("due_all_day")), (None, "2030-03-12", True),
         "an all-day task answers due null and due_date YYYY-MM-DD")
    t.eq(allday.get("priority"), 1)
    berlin = mk_task(u1, cal, summary="Call mum", due="2030-03-11T18:00:00+01:00", timezone="Europe/Berlin")
    t.eq(berlin.get("due"), "2030-03-11T17:00:00Z", "a TZID due is answered in UTC")
    undated = mk_task(u1, cal, summary="Someday")
    done = mk_task(u1, cal, summary="Done already", status="completed")
    t.eq((done.get("status"), done.get("completed"), done.get("percent_complete")), ("completed", True, 100),
         "status completed on create")
    t.true(done.get("completed_at"), "a task created completed has completed_at")
    utc_z(t, done, "create (completed)")

    for task in (timed, allday, berlin, undated, done):
        r = u1.api("GET", task_url(cal, task["id"]))
        if t.status(r, 200, msg=f"GET {task['summary']}"):
            same_task(t, task, r.jdict(), f"GET answers what create answered ({task['summary']})")
            utc_z(t, r.jdict(), f"GET {task['summary']}")

    body = page(t, u1, tasks_url(cal))
    t.eq(summaries(body), ["Plan trip", "Call mum", "Pay rent", "Someday"],
         "default: open tasks only, by due date, undated last")
    t.eq((body.get("count"), body.get("total")), (4, 4), "count and total")
    for task in body.get("tasks") or []:
        utc_z(t, task, f"list {task.get('summary')}")
    body = page(t, u1, tasks_url(cal) + "?status=completed")
    t.eq(summaries(body), ["Done already"], "status=completed")
    body = page(t, u1, tasks_url(cal) + "?status=all")
    t.eq(len(body.get("tasks") or []), 5, "status=all lists every task")
    body = page(t, u1, tasks_url(cal) + qs(q="PAY R"))
    t.eq(summaries(body), ["Pay rent"], "q matches a substring, case-insensitively")
    body = page(t, u1, tasks_url(cal) + "?limit=2&offset=1")
    t.eq(summaries(body), ["Call mum", "Pay rent"], "limit/offset page through the server order")
    t.eq((body.get("count"), body.get("total")), (2, 4), "count is the page, total all open tasks")


# --- E2: PATCH, complete, uncomplete ---------------------------------------------

def test_patch_complete_uncomplete(t, e2e, u1, tk):
    """PATCH null removes, an unchanged body keeps the etag; complete is idempotent; uncomplete resets only 100%"""
    cal = u1.mk_calendar(e2e.uniq("tasks-edit"), kind="tasks")
    task = mk_task(u1, cal, summary="Paint the fence", description="White", priority=5, percent_complete=40)
    url = task_url(cal, task["id"])

    r = u1.api("PATCH", url, json={"description": None, "priority": None})
    if not t.status(r, 200):
        return
    patched = r.jdict()
    t.eq((patched.get("description"), patched.get("priority")), ("", None), "null removes description and priority")
    t.eq(patched.get("percent_complete"), 40, "an absent key keeps its value")
    t.ne(patched.get("etag"), task.get("etag"), "a change gives a new ETag")

    for body in ({"summary": patched.get("summary"), "percent_complete": 40, "description": ""}, {}):
        r = u1.api("PATCH", url, json=body)
        if t.status(r, 200, msg=f"PATCH {body}"):
            t.eq(r.jdict().get("etag"), patched.get("etag"), f"PATCH {body} changes nothing, so the ETag stays")

    r = u1.api("POST", f"{url}/complete")
    if not t.status(r, 200):
        return
    done = r.jdict()
    t.eq((done.get("status"), done.get("completed"), done.get("percent_complete")), ("completed", True, 100))
    t.true((done.get("completed_at") or "").endswith("Z") and instant(done.get("completed_at")),
           f"completed_at is UTC, got '{done.get('completed_at')}'")
    r = u1.api("POST", f"{url}/complete")
    if t.status(r, 200, msg="complete again"):
        t.eq(r.jdict().get("etag"), done.get("etag"), "completing twice writes nothing (same ETag)")
        t.eq(r.jdict().get("completed_at"), done.get("completed_at"), "and keeps completed_at")

    r = u1.api("POST", f"{url}/uncomplete")
    if not t.status(r, 200):
        return
    reopened = r.jdict()
    t.eq((reopened.get("status"), reopened.get("completed"), reopened.get("completed_at")),
         ("needs-action", False, None))
    t.eq(reopened.get("percent_complete"), 0, "100% goes back to 0, the 40% before completing is not restored")
    r = u1.api("POST", f"{url}/uncomplete")
    if t.status(r, 200, msg="uncomplete again"):
        t.eq(r.jdict().get("etag"), reopened.get("etag"), "uncompleting an open task writes nothing")

    # A client may complete a task at less than 100%: uncomplete keeps that value.
    uid = e2e.uniq("partly-done")
    href = f"{tk.col}{uid}.ics"
    if not t.status(u1.dav("PUT", href, ics=ics_todo_due(uid, "Partly done", "20300320T120000Z", "STATUS:COMPLETED",
                                                         "COMPLETED:20300319T120000Z", "PERCENT-COMPLETE:60")), 201):
        return
    partly = by_uid(t, u1, tk.list, uid)
    if not partly:
        return
    t.eq((partly.get("completed"), partly.get("percent_complete")), (True, 60), "a DAV task completed at 60%")
    r = u1.api("POST", f"{task_url(tk.list, partly['id'])}/uncomplete")
    if t.status(r, 200):
        t.eq((r.jdict().get("status"), r.jdict().get("percent_complete")), ("needs-action", 60),
             "uncomplete resets the percentage only when it is 100")


# --- E3: DELETE -------------------------------------------------------------------

def test_delete(t, e2e, u1):
    """DELETE is 204 and permanent: GET and a second DELETE answer 404 Task not found"""
    cal = u1.mk_calendar(e2e.uniq("tasks-delete"), kind="tasks")
    task = mk_task(u1, cal, summary="Throw away")
    url = task_url(cal, task["id"])
    r = u1.api("DELETE", url)
    if not t.status(r, 204):
        return
    t.eq(r.text, "", "204 has no body")
    for method in ("GET", "DELETE"):
        r = u1.api(method, url)
        if t.status(r, 404, msg=f"{method} after the delete"):
            t.eq(r.jmsg(), TASK_NOT_FOUND)
    t.eq(summaries(page(t, u1, tasks_url(cal) + "?status=all")), [], "the list is empty again")


# --- E4: refusals -----------------------------------------------------------------

def test_refusals(t, e2e, u1, u2):
    """refusals: 400 texts, one 404 body for every miss (a read sharee too), Calendar not found, stale If-Match 412"""
    cal = u1.mk_calendar(e2e.uniq("tasks-refuse"), kind="tasks")
    other = u1.mk_calendar(e2e.uniq("tasks-refuse-other"), kind="tasks")
    mixed = u1.mk_calendar(e2e.uniq("tasks-refuse-mixed"), kind="both")
    task = mk_task(u1, cal, summary="Stays as it is")
    for body, msg in (({"summary": "   "}, "summary is required"),
                      ({"summary": "x", "start": "2030-03-10T09:00:00Z", "due": "2030-03-10T08:00:00Z"},
                       "due must not be before start"),
                      ({"summary": "x", "priority": 10}, "priority must be between 0 and 9")):
        r = u1.api("POST", tasks_url(cal), json=body)
        if t.status(r, 400, msg=f"create {body}"):
            t.eq(r.jmsg(), msg, f"create {body}")
    r = u1.api("GET", tasks_url(cal) + "?status=done")
    if t.status(r, 400, msg="status=done"):
        t.eq(r.jmsg(), 'status must be "open", "completed" or "all"')

    unknown = u1.api("GET", task_url(cal, ZERO_UUID))
    if not t.status(unknown, 404, msg="reference: an unknown task") or not t.eq(unknown.jmsg(), TASK_NOT_FOUND):
        return
    event = u1.mk_event(mixed["uuid"], f"Not a task {e2e.run_id}", "2030-03-10T09:00:00Z", "2030-03-10T10:00:00Z")
    verbs = (("GET", "", None), ("PATCH", "", {"summary": "changed"}), ("DELETE", "", None),
             ("POST", "/complete", None), ("POST", "/uncomplete", None))
    misses = ((u1, task_url(other, task["id"]), "a task under another of the caller's lists"),
              (u1, task_url({"uuid": ZERO_UUID}, task["id"]), "a task under an unknown list"),
              (u1, task_url(cal, ZERO_UUID), "an unknown task"),
              (u1, task_url(mixed, event["id"]), "an event's UUID"))
    for user, url, what in misses:
        for method, suffix, body in verbs:
            r = user.api(method, url + suffix, **({"json": body} if body is not None else {}))
            t.status(r, 404, msg=f"{method}{suffix} of {what}")
            t.eq(r.text, unknown.text, f"{method}{suffix} of {what} answers like an unknown task")

    # A read sharee reads the mixed calendar but cannot write it: a task there is
    # a 403 to them, an event's UUID still the 404 of an unknown task.
    if share(t, u1, mixed, u2, "read"):
        real = mk_task(u1, mixed, summary="Shared chore")
        t.status(u2.api("GET", task_url(mixed, real["id"])), 200, msg="control: the sharee reads a task")
        t.status(u2.api("PATCH", task_url(mixed, real["id"]), json={"summary": "x"}), 403,
                 msg="control: a read sharee's write on a task is 403")
        for method, suffix, body in verbs:
            r = u2.api(method, task_url(mixed, event["id"]) + suffix, **({"json": body} if body is not None else {}))
            t.status(r, 404, msg=f"read sharee: {method}{suffix} of an event's UUID")
            t.eq(r.text, unknown.text, f"read sharee: {method}{suffix} of an event's UUID is an unknown task")

    for method in ("GET", "POST"):
        r = u1.api(method, tasks_url({"uuid": ZERO_UUID}), **({"json": {"summary": "x"}} if method == "POST" else {}))
        if t.status(r, 404, msg=f"{method} on the tasks of an unknown list"):
            t.eq(r.jmsg(), CALENDAR_NOT_FOUND)

    r = u1.api("PATCH", task_url(cal, task["id"]), json={"summary": "stale write"}, headers={"If-Match": '"stale"'})
    if t.status(r, 412, msg="a stale If-Match"):
        t.eq(r.jmsg(), "ETag precondition failed")
    r = u1.api("GET", task_url(cal, task["id"]))
    if t.status(r, 200, msg="the task survives every refusal"):
        t.eq((r.jdict().get("summary"), r.jdict().get("etag")), ("Stays as it is", task["etag"]),
             "nothing was written")


# --- E5: repeating tasks ------------------------------------------------------------

def test_recurring(t, e2e, u1, tk):
    """a repeating task (DAV RRULE) cannot be completed over REST: 409 with the sentence, DAV bytes unchanged"""
    uid = e2e.uniq("weekly")
    href = f"{tk.col}{uid}.ics"
    if not t.status(u1.dav("PUT", href, ics=ics_todo_rrule(uid, "Water the plants", "20300107T090000Z", "FREQ=WEEKLY",
                                                         "DTSTART:20300107T080000Z")), 201):
        return
    task = by_uid(t, u1, tk.list, uid)
    if not task:
        return
    t.eq((task.get("is_recurring"), task.get("rrule")), (True, "FREQ=WEEKLY"), "REST sees the repeat rule")
    before, etag = dav_get(t, u1, href)
    url = task_url(tk.list, task["id"])
    for method, suffix, body in (("POST", "/complete", None), ("PATCH", "", {"status": "completed"}),
                                 ("PATCH", "", {"percent_complete": 100})):
        r = u1.api(method, url + suffix, **({"json": body} if body is not None else {}))
        if t.status(r, 409, msg=f"{method}{suffix} {body or ''} on a repeating task"):
            t.eq(r.jmsg(), RECURRING)
    after, etag_after = dav_get(t, u1, href)
    t.true(before and after == before, "the DAV object is byte-identical after the refusals")
    t.eq(etag_after, etag, "and keeps its ETag")


# --- E6: sharing ---------------------------------------------------------------------

def test_sharing(t, e2e, u1, u2):
    """sharing: no share is 404 everywhere; a read sharee reads and gets 403 on writes; read-write completes"""
    cal = u1.mk_calendar(e2e.uniq("tasks-shared"), kind="tasks")
    task = mk_task(u1, cal, summary="Shared errand", due="2030-04-01T10:00:00Z")
    url = task_url(cal, task["id"])
    writes = (("PATCH", url, {"summary": "hijacked"}), ("DELETE", url, None), ("POST", url + "/complete", None),
              ("POST", url + "/uncomplete", None))

    for method, path, body in (("GET", url, None), *writes):
        r = u2.api(method, path, **({"json": body} if body is not None else {}))
        if t.status(r, 404, msg=f"no share: {method} {path}"):
            t.eq(r.jmsg(), TASK_NOT_FOUND)
    for method, path, body in (("GET", tasks_url(cal), None), ("POST", tasks_url(cal), {"summary": "planted"}),
                               ("GET", "/tasks" + qs(calendar_uuid=cal["uuid"]), None)):
        r = u2.api(method, path, **({"json": body} if body is not None else {}))
        if t.status(r, 404, msg=f"no share: {method} {path}"):
            t.eq(r.jmsg(), CALENDAR_NOT_FOUND)

    share_id = share(t, u1, cal, u2, "read")
    if not share_id:
        return
    t.eq(summaries(page(t, u2, tasks_url(cal))), ["Shared errand"], "a read sharee lists the tasks")
    t.status(u2.api("GET", url), 200, msg="a read sharee reads the task")
    t.eq(summaries(page(t, u2, "/tasks" + qs(calendar_uuid=cal["uuid"]))), ["Shared errand"],
         "GET /tasks follows the share")
    for method, path, body in (*writes, ("POST", tasks_url(cal), {"summary": "planted"})):
        r = u2.api(method, path, **({"json": body} if body is not None else {}))
        if t.status(r, 403, msg=f"read share: {method} {path}"):
            t.eq(r.jmsg(), READ_ONLY)
    r = u1.api("GET", url)
    t.eq((r.jdict().get("summary"), r.jdict().get("etag")), ("Shared errand", task["etag"]),
         "the read sharee's writes changed nothing")
    t.eq(summaries(page(t, u1, tasks_url(cal) + "?status=all")), ["Shared errand"], "nothing was planted")

    r = u1.api("PATCH", f"/calendars/{cal['uuid']}/shares/{share_id}", json={"permission": "read-write"})
    if not t.status(r, 200, msg="upgrading the share to read-write"):
        return
    r = u2.api("POST", url + "/complete")
    if t.status(r, 200, msg="a read-write sharee completes"):
        t.eq(r.jdict().get("completed"), True)
    t.eq(u1.api("GET", url).jdict().get("status"), "completed", "the owner sees the sharee's completion")


# --- E7: GET /tasks and GET /tasks/summary ------------------------------------------

def test_cross_list_and_summary(t, e2e, u1):
    """GET /tasks (name/colour, calendar_uuid, due window) and /tasks/summary counts as of a fixed 2030 time"""
    ux = e2e.create_user("TASKS", "E2E Task Summary")  # u1's other suites put tasks in its lists
    a = ux.mk_calendar(e2e.uniq("summary-a"), "#112233", kind="tasks")
    b = ux.mk_calendar(e2e.uniq("summary-b"), "#445566", kind="both")
    # As of 2030-06-15 12:00 +02:00 (10:00Z), today is 2030-06-15 in that offset.
    for cal, body in ((a, {"summary": "a1 overdue", "due": "2030-06-14T09:00:00Z"}),
                      (a, {"summary": "a2 later today", "due": "2030-06-15T18:00:00+02:00"}),
                      (a, {"summary": "a3 earlier today", "due": "2030-06-15T08:00:00+02:00"}),
                      (a, {"summary": "a4 all day today", "due": "2030-06-15", "due_all_day": True}),
                      (a, {"summary": "a5 done", "due": "2030-06-01T09:00:00Z", "status": "completed"}),
                      (a, {"summary": "a6 cancelled", "due": "2030-06-01T09:00:00Z", "status": "cancelled"}),
                      (b, {"summary": "b1 next month", "due": "2030-07-01T09:00:00Z"}),
                      (b, {"summary": "b2 undated"})):
        mk_task(ux, cal, **body)

    r = ux.api("GET", "/calendars/")
    lists = {c["uuid"]: c for c in r.jdict().get("calendars") or [] if c.get("kind") in ("tasks", "both")}
    t.eq(len(lists), 3, "the scratch user's task lists: its default Tasks list, A and B")

    body = page(t, ux, "/tasks?status=all")
    t.eq(body.get("total"), 8, "GET /tasks spans every task list")
    for task in body.get("tasks") or []:
        cal = lists.get(task.get("calendar_uuid"), {})
        t.eq((task.get("calendar_name"), task.get("calendar_color")), (cal.get("name"), cal.get("color")),
             f"{task.get('summary')} carries its list's name and colour")
    body = page(t, ux, "/tasks" + qs(calendar_uuid=a["uuid"], status="all"))
    t.eq(sorted(summaries(body)), ["a1 overdue", "a2 later today", "a3 earlier today", "a4 all day today", "a5 done",
                                   "a6 cancelled"], "calendar_uuid narrows to one list")
    for unreadable in (ZERO_UUID, u1.mk_calendar(e2e.uniq("summary-foreign"), kind="tasks")["uuid"]):
        r = ux.api("GET", "/tasks" + qs(calendar_uuid=unreadable))
        if t.status(r, 404, msg=f"calendar_uuid {unreadable} the caller cannot read"):
            t.eq(r.jmsg(), CALENDAR_NOT_FOUND)
    body = page(t, ux, "/tasks" + qs(due_after="2030-06-15T00:00:00+02:00", due_before="2030-06-16T00:00:00+02:00"))
    t.eq(sorted(summaries(body)), ["a2 later today", "a3 earlier today", "a4 all day today"],
         "the due window [due_after, due_before), offsets sent as %2B")
    r = ux.api("GET", "/tasks?due_after=2030-06-15T00:00:00+02:00")
    if t.status(r, 400, msg="an unencoded '+' arrives as a space"):
        t.eq(r.jmsg(), "Invalid 'due_after' value")

    r = ux.api("GET", "/tasks/summary" + qs(as_of="2030-06-15T12:00:00+02:00"))
    if t.status(r, 200):
        other = next((u for u in lists if u not in (a["uuid"], b["uuid"])), "")
        t.eq(r.json(), {"open": 6, "overdue": 2, "due_today": 3, "completed": 2, "by_calendar": {
            a["uuid"]: {"open": 4, "overdue": 2, "completed": 2}, b["uuid"]: {"open": 2, "overdue": 0, "completed": 0},
            other: {"open": 0, "overdue": 0, "completed": 0}}},
            "overdue a1+a3, due today a2+a3+a4, completed + cancelled = 2, every list in by_calendar")
        done = page(t, ux, "/tasks?status=completed")
        t.eq(r.jdict().get("completed"), done.get("total"), "completed equals the total of ?status=completed")
    for query, msg in (("", "Query parameter 'as_of' is required"),
                       ("?as_of=2030-06-15T12:00:00+02:00", "Invalid 'as_of' value")):
        r = ux.api("GET", "/tasks/summary" + query)
        if t.status(r, 400, msg=f"summary{query or ' without as_of'}"):
            t.eq(r.jmsg(), msg)


# --- E8: plain text --------------------------------------------------------------------

def test_plain_text(t, e2e, u1, tk):
    """summary and notes are plain text both ways; sending them back keeps the etag; DAV escapes them once"""
    summary, notes = "Buy milk, eggs; bread \\ x", "line one, two;\nline \\ three"
    task = mk_task(u1, tk.list, summary=summary, description=notes)
    t.eq((task.get("summary"), task.get("description")), (summary, notes), "create answers the text as sent")
    url = task_url(tk.list, task["id"])
    r = u1.api("GET", url)
    t.eq((r.jdict().get("summary"), r.jdict().get("description")), (summary, notes), "GET answers it unchanged")
    r = u1.api("PATCH", url, json={"summary": r.jdict().get("summary"), "description": r.jdict().get("description")})
    if t.status(r, 200, msg="sending the text back"):
        t.eq(r.jdict().get("etag"), task.get("etag"), "the text the API answered, sent back, writes nothing")
    body, _ = dav_get(t, u1, f"{tk.col}{task['id']}.ics")
    lines = ical_lines(body)
    t.true("SUMMARY:Buy milk\\, eggs\\; bread \\\\ x" in lines, f"DAV: SUMMARY escaped once, got {lines}")
    t.true("DESCRIPTION:line one\\, two\\;\\nline \\\\ three" in lines, f"DAV: DESCRIPTION escaped once, got {lines}")


# --- E9: DAV -> REST ---------------------------------------------------------------------

def test_dav_to_rest(t, e2e, u1, tk):
    """a DAV-written VTODO (TZID due, priority, percent, in-process) is listed by REST with the DAV ETag"""
    uid = e2e.uniq("plumber")
    href = f"{tk.col}{uid}.ics"

    def body(summary, percent):
        return ics_calendar(VTIMEZONE_BERLIN, ics_vtodo(
            uid, summary, "DTSTART:20300401T080000Z", "DUE;TZID=Europe/Berlin:20300405T170000", "PRIORITY:2",
            f"PERCENT-COMPLETE:{percent}", "STATUS:IN-PROCESS", "DESCRIPTION:Kitchen sink"))

    if not t.status(u1.dav("PUT", href, ics=body("Call the plumber", 30), headers={"If-None-Match": "*"}), 201):
        return
    _, etag = dav_get(t, u1, href)
    task = next((x for x in page(t, u1, tasks_url(tk.list)).get("tasks") or [] if x.get("uid") == uid), {})
    if not t.true(task, f"GET …/tasks lists the DAV-written task {uid}"):
        return
    for key, want in (("summary", "Call the plumber"), ("description", "Kitchen sink"), ("status", "in-process"),
                      ("completed", False), ("due", "2030-04-05T15:00:00Z"), ("due_all_day", False),
                      ("start", "2030-04-01T08:00:00Z"), ("priority", 2), ("percent_complete", 30),
                      ("etag", unquote_etag(etag))):
        t.eq(task.get(key), want, f"DAV task: {key}")
    body_all = page(t, u1, "/tasks" + qs(calendar_uuid=tk.list["uuid"]))
    listed = next((x for x in body_all.get("tasks") or [] if x.get("uid") == uid), {})
    t.eq((listed.get("id"), listed.get("percent_complete"), listed.get("calendar_name")),
         (task.get("id"), 30, tk.list["name"]), "GET /tasks lists it too, with its list's name")

    r = u1.dav("PUT", href, ics=body("Call the plumber again", 70), headers={"If-Match": etag})
    if not t.status(r, 200, 201, 204, msg="update PUT"):
        return
    _, etag2 = dav_get(t, u1, href)
    r = u1.api("GET", task_url(tk.list, task["id"]))
    if t.status(r, 200, msg="the REST id survives an update PUT"):
        t.eq((r.jdict().get("summary"), r.jdict().get("percent_complete"), r.jdict().get("etag")),
             ("Call the plumber again", 70, unquote_etag(etag2)), "REST sees the update and the new DAV ETag")
    t.ne(etag2, etag, "the update PUT changed the ETag")


# --- E10: REST -> DAV ---------------------------------------------------------------------

def test_rest_to_dav(t, e2e, u1, tk):
    """a REST task is served over DAV (UID, TZID due, VTIMEZONE); complete shows STATUS:COMPLETED; an edit keeps foreign lines"""
    task = mk_task(u1, tk.list, summary="Book flights", due="2030-05-02T10:00:00+02:00", timezone="Europe/Berlin")
    href = f"{tk.col}{task['id']}.ics"
    body, etag = dav_get(t, u1, href)
    lines = ical_lines(body)
    for want in ("BEGIN:VTODO", f"UID:{task['uid']}", "SUMMARY:Book flights", "DUE;TZID=Europe/Berlin:20300502T100000",
                 "STATUS:NEEDS-ACTION", "BEGIN:VTIMEZONE", "TZID:Europe/Berlin"):
        t.true(want in lines, f"DAV GET of the REST task has '{want}': {lines}")
    t.eq(etag, f'"{task["etag"]}"', "DAV and REST report the same ETag")

    r = u1.api("POST", f"{task_url(tk.list, task['id'])}/complete")
    if t.status(r, 200, msg="REST complete"):
        body, etag = dav_get(t, u1, href)
        lines = ical_lines(body)
        t.true("STATUS:COMPLETED" in lines and "PERCENT-COMPLETE:100" in lines,
               f"DAV shows the REST completion: {lines}")
        t.true(any(re.fullmatch(r"COMPLETED:\d{8}T\d{6}Z", x) for x in lines), f"a UTC COMPLETED line: {lines}")
        t.eq(etag, f'"{r.jdict().get("etag")}"', "with the ETag REST answered")

    # A phone's task with an alarm, a parent link, a repeat rule and X- lines.
    uid = e2e.uniq("foreign")
    phone_href = f"{tk.col}{uid}.ics"
    alarm = ["BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Reminder", "TRIGGER:-PT15M", "END:VALARM"]
    foreign = ["RRULE:FREQ=WEEKLY;BYDAY=MO", "RELATED-TO;RELTYPE=PARENT:garden-project@phone",
               "X-APPLE-SORT-ORDER:731492317", "X-CALCARD-E2E;X-PARAM=kept:client value"]
    if not t.status(u1.dav("PUT", phone_href, ics=ics_todo_due(uid, "Water plants", "20300506T090000Z",
                                                               "DTSTART:20300506T080000Z", *foreign, *alarm)), 201):
        return
    phone = by_uid(t, u1, tk.list, uid)
    if not phone:
        return
    r = u1.api("PATCH", task_url(tk.list, phone["id"]), json={"summary": "Water the plants (edited)"})
    if not t.status(r, 200, msg="a REST title edit of a DAV-written task"):
        return
    lines = ical_lines(dav_get(t, u1, phone_href)[0])
    t.true("SUMMARY:Water the plants (edited)" in lines, f"the edit reached DAV: {lines}")
    for want in foreign:
        t.true(want in lines, f"the REST edit keeps '{want}': {lines}")
    start = lines.index("BEGIN:VALARM") if "BEGIN:VALARM" in lines else -1
    t.eq(lines[start:start + len(alarm)] if start >= 0 else [], alarm, "the VALARM block is kept line for line")


# --- E11: sync ------------------------------------------------------------------------------

def test_sync(t, e2e, u1, tk):
    """sync: a REST DELETE is reported once as a 404; a DAV DELETE makes REST answer 404"""
    task = mk_task(u1, tk.list, summary="Sync me away")
    r = u1.dav("REPORT", tk.col, depth=1, xml=sync_body())
    if not t.status(r, 207, msg="initial sync"):
        return
    token = xml_text(r, "sync-token")
    if not t.status(u1.api("DELETE", task_url(tk.list, task["id"])), 204):
        return
    r = u1.dav("REPORT", tk.col, depth=1, xml=sync_body(token))
    if not t.status(r, 207, msg="incremental sync after the REST DELETE"):
        return
    t.xpath(r, f"{lp('response')}[contains({el('href')},'{task['id']}.ics')]/{el('status')}[contains(.,'404')]",
            "the hard-deleted task is reported 404 (RFC 6578 3.5.2)")
    t.eq(xcount(r, lp("response")), 1, "and nothing else changed")
    r = u1.dav("REPORT", tk.col, depth=1, xml=sync_body(xml_text(r, "sync-token")))
    if t.status(r, 207, msg="the next sync"):
        t.eq(xcount(r, lp("response")), 0, "the removal is reported once")

    uid = e2e.uniq("dav-delete")
    href = f"{tk.col}{uid}.ics"
    if not t.status(u1.dav("PUT", href, ics=ics_todo_due(uid, "Deleted by a phone", "20300601T090000Z")), 201):
        return
    phone = by_uid(t, u1, tk.list, uid)
    if not phone or not t.status(u1.dav("DELETE", href), 204, msg="DAV DELETE"):
        return
    r = u1.api("GET", task_url(tk.list, phone["id"]))
    if t.status(r, 404, msg="REST after the DAV DELETE"):
        t.eq(r.jmsg(), TASK_NOT_FOUND)
