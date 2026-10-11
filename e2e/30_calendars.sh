#!/usr/bin/env bash
# Calendars over REST: CRUD, validation, ownership isolation, export, the
# "cannot delete your last calendar" guard.
set -u
SUITE_DESC_calendars="calendar CRUD, validation, isolation, export, last-calendar guard"

suite_calendars() {
	ensure_user U1
	ensure_user U2
	t_run "create returns the calendar (201)" t_cal_create
	t_run "create validates name and colour" t_cal_create_invalid
	t_run "list contains the calendar as owned" t_cal_list
	t_run "get by UUID; unknown UUID and numeric id are 404" t_cal_get
	t_run "PATCH renames / recolours / re-describes" t_cal_patch
	t_run "PATCH validates the colour" t_cal_patch_invalid
	t_run "another user cannot read, change or delete it" t_cal_isolation
	t_run "unknown or foreign calendar is a 404 on every verb" t_cal_notfound_status
	t_run "export returns an iCalendar file with the events" t_cal_export
	t_run "delete requires the DELETE confirmation" t_cal_delete
	t_run "the last remaining calendar cannot be deleted" t_cal_last_guard
}

C1_UUID='' C1_ID='' C1_PATH='' C1_NAME=''
t_cal_create() {
	C1_NAME=$(uniq "Work Calendar")
	api POST /calendars/ --json "$(jobj name="$C1_NAME" description="e2e calendar" color="#336699" timezone=Europe/Berlin)"
	assert_status 201 || return
	C1_UUID=$(jget .uuid) C1_ID=$(jget .id) C1_PATH=$(jget .path)
	defer "delete calendar $C1_NAME" cleanup_api U1 DELETE "/calendars/$C1_UUID" --json '{"confirmation":"DELETE"}'
	assert_json .name "$C1_NAME"
	assert_json .color "#336699"
	assert_json .timezone Europe/Berlin
	assert_json .description "e2e calendar"
	assert_json .path "$C1_UUID.ics" "server-created calendars live at <uuid>.ics in DAV"
	assert_jq '.id > 0 and (.uuid | test("^[0-9a-f-]{36}$"))'
	assert_jq '.supported_components | test("VEVENT")'
	assert_json .subscribed false
}

t_cal_create_invalid() {
	api POST /calendars/ --json '{"color":"#336699"}'
	assert_status 400 "a calendar needs a name"
	api POST /calendars/ --json "$(jobj name="$(uniq badcolor)" color=blue)"
	assert_status 400 "colour must be a hex value"
	api POST /calendars/ --data '{"name":' --ctype application/json
	assert_status 400 "malformed JSON"
	# Clean up in case the server accepted any of them.
	api GET /calendars/
	local u
	for u in $(jget ".calendars[] | select(.name == \"$(uniq badcolor)\") | .uuid"); do
		api DELETE "/calendars/$u" --json '{"confirmation":"DELETE"}'
	done
}

t_cal_list() {
	need C1_UUID || return
	api GET /calendars/
	assert_status 200 || return
	assert_jq ".calendars | map(select(.uuid == \"$C1_UUID\")) | length == 1"
	assert_jq ".calendars[] | select(.uuid == \"$C1_UUID\") | .shared == false and .permission == \"owner\"" \
		"an owned calendar is shared=false, permission=owner"
	assert_jq '[.calendars[] | select(.name == "Personal")] | length >= 1' "registration seeds a Personal calendar"
}

t_cal_get() {
	need C1_UUID || return
	api GET "/calendars/$C1_UUID"
	assert_status 200 || return
	assert_json .uuid "$C1_UUID"
	api GET /calendars/00000000-0000-0000-0000-000000000000
	assert_status 404
	api GET "/calendars/$C1_ID"
	assert_status 404 "calendars are addressed by UUID, not numeric id"
}

t_cal_patch() {
	need C1_UUID || return
	C1_NAME="$C1_NAME (renamed)"
	api PATCH "/calendars/$C1_UUID" --json "$(jobj name="$C1_NAME" color="#00aa55" description="changed")"
	assert_status 200 || return
	assert_json .name "$C1_NAME"
	assert_json .color "#00aa55"
	assert_json .description changed
	api GET "/calendars/$C1_UUID"
	assert_json .name "$C1_NAME" "rename must persist"
	assert_json .timezone Europe/Berlin "fields not in the PATCH stay untouched"
}

