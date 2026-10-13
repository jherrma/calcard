"""Calendars over REST: CRUD, validation, ownership isolation, export, the
"cannot delete your last calendar" guard."""

import re

import pytest

from harness.data import PROPFIND_LIST, ics_event, sync_body
from harness.session import ZERO_UUID
from harness.xml import lp, xcount, xml_text, xstr

DESCRIPTION = "calendar CRUD, validation, isolation, export, last-calendar guard"

pytestmark = pytest.mark.usefixtures("u1", "u2")


def test_create(t, e2e, u1, st):
    """create returns the calendar (201)"""
    st.c1_name = e2e.uniq("Work Calendar")
    r = u1.api("POST", "/calendars/", json={"name": st.c1_name, "description": "e2e calendar", "color": "#336699",
                                            "timezone": "Europe/Berlin"})
    if not t.status(r, 201):
        return
    body = r.json()
    st.c1_uuid, st.c1_id, st.c1_path = body.get("uuid"), body.get("id"), body.get("path")
    e2e.defer(f"delete calendar {st.c1_name}", e2e.cleanup_api, u1, "DELETE", f"/calendars/{st.c1_uuid}",
              json={"confirmation": "DELETE"})
    t.json(r, "name", st.c1_name)
    t.json(r, "color", "#336699")
    t.json(r, "timezone", "Europe/Berlin")
    t.json(r, "description", "e2e calendar")
    t.json(r, "path", f"{st.c1_uuid}.ics", "server-created calendars live at <uuid>.ics in DAV")
    t.jq(r, lambda j: j["id"] > 0 and re.fullmatch(r"[0-9a-f-]{36}", j["uuid"]),
         'expected JSON condition to hold: .id > 0 and (.uuid | test("^[0-9a-f-]{36}$"))')
    t.jq(r, lambda j: "VEVENT" in j["supported_components"],
         'expected JSON condition to hold: .supported_components | test("VEVENT")')
    t.json(r, "subscribed", False)


def test_create_invalid(t, e2e, u1):
    """create validates name and colour"""
    t.status(u1.api("POST", "/calendars/", json='{"color":"#336699"}'), 400, msg="a calendar needs a name")
    bad = e2e.uniq("badcolor")
    t.status(u1.api("POST", "/calendars/", json={"name": bad, "color": "blue"}), 400, msg="colour must be a hex value")
    t.status(u1.api("POST", "/calendars/", data='{"name":', ctype="application/json"), 400, msg="malformed JSON")
    # Clean up in case the server accepted any of them.
    r = u1.api("GET", "/calendars/")
    for c in r.jdict().get("calendars") or []:
        if c.get("name") == bad:
            u1.api("DELETE", f"/calendars/{c['uuid']}", json={"confirmation": "DELETE"})


def test_list(t, u1, st):
    """list contains the calendar as owned"""
    t.need(st, "c1_uuid")
    r = u1.api("GET", "/calendars/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([c for c in j["calendars"] if c["uuid"] == st.c1_uuid]) == 1,
         "expected JSON condition to hold: the calendar is listed once")
    t.jq(r, lambda j: all(c["shared"] is False and c["permission"] == "owner"
                          for c in j["calendars"] if c["uuid"] == st.c1_uuid)
         and any(c["uuid"] == st.c1_uuid for c in j["calendars"]),
         "an owned calendar is shared=false, permission=owner")
    t.jq(r, lambda j: len([c for c in j["calendars"] if c["name"] == "Personal"]) >= 1,
         "registration seeds a Personal calendar")


def test_get(t, u1, st):
    """get by UUID; unknown UUID and numeric id are 404"""
    t.need(st, "c1_uuid")
    r = u1.api("GET", f"/calendars/{st.c1_uuid}")
    if not t.status(r, 200):
        return
    t.json(r, "uuid", st.c1_uuid)
    t.status(u1.api("GET", f"/calendars/{ZERO_UUID}"), 404)
    t.status(u1.api("GET", f"/calendars/{st.c1_id}"), 404, msg="calendars are addressed by UUID, not numeric id")


def test_patch(t, u1, st):
    """PATCH renames / recolours / re-describes"""
    t.need(st, "c1_uuid")
    st.c1_name = f"{st.c1_name} (renamed)"
    r = u1.api("PATCH", f"/calendars/{st.c1_uuid}", json={"name": st.c1_name, "color": "#00aa55", "description": "changed"})
    if not t.status(r, 200):
        return
    t.json(r, "name", st.c1_name)
    t.json(r, "color", "#00aa55")
    t.json(r, "description", "changed")
    r = u1.api("GET", f"/calendars/{st.c1_uuid}")
    t.json(r, "name", st.c1_name, "rename must persist")
    t.json(r, "timezone", "Europe/Berlin", "fields not in the PATCH stay untouched")


