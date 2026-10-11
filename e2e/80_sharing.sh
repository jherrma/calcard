#!/usr/bin/env bash
# Sharing: calendar and address-book shares (REST management, what the sharee
# sees over REST and DAV, read vs read-write enforcement, revocation) and
# public calendar links.
set -u
SUITE_DESC_sharing="calendar + address book shares (REST & DAV), public calendar links"

suite_sharing() {
	ensure_user U1
	ensure_user U2
	user_ensure_dav U1 || die "sharing: no DAV app password for U1"
	user_ensure_dav U2 || die "sharing: no DAV app password for U2"
	mk_calendar SH_CAL "$(uniq shared-cal)" "#445566" || die "sharing: cannot create a calendar"
	mk_event SH_EV "$SH_CAL_UUID" "Owner event $E2E_RUN_ID" 2030-03-03T09:00:00Z 2030-03-03T10:00:00Z || die "sharing: seed failed"
	mk_addressbook SH_AB "$(uniq shared-book)" || die "sharing: cannot create an address book"
	mk_contact SH_CT "$SH_AB_UUID" "Wanda Shared $E2E_RUN_ID" || die "sharing: seed failed"
	# calendar shares
	t_run "calendar share: validation (self, unknown user, bad permission)" t_sh_cal_validation
	t_run "calendar share: create read share by e-mail" t_sh_cal_create
	t_run "calendar share: owner lists it, sharee cannot manage shares" t_sh_cal_list
	t_run "calendar share: duplicate share is refused" t_sh_cal_duplicate
	t_run "sharee sees the calendar over REST (shared, owner, permission)" t_sh_cal_sharee_rest
	t_run "sharee sees the calendar in the DAV home under its UUID" t_sh_cal_sharee_dav
	t_run "read-only sharee: reads work, every write is 403" t_sh_cal_readonly
	t_run "upgrade to read-write: sharee writes over REST and DAV" t_sh_cal_readwrite
	t_run "sharee cannot rename, re-share or delete the calendar" t_sh_cal_sharee_limits
	t_run "revoking the share removes all access" t_sh_cal_revoke
	t_run "deleting a shared calendar removes it for the sharee" t_sh_cal_delete_cascade
	t_run "shares accept the username as identifier" t_sh_by_username
	# address-book shares
	t_run "address-book share: create and list" t_sh_ab_create
	t_run "sharee sees the address book over REST" t_sh_ab_sharee_rest
	t_run "shared address book does not leak the owner's account data" t_sh_ab_no_leak
	t_run "contact search covers shared books (and only those)" t_sh_ab_search
	t_run "sharee reads and writes the shared book over CardDAV" t_sh_ab_sharee_dav
	t_run "read-only address-book share refuses writes" t_sh_ab_readonly
	t_run "revoking the address-book share removes access" t_sh_ab_revoke
	# public links
	t_run "public link: disabled by default" t_sh_pub_default
	t_run "public link: enable serves an anonymous iCalendar feed" t_sh_pub_enable
	t_run "public link: unknown token is 404" t_sh_pub_unknown
	t_run "public link: regenerate invalidates the old URL" t_sh_pub_regenerate
	t_run "public link: only the owner can manage it" t_sh_pub_owner_only
	t_run "public link: disable makes the feed 404" t_sh_pub_disable
	t_run "a deleted account's shares and public links stop serving its data" t_sh_deleted_owner
}

_share_cal() { # [ACTOR=U1] UUID IDENTIFIER PERMISSION
	api POST "/calendars/$1/shares" --json "$(jobj user_identifier="$2" permission="$3")"
}

