"""CalDAV (RFC 4791) in depth: collection creation (MKCALENDAR / extended
MKCOL), object PUT/GET/HEAD/DELETE with ETag preconditions, UID uniqueness,
VTODO, recurrence overrides, calendar-query / calendar-multiget REPORTs,
PROPPATCH, COPY/MOVE, REST <-> DAV consistency."""

import re

import pytest

from harness import out
from harness.checks import known
from harness.data import PROPFIND_COLLECTION, PROPFIND_LIST, ics_calendar, ics_event, ics_todo, ics_vevent
from harness.session import SetupFailed
from harness.xml import el, lp, xcount, xstr

DESCRIPTION = "MKCALENDAR, PUT/GET/DELETE + ETags, REPORTs, PROPPATCH, COPY/MOVE"

pytestmark = pytest.mark.usefixtures("u1")

CONFIRM = {"confirmation": "DELETE"}


@pytest.fixture(scope="module", autouse=True)
def cd_setup(e2e, u1, st):
    u1.require_dav()
    st.cal = u1.mk_calendar(e2e.uniq("caldav"))
    st.col = f"{u1.home_cal}{st.cal['path']}/"


def mkcal_body(name):
    return ('<?xml version="1.0" encoding="utf-8"?>\n<c:mkcalendar xmlns:d="DAV:" '
            'xmlns:c="urn:ietf:params:xml:ns:caldav"><d:set><d:prop>'
            f"<d:displayname>{name}</d:displayname></d:prop></d:set></c:mkcalendar>")


def _find_cal_by_path(u1, path):
    """→ (count, uuid, name, response) of the REST calendars at PATH."""
    r = u1.api("GET", "/calendars/")
    body = r.json()
    cals = [c for c in body.get("calendars") or [] if c.get("path") == path] if isinstance(body, dict) else []
    first = cals[0] if cals else {}
    return len(cals), first.get("uuid", ""), first.get("name", ""), r


def _cleanup_path(e2e, u1, path):
    """Delete every REST calendar at PATH later."""
    _, _, _, r = _find_cal_by_path(u1, path)
    body = r.json()
    for c in (body.get("calendars") or [] if isinstance(body, dict) else []):
        if c.get("path") == path:
            e2e.defer(f"delete DAV-created calendar {path}", e2e.cleanup_api, u1, "DELETE",
                      f"/calendars/{c['uuid']}", json=CONFIRM)


def _query(u1, st, comp_filter):
    return u1.dav("REPORT", st.col, depth=1, xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">\n'
        "<d:prop><d:getetag/><c:calendar-data/></d:prop>\n"
        f'<c:filter><c:comp-filter name="VCALENDAR">{comp_filter}</c:comp-filter></c:filter></c:calendar-query>'))


def test_mkcalendar(t, e2e, u1, st):
    """MKCALENDAR creates a calendar with its displayname"""
    st.mk_slug = e2e.uniq("mkcal")
    r = u1.dav("MKCALENDAR", f"{u1.home_cal}{st.mk_slug}/", xml=mkcal_body("Made by MKCALENDAR"))
    if not t.status(r, 201):
        return
    _cleanup_path(e2e, u1, st.mk_slug)
    n, _, name, _ = _find_cal_by_path(u1, st.mk_slug)
    if not t.eq(n, 1, "exactly one calendar at the new path"):
        return
    t.eq(name, "Made by MKCALENDAR", "the MKCALENDAR displayname must be kept")
    r = u1.dav("PROPFIND", f"{u1.home_cal}{st.mk_slug}/", depth=0, xml=PROPFIND_COLLECTION)
    if not t.status(r, 207):
        return
    t.xpath(r, lp("resourcetype", "calendar"))


def test_mkcol(t, e2e, u1):
    """extended MKCOL creates a calendar"""
    slug = e2e.uniq("mkcol")
    r = u1.dav("MKCOL", f"{u1.home_cal}{slug}/", xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<d:mkcol xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:set><d:prop><d:resourcetype>'
        "<d:collection/><c:calendar/></d:resourcetype><d:displayname>Made by MKCOL</d:displayname>"
        "</d:prop></d:set></d:mkcol>"))
    if not t.status(r, 201):
        return
    _cleanup_path(e2e, u1, slug)
    n, _, name, _ = _find_cal_by_path(u1, slug)
    t.eq(n, 1, "exactly one calendar at the new path")
    t.eq(name, "Made by MKCOL")


