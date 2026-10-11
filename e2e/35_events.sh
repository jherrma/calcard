#!/usr/bin/env bash
# Events over REST: CRUD, validation, conditional updates, all-day events,
# recurrence (expand, scope=this / this_and_future), moving between calendars.
set -u
SUITE_DESC_events="event CRUD, validation, If-Match, all-day, recurrence scopes, move"

suite_events() {
	ensure_user U1
	ensure_user U2
	mk_calendar EV_CAL "$(uniq events)" || die "events suite: cannot create a calendar"
	mk_calendar EV_CAL2 "$(uniq events-target)" || die "events suite: cannot create a calendar"
	t_run "create returns the event (201)" t_ev_create
	t_run "get returns the stored event" t_ev_get
	t_run "list honours the start/end window" t_ev_list_window
	t_run "list rejects malformed window bounds" t_ev_list_bad
	t_run "PATCH updates only the given fields" t_ev_patch
	t_run "PATCH with a stale If-Match is 412" t_ev_if_match
	t_run "create rejects end before start" t_ev_invalid_range
	t_run "create rejects a missing start/end" t_ev_missing_times
	t_run "stored iCalendar has a UTC DTSTAMP (RFC 5545 3.8.7.2)" t_ev_dtstamp
	t_run "all-day events round-trip as DATE values" t_ev_allday
	t_run "event URL must match its calendar" t_ev_wrong_calendar
	t_run "another user cannot read, change, delete or move it" t_ev_isolation
	t_run "recurrence: expand=true yields every instance" t_ev_rrule_expand
	t_run "recurrence: BYDAY rule lands on the right weekdays" t_ev_rrule_byday
	t_run "recurrence: delete scope=this removes one instance" t_ev_rrule_delete_this
	t_run "recurrence: update scope=this edits one instance" t_ev_rrule_update_this
	t_run "recurrence: update scope=this_and_future splits the series" t_ev_rrule_update_future
	t_run "recurrence: delete scope=this_and_future truncates" t_ev_rrule_delete_future
	t_run "move to another own calendar keeps the UID" t_ev_move
	t_run "delete is 204, then the event is gone" t_ev_delete
}

WIDE='start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z'

# _ev_list CAL QUERY — GET events; leaves the response for jq.
_ev_list() { api GET "/calendars/$1/events?$2"; }

EV_ID='' EV_UID=''
t_ev_create() {
	api POST "/calendars/$EV_CAL_UUID/events" --json "$(jobj summary="Planning $E2E_RUN_ID" description="Plan the quarter" \
		location="Room 4" start=2030-01-15T10:00:00Z end=2030-01-15T11:00:00Z timezone=UTC 'all_day:=false')"
	assert_status 201 || return
	EV_ID=$(jget .id) EV_UID=$(jget .uid)
	assert_jq '.id | test("^[0-9a-f-]{36}$")' "id is the event UUID"
	assert_json_nonempty .uid "an iCalendar UID is generated"
	assert_json .summary "Planning $E2E_RUN_ID"
	assert_json .location "Room 4"
	assert_json .start 2030-01-15T10:00:00Z
	assert_json .end 2030-01-15T11:00:00Z
	assert_json .all_day false
	assert_json .is_recurring false
	assert_json .calendar_id "$EV_CAL_ID"
}

t_ev_get() {
	need EV_ID || return
	api GET "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_status 200 || return
	assert_json .id "$EV_ID"
	assert_json .uid "$EV_UID"
	assert_json .description "Plan the quarter"
	api GET "/calendars/$EV_CAL_UUID/events/00000000-0000-0000-0000-000000000000"
	assert_status 404
}

t_ev_list_window() {
	need EV_ID || return
	_ev_list "$EV_CAL_UUID" "start=2030-01-01T00:00:00Z&end=2030-02-01T00:00:00Z"
	assert_status 200 || return
	assert_jq "[.events[] | select(.id == \"$EV_ID\")] | length == 1" "event inside the window must be listed"
	assert_jq '.count == (.events | length)' "count must match the list"
	_ev_list "$EV_CAL_UUID" "start=2031-01-01T00:00:00Z&end=2031-02-01T00:00:00Z"
	assert_status 200
	assert_jq "[.events[] | select(.id == \"$EV_ID\")] | length == 0" "event outside the window must not be listed"
}

t_ev_list_bad() {
	_ev_list "$EV_CAL_UUID" "start=yesterday"
	assert_status 400
	_ev_list "$EV_CAL_UUID" "end=2030-13-45"
	assert_status 400
	api GET "/calendars/00000000-0000-0000-0000-000000000000/events?$WIDE"
	assert_status 404
}