SH_SHARE_ID=''
t_sh_cal_validation() {
	_share_cal "$SH_CAL_UUID" "$(uget U1 EMAIL)" read
	assert_status 400 "sharing with yourself"
	_share_cal "$SH_CAL_UUID" "$(uniq nobody)@$E2E_EMAIL_DOMAIN" read
	assert_status_class 4xx "sharing with an unknown user"
	_share_cal "$SH_CAL_UUID" "$(uget U2 EMAIL)" admin
	assert_status 400 "unknown permission"
	_share_cal 00000000-0000-0000-0000-000000000000 "$(uget U2 EMAIL)" read
	assert_status 404 "unknown calendar"
	_share_cal "$SH_CAL_ID" "$(uget U2 EMAIL)" read
	assert_status 404 "shares are addressed by calendar UUID, not numeric id"
}

t_sh_cal_create() {
	_share_cal "$SH_CAL_UUID" "$(uget U2 EMAIL)" read
	assert_status 201 || return
	SH_SHARE_ID=$(jget .id)
	defer "revoke calendar share" cleanup_api U1 DELETE "/calendars/$SH_CAL_UUID/shares/$SH_SHARE_ID"
	assert_json .permission read
	assert_json .shared_with.email "$(uget U2 EMAIL)"
	assert_json .calendar_id "$SH_CAL_UUID"
}

t_sh_cal_list() {
	need SH_SHARE_ID || return
	api GET "/calendars/$SH_CAL_UUID/shares"
	assert_status 200 || return
	assert_jq "[.shares[] | select(.id == \"$SH_SHARE_ID\" and .permission == \"read\")] | length == 1"
	as U2 api GET "/calendars/$SH_CAL_UUID/shares"
	assert_status 404 "a sharee cannot list the shares"
}

t_sh_cal_duplicate() {
	need SH_SHARE_ID || return
	_share_cal "$SH_CAL_UUID" "$(uget U2 EMAIL)" read-write
	assert_status_class 4xx "a second share for the same user" || return
	api GET "/calendars/$SH_CAL_UUID/shares"
	assert_jq '.shares | length == 1' "still exactly one share"
}

t_sh_cal_sharee_rest() {
	need SH_SHARE_ID || return
	as U2 api GET /calendars/
	assert_status 200 || return
	assert_jq ".calendars[] | select(.uuid == \"$SH_CAL_UUID\") | .shared == true and .permission == \"read\"" \
		"the shared calendar is listed with shared=true, permission=read" || return
	assert_jq ".calendars[] | select(.uuid == \"$SH_CAL_UUID\") | .owner.display_name == \"$(uget U1 DISPLAY)\"" "with the owner's name"
	as U2 api GET "/calendars/$SH_CAL_UUID/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z"
	assert_status 200 || return
	assert_jq "[.events[] | select(.summary == \"Owner event $E2E_RUN_ID\")] | length == 1" "the sharee reads the events"
}

SH_DAV_COL=''
t_sh_cal_sharee_dav() {
	need SH_SHARE_ID || return
	as U2 dav PROPFIND "$(dav_home_cal U2)" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 207 || return
	SH_DAV_COL="$(dav_home_cal U2)$SH_CAL_UUID/"
	assert_contains "<href>$SH_DAV_COL</href>" "shared calendars appear under /calendars/<uuid>/ in the sharee's home"
	as U2 dav PROPFIND "$SH_DAV_COL" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 207 || return
	assert_xpath "$(lp current-user-privilege-set '**' read)" "read privilege"
	assert_xpath "not($(lp current-user-privilege-set '**' write))" "a read share must not advertise write"
	as U2 dav REPORT "$SH_DAV_COL" --depth 1 --xml "$(sync_body)"
	assert_status 207 || return
	SH_DAV_OBJ=$(xpath "string(($(lp response href))[contains(.,'.ics')][1])")
	[[ $SH_DAV_OBJ == "$SH_DAV_COL"* ]] || t_fail "sync must list hrefs inside the shared collection, got '$SH_DAV_OBJ'"
	as U2 dav GET "$SH_DAV_OBJ"
	assert_status 200
	assert_contains "SUMMARY:Owner event $E2E_RUN_ID"
}

