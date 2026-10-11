#!/usr/bin/env bash
# Import (.ics / .vcf: raw body, JSON {data}, multipart upload, duplicate
# handling) and export (calendar .ics, address book .vcf) round trips.
set -u
SUITE_DESC_import_export="calendar/contact import (body, JSON, multipart, duplicates) and export"

suite_import_export() {
	ensure_user U1
	ensure_user U2
	mk_calendar IMP_CAL "$(uniq import)" || die "import suite: cannot create a calendar"
	mk_addressbook IMP_AB "$(uniq import-book)" || die "import suite: cannot create an address book"
	t_run "calendar import from a text/calendar body" t_imp_cal_body
	t_run "re-import skips duplicates by UID (default)" t_imp_cal_skip
	t_run "duplicate_handling=replace overwrites by UID" t_imp_cal_replace
	t_run "calendar import from JSON {data} and multipart upload" t_imp_cal_json_multipart
	t_run "calendar import rejects garbage and empty bodies" t_imp_cal_invalid
	t_run "calendar import into a foreign calendar is refused" t_imp_cal_foreign
	t_run "import keeps recurrence exceptions (RECURRENCE-ID)" t_imp_cal_overrides
	t_run "imported TZID events keep their VTIMEZONE" t_imp_cal_vtimezone
	t_run "export → import round trip preserves UIDs and summaries" t_imp_cal_roundtrip
	t_run "contact import from a text/vcard body" t_imp_vcf
	t_run "address book export contains the contacts" t_imp_vcf_export
}

_ics_two() {
	printf 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\n'
	printf 'BEGIN:VEVENT\r\nUID:%s-1@e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300301T090000Z\r\nDTEND:20300301T100000Z\r\nSUMMARY:Imported one %s\r\nEND:VEVENT\r\n' "$1" "$1"
	printf 'BEGIN:VEVENT\r\nUID:%s-2@e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300302T090000Z\r\nDTEND:20300302T100000Z\r\nSUMMARY:Imported two %s\r\nEND:VEVENT\r\n' "$1" "$1"
	printf 'END:VCALENDAR\r\n'
}

_imp_summaries() { # CAL_UUID → sorted summaries in 2030
	api GET "/calendars/$1/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z&expand=false"
	jq -r '[.events[].summary] | sort | join("|")' <<<"$RESP_BODY"
}

t_imp_cal_body() {
	api POST "/calendars/$IMP_CAL_UUID/import" --data "$(_ics_two "$E2E_RUN_ID")" --ctype text/calendar
	assert_status 200 || return
	assert_json .total 2
	assert_json .imported 2
	assert_json .failed 0
	assert_eq "$(_imp_summaries "$IMP_CAL_UUID")" "Imported one $E2E_RUN_ID|Imported two $E2E_RUN_ID" "both events must be listed"
}

t_imp_cal_skip() {
	api POST "/calendars/$IMP_CAL_UUID/import" --data "$(_ics_two "$E2E_RUN_ID")" --ctype text/calendar
	assert_status 200 || return
	assert_json .imported 0
	assert_json .skipped 2
	assert_eq "$(_imp_summaries "$IMP_CAL_UUID")" "Imported one $E2E_RUN_ID|Imported two $E2E_RUN_ID" "no duplicates may appear"
}