t_ev_patch() {
	need EV_ID || return
	api PATCH "/calendars/$EV_CAL_UUID/events/$EV_ID" --json "$(jobj summary="Planning (moved) $E2E_RUN_ID" start=2030-01-15T12:00:00Z end=2030-01-15T13:30:00Z)"
	assert_status 200 || return
	assert_json .summary "Planning (moved) $E2E_RUN_ID"
	assert_json .start 2030-01-15T12:00:00Z
	assert_json .end 2030-01-15T13:30:00Z
	assert_json .location "Room 4" "fields not in the PATCH keep their value"
	assert_json .uid "$EV_UID" "the UID never changes"
}

t_ev_if_match() {
	need EV_ID || return
	api PATCH "/calendars/$EV_CAL_UUID/events/$EV_ID" --json '{"summary":"stale write"}' -H 'If-Match: "definitely-stale"'
	assert_status 412 || return
	api GET "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_json .summary "Planning (moved) $E2E_RUN_ID" "a rejected conditional write must not change the event"
	api DELETE "/calendars/$EV_CAL_UUID/events/$EV_ID" -H 'If-Match: "definitely-stale"'
	assert_status 412 "conditional delete with a stale ETag"
}

t_ev_invalid_range() {
	api POST "/calendars/$EV_CAL_UUID/events" --json '{"summary":"backwards","start":"2030-01-01T10:00:00Z","end":"2030-01-01T09:00:00Z"}'
	assert_status 400
	api POST "/calendars/$EV_CAL_UUID/events" --json '{"summary":"bad","start":"tomorrow","end":"2030-01-01T09:00:00Z"}'
	assert_status 400
}

t_ev_missing_times() {
	t_known F13
	api POST "/calendars/$EV_CAL_UUID/events" --json "$(jobj summary="no times $E2E_RUN_ID")"
	if [[ $RESP_STATUS == 201 ]]; then
		local id
		id=$(jget .id)
		t_fail "an event without start/end was accepted and stored at start=$(jget .start)" with-response
		api DELETE "/calendars/$EV_CAL_UUID/events/$id"
		return
	fi
	assert_status 400
}

t_ev_dtstamp() {
	need EV_ID || return
	t_known F04
	api GET "/calendars/$EV_CAL_UUID/export"
	assert_status 200 || return
	local line
	line=$(grep -m1 '^DTSTAMP' <<<"${RESP_BODY//$'\r'/}")
	[[ -n $line ]] || {
		t_fail "exported VEVENT has no DTSTAMP" with-response
		return
	}
	[[ $line =~ ^DTSTAMP:[0-9]{8}T[0-9]{6}Z$ ]] ||
		t_fail "DTSTAMP must be a UTC date-time like DTSTAMP:20300101T000000Z, got '$line'"
}

t_ev_allday() {
	local id
	mk_event id "$EV_CAL_UUID" "All day $E2E_RUN_ID" 2030-02-01T00:00:00Z 2030-02-02T00:00:00Z 'all_day:=true' || {
		t_fail "create failed" with-response
		return
	}
	api GET "/calendars/$EV_CAL_UUID/events/$id"
	assert_json .all_day true || return
	assert_jq '.start | startswith("2030-02-01")' "the date must survive"
	api GET "/calendars/$EV_CAL_UUID/export"
	assert_match "DTSTART;VALUE=DATE:20300201" "all-day events are stored as DATE values"
}

t_ev_wrong_calendar() {
	need EV_ID || return
	t_known F09
	api GET "/calendars/$EV_CAL2_UUID/events/$EV_ID"
	assert_status 404 "an event addressed through a calendar it does not belong to" || return
	api GET "/calendars/00000000-0000-0000-0000-000000000000/events/$EV_ID"
	assert_status 404 "an event addressed through a non-existent calendar"
}

t_ev_isolation() {
	need EV_ID || return
	as U2 api GET "/calendars/$EV_CAL_UUID/events?$WIDE"
	assert_status 404 "U2 listing U1's calendar"
	as U2 api GET "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_status 404 "U2 reading U1's event"
	as U2 api PATCH "/calendars/$EV_CAL_UUID/events/$EV_ID" --json '{"summary":"hijacked"}'
	assert_status 404 "U2 patching U1's event"
	as U2 api DELETE "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_status 404 "U2 deleting U1's event"
	as U2 api POST "/calendars/$EV_CAL_UUID/events" --json '{"summary":"intruder","start":"2030-01-01T10:00:00Z","end":"2030-01-01T11:00:00Z"}'
	assert_status 404 "U2 creating in U1's calendar"
	as U2 api GET /calendars/
	local u2cal
	u2cal=$(jget '.calendars[0].uuid')
	as U2 api POST "/calendars/$EV_CAL_UUID/events/$EV_ID/move" --json "$(jobj target_calendar_id="$u2cal")"
	assert_status 404 "U2 moving U1's event into their own calendar"
	api GET "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_json .summary "Planning (moved) $E2E_RUN_ID" "U1's event must be untouched"
}

