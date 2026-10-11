#!/usr/bin/env bash
# DAV authentication and authorization: 401 shapes, accepted credential kinds,
# and — most important — that one user can never read or write another
# user's DAV collections, objects or homes.
set -u
SUITE_DESC_dav_authz="DAV 401 shapes, credential kinds, cross-user isolation on every verb"

suite_dav_authz() {
	ensure_user U1
	ensure_user U2
	user_ensure_dav U1 || die "dav_authz: no DAV app password for U1"
	user_ensure_dav U2 || die "dav_authz: no DAV app password for U2"
	_az_seed_u2
	t_run "no credentials → 401 with a Basic challenge" t_az_noauth
	t_run "wrong password / unknown user → 401" t_az_wrong
	t_run "malformed Authorization headers → 401, never 5xx" t_az_malformed
	t_run "the account password and a JWT bearer are accepted too" t_az_other_credentials
	t_run "another user's principal URL reveals none of their data" t_az_foreign_principal
	t_run "another user's calendar home leaks nothing" t_az_foreign_home_leak
	t_run "another user's calendar home is refused (403/404)" t_az_foreign_home_status
	t_run "another user's calendar collection: PROPFIND / REPORT refused" t_az_foreign_collection
	t_run "another user's event: GET / PUT / DELETE refused, data intact" t_az_foreign_object
	t_run "another user's calendar: PROPPATCH / MKCALENDAR / DELETE refused" t_az_foreign_collection_writes
	t_run "another user's address book and cards are refused" t_az_foreign_addressbook
	t_run "path traversal out of the own home is refused" t_az_traversal
	t_run "sync-collection on a missing / foreign collection is a 404, not 500" t_az_sync_status
}

# U2 owns one calendar with one event and one address book with one card.
_az_seed_u2() {
	as U2 mk_calendar AZ_CAL "$(uniq u2-private-cal)" || die "dav_authz: seed failed"
	as U2 mk_addressbook AZ_AB "$(uniq u2-private-book)" || die "dav_authz: seed failed"
	AZ_U2=$(uget U2 USERNAME)
	AZ_COL="/dav/$AZ_U2/calendars/$AZ_CAL_PATH/"
	AZ_ABCOL="/dav/$AZ_U2/addressbooks/$AZ_AB_PATH/"
	AZ_EV_UID="$(uniq secret)@e2e"
	AZ_OBJ="${AZ_COL}$(uniq secret).ics"
	as U2 dav PUT "$AZ_OBJ" --ics "$(ics_event "$AZ_EV_UID" "U2 secret meeting" 20300101T100000Z 20300101T110000Z)"
	[[ $RESP_STATUS == 201 || $RESP_STATUS == 204 ]] || die "dav_authz: seeding U2's event failed (HTTP $RESP_STATUS)"
	AZ_CARD="${AZ_ABCOL}$(uniq secret-card).vcf"
	as U2 dav PUT "$AZ_CARD" --vcf "$(vcard "$(uniq secret-card)" "Secret Person")"
	[[ $RESP_STATUS == 201 || $RESP_STATUS == 204 ]] || die "dav_authz: seeding U2's card failed (HTTP $RESP_STATUS)"
}

t_az_noauth() {
	raw PROPFIND /dav/ --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 401 || return
	assert_header WWW-Authenticate Basic
	assert_header WWW-Authenticate realm=
	raw GET "$AZ_OBJ"
	assert_status 401 "object access without credentials"
	raw OPTIONS /dav/
	assert_status 401 "OPTIONS is authenticated as well"
}

t_az_wrong() {
	req PROPFIND "$BASE_URL/dav/" --basic "$(uget U1 EMAIL):wrong-$E2E_RUN_ID" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 401 || return
	assert_header WWW-Authenticate Basic
	req PROPFIND "$BASE_URL/dav/" --basic "$(uniq nobody)@$E2E_EMAIL_DOMAIN:whatever" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 401
	# U2's app password does not work for U1's identity.
	req PROPFIND "$BASE_URL/dav/" --basic "$(uget U1 EMAIL):$(uget U2 APPPW)" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 401 "another user's app password must not authenticate U1"
}

