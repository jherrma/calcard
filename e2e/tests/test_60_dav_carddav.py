"""CardDAV (RFC 6352) in depth: address-book creation, vCard PUT/GET/DELETE with
ETag preconditions, UID uniqueness, comma lists, addressbook-query /
addressbook-multiget, PROPPATCH, REST <-> DAV consistency incl. photos."""

import re
from types import SimpleNamespace

import pytest

from harness import xml
from harness.data import PNG_PIXEL, PROPFIND_COLLECTION, PROPFIND_LIST, vcard
from harness.xml import el, lp

DESCRIPTION = "MKCOL address book, vCard PUT/GET/DELETE + ETags, REPORTs, PROPPATCH"

pytestmark = pytest.mark.usefixtures("u1")

CONFIRM = {"confirmation": "DELETE"}

MKCOL_BODY = '''<?xml version="1.0" encoding="utf-8"?>
<d:mkcol xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:set><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype><d:displayname>Made by MKCOL</d:displayname></d:prop></d:set></d:mkcol>'''


@pytest.fixture(scope="module", autouse=True)
def cc(e2e, u1):
    """Suite setup: a DAV app password and one address book."""
    u1.require_dav()
    ab = u1.mk_addressbook(e2e.uniq("carddav"))
    return SimpleNamespace(ab=ab, col=f"{u1.home_ab}{ab['Path']}/")


def _books_at(u1, path):
    """(REST response, address books whose Path is PATH)."""
    r = u1.api("GET", "/addressbooks/")
    books = [a for a in (r.jdict().get("addressbooks") or []) if a.get("Path") == path]
    return r, books


def _cleanup_path(e2e, u1, path):
    """Delete every REST address book at PATH later."""
    _, books = _books_at(u1, path)
    for b in books:
        e2e.defer(f"delete DAV-created address book {path}", e2e.cleanup_api, u1, "DELETE",
                  f"/addressbooks/{b['UUID']}", json=CONFIRM)


def test_mkcol(t, e2e, u1, st):
    """extended MKCOL creates an address book"""
    st.slug = e2e.uniq("book")
    r = u1.dav("MKCOL", f"{u1.home_ab}{st.slug}/", xml=MKCOL_BODY)
    if not t.status(r, 201):
        return
    _cleanup_path(e2e, u1, st.slug)
    _, books = _books_at(u1, st.slug)
    t.eq(len(books), 1, "exactly one address book at the new path")
    t.eq(books[0].get("Name") if books else "", "Made by MKCOL", "the displayname from MKCOL must be kept")
    r = u1.dav("PROPFIND", f"{u1.home_ab}{st.slug}/", depth=0, xml=PROPFIND_COLLECTION)
    if not t.status(r, 207):
        return
    t.xpath(r, lp("resourcetype", "addressbook"))


def test_mkcol_existing(t, e2e, u1, st):
    """MKCOL on an existing address book is refused (405)"""
    t.need(st, "slug")
    r = u1.dav("MKCOL", f"{u1.home_ab}{st.slug}/", xml=MKCOL_BODY)
    t.status(r, 405, msg="MKCOL on an existing address book (RFC 4918 9.3.1)")
    _, books = _books_at(u1, st.slug)
    t.true(len(books) == 1, f"no duplicate address book may be created at '{st.slug}' ({len(books)} there now)")
    if len(books) > 1:
        for b in books:
            e2e.defer(f"delete duplicate address book at {st.slug}", e2e.cleanup_api, u1, "DELETE",
                      f"/addressbooks/{b['UUID']}", json=CONFIRM)


def _mkcol_props(props):
    return ('<?xml version="1.0" encoding="utf-8"?>\n<d:mkcol xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">'
            "<d:set><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype>"
            f"{props}</d:prop></d:set></d:mkcol>")


def test_mkcol_description(t, e2e, u1):
    """extended MKCOL keeps the addressbook-description (DAVx5 payload)"""
    slug = e2e.uniq("descbook")
    r = u1.dav("MKCOL", f"{u1.home_ab}{slug}/", xml=_mkcol_props(
        "<d:displayname>Described</d:displayname><c:addressbook-description>from DAVx5</c:addressbook-description>"))
    if not t.status(r, 201):
        return
    _cleanup_path(e2e, u1, slug)
    _, books = _books_at(u1, slug)
    t.eq(books[0].get("Description") if books else None, "from DAVx5", "addressbook-description")