def test_mkcol_existing(t, e2e, u1, st):
    """MKCALENDAR / MKCOL on an existing collection is refused (405)"""
    t.need(st, "mk_slug")
    col = f"{u1.home_cal}{st.mk_slug}/"
    r = u1.dav("MKCALENDAR", col, xml=mkcal_body("Duplicate"))
    t.status(r, 405, 403, 409, msg="MKCALENDAR on an existing calendar (RFC 4791 5.3.1: resource-must-be-null)")
    s1 = r.status
    r = u1.dav("MKCOL", col)
    t.status(r, 405, msg="MKCOL on an existing collection (RFC 4918 9.3.1)")
    n, _, _, r = _find_cal_by_path(u1, st.mk_slug)
    t.true(n == 1, f"the refused requests must not create duplicate calendars at '{st.mk_slug}' "
                   f"(MKCALENDAR answered {s1}; {n} calendars there now)")
    # Never leave duplicates behind: delete the extra ones right away (the
    # original stays registered for cleanup).
    if n > 1:
        for c in r.jdict().get("calendars") or []:
            if c.get("path") == st.mk_slug:
                e2e.defer(f"delete duplicate calendar at {st.mk_slug}", e2e.cleanup_api, u1, "DELETE",
                          f"/calendars/{c['uuid']}", json=CONFIRM)


def _cal_uuids(t, u1):
    """UUIDs of every calendar U1 lists (None when the list fails)."""
    r = u1.api("GET", "/calendars/")
    if not t.status(r, 200, msg="listing U1's calendars"):
        return None
    return {c.get("uuid") for c in r.jdict().get("calendars") or []}


def test_mkcol_outside(t, e2e, u1):
    """MKCALENDAR outside the own calendar home is refused"""
    before = _cal_uuids(t, u1)
    if before is None:
        return
    nested, loose = e2e.uniq("nested"), e2e.uniq("loose")
    r = u1.dav("MKCALENDAR", f"{u1.home_cal}{nested}/deeper/", xml=mkcal_body("nested"))
    t.status_class(r, "4xx", msg="a calendar inside a calendar")
    r = u1.dav("MKCALENDAR", f"/dav/{u1.username}/{loose}/", xml=mkcal_body("loose"))
    t.status_class(r, "4xx", msg="a calendar outside the calendar home")
    for path in (f"{u1.home_cal}{nested}/", f"{u1.home_cal}{nested}/deeper/", f"/dav/{u1.username}/{loose}/"):
        r = u1.dav("PROPFIND", path, depth=0, xml=PROPFIND_LIST)
        # Outside the homes the server answers an empty 207 (FINDINGS.md, observations).
        t.true(r.status == 404 or (r.status == 207 and xcount(r, lp("response")) == 0),
               f"nothing may exist at {path} (404, or a 207 without any response)", r)
    r = u1.api("GET", "/calendars/")
    created = [c for c in r.jdict().get("calendars") or [] if c.get("uuid") not in before]
    for c in created:  # never leave them behind
        e2e.defer(f"delete calendar created by a refused MKCALENDAR ({c.get('path')})", e2e.cleanup_api, u1,
                  "DELETE", f"/calendars/{c.get('uuid')}", json=CONFIRM)
    t.eq([c.get("path") for c in created], [], "the refused MKCALENDARs must not have created any calendar")


