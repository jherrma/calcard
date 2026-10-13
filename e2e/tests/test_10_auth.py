"""Authentication: registration, login, JWT refresh rotation, logout, password
change, forgot/reset/verify edges, OAuth surface, account deletion.

Since P02 (#189) a logout with a live refresh token (or a bearer), a password
change and a password reset revoke EVERY access token of the account. So the
steps that log in, refresh, log out or change a password never touch the
shared U1/U2 accounts: login/refresh/logout run on their own throwaway
account (ULOG), password changes on UA, account deletion on UDEL."""

import base64
import re
import time

import pytest

from harness.checks import known
from harness.data import PROPFIND_PRINCIPAL
from harness.session import TOKEN_REJECTED

DESCRIPTION = "register, login, refresh, logout, password change, account deletion"

pytestmark = pytest.mark.usefixtures("u1")


@pytest.fixture(scope="module")
def ulog(e2e):
    """Throwaway account for the login / refresh / logout tests: each logout
    revokes all of ITS access tokens, nobody else's."""
    return e2e.create_user("ULOG", "E2E Logout User")


def _session(e2e, user):
    """Open an extra login session → ((access, refresh) or None, response)."""
    r = e2e.anon("POST", "/auth/login", json={"email": user.email, "password": user.password})
    d = r.jdict().get("data") or {}
    if r.status != 200 or not d.get("access_token") or not d.get("refresh_token"):
        return None, r
    return (d["access_token"], d["refresh_token"]), r


def _me(e2e, token):
    """GET /users/me with exactly this access token (no re-login)."""
    return e2e.req("GET", f"{e2e.base}/api/v1/users/me", bearer=token)


def test_register(t, e2e, st):
    """register returns the new account (active at once, SMTP or not)"""
    st.ua = ua = e2e.create_user("UA", "E2E Auth User")
    # create_user already logged in, so the account is usable; look at what it stored.
    t.true(ua.username, f"username must be a non-empty server-assigned slug (got '{ua.username}')")
    if not re.fullmatch(r"[A-Za-z0-9]+", ua.username):
        t.fail(f"username '{ua.username}' is not alphanumeric")
    if not re.fullmatch(r"[0-9a-f-]{36}", ua.id):
        t.fail(f"id '{ua.id}' is not a UUID")
    r = ua.api("GET", "/users/me")
    if not t.status(r, 200):
        return
    t.json(r, "data.email", ua.email)
    t.json(r, "data.display_name", "E2E Auth User")
    t.json(r, "data.is_active", True)
    # email_verified depends on the server's SMTP setting, never on is_active
    # (an account is active either way): with SMTP off nothing can be confirmed,
    # so it is verified on registration; with SMTP on it stays false until the
    # mailed link is opened, which the suite cannot do.
    smtp = (e2e.anon("GET", "/system/settings").jdict().get("data") or {}).get("smtp_enabled")
    if not isinstance(smtp, bool):
        return t.fail(f"GET /system/settings has no boolean smtp_enabled (got {smtp!r})")
    t.json(r, "data.email_verified", not smtp,
           "SMTP on: unverified until the mailed link is opened" if smtp else "SMTP off: verified on registration")


def test_register_dup(t, e2e, u1):
    """register rejects a duplicate e-mail (409)"""
    r = e2e.anon("POST", "/auth/register", json={"email": u1.email, "password": "Another!Pass123", "display_name": "dup"})
    if not t.status(r, 409):
        return
    t.json(r, "status", "error")


def test_register_validation(t, e2e):
    """register validates e-mail and password"""
    r = e2e.anon("POST", "/auth/register", json={"email": "not-an-email", "password": "Valid!Pass123", "display_name": "x"})
    t.status(r, 400)
    r = e2e.anon("POST", "/auth/register",
                 json={"email": f"{e2e.uniq('weak')}@{e2e.domain}", "password": "short", "display_name": "x"})
    t.status(r, 400)
    t.contains(r, "8", "the error should state the minimum length")
    t.status(e2e.anon("POST", "/auth/register", json="{}"), 400)
    t.status(e2e.anon("POST", "/auth/register", data="{not json", ctype="application/json"), 400)


def test_login_shape(t, e2e, ulog):
    """login returns bearer tokens with expires_at"""
    r = e2e.anon("POST", "/auth/login", json={"email": ulog.email, "password": ulog.password})
    if not t.status(r, 200):
        return
    t.json(r, "data.token_type", "Bearer")
    t.nonempty(r, "data.access_token")
    t.nonempty(r, "data.refresh_token")
    now = int(time.time())
    t.jq(r, lambda j: j["data"]["expires_at"] > now, "expires_at must be a future Unix timestamp")
    t.json(r, "data.user.email", ulog.email)
    t.json(r, "data.user.username", ulog.username, "the SPA builds its DAV URLs from the login user's username")
    # A JWT has three base64url segments.
    data = r.jdict().get("data") or {}
    if not re.fullmatch(r"[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+", data.get("access_token") or ""):
        t.fail("access_token does not look like a JWT")
    # Close the extra session (this revokes ULOG's access tokens, nobody else's).
    e2e.anon("POST", "/auth/logout", json={"refresh_token": data.get("refresh_token") or ""})


