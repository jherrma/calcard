"""DAV credentials managed over REST: app passwords (scoped, revocable) and the
dedicated CalDAV / CardDAV credentials (own username, read or read-write)."""

import re
from datetime import datetime, timedelta, timezone

import pytest

from harness.checks import known
from harness.data import PROPFIND_PRINCIPAL, ics_event, vcard

DESCRIPTION = "app passwords, CalDAV/CardDAV credentials: CRUD, scopes, revocation"

pytestmark = pytest.mark.usefixtures("u1", "u2")


def _probe(e2e, user, password, path, method="PROPFIND", **kw):
    """PROPFIND (default) with Basic auth."""
    if method == "PROPFIND":
        return e2e.req("PROPFIND", f"{e2e.base}{path}", basic=(user, password), depth=0, xml=PROPFIND_PRINCIPAL)
    return e2e.req(method, f"{e2e.base}{path}", basic=(user, password), **kw)


def test_ap_create(t, e2e, u1, st):
    """app password: create returns the secret once"""
    r = u1.api("POST", "/app-passwords/", json={"name": e2e.uniq("ap-main"), "scopes": ["caldav", "carddav"]})
    if not t.status(r, 200, 201):
        return
    t.json(r, "status", "ok")
    data = r.jdict().get("data") or {}
    st.ap_id = data.get("id")
    st.ap_pw = data.get("password")
    st.ap_user = (data.get("credentials") or {}).get("username")
    e2e.defer(f"revoke app password {e2e.uniq('ap-main')}", e2e.cleanup_api, u1, "DELETE", f"/app-passwords/{st.ap_id}")
    t.nonempty(r, "data.password", "the secret must be returned on creation")
    t.json(r, "data.credentials.password", st.ap_pw, "credentials.password mirrors password")
    t.json(r, "data.credentials.username", u1.username, "credentials.username is the account username")
    t.jq(r, lambda j: re.fullmatch(r"https?://\S+", j["data"]["credentials"]["server_url"]),
         "credentials.server_url must be an absolute URL (derived from the request when CALDAV_BASE_URL is unset, #187)")
    t.jq(r, lambda j: j["data"]["scopes"] == ["caldav", "carddav"], 'expected JSON condition to hold: .data.scopes == ["caldav","carddav"]')
    if len(st.ap_pw or "") < 16:
        t.fail(f"app password is suspiciously short ({len(st.ap_pw or '')} chars)")


def test_ap_list(t, u1, st):
    """app password: list shows metadata, never the secret"""
    t.need(st, "ap_id")
    r = u1.api("GET", "/app-passwords/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([a for a in j["data"]["app_passwords"] if a["id"] == st.ap_id]) == 1,
         "list must contain the new app password")
    t.not_contains(r, st.ap_pw, "the list must never contain the secret")
    t.jq(r, lambda j: not any("password" in a for a in j["data"]["app_passwords"]),
         "no list entry may carry a password field")


def test_ap_dav_email(t, e2e, u1, st):
    """app password: authenticates DAV with the e-mail"""
    t.need(st, "ap_pw")
    r = _probe(e2e, u1.email, st.ap_pw, "/dav/")
    if not t.status(r, 207):
        return
    t.contains(r, f"/dav/{u1.username}/")
    r = _probe(e2e, u1.email, f"wrong-{st.ap_pw}", "/dav/")
    t.status(r, 401)
    t.header(r, "WWW-Authenticate", "Basic")


def test_ap_dav_username(t, e2e, st):
    """app password: authenticates DAV with the returned username"""
    t.need(st, "ap_pw", "ap_user")
    # The server tells clients to use this username (credentials.username) and
    # it is the segment of every DAV URL — it has to work for Basic auth.
    r = _probe(e2e, st.ap_user, st.ap_pw, "/dav/")
    if any(c.isupper() for c in st.ap_user):
        t.known("F02")  # the lookup lower-cases the mixed-case username → 401
        t.bug.status(r, 207, was=401, msg="Basic auth with the username returned by POST /app-passwords")
    else:
        t.status(r, 207, msg="Basic auth with the username returned by POST /app-passwords")


