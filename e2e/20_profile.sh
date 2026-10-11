#!/usr/bin/env bash
# User profile, preferences and the full-account backup export.
set -u
SUITE_DESC_profile="user profile, preferences, backup export (ZIP)"

suite_profile() {
	ensure_user U1
	t_run "GET /users/me returns the profile with stats" t_prof_get
	t_run "PATCH /users/me updates the display name" t_prof_patch
	t_run "preferences: defaults for every known key" t_prof_prefs_defaults
	t_run "preferences: valid update is stored and normalized" t_prof_prefs_update
	t_run "preferences: unknown key / invalid value / empty body rejected" t_prof_prefs_invalid
	t_run "backup export streams a ZIP with metadata, .ics and .vcf" t_prof_backup
}

t_prof_get() {
	api GET /users/me
	assert_status 200 || return
	assert_json .data.id "$(uget U1 ID)"
	assert_json .data.email "$(uget U1 EMAIL)"
	assert_json .data.is_active true
	assert_jq '.data.stats.calendar_count >= 1' "registration seeds a default calendar"
	assert_jq '.data.stats | has("contact_count") and has("app_password_count")'
	assert_jq '.data.created_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T")' "created_at is RFC 3339"
	assert_not_contains "PasswordHash" "the profile must never expose the password hash"
	assert_not_contains "password_hash"
}

t_prof_patch() {
	local name
	name="E2E Renamed $E2E_RUN_ID"
	api PATCH /users/me --json "$(jobj display_name="$name")"
	assert_status 200 || return
	assert_json .data.display_name "$name"
	api GET /users/me
	assert_json .data.display_name "$name" "the new name must persist"
	api PATCH /users/me --json "$(jobj display_name="$(uget U1 DISPLAY)")"
	assert_status 200
}

t_prof_prefs_defaults() {
	api GET /users/me/preferences
	assert_status 200 || return
	assert_jq '.data.preferences | has("default_event_duration") and has("default_all_day") and has("time_format") and has("accent_color")' \
		"all four known preference keys must be present (defaults filled in)"
}

t_prof_prefs_update() {
	api PATCH /users/me/preferences --json '{"preferences":{"time_format":"12h","default_event_duration":"30","accent_color":"#8B5CF6"}}'
	assert_status 200 || return
	assert_json .data.preferences.time_format 12h
	assert_json .data.preferences.default_event_duration 30
	assert_json .data.preferences.accent_color "#8b5cf6" "accent colour is normalized to lower case"
	api GET /users/me/preferences
	assert_json .data.preferences.time_format 12h "the update must persist"
	# restore defaults
	api PATCH /users/me/preferences --json '{"preferences":{"time_format":"24h","default_event_duration":"60","accent_color":"#3b82f6"}}'
	assert_status 200
}

t_prof_prefs_invalid() {
	api PATCH /users/me/preferences --json '{"preferences":{"bogus_key":"1"}}'
	assert_status 400
	api PATCH /users/me/preferences --json '{"preferences":{"time_format":"13h"}}'
	assert_status 400
	api PATCH /users/me/preferences --json '{"preferences":{"default_event_duration":"17"}}'
	assert_status 400 "durations are a closed set"
	api PATCH /users/me/preferences --json '{"preferences":{"accent_color":"blue"}}'
	assert_status 400
	api PATCH /users/me/preferences --json '{"preferences":{}}'
	assert_status 400
}

t_prof_backup() {
	mk_calendar BK_CAL "$(uniq backup-cal)" || {
		t_fail "could not create a calendar" with-response
		return
	}
	local ev ct
	mk_event ev "$BK_CAL_UUID" "Backup event $E2E_RUN_ID" 2030-03-01T10:00:00Z 2030-03-01T11:00:00Z || t_fail "seed event failed" with-response
	mk_addressbook BK_AB "$(uniq backup-book)" || {
		t_fail "could not create an address book" with-response
		return
	}
	mk_contact ct "$BK_AB_UUID" "Backup Contact $E2E_RUN_ID" || t_fail "seed contact failed" with-response
	api GET /users/me/export
	assert_status 200 || return
	assert_header Content-Type application/zip
	assert_header Content-Disposition attachment
	local magic
	magic=$(head -c 2 "$RESP_BODY_FILE")
	assert_eq "$magic" "PK" "the body must be a ZIP archive" || return
	local zip="$E2E_TMP/backup.zip" listing='' meta=''
	cp "$RESP_BODY_FILE" "$zip"
	if $E2E_HAVE_PYTHON; then
		listing=$(python3 -I -c 'import sys,zipfile; print("\n".join(zipfile.ZipFile(sys.argv[1]).namelist()))' "$zip" 2>&1)
		meta=$(python3 -I -c 'import sys,zipfile; print(zipfile.ZipFile(sys.argv[1]).read("metadata.json").decode())' "$zip" 2>&1)
	elif $E2E_HAVE_UNZIP; then
		listing=$(unzip -Z1 "$zip" 2>&1)
		meta=$(unzip -p "$zip" metadata.json 2>&1)
	else
		info "      (python3/unzip not installed: archive content not inspected)"
		return 0
	fi
	[[ $listing == *metadata.json* ]] || t_fail "archive lacks metadata.json (entries: ${listing//$'\n'/, })"
	[[ $listing == *"calendars/$(uniq backup-cal).ics"* ]] || t_fail "archive lacks calendars/$(uniq backup-cal).ics (entries: ${listing//$'\n'/, })"
	[[ $listing == *"addressbooks/$(uniq backup-book).vcf"* ]] || t_fail "archive lacks addressbooks/$(uniq backup-book).vcf"
	if ! jq -e --arg n "$(uniq backup-cal)" '.calendars[] | select(.name == $n) | .event_count == 1' >/dev/null 2>&1 <<<"$meta"; then
		t_fail "metadata.json must report event_count 1 for the seeded calendar: ${meta:0:400}"
	fi
	if ! jq -e --arg n "$(uniq backup-book)" '.addressbooks[] | select(.name == $n) | .contact_count == 1' >/dev/null 2>&1 <<<"$meta"; then
		t_fail "metadata.json must report contact_count 1 for the seeded address book"
	fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main profile "$@"
fi
