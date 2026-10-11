#!/usr/bin/env bash
# DAV discovery (RFC 6764 / 4791 / 6352 / 4918): well-known redirects,
# OPTIONS capabilities, principal and home-set PROPFINDs, collection
# properties at Depth 0 and 1.
set -u
SUITE_DESC_dav_discovery="well-known redirects, OPTIONS, principal / home-set / collection PROPFIND"

suite_dav_discovery() {
	ensure_user U1
	user_ensure_dav U1 || die "dav_discovery: no DAV app password"
	mk_calendar DD_CAL "$(uniq disco-cal)" || die "dav_discovery: cannot create a calendar"
	mk_addressbook DD_AB "$(uniq disco-book)" || die "dav_discovery: cannot create an address book"
	DD_USER=$(uget U1 USERNAME)
	t_run "GET /.well-known/caldav and /carddav redirect to /dav/ (301)" t_dd_wellknown_get
	t_run "PROPFIND on /.well-known/* redirects too (iOS/macOS discovery)" t_dd_wellknown_propfind
	t_run "OPTIONS /dav/ advertises DAV 1, 3, calendar-access, addressbook" t_dd_options_root
	t_run "OPTIONS on a calendar collection lists the allowed methods" t_dd_options_collection
	t_run "PROPFIND /dav/ (Depth 0): current-user-principal and both home sets" t_dd_root
	t_run "PROPFIND principal: calendar-home-set and addressbook-home-set" t_dd_principal
	t_run "PROPFIND with an empty body is treated as allprop (RFC 4918 9.1)" t_dd_empty_body
	t_run "calendar home (Depth 1) lists the calendars as calendar collections" t_dd_cal_home
	t_run "calendar home (Depth 0) describes only the home" t_dd_cal_home_depth0
	t_run "calendar collection (Depth 0): resourcetype, sync-token, ctag, reports, privileges" t_dd_cal_collection
	t_run "calendar collection properties agree between Depth 0 and Depth 1" t_dd_depth_consistency
	t_run "address-book home (Depth 1) lists the books as addressbook collections" t_dd_ab_home
	t_run "address-book collection (Depth 0): resourcetype, sync-token, reports" t_dd_ab_collection
	t_run "unknown DAV paths are 404, not 5xx" t_dd_unknown
}

t_dd_wellknown_get() {
	local p
	for p in /.well-known/caldav /.well-known/carddav; do
		raw GET "$p"
		assert_status 301 "$p" || continue
		assert_header_eq Location /dav/ "$p must redirect to /dav/"
	done
}

t_dd_wellknown_propfind() {
	local p
	for p in /.well-known/caldav /.well-known/carddav; do
		raw PROPFIND "$p" --depth 0 --xml "$PROPFIND_PRINCIPAL"
		assert_status 301 "PROPFIND $p" || continue
		assert_header_eq Location /dav/
	done
}

t_dd_options_root() {
	dav OPTIONS /dav/
	assert_status 200 204 || return
	local h
	h=$(hdr DAV)
	[[ $h =~ (^|[ ,])1([ ,]|$) ]] || t_fail "DAV header must advertise class 1, got '$h'"
	[[ $h =~ (^|[ ,])3([ ,]|$) ]] || t_fail "DAV header must advertise class 3, got '$h'"
	[[ $h == *calendar-access* ]] || t_fail "DAV header must advertise calendar-access (CalDAV), got '$h'"
	[[ $h == *addressbook* ]] || t_fail "DAV header must advertise addressbook (CardDAV), got '$h'"
	assert_header Allow PROPFIND
}

t_dd_options_collection() {
	dav OPTIONS "$(dav_home_cal)$DD_CAL_PATH/"
	assert_status 200 204 || return
	assert_header DAV calendar-access
	assert_header Allow PROPFIND
	assert_header Allow REPORT
}

# _dd_principal_checks — shared assertions on a principal PROPFIND response.
_dd_principal_checks() {
	assert_xml_wellformed
	assert_xpath "$(lp response propstat prop current-user-principal href)[normalize-space()='/dav/$DD_USER/']" \
		"current-user-principal must be /dav/$DD_USER/"
	assert_xpath "$(lp calendar-home-set href)[normalize-space()='/dav/$DD_USER/calendars/']" \
		"calendar-home-set must be /dav/$DD_USER/calendars/"
	assert_xpath "$(lp addressbook-home-set href)[normalize-space()='/dav/$DD_USER/addressbooks/']" \
		"addressbook-home-set must be /dav/$DD_USER/addressbooks/"
	assert_contains "/dav/$DD_USER/calendars/"
	assert_contains "/dav/$DD_USER/addressbooks/"
}

t_dd_root() {
	dav PROPFIND /dav/ --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 207 || return
	_dd_principal_checks
}

t_dd_principal() {
	dav PROPFIND "/dav/$DD_USER/" --depth 0 --xml "$PROPFIND_PRINCIPAL"
	assert_status 207 || return
	_dd_principal_checks
	assert_xpath "$(lp response href)[normalize-space()='/dav/$DD_USER/']" "the response must describe the principal URL"
}

t_dd_empty_body() {
	t_known F06
	# Collections already handle this; the principal/root path does not.
	dav PROPFIND "$(dav_home_cal)$DD_CAL_PATH/" --depth 0
	assert_status 207 "empty-body PROPFIND on a calendar collection"
	dav PROPFIND /dav/ --depth 0
	assert_status 207 "empty-body PROPFIND on /dav/ (some clients probe like this)"
	dav PROPFIND "/dav/$DD_USER/" --depth 0
	assert_status 207 "empty-body PROPFIND on the principal"
}

