"""WebDAV-Sync (RFC 6578) for calendars and address books: initial sync,
token progression, deltas for create / modify / delete, invalid tokens,
truncation with <limit>, ctag changes and REST changes showing up in DAV."""

import pytest

from harness.data import PROPFIND_COLLECTION, ics_event, sync_body, vcard
from harness.session import SetupFailed
from harness.xml import el, lp, xcount, xml_text, xstr

DESCRIPTION = "sync-collection REPORT: tokens, deltas, deletions, limits, ctag"

pytestmark = pytest.mark.usefixtures("u1")


@pytest.fixture(scope="module", autouse=True)
def sy_setup(e2e, u1, st):
    u1.require_dav()
    st.cal = u1.mk_calendar(e2e.uniq("sync-cal"))
    st.ab = u1.mk_addressbook(e2e.uniq("sync-book"))
    st.col = f"{u1.home_cal}{st.cal['path']}/"
    st.abcol = f"{u1.home_ab}{st.ab['Path']}/"


def _sync(u1, col, token="", extra=""):
    """Run a sync REPORT → (response, new sync-token or '')."""
    r = u1.dav("REPORT", col, depth=1, xml=sync_body(token, extra))
    return r, (xml_text(r, "sync-token") if r.status == 207 else "")


def _put_ev(u1, st, name, summary):
    return u1.dav("PUT", f"{st.col}{name}.ics",
                  ics=ics_event(f"{name}@e2e", summary, "20300901T100000Z", "20300901T110000Z"))


def _resp_count(r):
    return xcount(r, lp("response"))


def test_cal_initial(t, e2e, u1, st):
    """CalDAV: initial sync returns a token and every member"""
    seed = e2e.uniq("s-seed")
    if not t.status(_put_ev(u1, st, seed, "Seed"), 201, 204):
        return
    r, st.t0 = _sync(u1, st.col)
    if not t.status(r, 207):
        return
    if not st.t0:
        t.fail("the initial sync must return a sync-token", r)
    t.xpath(r, f"{lp('response', 'href')}[contains(.,'{seed}.ics')]", "the seeded object is listed")
    t.xpath(r, f"{lp('response')}[contains({el('href')},'{seed}.ics')]//{el('getetag')}", "with its ETag")


def test_cal_nochange(t, u1, st):
    """CalDAV: no changes → empty delta"""
    t.need(st, "t0")
    r, new = _sync(u1, st.col, st.t0)
    if not t.status(r, 207):
        return
    t.eq(_resp_count(r), 0, "nothing changed, so the delta must be empty")
    if not new:
        t.fail("an empty delta still carries a sync-token")


def test_cal_create(t, e2e, u1, st):
    """CalDAV: a new object is the only item in the delta"""
    t.need(st, "t0")
    _put_ev(u1, st, e2e.uniq("s-new"), "New")
    r, st.t1 = _sync(u1, st.col, st.t0)
    if not t.status(r, 207):
        return
    t.contains(r, f"{e2e.uniq('s-new')}.ics", "the delta must carry the new object")
    t.not_contains(r, f"{e2e.uniq('s-seed')}.ics", "the delta must not re-deliver unchanged objects")
    t.ne(st.t1, st.t0, "the sync-token must advance after a change")


def test_cal_modify(t, e2e, u1, st):
    """CalDAV: a modified object comes back with its new ETag"""
    t.need(st, "t1")
    old_etag = u1.dav("GET", f"{st.col}{e2e.uniq('s-seed')}.ics").hdr("ETag")
    _put_ev(u1, st, e2e.uniq("s-seed"), "Seed (modified)")
    r, new = _sync(u1, st.col, st.t1)
    if not t.status(r, 207):
        return
    t.eq(_resp_count(r), 1, "exactly the modified object")
    t.ne(xstr(r, lp("response", "getetag")), old_etag, "the delta must carry the new ETag")
    st.t2 = new


def test_cal_delete(t, e2e, u1, st):
    """CalDAV: a deleted object is reported 404"""
    t.need(st, "t2")
    if not t.status(u1.dav("DELETE", f"{st.col}{e2e.uniq('s-new')}.ics"), 204):
        return
    r, new = _sync(u1, st.col, st.t2)
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response')}[contains({el('href')},'{e2e.uniq('s-new')}.ics')]/{el('status')}"
               "[contains(.,'404')]",
            "a deleted member is reported with status 404 (RFC 6578 3.5.2)")
    st.t3 = new