# The calendar creation body iOS / macOS Calendar sends (shortened VTIMEZONE).
APPLE_MKCALENDAR = (
    '<?xml version="1.0" encoding="UTF-8"?>\n'
    '<B:mkcalendar xmlns:B="urn:ietf:params:xml:ns:caldav"><A:set xmlns:A="DAV:"><A:prop>'
    "<B:calendar-timezone>BEGIN:VCALENDAR&#13;\nVERSION:2.0&#13;\nPRODID:-//Apple Inc.//iPhone OS 17.0//EN&#13;\n"
    "BEGIN:VTIMEZONE&#13;\nTZID:Europe/Berlin&#13;\nBEGIN:STANDARD&#13;\nDTSTART:19701025T030000&#13;\n"
    "TZOFFSETFROM:+0200&#13;\nTZOFFSETTO:+0100&#13;\nEND:STANDARD&#13;\nEND:VTIMEZONE&#13;\nEND:VCALENDAR&#13;\n"
    "</B:calendar-timezone>"
    '<D:calendar-order xmlns:D="http://apple.com/ns/ical/">3</D:calendar-order>'
    '<B:supported-calendar-component-set><B:comp name="VEVENT"/></B:supported-calendar-component-set>'
    '<D:calendar-color xmlns:D="http://apple.com/ns/ical/" symbolic-color="custom">#1BADF8FF</D:calendar-color>'
    "<A:displayname>Apple calendar</A:displayname>"
    "<B:calendar-description>made on an iPhone</B:calendar-description>"
    "<B:schedule-calendar-transp><B:opaque/></B:schedule-calendar-transp>"
    "</A:prop></A:set></B:mkcalendar>")


def _rest_cal_at(u1, path):
    """The REST calendar object at PATH ({} when there is none)."""
    body = u1.api("GET", "/calendars/").jdict()
    return next((c for c in body.get("calendars") or [] if c.get("path") == path), {})


def test_mkcalendar_properties(t, e2e, u1):
    """MKCALENDAR applies colour, description, timezone and component set (Apple payload)"""
    slug = e2e.uniq("applecal")
    r = u1.dav("MKCALENDAR", f"{u1.home_cal}{slug}/", xml=APPLE_MKCALENDAR)
    if not t.status(r, 201):
        return
    _cleanup_path(e2e, u1, slug)
    cal = _rest_cal_at(u1, slug)
    t.eq(cal.get("name"), "Apple calendar", "displayname")
    t.eq(cal.get("description"), "made on an iPhone", "calendar-description must not be dropped")
    t.eq((cal.get("color") or "").upper(), "#1BADF8", "calendar-color (alpha removed) must not be dropped")
    t.eq(cal.get("timezone"), "Europe/Berlin", "the VTIMEZONE's TZID must not be dropped")
    r = u1.dav("PROPFIND", f"{u1.home_cal}{slug}/", depth=1, xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">'
        "<d:prop><c:supported-calendar-component-set/></d:prop></d:propfind>"))
    if not t.status(r, 207):
        return
    comps = lp("supported-calendar-component-set", "comp")
    t.xpath(r, f"{comps}[@name='VEVENT']")
    t.eq(xcount(r, f"{comps}[@name='VTODO']"), 0, "the requested VEVENT-only component set is reported")


def test_mkcalendar_refused_property(t, e2e, u1):
    """MKCALENDAR with a property the server cannot set creates nothing (207, 403 + 424)"""
    slug = e2e.uniq("refusedcal")
    r = u1.dav("MKCALENDAR", f"{u1.home_cal}{slug}/", xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n<c:mkcalendar xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">'
        '<d:set><d:prop><d:displayname>never created</d:displayname>'
        '<c:supported-calendar-component-set><c:comp name="VJOURNAL"/></c:supported-calendar-component-set>'
        "</d:prop></d:set></c:mkcalendar>"))
    n, _, _, _ = _find_cal_by_path(u1, slug)
    if n:
        _cleanup_path(e2e, u1, slug)
    t.status(r, 207, msg="RFC 4791 5.3.1.1: a failed DAV:set instruction is a 207 and nothing is created")
    t.xpath(r, f"{lp('propstat')}[{el('status')}[contains(., '403')]]/{el('prop')}/{el('supported-calendar-component-set')}")
    t.xpath(r, f"{lp('propstat')}[{el('status')}[contains(., '424')]]/{el('prop')}/{el('displayname')}")
    t.eq(n, 0, "the refused MKCALENDAR must not create a calendar")