t_az_malformed() {
	local h
	for h in 'Basic' 'Basic !!!not-base64!!!' "Basic $(printf 'no-colon' | base64)" 'Digest username="x"' 'Bearer not.a.jwt' 'Bearer'; do
		raw PROPFIND /dav/ --depth 0 -H "Authorization: $h" --xml "$PROPFIND_PRINCIPAL"
		assert_status 401 "Authorization: $h"
	done
}

t_az_other_credentials() {
	req PROPFIND "$BASE_URL/dav/" --basic "$(uget U1 EMAIL):$(uget U1 PASSWORD)" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 207 "Basic with the account password"
	user_ensure_token U1
	req PROPFIND "$BASE_URL/dav/" --bearer "$(uget U1 TOKEN)" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 207 "Bearer with a JWT access token"
}

t_az_foreign_principal() {
	dav PROPFIND "/dav/$AZ_U2/" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status_not 500 502 503
	assert_not_contains "/dav/$AZ_U2/calendars/" "U2's home set must not be advertised to U1"
	assert_not_contains "/dav/$AZ_U2/addressbooks/"
}

t_az_foreign_home_leak() {
	dav PROPFIND "/dav/$AZ_U2/calendars/" --depth 1 --xml "$PROPFIND_LIST"
	assert_not_contains "$AZ_CAL_PATH" "U2's calendar path must not leak"
	assert_not_contains "u2-private-cal" "U2's calendar name must not leak"
	dav PROPFIND "/dav/$AZ_U2/addressbooks/" --depth 1 --xml "$PROPFIND_LIST"
	assert_not_contains "$AZ_AB_PATH"
	assert_not_contains "u2-private-book"
}

t_az_foreign_home_status() {
	t_known F11
	dav PROPFIND "/dav/$AZ_U2/calendars/" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 403 404 "PROPFIND on another user's calendar home"
	dav PROPFIND "/dav/$AZ_U2/addressbooks/" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 403 404 "PROPFIND on another user's address-book home"
}

t_az_foreign_collection() {
	dav PROPFIND "$AZ_COL" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 403 404 "Depth 0"
	assert_not_contains "u2-private-cal"
	dav PROPFIND "$AZ_COL" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 403 404 "Depth 1"
	assert_not_contains "secret"
	dav REPORT "$AZ_COL" --depth 1 --xml "$(sync_body)"
	assert_status_not 200 207 "sync-collection on U2's calendar" # exact status: see t_az_sync_status
	assert_not_contains "secret"
	dav REPORT "$AZ_COL" --depth 1 --xml '<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-data/></d:prop><c:filter><c:comp-filter name="VCALENDAR"/></c:filter></c:calendar-query>'
	assert_status 403 404 "calendar-query"
	assert_not_contains "U2 secret meeting"
	dav REPORT "$AZ_COL" --depth 1 --xml "<?xml version=\"1.0\" encoding=\"utf-8\"?>
<c:calendar-multiget xmlns:d=\"DAV:\" xmlns:c=\"urn:ietf:params:xml:ns:caldav\"><d:prop><c:calendar-data/></d:prop><d:href>$AZ_OBJ</d:href></c:calendar-multiget>"
	assert_not_contains "U2 secret meeting" "calendar-multiget must not return U2's data"
	# A multiget against U1's OWN collection naming U2's object href.
	mk_calendar AZ_OWN "$(uniq own)" || return
	dav REPORT "$(dav_home_cal)$AZ_OWN_PATH/" --depth 1 --xml "<?xml version=\"1.0\" encoding=\"utf-8\"?>
<c:calendar-multiget xmlns:d=\"DAV:\" xmlns:c=\"urn:ietf:params:xml:ns:caldav\"><d:prop><c:calendar-data/></d:prop><d:href>$AZ_OBJ</d:href></c:calendar-multiget>"
	assert_not_contains "U2 secret meeting" "a multiget through U1's own collection must not fetch U2's object"
}