t_cal_patch_invalid() {
	need C1_UUID || return
	api PATCH "/calendars/$C1_UUID" --json '{"color":"not-a-colour"}'
	assert_status 400
	api PATCH "/calendars/$C1_UUID" --json '{"name":""}'
	assert_status 400 "an empty name is not allowed"
	api GET "/calendars/$C1_UUID"
	assert_json .color "#00aa55" "a rejected PATCH must not change anything"
}

t_cal_isolation() {
	need C1_UUID || return
	as U2 api GET "/calendars/$C1_UUID"
	assert_status_class 4xx "U2 reading U1's calendar"
	as U2 api PATCH "/calendars/$C1_UUID" --json '{"name":"hijacked"}'
	assert_status_class 4xx "U2 renaming U1's calendar"
	as U2 api DELETE "/calendars/$C1_UUID" --json '{"confirmation":"DELETE"}'
	assert_status_class 4xx "U2 deleting U1's calendar"
	as U2 api GET "/calendars/$C1_UUID/export"
	assert_status_class 4xx "U2 exporting U1's calendar"
	as U2 api GET /calendars/
	assert_not_contains "$C1_UUID" "U1's calendar must not appear in U2's list"
	api GET "/calendars/$C1_UUID"
	assert_json .name "$C1_NAME" "U1's calendar must be untouched"
}

t_cal_notfound_status() {
	need C1_UUID || return
	# GET already answers 404 (the integration tests call 404 deliberate: it
	# does not leak existence). The write verbs must agree.
	t_known F12
	local zero=00000000-0000-0000-0000-000000000000
	api PATCH "/calendars/$zero" --json '{"name":"x"}'
	assert_status 404 "PATCH unknown calendar"
	api DELETE "/calendars/$zero" --json '{"confirmation":"DELETE"}'
	assert_status 404 "DELETE unknown calendar"
	as U2 api PATCH "/calendars/$C1_UUID" --json '{"name":"x"}'
	assert_status 404 "PATCH another user's calendar"
	as U2 api POST "/calendars/$C1_UUID/import" --data "$(ics_event "$(uniq nf)" x 20300101T100000Z 20300101T110000Z)" --ctype text/calendar
	assert_status 404 "import into another user's calendar"
}

t_cal_export() {
	need C1_UUID || return
	local ev
	mk_event ev "$C1_UUID" "Export me $E2E_RUN_ID" 2030-05-05T08:00:00Z 2030-05-05T09:00:00Z || {
		t_fail "seed event failed" with-response
		return
	}
	api GET "/calendars/$C1_UUID/export"
	assert_status 200 || return
	assert_header Content-Type text/calendar
	assert_header Content-Disposition attachment
	assert_match '^BEGIN:VCALENDAR' "export must start with BEGIN:VCALENDAR"
	assert_contains "SUMMARY:Export me $E2E_RUN_ID"
	assert_contains "END:VCALENDAR"
}

t_cal_delete() {
	local name
	name=$(uniq "Delete me")
	mk_calendar DEL "$name" || {
		t_fail "create failed" with-response
		return
	}
	api DELETE "/calendars/$DEL_UUID"
	assert_status 400 "delete without confirmation must be refused"
	api DELETE "/calendars/$DEL_UUID" --json '{"confirmation":"yes"}'
	assert_status 400 "delete with a wrong confirmation must be refused"
	api GET "/calendars/$DEL_UUID"
	assert_status 200 "the calendar must survive refused deletes" || return
	api DELETE "/calendars/$DEL_UUID" --json '{"confirmation":"DELETE"}'
	assert_status 204 || return
	api GET "/calendars/$DEL_UUID"
	assert_status 404
	api GET /calendars/
	assert_not_contains "$DEL_UUID"
}

t_cal_last_guard() {
	user_create UCAL "E2E Single Calendar" || {
		t_fail "could not create a scratch user" with-response
		return
	}
	as UCAL api GET /calendars/
	assert_status 200 || return
	local n uuid
	n=$(jget '.calendars | length')
	uuid=$(jget '.calendars[0].uuid')
	assert_eq "$n" 1 "a fresh account has exactly one calendar" || return
	as UCAL api DELETE "/calendars/$uuid" --json '{"confirmation":"DELETE"}'
	assert_status_class 4xx "deleting the only calendar must be refused"
	as UCAL api GET "/calendars/$uuid"
	assert_status 200 "the only calendar must survive"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main calendars "$@"
fi