t_imp_cal_replace() {
	local ics
	ics=$(_ics_two "$E2E_RUN_ID")
	ics=${ics//Imported one/Replaced one}
	api POST "/calendars/$IMP_CAL_UUID/import?duplicate_handling=replace" --data "$ics" --ctype text/calendar
	assert_status 200 || return
	assert_json .imported 2
	assert_eq "$(_imp_summaries "$IMP_CAL_UUID")" "Imported two $E2E_RUN_ID|Replaced one $E2E_RUN_ID" "the replaced event carries the new summary, no duplicate"
}

t_imp_cal_json_multipart() {
	mk_calendar IMP_CAL2 "$(uniq import-json)" || {
		t_fail "create failed" with-response
		return
	}
	api POST "/calendars/$IMP_CAL2_UUID/import" --json "$(jobj data="$(ics_event "$(uniq json-import)" "From JSON" 20300401T090000Z 20300401T100000Z)"$'\n')"
	assert_status 200 || return
	assert_json .imported 1
	local f="$E2E_TMP/upload.ics"
	ics_event "$(uniq multipart)" "From upload" 20300402T090000Z 20300402T100000Z >"$f"
	api POST "/calendars/$IMP_CAL2_UUID/import" --form "file=@$f;type=text/calendar"
	assert_status 200 || return
	assert_json .imported 1
	assert_eq "$(_imp_summaries "$IMP_CAL2_UUID")" "From JSON|From upload"
}

t_imp_cal_invalid() {
	api POST "/calendars/$IMP_CAL_UUID/import" --data 'BEGIN:VCALENDAR garbage' --ctype text/calendar
	assert_status 400
	api POST "/calendars/$IMP_CAL_UUID/import"
	assert_status 400 "no import data"
	api POST "/calendars/$IMP_CAL_UUID/import" --json '{"data":""}'
	assert_status 400 "empty data"
}

t_imp_cal_foreign() {
	as U2 api POST "/calendars/$IMP_CAL_UUID/import" --data "$(ics_event "$(uniq intruder)" Intruder 20300501T090000Z 20300501T100000Z)" --ctype text/calendar
	assert_status_class 4xx || return
	assert_not_contains '"imported":1'
	assert_eq "$(_imp_summaries "$IMP_CAL_UUID")" "Imported two $E2E_RUN_ID|Replaced one $E2E_RUN_ID" "nothing may land in U1's calendar"
}

t_imp_cal_overrides() {
	t_known F14
	mk_calendar IMP_REC "$(uniq import-rec)" || {
		t_fail "create failed" with-response
		return
	}
	local uid ics
	uid=$(uniq series)@e2e
	ics=$(printf 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300701T100000Z\r\nDTEND:20300701T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nSUMMARY:Series\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\nRECURRENCE-ID:20300702T100000Z\r\nDTSTART:20300702T150000Z\r\nDTEND:20300702T160000Z\r\nSUMMARY:Series (moved)\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n' "$uid" "$uid")
	api POST "/calendars/$IMP_REC_UUID/import" --data "$ics" --ctype text/calendar
	assert_status 200 || return
	assert_json .failed 0
	assert_json .skipped 0 "the RECURRENCE-ID override is part of the series, not a duplicate"
	api GET "/calendars/$IMP_REC_UUID/events?start=2030-06-30T00:00:00Z&end=2030-07-10T00:00:00Z&expand=true"
	assert_jq '[.events[] | select(.recurrence_id == "20300702T100000Z")] | .[0].summary == "Series (moved)" and .[0].start == "2030-07-02T15:00:00Z"' \
		"the overridden instance must keep its moved time and summary"
}

t_imp_cal_vtimezone() {
	t_known F15
	mk_calendar IMP_TZ "$(uniq import-tz)" || {
		t_fail "create failed" with-response
		return
	}
	local uid ics
	uid=$(uniq tz)@e2e
	ics=$(printf 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\nBEGIN:STANDARD\r\nDTSTART:19701025T030000\r\nRRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nTZNAME:CET\r\nEND:STANDARD\r\nBEGIN:DAYLIGHT\r\nDTSTART:19700329T020000\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\nTZNAME:CEST\r\nEND:DAYLIGHT\r\nEND:VTIMEZONE\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\nDTSTART;TZID=Europe/Berlin:20300601T100000\r\nDTEND;TZID=Europe/Berlin:20300601T110000\r\nSUMMARY:Berlin morning\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n' "$uid")
	api POST "/calendars/$IMP_TZ_UUID/import" --data "$ics" --ctype text/calendar
	assert_status 200 || return
	api GET "/calendars/$IMP_TZ_UUID/events?start=2030-05-01T00:00:00Z&end=2030-07-01T00:00:00Z"
	assert_jq '.events[0].start == "2030-06-01T10:00:00+02:00" or .events[0].start == "2030-06-01T08:00:00Z"' \
		"the local time must be interpreted in Europe/Berlin"
	# RFC 5545 3.2.19: every TZID used needs its VTIMEZONE in the object.
	dav GET "$(dav_home_cal U1)$IMP_TZ_PATH/$uid.ics"
	assert_status 200 || return
	assert_contains "TZID=Europe/Berlin"
	assert_contains "BEGIN:VTIMEZONE" "the stored object references TZID=Europe/Berlin but carries no VTIMEZONE"
}

t_imp_cal_roundtrip() {
	api GET "/calendars/$IMP_CAL_UUID/export"
	assert_status 200 || return
	local exported=$RESP_BODY
	mk_calendar IMP_RT "$(uniq roundtrip)" || {
		t_fail "create failed" with-response
		return
	}
	api POST "/calendars/$IMP_RT_UUID/import" --data "$exported" --ctype text/calendar
	assert_status 200 || return
	assert_json .failed 0
	assert_eq "$(_imp_summaries "$IMP_RT_UUID")" "$(_imp_summaries "$IMP_CAL_UUID")" "re-imported calendar must match the original"
	api GET "/calendars/$IMP_RT_UUID/export"
	assert_contains "UID:$E2E_RUN_ID-1@e2e" "UIDs survive the round trip"
}

_vcf_two() {
	printf 'BEGIN:VCARD\r\nVERSION:3.0\r\nUID:%s-c1\r\nFN:Ada Import%s\r\nN:Import%s;Ada;;;\r\nEMAIL;TYPE=work:ada-%s@example.test\r\nEND:VCARD\r\n' "$1" "$1" "$1" "$1"
	printf 'BEGIN:VCARD\r\nVERSION:3.0\r\nUID:%s-c2\r\nFN:Bob Import%s\r\nN:Import%s;Bob;;;\r\nTEL;TYPE=cell:+49 30 1234567\r\nEND:VCARD\r\n' "$1" "$1" "$1"
}

t_imp_vcf() {
	api POST "/addressbooks/$IMP_AB_UUID/import" --data "$(_vcf_two "$E2E_RUN_ID")" --ctype text/vcard
	assert_status 200 || return
	assert_json .total 2
	assert_json .imported 2
	api GET "/addressbooks/$IMP_AB_UUID/contacts?limit=100"
	assert_status 200 || return
	assert_jq "[.Contacts[].formatted_name] | sort == [\"Ada Import$E2E_RUN_ID\", \"Bob Import$E2E_RUN_ID\"]"
	api POST "/addressbooks/$IMP_AB_UUID/import" --data "$(_vcf_two "$E2E_RUN_ID")" --ctype text/vcard
	assert_json .skipped 2 "re-importing the same UIDs is skipped"
	as U2 api POST "/addressbooks/$IMP_AB_UUID/import" --data "$(_vcf_two x)" --ctype text/vcard
	assert_status_class 4xx "U2 importing into U1's address book"
}

t_imp_vcf_export() {
	api GET "/addressbooks/$IMP_AB_UUID/export"
	assert_status 200 || return
	assert_header Content-Type vcard
	assert_contains "BEGIN:VCARD"
	assert_contains "FN:Ada Import$E2E_RUN_ID"
	assert_contains "UID:$E2E_RUN_ID-c2"
	as U2 api GET "/addressbooks/$IMP_AB_UUID/export"
	assert_status_class 4xx "U2 exporting U1's address book"
	assert_not_contains "Ada Import$E2E_RUN_ID"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main import_export "$@"
fi
