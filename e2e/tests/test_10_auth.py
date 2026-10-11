"""Authentication: registration, login, JWT refresh rotation, logout, password
change, forgot/reset/verify edges, OAuth surface, account deletion.
Mutating steps run on a dedicated throwaway user (UA / UDEL) so the shared
U1/U2 sessions of other suites are never disturbed."""

import base64
import re
import time

import pytest

from harness.checks import known
from harness.data import PROPFIND_PRINCIPAL

DESCRIPTION = "register, login, refresh, logout, password change, account deletion"

pytestmark = pytest.mark.usefixtures("u1")

SMTP_REASON = "needs an SMTP server and access to the mailbox (external system)"


def _session(e2e, user):
    """Open an extra login session → (access, refresh) or None."""
    r = e2e.anon("POST", "/auth/login", json={"email": user.email, "password": user.password})
    if r.status != 200:
        return None, r
    d = r.json()["data"]
    return (d["access_token"], d["refresh_token"]), r


def test_register(t, e2e, st):
    """register returns the new account (auto-activated)"""
    st.ua = ua = e2e.create_user("UA", "E2E Auth User")
    # create_user already checked is_active; look at what it stored.
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
    t.json(r, "data.email_verified", True, "with SMTP off accounts are verified on registration")


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


def test_login_shape(t, e2e, u1):
    """login returns bearer tokens with expires_at"""
    r = e2e.anon("POST", "/auth/login", json={"email": u1.email, "password": u1.password})
    if not t.status(r, 200):
        return
    t.json(r, "data.token_type", "Bearer")
    t.nonempty(r, "data.access_token")
    t.nonempty(r, "data.refresh_token")
    now = int(time.time())
    t.jq(r, lambda j: j["data"]["expires_at"] > now, "expires_at must be a future Unix timestamp")
    t.json(r, "data.user.email", u1.email)
    # A JWT has three base64url segments.
    if not re.fullmatch(r"[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+", r.json()["data"].get("access_token") or ""):
        t.fail("access_token does not look like a JWT")
    # Clean up the extra session we just opened.
    u1.api("POST", "/auth/logout", json={"refresh_token": r.json()["data"]["refresh_token"]})


def test_login_wrong(t, e2e, u1):
    """login failure does not reveal whether the account exists"""
    r = e2e.anon("POST", "/auth/login", json={"email": u1.email, "password": f"wrong-{e2e.run_id}"})
    if not t.status(r, 401):
        return
    msg_known = r.json().get("message")
    r = e2e.anon("POST", "/auth/login", json={"email": f"{e2e.uniq('nobody')}@{e2e.domain}", "password": f"wrong-{e2e.run_id}"})
    if not t.status(r, 401):
        return
    t.eq(r.json().get("message"), msg_known, "wrong password and unknown account must produce the same message")


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


def test_refresh(t, e2e, st):
    """refresh rotates the refresh token, old one dies"""
    t.need(st, "ua")
    tokens, r = _session(e2e, st.ua)
    if tokens is None:
        return t.fail("login failed", r)
    first = tokens[1]
    r = e2e.anon("POST", "/auth/refresh", json={"refresh_token": first})
    if not t.status(r, 200):
        return
    t.json(r, "data.token_type", "Bearer")
    second = r.json()["data"]["refresh_token"]
    t.ne(second, first, "refresh must rotate the refresh token")
    t.status(e2e.req("GET", f"{e2e.base}/api/v1/users/me", bearer=r.json()["data"]["access_token"]), 200)
    t.status(e2e.anon("POST", "/auth/refresh", json={"refresh_token": first}), 401,
             msg="a rotated (old) refresh token must be rejected")
    r = e2e.anon("POST", "/auth/refresh", json={"refresh_token": second})
    if not t.status(r, 200, msg="the current refresh token must keep working"):
        return
    e2e.anon("POST", "/auth/logout", json={"refresh_token": r.json()["data"]["refresh_token"]})


def test_refresh_bad(t, e2e):
    """refresh rejects garbage and missing tokens"""
    t.status(e2e.anon("POST", "/auth/refresh", json='{"refresh_token":"clearly-not-a-real-token"}'), 401)
    t.status(e2e.anon("POST", "/auth/refresh", json="{}"), 400, 401)