def test_mkcol_refused_property(t, e2e, u1):
    """extended MKCOL with a property the server cannot set creates nothing (403 mkcol-response)"""
    slug = e2e.uniq("refusedbook")
    r = u1.dav("MKCOL", f"{u1.home_ab}{slug}/", xml=_mkcol_props(
        '<d:displayname>never created</d:displayname><x:frobnicate xmlns:x="urn:x-e2e">1</x:frobnicate>'))
    _, books = _books_at(u1, slug)
    if books:
        _cleanup_path(e2e, u1, slug)
    t.status(r, 403, msg="RFC 5689 3: the whole MKCOL fails when a property cannot be set")
    t.xpath(r, f"{lp('mkcol-response', 'propstat')}[{el('status')}[contains(., '403')]]/{el('prop')}/{el('frobnicate')}")
    t.eq(len(books), 0, "the refused MKCOL must not create an address book")


def test_put_create(t, e2e, u1, cc, st):
    """PUT with If-None-Match: * creates (201 + ETag)"""
    st.uid = e2e.uniq("card1")
    st.obj = f"{cc.col}{st.uid}.vcf"
    r = u1.dav("PUT", st.obj, vcf=vcard(st.uid, "Charlie Davtest", "EMAIL;TYPE=work:charlie@example.test",
                                        "TEL;TYPE=cell:+49 30 555"),
               headers={"If-None-Match": "*"})
    if not t.status(r, 201):
        return
    st.etag = r.hdr("ETag")
    if not re.fullmatch(r'".+"', st.etag):
        t.fail(f"PUT must return a quoted ETag, got '{st.etag}'")


def test_put_create_again(t, u1, st):
    """PUT with If-None-Match: * on an existing card is 412"""
    t.need(st, "obj")
    r = u1.dav("PUT", st.obj, vcf=vcard(st.uid, "Clobber Card"), headers={"If-None-Match": "*"})
    t.status(r, 412)


def test_get(t, u1, st):
    """GET returns the vCard with the same ETag"""
    t.need(st, "obj")
    r = u1.dav("GET", st.obj)
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "text/vcard")
    t.header_eq(r, "ETag", st.etag)
    t.contains(r, f"UID:{st.uid}")
    t.contains(r, "FN:Charlie Davtest")
    t.contains(r, "charlie@example.test")


def test_if_match(t, u1, st):
    """PUT with matching If-Match updates; stale If-Match is 412"""
    t.need(st, "etag")
    old = st.etag
    try:
        r = u1.dav("PUT", st.obj, vcf=vcard(st.uid, "Charlie Davtest v2"), headers={"If-Match": old})
        if not t.status(r, 200, 201, 204):
            return
        r = u1.dav("GET", st.obj)
        t.contains(r, "FN:Charlie Davtest v2")
        t.ne(r.hdr("ETag"), old, "the ETag must change")
        r = u1.dav("PUT", st.obj, vcf=vcard(st.uid, "Stale Write"), headers={"If-Match": old})
        if not t.status(r, 412):
            return
        r = u1.dav("GET", st.obj)
        t.not_contains(r, "Stale Write", "a rejected write must not change the card")
    finally:
        # Whatever happened above, later tests condition on the card's CURRENT
        # ETag — a stale one would add a second, misleading 412 failure.
        st.etag = u1.dav("GET", st.obj).hdr("ETag") or old


def test_uid_conflict(t, e2e, u1, cc, st):
    """same UID at a second path is a 409"""
    t.need(st, "uid")
    other = f"{cc.col}{e2e.uniq('other')}.vcf"
    if not t.status(u1.dav("PUT", other, vcf=vcard(st.uid, "Same UID")), 409):
        return
    t.status(u1.dav("GET", other), 404)


