#!/usr/bin/env bash
# Remote calendar subscriptions (story 100). URL validation always runs; the
# fetch/refresh part needs a feed the SERVER can reach, served from this
# machine by python3 -m http.server. When the server refuses it (SSRF guard
# for private hosts, http:// not allowed) or cannot reach it, those tests are
# SKIPPED with the reason — never failed.
set -u
SUITE_DESC_subscriptions="calendar subscriptions: validation, fetch, read-only mirror, refresh"

suite_subscriptions() {
	ensure_user U1
	ensure_user U2
	user_ensure_dav U1 || die "subscriptions: no DAV app password"
	api GET /calendar-subscriptions/
	if [[ $RESP_STATUS == 404 ]]; then
		skip_test "calendar subscriptions" "the endpoints are not routed (404) — subscriptions disabled on the target (CALDAV_SUBSCRIPTIONS_ENABLED=false)"
		return 0
	fi
	t_run "list starts empty for a new account" t_sub_list_empty
	t_run "create rejects unusable URLs (scheme, credentials, host)" t_sub_bad_urls
	t_run "create rejects an invalid refresh interval" t_sub_bad_interval
	t_run "create mirrors a feed into a new subscribed calendar" t_sub_create
	t_run "a non-iCalendar URL (landing page) is refused" t_sub_landing_page
	t_run "the mirror is listed as subscribed with the feed's events" t_sub_listed
	t_run "the mirror refuses writes over REST" t_sub_readonly_rest
	t_run "the mirror refuses writes over CalDAV and exposes CS:source" t_sub_readonly_dav
	t_run "refresh applies the feed's changes (created/updated/deleted)" t_sub_refresh
	t_run "settings (name, colour, interval) can be changed" t_sub_patch
	t_run "a failing feed is reported on the subscription (HTTP 200)" t_sub_failing_feed
	t_run "another user can neither see nor touch it" t_sub_isolation
	t_run "delete removes the subscription and its calendar" t_sub_delete
}

FEED_V1=$'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nX-WR-CALNAME:E2E test feed\r\nBEGIN:VEVENT\r\nUID:one@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300301T100000Z\r\nDTEND:20300301T110000Z\r\nSUMMARY:Feed event one\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:two@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300302T100000Z\r\nDTEND:20300302T110000Z\r\nSUMMARY:Feed event two\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n'
FEED_V2=$'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nX-WR-CALNAME:E2E test feed\r\nBEGIN:VEVENT\r\nUID:one@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300301T100000Z\r\nDTEND:20300301T110000Z\r\nSUMMARY:Feed event one (renamed)\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:three@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300303T100000Z\r\nDTEND:20300303T110000Z\r\nSUMMARY:Feed event three\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n'

SUB_ID='' SUB_CAL='' SUB_SKIP=''

_sub_summaries() {
	api GET "/calendars/$1/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z&expand=false"
	jq -r '[.events[].summary] | sort | join("|")' <<<"$RESP_BODY"
}

# _sub_need — skip the running test when the fetch part is unavailable.
_sub_need() {
	if [[ -n $SUB_SKIP ]]; then
		t_skip "$SUB_SKIP"
		return 1
	fi
	need SUB_ID
}

t_sub_list_empty() {
	api GET /calendar-subscriptions/
	assert_status 200 || return
	assert_jq '.subscriptions | length == 0'
}

t_sub_bad_urls() {
	local u
	for u in "ftp://example.com/feed.ics" "https://user:secret-$E2E_RUN_ID@example.com/feed.ics" "https:///feed.ics" "not a url" ""; do
		api POST /calendar-subscriptions/ --json "$(jobj url="$u")"
		assert_status 400 "url '$u'"
		assert_not_contains "secret-$E2E_RUN_ID" "a URL's credentials must never be echoed back"
	done
	api GET /calendar-subscriptions/
	assert_jq '.subscriptions | length == 0' "nothing may have been created"
}

