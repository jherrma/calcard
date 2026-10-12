"""DAV discovery (RFC 6764 / 4791 / 6352 / 4918): well-known redirects,
OPTIONS capabilities, principal and home-set PROPFINDs, collection
properties at Depth 0 and 1."""

import re
from types import SimpleNamespace

import pytest

from harness import xml
from harness.checks import known
from harness.data import PROPFIND_COLLECTION, PROPFIND_LIST, PROPFIND_PRINCIPAL
from harness.xml import el, lp

DESCRIPTION = "well-known redirects, OPTIONS, principal / home-set / collection PROPFIND"

pytestmark = pytest.mark.usefixtures("u1")


@pytest.fixture(scope="module", autouse=True)
def dd(e2e, u1):
    """Suite setup: a DAV app password, one calendar, one address book."""
    u1.require_dav()
    cal = u1.mk_calendar(e2e.uniq("disco-cal"))
    ab = u1.mk_addressbook(e2e.uniq("disco-book"))
    return SimpleNamespace(cal=cal, ab=ab, user=u1.username)


def test_wellknown_get(t, e2e):
    """GET /.well-known/caldav and /carddav redirect to /dav/ (301)"""
    for p in ("/.well-known/caldav", "/.well-known/carddav"):
        r = e2e.raw("GET", p)
        if not t.status(r, 301, msg=p):
            continue
        t.header_eq(r, "Location", "/dav/", f"{p} must redirect to /dav/")


def test_wellknown_propfind(t, e2e):
    """PROPFIND on /.well-known/* redirects too (iOS/macOS discovery)"""
    for p in ("/.well-known/caldav", "/.well-known/carddav"):
        r = e2e.raw("PROPFIND", p, depth=0, xml=PROPFIND_PRINCIPAL)
        if not t.status(r, 301, msg=f"PROPFIND {p}"):
            continue
        t.header_eq(r, "Location", "/dav/")


def test_options_root(t, u1):
    """OPTIONS /dav/ advertises DAV 1, 3, calendar-access, addressbook"""
    r = u1.dav("OPTIONS", "/dav/")
    if not t.status(r, 200, 204):
        return
    h = r.hdr("DAV")
    if not re.search(r"(^|[ ,])1([ ,]|$)", h):
        t.fail(f"DAV header must advertise class 1, got '{h}'")
    if not re.search(r"(^|[ ,])3([ ,]|$)", h):
        t.fail(f"DAV header must advertise class 3, got '{h}'")
    if "calendar-access" not in h:
        t.fail(f"DAV header must advertise calendar-access (CalDAV), got '{h}'")
    if "addressbook" not in h:
        t.fail(f"DAV header must advertise addressbook (CardDAV), got '{h}'")
    t.header(r, "Allow", "PROPFIND")


def test_options_collection(t, u1, dd):
    """OPTIONS on a calendar collection lists the allowed methods"""
    r = u1.dav("OPTIONS", f"{u1.home_cal}{dd.cal['path']}/")
    if not t.status(r, 200, 204):
        return
    t.header(r, "DAV", "calendar-access")
    t.header(r, "Allow", "PROPFIND")
    t.header(r, "Allow", "REPORT")


def _principal_checks(t, r, user):
    """Shared assertions on a principal PROPFIND response."""
    t.xml_wellformed(r)
    t.xpath(r, f"{lp('response', 'propstat', 'prop', 'current-user-principal', 'href')}"
               f"[normalize-space()='/dav/{user}/']",
            f"current-user-principal must be /dav/{user}/")
    t.xpath(r, f"{lp('calendar-home-set', 'href')}[normalize-space()='/dav/{user}/calendars/']",
            f"calendar-home-set must be /dav/{user}/calendars/")
    t.xpath(r, f"{lp('addressbook-home-set', 'href')}[normalize-space()='/dav/{user}/addressbooks/']",
            f"addressbook-home-set must be /dav/{user}/addressbooks/")
    t.contains(r, f"/dav/{user}/calendars/")
    t.contains(r, f"/dav/{user}/addressbooks/")


