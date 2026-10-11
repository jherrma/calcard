"""Calendars over REST: CRUD, validation, ownership isolation, export, the
"cannot delete your last calendar" guard."""

import re

import pytest

from harness.checks import known
from harness.data import ics_event
from harness.session import ZERO_UUID

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
    for c in (r.json() or {}).get("calendars", []) if isinstance(r.json(), dict) else []:
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


@known("F12")
def test_notfound_status(t, e2e, u1, u2, st):
    """unknown or foreign calendar is a 404 on every verb"""
    t.need(st, "c1_uuid")
    # GET already answers 404 (the integration tests call 404 deliberate: it
    # does not leak existence). The write verbs must agree.
    # F12: calendar_handler.go maps every error of these verbs to 400.
    t.bug.status(u1.api("PATCH", f"/calendars/{ZERO_UUID}", json='{"name":"x"}'), 404, was=400,
                 msg="PATCH unknown calendar")
    t.bug.status(u1.api("DELETE", f"/calendars/{ZERO_UUID}", json='{"confirmation":"DELETE"}'), 404, was=400,
                 msg="DELETE unknown calendar")
    t.bug.status(u2.api("PATCH", f"/calendars/{st.c1_uuid}", json='{"name":"x"}'), 404, was=400,
                 msg="PATCH another user's calendar")
    r = u2.api("POST", f"/calendars/{st.c1_uuid}/import",
               data=ics_event(e2e.uniq("nf"), "x", "20300101T100000Z", "20300101T110000Z"), ctype="text/calendar")
    t.bug.status(r, 404, was=400, msg="import into another user's calendar")
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


def test_last_guard(t, e2e):
    """the last remaining calendar cannot be deleted"""
    ucal = e2e.create_user("UCAL", "E2E Single Calendar")
    r = ucal.api("GET", "/calendars/")
    if not t.status(r, 200):
        return
    cals = (r.json() or {}).get("calendars") or []
    if not t.eq(len(cals), 1, "a fresh account has exactly one calendar"):
        return
    uuid = cals[0].get("uuid")
    t.status_class(ucal.api("DELETE", f"/calendars/{uuid}", json='{"confirmation":"DELETE"}'), "4xx",
                   msg="deleting the only calendar must be refused")
    t.status(ucal.api("GET", f"/calendars/{uuid}"), 200, msg="the only calendar must survive")