t_sh_cal_readonly() {
	need SH_DAV_COL SH_DAV_OBJ || return
	as U2 api POST "/calendars/$SH_CAL_UUID/events" --json '{"summary":"x","start":"2030-03-04T09:00:00Z","end":"2030-03-04T10:00:00Z"}'
	assert_status 403 "REST create"
	as U2 api PATCH "/calendars/$SH_CAL_UUID/events/$SH_EV" --json '{"summary":"hijack"}'
	assert_status 403 "REST update"
	as U2 api DELETE "/calendars/$SH_CAL_UUID/events/$SH_EV"
	assert_status 403 "REST delete"
	as U2 dav PUT "${SH_DAV_COL}$(uniq ro-put).ics" --ics "$(ics_event "$(uniq ro-put)" "ro" 20300304T090000Z 20300304T100000Z)"
	assert_status 403 "DAV PUT"
	as U2 dav DELETE "$SH_DAV_OBJ"
	assert_status 403 "DAV DELETE"
	api GET "/calendars/$SH_CAL_UUID/events/$SH_EV"
	assert_json .summary "Owner event $E2E_RUN_ID" "the owner's event must be untouched"
}

t_sh_cal_readwrite() {
	need SH_SHARE_ID SH_DAV_COL || return
	api PATCH "/calendars/$SH_CAL_UUID/shares/$SH_SHARE_ID" --json '{"permission":"read-write"}'
	assert_status 200 || return
	assert_json .permission read-write
	local uid
	uid="$(uniq by-sharee)"
	as U2 dav PUT "${SH_DAV_COL}$uid.ics" --ics "$(ics_event "$uid@e2e" "Put by sharee" 20300305T090000Z 20300305T100000Z)"
	assert_status 201 204 "DAV PUT by a read-write sharee" || return
	as U2 api POST "/calendars/$SH_CAL_UUID/events" --json '{"summary":"REST by sharee","start":"2030-03-06T09:00:00Z","end":"2030-03-06T10:00:00Z"}'
	assert_status 201 "REST create by a read-write sharee"
	api GET "/calendars/$SH_CAL_UUID/events?start=2030-03-01T00:00:00Z&end=2030-04-01T00:00:00Z"
	assert_jq '[.events[].summary] | (index("Put by sharee") != null) and (index("REST by sharee") != null)' \
		"the owner sees what the sharee wrote"
	as U2 dav PROPFIND "$SH_DAV_COL" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_xpath "$(lp current-user-privilege-set '**' write)" "a read-write share advertises write"
}

t_sh_cal_sharee_limits() {
	need SH_SHARE_ID || return
	as U2 api PATCH "/calendars/$SH_CAL_UUID" --json '{"name":"renamed by sharee"}'
	assert_status_class 4xx "sharee renaming the calendar"
	as U2 dav PROPPATCH "$SH_DAV_COL" --xml '<?xml version="1.0" encoding="utf-8"?><d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>renamed by sharee</d:displayname></d:prop></d:set></d:propertyupdate>'
	assert_status 403 "sharee PROPPATCH on the shared collection"
	as U2 api POST "/calendars/$SH_CAL_UUID/shares" --json "$(jobj user_identifier="$(uget U1 EMAIL)" permission=read)"
	assert_status 404 "a sharee cannot create shares"
	as U2 api DELETE "/calendars/$SH_CAL_UUID" --json '{"confirmation":"DELETE"}'
	assert_status_class 4xx "sharee deleting the calendar"
	as U2 api POST "/calendars/$SH_CAL_UUID/public" --json '{"enabled":true}'
	assert_status 404 "a sharee cannot publish the calendar"
	api GET "/calendars/$SH_CAL_UUID"
	assert_status 200 || return
	assert_json .name "$(uniq shared-cal)" "the calendar must be untouched"
}