t_dd_cal_home() {
	dav PROPFIND "$(dav_home_cal)" --depth 1 --xml '<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:resourcetype/><d:displayname/><c:supported-calendar-component-set/></d:prop></d:propfind>'
	assert_status 207 || return
	assert_xml_wellformed
	local href
	href="$(dav_home_cal)$DD_CAL_PATH/"
	assert_contains "<href>$href</href>" "the home listing must include the new calendar"
	assert_xpath "$(lp response)[$(el href)='$href']//*[local-name()='resourcetype']/*[local-name()='calendar']" \
		"the calendar must have resourcetype calendar"
	assert_xpath "$(lp response)[$(el href)='$href']//*[local-name()='displayname'][.='$(uniq disco-cal)']" \
		"displayname must be the calendar name"
	assert_xpath "$(lp response)[$(el href)='$href']//*[local-name()='comp'][@name='VEVENT']" \
		"supported-calendar-component-set must contain VEVENT"
	assert_xpath "count($(lp response)) >= 3" "home + Personal + the new calendar"
}

t_dd_cal_home_depth0() {
	dav PROPFIND "$(dav_home_cal)" --depth 0 --xml "$PROPFIND_LIST"
	assert_status 207 || return
	assert_xpath_count "$(lp response)" 1 "Depth 0 must describe the home only"
	assert_not_contains "$DD_CAL_PATH"
}

t_dd_cal_collection() {
	dav PROPFIND "$(dav_home_cal)$DD_CAL_PATH/" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 207 || return
	assert_xml_wellformed
	assert_xpath "$(lp resourcetype collection)" "resourcetype must include DAV:collection"
	assert_xpath "$(lp resourcetype calendar)" "resourcetype must include CALDAV:calendar"
	assert_xpath "$(lp displayname)[.='$(uniq disco-cal)']"
	local tok
	tok=$(xml_text sync-token)
	[[ -n $tok ]] || t_fail "sync-token must be present" with-response
	[[ -n $(xml_text getctag) ]] || t_fail "getctag must be present" with-response
	assert_xpath "$(lp supported-report-set '**' sync-collection)" "sync-collection REPORT advertised"
	assert_xpath "$(lp supported-report-set '**' calendar-multiget)" "calendar-multiget REPORT advertised"
	assert_xpath "$(lp supported-report-set '**' calendar-query)" "calendar-query REPORT advertised"
	assert_xpath "$(lp current-user-privilege-set '**' write)" "the owner has write privilege"
}

t_dd_depth_consistency() {
	t_known F07
	local body='<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/">
<d:prop><d:displayname/><c:calendar-description/><c:supported-calendar-component-set/><d:sync-token/><cs:getctag/></d:prop></d:propfind>'
	local href
	href="$(dav_home_cal)$DD_CAL_PATH/"
	dav PROPFIND "$href" --depth 0 --xml "$body"
	assert_status 207 || return
	local d0=$RESP_BODY
	dav PROPFIND "$(dav_home_cal)" --depth 1 --xml "$body"
	assert_status 207 || return
	local d1=$RESP_BODY p ok0 ok1
	# The same resource must expose the same properties whatever the Depth.
	for p in calendar-description supported-calendar-component-set sync-token getctag; do
		ok0=$(xpath "boolean($(lp response propstat)[contains(*[local-name()='status'],'200')]//*[local-name()='$p'])" "$d0")
		ok1=$(xpath "boolean($(lp response)[*[local-name()='href']='$href']/*[local-name()='propstat'][contains(*[local-name()='status'],'200')]//*[local-name()='$p'])" "$d1")
		[[ $ok0 == true ]] || t_fail "Depth 0 PROPFIND on the collection does not return $p (200)"
		[[ $ok1 == true ]] || t_fail "Depth 1 PROPFIND on the home does not return $p (200) for the same collection"
	done
}

t_dd_ab_home() {
	dav PROPFIND "$(dav_home_ab)" --depth 1 --xml '<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:displayname/></d:prop></d:propfind>'
	assert_status 207 || return
	local href
	href="$(dav_home_ab)$DD_AB_PATH/"
	assert_contains "<href>$href</href>"
	assert_xpath "$(lp response)[$(el href)='$href']//*[local-name()='resourcetype']/*[local-name()='addressbook']" \
		"the book must have resourcetype addressbook"
	assert_xpath "$(lp response)[$(el href)='$href']//*[local-name()='displayname'][.='$(uniq disco-book)']"
}

t_dd_ab_collection() {
	dav PROPFIND "$(dav_home_ab)$DD_AB_PATH/" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 207 || return
	assert_xpath "$(lp resourcetype addressbook)" "resourcetype must include CARDDAV:addressbook"
	[[ -n $(xml_text sync-token) ]] || t_fail "sync-token must be present" with-response
	assert_xpath "$(lp supported-report-set '**' addressbook-multiget)"
	assert_xpath "$(lp supported-report-set '**' addressbook-query)"
	assert_xpath "$(lp supported-report-set '**' sync-collection)"
}

t_dd_unknown() {
	dav PROPFIND "$(dav_home_cal)no-such-calendar-$E2E_RUN_ID/" --depth 0 --xml "$PROPFIND_LIST"
	assert_status 404
	dav GET "$(dav_home_cal)no-such-calendar-$E2E_RUN_ID/x.ics"
	assert_status 404
	dav PROPFIND "$(dav_home_ab)no-such-book-$E2E_RUN_ID/" --depth 0 --xml "$PROPFIND_LIST"
	assert_status 404
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main dav_discovery "$@"
fi
