"""DAV authentication and authorization: 401 shapes, accepted credential kinds,
and — most important — that one user can never read or write another
user's DAV collections, objects or homes."""

import base64
from types import SimpleNamespace

import pytest

from harness.checks import known
from harness.data import PROPFIND_COLLECTION, PROPFIND_LIST, PROPFIND_PRINCIPAL, ics_event, sync_body, vcard
from harness.session import SetupFailed

DESCRIPTION = "DAV 401 shapes, credential kinds, cross-user isolation on every verb"

pytestmark = pytest.mark.usefixtures("u1", "u2")


@pytest.fixture(scope="module", autouse=True)
def az(e2e, u1, u2):
    """U2 owns one calendar with one event and one address book with one card."""
    if not u1.ensure_dav():
        raise SetupFailed("dav_authz: no DAV app password for U1")
    if not u2.ensure_dav():
        raise SetupFailed("dav_authz: no DAV app password for U2")
    cal = u2.mk_calendar(e2e.uniq("u2-private-cal"))
    ab = u2.mk_addressbook(e2e.uniq("u2-private-book"))
    a = SimpleNamespace(cal=cal, ab=ab, u2=u2.username)
    a.col = f"/dav/{a.u2}/calendars/{cal['path']}/"
    a.abcol = f"/dav/{a.u2}/addressbooks/{ab['Path']}/"
    a.ev_uid = f"{e2e.uniq('secret')}@e2e"
    a.obj = f"{a.col}{e2e.uniq('secret')}.ics"
    r = u2.dav("PUT", a.obj, ics=ics_event(a.ev_uid, "U2 secret meeting", "20300101T100000Z", "20300101T110000Z"))
    if r.status not in (201, 204):
        raise SetupFailed(f"dav_authz: seeding U2's event failed (HTTP {r.status})", r)
    a.card = f"{a.abcol}{e2e.uniq('secret-card')}.vcf"
    r = u2.dav("PUT", a.card, vcf=vcard(e2e.uniq("secret-card"), "Secret Person"))
    if r.status not in (201, 204):
        raise SetupFailed(f"dav_authz: seeding U2's card failed (HTTP {r.status})", r)
    return a


def test_noauth(t, e2e, az):
    """no credentials → 401 with a Basic challenge"""
    r = e2e.raw("PROPFIND", "/dav/", depth=0, xml=PROPFIND_PRINCIPAL)
    if not t.status(r, 401):
        return
    t.header(r, "WWW-Authenticate", "Basic")
    t.header(r, "WWW-Authenticate", "realm=")
    t.status(e2e.raw("GET", az.obj), 401, msg="object access without credentials")
    t.status(e2e.raw("OPTIONS", "/dav/"), 401, msg="OPTIONS is authenticated as well")


def test_wrong(t, e2e, u1, u2):
    """wrong password / unknown user → 401"""
    dav = f"{e2e.base}/dav/"
    r = e2e.req("PROPFIND", dav, basic=(u1.email, f"wrong-{e2e.run_id}"), depth=0, xml=PROPFIND_PRINCIPAL)
    if not t.status(r, 401):
        return
    t.header(r, "WWW-Authenticate", "Basic")
    r = e2e.req("PROPFIND", dav, basic=(f"{e2e.uniq('nobody')}@{e2e.domain}", "whatever"), depth=0,
                xml=PROPFIND_PRINCIPAL)
    t.status(r, 401)
    # U2's app password does not work for U1's identity.
    r = e2e.req("PROPFIND", dav, basic=(u1.email, u2.apppw), depth=0, xml=PROPFIND_PRINCIPAL)
    t.status(r, 401, msg="another user's app password must not authenticate U1")


def test_malformed(t, e2e):
    """malformed Authorization headers → 401, never 5xx"""
    no_colon = base64.b64encode(b"no-colon").decode()
    for h in ("Basic", "Basic !!!not-base64!!!", f"Basic {no_colon}", 'Digest username="x"', "Bearer not.a.jwt",
              "Bearer"):
        r = e2e.raw("PROPFIND", "/dav/", depth=0, headers={"Authorization": h}, xml=PROPFIND_PRINCIPAL)
        t.status(r, 401, msg=f"Authorization: {h}")


def test_other_credentials(t, e2e, u1):
    """the account password and a JWT bearer are accepted too"""
    r = e2e.req("PROPFIND", f"{e2e.base}/dav/", basic=(u1.email, u1.password), depth=0, xml=PROPFIND_PRINCIPAL)
    t.status(r, 207, msg="Basic with the account password")
    u1.ensure_token()
    r = e2e.req("PROPFIND", f"{e2e.base}/dav/", bearer=u1.token, depth=0, xml=PROPFIND_PRINCIPAL)
    t.status(r, 207, msg="Bearer with a JWT access token")