def test_patch_invalid(t, u1, st):
    """PATCH validates the colour"""
    t.need(st, "c1_uuid")
    t.status(u1.api("PATCH", f"/calendars/{st.c1_uuid}", json='{"color":"not-a-colour"}'), 400)
    t.status(u1.api("PATCH", f"/calendars/{st.c1_uuid}", json='{"name":""}'), 400, msg="an empty name is not allowed")
    t.json(u1.api("GET", f"/calendars/{st.c1_uuid}"), "color", "#00aa55", "a rejected PATCH must not change anything")


def test_isolation(t, u1, u2, st):
    """another user cannot read, change or delete it"""
    t.need(st, "c1_uuid")
    c = st.c1_uuid
    t.status_class(u2.api("GET", f"/calendars/{c}"), "4xx", msg="U2 reading U1's calendar")
    t.status_class(u2.api("PATCH", f"/calendars/{c}", json='{"name":"hijacked"}'), "4xx", msg="U2 renaming U1's calendar")
    t.status_class(u2.api("DELETE", f"/calendars/{c}", json='{"confirmation":"DELETE"}'), "4xx",
                   msg="U2 deleting U1's calendar")
    t.status_class(u2.api("GET", f"/calendars/{c}/export"), "4xx", msg="U2 exporting U1's calendar")
    t.not_contains(u2.api("GET", "/calendars/"), c, "U1's calendar must not appear in U2's list")
    t.json(u1.api("GET", f"/calendars/{c}"), "name", st.c1_name, "U1's calendar must be untouched")


def test_notfound_status(t, e2e, u1, u2, st):
    """unknown or foreign calendar is a 404 on every verb"""
    t.need(st, "c1_uuid")
    # GET answers 404 (the integration tests call 404 deliberate: it does not
    # leak existence). The write verbs agree (F12, fixed).
    t.status(u1.api("PATCH", f"/calendars/{ZERO_UUID}", json='{"name":"x"}'), 404, msg="PATCH unknown calendar")
    t.status(u1.api("DELETE", f"/calendars/{ZERO_UUID}", json='{"confirmation":"DELETE"}'), 404,
             msg="DELETE unknown calendar")
    t.status(u2.api("PATCH", f"/calendars/{st.c1_uuid}", json='{"name":"x"}'), 404, msg="PATCH another user's calendar")
    r = u2.api("POST", f"/calendars/{st.c1_uuid}/import",
               data=ics_event(e2e.uniq("nf"), "x", "20300101T100000Z", "20300101T110000Z"), ctype="text/calendar")
    t.status(r, 404, msg="import into another user's calendar")
    t.json(u1.api("GET", f"/calendars/{st.c1_uuid}"), "name", st.c1_name, "U1's calendar must be untouched")


def test_notfound_before_body(t, u1, u2, st):
    """an unknown or foreign calendar is a 404 whatever the request body says"""
    t.need(st, "c1_uuid")
    # The calendar is resolved before the body is validated (F28), so a bad
    # body cannot turn the 404 into a 400, and unknown and foreign stay alike.
    cases = [
        ("DELETE", "", {"json": '{"confirmation":"yes"}'}, "DELETE with a wrong confirmation"),
        ("PATCH", "", {"data": '{"name":', "ctype": "application/json"}, "PATCH with malformed JSON"),
        ("POST", "/import", {"json": "{}"}, "import without data"),
    ]
    for method, suffix, kw, what in cases:
        unknown = u1.api(method, f"/calendars/{ZERO_UUID}{suffix}", **kw)
        foreign = u2.api(method, f"/calendars/{st.c1_uuid}{suffix}", **kw)
        t.status(unknown, 404, msg=f"{what}, unknown calendar")
        t.status(foreign, 404, msg=f"{what}, another user's calendar")
        t.eq(foreign.text, unknown.text, f"{what}: unknown and foreign answer the same body")
    # On an owned calendar the same bodies are still rejected as bad requests.
    t.status(u1.api("DELETE", f"/calendars/{st.c1_uuid}", json='{"confirmation":"yes"}'), 400,
             msg="a wrong confirmation on an owned calendar")
    t.status(u1.api("PATCH", f"/calendars/{st.c1_uuid}", data='{"name":', ctype="application/json"), 400,
             msg="malformed JSON on an owned calendar")
    t.json(u1.api("GET", f"/calendars/{st.c1_uuid}"), "name", st.c1_name, "U1's calendar must be untouched")


def test_export(t, e2e, u1, st):
    """export returns an iCalendar file with the events"""
    t.need(st, "c1_uuid")
    u1.mk_event(st.c1_uuid, f"Export me {e2e.run_id}", "2030-05-05T08:00:00Z", "2030-05-05T09:00:00Z")
    r = u1.api("GET", f"/calendars/{st.c1_uuid}/export")
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "text/calendar")
    t.header(r, "Content-Disposition", "attachment")
    t.match(r, "^BEGIN:VCALENDAR", "export must start with BEGIN:VCALENDAR")
    t.contains(r, f"SUMMARY:Export me {e2e.run_id}")
    t.contains(r, "END:VCALENDAR")


