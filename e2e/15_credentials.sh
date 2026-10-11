#!/usr/bin/env bash
# DAV credentials managed over REST: app passwords (scoped, revocable) and the
# dedicated CalDAV / CardDAV credentials (own username, read or read-write).
set -u
SUITE_DESC_credentials="app passwords, CalDAV/CardDAV credentials: CRUD, scopes, revocation"

suite_credentials() {
	ensure_user U1
	ensure_user U2
	t_run "app password: create returns the secret once" t_cred_ap_create
	t_run "app password: list shows metadata, never the secret" t_cred_ap_list
	t_run "app password: authenticates DAV with the e-mail" t_cred_ap_dav_email
	t_run "app password: authenticates DAV with the returned username" t_cred_ap_dav_username
	t_run "app password: caldav-only scope is refused on address books" t_cred_ap_scope_caldav
	t_run "app password: carddav-only scope is refused on calendars" t_cred_ap_scope_carddav
	t_run "app password: another user cannot revoke it (404)" t_cred_ap_foreign_revoke
	t_run "app password: revoke takes effect immediately" t_cred_ap_revoke
	t_run "CalDAV credential: create / list / validation" t_cred_caldav_crud
	t_run "CalDAV credential: read-only may read, not write" t_cred_caldav_readonly
	t_run "CalDAV credential: confined to calendar paths" t_cred_caldav_scope
	t_run "CalDAV credential: duplicate username is a 409" t_cred_caldav_dup
	t_run "CalDAV credential: expired credential is rejected" t_cred_caldav_expired
	t_run "CalDAV credential: revoke takes effect immediately" t_cred_caldav_revoke
	t_run "CardDAV credential: read-write may write contacts" t_cred_carddav_rw
	t_run "CardDAV credential: confined to address-book paths" t_cred_carddav_scope
	t_run "CardDAV credential: mixed-case username authenticates" t_cred_carddav_mixedcase
	t_run "CardDAV credential: revoke takes effect immediately" t_cred_carddav_revoke
}

# _cred_probe USER PASS PATH [METHOD] [BODY] — PROPFIND (default) with Basic auth.
_cred_probe() {
	local u=$1 p=$2 path=$3 m=${4:-PROPFIND}
	if [[ $m == PROPFIND ]]; then
		req PROPFIND "$BASE_URL$path" --basic "$u:$p" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	else
		req "$m" "$BASE_URL$path" --basic "$u:$p" "${@:5}"
	fi
}

