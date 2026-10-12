"""Address books and contacts over REST: CRUD (raw JSON, PascalCase address
books), paging, photos, moving between books, contact search, isolation and
the "cannot delete your last address book" guard."""

import re

import pytest

from harness.data import PNG_PIXEL
from harness.session import ZERO_UUID, SetupFailed

DESCRIPTION = "address book + contact CRUD, paging, photo, move, contact search"

pytestmark = pytest.mark.usefixtures("u1", "u2")

CONFIRM = {"confirmation": "DELETE"}


def test_ab_create(t, e2e, u1, st):
    """address book create (201, raw JSON)"""
    st.ab1_name = e2e.uniq("Colleagues")
    r = u1.api("POST", "/addressbooks/", json={"name": st.ab1_name, "description": "Work contacts"})
    if not t.status(r, 201):
        return
    j = r.json()
    st.ab1_uuid, st.ab1_id, st.ab1_path = j.get("UUID"), j.get("ID"), j.get("Path")
    e2e.defer(f"delete address book {st.ab1_name}", e2e.cleanup_api, u1, "DELETE",
              f"/addressbooks/{st.ab1_uuid}", json=CONFIRM)
    t.json(r, "Name", st.ab1_name)
    t.json(r, "Description", "Work contacts")
    t.jq(r, lambda j: j["ID"] > 0 and re.fullmatch(r"[0-9a-f-]{36}", j["UUID"]))
    t.nonempty(r, "Path")
    r = u1.api("POST", "/addressbooks/", json={"description": "no name"})
    t.status(r, 400, msg="an address book needs a name")


def test_ab_list_get_patch(t, u1, st):
    """address book list / get / rename"""
    t.need(st, "ab1_uuid")
    uuid = st.ab1_uuid
    r = u1.api("GET", "/addressbooks/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([a for a in j["addressbooks"] if a["UUID"] == uuid and a["shared"] is False
                           and a["permission"] == "owner"]) == 1)
    t.jq(r, lambda j: len([a for a in j["addressbooks"] if a["Name"] == "Contacts"]) >= 1,
         "registration seeds a Contacts book")
    r = u1.api("GET", f"/addressbooks/{uuid}")
    if not t.status(r, 200):
        return
    t.json(r, "UUID", uuid)
    st.ab1_name = f"{st.ab1_name} (renamed)"
    r = u1.api("PATCH", f"/addressbooks/{uuid}", json={"name": st.ab1_name})
    if not t.status(r, 200):
        return
    t.json(r, "Name", st.ab1_name)
    r = u1.api("GET", f"/addressbooks/{uuid}")
    t.json(r, "Name", st.ab1_name, "rename must persist")
    t.status(u1.api("GET", f"/addressbooks/{ZERO_UUID}"), 404)


def test_ab_isolation(t, u1, u2, st):
    """another user cannot read, change or delete it"""
    t.need(st, "ab1_uuid")
    uuid = st.ab1_uuid
    t.status(u2.api("GET", f"/addressbooks/{uuid}"), 404, msg="U2 reading U1's address book")
    t.status_not(u2.api("PATCH", f"/addressbooks/{uuid}", json={"name": "hijacked"}), 200, 204,
                 msg="U2 renaming U1's address book")
    t.status_not(u2.api("DELETE", f"/addressbooks/{uuid}", json=CONFIRM), 200, 204,
                 msg="U2 deleting U1's address book")
    t.status(u2.api("GET", f"/addressbooks/{uuid}/contacts"), 404, msg="U2 listing U1's contacts")
    t.status(u2.api("POST", f"/addressbooks/{uuid}/contacts", json={"formatted_name": "Intruder"}), 404,
             msg="U2 creating a contact in U1's book")
    t.not_contains(u2.api("GET", "/addressbooks/"), uuid)
    r = u1.api("GET", f"/addressbooks/{uuid}")
    if not t.status(r, 200, msg="U1's book must survive"):
        return
    t.json(r, "Name", st.ab1_name, "U1's book must be unchanged")