t_sh_cal_revoke() {
	need SH_SHARE_ID || return
	as U2 api DELETE "/calendars/$SH_CAL_UUID/shares/$SH_SHARE_ID"
	assert_status 404 "the sharee cannot revoke (manage) shares"
	api DELETE "/calendars/$SH_CAL_UUID/shares/$SH_SHARE_ID"
	assert_status 204 || return
	as U2 api GET /calendars/
	assert_not_contains "$SH_CAL_UUID" "the calendar must leave the sharee's list"
	as U2 api GET "/calendars/$SH_CAL_UUID/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z"
	assert_status 404 "REST access ends"
	as U2 dav PROPFIND "$SH_DAV_COL" --depth 0 --xml "$PROPFIND_LIST"
	assert_status 404 "DAV access ends"
	_share_cal "$SH_CAL_UUID" "$(uget U2 EMAIL)" read
	assert_status 201 "re-sharing after a revoke works" || return
	api DELETE "/calendars/$SH_CAL_UUID/shares/$(jget .id)"
}

t_sh_cal_delete_cascade() {
	mk_calendar SH_DOOM "$(uniq doomed-shared)" || return
	_share_cal "$SH_DOOM_UUID" "$(uget U2 EMAIL)" read
	assert_status 201 || return
	api DELETE "/calendars/$SH_DOOM_UUID" --json '{"confirmation":"DELETE"}'
	assert_status 204 || return
	as U2 api GET /calendars/
	assert_not_contains "$SH_DOOM_UUID" "a deleted calendar must not linger in the sharee's list"
	as U2 dav PROPFIND "$(dav_home_cal U2)" --depth 1 --xml "$PROPFIND_LIST"
	assert_not_contains "$SH_DOOM_UUID" "nor in the sharee's DAV home"
}

t_sh_by_username() {
	# Usernames are mixed-case; the lookup lower-cases them (F02).
	[[ $(uget U2 USERNAME) =~ [A-Z] ]] && t_known F02
	mk_calendar SH_UN "$(uniq by-username)" || return
	_share_cal "$SH_UN_UUID" "$(uget U2 USERNAME)" read
	assert_status 201 "user_identifier may be the account username" || return
	assert_json .shared_with.email "$(uget U2 EMAIL)"
}

# --- address books ---------------------------------------------------------

SH_AB_SHARE=''
t_sh_ab_create() {
	api POST "/addressbooks/$SH_AB_UUID/shares" --json "$(jobj user_identifier="$(uget U2 EMAIL)" permission=read-write)"
	assert_status 201 || return
	SH_AB_SHARE=$(jget .id)
	defer "revoke address-book share" cleanup_api U1 DELETE "/addressbooks/$SH_AB_UUID/shares/$SH_AB_SHARE"
	assert_json .permission read-write
	api GET "/addressbooks/$SH_AB_UUID/shares"
	assert_status 200 || return
	assert_jq "[.shares[] | select(.id == \"$SH_AB_SHARE\")] | length == 1"
	as U2 api GET "/addressbooks/$SH_AB_UUID/shares"
	assert_status 404 "a sharee cannot list the shares"
}

t_sh_ab_sharee_rest() {
	need SH_AB_SHARE || return
	as U2 api GET /addressbooks/
	assert_status 200 || return
	assert_jq ".addressbooks[] | select(.UUID == \"$SH_AB_UUID\") | .shared == true and .permission == \"read-write\" and .owner.display_name == \"$(uget U1 DISPLAY)\""
	as U2 api GET "/addressbooks/$SH_AB_UUID/contacts"
	assert_status 200 || return
	assert_jq "[.Contacts[] | select(.id == \"$SH_CT\")] | length == 1" "the sharee lists the owner's contacts"
}

t_sh_ab_no_leak() {
	need SH_AB_SHARE || return
	t_known F01
	local hash_seen=0
	as U2 api GET /addressbooks/
	assert_status 200 || return
	if [[ $RESP_BODY == *'"PasswordHash":"$2'* ]]; then hash_seen=1; fi
	assert_jq "[.addressbooks[] | select(.UUID == \"$SH_AB_UUID\") | .. | objects | has(\"PasswordHash\")] | any | not" \
		"GET /addressbooks must not embed the owner's user record (PasswordHash)"
	assert_not_contains "$(uget U1 EMAIL)" "the owner's e-mail address must not be disclosed to the sharee"
	as U2 api GET "/search?q=$(uniq shared-book)&types=addressbooks"
	assert_status 200 || return
	assert_jq '[.. | objects | has("PasswordHash")] | any | not' "GET /search must not embed the owner's user record"
	((hash_seen)) && t_fail "SECURITY: the response contains the owner's bcrypt password hash"
	return 0
}

