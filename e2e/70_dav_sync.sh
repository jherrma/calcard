#!/usr/bin/env bash
# WebDAV-Sync (RFC 6578) for calendars and address books: initial sync,
# token progression, deltas for create / modify / delete, invalid tokens,
# truncation with <limit>, ctag changes and REST changes showing up in DAV.
set -u
SUITE_DESC_dav_sync="sync-collection REPORT: tokens, deltas, deletions, limits, ctag"

suite_dav_sync() {
	ensure_user U1
	user_ensure_dav U1 || die "dav_sync: no DAV app password"
	mk_calendar SY_CAL "$(uniq sync-cal)" || die "dav_sync: cannot create a calendar"
	mk_addressbook SY_AB "$(uniq sync-book)" || die "dav_sync: cannot create an address book"
	SY_COL="$(dav_home_cal)$SY_CAL_PATH/"
	SY_ABCOL="$(dav_home_ab)$SY_AB_PATH/"
	t_run "CalDAV: initial sync returns a token and every member" t_sy_cal_initial
	t_run "CalDAV: no changes → empty delta" t_sy_cal_nochange
	t_run "CalDAV: a new object is the only item in the delta" t_sy_cal_create
	t_run "CalDAV: a modified object comes back with its new ETag" t_sy_cal_modify
	t_run "CalDAV: a deleted object is reported 404" t_sy_cal_delete
	t_run "CalDAV: a REST change shows up in the DAV delta" t_sy_cal_rest_change
	t_run "CalDAV: unknown sync-token is 403 valid-sync-token" t_sy_cal_invalid_token
	t_run "CalDAV: <limit> truncates with 507 and resumes" t_sy_cal_limit
	t_run "CalDAV: getctag and the sync-token property change with content" t_sy_cal_ctag
	t_run "CardDAV: initial sync, create / modify / delete deltas" t_sy_ab_cycle
	t_run "CardDAV: unknown sync-token is 403 valid-sync-token" t_sy_ab_invalid_token
}

# _sync COLLECTION [TOKEN] [EXTRA] — run a sync REPORT; sets SY_TOKEN_NEW.
_sync() {
	dav REPORT "$1" --depth 1 --xml "$(sync_body "${2:-}" "${3:-}")"
	SY_TOKEN_NEW=''
	[[ $RESP_STATUS == 207 ]] && SY_TOKEN_NEW=$(xml_text sync-token)
	return 0
}
_put_ev() { dav PUT "${SY_COL}$1.ics" --ics "$(ics_event "$1@e2e" "$2" 20300901T100000Z 20300901T110000Z)"; }
_resp_count() { xpath "count($(lp response))"; }

SY_T0='' SY_T1='' SY_T2='' SY_T3=''
t_sy_cal_initial() {
	_put_ev "$(uniq s-seed)" "Seed"
	assert_status 201 204 || return
	_sync "$SY_COL"
	assert_status 207 || return
	SY_T0=$SY_TOKEN_NEW
	[[ -n $SY_T0 ]] || t_fail "the initial sync must return a sync-token" with-response
	assert_xpath "$(lp response href)[contains(.,'$(uniq s-seed).ics')]" "the seeded object is listed"
	assert_xpath "$(lp response)[contains($(el href),'$(uniq s-seed).ics')]//$(el getetag)" "with its ETag"
}

t_sy_cal_nochange() {
	need SY_T0 || return
	_sync "$SY_COL" "$SY_T0"
	assert_status 207 || return
	assert_eq "$(_resp_count)" 0 "nothing changed, so the delta must be empty"
	[[ -n $SY_TOKEN_NEW ]] || t_fail "an empty delta still carries a sync-token"
}

t_sy_cal_create() {
	need SY_T0 || return
	_put_ev "$(uniq s-new)" "New"
	_sync "$SY_COL" "$SY_T0"
	assert_status 207 || return
	assert_contains "$(uniq s-new).ics" "the delta must carry the new object"
	assert_not_contains "$(uniq s-seed).ics" "the delta must not re-deliver unchanged objects"
	SY_T1=$SY_TOKEN_NEW
	assert_ne "$SY_T1" "$SY_T0" "the sync-token must advance after a change"
}

t_sy_cal_modify() {
	need SY_T1 || return
	dav GET "${SY_COL}$(uniq s-seed).ics"
	local old_etag
	old_etag=$(hdr ETag)
	_put_ev "$(uniq s-seed)" "Seed (modified)"
	_sync "$SY_COL" "$SY_T1"
	assert_status 207 || return
	assert_eq "$(_resp_count)" 1 "exactly the modified object"
	local tag
	tag=$(xpath "string($(lp response getetag))")
	assert_ne "$tag" "$old_etag" "the delta must carry the new ETag"
	SY_T2=$SY_TOKEN_NEW
}