def test_root(t, u1, dd):
    """PROPFIND /dav/ (Depth 0): current-user-principal and both home sets"""
    r = u1.dav("PROPFIND", "/dav/", depth=0, xml=PROPFIND_PRINCIPAL)
    if not t.status(r, 207):
        return
    _principal_checks(t, r, dd.user)


def test_principal(t, u1, dd):
    """PROPFIND principal: calendar-home-set and addressbook-home-set"""
    r = u1.dav("PROPFIND", f"/dav/{dd.user}/", depth=0, xml=PROPFIND_PRINCIPAL)
    if not t.status(r, 207):
        return
    _principal_checks(t, r, dd.user)
    t.xpath(r, f"{lp('response', 'href')}[normalize-space()='/dav/{dd.user}/']",
            "the response must describe the principal URL")


def test_empty_body(t, u1, dd):
    """PROPFIND with an empty body is treated as allprop (RFC 4918 9.1)"""
    t.status(u1.dav("PROPFIND", f"{u1.home_cal}{dd.cal['path']}/", depth=0), 207,
             msg="empty-body PROPFIND on a calendar collection")
    # Was a 400 "webdav: expected application/xml request" until F06 was fixed.
    for p, what in (("/dav/", "/dav/ (some clients probe like this)"), (f"/dav/{dd.user}/", "the principal")):
        r = u1.dav("PROPFIND", p, depth=0)
        if t.status(r, 207, msg=f"empty-body PROPFIND on {what}"):
            _principal_checks(t, r, dd.user)


def test_cal_home(t, e2e, u1, dd):
    """calendar home (Depth 1) lists the calendars as calendar collections"""
    r = u1.dav("PROPFIND", u1.home_cal, depth=1, xml='''<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:resourcetype/><d:displayname/><c:supported-calendar-component-set/></d:prop></d:propfind>''')
    if not t.status(r, 207):
        return
    t.xml_wellformed(r)
    href = f"{u1.home_cal}{dd.cal['path']}/"
    t.xpath(r, f"{lp('response', 'href')}[normalize-space()='{href}']", "the home listing must include the new calendar")
    t.xpath(r, f"{lp('response')}[{el('href')}='{href}']//*[local-name()='resourcetype']/*[local-name()='calendar']",
            "the calendar must have resourcetype calendar")
    t.xpath(r, f"{lp('response')}[{el('href')}='{href}']//*[local-name()='displayname'][.='{e2e.uniq('disco-cal')}']",
            "displayname must be the calendar name")
    t.xpath(r, f"{lp('response')}[{el('href')}='{href}']//*[local-name()='comp'][@name='VEVENT']",
            "supported-calendar-component-set must contain VEVENT")
    t.xpath(r, f"count({lp('response')}) >= 3", "home + Personal + the new calendar")


def test_cal_home_depth0(t, u1, dd):
    """calendar home (Depth 0) describes only the home"""
    r = u1.dav("PROPFIND", u1.home_cal, depth=0, xml=PROPFIND_LIST)
    if not t.status(r, 207):
        return
    t.xpath_count(r, lp("response"), 1, "Depth 0 must describe the home only")
    t.not_contains(r, dd.cal["path"])


def test_cal_collection(t, e2e, u1, dd):
    """calendar collection (Depth 0): resourcetype, sync-token, ctag, reports, privileges"""
    r = u1.dav("PROPFIND", f"{u1.home_cal}{dd.cal['path']}/", depth=0, xml=PROPFIND_COLLECTION)
    if not t.status(r, 207):
        return
    t.xml_wellformed(r)
    t.xpath(r, lp("resourcetype", "collection"), "resourcetype must include DAV:collection")
    t.xpath(r, lp("resourcetype", "calendar"), "resourcetype must include CALDAV:calendar")
    t.xpath(r, f"{lp('displayname')}[.='{e2e.uniq('disco-cal')}']")
    if not xml.xml_text(r, "sync-token"):
        t.fail("sync-token must be present", r)
    if not xml.xml_text(r, "getctag"):
        t.fail("getctag must be present", r)
    t.xpath(r, lp("supported-report-set", "**", "sync-collection"), "sync-collection REPORT advertised")
    t.xpath(r, lp("supported-report-set", "**", "calendar-multiget"), "calendar-multiget REPORT advertised")
    t.xpath(r, lp("supported-report-set", "**", "calendar-query"), "calendar-query REPORT advertised")
    t.xpath(r, lp("current-user-privilege-set", "**", "write"), "the owner has write privilege")


