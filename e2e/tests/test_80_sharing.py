"""Sharing: calendar and address-book shares (REST management, what the sharee
sees over REST and DAV, read vs read-write enforcement, revocation) and
public calendar links."""

from types import SimpleNamespace

import pytest

from harness.checks import known
from harness.data import PROPFIND_COLLECTION, PROPFIND_LIST, ics_event, sync_body, vcard
from harness.session import ZERO_UUID, SetupFailed
from harness.xml import lp, xstr

DESCRIPTION = "calendar + address book shares (REST & DAV), public calendar links"

pytestmark = pytest.mark.usefixtures("u1", "u2")


@pytest.fixture(scope="module", autouse=True)
def sh(e2e, u1, u2):
    if not u1.ensure_dav():
        raise SetupFailed("sharing: no DAV app password for U1")
    if not u2.ensure_dav():
        raise SetupFailed("sharing: no DAV app password for U2")
    cal = u1.mk_calendar(e2e.uniq("shared-cal"), "#445566")
    ev = u1.mk_event(cal["uuid"], f"Owner event {e2e.run_id}", "2030-03-03T09:00:00Z", "2030-03-03T10:00:00Z")
    ab = u1.mk_addressbook(e2e.uniq("shared-book"))
    ct = u1.mk_contact(ab["UUID"], f"Wanda Shared {e2e.run_id}")
    return SimpleNamespace(cal=cal, uuid=cal["uuid"], ev=ev["id"], ab=ab, ab_uuid=ab["UUID"], ct=ct["id"])


def _share_cal(user, uuid, identifier, permission):
    return user.api("POST", f"/calendars/{uuid}/shares", json={"user_identifier": identifier, "permission": permission})


def _selected(items, key, value):
    """jq ``.[] | select(.KEY == VALUE)`` on a raw JSON list."""
    return [x for x in items if x.get(key) == value]


def _last_holds(items, pred):
    """jq -e semantics on a select(...) stream: there is a match and the last one satisfies PRED."""
    return bool(items) and bool(pred(items[-1]))


def _has_key(obj, key):
    """jq ``[.. | objects | has(KEY)] | any``."""
    if isinstance(obj, dict):
        return key in obj or any(_has_key(v, key) for v in obj.values())
    if isinstance(obj, list):
        return any(_has_key(v, key) for v in obj)
    return False


# --- calendar shares ----------------------------------------------------------

def test_cal_validation(t, e2e, u1, u2, sh):
    """calendar share: validation (self, unknown user, bad permission)"""
    t.status(_share_cal(u1, sh.uuid, u1.email, "read"), 400, msg="sharing with yourself")
    t.status_class(_share_cal(u1, sh.uuid, f"{e2e.uniq('nobody')}@{e2e.domain}", "read"), "4xx",
                   msg="sharing with an unknown user")
    t.status(_share_cal(u1, sh.uuid, u2.email, "admin"), 400, msg="unknown permission")
    t.status(_share_cal(u1, ZERO_UUID, u2.email, "read"), 404, msg="unknown calendar")
    t.status(_share_cal(u1, sh.cal["id"], u2.email, "read"), 404,
             msg="shares are addressed by calendar UUID, not numeric id")


def test_cal_create(t, e2e, u1, u2, sh, st):
    """calendar share: create read share by e-mail"""
    r = _share_cal(u1, sh.uuid, u2.email, "read")
    if not t.status(r, 201):
        return
    st.share_id = r.json()["id"]
    e2e.defer("revoke calendar share", e2e.cleanup_api, u1, "DELETE", f"/calendars/{sh.uuid}/shares/{st.share_id}")
    t.json(r, "permission", "read")
    t.json(r, "shared_with.email", u2.email)
    t.json(r, "calendar_id", sh.uuid)


def test_cal_list(t, u1, u2, sh, st):
    """calendar share: owner lists it, sharee cannot manage shares"""
    t.need(st, "share_id")
    r = u1.api("GET", f"/calendars/{sh.uuid}/shares")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([s for s in j["shares"] if s["id"] == st.share_id and s["permission"] == "read"]) == 1)
    t.status(u2.api("GET", f"/calendars/{sh.uuid}/shares"), 404, msg="a sharee cannot list the shares")


