"""Import (.ics / .vcf: raw body, JSON {data}, multipart upload, duplicate
handling) and export (calendar .ics, address book .vcf) round trips."""

from types import SimpleNamespace

import pytest

from harness.checks import known
from harness.data import ics_event

DESCRIPTION = "calendar/contact import (body, JSON, multipart, duplicates) and export"

pytestmark = pytest.mark.usefixtures("u1", "u2")


@pytest.fixture(scope="module", autouse=True)
def imp(e2e, u1):
    """U1's import calendar and address book."""
    return SimpleNamespace(cal=u1.mk_calendar(e2e.uniq("import")), ab=u1.mk_addressbook(e2e.uniq("import-book")))


def _ics_two(tag: str) -> str:
    return ("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\n"
            f"BEGIN:VEVENT\r\nUID:{tag}-1@e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300301T090000Z\r\n"
            f"DTEND:20300301T100000Z\r\nSUMMARY:Imported one {tag}\r\nEND:VEVENT\r\n"
            f"BEGIN:VEVENT\r\nUID:{tag}-2@e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300302T090000Z\r\n"
            f"DTEND:20300302T100000Z\r\nSUMMARY:Imported two {tag}\r\nEND:VEVENT\r\n"
            "END:VCALENDAR\r\n")


def _summaries(u, cal_uuid) -> str:
    """Sorted summaries of the calendar's events in 2030, '|'-joined."""
    r = u.api("GET", f"/calendars/{cal_uuid}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z&expand=false")
    try:
        return "|".join(sorted(e["summary"] for e in r.json()["events"]))
    except (TypeError, KeyError):
        return ""


def _import(u, cal_uuid, ics, query=""):
    return u.api("POST", f"/calendars/{cal_uuid}/import{query}", data=ics, ctype="text/calendar")


def test_cal_body(t, e2e, u1, imp):
    """calendar import from a text/calendar body"""
    r = _import(u1, imp.cal["uuid"], _ics_two(e2e.run_id))
    if not t.status(r, 200):
        return
    t.json(r, "total", 2)
    t.json(r, "imported", 2)
    t.json(r, "failed", 0)
    t.eq(_summaries(u1, imp.cal["uuid"]), f"Imported one {e2e.run_id}|Imported two {e2e.run_id}",
         "both events must be listed")


def test_cal_skip(t, e2e, u1, imp):
    """re-import skips duplicates by UID (default)"""
    r = _import(u1, imp.cal["uuid"], _ics_two(e2e.run_id))
    if not t.status(r, 200):
        return
    t.json(r, "imported", 0)
    t.json(r, "skipped", 2)
    t.eq(_summaries(u1, imp.cal["uuid"]), f"Imported one {e2e.run_id}|Imported two {e2e.run_id}",
         "no duplicates may appear")


def test_cal_replace(t, e2e, u1, imp):
    """duplicate_handling=replace overwrites by UID"""
    ics = _ics_two(e2e.run_id).replace("Imported one", "Replaced one")
    r = _import(u1, imp.cal["uuid"], ics, "?duplicate_handling=replace")
    if not t.status(r, 200):
        return
    t.json(r, "imported", 2)
    t.eq(_summaries(u1, imp.cal["uuid"]), f"Imported two {e2e.run_id}|Replaced one {e2e.run_id}",
         "the replaced event carries the new summary, no duplicate")


def test_cal_json_multipart(t, e2e, u1):
    """calendar import from JSON {data} and multipart upload"""
    cal2 = u1.mk_calendar(e2e.uniq("import-json"))
    r = u1.api("POST", f"/calendars/{cal2['uuid']}/import",
               json={"data": ics_event(e2e.uniq("json-import"), "From JSON", "20300401T090000Z", "20300401T100000Z")})
    if not t.status(r, 200):
        return
    t.json(r, "imported", 1)
    upload = ics_event(e2e.uniq("multipart"), "From upload", "20300402T090000Z", "20300402T100000Z")
    r = u1.api("POST", f"/calendars/{cal2['uuid']}/import", files={"file": ("upload.ics", upload, "text/calendar")})
    if not t.status(r, 200):
        return
    t.json(r, "imported", 1)
    t.eq(_summaries(u1, cal2["uuid"]), "From JSON|From upload")


def test_cal_invalid(t, u1, imp):
    """calendar import rejects garbage and empty bodies"""
    c = imp.cal["uuid"]
    t.status(_import(u1, c, "BEGIN:VCALENDAR garbage"), 400)
    t.status(u1.api("POST", f"/calendars/{c}/import"), 400, msg="no import data")
    t.status(u1.api("POST", f"/calendars/{c}/import", json='{"data":""}'), 400, msg="empty data")


