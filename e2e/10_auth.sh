#!/usr/bin/env bash
# Authentication: registration, login, JWT refresh rotation, logout, password
# change, forgot/reset/verify edges, OAuth surface, account deletion.
# Mutating steps run on a dedicated throwaway user (UA / UDEL) so the shared
# U1/U2 sessions of other suites are never disturbed.
set -u
SUITE_DESC_auth="register, login, refresh, logout, password change, account deletion"

suite_auth() {
	ensure_user U1
	t_run "register returns the new account (auto-activated)" t_auth_register
	t_run "register rejects a duplicate e-mail (409)" t_auth_register_dup
	t_run "register validates e-mail and password" t_auth_register_validation
	t_run "login returns bearer tokens with expires_at" t_auth_login_shape
	t_run "login failure does not reveal whether the account exists" t_auth_login_wrong
	t_run "protected endpoints reject missing / bad tokens" t_auth_protected
	t_run "refresh rotates the refresh token, old one dies" t_auth_refresh
	t_run "refresh rejects garbage and missing tokens" t_auth_refresh_bad
	t_run "logout revokes the presented refresh token" t_auth_logout
	t_run "logout without a body is a harmless no-op" t_auth_logout_nobody
	t_run "password change validates the current password" t_auth_pw_wrong
	t_run "password change rejects new == current as a validation error" t_auth_pw_same
	t_run "password change swaps credentials and revokes sessions" t_auth_pw_change
	t_run "forgot-password answers identically for known/unknown e-mail" t_auth_forgot
	t_run "reset-password / verify reject bogus tokens" t_auth_reset_bogus
	skip_test "password reset e-mail round trip" "needs an SMTP server and access to the mailbox (external system)"
	skip_test "e-mail verification round trip" "needs an SMTP server and access to the mailbox (external system)"
	t_run "OAuth: linked-provider listing and unknown provider" t_auth_oauth_surface
	skip_test "OAuth/OIDC login flow" "needs an external identity provider (Google/Microsoft/custom OIDC) and a browser"
	t_run "account deletion: guards, then 204 and the account is gone" t_auth_delete_account
	t_run "a deleted account's e-mail can register again" t_auth_reregister
}

t_auth_register() {
	user_create UA "E2E Auth User" || {
		t_fail "could not register a fresh user" with-response
		return
	}
	# user_create already checked is_active; look at what it stored.
	assert_true "username must be a non-empty server-assigned slug (got '$(uget UA USERNAME)')" \
		[ -n "$(uget UA USERNAME)" ]
	[[ $(uget UA USERNAME) =~ ^[A-Za-z0-9]+$ ]] || t_fail "username '$(uget UA USERNAME)' is not alphanumeric"
	[[ $(uget UA ID) =~ ^[0-9a-f-]{36}$ ]] || t_fail "id '$(uget UA ID)' is not a UUID"
	as UA api GET /users/me
	assert_status 200 || return
	assert_json .data.email "$(uget UA EMAIL)"
	assert_json .data.display_name "E2E Auth User"
	assert_json .data.is_active true
	assert_json .data.email_verified true "with SMTP off accounts are verified on registration"
}

t_auth_register_dup() {
	api_anon POST /auth/register --json "$(jobj email="$(uget U1 EMAIL)" password="Another!Pass123" display_name=dup)"
	assert_status 409 || return
	assert_json .status error
}

t_auth_register_validation() {
	api_anon POST /auth/register --json "$(jobj email=not-an-email password="Valid!Pass123" display_name=x)"
	assert_status 400
	api_anon POST /auth/register --json "$(jobj email="$(uniq weak)@$E2E_EMAIL_DOMAIN" password=short display_name=x)"
	assert_status 400
	assert_contains "8" "the error should state the minimum length"
	api_anon POST /auth/register --json '{}'
	assert_status 400
	api_anon POST /auth/register --data '{not json' --ctype application/json
	assert_status 400
}

t_auth_login_shape() {
	api_anon POST /auth/login --json "$(jobj email="$(uget U1 EMAIL)" password="$(uget U1 PASSWORD)")"
	assert_status 200 || return
	assert_json .data.token_type Bearer
	assert_json_nonempty .data.access_token
	assert_json_nonempty .data.refresh_token
	assert_jq ".data.expires_at > $(now_s)" "expires_at must be a future Unix timestamp"
	assert_json .data.user.email "$(uget U1 EMAIL)"
	# A JWT has three base64url segments.
	[[ $(jget .data.access_token) =~ ^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$ ]] ||
		t_fail "access_token does not look like a JWT"
	# Clean up the extra session we just opened.
	local rt
	rt=$(jget .data.refresh_token)
	api POST /auth/logout --json "$(jobj refresh_token="$rt")"
}