def test_cal_duplicate(t, u1, u2, sh, st):
    """calendar share: duplicate share is refused"""
    t.need(st, "share_id")
    if not t.status_class(_share_cal(u1, sh.uuid, u2.email, "read-write"), "4xx",
                          msg="a second share for the same user"):
        return
    r = u1.api("GET", f"/calendars/{sh.uuid}/shares")
    t.jq(r, lambda j: len(j["shares"]) == 1, "still exactly one share")


def test_cal_sharee_rest(t, e2e, u1, u2, sh, st):
    """sharee sees the calendar over REST (shared, owner, permission)"""
    t.need(st, "share_id")
    r = u2.api("GET", "/calendars/")
    if not t.status(r, 200):
        return
    if not t.jq(r, lambda j: _last_holds(_selected(j["calendars"], "uuid", sh.uuid),
                                         lambda c: c["shared"] is True and c["permission"] == "read"),
                "the shared calendar is listed with shared=true, permission=read"):
        return
    t.jq(r, lambda j: _last_holds(_selected(j["calendars"], "uuid", sh.uuid),
                                  lambda c: c["owner"]["display_name"] == u1.display), "with the owner's name")
    t.jq(r, lambda j: not any(_has_key(c, "user_id") for c in _selected(j["calendars"], "uuid", sh.uuid)),
         "the sharee must not learn the owner's internal user id (P03, #190)")
    r = u2.api("GET", f"/calendars/{sh.uuid}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([e for e in j["events"] if e["summary"] == f"Owner event {e2e.run_id}"]) == 1,
         "the sharee reads the events")


def test_cal_sharee_dav(t, e2e, u2, sh, st):
    """sharee sees the calendar in the DAV home under its UUID"""
    t.need(st, "share_id")
    r = u2.dav("PROPFIND", u2.home_cal, depth=1, xml=PROPFIND_LIST)
    if not t.status(r, 207):
        return
    st.dav_col = f"{u2.home_cal}{sh.uuid}/"
    t.xpath(r, f"{lp('response', 'href')}[normalize-space()='{st.dav_col}']",
            "shared calendars appear under /calendars/<uuid>/ in the sharee's home")
    r = u2.dav("PROPFIND", st.dav_col, depth=0, xml=PROPFIND_COLLECTION)
    if not t.status(r, 207):
        return
    t.xpath(r, lp("current-user-privilege-set", "**", "read"), "read privilege")
    t.xpath(r, f"not({lp('current-user-privilege-set', '**', 'write')})", "a read share must not advertise write")
    r = u2.dav("REPORT", st.dav_col, depth=1, xml=sync_body())
    if not t.status(r, 207):
        return
    st.dav_obj = xstr(r, f"({lp('response', 'href')})[contains(.,'.ics')][1]")
    if not st.dav_obj.startswith(st.dav_col):
        t.fail(f"sync must list hrefs inside the shared collection, got '{st.dav_obj}'")
    r = u2.dav("GET", st.dav_obj)
    t.status(r, 200)
    t.contains(r, f"SUMMARY:Owner event {e2e.run_id}")


def test_cal_readonly(t, e2e, u1, u2, sh, st):
    """read-only sharee: reads work, every write is 403"""
    t.need(st, "dav_col", "dav_obj")
    r = u2.api("POST", f"/calendars/{sh.uuid}/events",
               json='{"summary":"x","start":"2030-03-04T09:00:00Z","end":"2030-03-04T10:00:00Z"}')
    t.status(r, 403, msg="REST create")
    t.status(u2.api("PATCH", f"/calendars/{sh.uuid}/events/{sh.ev}", json='{"summary":"hijack"}'), 403,
             msg="REST update")
    t.status(u2.api("DELETE", f"/calendars/{sh.uuid}/events/{sh.ev}"), 403, msg="REST delete")
    r = u2.dav("PUT", f"{st.dav_col}{e2e.uniq('ro-put')}.ics",
               ics=ics_event(e2e.uniq("ro-put"), "ro", "20300304T090000Z", "20300304T100000Z"))
    t.status(r, 403, msg="DAV PUT")
    t.status(u2.dav("DELETE", st.dav_obj), 403, msg="DAV DELETE")
    r = u1.api("GET", f"/calendars/{sh.uuid}/events/{sh.ev}")
    t.json(r, "summary", f"Owner event {e2e.run_id}", "the owner's event must be untouched")