def test_foreign_principal(t, u1, az):
    """another user's principal URL reveals none of their data"""
    r = u1.dav("PROPFIND", f"/dav/{az.u2}/", depth=0, xml=PROPFIND_PRINCIPAL)
    t.status_not(r, 500, 502, 503)
    t.not_contains(r, f"/dav/{az.u2}/calendars/", "U2's home set must not be advertised to U1")
    t.not_contains(r, f"/dav/{az.u2}/addressbooks/")


def test_foreign_home_leak(t, u1, az):
    """another user's calendar home leaks nothing"""
    r = u1.dav("PROPFIND", f"/dav/{az.u2}/calendars/", depth=1, xml=PROPFIND_LIST)
    t.not_contains(r, az.cal["path"], "U2's calendar path must not leak")
    t.not_contains(r, "u2-private-cal", "U2's calendar name must not leak")
    r = u1.dav("PROPFIND", f"/dav/{az.u2}/addressbooks/", depth=1, xml=PROPFIND_LIST)
    t.not_contains(r, az.ab["Path"])
    t.not_contains(r, "u2-private-book")


@known("F11")
def test_foreign_home_status(t, u1, az):
    """another user's calendar home is refused (403/404)"""
    # F11: 207 with an empty <multistatus/> (no data leaks — see test_foreign_home_leak).
    r = u1.dav("PROPFIND", f"/dav/{az.u2}/calendars/", depth=1, xml=PROPFIND_LIST)
    t.bug.status(r, 403, 404, was=207, msg="PROPFIND on another user's calendar home")
    r = u1.dav("PROPFIND", f"/dav/{az.u2}/addressbooks/", depth=1, xml=PROPFIND_LIST)
    t.bug.status(r, 403, 404, was=207, msg="PROPFIND on another user's address-book home")


def _multiget(href: str) -> str:
    return ('<?xml version="1.0" encoding="utf-8"?>\n'
            '<c:calendar-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-data/>'
            f"</d:prop><d:href>{href}</d:href></c:calendar-multiget>")


def test_foreign_collection(t, e2e, u1, az):
    """another user's calendar collection: PROPFIND / REPORT refused"""
    r = u1.dav("PROPFIND", az.col, depth=0, xml=PROPFIND_COLLECTION)
    t.status(r, 403, 404, msg="Depth 0")
    t.not_contains(r, "u2-private-cal")
    r = u1.dav("PROPFIND", az.col, depth=1, xml=PROPFIND_LIST)
    t.status(r, 403, 404, msg="Depth 1")
    t.not_contains(r, "secret")
    r = u1.dav("REPORT", az.col, depth=1, xml=sync_body())
    t.status_not(r, 200, 207, msg="sync-collection on U2's calendar")  # exact status: see test_sync_status
    t.not_contains(r, "secret")
    r = u1.dav("REPORT", az.col, depth=1, xml='<?xml version="1.0" encoding="utf-8"?>\n'
               '<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-data/>'
               '</d:prop><c:filter><c:comp-filter name="VCALENDAR"/></c:filter></c:calendar-query>')
    t.status(r, 403, 404, msg="calendar-query")
    t.not_contains(r, "U2 secret meeting")
    r = u1.dav("REPORT", az.col, depth=1, xml=_multiget(az.obj))
    t.not_contains(r, "U2 secret meeting", "calendar-multiget must not return U2's data")
    # A multiget against U1's OWN collection naming U2's object href.
    own = u1.mk_calendar(e2e.uniq("own"))
    r = u1.dav("REPORT", f"{u1.home_cal}{own['path']}/", depth=1, xml=_multiget(az.obj))
    t.not_contains(r, "U2 secret meeting", "a multiget through U1's own collection must not fetch U2's object")


def test_foreign_object(t, e2e, u1, u2, az):
    """another user's event: GET / PUT / DELETE refused, data intact"""
    r = u1.dav("GET", az.obj)
    t.status(r, 403, 404, msg="GET")
    t.not_contains(r, "U2 secret meeting")
    t.status(u1.dav("HEAD", az.obj), 403, 404, msg="HEAD")
    r = u1.dav("PUT", az.obj, ics=ics_event(az.ev_uid, "Overwritten by U1", "20300101T100000Z", "20300101T110000Z"))
    t.status(r, 403, 404, msg="PUT over U2's event")
    planted = f"{az.col}{e2e.uniq('planted')}.ics"
    r = u1.dav("PUT", planted,
               ics=ics_event(e2e.uniq("planted"), "Planted by U1", "20300101T100000Z", "20300101T110000Z"))
    t.status(r, 403, 404, msg="PUT a new event into U2's calendar")
    t.status(u1.dav("DELETE", az.obj), 403, 404, msg="DELETE U2's event")
    r = u2.dav("GET", az.obj)
    if not t.status(r, 200, msg="U2's event must still exist"):
        return
    t.contains(r, "U2 secret meeting", "U2's event must be unchanged")
    t.status(u2.dav("GET", planted), 404, msg="nothing may have been planted in U2's calendar")