def test_put_create(t, e2e, u1, st):
    """PUT with If-None-Match: * creates (201 + ETag)"""
    st.uid = f"{e2e.uniq('ev1')}@e2e"
    st.obj = f"{st.col}{e2e.uniq('ev1')}.ics"
    r = u1.dav("PUT", st.obj, ics=ics_event(st.uid, "CalDAV PUT event", "20300601T100000Z", "20300601T110000Z",
                                             "LOCATION:Room 7"), headers={"If-None-Match": "*"})
    if not t.status(r, 201):
        return
    st.etag = r.hdr("ETag")
    if not re.fullmatch(r'".+"', st.etag):
        t.fail(f"PUT must return a strong, quoted ETag, got '{st.etag}'")


def test_put_create_again(t, u1, st):
    """PUT with If-None-Match: * on an existing object is 412"""
    t.need(st, "obj")
    r = u1.dav("PUT", st.obj, ics=ics_event(st.uid, "Clobber", "20300601T100000Z", "20300601T110000Z"),
               headers={"If-None-Match": "*"})
    t.status(r, 412)


def test_get(t, u1, st):
    """GET returns the object with the same ETag"""
    t.need(st, "obj")
    r = u1.dav("GET", st.obj)
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "text/calendar")
    t.header_eq(r, "ETag", getattr(st, "etag", ""), "GET must serve the ETag PUT returned")
    t.contains(r, f"UID:{st.uid}")
    t.contains(r, "SUMMARY:CalDAV PUT event")
    t.contains(r, "LOCATION:Room 7")
    t.match(r, "BEGIN:VCALENDAR\r\n", "lines must end with CRLF")


def test_head(t, u1, st):
    """HEAD returns the headers of GET"""
    t.need(st, "obj")
    r = u1.dav("HEAD", st.obj)
    if not t.status(r, 200):
        return
    t.header_eq(r, "ETag", getattr(st, "etag", ""))
    t.header(r, "Content-Type", "text/calendar")
    t.header(r, "Content-Length")


def test_put_if_match(t, u1, st):
    """PUT with matching If-Match updates and changes the ETag"""
    t.need(st, "etag")
    r = u1.dav("PUT", st.obj, ics=ics_event(st.uid, "CalDAV PUT event v2", "20300601T100000Z", "20300601T113000Z"),
               headers={"If-Match": st.etag})
    if not t.status(r, 200, 201, 204):
        return
    new = r.hdr("ETag")
    if new:
        t.ne(new, st.etag, "the ETag must change when the content changes")
    r = u1.dav("GET", st.obj)
    t.contains(r, "SUMMARY:CalDAV PUT event v2")
    t.ne(r.hdr("ETag"), st.etag, "GET must serve the new ETag")
    st.old_etag = st.etag
    st.etag = r.hdr("ETag")


def test_put_stale(t, u1, st):
    """PUT with a stale If-Match is 412 and changes nothing"""
    t.need(st, "old_etag")
    r = u1.dav("PUT", st.obj, ics=ics_event(st.uid, "stale write", "20300601T100000Z", "20300601T110000Z"),
               headers={"If-Match": st.old_etag})
    if not t.status(r, 412):
        return
    r = u1.dav("GET", st.obj)
    t.contains(r, "SUMMARY:CalDAV PUT event v2", "the rejected write must not change the object")
    t.not_contains(r, "stale write")


def test_put_if_match_missing(t, e2e, u1, st):
    """PUT with If-Match on a missing object is 412"""
    ghost = e2e.uniq("ghost")
    r = u1.dav("PUT", f"{st.col}{ghost}.ics", ics=ics_event(ghost, "ghost", "20300601T100000Z", "20300601T110000Z"),
               headers={"If-Match": '"anything"'})
    t.status(r, 412)
    t.status(u1.dav("GET", f"{st.col}{ghost}.ics"), 404, msg="nothing may have been created")


def test_uid_conflict(t, e2e, u1, st):
    """same UID at a second path is a 409 (no-uid-conflict)"""
    t.need(st, "uid")
    other = f"{st.col}{e2e.uniq('other-path')}.ics"
    r = u1.dav("PUT", other, ics=ics_event(st.uid, "same uid elsewhere", "20300601T100000Z", "20300601T110000Z"))
    if not t.status(r, 409):
        return
    t.contains(r, "no-uid-conflict", "RFC 4791 5.3.2.1: precondition element CALDAV:no-uid-conflict")
    t.status(u1.dav("GET", other), 404)