def test_cal_readwrite(t, e2e, u1, u2, sh, st):
    """upgrade to read-write: sharee writes over REST and DAV"""
    t.need(st, "share_id", "dav_col")
    r = u1.api("PATCH", f"/calendars/{sh.uuid}/shares/{st.share_id}", json='{"permission":"read-write"}')
    if not t.status(r, 200):
        return
    t.json(r, "permission", "read-write")
    uid = e2e.uniq("by-sharee")
    r = u2.dav("PUT", f"{st.dav_col}{uid}.ics",
               ics=ics_event(f"{uid}@e2e", "Put by sharee", "20300305T090000Z", "20300305T100000Z"))
    if not t.status(r, 201, 204, msg="DAV PUT by a read-write sharee"):
        return
    r = u2.api("POST", f"/calendars/{sh.uuid}/events",
               json='{"summary":"REST by sharee","start":"2030-03-06T09:00:00Z","end":"2030-03-06T10:00:00Z"}')
    t.status(r, 201, msg="REST create by a read-write sharee")
    r = u1.api("GET", f"/calendars/{sh.uuid}/events?start=2030-03-01T00:00:00Z&end=2030-04-01T00:00:00Z")
    t.jq(r, lambda j: {"Put by sharee", "REST by sharee"} <= {e["summary"] for e in j["events"]},
         "the owner sees what the sharee wrote")
    r = u2.dav("PROPFIND", st.dav_col, depth=0, xml=PROPFIND_COLLECTION)
    t.xpath(r, lp("current-user-privilege-set", "**", "write"), "a read-write share advertises write")


def test_cal_sharee_limits(t, e2e, u1, u2, sh, st):
    """sharee cannot rename, re-share or delete the calendar"""
    t.need(st, "share_id")
    t.status_class(u2.api("PATCH", f"/calendars/{sh.uuid}", json='{"name":"renamed by sharee"}'), "4xx",
                   msg="sharee renaming the calendar")
    r = u2.dav("PROPPATCH", getattr(st, "dav_col", ""),
               xml='<?xml version="1.0" encoding="utf-8"?><d:propertyupdate xmlns:d="DAV:"><d:set><d:prop>'
                   '<d:displayname>renamed by sharee</d:displayname></d:prop></d:set></d:propertyupdate>')
    t.status(r, 403, msg="sharee PROPPATCH on the shared collection")
    r = u2.api("POST", f"/calendars/{sh.uuid}/shares", json={"user_identifier": u1.email, "permission": "read"})
    t.status(r, 404, msg="a sharee cannot create shares")
    t.status_class(u2.api("DELETE", f"/calendars/{sh.uuid}", json='{"confirmation":"DELETE"}'), "4xx",
                   msg="sharee deleting the calendar")
    t.status(u2.api("POST", f"/calendars/{sh.uuid}/public", json='{"enabled":true}'), 404,
             msg="a sharee cannot publish the calendar")
    r = u1.api("GET", f"/calendars/{sh.uuid}")
    if not t.status(r, 200):
        return
    t.json(r, "name", e2e.uniq("shared-cal"), "the calendar must be untouched")


def test_cal_revoke(t, u1, u2, sh, st):
    """revoking the share removes all access"""
    t.need(st, "share_id")
    t.status(u2.api("DELETE", f"/calendars/{sh.uuid}/shares/{st.share_id}"), 404,
             msg="the sharee cannot revoke (manage) shares")
    if not t.status(u1.api("DELETE", f"/calendars/{sh.uuid}/shares/{st.share_id}"), 204):
        return
    t.not_contains(u2.api("GET", "/calendars/"), sh.uuid, "the calendar must leave the sharee's list")
    r = u2.api("GET", f"/calendars/{sh.uuid}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z")
    t.status(r, 404, msg="REST access ends")
    t.status(u2.dav("PROPFIND", getattr(st, "dav_col", ""), depth=0, xml=PROPFIND_LIST), 404, msg="DAV access ends")
    r = _share_cal(u1, sh.uuid, u2.email, "read")
    if not t.status(r, 201, msg="re-sharing after a revoke works"):
        return
    u1.api("DELETE", f"/calendars/{sh.uuid}/shares/{r.jdict().get('id')}")