t_sub_bad_interval() {
	api POST /calendar-subscriptions/ --json '{"url":"https://example.invalid/feed.ics","refresh_interval":"1m"}'
	assert_status 400 || return
	assert_contains "refresh interval"
}

t_sub_create() {
	if ! feed_start; then
		SUB_SKIP="no local feed server: $E2E_FEED_UNAVAILABLE"
		t_skip "$SUB_SKIP"
		return
	fi
	feed_put "feed.ics" "$FEED_V1"
	local url="$E2E_FEED_URL/feed.ics"
	api POST /calendar-subscriptions/ --json "$(jobj url="$url" refresh_interval=1h)"
	if [[ $RESP_STATUS == 400 ]]; then
		local msg
		msg=$(jget .message)
		case $msg in
		*"only https"*)
			SUB_SKIP="server refuses plain-http feeds (CALDAV_SUBSCRIPTIONS_ALLOW_INSECURE_URLS=false); the local feed is http://"
			;;
		*"publicly routable"*)
			SUB_SKIP="server refuses private/loopback feed hosts (SSRF guard, CALDAV_SUBSCRIPTIONS_ALLOW_PRIVATE_HOSTS=false); feed at $url"
			;;
		*)
			if [[ $(feed_hits feed.ics) == 0 ]]; then
				SUB_SKIP="the server could not reach the local feed at $url ($msg) — set E2E_FEED_HOST to an address of this machine the server can reach"
			fi
			;;
		esac
		if [[ -n $SUB_SKIP ]]; then
			t_skip "$SUB_SKIP"
			return
		fi
	fi
	assert_status 201 || return
	SUB_ID=$(jget .id) SUB_CAL=$(jget .calendar_id)
	defer "delete subscription" cleanup_api U1 DELETE "/calendar-subscriptions/$SUB_ID"
	assert_json .name "E2E test feed" "the calendar is named from X-WR-CALNAME"
	assert_json .status synced
	assert_json .event_count 2
	assert_json .refresh_interval 1h
	assert_json_nonempty .last_synced_at
	assert_eq "$(feed_hits feed.ics)" 1 "the feed is fetched once, at create time"
}

t_sub_landing_page() {
	[[ -n $SUB_SKIP ]] && {
		t_skip "$SUB_SKIP"
		return
	}
	need E2E_FEED_URL || return
	feed_put "landing.html" "<!DOCTYPE html><html><body>Subscribe!</body></html>"
	api POST /calendar-subscriptions/ --json "$(jobj url="$E2E_FEED_URL/landing.html")"
	assert_status 400 || return
	assert_contains "iCalendar"
}

t_sub_listed() {
	_sub_need || return
	api GET /calendars/
	assert_jq ".calendars[] | select(.uuid == \"$SUB_CAL\") | .subscribed == true" "the mirror carries subscribed=true"
	assert_eq "$(_sub_summaries "$SUB_CAL")" "Feed event one|Feed event two"
	api GET /calendar-subscriptions/
	assert_jq "[.subscriptions[] | select(.id == \"$SUB_ID\")] | length == 1"
}

t_sub_readonly_rest() {
	_sub_need || return
	api POST "/calendars/$SUB_CAL/events" --json '{"summary":"mine","start":"2030-03-05T10:00:00Z","end":"2030-03-05T11:00:00Z"}'
	assert_status 403 "REST create"
	api GET "/calendars/$SUB_CAL/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z"
	local victim
	victim=$(jget '.events[0].id')
	api PATCH "/calendars/$SUB_CAL/events/$victim" --json '{"summary":"edited"}'
	assert_status 403 "REST update"
	api DELETE "/calendars/$SUB_CAL/events/$victim"
	assert_status 403 "REST delete"
	api POST "/calendars/$SUB_CAL/import" --data "$(ics_event "$(uniq imp)" Imported 20300305T100000Z 20300305T110000Z)" --ctype text/calendar
	assert_status_class 4xx "import"
	assert_eq "$(_sub_summaries "$SUB_CAL")" "Feed event one|Feed event two" "the mirror must be unchanged"
}