def test_put_invalid(t, e2e, u1, cc):
    """PUT rejects wrong media type and malformed vCards"""
    b1, b2 = e2e.uniq("bad1"), e2e.uniq("bad2")
    r = u1.dav("PUT", f"{cc.col}{b1}.vcf", data=vcard(b1, "Bad Type"), ctype="application/json")
    t.status_class(r, "4xx", msg="non-vCard media type")
    r = u1.dav("PUT", f"{cc.col}{b2}.vcf", data="not a vcard at all", ctype="text/vcard")
    t.status(r, 400, msg="malformed vCard")
    t.status(u1.dav("GET", f"{cc.col}{b2}.vcf"), 404)


def test_categories(t, e2e, u1, cc):
    """comma lists (CATEGORIES) survive the round trip"""
    uid = e2e.uniq("cats")
    r = u1.dav("PUT", f"{cc.col}{uid}.vcf", vcf=vcard(uid, "Cat Egories", "CATEGORIES:Work,Important"))
    if not t.status(r, 201, 204):
        return
    r = u1.dav("GET", f"{cc.col}{uid}.vcf")
    if not t.status(r, 200):
        return
    t.not_contains(r, "Work\\,Important", "a comma-separated list must not come back escaped into one value")
    # One property with a list, or one property per value — both mean the same.
    body = r.text
    if not ("CATEGORIES:Work,Important" in body
            or ("CATEGORIES:Work\r" in body and "CATEGORIES:Important\r" in body)):
        t.fail("categories Work and Important must both survive", r)


def test_vcard4(t, e2e, u1, cc):
    """vCard 4.0 is accepted"""
    uid = e2e.uniq("v4")
    card = (f"BEGIN:VCARD\r\nVERSION:4.0\r\nUID:{uid}\r\nFN:Vera Four\r\nN:Four;Vera;;;\r\n"
            "EMAIL;TYPE=home:vera@example.test\r\nEND:VCARD\r\n")
    if not t.status(u1.dav("PUT", f"{cc.col}{uid}.vcf", vcf=card), 201, 204):
        return
    r = u1.dav("GET", f"{cc.col}{uid}.vcf")
    if not t.status(r, 200):
        return
    t.contains(r, "FN:Vera Four")


def test_query(t, e2e, u1, cc):
    """addressbook-query filters on FN"""
    for n in ("Alice Testerson", "Bob Testerson", "Carl Unrelated"):
        uid = e2e.uniq(f"q-{n.split(' ')[0]}")
        if not t.status(u1.dav("PUT", f"{cc.col}{uid}.vcf", vcf=vcard(uid, n)), 201, 204):
            return
    r = u1.dav("REPORT", cc.col, depth=1, xml='''<?xml version="1.0" encoding="utf-8"?>
<c:addressbook-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">
<d:prop><d:getetag/><c:address-data/></d:prop>
<c:filter><c:prop-filter name="FN"><c:text-match collation="i;unicode-casemap" match-type="contains">testerson</c:text-match></c:prop-filter></c:filter>
</c:addressbook-query>''')
    if not t.status(r, 207):
        return
    t.contains(r, e2e.uniq("q-Alice"))
    t.contains(r, e2e.uniq("q-Bob"))
    t.not_contains(r, e2e.uniq("q-Carl"), "a non-matching card must not be returned")
    t.xpath(r, lp("response", "propstat", "prop", "address-data"), "address-data must be returned when requested")


def test_multiget(t, e2e, u1, cc, st):
    """addressbook-multiget returns requested cards, 404 for missing"""
    t.need(st, "obj")
    missing, other = f"{cc.col}{e2e.uniq('missing')}.vcf", f"{cc.col}{e2e.uniq('q-Bob')}.vcf"
    r = u1.dav("REPORT", cc.col, depth=1, xml=f'''<?xml version="1.0" encoding="utf-8"?>
<c:addressbook-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">
<d:prop><d:getetag/><c:address-data/></d:prop>
<d:href>{st.obj}</d:href><d:href>{other}</d:href><d:href>{missing}</d:href></c:addressbook-multiget>''')
    if not t.status(r, 207):
        return
    t.xpath(r, f"{lp('response')}[{el('href')}='{st.obj}']//{el('address-data')}[contains(.,'Charlie Davtest v2')]")
    t.xpath(r, f"{lp('response')}[{el('href')}='{other}']//{el('address-data')}[contains(.,'Bob Testerson')]")
    t.xpath(r, f"{lp('response')}[{el('href')}='{missing}']//{el('status')}[contains(.,'404')]",
            "a missing href is a 404 inside the multistatus")