def test_ap_scope_caldav(t, e2e, u1):
    """app password: caldav-only scope is refused on address books"""
    r = u1.api("POST", "/app-passwords/", json={"name": e2e.uniq("ap-caldav"), "scopes": ["caldav"]})
    if not t.status(r, 200, 201):
        return
    data = r.json()["data"]
    aid, pw = data.get("id"), data.get("password")
    e2e.defer(f"revoke app password {e2e.uniq('ap-caldav')}", e2e.cleanup_api, u1, "DELETE", f"/app-passwords/{aid}")
    t.status(_probe(e2e, u1.email, pw, u1.home_cal), 207, msg="caldav scope on the calendar home")
    t.status(_probe(e2e, u1.email, pw, u1.home_ab), 403, msg="caldav-only app password on the address-book home")
    t.status(_probe(e2e, u1.email, pw, "/dav/"), 207, msg="discovery (/dav/) is open to every scope")
    # Revoke right away: every live app password costs a bcrypt per DAV request.
    u1.api("DELETE", f"/app-passwords/{aid}")


def test_ap_scope_carddav(t, e2e, u1):
    """app password: carddav-only scope is refused on calendars"""
    r = u1.api("POST", "/app-passwords/", json={"name": e2e.uniq("ap-carddav"), "scopes": ["carddav"]})
    if not t.status(r, 200, 201):
        return
    data = r.json()["data"]
    aid, pw = data.get("id"), data.get("password")
    e2e.defer(f"revoke app password {e2e.uniq('ap-carddav')}", e2e.cleanup_api, u1, "DELETE", f"/app-passwords/{aid}")
    t.status(_probe(e2e, u1.email, pw, u1.home_ab), 207)
    t.status(_probe(e2e, u1.email, pw, u1.home_cal), 403)
    # Scope is decided by path segment, not substring: "calendars" in an
    # address-book object name must not unlock it for a caldav credential.
    t.status(_probe(e2e, u1.email, pw, f"{u1.home_cal}x/addressbooks.ics", "GET"), 403)
    u1.api("DELETE", f"/app-passwords/{aid}")


def test_ap_validation(t, e2e, u1):
    """app password: blank name, unknown or empty scopes are refused (400)"""
    cases = [
        ({"name": "", "scopes": ["caldav"]}, "an empty name"),
        ({"name": "   ", "scopes": ["caldav"]}, "a whitespace-only name"),
        ({"scopes": ["caldav"]}, "a missing name"),
        ({"name": "x" * 101, "scopes": ["caldav"]}, "a name over 100 characters"),
        ({"name": e2e.uniq("ap-bad"), "scopes": ["admin"]}, "an unknown scope"),
        ({"name": e2e.uniq("ap-bad"), "scopes": ["caldav", "CardDAV"]}, "an unknown scope next to a valid one"),
        ({"name": e2e.uniq("ap-bad"), "scopes": []}, "an empty scope list"),
    ]
    for body, what in cases:
        r = u1.api("POST", "/app-passwords/", json=body)
        if r.status in (200, 201):  # accepted after all: do not leave it behind
            aid = (r.jdict().get("data") or {}).get("id")
            e2e.defer(f"revoke app password ({what})", e2e.cleanup_api, u1, "DELETE", f"/app-passwords/{aid}")
        if t.status(r, 400, msg=f"{what} must be refused"):
            t.json(r, "status", "error")
            t.true(r.jmsg(), f"the 400 for {what} must say what is wrong", r)


def test_ap_default_scopes(t, e2e, u1):
    """app password: omitted scopes default to caldav + carddav, the name is trimmed"""
    name = e2e.uniq("ap-default")
    r = u1.api("POST", "/app-passwords/", json={"name": f"  {name}  "})
    if not t.status(r, 200, 201):
        return
    data = r.jdict().get("data") or {}
    aid, pw = data.get("id"), data.get("password")
    e2e.defer(f"revoke app password {name}", e2e.cleanup_api, u1, "DELETE", f"/app-passwords/{aid}")
    t.json(r, "data.name", name, "the name is stored trimmed")
    t.jq(r, lambda j: j["data"]["scopes"] == ["caldav", "carddav"],
         'expected JSON condition to hold: .data.scopes == ["caldav","carddav"] when scopes are omitted')
    t.status(_probe(e2e, u1.email, pw, u1.home_cal), 207, msg="default scope covers calendars")
    t.status(_probe(e2e, u1.email, pw, u1.home_ab), 207, msg="default scope covers address books")
    u1.api("DELETE", f"/app-passwords/{aid}")


def test_ap_foreign_revoke(t, e2e, u1, u2, st):
    """app password: another user cannot revoke it (404)"""
    t.need(st, "ap_id")
    if not t.status(u2.api("DELETE", f"/app-passwords/{st.ap_id}"), 404):
        return
    t.status(_probe(e2e, u1.email, st.ap_pw, "/dav/"), 207, msg="U1's app password must survive U2's revoke attempt")