t_auth_login_wrong() {
	api_anon POST /auth/login --json "$(jobj email="$(uget U1 EMAIL)" password="wrong-$E2E_RUN_ID")"
	assert_status 401 || return
	local msg_known
	msg_known=$(jget .message)
	api_anon POST /auth/login --json "$(jobj email="$(uniq nobody)@$E2E_EMAIL_DOMAIN" password="wrong-$E2E_RUN_ID")"
	assert_status 401 || return
	assert_eq "$(jget .message)" "$msg_known" "wrong password and unknown account must produce the same message"
}

t_auth_protected() {
	api_anon GET /users/me
	assert_status 401
	req GET "$BASE_URL/api/v1/users/me" --bearer "not.a.jwt"
	assert_status 401
	req GET "$BASE_URL/api/v1/users/me" -H "Authorization: Basic $(printf '%s:%s' "$(uget U1 EMAIL)" "$(uget U1 PASSWORD)" | base64 | tr -d '\n')"
	assert_status 401
	# A token signed with another key must be refused (alg/secret check).
	req GET "$BASE_URL/api/v1/users/me" --bearer "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJleHAiOjQxMDI0NDQ4MDB9.c2lnbmF0dXJlLW5vdC12YWxpZA"
	assert_status 401
}

# _auth_session KEY — open an extra login session; sets S_ACCESS / S_REFRESH.
_auth_session() {
	S_ACCESS='' S_REFRESH=''
	api_anon POST /auth/login --json "$(jobj email="$(uget "$1" EMAIL)" password="$(uget "$1" PASSWORD)")"
	[[ $RESP_STATUS == 200 ]] || return 1
	S_ACCESS=$(jget .data.access_token)
	S_REFRESH=$(jget .data.refresh_token)
}

t_auth_refresh() {
	need UA_EMAIL || return
	_auth_session UA || {
		t_fail "login failed" with-response
		return
	}
	local first=$S_REFRESH
	api_anon POST /auth/refresh --json "$(jobj refresh_token="$first")"
	assert_status 200 || return
	assert_json .data.token_type Bearer
	local second
	second=$(jget .data.refresh_token)
	assert_ne "$second" "$first" "refresh must rotate the refresh token"
	req GET "$BASE_URL/api/v1/users/me" --bearer "$(jget .data.access_token)"
	assert_status 200
	api_anon POST /auth/refresh --json "$(jobj refresh_token="$first")"
	assert_status 401 "a rotated (old) refresh token must be rejected"
	api_anon POST /auth/refresh --json "$(jobj refresh_token="$second")"
	assert_status 200 "the current refresh token must keep working" || return
	api_anon POST /auth/logout --json "$(jobj refresh_token="$(jget .data.refresh_token)")"
}

t_auth_refresh_bad() {
	api_anon POST /auth/refresh --json '{"refresh_token":"clearly-not-a-real-token"}'
	assert_status 401
	api_anon POST /auth/refresh --json '{}'
	assert_status 400 401
}

t_auth_logout() {
	need UA_EMAIL || return
	_auth_session UA || {
		t_fail "login failed" with-response
		return
	}
	req POST "$BASE_URL/api/v1/auth/logout" --bearer "$S_ACCESS" --json "$(jobj refresh_token="$S_REFRESH")"
	assert_status 200 || return
	api_anon POST /auth/refresh --json "$(jobj refresh_token="$S_REFRESH")"
	assert_status 401 "a logged-out refresh token must be dead"
}

t_auth_logout_nobody() {
	need UA_EMAIL || return
	_auth_session UA || return
	req POST "$BASE_URL/api/v1/auth/logout" --bearer "$S_ACCESS"
	assert_status 200 || return
	api_anon POST /auth/refresh --json "$(jobj refresh_token="$S_REFRESH")"
	assert_status 200 "a body-less logout must not revoke anything"
	api_anon POST /auth/logout --json "$(jobj refresh_token="$(jget .data.refresh_token)")"
}

t_auth_pw_wrong() {
	need UA_EMAIL || return
	as UA api PUT /users/me/password --json "$(jobj current_password="wrong-$E2E_RUN_ID" new_password="Brand!New12345")"
	assert_status 400 401 403 || return
	as UA api PUT /users/me/password --json "$(jobj current_password="$(uget UA PASSWORD)" new_password=short)"
	assert_status 400
}

t_auth_pw_same() {
	need UA_EMAIL || return
	t_known F10
	as UA api PUT /users/me/password --json "$(jobj current_password="$(uget UA PASSWORD)" new_password="$(uget UA PASSWORD)")"
	assert_status_class 4xx "re-using the current password must be refused" || return
	# It is a validation error of the request body, not an authentication
	# failure: 401 makes clients (the SPA included) refresh/re-login.
	assert_status 400 "new == current is a validation error"
}