def test_put_invalid(t, e2e, u1, st):
    """PUT rejects wrong media type and malformed iCalendar"""
    bad1 = e2e.uniq("bad1")
    r = u1.dav("PUT", f"{st.col}{bad1}.ics", data=ics_event(bad1, "x", "20300601T100000Z", "20300601T110000Z"),
               ctype="application/json")
    t.status_class(r, "4xx", msg="non-iCalendar media type")
    r = u1.dav("PUT", f"{st.col}{e2e.uniq('bad2')}.ics", data="this is not iCalendar", ctype="text/calendar")
    t.status(r, 400, msg="malformed iCalendar")
    bad4 = e2e.uniq("bad4")
    # Malformed on purpose: the first VEVENT ends in "END:VEVENT\r\r\n" (a stray
    # CR). The clean two-UID case is test_put_two_uids.
    first = ics_event(f"{bad4}-a", "a", "20300601T100000Z", "20300601T110000Z")[:-len("END:VCALENDAR\r\n")]
    second = "".join(ics_event(f"{bad4}-b", "b", "20300601T100000Z", "20300601T110000Z")
                     .splitlines(keepends=True)[3:])
    two = first[:-1] + "\r\n" + second
    r = u1.dav("PUT", f"{st.col}{bad4}.ics", ics=two)
    t.status_class(r, "4xx", msg="malformed object with two UIDs")


def test_put_two_uids(t, e2e, u1, st):
    """PUT of a well-formed object with two different UIDs is a 4xx, nothing stored"""
    uid = e2e.uniq("twouid")
    body = ics_calendar(ics_vevent(f"{uid}-a", "a", "20300601T100000Z", "20300601T110000Z"),
                        ics_vevent(f"{uid}-b", "b", "20300601T100000Z", "20300601T110000Z"))
    r = u1.dav("PUT", f"{st.col}{uid}.ics", ics=body)
    if t.status_class(r, "4xx", msg="two different UIDs in one object (RFC 4791 4.1), fixed in #194"):
        t.contains(r, "valid-calendar-object-resource", "names the failed precondition")
    t.status(u1.dav("GET", f"{st.col}{uid}.ics"), 404, msg="nothing was stored")


def test_put_empty(t, e2e, u1, st):
    """PUT of a VCALENDAR without components is a 4xx, nothing stored"""
    obj = f"{st.col}{e2e.uniq('empty')}.ics"
    r = u1.dav("PUT", obj, ics="BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//x//EN\r\nEND:VCALENDAR\r\n")
    if t.status_class(r, "4xx", msg="a VCALENDAR without any component is invalid client input, "
                      "not a server error (was a 500 before F16 was fixed)"):
        t.contains(r, "valid-calendar-object-resource", "RFC 4791 4.1: names the failed precondition")
    t.status(u1.dav("GET", obj), 404, msg="nothing was stored")


def test_vtodo(t, e2e, u1, st):
    """VTODO objects are stored and served"""
    uid, obj = f"{e2e.uniq('todo')}@e2e", f"{st.col}{e2e.uniq('todo')}.ics"
    if not t.status(u1.dav("PUT", obj, ics=ics_todo(uid, "Buy milk")), 201, 204):
        return
    r = u1.dav("GET", obj)
    if not t.status(r, 200):
        return
    t.contains(r, "BEGIN:VTODO")
    t.contains(r, "SUMMARY:Buy milk")
    st.todo_uid = uid