def test_depth_consistency(t, u1, dd):
    """calendar collection properties agree between Depth 0 and Depth 1"""
    body = '''<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/">
<d:prop><d:displayname/><c:calendar-description/><c:supported-calendar-component-set/><d:sync-token/><cs:getctag/></d:prop></d:propfind>'''
    href = f"{u1.home_cal}{dd.cal['path']}/"
    r0 = u1.dav("PROPFIND", href, depth=0, xml=body)
    if not t.status(r0, 207):
        return
    r1 = u1.dav("PROPFIND", u1.home_cal, depth=1, xml=body)
    if not t.status(r1, 207):
        return
    # The same resource must expose the same properties whatever the Depth.
    for p in ("calendar-description", "supported-calendar-component-set", "sync-token", "getctag"):
        ok0 = xml.xbool(r0, f"{lp('response', 'propstat')}[contains(*[local-name()='status'],'200')]"
                            f"//*[local-name()='{p}']")
        ok1 = xml.xbool(r1, f"{lp('response')}[*[local-name()='href']='{href}']/*[local-name()='propstat']"
                            f"[contains(*[local-name()='status'],'200')]//*[local-name()='{p}']")
        # Fixed in F07: each Depth used to leave some of them out.
        t.true(ok0, f"Depth 0 PROPFIND on the collection does not return {p} (200)", r0)
        t.true(ok1, f"Depth 1 PROPFIND on the home does not return {p} (200) for the same collection", r1)


def test_ab_home(t, e2e, u1, dd):
    """address-book home (Depth 1) lists the books as addressbook collections"""
    r = u1.dav("PROPFIND", u1.home_ab, depth=1, xml='''<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:displayname/></d:prop></d:propfind>''')
    if not t.status(r, 207):
        return
    href = f"{u1.home_ab}{dd.ab['Path']}/"
    t.xpath(r, f"{lp('response', 'href')}[normalize-space()='{href}']", "the home listing must include the new book")
    t.xpath(r, f"{lp('response')}[{el('href')}='{href}']//*[local-name()='resourcetype']/*[local-name()='addressbook']",
            "the book must have resourcetype addressbook")
    t.xpath(r, f"{lp('response')}[{el('href')}='{href}']//*[local-name()='displayname'][.='{e2e.uniq('disco-book')}']")


def test_ab_collection(t, u1, dd):
    """address-book collection (Depth 0): resourcetype, sync-token, reports"""
    r = u1.dav("PROPFIND", f"{u1.home_ab}{dd.ab['Path']}/", depth=0, xml=PROPFIND_COLLECTION)
    if not t.status(r, 207):
        return
    t.xpath(r, lp("resourcetype", "addressbook"), "resourcetype must include CARDDAV:addressbook")
    if not xml.xml_text(r, "sync-token"):
        t.fail("sync-token must be present", r)
    t.xpath(r, lp("supported-report-set", "**", "addressbook-multiget"))
    t.xpath(r, lp("supported-report-set", "**", "addressbook-query"))
    t.xpath(r, lp("supported-report-set", "**", "sync-collection"))