def test_cal_foreign(t, e2e, u1, u2, imp):
    """calendar import into a foreign calendar is refused"""
    r = _import(u2, imp.cal["uuid"], ics_event(e2e.uniq("intruder"), "Intruder", "20300501T090000Z", "20300501T100000Z"))
    if not t.status_class(r, "4xx"):
        return
    t.not_contains(r, '"imported":1')
    t.eq(_summaries(u1, imp.cal["uuid"]), f"Imported two {e2e.run_id}|Replaced one {e2e.run_id}",
         "nothing may land in U1's calendar")


@known("F14")
def test_cal_overrides(t, e2e, u1):
    """import keeps recurrence exceptions (RECURRENCE-ID)"""
    rec = u1.mk_calendar(e2e.uniq("import-rec"))
    uid = f"{e2e.uniq('series')}@e2e"
    ics = ("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VEVENT\r\n"
           f"UID:{uid}\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300701T100000Z\r\nDTEND:20300701T110000Z\r\n"
           "RRULE:FREQ=DAILY;COUNT=3\r\nSUMMARY:Series\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\n"
           f"UID:{uid}\r\nDTSTAMP:20260101T000000Z\r\nRECURRENCE-ID:20300702T100000Z\r\nDTSTART:20300702T150000Z\r\n"
           "DTEND:20300702T160000Z\r\nSUMMARY:Series (moved)\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
    r = _import(u1, rec["uuid"], ics)
    if not t.status(r, 200):
        return
    t.json(r, "failed", 0)
    # F14: the override is handled as a separate object and skipped as a duplicate UID.
    t.bug.json(r, "skipped", 0, "the RECURRENCE-ID override is part of the series, not a duplicate")
    r = u1.api("GET", f"/calendars/{rec['uuid']}/events?start=2030-06-30T00:00:00Z&end=2030-07-10T00:00:00Z&expand=true")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([x for x in j["events"] if x.get("uid") == uid]) == 3, "the series has 3 instances")

    def moved(j):
        e = [x for x in j["events"] if x.get("recurrence_id") == "20300702T100000Z"][0]
        return e["summary"] == "Series (moved)" and e["start"] == "2030-07-02T15:00:00Z"

    t.bug.jq(r, moved, "the overridden instance must keep its moved time and summary")


@known("F15")
def test_cal_vtimezone(t, e2e, u1):
    """imported TZID events keep their VTIMEZONE"""
    tz = u1.mk_calendar(e2e.uniq("import-tz"))
    uid = f"{e2e.uniq('tz')}@e2e"
    ics = ("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\n"
           "BEGIN:STANDARD\r\nDTSTART:19701025T030000\r\nRRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU\r\n"
           "TZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nTZNAME:CET\r\nEND:STANDARD\r\nBEGIN:DAYLIGHT\r\n"
           "DTSTART:19700329T020000\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU\r\nTZOFFSETFROM:+0100\r\n"
           "TZOFFSETTO:+0200\r\nTZNAME:CEST\r\nEND:DAYLIGHT\r\nEND:VTIMEZONE\r\nBEGIN:VEVENT\r\n"
           f"UID:{uid}\r\nDTSTAMP:20260101T000000Z\r\nDTSTART;TZID=Europe/Berlin:20300601T100000\r\n"
           "DTEND;TZID=Europe/Berlin:20300601T110000\r\nSUMMARY:Berlin morning\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
    r = _import(u1, tz["uuid"], ics)
    if not t.status(r, 200):
        return
    r = u1.api("GET", f"/calendars/{tz['uuid']}/events?start=2030-05-01T00:00:00Z&end=2030-07-01T00:00:00Z")
    t.jq(r, lambda j: j["events"][0]["start"] in ("2030-06-01T10:00:00+02:00", "2030-06-01T08:00:00Z"),
         "the local time must be interpreted in Europe/Berlin")
    # RFC 5545 3.2.19: every TZID used needs its VTIMEZONE in the object.
    r = u1.dav("GET", f"{u1.home_cal}{tz['path']}/{uid}.ics")
    if not t.status(r, 200):
        return
    t.contains(r, "TZID=Europe/Berlin")
    # F15: import re-wraps each VEVENT alone and drops the VTIMEZONE.
    t.bug.contains(r, "BEGIN:VTIMEZONE", "the stored object references TZID=Europe/Berlin but carries no VTIMEZONE")


def test_cal_roundtrip(t, e2e, u1, imp):
    """export → import round trip preserves UIDs and summaries"""
    r = u1.api("GET", f"/calendars/{imp.cal['uuid']}/export")
    if not t.status(r, 200):
        return
    exported = r.text
    rt = u1.mk_calendar(e2e.uniq("roundtrip"))
    r = _import(u1, rt["uuid"], exported)
    if not t.status(r, 200):
        return
    t.json(r, "failed", 0)
    t.eq(_summaries(u1, rt["uuid"]), _summaries(u1, imp.cal["uuid"]), "re-imported calendar must match the original")
    r = u1.api("GET", f"/calendars/{rt['uuid']}/export")
    t.contains(r, f"UID:{e2e.run_id}-1@e2e", "UIDs survive the round trip")


def _vcf_two(tag: str) -> str:
    return (f"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:{tag}-c1\r\nFN:Ada Import{tag}\r\nN:Import{tag};Ada;;;\r\n"
            f"EMAIL;TYPE=work:ada-{tag}@example.test\r\nEND:VCARD\r\n"
            f"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:{tag}-c2\r\nFN:Bob Import{tag}\r\nN:Import{tag};Bob;;;\r\n"
            "TEL;TYPE=cell:+49 30 1234567\r\nEND:VCARD\r\n")


def test_vcf(t, e2e, u1, u2, imp):
    """contact import from a text/vcard body"""
    ab = imp.ab["UUID"]
    r = u1.api("POST", f"/addressbooks/{ab}/import", data=_vcf_two(e2e.run_id), ctype="text/vcard")
    if not t.status(r, 200):
        return
    t.json(r, "total", 2)
    t.json(r, "imported", 2)
    r = u1.api("GET", f"/addressbooks/{ab}/contacts?limit=100")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: sorted(c["formatted_name"] for c in j["Contacts"])
         == [f"Ada Import{e2e.run_id}", f"Bob Import{e2e.run_id}"])
    r = u1.api("POST", f"/addressbooks/{ab}/import", data=_vcf_two(e2e.run_id), ctype="text/vcard")
    t.json(r, "skipped", 2, "re-importing the same UIDs is skipped")
    r = u2.api("POST", f"/addressbooks/{ab}/import", data=_vcf_two("x"), ctype="text/vcard")
    t.status_class(r, "4xx", msg="U2 importing into U1's address book")