def test_delete(t, e2e, u1):
    """delete requires the DELETE confirmation"""
    cal = u1.mk_calendar(e2e.uniq("Delete me"))
    uuid = cal["uuid"]
    t.status(u1.api("DELETE", f"/calendars/{uuid}"), 400, msg="delete without confirmation must be refused")
    t.status(u1.api("DELETE", f"/calendars/{uuid}", json='{"confirmation":"yes"}'), 400,
             msg="delete with a wrong confirmation must be refused")
    if not t.status(u1.api("GET", f"/calendars/{uuid}"), 200, msg="the calendar must survive refused deletes"):
        return
    if not t.status(u1.api("DELETE", f"/calendars/{uuid}", json='{"confirmation":"DELETE"}'), 204):
        return
    t.status(u1.api("GET", f"/calendars/{uuid}"), 404)
    t.not_contains(u1.api("GET", "/calendars/"), uuid)


def test_delete_leaves_nothing(t, e2e, u1, u2):
    """a REST-deleted calendar is gone for good: old sync token 404s, re-created at its path or name it is empty"""
    if not u1.ensure_dav():
        return t.skip("no DAV app password for U1")
    name = e2e.uniq("Purge me")
    cal = u1.mk_calendar(name)
    uuid, col = cal["uuid"], f"{u1.home_cal}{cal['path']}/"
    for i in (1, 2):
        u1.mk_event(uuid, f"Purged {i} {e2e.run_id}", f"2030-07-0{i}T09:00:00Z", f"2030-07-0{i}T10:00:00Z")
    t.status(u1.api("POST", f"/calendars/{uuid}/shares", json={"user_identifier": u2.email, "permission": "read"}), 201)

    # A DAV client syncs the calendar and keeps the token and the hrefs.
    r = u1.dav("REPORT", col, depth=1, xml=sync_body())
    if not t.status(r, 207):
        return
    old_token = xml_text(r, "sync-token")
    hrefs = [xstr(r, f"({lp('response', 'href')})[{i}]") for i in (1, 2)]
    if not t.true(old_token and all(hrefs), "the initial sync lists both events and hands out a token"):
        return

    if not t.status(u1.api("DELETE", f"/calendars/{uuid}", json={"confirmation": "DELETE"}), 204):
        return
    t.status(u1.dav("REPORT", col, depth=1, xml=sync_body(old_token)), 404,
             msg="the client syncing the deleted calendar gets a 404")
    t.status(u1.dav("PROPFIND", col, depth=0, xml=PROPFIND_LIST), 404)
    t.status(u2.api("GET", f"/calendars/{uuid}"), 404, msg="the share went with the calendar")
    t.not_contains(u2.api("GET", "/calendars/"), uuid)

    # A new calendar at the very same DAV path starts empty.
    if not t.status(u1.dav("MKCALENDAR", col), 201):
        return
    _cleanup_calendars_at(e2e, u1, cal["path"])
    r = u1.dav("PROPFIND", col, depth=1, xml=PROPFIND_LIST)
    if t.status(r, 207):
        t.eq(xcount(r, lp("response")), 1, "the re-created collection lists itself and no member")
    for href in hrefs:
        t.status(u1.dav("GET", href), 404, msg="an event of the deleted calendar must not reappear at its old URL")
    r = u1.dav("REPORT", col, depth=1, xml=sync_body(old_token))
    if t.status(r, 403, msg="the old token means nothing to the new collection"):
        t.xpath(r, lp("error", "valid-sync-token"))

    # And so does a new calendar with the same name.
    again = u1.mk_calendar(name)
    r = u1.api("GET", f"/calendars/{again['uuid']}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z")
    if t.status(r, 200):
        t.jq(r, lambda j: not j["events"], "a calendar re-created with the same name holds no events")


def _cleanup_calendars_at(e2e, user, path):
    """Delete, at the end of the run, every REST calendar of USER at PATH."""
    for c in user.api("GET", "/calendars/").jdict().get("calendars") or []:
        if c.get("path") == path:
            e2e.defer(f"delete calendar at {path}", e2e.cleanup_api, user, "DELETE", f"/calendars/{c['uuid']}",
                      json={"confirmation": "DELETE"})


def test_last_guard(t, e2e):
    """the last remaining calendar cannot be deleted"""
    ucal = e2e.create_user("UCAL", "E2E Single Calendar")
    r = ucal.api("GET", "/calendars/")
    if not t.status(r, 200):
        return
    cals = r.jdict().get("calendars") or []
    if not t.eq(len(cals), 1, "a fresh account has exactly one calendar"):
        return
    uuid = cals[0].get("uuid")
    t.status_class(ucal.api("DELETE", f"/calendars/{uuid}", json='{"confirmation":"DELETE"}'), "4xx",
                   msg="deleting the only calendar must be refused")
    t.status(ucal.api("GET", f"/calendars/{uuid}"), 200, msg="the only calendar must survive")