t_sy_cal_delete() {
	need SY_T2 || return
	dav DELETE "${SY_COL}$(uniq s-new).ics"
	assert_status 204 || return
	_sync "$SY_COL" "$SY_T2"
	assert_status 207 || return
	assert_xpath "$(lp response)[contains($(el href),'$(uniq s-new).ics')]/$(el status)[contains(.,'404')]" \
		"a deleted member is reported with status 404 (RFC 6578 3.5.2)"
	SY_T3=$SY_TOKEN_NEW
}

t_sy_cal_rest_change() {
	need SY_T3 || return
	local id
	mk_event id "$SY_CAL_UUID" "From REST $E2E_RUN_ID" 2030-09-02T10:00:00Z 2030-09-02T11:00:00Z || {
		t_fail "REST create failed" with-response
		return
	}
	_sync "$SY_COL" "$SY_T3"
	assert_status 207 || return
	assert_eq "$(_resp_count)" 1 "the REST-created event must be the delta" || return
	local href
	href=$(xpath "string($(lp response href))")
	dav GET "$href"
	assert_contains "SUMMARY:From REST $E2E_RUN_ID"
}

t_sy_cal_invalid_token() {
	_sync "$SY_COL" "data:,not-a-token-$E2E_RUN_ID"
	assert_status 403 || return
	assert_xpath "$(lp error valid-sync-token)" "the error body must name DAV:valid-sync-token"
	_sync "$SY_COL" "http://example.com/foreign-token"
	assert_status 403
}

t_sy_cal_limit() {
	_sync "$SY_COL"
	local base=$SY_TOKEN_NEW
	need base || return
	_put_ev "$(uniq s-lim1)" "Limit 1"
	_put_ev "$(uniq s-lim2)" "Limit 2"
	_put_ev "$(uniq s-lim3)" "Limit 3"
	_sync "$SY_COL" "$base" "<d:limit><d:nresults>2</d:nresults></d:limit>"
	assert_status 207 || return
	assert_xpath "$(lp response status)[contains(.,'507')]" "a truncated result ends with a 507 response for the collection"
	local first=$SY_TOKEN_NEW
	_sync "$SY_COL" "$first"
	assert_status 207 || return
	assert_contains "$(uniq s-lim3).ics" "the follow-up sync must deliver the remainder"
	assert_not_contains "$(uniq s-lim1).ics" "and not repeat what was already delivered"
}

t_sy_cal_ctag() {
	dav PROPFIND "$SY_COL" --depth 0 --xml "$PROPFIND_COLLECTION"
	local ctag1 tok1
	ctag1=$(xml_text getctag) tok1=$(xml_text sync-token)
	_put_ev "$(uniq s-ctag)" "Ctag bump"
	dav PROPFIND "$SY_COL" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 207 || return
	assert_ne "$(xml_text getctag)" "$ctag1" "getctag must change when the collection changes"
	assert_ne "$(xml_text sync-token)" "$tok1" "the sync-token property must change too"
	local prop_tok
	prop_tok=$(xml_text sync-token)
	_sync "$SY_COL"
	assert_eq "$SY_TOKEN_NEW" "$prop_tok" "REPORT and PROPFIND must agree on the current token"
}

t_sy_ab_cycle() {
	local a b
	a=$(uniq sa-1) b=$(uniq sa-2)
	dav PUT "${SY_ABCOL}$a.vcf" --vcf "$(vcard "$a" "Sync First")"
	assert_status 201 204 || return
	_sync "$SY_ABCOL"
	assert_status 207 || return
	local t0=$SY_TOKEN_NEW
	[[ -n $t0 ]] || t_fail "initial CardDAV sync must return a token" with-response
	assert_contains "$a.vcf"
	dav PUT "${SY_ABCOL}$b.vcf" --vcf "$(vcard "$b" "Sync Second")"
	_sync "$SY_ABCOL" "$t0"
	assert_status 207 || return
	assert_contains "$b.vcf" "create delta"
	assert_not_contains "$a.vcf" "unchanged card must not be re-delivered"
	local t1=$SY_TOKEN_NEW
	dav PUT "${SY_ABCOL}$a.vcf" --vcf "$(vcard "$a" "Sync First Modified")"
	_sync "$SY_ABCOL" "$t1"
	assert_contains "$a.vcf" "modify delta"
	assert_not_contains "$b.vcf"
	local t2=$SY_TOKEN_NEW
	dav DELETE "${SY_ABCOL}$b.vcf"
	_sync "$SY_ABCOL" "$t2"
	assert_status 207 || return
	assert_xpath "$(lp response)[contains($(el href),'$b.vcf')]/$(el status)[contains(.,'404')]" "deleted card reported 404"
	local id
	mk_contact id "$SY_AB_UUID" "Sync From REST $E2E_RUN_ID" || return
	_sync "$SY_ABCOL" "$SY_TOKEN_NEW"
	assert_eq "$(_resp_count)" 1 "a REST-created contact must be the CardDAV delta"
}

t_sy_ab_invalid_token() {
	_sync "$SY_ABCOL" "data:,bogus-$E2E_RUN_ID"
	assert_status 403 || return
	assert_xpath "$(lp error valid-sync-token)"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main dav_sync "$@"
fi
