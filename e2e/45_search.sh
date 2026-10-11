#!/usr/bin/env bash
# Unified search (GET /api/v1/search): every group, no default date window,
# recurring series as one hit, type filters, explicit bounds, limits, isolation.
set -u
SUITE_DESC_search="unified search across events, contacts, calendars, address books"

suite_search() {
	ensure_user U1
	ensure_user U2
	SR_TOKEN="Expd${E2E_RUN_ID: -8}"
	mk_calendar SR_CAL "$SR_TOKEN Calendar" "#abcdef" || die "search suite: cannot create a calendar"
	mk_addressbook SR_AB "$SR_TOKEN Contacts" || die "search suite: cannot create an address book"
	_sr_seed
	t_run "one query finds events, contacts, calendars and address books" t_sr_all_groups
	t_run "events are found without a date window (±3 years)" t_sr_no_window
	t_run "a recurring series is one hit at its next occurrence" t_sr_recurring
	t_run "types= narrows and marks skipped groups searched=false" t_sr_types
	t_run "explicit start/end bound the event search" t_sr_bounds
	t_run "limit caps each group and reports has_more / max_limit" t_sr_limit
	t_run "short query is 400, anonymous is 401" t_sr_invalid
	t_run "another user finds none of it" t_sr_isolation
}

_sr_seed() {
	local id now_y
	now_y=$(printf '%(%Y)T' -1)
	SR_FUTURE=$((now_y + 3))
	SR_PAST=$((now_y - 3))
	mk_event id "$SR_CAL_UUID" "$SR_TOKEN briefing far ahead" "$SR_FUTURE-06-01T10:00:00Z" "$SR_FUTURE-06-01T11:00:00Z" || warn "search seed failed"
	mk_event id "$SR_CAL_UUID" "$SR_TOKEN debrief long past" "$SR_PAST-06-01T10:00:00Z" "$SR_PAST-06-01T11:00:00Z" || warn "search seed failed"
	mk_event id "$SR_CAL_UUID" "$SR_TOKEN weekly sync" "$((now_y - 1))-01-03T09:00:00Z" "$((now_y - 1))-01-03T10:00:00Z" 'recurrence:={"frequency":"WEEKLY"}' || warn "search seed failed"
	mk_event id "$SR_CAL_UUID" "Unrelated dentist" "$SR_FUTURE-01-01T10:00:00Z" "$SR_FUTURE-01-01T11:00:00Z" || warn "search seed failed"
	mk_contact id "$SR_AB_UUID" "$SR_TOKEN Leader" 'emails:=[{"type":"work","value":"leader@example.test"}]' || warn "search seed failed"
}

_sr() { api GET "/search?$1"; }

t_sr_all_groups() {
	_sr "q=$SR_TOKEN"
	assert_status 200 || return
	assert_json .query "$SR_TOKEN"
	assert_jq '[.events.items[].event.summary] | sort == ["'"$SR_TOKEN"' briefing far ahead", "'"$SR_TOKEN"' debrief long past", "'"$SR_TOKEN"' weekly sync"]' \
		"exactly the three matching events"
	assert_jq '[.contacts.items[].contact.formatted_name] == ["'"$SR_TOKEN"' Leader"]'
	assert_jq '.contacts.items[0].addressbook_name == "'"$SR_TOKEN"' Contacts"' "contact hits carry their book name"
	assert_jq '[.calendars.items[].name] == ["'"$SR_TOKEN"' Calendar"]'
	assert_jq '[.addressbooks.items[].Name] == ["'"$SR_TOKEN"' Contacts"]'
	assert_jq '.events.searched and .contacts.searched and .calendars.searched and .addressbooks.searched'
	assert_json .max_limit 100
	assert_jq '.events.items[0] | has("calendar_uuid") and has("calendar_name") and has("calendar_color")' \
		"event hits are self-contained"
}

t_sr_no_window() {
	# Queries are substring matches, so search for a prefix of the summary.
	_sr "q=$SR_TOKEN+briefing"
	assert_jq '.events.items | length == 1' "an event 3 years ahead"
	_sr "q=$SR_TOKEN+debrief"
	assert_jq '.events.items | length == 1' "an event 3 years back"
}

t_sr_recurring() {
	_sr "q=$SR_TOKEN+weekly&types=events"
	assert_status 200 || return
	assert_jq '.events.items | length == 1' "one hit per series, not one per occurrence" || return
	assert_json '.events.items[0].event.is_recurring' true
	assert_json_nonempty '.events.items[0].event.recurrence_id'
	local start now
	start=$(jget '.events.items[0].event.start')
	now=$(printf '%(%Y-%m-%dT%H:%M:%SZ)T' "$(($(now_s) - 8 * 86400))")
	[[ $start > "$now" ]] || t_fail "expected the upcoming occurrence, got $start"
	assert_json '.events.items[0].calendar_uuid' "$SR_CAL_UUID"
	assert_json '.events.items[0].calendar_color' "#abcdef"
}

t_sr_types() {
	_sr "q=$SR_TOKEN&types=contacts"
	assert_status 200 || return
	assert_json .contacts.searched true
	assert_json .events.searched false "an unsearched group must say so"
	assert_jq '.events.items | length == 0'
	_sr "q=$SR_TOKEN&types=calendars,addressbooks"
	assert_jq '.calendars.searched and .addressbooks.searched and (.events.searched | not) and (.contacts.searched | not)'
}

t_sr_bounds() {
	_sr "q=$SR_TOKEN&types=events&start=$SR_FUTURE-01-01T00:00:00Z&end=$SR_FUTURE-12-31T00:00:00Z"
	assert_status 200 || return
	assert_jq '[.events.items[].event.summary] | index("'"$SR_TOKEN"' briefing far ahead") != null' "in-range event found"
	assert_jq '[.events.items[].event.summary] | index("'"$SR_TOKEN"' debrief long past") == null' "out-of-range event excluded"
}

t_sr_limit() {
	_sr "q=$SR_TOKEN&types=events&limit=1"
	assert_status 200 || return
	assert_jq '.events.items | length == 1'
	assert_json .events.has_more true
	_sr "q=$SR_TOKEN&limit=1000"
	assert_status 200 || return
	assert_jq '.limit <= .max_limit' "limit is clamped to max_limit"
}

t_sr_invalid() {
	_sr "q=e"
	assert_status 400 "queries need at least 2 characters"
	_sr ""
	assert_status 400
	api_anon GET "/search?q=$SR_TOKEN"
	assert_status 401
}

t_sr_isolation() {
	as U2 api GET "/search?q=$SR_TOKEN"
	assert_status 200 || return
	assert_jq '(.events.items | length) == 0 and (.contacts.items | length) == 0 and (.calendars.items | length) == 0 and (.addressbooks.items | length) == 0' \
		"U2 must not see any of U1's data"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main search "$@"
fi