t_sh_ab_search() {
	need SH_AB_SHARE || return
	as U2 api GET "/contacts/search?q=Wanda+Shared+$E2E_RUN_ID"
	assert_status 200 || return
	assert_jq "[.contacts[] | select(.id == \"$SH_CT\")] | length == 1" "contacts in a shared book are searchable"
	as U2 api GET "/contacts/search?q=Wanda&addressbook_id=$SH_AB_UUID"
	assert_status 200 "addressbook_id may name a shared book"
}

SH_AB_DAVCOL=''
t_sh_ab_sharee_dav() {
	need SH_AB_SHARE || return
	as U2 dav PROPFIND "$(dav_home_ab U2)" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 207 || return
	assert_contains "$SH_AB_UUID" "the shared book appears in the sharee's CardDAV home"
	SH_AB_DAVCOL="$(dav_home_ab U2)$SH_AB_UUID/"
	as U2 dav PROPFIND "$SH_AB_DAVCOL" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 207 || return
	local uid
	uid="$(uniq ab-by-sharee)"
	as U2 dav PUT "${SH_AB_DAVCOL}$uid.vcf" --vcf "$(vcard "$uid" "Added By Sharee")"
	assert_status 201 204 "a read-write sharee may add cards" || return
	api GET "/addressbooks/$SH_AB_UUID/contacts?limit=200"
	assert_jq '[.Contacts[] | select(.formatted_name == "Added By Sharee")] | length == 1' "the owner sees the new card"
}

t_sh_ab_readonly() {
	need SH_AB_SHARE SH_AB_DAVCOL || return
	api PATCH "/addressbooks/$SH_AB_UUID/shares/$SH_AB_SHARE" --json '{"permission":"read"}'
	assert_status 200 || return
	local uid
	uid="$(uniq ab-ro)"
	as U2 dav PUT "${SH_AB_DAVCOL}$uid.vcf" --vcf "$(vcard "$uid" "Read Only Write")"
	assert_status 403 "DAV PUT on a read share"
	as U2 api POST "/addressbooks/$SH_AB_UUID/contacts" --json '{"formatted_name":"RO REST"}'
	assert_status 403 "REST create on a read share"
	as U2 api DELETE "/addressbooks/$SH_AB_UUID/contacts/$SH_CT"
	assert_status 403 "REST delete on a read share"
	api GET "/addressbooks/$SH_AB_UUID/contacts/$SH_CT"
	assert_status 200 "the owner's contact survives"
}

t_sh_ab_revoke() {
	need SH_AB_SHARE || return
	api DELETE "/addressbooks/$SH_AB_UUID/shares/$SH_AB_SHARE"
	assert_status 204 || return
	as U2 api GET /addressbooks/
	assert_not_contains "$SH_AB_UUID"
	as U2 dav PROPFIND "$(dav_home_ab U2)" --depth 1 --xml "$PROPFIND_LIST"
	assert_not_contains "$SH_AB_UUID" "the book leaves the sharee's CardDAV home"
	as U2 api GET "/contacts/search?q=Wanda+Shared+$E2E_RUN_ID"
	assert_jq '.contacts | length == 0' "and the sharee's contact search"
}

# --- public links ------------------------------------------------------------

SH_PUB_TOKEN=''
t_sh_pub_default() {
	api GET "/calendars/$SH_CAL_UUID/public"
	assert_status 200 || return
	assert_json .enabled false
}