t_az_foreign_object() {
	dav GET "$AZ_OBJ"
	assert_status 403 404 "GET"
	assert_not_contains "U2 secret meeting"
	dav HEAD "$AZ_OBJ"
	assert_status 403 404 "HEAD"
	dav PUT "$AZ_OBJ" --ics "$(ics_event "$AZ_EV_UID" "Overwritten by U1" 20300101T100000Z 20300101T110000Z)"
	assert_status 403 404 "PUT over U2's event"
	dav PUT "${AZ_COL}$(uniq planted).ics" --ics "$(ics_event "$(uniq planted)" "Planted by U1" 20300101T100000Z 20300101T110000Z)"
	assert_status 403 404 "PUT a new event into U2's calendar"
	dav DELETE "$AZ_OBJ"
	assert_status 403 404 "DELETE U2's event"
	as U2 dav GET "$AZ_OBJ"
	assert_status 200 "U2's event must still exist" || return
	assert_contains "U2 secret meeting" "U2's event must be unchanged"
	as U2 dav GET "${AZ_COL}$(uniq planted).ics"
	assert_status 404 "nothing may have been planted in U2's calendar"
}

t_az_foreign_collection_writes() {
	dav PROPPATCH "$AZ_COL" --xml '<?xml version="1.0" encoding="utf-8"?>
<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>Hijacked</d:displayname></d:prop></d:set></d:propertyupdate>'
	assert_status 403 404 "PROPPATCH on U2's calendar"
	dav MKCALENDAR "/dav/$AZ_U2/calendars/$(uniq planted-cal)/" --xml '<?xml version="1.0" encoding="utf-8"?><c:mkcalendar xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"/>'
	assert_status 403 404 "MKCALENDAR in U2's home"
	dav DELETE "$AZ_COL"
	assert_status 403 404 "DELETE U2's calendar"
	as U2 api GET "/calendars/$AZ_CAL_UUID"
	assert_status 200 "U2's calendar must survive" || return
	assert_json .name "$(uniq u2-private-cal)" "U2's calendar name must be unchanged"
	as U2 api GET /calendars/
	assert_not_contains "$(uniq planted-cal)" "no calendar may have been created for U2"
	api GET /calendars/
	local u
	for u in $(jget ".calendars[] | select(.path == \"$(uniq planted-cal)\") | .uuid"); do
		t_fail "U1's MKCALENDAR in U2's home created a calendar for U1 ($u)"
		api DELETE "/calendars/$u" --json '{"confirmation":"DELETE"}'
	done
}

t_az_foreign_addressbook() {
	dav PROPFIND "$AZ_ABCOL" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 403 404 "PROPFIND on U2's address book"
	assert_not_contains "secret-card"
	dav GET "$AZ_CARD"
	assert_status 403 404 "GET U2's card"
	assert_not_contains "Secret Person"
	dav PUT "$AZ_CARD" --vcf "$(vcard "$(uniq secret-card)" "Overwritten Person")"
	assert_status 403 404 "PUT over U2's card"
	dav DELETE "$AZ_CARD"
	assert_status 403 404 "DELETE U2's card"
	dav REPORT "$AZ_ABCOL" --depth 1 --xml "$(sync_body)"
	assert_status_not 200 207 "sync on U2's address book"
	assert_not_contains "secret-card"
	dav DELETE "$AZ_ABCOL"
	assert_status 403 404 "DELETE U2's address book"
	as U2 dav GET "$AZ_CARD"
	assert_status 200 || return
	assert_contains "FN:Secret Person" "U2's card must be unchanged"
}

t_az_traversal() {
	local own
	own="$(dav_home_cal)"
	dav PROPFIND "${own}../../$AZ_U2/calendars/$AZ_CAL_PATH/" --depth 1 --xml "$PROPFIND_LIST" --path-as-is
	assert_status_class 4xx "dot-dot segments"
	assert_not_contains "secret"
	dav GET "${own}%2e%2e/%2e%2e/$AZ_U2/calendars/$AZ_CAL_PATH/$(uniq secret).ics" --path-as-is
	assert_status_class 4xx "encoded dot-dot segments"
	assert_not_contains "U2 secret meeting"
}

t_az_sync_status() {
	t_known F17
	dav REPORT "$(dav_home_cal)no-such-calendar-$E2E_RUN_ID/" --depth 1 --xml "$(sync_body)"
	assert_status 403 404 "sync-collection on a calendar that does not exist"
	dav REPORT "$AZ_COL" --depth 1 --xml "$(sync_body)"
	assert_status 403 404 "sync-collection on another user's calendar"
	dav REPORT "$AZ_ABCOL" --depth 1 --xml "$(sync_body)"
	assert_status 403 404 "sync-collection on another user's address book"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main dav_authz "$@"
fi