# _rrule_event OUTVAR SUMMARY START END RECURRENCE_JSON — create a series in EV_CAL.
_rrule_event() {
	api POST "/calendars/$EV_CAL_UUID/events" --json "$(jobj summary="$2" start="$3" end="$4" timezone=UTC 'all_day:=false' "recurrence:=$5")"
	if [[ $RESP_STATUS != 201 ]]; then
		t_fail "create recurring event failed" with-response
		printf -v "$1" '%s' ''
		return 1
	fi
	printf -v "$1" '%s' "$(jget .id)"
}

# _instances SUMMARY_PREFIX WINDOW — sets INST (jq array of matching instances).
_instances() {
	_ev_list "$EV_CAL_UUID" "$2&expand=true"
	INST=$(jq -c --arg p "$1" '[.events[] | select(.summary | startswith($p))] | sort_by(.start)' <<<"$RESP_BODY")
}

t_ev_rrule_expand() {
	local id
	_rrule_event id "Weekly A $E2E_RUN_ID" 2033-05-02T10:00:00Z 2033-05-02T11:00:00Z '{"frequency":"WEEKLY","count":4}' || return
	_instances "Weekly A $E2E_RUN_ID" "start=2033-05-01T00:00:00Z&end=2033-06-30T00:00:00Z"
	assert_status 200 || return
	assert_eq "$(jq length <<<"$INST")" 4 "COUNT=4 must expand to 4 instances" || return
	assert_eq "$(jq -r '[.[].recurrence_id] | join(",")' <<<"$INST")" \
		"20330502T100000Z,20330509T100000Z,20330516T100000Z,20330523T100000Z" "recurrence ids of the instances"
	assert_eq "$(jq -r 'map(.is_recurring) | all' <<<"$INST")" true "instances are flagged is_recurring"
	_ev_list "$EV_CAL_UUID" "start=2033-05-01T00:00:00Z&end=2033-06-30T00:00:00Z&expand=false"
	assert_jq "[.events[] | select(.id == \"$id\")] | length == 1" "expand=false returns the series once"
}

t_ev_rrule_byday() {
	local id
	_rrule_event id "Byday $E2E_RUN_ID" 2033-09-05T08:00:00Z 2033-09-05T08:30:00Z '{"frequency":"WEEKLY","by_day":["MO","WE"],"count":4}' || return
	_instances "Byday $E2E_RUN_ID" "start=2033-09-01T00:00:00Z&end=2033-10-01T00:00:00Z"
	assert_eq "$(jq -r '[.[].start[0:10]] | join(",")' <<<"$INST")" \
		"2033-09-05,2033-09-07,2033-09-12,2033-09-14" "Mondays and Wednesdays, 4 in total"
}

t_ev_rrule_delete_this() {
	local id
	_rrule_event id "Weekly B $E2E_RUN_ID" 2033-07-04T14:00:00Z 2033-07-04T14:30:00Z '{"frequency":"WEEKLY","count":4}' || return
	api DELETE "/calendars/$EV_CAL_UUID/events/$id?scope=this&recurrence_id=20330711T140000Z"
	assert_status 204 || return
	_instances "Weekly B $E2E_RUN_ID" "start=2033-07-01T00:00:00Z&end=2033-08-31T00:00:00Z"
	assert_eq "$(jq -r '[.[].recurrence_id] | join(",")' <<<"$INST")" \
		"20330704T140000Z,20330718T140000Z,20330725T140000Z" "exactly the second instance is excluded (EXDATE)"
}