def test_ab_foreign_status(t, u2, st):
    """another user's book is a clean 404 (no 5xx / 400)"""
    t.need(st, "ab1_uuid")
    # The access is refused (see the isolation test); this pins the status:
    # a foreign book is "not found" like on GET — never a 5xx, never a 400.
    uuid = st.ab1_uuid
    t.status(u2.api("PATCH", f"/addressbooks/{uuid}", json={"name": "hijacked"}), 404,
             msg="PATCH on another user's book")
    t.status(u2.api("DELETE", f"/addressbooks/{uuid}", json=CONFIRM), 404, msg="DELETE on another user's book")
    t.status(u2.api("GET", f"/addressbooks/{uuid}/export"), 404, msg="export of another user's book")


def test_ab_contact_create(t, e2e, u1, st):
    """contact create returns the parsed contact"""
    t.need(st, "ab1_uuid")
    rid = e2e.run_id
    r = u1.api("POST", f"/addressbooks/{st.ab1_uuid}/contacts", json={
        "formatted_name": f"Jane Doe {rid}", "given_name": "Jane", "family_name": f"Doe{rid}",
        "organization": "Example Corp", "title": "Engineer", "birthday": "1990-04-01", "notes": "met at e2e",
        "emails": [{"type": "work", "value": "jane@example.test", "primary": True}],
        "phones": [{"type": "cell", "value": "+49 30 1234567"}],
        "addresses": [{"type": "work", "street": "Main St 1", "city": "Berlin", "postal_code": "10115",
                       "country": "DE"}],
        "urls": [{"type": "work", "value": "https://example.test"}],
    })
    if not t.status(r, 201):
        return
    st.ct_id, st.ct_uid = r.jdict().get("id"), r.jdict().get("uid")
    t.json(r, "formatted_name", f"Jane Doe {rid}")
    t.json(r, "given_name", "Jane")
    t.json(r, "organization", "Example Corp")
    t.json(r, "emails[0].value", "jane@example.test")
    t.json(r, "phones[0].value", "+49 30 1234567")
    t.json(r, "addresses[0].city", "Berlin")
    t.json(r, "birthday", "1990-04-01")
    t.nonempty(r, "uid")


def test_ab_contact_get_patch(t, e2e, u1, st):
    """contact get / PATCH round trip"""
    t.need(st, "ct_id")
    rid = e2e.run_id
    url = f"/addressbooks/{st.ab1_uuid}/contacts/{st.ct_id}"
    r = u1.api("GET", url)
    if not t.status(r, 200):
        return
    t.json(r, "id", st.ct_id)
    t.json(r, "title", "Engineer")
    r = u1.api("PATCH", url, json={"formatted_name": f"Jane Q. Doe {rid}", "given_name": "Jane",
                                   "middle_name": "Q.", "family_name": f"Doe{rid}"})
    if not t.status(r, 200):
        return
    t.json(r, "formatted_name", f"Jane Q. Doe {rid}")
    t.json(r, "middle_name", "Q.")
    r = u1.api("GET", url)
    t.json(r, "formatted_name", f"Jane Q. Doe {rid}", "the update must persist")
    t.json(r, "uid", st.ct_uid, "the UID never changes")


def test_ab_contact_derived_fn(t, e2e, u1):
    """a contact created without formatted_name gets an FN derived from its name"""
    ab = u1.mk_addressbook(e2e.uniq("derive-fn"))
    rid = e2e.run_id
    r = u1.api("POST", f"/addressbooks/{ab['UUID']}/contacts", json={"given_name": "Grace",
                                                                     "family_name": f"Hopper{rid}"})
    if not t.status(r, 201):
        return
    # RFC 6350 6.2.1: FN is required; it used to be stored as an empty "FN:".
    t.json(r, "formatted_name", f"Grace Hopper{rid}")
    r = u1.api("GET", f"/addressbooks/{ab['UUID']}/export")
    if t.status(r, 200):
        t.contains(r, f"FN:Grace Hopper{rid}")
        t.true("FN:\r\n" not in r.text, "no exported card may carry an empty FN", r)