def test_override(t, e2e, u1, st):
    """recurrence overrides (RECURRENCE-ID) round-trip"""
    uid, obj = f"{e2e.uniq('series')}@e2e", f"{st.col}{e2e.uniq('series')}.ics"
    ics = ("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VEVENT\r\n"
           f"UID:{uid}\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300701T100000Z\r\nDTEND:20300701T110000Z\r\n"
           "RRULE:FREQ=DAILY;COUNT=3\r\nSUMMARY:DAV series\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\n"
           f"UID:{uid}\r\nDTSTAMP:20260101T000000Z\r\nRECURRENCE-ID:20300702T100000Z\r\n"
           "DTSTART:20300702T150000Z\r\nDTEND:20300702T160000Z\r\nSUMMARY:DAV series (moved)\r\n"
           "END:VEVENT\r\nEND:VCALENDAR\r\n")
    if not t.status(u1.dav("PUT", obj, ics=ics), 201, 204):
        return
    r = u1.dav("GET", obj)
    t.contains(r, "RECURRENCE-ID:20300702T100000Z", "the override must be stored with the master")
    t.contains(r, "RRULE:FREQ=DAILY;COUNT=3")
    r = u1.api("GET", f"/calendars/{st.cal['uuid']}/events?start=2030-06-30T00:00:00Z&end=2030-07-10T00:00:00Z"
                      "&expand=true")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([e for e in j["events"] if e["uid"] == uid]) == 3, "3 instances")
    t.jq(r, lambda j: (lambda e: e["summary"] == "DAV series (moved)" and e["start"] == "2030-07-02T15:00:00Z")(
        [e for e in j["events"] if e["uid"] == uid and e.get("recurrence_id") == "20300702T100000Z"][0]),
        "the REST expansion must apply the override")


def test_query_timerange(t, e2e, u1, st):
    """calendar-query with a time-range returns only matches"""
    for label, ts in (("in1", "20310510T090000Z"), ("in2", "20310515T140000Z"), ("out", "20320101T090000Z")):
        name = e2e.uniq(f"q-{label}")
        if not t.status(u1.dav("PUT", f"{st.col}{name}.ics", ics=ics_event(name, f"Query {label}", ts, ts)), 201, 204):
            return
    r = _query(u1, st, '<c:comp-filter name="VEVENT"><c:time-range start="20310501T000000Z" '
                       'end="20310601T000000Z"/></c:comp-filter>')
    if not t.status(r, 207):
        return
    t.contains(r, e2e.uniq("q-in1"), "in-range event 1")
    t.contains(r, e2e.uniq("q-in2"), "in-range event 2")
    t.not_contains(r, e2e.uniq("q-out"), "out-of-range event must not match")
    if getattr(st, "uid", ""):  # the 2030 object of test_put_create (absent when run alone)
        t.not_contains(r, st.uid, "the 2030 event must not match")
    t.xpath(r, lp("response", "propstat", "prop", "calendar-data"), "calendar-data must be returned when requested")


def test_query_vtodo(t, u1, st):
    """calendar-query by component (VTODO)"""
    t.need(st, "todo_uid")
    r = _query(u1, st, '<c:comp-filter name="VTODO"/>')
    if not t.status(r, 207):
        return
    t.contains(r, st.todo_uid)
    if getattr(st, "uid", ""):
        t.not_contains(r, st.uid, "VEVENTs must not match a VTODO filter")
    t.not_contains(r, "BEGIN:VEVENT", "VEVENTs must not match a VTODO filter")


def test_multiget(t, e2e, u1, st):
    """calendar-multiget returns requested objects, 404 for missing"""
    t.need(st, "obj")
    todo, missing = f"{st.col}{e2e.uniq('todo')}.ics", f"{st.col}{e2e.uniq('missing')}.ics"
    r = u1.dav("REPORT", st.col, depth=1, xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<c:calendar-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">\n'
        "<d:prop><d:getetag/><c:calendar-data/></d:prop>\n"
        f"<d:href>{st.obj}</d:href><d:href>{todo}</d:href><d:href>{missing}</d:href></c:calendar-multiget>"))
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response')}[{el('href')}='{st.obj}']//{el('getetag')}", "the event is returned with its ETag")
    t.xpath(r, f"{lp('response')}[{el('href')}='{todo}']//{el('calendar-data')}[contains(.,'VTODO')]",
            "the todo is returned with calendar-data")
    t.xpath(r, f"{lp('response')}[{el('href')}='{missing}']//{el('status')}[contains(.,'404')]",
            "a missing href is reported 404 inside the multistatus")
    tag = xstr(r, f"{lp('response')}[{el('href')}='{st.obj}']//{el('getetag')}")
    etag = getattr(st, "etag", "")
    if tag and tag != etag:
        t.fail(f"multiget ETag {tag} differs from GET ETag {etag}")