def test_cal_delete_cascade(t, e2e, u1, u2):
    """deleting a shared calendar removes it for the sharee"""
    doom = u1.mk_calendar(e2e.uniq("doomed-shared"))
    if not t.status(_share_cal(u1, doom["uuid"], u2.email, "read"), 201):
        return
    if not t.status(u1.api("DELETE", f"/calendars/{doom['uuid']}", json='{"confirmation":"DELETE"}'), 204):
        return
    t.not_contains(u2.api("GET", "/calendars/"), doom["uuid"],
                   "a deleted calendar must not linger in the sharee's list")
    r = u2.dav("PROPFIND", u2.home_cal, depth=1, xml=PROPFIND_LIST)
    t.not_contains(r, doom["uuid"], "nor in the sharee's DAV home")


def test_by_username(t, e2e, u1, u2):
    """shares accept the username as identifier"""
    cal = u1.mk_calendar(e2e.uniq("by-username"))
    r = _share_cal(u1, cal["uuid"], u2.username, "read")
    if t.status(r, 201, msg="user_identifier may be the account username"):
        t.json(r, "shared_with.email", u2.email)


# --- address books ------------------------------------------------------------

def test_ab_create(t, e2e, u1, u2, sh, st):
    """address-book share: create and list"""
    r = u1.api("POST", f"/addressbooks/{sh.ab_uuid}/shares",
               json={"user_identifier": u2.email, "permission": "read-write"})
    if not t.status(r, 201):
        return
    st.ab_share = r.json()["id"]
    e2e.defer("revoke address-book share", e2e.cleanup_api, u1, "DELETE",
              f"/addressbooks/{sh.ab_uuid}/shares/{st.ab_share}")
    t.json(r, "permission", "read-write")
    r = u1.api("GET", f"/addressbooks/{sh.ab_uuid}/shares")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([s for s in j["shares"] if s["id"] == st.ab_share]) == 1)
    t.status(u2.api("GET", f"/addressbooks/{sh.ab_uuid}/shares"), 404, msg="a sharee cannot list the shares")


def test_ab_sharee_rest(t, u1, u2, sh, st):
    """sharee sees the address book over REST"""
    t.need(st, "ab_share")
    r = u2.api("GET", "/addressbooks/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: _last_holds(
        _selected(j["addressbooks"], "UUID", sh.ab_uuid),
        lambda b: b["shared"] is True and b["permission"] == "read-write" and b["owner"]["display_name"] == u1.display))
    r = u2.api("GET", f"/addressbooks/{sh.ab_uuid}/contacts")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([c for c in j["Contacts"] if c["id"] == sh.ct]) == 1, "the sharee lists the owner's contacts")


def test_ab_no_leak(t, e2e, u1, u2, sh, st):
    """shared address book does not leak the owner's account data"""
    t.need(st, "ab_share")
    r = u2.api("GET", "/addressbooks/")
    if not t.status(r, 200):
        return
    # Raw bytes first: a typed model would silently drop an unexpected field.
    hash_seen = '"PasswordHash":"$2' in r.text
    t.jq(r, lambda j: not any(_has_key(b, "PasswordHash") for b in _selected(j["addressbooks"], "UUID", sh.ab_uuid)),
         "GET /addressbooks must not embed the owner's user record (PasswordHash)")
    t.not_contains(r, u1.email, "the owner's e-mail address must not be disclosed to the sharee")
    t.jq(r, lambda j: not any(_has_key(b, k) for b in _selected(j["addressbooks"], "UUID", sh.ab_uuid)
                              for k in ("UserID", "user_id", "DeletedAt")),
         "the sharee must not learn the owner's internal user id (P03, #190)")
    r = u2.api("GET", f"/search?q={e2e.uniq('shared-book')}&types=addressbooks")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: not _has_key(j, "PasswordHash"), "GET /search must not embed the owner's user record")
    if hash_seen:
        t.fail("SECURITY: the response contains the owner's bcrypt password hash")


def test_ab_search(t, e2e, u2, sh, st):
    """contact search covers shared books (and only those)"""
    t.need(st, "ab_share")
    r = u2.api("GET", f"/contacts/search?q=Wanda+Shared+{e2e.run_id}")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([c for c in j["contacts"] if c["id"] == sh.ct]) == 1, "contacts in a shared book are searchable")
    t.status(u2.api("GET", f"/contacts/search?q=Wanda&addressbook_id={sh.ab_uuid}"), 200,
             msg="addressbook_id may name a shared book")