def test_cal_rest_change(t, e2e, u1, st):
    """CalDAV: a REST change shows up in the DAV delta"""
    t.need(st, "t3")
    try:
        u1.mk_event(st.cal["uuid"], f"From REST {e2e.run_id}", "2030-09-02T10:00:00Z", "2030-09-02T11:00:00Z")
    except SetupFailed as e:
        return t.fail("REST create failed", e.resp)
    r, _ = _sync(u1, st.col, st.t3)
    if not t.status(r, 207):
        return
    if not t.eq(_resp_count(r), 1, "the REST-created event must be the delta"):
        return
    r = u1.dav("GET", xstr(r, lp("response", "href")))
    t.contains(r, f"SUMMARY:From REST {e2e.run_id}")


def test_cal_invalid_token(t, e2e, u1, st):
    """CalDAV: unknown sync-token is 403 valid-sync-token"""
    r, _ = _sync(u1, st.col, f"data:,not-a-token-{e2e.run_id}")
    if not t.status(r, 403):
        return
    t.xpath(r, lp("error", "valid-sync-token"), "the error body must name DAV:valid-sync-token")
    r, _ = _sync(u1, st.col, "http://example.com/foreign-token")
    t.status(r, 403)


def test_cal_limit(t, e2e, u1, st):
    """CalDAV: <limit> truncates with 507 and resumes"""
    _, base = _sync(u1, st.col)
    if not base:
        t.skip("precondition not met: base is empty (an earlier step failed)")
    for i in (1, 2, 3):
        _put_ev(u1, st, e2e.uniq(f"s-lim{i}"), f"Limit {i}")
    r, first = _sync(u1, st.col, base, "<d:limit><d:nresults>2</d:nresults></d:limit>")
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response', 'status')}[contains(.,'507')]",
            "a truncated result ends with a 507 response for the collection")
    r, _ = _sync(u1, st.col, first)
    if not t.status(r, 207):
        return
    t.contains(r, f"{e2e.uniq('s-lim3')}.ics", "the follow-up sync must deliver the remainder")
    t.not_contains(r, f"{e2e.uniq('s-lim1')}.ics", "and not repeat what was already delivered")


def test_cal_ctag(t, e2e, u1, st):
    """CalDAV: getctag and the sync-token property change with content"""
    r = u1.dav("PROPFIND", st.col, depth=0, xml=PROPFIND_COLLECTION)
    ctag1, tok1 = xml_text(r, "getctag"), xml_text(r, "sync-token")
    _put_ev(u1, st, e2e.uniq("s-ctag"), "Ctag bump")
    r = u1.dav("PROPFIND", st.col, depth=0, xml=PROPFIND_COLLECTION)
    if not t.status(r, 207):
        return
    t.ne(xml_text(r, "getctag"), ctag1, "getctag must change when the collection changes")
    t.ne(xml_text(r, "sync-token"), tok1, "the sync-token property must change too")
    prop_tok = xml_text(r, "sync-token")
    _, new = _sync(u1, st.col)
    t.eq(new, prop_tok, "REPORT and PROPFIND must agree on the current token")


def test_ab_cycle(t, e2e, u1, st):
    """CardDAV: initial sync, create / modify / delete deltas"""
    a, b = e2e.uniq("sa-1"), e2e.uniq("sa-2")
    if not t.status(u1.dav("PUT", f"{st.abcol}{a}.vcf", vcf=vcard(a, "Sync First")), 201, 204):
        return
    r, t0 = _sync(u1, st.abcol)
    if not t.status(r, 207):
        return
    if not t0:
        t.fail("initial CardDAV sync must return a token", r)
    t.contains(r, f"{a}.vcf")
    u1.dav("PUT", f"{st.abcol}{b}.vcf", vcf=vcard(b, "Sync Second"))
    r, t1 = _sync(u1, st.abcol, t0)
    if not t.status(r, 207):
        return
    t.contains(r, f"{b}.vcf", "create delta")
    t.not_contains(r, f"{a}.vcf", "unchanged card must not be re-delivered")
    u1.dav("PUT", f"{st.abcol}{a}.vcf", vcf=vcard(a, "Sync First Modified"))
    r, t2 = _sync(u1, st.abcol, t1)
    t.contains(r, f"{a}.vcf", "modify delta")
    t.not_contains(r, f"{b}.vcf")
    u1.dav("DELETE", f"{st.abcol}{b}.vcf")
    r, t3 = _sync(u1, st.abcol, t2)
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response')}[contains({el('href')},'{b}.vcf')]/{el('status')}[contains(.,'404')]",
            "deleted card reported 404")
    u1.mk_contact(st.ab["UUID"], f"Sync From REST {e2e.run_id}")
    r, _ = _sync(u1, st.abcol, t3)
    t.eq(_resp_count(r), 1, "a REST-created contact must be the CardDAV delta")


def test_ab_invalid_token(t, e2e, u1, st):
    """CardDAV: unknown sync-token is 403 valid-sync-token"""
    r, _ = _sync(u1, st.abcol, f"data:,bogus-{e2e.run_id}")
    if not t.status(r, 403):
        return
    t.xpath(r, lp("error", "valid-sync-token"))