def test_without_slash(t, u1, dd):
    """a home set or collection named without its trailing slash is served as the collection (RFC 4918 5.2)"""
    for home, member in ((u1.home_cal, f"{u1.home_cal}{dd.cal['path']}/"),
                         (u1.home_ab, f"{u1.home_ab}{dd.ab['Path']}/")):
        r = u1.dav("PROPFIND", home.rstrip("/"), depth=1, xml=PROPFIND_LIST)
        if not t.status(r, 207, msg=f"PROPFIND {home.rstrip('/')}"):
            continue
        t.xpath(r, f"{lp('response', 'href')}[normalize-space()='{home}']",
                f"the home must be described as {home} (it was an empty multistatus)")
        t.xpath(r, f"{lp('response', 'href')}[normalize-space()='{member}']", "the listing must include the collection")
        t.header_eq(r, "Content-Location", home, "Content-Location names the URL with the slash")
    col = f"{u1.home_cal}{dd.cal['path']}/"
    for depth in (0, 1):
        r = u1.dav("PROPFIND", col.rstrip("/"), depth=depth, xml=PROPFIND_LIST)
        if t.status(r, 207, msg=f"Depth {depth} PROPFIND on the collection without its slash"):
            t.xpath(r, f"{lp('response', 'href')}[normalize-space()='{col}']",
                    f"Depth {depth} must describe the collection under its slash URL")


def test_path_spellings(t, u1, dd):
    """doubled slashes, a percent-encoded segment and /DAV/ name the same home"""
    member = f"{u1.home_cal}{dd.cal['path']}/"
    for p in (f"/dav/{dd.user}//calendars/", f"/dav//{dd.user}/calendars/", f"/dav/{dd.user}/calendars//",
              f"/dav/{dd.user}/%63alendars/", f"/DAV/{dd.user}/calendars/"):
        r = u1.dav("PROPFIND", p, depth=1, xml=PROPFIND_LIST, path_as_is=True)
        if t.status(r, 207, msg=f"PROPFIND {p}"):
            t.xpath(r, f"{lp('response', 'href')}[normalize-space()='{member}']", f"{p} must list the calendars")
    # Path segments other than the /dav mount stay case-sensitive.
    t.status(u1.dav("PROPFIND", f"/dav/{dd.user}/Calendars/", depth=1, xml=PROPFIND_LIST), 404)


def test_principal_other_name(t, e2e, u1, u2, dd):
    """a principal URL with another user's name reveals nothing about that account"""
    # Kept lenient on purpose: any /dav/{x}/ answers with the caller's own
    # principal, so a client configured with a slightly different URL still
    # finds its home sets. The name is never looked up, so an existing and a
    # non-existing account must be indistinguishable.
    other = u1.dav("PROPFIND", f"/dav/{u2.username}/", depth=0, xml=PROPFIND_PRINCIPAL)
    nobody = u1.dav("PROPFIND", f"/dav/nobody-{e2e.run_id}/", depth=0, xml=PROPFIND_PRINCIPAL)
    t.eq(other.status, nobody.status, "an existing and an unknown name must get the same status")
    for r in (other, nobody):
        if not t.status(r, 207):
            continue
        _principal_checks(t, r, dd.user)
        t.not_contains(r, f"/dav/{u2.username}/calendars/", "no home set of the other account")
        t.not_contains(r, f"/dav/{u2.username}/addressbooks/", "no home set of the other account")


def test_unknown(t, e2e, u1):
    """unknown DAV paths are 404, not 5xx"""
    rid = e2e.run_id
    t.status(u1.dav("PROPFIND", f"{u1.home_cal}no-such-calendar-{rid}/", depth=0, xml=PROPFIND_LIST), 404)
    t.status(u1.dav("GET", f"{u1.home_cal}no-such-calendar-{rid}/x.ics"), 404)
    t.status(u1.dav("PROPFIND", f"{u1.home_ab}no-such-book-{rid}/", depth=0, xml=PROPFIND_LIST), 404)
    t.status(u1.dav("PROPFIND", f"/dav/{u1.username}/no-such-home-{rid}/", depth=1, xml=PROPFIND_LIST), 404,
             msg="a path under the principal that is no home set")