t_sh_pub_enable() {
	api POST "/calendars/$SH_CAL_UUID/public" --json '{"enabled":true}'
	assert_status 200 || return
	defer "disable public link" cleanup_api U1 POST "/calendars/$SH_CAL_UUID/public" --json '{"enabled":false}'
	assert_json .enabled true
	SH_PUB_TOKEN=$(jget .token)
	assert_json_nonempty .public_url
	[[ ${#SH_PUB_TOKEN} -ge 16 ]] || t_fail "the public token is too short to be unguessable: '$SH_PUB_TOKEN'"
	raw GET "/public/calendar/$SH_PUB_TOKEN"
	assert_status 200 "anonymous GET of the feed" || return
	assert_header Content-Type text/calendar
	assert_contains "BEGIN:VCALENDAR"
	assert_contains "SUMMARY:Owner event $E2E_RUN_ID"
	raw GET "/public/calendar/$SH_PUB_TOKEN.ics"
	assert_status 200 "the .ics suffix variant"
	api GET "/calendars/$SH_CAL_UUID/public"
	assert_json .token "$SH_PUB_TOKEN" "GET …/public reports the active token"
}

t_sh_pub_unknown() {
	raw GET "/public/calendar/not-a-real-token-$E2E_RUN_ID"
	assert_status 404
}

t_sh_pub_regenerate() {
	need SH_PUB_TOKEN || return
	api POST "/calendars/$SH_CAL_UUID/public/regenerate"
	assert_status 200 || return
	local new
	new=$(jget .token)
	assert_ne "$new" "$SH_PUB_TOKEN" "regenerate must mint a new token" || return
	raw GET "/public/calendar/$SH_PUB_TOKEN"
	assert_status 404 "the old link must stop working"
	raw GET "/public/calendar/$new"
	assert_status 200 "the new link works"
	SH_PUB_TOKEN=$new
}

t_sh_pub_owner_only() {
	as U2 api GET "/calendars/$SH_CAL_UUID/public"
	assert_status 404 "a stranger cannot read the public status / token"
	as U2 api POST "/calendars/$SH_CAL_UUID/public/regenerate"
	assert_status 404 "a stranger cannot regenerate the token"
}

t_sh_pub_disable() {
	need SH_PUB_TOKEN || return
	api POST "/calendars/$SH_CAL_UUID/public" --json '{"enabled":false}'
	assert_status 200 || return
	assert_json .enabled false
	raw GET "/public/calendar/$SH_PUB_TOKEN"
	assert_status 404 "a disabled link must 404"
}

t_sh_deleted_owner() {
	t_known F18
	user_create UGONE "E2E Leaving Owner" || {
		t_fail "could not create a scratch owner" with-response
		return
	}
	local cal tok
	as UGONE mk_calendar GONE "$(uniq leaving)" || return
	cal=$GONE_UUID
	as UGONE mk_event GONE_EV "$cal" "Private appointment $E2E_RUN_ID" 2030-04-04T09:00:00Z 2030-04-04T10:00:00Z || return
	as UGONE _share_cal "$cal" "$(uget U2 EMAIL)" read
	assert_status 201 || return
	as UGONE api POST "/calendars/$cal/public" --json '{"enabled":true}'
	tok=$(jget .token)
	raw GET "/public/calendar/$tok"
	assert_status 200 "the public link works while the owner exists" || return
	# The server offers no way to clean up after a deleted account, so empty
	# the calendar first: whatever outlives the account is an empty calendar.
	as UGONE api DELETE "/calendars/$cal/events/$GONE_EV"
	as UGONE user_delete UGONE || {
		t_fail "account deletion failed" with-response
		return
	}
	as U2 api GET /calendars/
	assert_not_contains "$cal" "the deleted owner's calendar must leave the sharee's list"
	as U2 api GET "/calendars/$cal/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z"
	assert_status 404 "the sharee must no longer read the deleted owner's calendar"
	raw GET "/public/calendar/$tok"
	assert_status 404 "the public link of a deleted account must stop serving its calendar"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main sharing "$@"
fi