t_auth_pw_change() {
	need UA_EMAIL || return
	_auth_session UA || return
	local old_session_refresh=$S_REFRESH old new
	old=$(uget UA PASSWORD)
	new="$(uget UA PASSWORD)-Nw9"
	as UA api PUT /users/me/password --json "$(jobj current_password="$old" new_password="$new")"
	assert_status 200 || return
	assert_json_nonempty .data.access_token "password change must hand back a fresh access token"
	uset UA PASSWORD "$new"
	uset UA TOKEN "$(jget .data.access_token)"
	uset UA REFRESH ''
	api_anon POST /auth/login --json "$(jobj email="$(uget UA EMAIL)" password="$old")"
	assert_status 401 "the old password must stop working"
	api_anon POST /auth/login --json "$(jobj email="$(uget UA EMAIL)" password="$new")"
	assert_status 200 "the new password must work" || return
	uset UA TOKEN "$(jget .data.access_token)"
	uset UA REFRESH "$(jget .data.refresh_token)"
	uset UA EXP "$(jget .data.expires_at)"
	api_anon POST /auth/refresh --json "$(jobj refresh_token="$old_session_refresh")"
	assert_status 401 "sessions opened before the change must be revoked"
}

t_auth_forgot() {
	api_anon POST /auth/forgot-password --json "$(jobj email="$(uniq nobody)@$E2E_EMAIL_DOMAIN")"
	assert_status 200 || return
	local unknown_msg
	unknown_msg=$(jget .data)
	need UA_EMAIL || return
	api_anon POST /auth/forgot-password --json "$(jobj email="$(uget UA EMAIL)")"
	assert_status 200 || return
	assert_eq "$(jget .data)" "$unknown_msg" "known and unknown addresses must get the same answer (no account enumeration)"
	api_anon POST /auth/forgot-password --data 'nope' --ctype application/json
	assert_status 400
}

t_auth_reset_bogus() {
	api_anon POST /auth/reset-password --json '{"token":"bogus-token","new_password":"Some!Valid12345"}'
	assert_status 400
	api_anon GET "/auth/verify?token=bogus-token"
	assert_status 400
	api_anon GET /auth/verify
	assert_status 400
}

t_auth_oauth_surface() {
	api GET /auth/oauth/providers
	assert_status 200 || return
	assert_json .has_password true "a password account must report has_password"
	api_anon GET "/auth/oauth/not-a-provider-$E2E_RUN_ID"
	assert_status_class 4xx
	api_anon GET /auth/oauth/providers
	assert_status 401
}

t_auth_delete_account() {
	user_create UDEL "E2E Delete Me" || {
		t_fail "could not register the throwaway account" with-response
		return
	}
	user_ensure_dav UDEL
	local email pw ap
	email=$(uget UDEL EMAIL) pw=$(uget UDEL PASSWORD) ap=$(uget UDEL APPPW)
	as UDEL api DELETE /users/me --json "$(jobj password="wrong-$E2E_RUN_ID" confirmation=DELETE)"
	assert_status 401 "a wrong password must not delete the account"
	as UDEL api DELETE /users/me --json "$(jobj password="$pw" confirmation="yes please")"
	assert_status 400 401 "a wrong confirmation text must not delete the account"
	as UDEL api DELETE /users/me --json "$(jobj password="$pw")"
	assert_status_class 4xx
	as UDEL api GET /users/me
	assert_status 200 "account must survive the failed attempts" || return
	as UDEL api DELETE /users/me --json "$(jobj password="$pw" confirmation=DELETE)"
	assert_status 204 || return
	local tok
	tok=$(uget UDEL TOKEN)
	uset UDEL DELETED 1
	req GET "$BASE_URL/api/v1/users/me" --bearer "$tok"
	assert_status 401 "the old access token must die with the account"
	api_anon POST /auth/login --json "$(jobj email="$email" password="$pw")"
	assert_status 401 "a deleted account must not log in"
	req PROPFIND "$BASE_URL/dav/" --basic "$email:$ap" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 401 "the deleted account's app password must stop working"
}

t_auth_reregister() {
	need UDEL_EMAIL || return
	[[ $(uget UDEL DELETED) == 1 ]] || {
		t_skip "the throwaway account was not deleted (earlier step failed)"
		return
	}
	t_known F19
	api_anon POST /auth/register --json "$(jobj email="$(uget UDEL EMAIL)" password="$(uget UDEL PASSWORD)" display_name="E2E Returning")"
	if [[ $RESP_STATUS == 200 || $RESP_STATUS == 201 ]]; then
		# It worked: hand the new account to the fixture so it is deleted again.
		uset UDEL DELETED ''
		uset UDEL TOKEN ''
		uset UDEL REFRESH ''
		defer "delete re-registered account $(uget UDEL EMAIL)" user_delete UDEL
		return 0
	fi
	assert_status 200 201 "registering the e-mail of a deleted account"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main auth "$@"
fi