def test_ab_sharee_dav(t, e2e, u1, u2, sh, st):
    """sharee reads and writes the shared book over CardDAV"""
    t.need(st, "ab_share")
    r = u2.dav("PROPFIND", u2.home_ab, depth=1, xml=PROPFIND_LIST)
    if not t.status(r, 207):
        return
    t.contains(r, sh.ab_uuid, "the shared book appears in the sharee's CardDAV home")
    st.ab_davcol = f"{u2.home_ab}{sh.ab_uuid}/"
    if not t.status(u2.dav("PROPFIND", st.ab_davcol, depth=1, xml=PROPFIND_LIST), 207):
        return
    uid = e2e.uniq("ab-by-sharee")
    r = u2.dav("PUT", f"{st.ab_davcol}{uid}.vcf", vcf=vcard(uid, "Added By Sharee"))
    if not t.status(r, 201, 204, msg="a read-write sharee may add cards"):
        return
    r = u1.api("GET", f"/addressbooks/{sh.ab_uuid}/contacts?limit=200")
    t.jq(r, lambda j: len([c for c in j["Contacts"] if c["formatted_name"] == "Added By Sharee"]) == 1,
         "the owner sees the new card")


def test_ab_readonly(t, e2e, u1, u2, sh, st):
    """read-only address-book share refuses writes"""
    t.need(st, "ab_share", "ab_davcol")
    r = u1.api("PATCH", f"/addressbooks/{sh.ab_uuid}/shares/{st.ab_share}", json='{"permission":"read"}')
    if not t.status(r, 200):
        return
    uid = e2e.uniq("ab-ro")
    t.status(u2.dav("PUT", f"{st.ab_davcol}{uid}.vcf", vcf=vcard(uid, "Read Only Write")), 403,
             msg="DAV PUT on a read share")
    t.status(u2.api("POST", f"/addressbooks/{sh.ab_uuid}/contacts", json='{"formatted_name":"RO REST"}'), 403,
             msg="REST create on a read share")
    t.status(u2.api("DELETE", f"/addressbooks/{sh.ab_uuid}/contacts/{sh.ct}"), 403, msg="REST delete on a read share")
    t.status(u1.api("GET", f"/addressbooks/{sh.ab_uuid}/contacts/{sh.ct}"), 200, msg="the owner's contact survives")


def test_ab_revoke(t, e2e, u1, u2, sh, st):
    """revoking the address-book share removes access"""
    t.need(st, "ab_share")
    if not t.status(u1.api("DELETE", f"/addressbooks/{sh.ab_uuid}/shares/{st.ab_share}"), 204):
        return
    t.not_contains(u2.api("GET", "/addressbooks/"), sh.ab_uuid)
    r = u2.dav("PROPFIND", u2.home_ab, depth=1, xml=PROPFIND_LIST)
    t.not_contains(r, sh.ab_uuid, "the book leaves the sharee's CardDAV home")
    r = u2.api("GET", f"/contacts/search?q=Wanda+Shared+{e2e.run_id}")
    t.jq(r, lambda j: len(j["contacts"]) == 0, "and the sharee's contact search")


# --- public links -------------------------------------------------------------

def test_pub_default(t, u1, sh):
    """public link: disabled by default"""
    r = u1.api("GET", f"/calendars/{sh.uuid}/public")
    if not t.status(r, 200):
        return
    t.json(r, "enabled", False)


def test_pub_enable(t, e2e, u1, sh, st):
    """public link: enable serves an anonymous iCalendar feed"""
    r = u1.api("POST", f"/calendars/{sh.uuid}/public", json='{"enabled":true}')
    if not t.status(r, 200):
        return
    e2e.defer("disable public link", e2e.cleanup_api, u1, "POST", f"/calendars/{sh.uuid}/public",
              json='{"enabled":false}')
    t.json(r, "enabled", True)
    st.pub_token = r.jdict().get("token") or ""
    t.nonempty(r, "public_url")
    if len(st.pub_token) < 16:
        t.fail(f"the public token is too short to be unguessable: '{st.pub_token}'")
    r = e2e.raw("GET", f"/public/calendar/{st.pub_token}")
    if not t.status(r, 200, msg="anonymous GET of the feed"):
        return
    t.header(r, "Content-Type", "text/calendar")
    t.contains(r, "BEGIN:VCALENDAR")
    t.contains(r, f"SUMMARY:Owner event {e2e.run_id}")
    t.status(e2e.raw("GET", f"/public/calendar/{st.pub_token}.ics"), 200, msg="the .ics suffix variant")
    r = u1.api("GET", f"/calendars/{sh.uuid}/public")
    t.json(r, "token", st.pub_token, "GET …/public reports the active token")