t_sub_readonly_dav() {
	_sub_need || return
	api GET "/calendars/$SUB_CAL"
	local col
	col="$(dav_home_cal)$(jget .path)/"
	dav PROPFIND "$col" --depth 0 --xml '<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/"><d:prop><d:current-user-privilege-set/><cs:source/><d:resourcetype/></d:prop></d:propfind>'
	assert_status 207 || return
	assert_xpath "$(lp current-user-privilege-set '**' read)"
	assert_xpath "not($(lp current-user-privilege-set '**' write))" "a mirror must not advertise write"
	assert_contains "$E2E_FEED_URL/feed.ics" "CS:source names the feed"
	assert_xpath "$(lp resourcetype calendar)" "it stays a calendar for clients"
	dav PUT "${col}$(uniq intruder).ics" --ics "$(ics_event "$(uniq intruder)" Intruder 20300305T100000Z 20300305T110000Z)"
	assert_status 403 "DAV PUT"
}

t_sub_refresh() {
	_sub_need || return
	feed_put "feed.ics" "$FEED_V2"
	api POST "/calendar-subscriptions/$SUB_ID/refresh"
	assert_status 200 || return
	assert_json .synced true
	assert_json .created 1
	assert_json .updated 1
	assert_json .deleted 1
	assert_json .status synced
	assert_eq "$(_sub_summaries "$SUB_CAL")" "Feed event one (renamed)|Feed event three"
}

t_sub_patch() {
	_sub_need || return
	api PATCH "/calendar-subscriptions/$SUB_ID" --json '{"name":"Renamed feed","color":"#123456","refresh_interval":"6h"}'
	assert_status 200 || return
	assert_json .name "Renamed feed"
	assert_json .color "#123456"
	assert_json .refresh_interval 6h
	api GET "/calendars/$SUB_CAL"
	assert_json .name "Renamed feed" "the calendar follows the subscription name"
	api PATCH "/calendar-subscriptions/$SUB_ID" --json '{"refresh_interval":"15m1s"}'
	assert_status 400 "intervals are a closed set"
}

t_sub_failing_feed() {
	_sub_need || return
	feed_rm "feed.ics"
	api POST "/calendar-subscriptions/$SUB_ID/refresh"
	assert_status 200 "a feed failure is not a request failure" || return
	assert_json .status error
	assert_jq '.error_count >= 1'
	assert_json_nonempty .last_error
	assert_jq ".last_error | contains(\"$E2E_FEED_URL\") | not" "last_error must not repeat the URL (it may carry a secret token)"
	assert_eq "$(_sub_summaries "$SUB_CAL")" "Feed event one (renamed)|Feed event three" "a failed refresh keeps the last good copy"
	feed_put "feed.ics" "$FEED_V2"
}

t_sub_isolation() {
	_sub_need || return
	as U2 api GET /calendar-subscriptions/
	assert_status 200 || return
	assert_jq '.subscriptions | length == 0'
	local m p
	for m in "GET /calendar-subscriptions/$SUB_ID" "PATCH /calendar-subscriptions/$SUB_ID" "DELETE /calendar-subscriptions/$SUB_ID" "POST /calendar-subscriptions/$SUB_ID/refresh"; do
		p=${m#* }
		as U2 api "${m%% *}" "$p" --json '{}'
		assert_status 404 "U2: $m"
	done
}

t_sub_delete() {
	_sub_need || return
	api DELETE "/calendar-subscriptions/$SUB_ID"
	assert_status 204 || return
	api GET "/calendar-subscriptions/$SUB_ID"
	assert_status 404
	api GET /calendars/
	assert_not_contains "$SUB_CAL" "the mirrored calendar goes with the subscription"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main subscriptions "$@"
fi