AP_ID='' AP_PW='' AP_USER=''
t_cred_ap_create() {
	api POST /app-passwords/ --json "$(jobj name="$(uniq ap-main)" 'scopes:=["caldav","carddav"]')"
	assert_status 200 201 || return
	assert_json .status ok
	AP_ID=$(jget .data.id) AP_PW=$(jget .data.password) AP_USER=$(jget .data.credentials.username)
	defer "revoke app password $(uniq ap-main)" cleanup_api U1 DELETE "/app-passwords/$AP_ID"
	assert_json_nonempty .data.password "the secret must be returned on creation"
	assert_json .data.credentials.password "$AP_PW" "credentials.password mirrors password"
	assert_json .data.credentials.username "$(uget U1 USERNAME)" "credentials.username is the account username"
	assert_json_nonempty .data.credentials.server_url
	assert_jq '.data.scopes == ["caldav","carddav"]'
	((${#AP_PW} >= 16)) || t_fail "app password is suspiciously short (${#AP_PW} chars)"
}

t_cred_ap_list() {
	need AP_ID || return
	api GET /app-passwords/
	assert_status 200 || return
	assert_jq "[.data.app_passwords[] | select(.id == \"$AP_ID\")] | length == 1" "list must contain the new app password"
	assert_not_contains "$AP_PW" "the list must never contain the secret"
	assert_jq '[.data.app_passwords[] | has("password")] | any | not' "no list entry may carry a password field"
}

t_cred_ap_dav_email() {
	need AP_PW || return
	_cred_probe "$(uget U1 EMAIL)" "$AP_PW" /dav/
	assert_status 207 || return
	assert_contains "/dav/$(uget U1 USERNAME)/"
	_cred_probe "$(uget U1 EMAIL)" "wrong-$AP_PW" /dav/
	assert_status 401
	assert_header WWW-Authenticate Basic
}

t_cred_ap_dav_username() {
	need AP_PW AP_USER || return
	# The server tells clients to use this username (credentials.username) and
	# it is the segment of every DAV URL — it has to work for Basic auth.
	[[ $AP_USER =~ [A-Z] ]] && t_known F02
	_cred_probe "$AP_USER" "$AP_PW" /dav/
	assert_status 207 "Basic auth with the username returned by POST /app-passwords"
}

t_cred_ap_scope_caldav() {
	api POST /app-passwords/ --json "$(jobj name="$(uniq ap-caldav)" 'scopes:=["caldav"]')"
	assert_status 200 201 || return
	local id pw
	id=$(jget .data.id) pw=$(jget .data.password)
	defer "revoke app password $(uniq ap-caldav)" cleanup_api U1 DELETE "/app-passwords/$id"
	_cred_probe "$(uget U1 EMAIL)" "$pw" "$(dav_home_cal U1)"
	assert_status 207 "caldav scope on the calendar home"
	_cred_probe "$(uget U1 EMAIL)" "$pw" "$(dav_home_ab U1)"
	assert_status 403 "caldav-only app password on the address-book home"
	_cred_probe "$(uget U1 EMAIL)" "$pw" /dav/
	assert_status 207 "discovery (/dav/) is open to every scope"
	# Revoke right away: every live app password costs a bcrypt per DAV request.
	api DELETE "/app-passwords/$id"
}

t_cred_ap_scope_carddav() {
	api POST /app-passwords/ --json "$(jobj name="$(uniq ap-carddav)" 'scopes:=["carddav"]')"
	assert_status 200 201 || return
	local id pw
	id=$(jget .data.id) pw=$(jget .data.password)
	defer "revoke app password $(uniq ap-carddav)" cleanup_api U1 DELETE "/app-passwords/$id"
	_cred_probe "$(uget U1 EMAIL)" "$pw" "$(dav_home_ab U1)"
	assert_status 207
	_cred_probe "$(uget U1 EMAIL)" "$pw" "$(dav_home_cal U1)"
	assert_status 403
	# Scope is decided by path segment, not substring: "calendars" in an
	# address-book object name must not unlock it for a caldav credential.
	_cred_probe "$(uget U1 EMAIL)" "$pw" "$(dav_home_cal U1)x/addressbooks.ics" GET
	assert_status 403
	api DELETE "/app-passwords/$id"
}

t_cred_ap_foreign_revoke() {
	need AP_ID || return
	as U2 api DELETE "/app-passwords/$AP_ID"
	assert_status 404 || return
	_cred_probe "$(uget U1 EMAIL)" "$AP_PW" /dav/
	assert_status 207 "U1's app password must survive U2's revoke attempt"
}

t_cred_ap_revoke() {
	api POST /app-passwords/ --json "$(jobj name="$(uniq ap-revoke)" 'scopes:=["caldav","carddav"]')"
	assert_status 200 201 || return
	local id pw
	id=$(jget .data.id) pw=$(jget .data.password)
	defer "revoke app password $(uniq ap-revoke)" cleanup_api U1 DELETE "/app-passwords/$id"
	_cred_probe "$(uget U1 EMAIL)" "$pw" /dav/
	assert_status 207 "works before revocation" || return
	api DELETE "/app-passwords/$id"
	assert_status 204 || return
	_cred_probe "$(uget U1 EMAIL)" "$pw" /dav/
	assert_status 401 "a revoked app password must be rejected on the next request"
	api GET /app-passwords/
	assert_jq "[.data.app_passwords[] | select(.id == \"$id\")] | length == 0" "revoked entry must leave the list"
	api DELETE "/app-passwords/$id"
	assert_status 404 "revoking twice is a 404"
}

# Dedicated credential usernames are global, so they carry the run id.
CALCRED_USER='' CALCRED_PW='' CALCRED_ID=''
t_cred_caldav_crud() {
	CALCRED_USER="$(uniq calro)" CALCRED_PW="ReadOnly_$E2E_RUN_ID"
	api POST /caldav-credentials/ --json "$(jobj name="e2e read-only" username="$CALCRED_USER" password="$CALCRED_PW" permission=read)"
	assert_status 201 || return
	CALCRED_ID=$(jget .id)
	defer "revoke CalDAV credential $CALCRED_USER" cleanup_api U1 DELETE "/caldav-credentials/$CALCRED_ID"
	assert_json .username "$CALCRED_USER"
	assert_json .permission read
	assert_jq 'has("password") | not' "the response must not echo the password"
	api GET /caldav-credentials/
	assert_status 200 || return
	assert_jq "[.credentials[] | select(.id == \"$CALCRED_ID\" and .permission == \"read\")] | length == 1"
	assert_not_contains "$CALCRED_PW"
	# Validation
	api POST /caldav-credentials/ --json "$(jobj name=x username=ab password=LongEnough_123)"
	assert_status 400 "username shorter than 3 chars"
	api POST /caldav-credentials/ --json "$(jobj name=x username="$(uniq bad)!" password=LongEnough_123)"
	assert_status 400 "username with forbidden characters"
	api POST /caldav-credentials/ --json "$(jobj name=x username="$(uniq short)" password=short)"
	assert_status 400 "password shorter than 8 chars"
	api POST /caldav-credentials/ --json "$(jobj name=x username="$(uniq perm)" password=LongEnough_123 permission=admin)"
	assert_status 400 "unknown permission"
	api POST /caldav-credentials/ --json "$(jobj name=x username="$(uniq exp)" password=LongEnough_123 expires_at=tomorrow)"
	assert_status 400 "expires_at must be RFC 3339"
}

t_cred_caldav_readonly() {
	need CALCRED_ID || return
	local cal
	api GET /calendars/
	cal=$(jget '[.calendars[] | select(.shared != true and .subscribed != true)][0].path')
	local col
	col="$(dav_home_cal U1)$cal/"
	_cred_probe "$CALCRED_USER" "$CALCRED_PW" "$col"
	assert_status 207 "read-only credential may PROPFIND" || return
	req PUT "$BASE_URL${col}$(uniq ro-write).ics" --basic "$CALCRED_USER:$CALCRED_PW" \
		--ics "$(ics_event "$(uniq ro-write)" "Must not land" 20300101T100000Z 20300101T110000Z)"
	assert_status 403 "read-only credential must not PUT"
	req PROPPATCH "$BASE_URL$col" --basic "$CALCRED_USER:$CALCRED_PW" \
		--xml '<?xml version="1.0"?><d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>hijack</d:displayname></d:prop></d:set></d:propertyupdate>'
	assert_status 403 "read-only credential must not PROPPATCH"
}

t_cred_caldav_scope() {
	need CALCRED_ID || return
	_cred_probe "$CALCRED_USER" "$CALCRED_PW" "/dav/$(uget U1 USERNAME)/"
	assert_status 207 || return
	assert_contains "$(dav_home_cal U1)" "principal must show the calendar home set"
	assert_not_contains "$(dav_home_ab U1)" "a CalDAV credential must not see the address-book home set"
	_cred_probe "$CALCRED_USER" "$CALCRED_PW" "$(dav_home_ab U1)"
	assert_status 403 "a CalDAV credential is confined to calendar paths"
}

t_cred_caldav_dup() {
	need CALCRED_USER || return
	api POST /caldav-credentials/ --json "$(jobj name=dup username="$CALCRED_USER" password=LongEnough_123)"
	assert_status 409 || return
	# Usernames are global: another account cannot take it either.
	as U2 api POST /caldav-credentials/ --json "$(jobj name=dup username="$CALCRED_USER" password=LongEnough_123)"
	assert_status 409 "credential usernames are unique across accounts"
}

t_cred_caldav_expired() {
	local u pw="Expired_$E2E_RUN_ID"
	u=$(uniq calexp)
	api POST /caldav-credentials/ --json "$(jobj name=expired username="$u" password="$pw" permission=read expires_at=2020-01-01T00:00:00Z)"
	if [[ $RESP_STATUS == 400 ]]; then
		return 0 # refusing a past expiry up front is fine too
	fi
	assert_status 201 || return
	defer "revoke CalDAV credential $u" cleanup_api U1 DELETE "/caldav-credentials/$(jget .id)"
	_cred_probe "$u" "$pw" /dav/
	assert_status 401 "an expired credential must not authenticate"
}

t_cred_caldav_revoke() {
	need CALCRED_ID || return
	_cred_probe "$CALCRED_USER" "$CALCRED_PW" /dav/
	assert_status 207 || return
	as U2 api DELETE "/caldav-credentials/$CALCRED_ID"
	assert_status 404 "another user cannot revoke it"
	api DELETE "/caldav-credentials/$CALCRED_ID"
	assert_status 204 || return
	_cred_probe "$CALCRED_USER" "$CALCRED_PW" /dav/
	assert_status 401 "revoked credential must stop working"
}

CARDCRED_USER='' CARDCRED_PW='' CARDCRED_ID=''
t_cred_carddav_rw() {
	CARDCRED_USER="$(uniq cardrw)" CARDCRED_PW="ReadWrite_$E2E_RUN_ID"
	api POST /carddav-credentials/ --json "$(jobj name="e2e card rw" username="$CARDCRED_USER" password="$CARDCRED_PW" permission=read-write)"
	assert_status 201 || return
	CARDCRED_ID=$(jget .id)
	defer "revoke CardDAV credential $CARDCRED_USER" cleanup_api U1 DELETE "/carddav-credentials/$CARDCRED_ID"
	assert_json .permission read-write
	mk_addressbook CRED_AB "$(uniq cred-book)" || {
		t_fail "could not create an address book" with-response
		return
	}
	local col uid
	col="$(dav_home_ab U1)$CRED_AB_PATH/" uid=$(uniq cardcred-contact)
	req PUT "$BASE_URL$col$uid.vcf" --basic "$CARDCRED_USER:$CARDCRED_PW" --vcf "$(vcard "$uid" "Cardcred Contact")"
	assert_status 201 204 || return
	api GET "/contacts/search?q=Cardcred"
	assert_contains "$uid" "the contact written with the credential must be visible to the owner"
}

t_cred_carddav_scope() {
	need CARDCRED_ID || return
	_cred_probe "$CARDCRED_USER" "$CARDCRED_PW" "$(dav_home_cal U1)"
	assert_status 403 || return
	_cred_probe "$CARDCRED_USER" "$CARDCRED_PW" "/dav/$(uget U1 USERNAME)/"
	assert_status 207 || return
	assert_not_contains "$(dav_home_cal U1)" "a CardDAV credential must not see the calendar home set"
	assert_contains "$(dav_home_ab U1)"
}

t_cred_carddav_mixedcase() {
	# The create endpoint accepts upper-case usernames ([a-zA-Z0-9_-]); a
	# credential the server accepts must also be usable.
	t_known F02
	local u pw="MixedCase_$E2E_RUN_ID"
	u="Mixed-$(uniq card)"
	api POST /carddav-credentials/ --json "$(jobj name=mixed username="$u" password="$pw" permission=read)"
	assert_status 201 400 || return
	[[ $RESP_STATUS == 400 ]] && return 0 # rejecting it up front would be consistent too
	defer "revoke CardDAV credential $u" cleanup_api U1 DELETE "/carddav-credentials/$(jget .id)"
	_cred_probe "$u" "$pw" "$(dav_home_ab U1)"
	assert_status 207 "Basic auth with the username exactly as created"
}

t_cred_carddav_revoke() {
	need CARDCRED_ID || return
	api DELETE "/carddav-credentials/$CARDCRED_ID"
	assert_status 204 || return
	_cred_probe "$CARDCRED_USER" "$CARDCRED_PW" "$(dav_home_ab U1)"
	assert_status 401
	api GET /carddav-credentials/
	assert_jq "[.credentials[] | select(.id == \"$CARDCRED_ID\")] | length == 0"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main credentials "$@"
fi