def test_ab_contact_paging(t, e2e, u1):
    """contact list pages with limit/offset and sorts by name"""
    pg = u1.mk_addressbook(e2e.uniq("paging"))
    for n in ("Charlie", "Alice", "Bob"):
        try:
            u1.mk_contact(pg["UUID"], f"{n} Pager", given_name=n, family_name="Pager")
        except SetupFailed as e:
            t.fail(f"seed {n} failed", e.resp)
    r = u1.api("GET", f"/addressbooks/{pg['UUID']}/contacts?limit=2&offset=0")
    if not t.status(r, 200):
        return
    t.json(r, "Total", 3)
    t.json(r, "Limit", 2)
    t.jq(r, lambda j: len(j["Contacts"]) == 2)
    t.jq(r, lambda j: [c["formatted_name"] for c in j["Contacts"]] == ["Alice Pager", "Bob Pager"],
         "default sort is by name, ascending")
    r = u1.api("GET", f"/addressbooks/{pg['UUID']}/contacts?limit=2&offset=2")
    t.jq(r, lambda j: [c["formatted_name"] for c in j["Contacts"]] == ["Charlie Pager"],
         "the second page holds the rest")
    r = u1.api("GET", f"/addressbooks/{pg['UUID']}/contacts?order=desc")
    t.jq(r, lambda j: j["Contacts"][0]["formatted_name"] == "Charlie Pager", "order=desc reverses")


def test_ab_photo(t, u1, st):
    """contact photo: upload, fetch, replace, delete"""
    t.need(st, "ct_id")
    base = f"/addressbooks/{st.ab1_uuid}/contacts/{st.ct_id}"
    if not t.status(u1.api("PUT", f"{base}/photo", data=PNG_PIXEL, ctype="image/png"), 204, 200):
        return
    r = u1.api("GET", f"{base}/photo")
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "image/png")
    if r.content != PNG_PIXEL:
        t.fail("the photo bytes must round-trip unchanged")
    r = u1.api("GET", base)
    t.nonempty(r, "photo_url", "the contact must advertise its photo URL")
    if not t.status(u1.api("DELETE", f"{base}/photo"), 204, 200):
        return
    t.status(u1.api("GET", f"{base}/photo"), 404, msg="a deleted photo is gone")
    r = u1.api("GET", base)
    t.jq(r, lambda j: (j.get("photo_url") or "") == "", "photo_url must be cleared")


def test_ab_photo_type(t, u1, st):
    """contact photo: wrong type is 415"""
    t.need(st, "ct_id")
    r = u1.api("PUT", f"/addressbooks/{st.ab1_uuid}/contacts/{st.ct_id}/photo",
               data="this is not an image", ctype="image/png")
    t.status(r, 415)


def test_ab_contact_unknown(t, u1, st):
    """unknown contact is a 404 for every verb"""
    t.need(st, "ab1_uuid")
    base = f"/addressbooks/{st.ab1_uuid}/contacts/{ZERO_UUID}"
    t.status(u1.api("GET", base), 404, msg="GET")
    t.status(u1.api("PATCH", base, json={"formatted_name": "x"}), 404, msg="PATCH")
    t.status(u1.api("DELETE", base), 404, msg="DELETE (must not be a 5xx)")
    t.status(u1.api("PUT", f"{base}/photo", data=PNG_PIXEL, ctype="image/png"), 404,
             msg="photo upload (must not be a 5xx)")
    t.status(u1.api("DELETE", f"{base}/photo"), 404, msg="photo delete (must not be a 5xx)")


def test_ab_contact_move(t, e2e, u1, u2, st):
    """contact move keeps the UID"""
    t.need(st, "ab1_uuid")
    mv = u1.mk_addressbook(e2e.uniq("move-target"))
    ct = u1.mk_contact(st.ab1_uuid, f"Mover McMoveson {e2e.run_id}")
    cid, uid = ct.get("id"), ct.get("uid")
    r = u1.api("POST", f"/addressbooks/{st.ab1_uuid}/contacts/{cid}/move",
               json={"target_addressbook_id": mv["UUID"]})
    if not t.status(r, 200):
        return
    t.json(r, "uid", uid, "the UID survives the move")
    r = u1.api("GET", f"/addressbooks/{st.ab1_uuid}/contacts?limit=200")
    t.jq(r, lambda j: len([c for c in j["Contacts"] or [] if c["uid"] == uid]) == 0, "gone from the source book")
    r = u1.api("GET", f"/addressbooks/{mv['UUID']}/contacts?limit=200")
    t.jq(r, lambda j: len([c for c in j["Contacts"] if c["uid"] == uid]) == 1, "present in the target book")
    r = u2.api("GET", "/addressbooks/")
    u2ab = (r.jdict().get("addressbooks") or [{}])[0].get("UUID")
    r = u1.api("POST", f"/addressbooks/{mv['UUID']}/contacts/{cid}/move", json={"target_addressbook_id": u2ab})
    t.status_class(r, "4xx", msg="moving into another user's book must be refused")