def test_pub_unknown(t, e2e):
    """public link: unknown token is 404"""
    t.status(e2e.raw("GET", f"/public/calendar/not-a-real-token-{e2e.run_id}"), 404)


def test_pub_regenerate(t, e2e, u1, sh, st):
    """public link: regenerate invalidates the old URL"""
    t.need(st, "pub_token")
    r = u1.api("POST", f"/calendars/{sh.uuid}/public/regenerate")
    if not t.status(r, 200):
        return
    new = r.jdict().get("token")
    if not t.ne(new, st.pub_token, "regenerate must mint a new token"):
        return
    t.status(e2e.raw("GET", f"/public/calendar/{st.pub_token}"), 404, msg="the old link must stop working")
    t.status(e2e.raw("GET", f"/public/calendar/{new}"), 200, msg="the new link works")
    st.pub_token = new


def test_pub_owner_only(t, u2, sh):
    """public link: only the owner can manage it"""
    t.status(u2.api("GET", f"/calendars/{sh.uuid}/public"), 404, msg="a stranger cannot read the public status / token")
    t.status(u2.api("POST", f"/calendars/{sh.uuid}/public/regenerate"), 404,
             msg="a stranger cannot regenerate the token")


def test_pub_disable(t, e2e, u1, sh, st):
    """public link: disable makes the feed 404"""
    t.need(st, "pub_token")
    r = u1.api("POST", f"/calendars/{sh.uuid}/public", json='{"enabled":false}')
    if not t.status(r, 200):
        return
    t.json(r, "enabled", False)
    t.status(e2e.raw("GET", f"/public/calendar/{st.pub_token}"), 404, msg="a disabled link must 404")


@known("F18")
def test_deleted_owner(t, e2e, u2):
    """a deleted account's shares and public links stop serving its data"""
    ugone = e2e.create_user("UGONE", "E2E Leaving Owner")
    cal = ugone.mk_calendar(e2e.uniq("leaving"))["uuid"]
    ev = ugone.mk_event(cal, f"Private appointment {e2e.run_id}", "2030-04-04T09:00:00Z", "2030-04-04T10:00:00Z")
    if not t.status(_share_cal(ugone, cal, u2.email, "read"), 201):
        return
    r = ugone.api("POST", f"/calendars/{cal}/public", json='{"enabled":true}')
    tok = r.jdict().get("token") or ""
    if not t.true(tok, "enabling the public link must return its token", r):
        return
    # Preconditions: ordinary checks — a regression here is a FAIL, never F18.
    if not t.status(e2e.raw("GET", f"/public/calendar/{tok}"), 200,
                    msg="the public link works while the owner exists"):
        return
    if not t.status(u2.api("GET", f"/calendars/{cal}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z"),
                    200, msg="the sharee reads the calendar while the owner exists"):
        return
    # The server offers no way to clean up after a deleted account, so empty
    # the calendar first: whatever outlives the account is an empty calendar.
    ugone.api("DELETE", f"/calendars/{cal}/events/{ev['id']}")
    if not ugone.delete_account():
        return t.fail("account deletion failed")
    # F18: DELETE /users/me soft-deletes the user row only; shares and the link live on.
    r = u2.api("GET", "/calendars/")
    t.status(r, 200)
    t.bug.not_contains(r, cal, "the deleted owner's calendar must leave the sharee's list")
    r = u2.api("GET", f"/calendars/{cal}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z")
    t.bug.status(r, 404, was=200, msg="the sharee must no longer read the deleted owner's calendar")
    t.bug.status(e2e.raw("GET", f"/public/calendar/{tok}"), 404, was=200,
                 msg="the public link of a deleted account must stop serving its calendar")