def test_list_objects(t, u1, st):
    """Depth 1 PROPFIND lists the objects with ETags"""
    t.need(st, "obj")
    r = u1.dav("PROPFIND", st.col, depth=1, xml=PROPFIND_LIST)
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response')}[{el('href')}='{st.obj}']//{el('getetag')}", "the object is listed with its ETag")
    t.xpath(r, f"count({lp('response')}) >= 5", "collection + objects")


def test_rest_crosscheck(t, e2e, u1, st):
    """objects PUT over DAV appear in REST, and vice versa"""
    t.need(st, "uid")
    r = u1.api("GET", f"/calendars/{st.cal['uuid']}/events?start=2030-01-01T00:00:00Z&end=2030-12-31T00:00:00Z"
                      "&expand=false")
    if not t.jq(r, lambda j: len([e for e in j["events"] if e["uid"] == st.uid
                                  and e["summary"] == "CalDAV PUT event v2"]) == 1,
                "an object PUT over DAV must appear in the REST event list"):
        return
    try:
        ev = u1.mk_event(st.cal["uuid"], f"Made over REST {e2e.run_id}", "2030-08-01T10:00:00Z",
                         "2030-08-01T11:00:00Z")
    except SetupFailed as e:
        return t.fail("REST create failed", e.resp)
    ev_id, uid = ev["id"], ev["uid"]
    r = u1.dav("PROPFIND", st.col, depth=1, xml=PROPFIND_LIST)
    href = xstr(r, f"({lp('response', 'href')})[contains(.,'{ev_id}') or contains(.,'{uid.split('@')[0]}')]")
    if not href:
        r = _query(u1, st, '<c:comp-filter name="VEVENT"><c:time-range start="20300801T000000Z" '
                           'end="20300802T000000Z"/></c:comp-filter>')
        t.contains(r, f"Made over REST {e2e.run_id}", "a REST-created event must be visible over CalDAV")
        return
    r = u1.dav("GET", href)
    if not t.status(r, 200):
        return
    t.contains(r, f"SUMMARY:Made over REST {e2e.run_id}")
    t.contains(r, f"UID:{uid}")


def test_options_object(t, u1, st):
    """OPTIONS on an object lists GET/PUT/DELETE"""
    t.need(st, "obj")
    r = u1.dav("OPTIONS", st.obj)
    if not t.status(r, 200, 204):
        return
    t.header(r, "Allow", "GET")
    t.header(r, "Allow", "PUT")
    t.header(r, "Allow", "DELETE")


def test_copy_move(t, e2e, u1, st):
    """COPY / MOVE are answered cleanly (not supported: 501)"""
    t.need(st, "obj")
    r = u1.dav("COPY", st.obj, headers={"Destination": f"{e2e.base}{st.col}{e2e.uniq('copy')}.ics"})
    if not t.status(r, 201, 204, 501, 403, 405, msg="COPY"):
        return
    if r.status == 501:
        out.info("      note: COPY is not implemented by the server (501)")
    r = u1.dav("MOVE", st.obj, headers={"Destination": f"{e2e.base}{st.col}{e2e.uniq('moved')}.ics"})
    if not t.status(r, 201, 204, 501, 403, 405, msg="MOVE"):
        return
    if r.status == 501 or str(r.status).startswith("4"):
        t.status(u1.dav("GET", st.obj), 200, msg="a refused MOVE must leave the source in place")


def test_delete_stale(t, u1, st):
    """DELETE with a stale If-Match is 412"""
    t.need(st, "obj")
    if not t.status(u1.dav("DELETE", st.obj, headers={"If-Match": '"stale-etag"'}), 412):
        return
    t.status(u1.dav("GET", st.obj), 200, msg="the object must survive a failed conditional DELETE")