def test_ab_search(t, e2e, u1, u2, st):
    """contact search finds by name and e-mail"""
    t.need(st, "ct_id")
    rid, cid = e2e.run_id, st.ct_id
    r = u1.api("GET", f"/contacts/search?q=Doe{rid}")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([c for c in j["contacts"] if c["id"] == cid]) == 1, "search by family name")
    t.jq(r, lambda j: j["count"] == len(j["contacts"]))
    r = u1.api("GET", "/contacts/search?q=jane@example.test")
    t.jq(r, lambda j: len([c for c in j["contacts"] if c["id"] == cid]) == 1, "search by e-mail")
    r = u1.api("GET", f"/contacts/search?q=Doe{rid}&addressbook_id={st.ab1_uuid}")
    t.jq(r, lambda j: len(j["contacts"]) == 1, "narrowed to one book")
    r = u2.api("GET", f"/contacts/search?q=Doe{rid}")
    t.status(r, 200)
    t.jq(r, lambda j: len(j["contacts"]) == 0, "another user must not find U1's contacts")


def test_ab_search_invalid(t, e2e, u1, u2, st):
    """contact search validates its input"""
    t.status(u1.api("GET", "/contacts/search"), 400, msg="q is required")
    t.status(u1.api("GET", f"/contacts/search?q=x&addressbook_id={ZERO_UUID}"), 404,
             msg="an unknown address book is a 404, not an empty result")
    t.need(st, "ab1_uuid")
    t.status(u2.api("GET", f"/contacts/search?q=x&addressbook_id={st.ab1_uuid}"), 404,
             msg="another user's address book is a 404")
    t.status(e2e.anon("GET", "/contacts/search?q=x"), 401)


def test_ab_contact_delete(t, e2e, u1, st):
    """contact delete is 204, then 404"""
    t.need(st, "ct_id")
    url = f"/addressbooks/{st.ab1_uuid}/contacts/{st.ct_id}"
    if not t.status(u1.api("DELETE", url), 204):
        return
    t.status(u1.api("GET", url), 404)
    r = u1.api("GET", f"/contacts/search?q=Doe{e2e.run_id}")
    t.jq(r, lambda j: len(j["contacts"]) == 0, "a deleted contact must not be found")


def test_ab_delete(t, e2e, u1):
    """address book delete needs the confirmation"""
    ab = u1.mk_addressbook(e2e.uniq("delete-me"))
    url = f"/addressbooks/{ab['UUID']}"
    t.status(u1.api("DELETE", url), 400, msg="missing confirmation")
    if not t.status(u1.api("GET", url), 200, msg="must survive the refused delete"):
        return
    if not t.status(u1.api("DELETE", url, json=CONFIRM), 204):
        return
    t.status(u1.api("GET", url), 404)


def test_ab_last_guard(t, e2e):
    """the last remaining address book cannot be deleted"""
    uab = e2e.create_user("UAB", "E2E Single Book")
    r = uab.api("GET", "/addressbooks/")
    if not t.status(r, 200):
        return
    books = r.jdict().get("addressbooks") or []
    uuid = books[0].get("UUID") if books else None
    if not t.eq(len(books), 1, "a fresh account has exactly one address book"):
        return
    t.status_class(uab.api("DELETE", f"/addressbooks/{uuid}", json=CONFIRM), "4xx",
                   msg="deleting the only address book must be refused")
    t.status(uab.api("GET", f"/addressbooks/{uuid}"), 200, msg="the only address book must survive")