def test_logout(t, e2e, st):
    """logout revokes the presented refresh token"""
    t.need(st, "ua")
    tokens, r = _session(e2e, st.ua)
    if tokens is None:
        return t.fail("login failed", r)
    access, refresh = tokens
    r = e2e.req("POST", f"{e2e.base}/api/v1/auth/logout", bearer=access, json={"refresh_token": refresh})
    if not t.status(r, 200):
        return
    t.status(e2e.anon("POST", "/auth/refresh", json={"refresh_token": refresh}), 401,
             msg="a logged-out refresh token must be dead")


def test_logout_nobody(t, e2e, st):
    """logout without a body is a harmless no-op"""
    t.need(st, "ua")
    tokens, _ = _session(e2e, st.ua)
    if tokens is None:
        return
    access, refresh = tokens
    r = e2e.req("POST", f"{e2e.base}/api/v1/auth/logout", bearer=access)
    if not t.status(r, 200):
        return
    r = e2e.anon("POST", "/auth/refresh", json={"refresh_token": refresh})
    t.status(r, 200, msg="a body-less logout must not revoke anything")
    e2e.anon("POST", "/auth/logout", json={"refresh_token": (r.json() or {}).get("data", {}).get("refresh_token", "")})


def test_pw_wrong(t, e2e, st):
    """password change validates the current password"""
    t.need(st, "ua")
    ua = st.ua
    r = ua.api("PUT", "/users/me/password", json={"current_password": f"wrong-{e2e.run_id}", "new_password": "Brand!New12345"})
    if not t.status(r, 400, 401, 403):
        return
    r = ua.api("PUT", "/users/me/password", json={"current_password": ua.password, "new_password": "short"})
    t.status(r, 400)


@known("F10")
def test_pw_same(t, st):
    """password change rejects new == current as a validation error"""
    t.need(st, "ua")
    ua = st.ua
    r = ua.api("PUT", "/users/me/password", json={"current_password": ua.password, "new_password": ua.password})
    if not t.status_class(r, "4xx", msg="re-using the current password must be refused"):
        return
    # It is a validation error of the request body, not an authentication
    # failure: 401 makes clients (the SPA included) refresh/re-login.
    t.status(r, 400, msg="new == current is a validation error")


def test_pw_change(t, e2e, st):
    """password change swaps credentials and revokes sessions"""
    t.need(st, "ua")
    ua = st.ua
    tokens, _ = _session(e2e, ua)
    if tokens is None:
        return
    old_session_refresh = tokens[1]
    old, new = ua.password, f"{ua.password}-Nw9"
    r = ua.api("PUT", "/users/me/password", json={"current_password": old, "new_password": new})
    if not t.status(r, 200):
        return
    t.nonempty(r, "data.access_token", "password change must hand back a fresh access token")
    ua.password = new
    ua.token = (r.json().get("data") or {}).get("access_token") or ""
    ua.refresh = ""
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
    unknown_msg = r.json().get("data")
    t.need(st, "ua")
    r = e2e.anon("POST", "/auth/forgot-password", json={"email": st.ua.email})
    if not t.status(r, 200):
        return
    t.eq(r.json().get("data"), unknown_msg,
         "known and unknown addresses must get the same answer (no account enumeration)")
    t.status(e2e.anon("POST", "/auth/forgot-password", data="nope", ctype="application/json"), 400)


def test_reset_bogus(t, e2e):
    """reset-password / verify reject bogus tokens"""
    t.status(e2e.anon("POST", "/auth/reset-password", json='{"token":"bogus-token","new_password":"Some!Valid12345"}'), 400)
    t.status(e2e.anon("GET", "/auth/verify?token=bogus-token"), 400)
    t.status(e2e.anon("GET", "/auth/verify"), 400)


@pytest.mark.skip(reason=SMTP_REASON)
def test_reset_roundtrip():
    """password reset e-mail round trip"""


@pytest.mark.skip(reason=SMTP_REASON)
def test_verify_roundtrip():
    """e-mail verification round trip"""


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


@known("F19")
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
        return
    t.status(r, 200, 201, msg="registering the e-mail of a deleted account")