def test_delete(t, u1, st):
    """DELETE removes the object; repeated DELETE is 404"""
    t.need(st, "obj")
    if not t.status(u1.dav("DELETE", st.obj, headers={"If-Match": getattr(st, "etag", "")}), 204, 200):
        return
    t.status(u1.dav("GET", st.obj), 404)
    t.status(u1.dav("DELETE", st.obj), 404)
    r = u1.api("GET", f"/calendars/{st.cal['uuid']}/events?start=2030-01-01T00:00:00Z&end=2030-12-31T00:00:00Z"
                      "&expand=false")
    t.jq(r, lambda j: len([e for e in j["events"] or [] if e["uid"] == st.uid]) == 0,
         "a DAV delete must remove the REST event too")


def test_proppatch(t, u1, st):
    """PROPPATCH renames and recolours the collection"""
    r = u1.dav("PROPPATCH", st.col, xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<d:propertyupdate xmlns:d="DAV:" xmlns:a="http://apple.com/ns/ical/"><d:set><d:prop>'
        "<d:displayname>Renamed over DAV</d:displayname><a:calendar-color>#FF8800FF</a:calendar-color>"
        "</d:prop></d:set></d:propertyupdate>"))
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response', 'propstat')}[contains({el('status')},'200')]//{el('displayname')}",
            "displayname must be acknowledged with 200")
    r = u1.api("GET", f"/calendars/{st.cal['uuid']}")
    t.json(r, "name", "Renamed over DAV")
    t.json(r, "color", "#FF8800", "Apple's #RRGGBBAA colour is stored as #RRGGBB")


def test_color_readback(t, u1, st):
    """the colour set by PROPPATCH is readable over PROPFIND"""
    r = u1.dav("PROPFIND", st.col, depth=0, xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<d:propfind xmlns:d="DAV:" xmlns:a="http://apple.com/ns/ical/"><d:prop><a:calendar-color/></d:prop>'
        "</d:propfind>"))
    if not t.status(r, 207):
        return
    # Fixed in F07: calendar-color used to be accepted by PROPPATCH but never returned.
    t.xpath(r, f"{lp('response', 'propstat')}[contains({el('status')},'200')]//{el('calendar-color')}"
               "[contains(.,'FF8800') or contains(.,'ff8800')]",
            "a client must be able to read the colour it (or the web UI) set")


def test_proppatch_atomic(t, u1, st):
    """PROPPATCH is atomic: one bad property fails them all (RFC 4918 9.2)"""
    r = u1.dav("PROPPATCH", st.col, xml=(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<d:propertyupdate xmlns:d="DAV:" xmlns:x="urn:example:e2e"><d:set><d:prop>'
        "<d:displayname>Should not stick</d:displayname><x:unsupported>1</x:unsupported>"
        "</d:prop></d:set></d:propertyupdate>"))
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response', 'propstat')}[contains({el('status')},'424')]//{el('displayname')}",
            "when one property fails, the others must be reported 424 Failed Dependency")
    t.xpath(r, f"{lp('response', 'propstat')}[contains({el('status')},'403')]//{el('unsupported')}",
            "the property the server cannot set must be reported 403")
    r = u1.api("GET", f"/calendars/{st.cal['uuid']}")
    t.json(r, "name", "Renamed over DAV", "a PROPPATCH that partly failed must not apply anything")


def test_delete_collection(t, e2e, u1):
    """DELETE on the collection removes the calendar"""
    slug = e2e.uniq("doomed")
    if not t.status(u1.dav("MKCALENDAR", f"{u1.home_cal}{slug}/", xml=mkcal_body("Doomed")), 201):
        return
    _cleanup_path(e2e, u1, slug)
    u1.dav("PUT", f"{u1.home_cal}{slug}/x.ics",
           ics=ics_event(e2e.uniq("doomed-ev"), "doomed", "20300101T100000Z", "20300101T110000Z"))
    if not t.status(u1.dav("DELETE", f"{u1.home_cal}{slug}/"), 204, 200):
        return
    n, _, _, _ = _find_cal_by_path(u1, slug)
    t.eq(n, 0, "the calendar must be gone from REST")
    t.status(u1.dav("PROPFIND", f"{u1.home_cal}{slug}/", depth=0, xml=PROPFIND_LIST), 404)