def test_foreign_collection_writes(t, e2e, u1, u2, az):
    """another user's calendar: PROPPATCH / MKCALENDAR / DELETE refused"""
    r = u1.dav("PROPPATCH", az.col, xml='<?xml version="1.0" encoding="utf-8"?>\n'
               '<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>Hijacked</d:displayname></d:prop>'
               '</d:set></d:propertyupdate>')
    t.status(r, 403, 404, msg="PROPPATCH on U2's calendar")
    r = u1.dav("MKCALENDAR", f"/dav/{az.u2}/calendars/{e2e.uniq('planted-cal')}/",
               xml='<?xml version="1.0" encoding="utf-8"?><c:mkcalendar xmlns:d="DAV:" '
                   'xmlns:c="urn:ietf:params:xml:ns:caldav"/>')
    t.status(r, 403, 404, msg="MKCALENDAR in U2's home")
    t.status(u1.dav("DELETE", az.col), 403, 404, msg="DELETE U2's calendar")
    r = u2.api("GET", f"/calendars/{az.cal['uuid']}")
    if not t.status(r, 200, msg="U2's calendar must survive"):
        return
    t.json(r, "name", e2e.uniq("u2-private-cal"), "U2's calendar name must be unchanged")
    r = u2.api("GET", "/calendars/")
    t.not_contains(r, e2e.uniq("planted-cal"), "no calendar may have been created for U2")
    r = u1.api("GET", "/calendars/")
    body = r.json() if isinstance(r.json(), dict) else {}
    for c in body.get("calendars") or []:
        if c.get("path") == e2e.uniq("planted-cal"):
            t.fail(f"U1's MKCALENDAR in U2's home created a calendar for U1 ({c.get('uuid')})")
            u1.api("DELETE", f"/calendars/{c.get('uuid')}", json={"confirmation": "DELETE"})


def test_foreign_addressbook(t, e2e, u1, u2, az):
    """another user's address book and cards are refused"""
    r = u1.dav("PROPFIND", az.abcol, depth=1, xml=PROPFIND_LIST)
    t.status(r, 403, 404, msg="PROPFIND on U2's address book")
    t.not_contains(r, "secret-card")
    r = u1.dav("GET", az.card)
    t.status(r, 403, 404, msg="GET U2's card")
    t.not_contains(r, "Secret Person")
    r = u1.dav("PUT", az.card, vcf=vcard(e2e.uniq("secret-card"), "Overwritten Person"))
    t.status(r, 403, 404, msg="PUT over U2's card")
    t.status(u1.dav("DELETE", az.card), 403, 404, msg="DELETE U2's card")
    r = u1.dav("REPORT", az.abcol, depth=1, xml=sync_body())
    t.status_not(r, 200, 207, msg="sync on U2's address book")
    t.not_contains(r, "secret-card")
    t.status(u1.dav("DELETE", az.abcol), 403, 404, msg="DELETE U2's address book")
    r = u2.dav("GET", az.card)
    if not t.status(r, 200):
        return
    t.contains(r, "FN:Secret Person", "U2's card must be unchanged")


def test_traversal(t, e2e, u1, az):
    """path traversal out of the own home is refused"""
    own = u1.home_cal
    r = u1.dav("PROPFIND", f"{own}../../{az.u2}/calendars/{az.cal['path']}/", depth=1, xml=PROPFIND_LIST,
               path_as_is=True)
    t.status_class(r, "4xx", msg="dot-dot segments")
    t.not_contains(r, "secret")
    r = u1.dav("GET", f"{own}%2e%2e/%2e%2e/{az.u2}/calendars/{az.cal['path']}/{e2e.uniq('secret')}.ics",
               path_as_is=True)
    t.status_class(r, "4xx", msg="encoded dot-dot segments")
    t.not_contains(r, "U2 secret meeting")


@known("F17")
def test_sync_status(t, e2e, u1, az):
    """sync-collection on a missing / foreign collection is a 404, not 500"""
    # F17: 500 with the body "404 Not Found" (go-webdav's HTTPError handed to Fiber).
    r = u1.dav("REPORT", f"{u1.home_cal}no-such-calendar-{e2e.run_id}/", depth=1, xml=sync_body())
    t.bug.status(r, 403, 404, was=500, msg="sync-collection on a calendar that does not exist")
    t.bug.status(u1.dav("REPORT", az.col, depth=1, xml=sync_body()), 403, 404, was=500,
                 msg="sync-collection on another user's calendar")
    t.bug.status(u1.dav("REPORT", az.abcol, depth=1, xml=sync_body()), 403, 404, was=500,
                 msg="sync-collection on another user's address book")