def test_ap_revoke(t, e2e, u1):
    """app password: revoke takes effect immediately"""
    r = u1.api("POST", "/app-passwords/", json={"name": e2e.uniq("ap-revoke"), "scopes": ["caldav", "carddav"]})
    if not t.status(r, 200, 201):
        return
    data = r.json()["data"]
    aid, pw = data.get("id"), data.get("password")
    e2e.defer(f"revoke app password {e2e.uniq('ap-revoke')}", e2e.cleanup_api, u1, "DELETE", f"/app-passwords/{aid}")
    if not t.status(_probe(e2e, u1.email, pw, "/dav/"), 207, msg="works before revocation"):
        return
    if not t.status(u1.api("DELETE", f"/app-passwords/{aid}"), 204):
        return
    t.status(_probe(e2e, u1.email, pw, "/dav/"), 401, msg="a revoked app password must be rejected on the next request")
    r = u1.api("GET", "/app-passwords/")
    t.jq(r, lambda j: len([a for a in j["data"]["app_passwords"] or [] if a["id"] == aid]) == 0,
         "revoked entry must leave the list")
    t.status(u1.api("DELETE", f"/app-passwords/{aid}"), 404, msg="revoking twice is a 404")


# Dedicated credential usernames are global, so they carry the run id.
def test_caldav_crud(t, e2e, u1, st):
    """CalDAV credential: create / list / validation"""
    st.calcred_user, st.calcred_pw = e2e.uniq("calro"), f"ReadOnly_{e2e.run_id}"
    r = u1.api("POST", "/caldav-credentials/", json={"name": "e2e read-only", "username": st.calcred_user,
                                                     "password": st.calcred_pw, "permission": "read"})
    if not t.status(r, 201):
        return
    st.calcred_id = r.jdict().get("id")
    e2e.defer(f"revoke CalDAV credential {st.calcred_user}", e2e.cleanup_api, u1, "DELETE",
              f"/caldav-credentials/{st.calcred_id}")
    t.json(r, "username", st.calcred_user)
    t.json(r, "permission", "read")
    t.jq(r, lambda j: "password" not in j, "the response must not echo the password")
    r = u1.api("GET", "/caldav-credentials/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([c for c in j["credentials"] if c["id"] == st.calcred_id and c["permission"] == "read"]) == 1,
         'expected JSON condition to hold: credential listed with permission "read"')
    t.not_contains(r, st.calcred_pw)
    # Validation
    r = u1.api("POST", "/caldav-credentials/", json={"name": "x", "username": "ab", "password": "LongEnough_123"})
    t.status(r, 400, msg="username shorter than 3 chars")
    r = u1.api("POST", "/caldav-credentials/", json={"name": "x", "username": f"{e2e.uniq('bad')}!", "password": "LongEnough_123"})
    t.status(r, 400, msg="username with forbidden characters")
    r = u1.api("POST", "/caldav-credentials/", json={"name": "x", "username": e2e.uniq("short"), "password": "short"})
    t.status(r, 400, msg="password shorter than 8 chars")
    r = u1.api("POST", "/caldav-credentials/", json={"name": "x", "username": e2e.uniq("perm"), "password": "LongEnough_123",
                                                     "permission": "admin"})
    t.status(r, 400, msg="unknown permission")
    r = u1.api("POST", "/caldav-credentials/", json={"name": "x", "username": e2e.uniq("exp"), "password": "LongEnough_123",
                                                     "expires_at": "tomorrow"})
    t.status(r, 400, msg="expires_at must be RFC 3339")


def test_caldav_readonly(t, e2e, u1, st):
    """CalDAV credential: read-only may read, not write"""
    t.need(st, "calcred_id")
    r = u1.api("GET", "/calendars/")
    own = [c for c in r.jdict().get("calendars") or [] if c.get("shared") is not True and c.get("subscribed") is not True]
    cal = own[0].get("path") if own else None
    col = f"{u1.home_cal}{cal}/"
    creds = (st.calcred_user, st.calcred_pw)
    if not t.status(_probe(e2e, *creds, col), 207, msg="read-only credential may PROPFIND"):
        return
    uid = e2e.uniq("ro-write")
    r = e2e.req("PUT", f"{e2e.base}{col}{uid}.ics", basic=creds,
                ics=ics_event(uid, "Must not land", "20300101T100000Z", "20300101T110000Z"))
    t.status(r, 403, msg="read-only credential must not PUT")
    r = e2e.req("PROPPATCH", f"{e2e.base}{col}", basic=creds,
                xml='<?xml version="1.0"?><d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>hijack</d:displayname></d:prop></d:set></d:propertyupdate>')
    t.status(r, 403, msg="read-only credential must not PROPPATCH")