def test_vcf_export(t, e2e, u1, u2, imp):
    """address book export contains the contacts"""
    ab = imp.ab["UUID"]
    r = u1.api("GET", f"/addressbooks/{ab}/export")
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "vcard")
    t.contains(r, "BEGIN:VCARD")
    t.contains(r, f"FN:Ada Import{e2e.run_id}")
    t.contains(r, f"UID:{e2e.run_id}-c2")
    r = u2.api("GET", f"/addressbooks/{ab}/export")
    t.status_class(r, "4xx", msg="U2 exporting U1's address book")
    t.not_contains(r, f"Ada Import{e2e.run_id}")


def test_vcf_invalid(t, e2e, u1):
    """contact import refuses cards without VERSION or with vCard 2.1, with a reason"""
    ab = u1.mk_addressbook(e2e.uniq("import-invalid"))["UUID"]
    tag = e2e.run_id
    vcf = (f"BEGIN:VCARD\r\nUID:{tag}-nover\r\nFN:No Version{tag}\r\nEND:VCARD\r\n"
           f"BEGIN:VCARD\r\nVERSION:2.1\r\nUID:{tag}-v21\r\nFN:Old Card{tag}\r\nN:Card{tag};Old;;;\r\nEND:VCARD\r\n"
           f"BEGIN:VCARD\r\nVERSION:4.0\r\nUID:{tag}-ok\r\nFN:Valid Card{tag}\r\nEND:VCARD\r\n")
    r = u1.api("POST", f"/addressbooks/{ab}/import", data=vcf, ctype="text/vcard")
    if not t.status(r, 200):
        return
    t.json(r, "total", 3)
    t.json(r, "imported", 1)
    t.json(r, "failed", 2, "a card without VERSION (RFC 6350 6.7.9) and a 2.1 card must fail")
    reasons = {e.get("uid"): e.get("error", "") for e in (r.jdict().get("errors") or [])}
    t.true("VERSION" in reasons.get(f"{tag}-nover", ""), "the missing VERSION is named", r)
    t.true("2.1" in reasons.get(f"{tag}-v21", ""), "the unsupported version is named", r)
    r = u1.api("GET", f"/addressbooks/{ab}/contacts?limit=100")
    if t.status(r, 200):
        t.jq(r, lambda j: [c["formatted_name"] for c in j["Contacts"]] == [f"Valid Card{tag}"],
             "only the valid card is stored")


def test_vcf_no_fn(t, e2e, u1):
    """contact import derives a missing FN from N"""
    ab = u1.mk_addressbook(e2e.uniq("import-no-fn"))["UUID"]
    tag = e2e.run_id
    r = u1.api("POST", f"/addressbooks/{ab}/import",
               data=f"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:{tag}-nofn\r\nN:Lovelace{tag};Ada;;;\r\nEND:VCARD\r\n",
               ctype="text/vcard")
    if not t.status(r, 200):
        return
    t.json(r, "imported", 1)
    r = u1.api("GET", f"/addressbooks/{ab}/export")
    if t.status(r, 200):
        t.contains(r, f"FN:Ada Lovelace{tag}", "FN is required (RFC 6350 6.2.1); derived from N")