def test_login_wrong(t, e2e, ulog):
    """login failure does not reveal whether the account exists"""
    r = e2e.anon("POST", "/auth/login", json={"email": ulog.email, "password": f"wrong-{e2e.run_id}"})
    if not t.status(r, 401):
        return
    msg_known = r.jmsg()
    t.true(msg_known, "a failed login must carry a message", r)
    r = e2e.anon("POST", "/auth/login", json={"email": f"{e2e.uniq('nobody')}@{e2e.domain}", "password": f"wrong-{e2e.run_id}"})
    if not t.status(r, 401):
        return
    t.eq(r.jmsg(), msg_known, "wrong password and unknown account must produce the same message")


def test_protected(t, e2e, u1):
    """protected endpoints reject missing / bad tokens"""
    me = f"{e2e.base}/api/v1/users/me"
    t.status(e2e.anon("GET", "/users/me"), 401)
    t.status(e2e.req("GET", me, bearer="not.a.jwt"), 401)
    basic = base64.b64encode(f"{u1.email}:{u1.password}".encode()).decode()
    t.status(e2e.req("GET", me, headers={"Authorization": f"Basic {basic}"}), 401)
    # A token signed with another key must be refused (alg/secret check).
    forged = ("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLC"
              "JleHAiOjQxMDI0NDQ4MDB9.c2lnbmF0dXJlLW5vdC12YWxpZA")
    t.status(e2e.req("GET", me, bearer=forged), 401)


def test_refresh(t, e2e, ulog):
    """refresh rotates the refresh token, old one dies"""
    tokens, r = _session(e2e, ulog)
    if tokens is None:
        return t.fail("login failed", r)
    first = tokens[1]
    r = e2e.anon("POST", "/auth/refresh", json={"refresh_token": first})
    if not t.status(r, 200):
        return
    t.json(r, "data.token_type", "Bearer")
    data = r.jdict().get("data") or {}
    second = data.get("refresh_token") or ""
    t.true(second, "refresh must return a new refresh token", r)
    t.ne(second, first, "refresh must rotate the refresh token")
    t.status(_me(e2e, data.get("access_token") or ""), 200, msg="the refreshed access token works")
    t.status(e2e.anon("POST", "/auth/refresh", json={"refresh_token": first}), 401,
             msg="a rotated (old) refresh token must be rejected")
    r = e2e.anon("POST", "/auth/refresh", json={"refresh_token": second})
    if not t.status(r, 200, msg="the current refresh token must keep working"):
        return
    e2e.anon("POST", "/auth/logout", json={"refresh_token": (r.jdict().get("data") or {}).get("refresh_token") or ""})


def test_refresh_bad(t, e2e):
    """refresh rejects garbage and missing tokens"""
    t.status(e2e.anon("POST", "/auth/refresh", json='{"refresh_token":"clearly-not-a-real-token"}'), 401)
    t.status(e2e.anon("POST", "/auth/refresh", json="{}"), 400, 401)


def test_logout(t, e2e, ulog):
    """logout revokes the refresh token and every access token of the account"""
    # The harness's own ULOG session plays the account's other "device".
    if not t.status(ulog.api("GET", "/users/me"), 200, msg="precondition: the other session works"):
        return
    other = ulog.token
    tokens, r = _session(e2e, ulog)
    if tokens is None:
        return t.fail("login failed", r)
    access, refresh = tokens
    r = e2e.req("POST", f"{e2e.base}/api/v1/auth/logout", bearer=access, json={"refresh_token": refresh})
    if not t.status(r, 200):
        return
    t.status(e2e.anon("POST", "/auth/refresh", json={"refresh_token": refresh}), 401,
             msg="a logged-out refresh token must be dead")
    t.status(_me(e2e, access), 401, msg="the logged-out access token must be dead at once (P02, #189)")
    r = _me(e2e, other)
    t.status(r, 401, msg="logout invalidates the account's other access tokens too (by design, #189)")
    # …while the other device's refresh token survives: one refresh and it carries on.
    t.status(ulog.api("GET", "/users/me"), 200, msg="the other session recovers through /auth/refresh")