def test_caldav_scope(t, e2e, u1, st):
    """CalDAV credential: confined to calendar paths"""
    t.need(st, "calcred_id")
    r = _probe(e2e, st.calcred_user, st.calcred_pw, f"/dav/{u1.username}/")
    if not t.status(r, 207):
        return
    t.contains(r, u1.home_cal, "principal must show the calendar home set")
    t.not_contains(r, u1.home_ab, "a CalDAV credential must not see the address-book home set")
    t.status(_probe(e2e, st.calcred_user, st.calcred_pw, u1.home_ab), 403,
             msg="a CalDAV credential is confined to calendar paths")


def test_caldav_dup(t, u1, u2, st):
    """CalDAV credential: duplicate username is a 409"""
    t.need(st, "calcred_user")
    body = {"name": "dup", "username": st.calcred_user, "password": "LongEnough_123"}
    if not t.status(u1.api("POST", "/caldav-credentials/", json=body), 409):
        return
    # Usernames are global: another account cannot take it either.
    t.status(u2.api("POST", "/caldav-credentials/", json=body), 409,
             msg="credential usernames are unique across accounts")


def test_caldav_expired(t, e2e, u1):
    """CalDAV credential: expired credential is rejected"""
    u, pw = e2e.uniq("calexp"), f"Expired_{e2e.run_id}"
    r = u1.api("POST", "/caldav-credentials/", json={"name": "expired", "username": u, "password": pw,
                                                     "permission": "read", "expires_at": "2020-01-01T00:00:00Z"})
    if r.status == 400:
        return  # refusing a past expiry up front is fine too
    if not t.status(r, 201):
        return
    e2e.defer(f"revoke CalDAV credential {u}", e2e.cleanup_api, u1, "DELETE", f"/caldav-credentials/{r.jdict().get('id')}")
    t.status(_probe(e2e, u, pw, "/dav/"), 401, msg="an expired credential must not authenticate")