def test_rest_crosscheck(t, e2e, u1, cc, st):
    """cards PUT over DAV appear in REST, and vice versa"""
    t.need(st, "uid")
    uid0 = st.uid
    r = u1.api("GET", f"/addressbooks/{cc.ab['UUID']}/contacts?limit=200")
    if not t.jq(r, lambda j: len([c for c in j["Contacts"] if c["uid"] == uid0
                                  and c["formatted_name"] == "Charlie Davtest v2"]) == 1,
                "a card PUT over DAV must appear in REST"):
        return
    ct = u1.mk_contact(cc.ab["UUID"], f"Rest Made {e2e.run_id}", given_name="Rest", family_name="Made")
    uid = ct.get("uid")
    st.rest_id = ct.get("id")
    r = u1.dav("PROPFIND", cc.col, depth=1, xml=PROPFIND_LIST)
    st.rest_href = xml.xstr(r, f"({lp('response', 'href')})[contains(.,'{uid}')]")
    if not st.rest_href:
        return t.fail(f"the REST-created contact (uid {uid}) is not listed over CardDAV", r)
    r = u1.dav("GET", st.rest_href)
    if not t.status(r, 200):
        return
    t.contains(r, f"FN:Rest Made {e2e.run_id}")


def test_photo(t, u1, cc, st):
    """a photo uploaded over REST is served in the DAV vCard"""
    t.need(st, "rest_id", "rest_href")
    r = u1.api("PUT", f"/addressbooks/{cc.ab['UUID']}/contacts/{st.rest_id}/photo", data=PNG_PIXEL, ctype="image/png")
    if not t.status(r, 204, 200):
        return
    r = u1.dav("GET", st.rest_href)
    if not t.status(r, 200):
        return
    t.match(r, "PHOTO[;:]", "the vCard served over CardDAV must carry the PHOTO")


def test_proppatch(t, u1, cc):
    """PROPPATCH renames the address book"""
    r = u1.dav("PROPPATCH", cc.col, xml='''<?xml version="1.0" encoding="utf-8"?>
<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>Book renamed over DAV</d:displayname></d:prop></d:set></d:propertyupdate>''')
    if not t.status(r, 207):
        return
    r = u1.api("GET", f"/addressbooks/{cc.ab['UUID']}")
    t.json(r, "Name", "Book renamed over DAV")


def test_delete(t, u1, cc, st):
    """DELETE with stale If-Match is 412; DELETE removes the card"""
    t.need(st, "obj")
    if not t.status(u1.dav("DELETE", st.obj, headers={"If-Match": '"stale"'}), 412):
        return
    if not t.status(u1.dav("DELETE", st.obj, headers={"If-Match": st.etag}), 204, 200):
        return
    t.status(u1.dav("GET", st.obj), 404)
    uid = st.uid
    r = u1.api("GET", f"/addressbooks/{cc.ab['UUID']}/contacts?limit=200")
    t.jq(r, lambda j: len([c for c in j["Contacts"] or [] if c["uid"] == uid]) == 0, "a DAV delete removes the REST contact")


def test_delete_collection(t, e2e, u1):
    """DELETE on the collection removes the address book"""
    slug = e2e.uniq("doomed-book")
    if not t.status(u1.dav("MKCOL", f"{u1.home_ab}{slug}/", xml=MKCOL_BODY), 201):
        return
    _cleanup_path(e2e, u1, slug)
    u1.dav("PUT", f"{u1.home_ab}{slug}/x.vcf", vcf=vcard(e2e.uniq("doomed-card"), "Doomed Card"))
    if not t.status(u1.dav("DELETE", f"{u1.home_ab}{slug}/"), 204, 200):
        return
    _, books = _books_at(u1, slug)
    t.eq(len(books), 0, "the address book must be gone from REST")
    t.status(u1.dav("PROPFIND", f"{u1.home_ab}{slug}/", depth=0, xml=PROPFIND_LIST), 404)