def test_logout_bearer_only(t, e2e, ulog):
    """logout with only a bearer keeps the refresh token; no credentials is a no-op"""
    tokens, r = _session(e2e, ulog)
    if tokens is None:
        return t.fail("login failed", r)
    access, refresh = tokens
    r = e2e.anon("POST", "/auth/logout")
    if not t.status(r, 200, msg="logout without any credential"):
        return
    t.status(_me(e2e, access), 200, msg="a logout naming no account must not revoke anything")
    r = e2e.req("POST", f"{e2e.base}/api/v1/auth/logout", bearer=access)
    if not t.status(r, 200):
        return
    t.status(_me(e2e, access), 401, msg="the bearer names the account: its access tokens die")
    r = e2e.anon("POST", "/auth/refresh", json={"refresh_token": refresh})
    t.status(r, 200, msg="a logout without a refresh token must not revoke the refresh token")
    e2e.anon("POST", "/auth/logout", json={"refresh_token": (r.jdict().get("data") or {}).get("refresh_token") or ""})


def test_pw_wrong(t, e2e, st):
    """password change validates the current password"""
    t.need(st, "ua")
    ua = st.ua
    if not t.status(ua.api("GET", "/users/me"), 200, msg="precondition: UA's session is valid"):
        return
    tok, url = ua.token, f"{e2e.base}/api/v1/users/me/password"
    # Exactly this token (e2e.req, not ua.api): a 401 here cannot be a
    # rejected session that the harness silently replaced.
    r = e2e.req("PUT", url, bearer=tok, json={"current_password": f"wrong-{e2e.run_id}", "new_password": "Brand!New12345"})
    t.status(r, 400, 401, 403, msg="a wrong current password must be refused")
    t.true(r.jmsg() not in TOKEN_REJECTED, f"the refusal must come from the password check, not from the "
           f"session (message '{r.jmsg()}')", r)
    t.status(_me(e2e, tok), 200, msg="a refused password change must not revoke the session")
    t.status(e2e.anon("POST", "/auth/login", json={"email": ua.email, "password": "Brand!New12345"}), 401,
             msg="the new password must not have been set")
    t.status(e2e.anon("POST", "/auth/login", json={"email": ua.email, "password": ua.password}), 200,
             msg="the current password must still work")
    r = ua.api("PUT", "/users/me/password", json={"current_password": ua.password, "new_password": "short"})
    t.status(r, 400, msg="a too-short new password")


def test_pw_same(t, st):
    """password change rejects new == current as a validation error"""
    t.need(st, "ua")
    ua = st.ua
    r = ua.api("PUT", "/users/me/password", json={"current_password": ua.password, "new_password": ua.password})
    # ua.api re-authenticates after a revoked token, so a 401 here is the handler's.
    t.true(r.jmsg() not in TOKEN_REJECTED, f"the session must be valid (message '{r.jmsg()}')", r)
    # It is a validation error of the request body, not an authentication
    # failure: 401 makes clients (the SPA included) refresh/re-login (F10).
    t.status(r, 400, msg="new == current is a validation error")


def test_password_max_bytes(t, e2e, st):
    """a new password over 72 bytes (bcrypt's limit) is a 400 naming the rule"""
    # Before the policy had a maximum, register answered "registration failed"
    # and a password change a plain 500: bcrypt refuses more than 72 bytes.
    want = "password must be at most 72 bytes"
    for pw, what in (("Aa1!" + "x" * 69, "73 bytes"), ("Aa1!" + "ä" * 35, "39 characters but 74 bytes")):
        r = e2e.anon("POST", "/auth/register",
                     json={"email": f"{e2e.uniq('long')}@{e2e.domain}", "password": pw, "display_name": "x"})
        if t.status(r, 400, msg=f"register with a password of {what}"):
            t.eq(r.jmsg(), want, f"register with a password of {what}")
    t.need(st, "ua")
    ua = st.ua
    r = ua.api("PUT", "/users/me/password", json={"current_password": ua.password, "new_password": "Aa1!" + "x" * 69})
    t.true(r.jmsg() not in TOKEN_REJECTED, f"the session must be valid (message '{r.jmsg()}')", r)
    if t.status(r, 400, msg="a new password of 73 bytes"):
        t.eq(r.jmsg(), want, "a new password of 73 bytes")


def test_pw_change(t, e2e, st):
    """password change swaps credentials and revokes sessions"""
    t.need(st, "ua")
    ua = st.ua
    tokens, r = _session(e2e, ua)
    if tokens is None:
        return t.fail("login failed", r)
    old_access, old_session_refresh = tokens
    old, new = ua.password, f"{ua.password}-Nw9"
    r = e2e.req("PUT", f"{e2e.base}/api/v1/users/me/password", bearer=old_access,
                json={"current_password": old, "new_password": new})
    if not t.status(r, 200):
        return
    t.nonempty(r, "data.access_token", "password change must hand back a fresh access token")
    ua.password = new
    ua.token = (r.jdict().get("data") or {}).get("access_token") or ""
    ua.refresh = ""
    t.status(_me(e2e, ua.token), 200, msg="the access token handed back by the change works")
    t.status(_me(e2e, old_access), 401, msg="access tokens issued before the change must be revoked (P02)")
    t.status(e2e.anon("POST", "/auth/login", json={"email": ua.email, "password": old}), 401,
             msg="the old password must stop working")
    r = e2e.anon("POST", "/auth/login", json={"email": ua.email, "password": new})
    if not t.status(r, 200, msg="the new password must work"):
        return
    ua._store_tokens(r)
    t.status(e2e.anon("POST", "/auth/refresh", json={"refresh_token": old_session_refresh}), 401,
             msg="sessions opened before the change must be revoked")