def test_dav_cred_validation(t, e2e, u1):
    """CalDAV / CardDAV credential: past expiry and blank name are refused (400)"""
    a_minute_ago = (datetime.now(timezone.utc) - timedelta(minutes=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
    tomorrow = (datetime.now(timezone.utc) + timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
    pw = f"Validate_{e2e.run_id}"
    for kind in ("caldav", "carddav"):
        path = f"/{kind}-credentials/"
        cases = [
            ({"name": "past", "username": e2e.uniq(f"{kind[:4]}past"), "password": pw,
              "expires_at": "2020-01-01T00:00:00Z"}, "an expiry in the past", "future"),
            ({"name": "past", "username": e2e.uniq(f"{kind[:4]}ago"), "password": pw,
              "expires_at": a_minute_ago}, "an expiry a minute ago", "future"),
            ({"name": "   ", "username": e2e.uniq(f"{kind[:4]}blank"), "password": pw}, "a whitespace-only name", "name"),
        ]
        for body, what, word in cases:
            r = u1.api("POST", path, json=body)
            if r.status == 201:  # accepted after all: do not leave it behind
                e2e.defer(f"revoke {kind} credential {body['username']}", e2e.cleanup_api, u1, "DELETE",
                          f"{path}{r.jdict().get('id')}")
            if t.status(r, 400, msg=f"{kind}: {what} must be refused"):
                t.true(word in r.jmsg(), f"{kind}: the 400 for {what} must mention '{word}', got {r.jmsg()!r}", r)
        # A future expiry and a padded name are fine; the name comes back trimmed.
        u = e2e.uniq(f"{kind[:4]}fut")
        r = u1.api("POST", path, json={"name": "  Phone  ", "username": u, "password": pw, "expires_at": tomorrow})
        if not t.status(r, 201, msg=f"{kind}: an expiry tomorrow is accepted"):
            continue
        cid = r.jdict().get("id")
        e2e.defer(f"revoke {kind} credential {u}", e2e.cleanup_api, u1, "DELETE", f"{path}{cid}")
        t.json(r, "name", "Phone", f"{kind}: the name is stored trimmed")
        t.nonempty(r, "expires_at")
        home = u1.home_cal if kind == "caldav" else u1.home_ab
        t.status(_probe(e2e, u, pw, home), 207, msg=f"{kind}: a credential expiring tomorrow authenticates")
        u1.api("DELETE", f"{path}{cid}")


def test_caldav_revoke(t, e2e, u1, u2, st):
    """CalDAV credential: revoke takes effect immediately"""
    t.need(st, "calcred_id")
    if not t.status(_probe(e2e, st.calcred_user, st.calcred_pw, "/dav/"), 207):
        return
    t.status(u2.api("DELETE", f"/caldav-credentials/{st.calcred_id}"), 404, msg="another user cannot revoke it")
    if not t.status(u1.api("DELETE", f"/caldav-credentials/{st.calcred_id}"), 204):
        return
    t.status(_probe(e2e, st.calcred_user, st.calcred_pw, "/dav/"), 401, msg="revoked credential must stop working")


def test_carddav_rw(t, e2e, u1, st):
    """CardDAV credential: read-write may write contacts"""
    st.cardcred_user, st.cardcred_pw = e2e.uniq("cardrw"), f"ReadWrite_{e2e.run_id}"
    r = u1.api("POST", "/carddav-credentials/", json={"name": "e2e card rw", "username": st.cardcred_user,
                                                      "password": st.cardcred_pw, "permission": "read-write"})
    if not t.status(r, 201):
        return
    st.cardcred_id = r.jdict().get("id")
    e2e.defer(f"revoke CardDAV credential {st.cardcred_user}", e2e.cleanup_api, u1, "DELETE",
              f"/carddav-credentials/{st.cardcred_id}")
    t.json(r, "permission", "read-write")
    ab = u1.mk_addressbook(e2e.uniq("cred-book"))
    col, uid = f"{u1.home_ab}{ab['Path']}/", e2e.uniq("cardcred-contact")
    r = e2e.req("PUT", f"{e2e.base}{col}{uid}.vcf", basic=(st.cardcred_user, st.cardcred_pw),
                vcf=vcard(uid, "Cardcred Contact"))
    if not t.status(r, 201, 204):
        return
    r = u1.api("GET", "/contacts/search?q=Cardcred")
    t.contains(r, uid, "the contact written with the credential must be visible to the owner")


def test_carddav_scope(t, e2e, u1, st):
    """CardDAV credential: confined to address-book paths"""
    t.need(st, "cardcred_id")
    creds = (st.cardcred_user, st.cardcred_pw)
    if not t.status(_probe(e2e, *creds, u1.home_cal), 403):
        return
    r = _probe(e2e, *creds, f"/dav/{u1.username}/")
    if not t.status(r, 207):
        return
    t.not_contains(r, u1.home_cal, "a CardDAV credential must not see the calendar home set")
    t.contains(r, u1.home_ab)


@known("F02")
def test_carddav_mixedcase(t, e2e, u1):
    """CardDAV credential: mixed-case username authenticates"""
    # The create endpoint accepts upper-case usernames ([a-zA-Z0-9_-]); a
    # credential the server accepts must also be usable.
    # The name fits the 50-character limit for any E2E_USER_PREFIX the
    # configuration accepts (harness/config.py MAX_RUN_TAG).
    u, pw = f"Mixed-{e2e.uniq('card')}", f"MixedCase_{e2e.run_id}"
    r = u1.api("POST", "/carddav-credentials/", json={"name": "mixed", "username": u, "password": pw, "permission": "read"})
    # A refusal here is NOT the fix of F02 (and never an XPASS): if the server
    # starts rejecting upper-case usernames on purpose, update this test.
    if not t.status(r, 201, msg="the create endpoint accepts upper-case usernames ([a-zA-Z0-9_-])"):
        return
    e2e.defer(f"revoke CardDAV credential {u}", e2e.cleanup_api, u1, "DELETE", f"/carddav-credentials/{r.jdict().get('id')}")
    t.bug.status(_probe(e2e, u, pw, u1.home_ab), 207, was=401, msg="Basic auth with the username exactly as created")


def test_carddav_revoke(t, e2e, u1, st):
    """CardDAV credential: revoke takes effect immediately"""
    t.need(st, "cardcred_id")
    if not t.status(u1.api("DELETE", f"/carddav-credentials/{st.cardcred_id}"), 204):
        return
    t.status(_probe(e2e, st.cardcred_user, st.cardcred_pw, u1.home_ab), 401)
    r = u1.api("GET", "/carddav-credentials/")
    t.jq(r, lambda j: len([c for c in j["credentials"] or [] if c["id"] == st.cardcred_id]) == 0,
         "expected JSON condition to hold: the revoked credential leaves the list")
