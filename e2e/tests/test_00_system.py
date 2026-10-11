"""System endpoints: health/readiness probes, public system settings, auth
methods, the open-source attribution list, security headers, routing edges."""

import pytest

from harness import out

DESCRIPTION = "health, readiness, system settings, auth methods, about, headers"

pytestmark = pytest.mark.usefixtures("u1")


def test_health(t, e2e):
    """health returns {status: ok}"""
    r = e2e.raw("GET", "/health")
    if not t.status(r, 200):
        return
    t.json(r, "status", "ok")
    t.header(r, "Content-Type", "application/json")


def test_ready(t, e2e):
    """ready pings the database"""
    r = e2e.raw("GET", "/ready")
    if not t.status(r, 200):
        return
    t.json(r, "status", "ok")
    t.json(r, "data.checks.database", "ok")


def test_settings(t, e2e):
    """system settings are public and well-formed"""
    r = e2e.anon("GET", "/system/settings")
    if not t.status(r, 200):
        return
    t.json(r, "status", "ok")
    t.jq(r, lambda j: {"admin_configured", "smtp_enabled", "registration_enabled"} <= j["data"].keys(),
         "settings must expose admin_configured / smtp_enabled / registration_enabled")
    # The suite registered U1, so the server has at least one user now.
    t.json(r, "data.admin_configured", True, "admin_configured once any account exists")
    t.json(r, "data.registration_enabled", True)


def test_auth_methods(t, e2e):
    """auth methods include local login"""
    r = e2e.anon("GET", "/auth/methods")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([m for m in j["data"]["methods"] if m["id"] == "local" and m["type"] == "local"]) == 1,
         "auth/methods must list the local (email + password) method")
    t.jq(r, lambda j: all(m["url"].startswith("http") for m in j["data"]["methods"] if m["type"] == "oidc"),
         "every OIDC method must carry an absolute url")


def test_about_anon(t, e2e):
    """about/open-source requires authentication"""
    t.status(e2e.anon("GET", "/about/open-source"), 401)


def test_about(t, u1):
    """about/open-source returns the attribution manifest"""
    r = u1.api("GET", "/about/open-source")
    if not t.status(r, 200):
        return
    # Content, not just status: an SPA catch-all would also answer 200.
    t.jq(r, lambda j: len(j["data"]["packages"]) > 0, "manifest must list packages")
    t.jq(r, lambda j: j["data"]["count"] == len(j["data"]["packages"]), "count must match the list")
    t.jq(r, lambda j: all(p["name"] != "" and p["version"] != "" and p["license"] != "" and p["url"] != ""
                          for p in j["data"]["packages"]),
         "every package needs name, version, license and url")
    t.jq(r, lambda j: "github.com/gofiber/fiber/v3" in [p["name"] for p in j["data"]["packages"]],
         "fiber must be attributed")


def test_security_headers(t, e2e):
    """security headers on API responses"""
    r = e2e.raw("GET", "/health")
    if not r.hdr("X-Content-Type-Options") and not r.hdr("X-Frame-Options"):
        t.skip("no security headers present — CALDAV_SECURITY_HEADERS_ENABLED=false on the target "
               "(or stripped by a proxy)")
    t.header(r, "X-Content-Type-Options", "nosniff")
    t.header(r, "X-Frame-Options", "DENY")
    t.header(r, "Referrer-Policy", "strict-origin-when-cross-origin")
    t.header(r, "Permissions-Policy", "camera=()")
    if e2e.cfg.scheme == "https" and not r.hdr("Strict-Transport-Security"):
        out.info("      note: no HSTS header over https (CALDAV_HSTS_ENABLED=false or terminated by a proxy)")


def test_unknown_api(t, e2e, u1):
    """unknown /api route is a JSON-free 404, not the SPA"""
    r = u1.api("GET", f"/definitely-not-a-route-{e2e.run_id}")
    if not t.status(r, 404):
        return
    t.not_contains(r, "<html", "an unknown API path must not fall through to the SPA index.html")
    r = e2e.raw("GET", f"/dav-not-a-route-{e2e.run_id}/x", headers={"Accept": "application/json"})
    # Not under /dav/, not /api: either the SPA (200 html) or 404 — just no 5xx.
    t.status_not(r, 500, 502, 503)


def test_webui(t, e2e):
    """web UI (SPA) is served at /"""
    r = e2e.raw("GET", "/", headers={"Accept": "text/html"})
    if r.status == 404:
        t.skip("no web UI bundled on this deployment (GET / → 404; the Docker image serves the SPA)")
    if not t.status(r, 200):
        return
    t.header(r, "Content-Type", "text/html")
    t.contains(r, "<html")
    # Deep links fall back to the SPA, API-ish paths never do.
    r = e2e.raw("GET", "/calendar", headers={"Accept": "text/html"})
    t.status(r, 200)
    t.contains(r, "<html")