t_ev_rrule_update_this() {
	local id
	_rrule_event id "Weekly C $E2E_RUN_ID" 2033-08-01T09:00:00Z 2033-08-01T10:00:00Z '{"frequency":"WEEKLY","count":4}' || return
	api PATCH "/calendars/$EV_CAL_UUID/events/$id?scope=this&recurrence_id=20330808T090000Z" --json "$(jobj summary="Weekly C $E2E_RUN_ID edited")"
	assert_status 200 || return
	_instances "Weekly C $E2E_RUN_ID" "start=2033-07-25T00:00:00Z&end=2033-09-15T00:00:00Z"
	assert_eq "$(jq length <<<"$INST")" 4 "scope=this keeps the instance count"
	assert_eq "$(jq -r '[.[] | select(.recurrence_id == "20330808T090000Z") | .summary] | join("")' <<<"$INST")" \
		"Weekly C $E2E_RUN_ID edited" "the targeted instance carries the edit"
	assert_eq "$(jq -r '[.[] | select(.recurrence_id != "20330808T090000Z") | .summary] | unique | join("")' <<<"$INST")" \
		"Weekly C $E2E_RUN_ID" "every other instance keeps the series summary"
	assert_eq "$(jq -r '[.[] | select(.recurrence_id == "20330808T090000Z") | .start] | join("")' <<<"$INST")" \
		"2033-08-08T09:00:00Z" "the edited instance keeps its own start"
}

t_ev_rrule_update_future() {
	local id
	_rrule_event id "Weekly D $E2E_RUN_ID" 2033-10-03T15:00:00Z 2033-10-03T15:30:00Z '{"frequency":"WEEKLY","count":4}' || return
	api PATCH "/calendars/$EV_CAL_UUID/events/$id?scope=this_and_future&recurrence_id=20331017T150000Z" --json "$(jobj summary="Weekly D $E2E_RUN_ID renamed")"
	assert_status 200 || return
	_instances "Weekly D $E2E_RUN_ID" "start=2033-09-20T00:00:00Z&end=2033-11-15T00:00:00Z"
	assert_eq "$(jq length <<<"$INST")" 4 "a split must preserve the total instance count" || return
	assert_eq "$(jq -r '[.[].summary] | join("|")' <<<"$INST")" \
		"Weekly D $E2E_RUN_ID|Weekly D $E2E_RUN_ID|Weekly D $E2E_RUN_ID renamed|Weekly D $E2E_RUN_ID renamed" \
		"instances before the split keep the old summary, the rest carry the new one"
}

t_ev_rrule_delete_future() {
	local id
	_rrule_event id "Weekly E $E2E_RUN_ID" 2033-11-07T07:00:00Z 2033-11-07T07:30:00Z '{"frequency":"WEEKLY","count":4}' || return
	api DELETE "/calendars/$EV_CAL_UUID/events/$id?scope=this_and_future&recurrence_id=20331121T070000Z"
	assert_status 204 || return
	_instances "Weekly E $E2E_RUN_ID" "start=2033-11-01T00:00:00Z&end=2033-12-31T00:00:00Z"
	assert_eq "$(jq -r '[.[].recurrence_id] | join(",")' <<<"$INST")" \
		"20331107T070000Z,20331114T070000Z" "only the instances before the split remain (UNTIL)"
	api DELETE "/calendars/$EV_CAL_UUID/events/$id?scope=bogus&recurrence_id=20331107T070000Z"
	assert_status 400 "an unknown scope is a client error"
}

t_ev_move() {
	local id uid
	mk_event id "$EV_CAL_UUID" "Mover $E2E_RUN_ID" 2031-08-01T10:00:00Z 2031-08-01T11:00:00Z || {
		t_fail "create failed" with-response
		return
	}
	uid=$(jget .uid)
	api POST "/calendars/$EV_CAL_UUID/events/$id/move" --json "$(jobj target_calendar_id="$EV_CAL2_UUID")"
	assert_status 200 || return
	assert_json .uid "$uid" "the UID survives a move"
	assert_json .calendar_id "$EV_CAL2_ID"
	_ev_list "$EV_CAL_UUID" "$WIDE&expand=false"
	assert_jq "[.events[] | select(.uid == \"$uid\")] | length == 0" "gone from the source calendar"
	_ev_list "$EV_CAL2_UUID" "$WIDE&expand=false"
	assert_jq "[.events[] | select(.uid == \"$uid\")] | length == 1" "present in the target calendar"
	api POST "/calendars/$EV_CAL2_UUID/events/$id/move" --json '{"target_calendar_id":"00000000-0000-0000-0000-000000000000"}'
	assert_status 404 "moving into an unknown calendar"
}

t_ev_delete() {
	need EV_ID || return
	api DELETE "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_status 204 || return
	api GET "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_status 404
	api DELETE "/calendars/$EV_CAL_UUID/events/$EV_ID"
	assert_status 404 "deleting twice"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main events "$@"
fi
