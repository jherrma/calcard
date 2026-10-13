"""User profile, preferences and the full-account backup export."""

import io
import json
import re
import zipfile

import pytest

from harness.session import SetupFailed

DESCRIPTION = "user profile, preferences, backup export (ZIP)"

pytestmark = pytest.mark.usefixtures("u1")


def test_get(t, u1):
    """GET /users/me returns the profile with stats"""
    r = u1.api("GET", "/users/me")
    if not t.status(r, 200):
        return
    t.json(r, "data.id", u1.id)
    t.json(r, "data.email", u1.email)
    # The account username, exactly as registration issued it: it is the
    # /dav/{username}/ segment the web UI builds the DAV URLs from.
    t.json(r, "data.username", u1.username)
    t.json(r, "data.is_active", True)
    t.jq(r, lambda j: j["data"]["stats"]["calendar_count"] >= 1, "registration seeds a default calendar")
    t.jq(r, lambda j: "contact_count" in j["data"]["stats"] and "app_password_count" in j["data"]["stats"],
         'expected JSON condition to hold: .data.stats | has("contact_count") and has("app_password_count")')
    t.jq(r, lambda j: re.match(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T", j["data"]["created_at"]), "created_at is RFC 3339")
    t.not_contains(r, "PasswordHash", "the profile must never expose the password hash")
    t.not_contains(r, "password_hash")


def test_patch(t, e2e, u1):
    """PATCH /users/me updates the display name"""
    name = f"E2E Renamed {e2e.run_id}"
    r = u1.api("PATCH", "/users/me", json={"display_name": name})
    if not t.status(r, 200):
        return
    t.json(r, "data.display_name", name)
    t.json(r, "data.username", u1.username, "PATCH answers with the same profile shape as GET")
    t.json(u1.api("GET", "/users/me"), "data.display_name", name, "the new name must persist")
    t.status(u1.api("PATCH", "/users/me", json={"display_name": u1.display}), 200)


def test_prefs_defaults(t, u1):
    """preferences: defaults for every known key"""
    r = u1.api("GET", "/users/me/preferences")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: {"default_event_duration", "default_all_day", "time_format", "accent_color"}
         <= j["data"]["preferences"].keys(),
         "all four known preference keys must be present (defaults filled in)")


def test_prefs_update(t, u1):
    """preferences: valid update is stored and normalized"""
    r = u1.api("PATCH", "/users/me/preferences",
               json='{"preferences":{"time_format":"12h","default_event_duration":"30","accent_color":"#8B5CF6"}}')
    if not t.status(r, 200):
        return
    t.json(r, "data.preferences.time_format", "12h")
    t.json(r, "data.preferences.default_event_duration", "30")
    t.json(r, "data.preferences.accent_color", "#8b5cf6", "accent colour is normalized to lower case")
    t.json(u1.api("GET", "/users/me/preferences"), "data.preferences.time_format", "12h", "the update must persist")
    # restore defaults
    r = u1.api("PATCH", "/users/me/preferences",
               json='{"preferences":{"time_format":"24h","default_event_duration":"60","accent_color":"#3b82f6"}}')
    t.status(r, 200)


def test_prefs_invalid(t, u1):
    """preferences: unknown key / invalid value / empty body rejected"""
    p = "/users/me/preferences"
    t.status(u1.api("PATCH", p, json='{"preferences":{"bogus_key":"1"}}'), 400)
    t.status(u1.api("PATCH", p, json='{"preferences":{"time_format":"13h"}}'), 400)
    t.status(u1.api("PATCH", p, json='{"preferences":{"default_event_duration":"17"}}'), 400,
             msg="durations are a closed set")
    t.status(u1.api("PATCH", p, json='{"preferences":{"accent_color":"blue"}}'), 400)
    t.status(u1.api("PATCH", p, json='{"preferences":{}}'), 400)


def test_backup(t, e2e, u1):
    """backup export streams a ZIP with metadata, .ics and .vcf"""
    cal_name, book_name = e2e.uniq("backup-cal"), e2e.uniq("backup-book")
    cal = u1.mk_calendar(cal_name)
    try:
        u1.mk_event(cal["uuid"], f"Backup event {e2e.run_id}", "2030-03-01T10:00:00Z", "2030-03-01T11:00:00Z")
    except SetupFailed as e:
        t.fail(f"seed event failed\n{e}")
    ab = u1.mk_addressbook(book_name)
    try:
        u1.mk_contact(ab["UUID"], f"Backup Contact {e2e.run_id}")
    except SetupFailed as e:
        t.fail(f"seed contact failed\n{e}")
    r = u1.api("GET", "/users/me/export")
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "application/zip")
    t.header(r, "Content-Disposition", "attachment")
    if not t.eq(r.content[:2].decode("latin-1"), "PK", "the body must be a ZIP archive"):
        return
    try:
        zf = zipfile.ZipFile(io.BytesIO(r.content))
        listing = zf.namelist()
        meta_text = zf.read("metadata.json").decode() if "metadata.json" in listing else ""
    except (zipfile.BadZipFile, KeyError) as e:
        return t.fail(f"the archive cannot be read: {e}")
    entries = ", ".join(listing)
    if not any("metadata.json" in n for n in listing):
        t.fail(f"archive lacks metadata.json (entries: {entries})")
    if not any(f"calendars/{cal_name}.ics" in n for n in listing):
        t.fail(f"archive lacks calendars/{cal_name}.ics (entries: {entries})")
    if not any(f"addressbooks/{book_name}.vcf" in n for n in listing):
        t.fail(f"archive lacks addressbooks/{book_name}.vcf")
    try:
        meta = json.loads(meta_text)
    except ValueError:
        meta = {}
    try:
        ok = any(c.get("event_count") == 1 for c in meta.get("calendars", []) if c.get("name") == cal_name)
    except (AttributeError, TypeError):
        ok = False
    if not ok:
        t.fail(f"metadata.json must report event_count 1 for the seeded calendar: {meta_text[:400]}")
    try:
        ok = any(b.get("contact_count") == 1 for b in meta.get("addressbooks", []) if b.get("name") == book_name)
    except (AttributeError, TypeError):
        ok = False
    if not ok:
        t.fail("metadata.json must report contact_count 1 for the seeded address book")