def test_forgot(t, e2e, st):
    """forgot-password answers identically for known/unknown e-mail"""
    r = e2e.anon("POST", "/auth/forgot-password", json={"email": f"{e2e.uniq('nobody')}@{e2e.domain}"})
    if not t.status(r, 200):
        return
    unknown_msg = r.jdict().get("data")
    t.need(st, "ua")
    r = e2e.anon("POST", "/auth/forgot-password", json={"email": st.ua.email})
    if not t.status(r, 200):
        return
    t.eq(r.jdict().get("data"), unknown_msg,
         "known and unknown addresses must get the same answer (no account enumeration)")
    t.status(e2e.anon("POST", "/auth/forgot-password", data="nope", ctype="application/json"), 400)


def test_reset_bogus(t, e2e):
    """reset-password / verify reject bogus tokens"""
    t.status(e2e.anon("POST", "/auth/reset-password", json='{"token":"bogus-token","new_password":"Some!Valid12345"}'), 400)
    t.status(e2e.anon("GET", "/auth/verify?token=bogus-token"), 400)
    t.status(e2e.anon("GET", "/auth/verify"), 400)


def test_oauth_surface(t, e2e, u1):
    """OAuth: linked-provider listing and unknown provider"""
    r = u1.api("GET", "/auth/oauth/providers")
    if not t.status(r, 200):
        return
    t.json(r, "has_password", True, "a password account must report has_password")
    t.status_class(e2e.anon("GET", f"/auth/oauth/not-a-provider-{e2e.run_id}"), "4xx")
    t.status(e2e.anon("GET", "/auth/oauth/providers"), 401)


@pytest.mark.skip(reason="needs an external identity provider (Google/Microsoft/custom OIDC) and a browser")
def test_oauth_login():
    """OAuth/OIDC login flow"""


def test_delete_account(t, e2e, st):
    """account deletion: guards, then 204 and the account is gone"""
    st.udel = udel = e2e.create_user("UDEL", "E2E Delete Me")
    udel.ensure_dav()
    email, pw, ap = udel.email, udel.password, udel.apppw
    r = udel.api("DELETE", "/users/me", json={"password": f"wrong-{e2e.run_id}", "confirmation": "DELETE"})
    t.status(r, 401, msg="a wrong password must not delete the account")
    r = udel.api("DELETE", "/users/me", json={"password": pw, "confirmation": "yes please"})
    t.status(r, 400, 401, msg="a wrong confirmation text must not delete the account")
    t.status_class(udel.api("DELETE", "/users/me", json={"password": pw}), "4xx")
    if not t.status(udel.api("GET", "/users/me"), 200, msg="account must survive the failed attempts"):
        return
    if not t.status(udel.api("DELETE", "/users/me", json={"password": pw, "confirmation": "DELETE"}), 204):
        return
    tok = udel.token
    udel.deleted = True
    t.status(e2e.req("GET", f"{e2e.base}/api/v1/users/me", bearer=tok), 401,
             msg="the old access token must die with the account")
    t.status(e2e.anon("POST", "/auth/login", json={"email": email, "password": pw}), 401,
             msg="a deleted account must not log in")
    r = e2e.req("PROPFIND", f"{e2e.base}/dav/", basic=(email, ap), depth=0, xml=PROPFIND_PRINCIPAL)
    t.status(r, 401, msg="the deleted account's app password must stop working")


def test_reregister(t, e2e, st):
    """a deleted account's e-mail can register again"""
    t.need(st, "udel")
    udel = st.udel
    if not udel.deleted:
        t.skip("the throwaway account was not deleted (earlier step failed)")
    r = e2e.anon("POST", "/auth/register",
                 json={"email": udel.email, "password": udel.password, "display_name": "E2E Returning"})
    if r.status in (200, 201):
        # It worked: hand the new account to the cleanup so it is deleted again.
        udel.deleted = False
        udel.token = udel.refresh = ""
        e2e.defer(f"delete re-registered account {udel.email}", udel.delete_account)
    # F19 (fixed): deletion tombstones the e-mail, so this was a 400 "registration failed".
    t.status(r, 200, 201, msg="registering the e-mail of a deleted account")
