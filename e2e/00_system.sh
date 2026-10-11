#!/usr/bin/env bash
# System endpoints: health/readiness probes, public system settings, auth
# methods, the open-source attribution list, security headers, routing edges.
set -u
SUITE_DESC_system="health, readiness, system settings, auth methods, about, headers"

suite_system() {
	ensure_user U1
	t_run "health returns {status: ok}" t_sys_health
	t_run "ready pings the database" t_sys_ready
	t_run "system settings are public and well-formed" t_sys_settings
	t_run "auth methods include local login" t_sys_auth_methods
	t_run "about/open-source requires authentication" t_sys_about_anon
	t_run "about/open-source returns the attribution manifest" t_sys_about
	t_run "security headers on API responses" t_sys_security_headers
	t_run "unknown /api route is a JSON-free 404, not the SPA" t_sys_unknown_api
	t_run "web UI (SPA) is served at /" t_sys_webui
}

t_sys_health() {
	raw GET /health
	assert_status 200 || return
	assert_json .status ok
	assert_header Content-Type application/json
}

t_sys_ready() {
	raw GET /ready
	assert_status 200 || return
	assert_json .status ok
	assert_json .data.checks.database ok
}

t_sys_settings() {
	api_anon GET /system/settings
	assert_status 200 || return
	assert_json .status ok
	assert_jq '.data | has("admin_configured") and has("smtp_enabled") and has("registration_enabled")' \
		"settings must expose admin_configured / smtp_enabled / registration_enabled"
	# The suite registered U1, so the server has at least one user now.
	assert_json .data.admin_configured true "admin_configured once any account exists"
	assert_json .data.registration_enabled true
}

t_sys_auth_methods() {
	api_anon GET /auth/methods
	assert_status 200 || return
	assert_jq '[.data.methods[] | select(.id == "local" and .type == "local")] | length == 1' \
		"auth/methods must list the local (email + password) method"
	assert_jq '[.data.methods[] | select(.type == "oidc") | .url | startswith("http")] | all' \
		"every OIDC method must carry an absolute url"
}

t_sys_about_anon() {
	api_anon GET /about/open-source
	assert_status 401
}

t_sys_about() {
	api GET /about/open-source
	assert_status 200 || return
	# Content, not just status: an SPA catch-all would also answer 200.
	assert_jq '.data.packages | length > 0' "manifest must list packages"
	assert_jq '.data.count == (.data.packages | length)' "count must match the list"
	assert_jq '[.data.packages[] | (.name != "" and .version != "" and .license != "" and .url != "")] | all' \
		"every package needs name, version, license and url"
	assert_jq '[.data.packages[].name] | index("github.com/gofiber/fiber/v3") != null' \
		"fiber must be attributed"
}

t_sys_security_headers() {
	raw GET /health
	if [[ -z $(hdr X-Content-Type-Options) && -z $(hdr X-Frame-Options) ]]; then
		t_skip "no security headers present — CALDAV_SECURITY_HEADERS_ENABLED=false on the target (or stripped by a proxy)"
		return
	fi
	assert_header X-Content-Type-Options nosniff
	assert_header X-Frame-Options DENY
	assert_header Referrer-Policy strict-origin-when-cross-origin
	assert_header Permissions-Policy "camera=()"
	if [[ $E2E_TARGET_SCHEME == https ]]; then
		[[ -z $(hdr Strict-Transport-Security) ]] && info "      note: no HSTS header over https (CALDAV_HSTS_ENABLED=false or terminated by a proxy)"
	fi
	return 0
}

t_sys_unknown_api() {
	api GET "/definitely-not-a-route-$E2E_RUN_ID"
	assert_status 404 || return
	assert_not_contains "<html" "an unknown API path must not fall through to the SPA index.html"
	raw GET "/dav-not-a-route-$E2E_RUN_ID/x" -H 'Accept: application/json'
	# Not under /dav/, not /api: either the SPA (200 html) or 404 — just no 5xx.
	assert_status_not 500 502 503
}

t_sys_webui() {
	raw GET / -H 'Accept: text/html'
	if [[ $RESP_STATUS == 404 ]]; then
		t_skip "no web UI bundled on this deployment (GET / → 404; the Docker image serves the SPA)"
		return
	fi
	assert_status 200 || return
	assert_header Content-Type text/html
	assert_contains "<html"
	# Deep links fall back to the SPA, API-ish paths never do.
	raw GET /calendar -H 'Accept: text/html'
	assert_status 200
	assert_contains "<html"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main system "$@"
fi
