#!/usr/bin/env bash
# CalDAV (RFC 4791) in depth: collection creation (MKCALENDAR / extended
# MKCOL), object PUT/GET/HEAD/DELETE with ETag preconditions, UID uniqueness,
# VTODO, recurrence overrides, calendar-query / calendar-multiget REPORTs,
# PROPPATCH, COPY/MOVE, REST <-> DAV consistency.
set -u
SUITE_DESC_dav_caldav="MKCALENDAR, PUT/GET/DELETE + ETags, REPORTs, PROPPATCH, COPY/MOVE"

suite_dav_caldav() {
	ensure_user U1
	user_ensure_dav U1 || die "dav_caldav: no DAV app password"
	mk_calendar CD_CAL "$(uniq caldav)" || die "dav_caldav: cannot create a calendar"
	CD_COL="$(dav_home_cal)$CD_CAL_PATH/"
	t_run "MKCALENDAR creates a calendar with its displayname" t_cd_mkcalendar
	t_run "extended MKCOL creates a calendar" t_cd_mkcol
	t_run "MKCALENDAR / MKCOL on an existing collection is refused (405)" t_cd_mkcol_existing
	t_run "MKCALENDAR outside the own calendar home is refused" t_cd_mkcol_outside
	t_run "PUT with If-None-Match: * creates (201 + ETag)" t_cd_put_create
	t_run "PUT with If-None-Match: * on an existing object is 412" t_cd_put_create_again
	t_run "GET returns the object with the same ETag" t_cd_get
	t_run "HEAD returns the headers of GET" t_cd_head
	t_run "PUT with matching If-Match updates and changes the ETag" t_cd_put_if_match
	t_run "PUT with a stale If-Match is 412 and changes nothing" t_cd_put_stale
	t_run "PUT with If-Match on a missing object is 412" t_cd_put_if_match_missing
	t_run "same UID at a second path is a 409 (no-uid-conflict)" t_cd_uid_conflict
	t_run "PUT rejects wrong media type and malformed iCalendar" t_cd_put_invalid
	t_run "PUT of a VCALENDAR without components is a 4xx" t_cd_put_empty
	t_run "VTODO objects are stored and served" t_cd_vtodo
	t_run "recurrence overrides (RECURRENCE-ID) round-trip" t_cd_override
	t_run "calendar-query with a time-range returns only matches" t_cd_query_timerange
	t_run "calendar-query by component (VTODO)" t_cd_query_vtodo
	t_run "calendar-multiget returns requested objects, 404 for missing" t_cd_multiget
	t_run "Depth 1 PROPFIND lists the objects with ETags" t_cd_list_objects
	t_run "objects PUT over DAV appear in REST, and vice versa" t_cd_rest_crosscheck
	t_run "OPTIONS on an object lists GET/PUT/DELETE" t_cd_options_object
	t_run "COPY / MOVE are answered cleanly (not supported: 501)" t_cd_copy_move
	t_run "DELETE with a stale If-Match is 412" t_cd_delete_stale
	t_run "DELETE removes the object; repeated DELETE is 404" t_cd_delete
	t_run "PROPPATCH renames and recolours the collection" t_cd_proppatch
	t_run "the colour set by PROPPATCH is readable over PROPFIND" t_cd_color_readback
	t_run "PROPPATCH is atomic: one bad property fails them all (RFC 4918 9.2)" t_cd_proppatch_atomic
	t_run "DELETE on the collection removes the calendar" t_cd_delete_collection
}

CD_MKCAL_BODY() {
	printf '<?xml version="1.0" encoding="utf-8"?>\n<c:mkcalendar xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:set><d:prop><d:displayname>%s</d:displayname></d:prop></d:set></c:mkcalendar>' "$1"
}

# _cd_find_cal_by_path PATH — sets CD_FOUND (count) and CD_FOUND_UUID.
_cd_find_cal_by_path() {
	api GET /calendars/
	CD_FOUND=$(jq --arg p "$1" '[.calendars[] | select(.path == $p)] | length' <<<"$RESP_BODY")
	CD_FOUND_UUID=$(jq -r --arg p "$1" '[.calendars[] | select(.path == $p)][0].uuid // ""' <<<"$RESP_BODY")
	CD_FOUND_NAME=$(jq -r --arg p "$1" '[.calendars[] | select(.path == $p)][0].name // ""' <<<"$RESP_BODY")
}

# _cd_register_cleanup_by_path PATH — delete every REST calendar at PATH later.
_cd_cleanup_path() {
	local p=$1 u
	api GET /calendars/
	for u in $(jq -r --arg p "$p" '.calendars[] | select(.path == $p) | .uuid' <<<"$RESP_BODY"); do
		defer "delete DAV-created calendar $p" cleanup_api U1 DELETE "/calendars/$u" --json '{"confirmation":"DELETE"}'
	done
}

CD_MK_SLUG=''
t_cd_mkcalendar() {
	CD_MK_SLUG="$(uniq mkcal)"
	dav MKCALENDAR "$(dav_home_cal)$CD_MK_SLUG/" --xml "$(CD_MKCAL_BODY "Made by MKCALENDAR")"
	assert_status 201 || return
	_cd_cleanup_path "$CD_MK_SLUG"
	_cd_find_cal_by_path "$CD_MK_SLUG"
	assert_eq "$CD_FOUND" 1 "exactly one calendar at the new path" || return
	assert_eq "$CD_FOUND_NAME" "Made by MKCALENDAR" "the MKCALENDAR displayname must be kept"
	dav PROPFIND "$(dav_home_cal)$CD_MK_SLUG/" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 207 || return
	assert_xpath "$(lp resourcetype calendar)"
}

t_cd_mkcol() {
	local slug
	slug="$(uniq mkcol)"
	dav MKCOL "$(dav_home_cal)$slug/" --xml '<?xml version="1.0" encoding="utf-8"?>
<d:mkcol xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:set><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Made by MKCOL</d:displayname></d:prop></d:set></d:mkcol>'
	assert_status 201 || return
	_cd_cleanup_path "$slug"
	_cd_find_cal_by_path "$slug"
	assert_eq "$CD_FOUND" 1 "exactly one calendar at the new path"
	assert_eq "$CD_FOUND_NAME" "Made by MKCOL"
}

t_cd_mkcol_existing() {
	need CD_MK_SLUG || return
	t_known F03
	local col
	col="$(dav_home_cal)$CD_MK_SLUG/"
	dav MKCALENDAR "$col" --xml "$(CD_MKCAL_BODY "Duplicate")"
	assert_status 405 403 409 "MKCALENDAR on an existing calendar (RFC 4791 5.3.1: resource-must-be-null)"
	local s1=$RESP_STATUS
	dav MKCOL "$col"
	assert_status 405 "MKCOL on an existing collection (RFC 4918 9.3.1)"
	_cd_find_cal_by_path "$CD_MK_SLUG"
	assert_eq "$CD_FOUND" 1 "the refused requests must not create duplicate calendars at '$CD_MK_SLUG' (MKCALENDAR answered $s1)"
	# Never leave duplicates behind: delete the extra ones right away (the
	# original stays registered for cleanup).
	if ((CD_FOUND > 1)); then
		local u
		for u in $(jq -r --arg p "$CD_MK_SLUG" '.calendars[] | select(.path == $p) | .uuid' <<<"$RESP_BODY"); do
			defer "delete duplicate calendar at $CD_MK_SLUG" cleanup_api U1 DELETE "/calendars/$u" --json '{"confirmation":"DELETE"}'
		done
	fi
}

t_cd_mkcol_outside() {
	dav MKCALENDAR "$(dav_home_cal)$(uniq nested)/deeper/" --xml "$(CD_MKCAL_BODY nested)"
	assert_status_class 4xx "a calendar inside a calendar"
	dav MKCALENDAR "/dav/$(uget U1 USERNAME)/$(uniq loose)/" --xml "$(CD_MKCAL_BODY loose)"
	assert_status_class 4xx "a calendar outside the calendar home"
	_cd_find_cal_by_path "deeper"
	assert_eq "$CD_FOUND" 0 "nothing may have been created"
}

CD_UID='' CD_OBJ='' CD_ETAG=''
t_cd_put_create() {
	CD_UID="$(uniq ev1)@e2e"
	CD_OBJ="${CD_COL}$(uniq ev1).ics"
	dav PUT "$CD_OBJ" --ics "$(ics_event "$CD_UID" "CalDAV PUT event" 20300601T100000Z 20300601T110000Z "LOCATION:Room 7")" -H 'If-None-Match: *'
	assert_status 201 || return
	CD_ETAG=$(hdr ETag)
	[[ $CD_ETAG =~ ^\".+\"$ ]] || t_fail "PUT must return a strong, quoted ETag, got '$CD_ETAG'"
}

t_cd_put_create_again() {
	need CD_OBJ || return
	dav PUT "$CD_OBJ" --ics "$(ics_event "$CD_UID" "Clobber" 20300601T100000Z 20300601T110000Z)" -H 'If-None-Match: *'
	assert_status 412
}

t_cd_get() {
	need CD_OBJ || return
	dav GET "$CD_OBJ"
	assert_status 200 || return
	assert_header Content-Type text/calendar
	assert_header_eq ETag "$CD_ETAG" "GET must serve the ETag PUT returned"
	assert_contains "UID:$CD_UID"
	assert_contains "SUMMARY:CalDAV PUT event"
	assert_contains "LOCATION:Room 7"
	assert_match $'BEGIN:VCALENDAR\r\n' "lines must end with CRLF"
}

t_cd_head() {
	need CD_OBJ || return
	dav HEAD "$CD_OBJ"
	assert_status 200 || return
	assert_header_eq ETag "$CD_ETAG"
	assert_header Content-Type text/calendar
	assert_header Content-Length
}

t_cd_put_if_match() {
	need CD_ETAG || return
	dav PUT "$CD_OBJ" --ics "$(ics_event "$CD_UID" "CalDAV PUT event v2" 20300601T100000Z 20300601T113000Z)" -H "If-Match: $CD_ETAG"
	assert_status 200 201 204 || return
	local new
	new=$(hdr ETag)
	if [[ -n $new ]]; then
		assert_ne "$new" "$CD_ETAG" "the ETag must change when the content changes"
	fi
	dav GET "$CD_OBJ"
	assert_contains "SUMMARY:CalDAV PUT event v2"
	assert_ne "$(hdr ETag)" "$CD_ETAG" "GET must serve the new ETag"
	CD_OLD_ETAG=$CD_ETAG
	CD_ETAG=$(hdr ETag)
}

t_cd_put_stale() {
	need CD_OLD_ETAG || return
	dav PUT "$CD_OBJ" --ics "$(ics_event "$CD_UID" "stale write" 20300601T100000Z 20300601T110000Z)" -H "If-Match: $CD_OLD_ETAG"
	assert_status 412 || return
	dav GET "$CD_OBJ"
	assert_contains "SUMMARY:CalDAV PUT event v2" "the rejected write must not change the object"
	assert_not_contains "stale write"
}

t_cd_put_if_match_missing() {
	dav PUT "${CD_COL}$(uniq ghost).ics" --ics "$(ics_event "$(uniq ghost)" ghost 20300601T100000Z 20300601T110000Z)" -H 'If-Match: "anything"'
	assert_status 412
	dav GET "${CD_COL}$(uniq ghost).ics"
	assert_status 404 "nothing may have been created"
}

t_cd_uid_conflict() {
	need CD_UID || return
	dav PUT "${CD_COL}$(uniq other-path).ics" --ics "$(ics_event "$CD_UID" "same uid elsewhere" 20300601T100000Z 20300601T110000Z)"
	assert_status 409 || return
	assert_contains "no-uid-conflict" "RFC 4791 5.3.2.1: precondition element CALDAV:no-uid-conflict"
	dav GET "${CD_COL}$(uniq other-path).ics"
	assert_status 404
}

t_cd_put_invalid() {
	dav PUT "${CD_COL}$(uniq bad1).ics" --data "$(ics_event "$(uniq bad1)" x 20300601T100000Z 20300601T110000Z)" --ctype application/json
	assert_status_class 4xx "non-iCalendar media type"
	dav PUT "${CD_COL}$(uniq bad2).ics" --data 'this is not iCalendar' --ctype text/calendar
	assert_status 400 "malformed iCalendar"
	dav PUT "${CD_COL}$(uniq bad4).ics" --ics "$(ics_event "$(uniq bad4)-a" a 20300601T100000Z 20300601T110000Z | sed '$d')"$'\r\n'"$(ics_event "$(uniq bad4)-b" b 20300601T100000Z 20300601T110000Z | sed '1,3d')"
	assert_status_class 4xx "two different UIDs in one object (RFC 4791 4.1)"
}

t_cd_put_empty() {
	t_known F16
	dav PUT "${CD_COL}$(uniq empty).ics" --ics $'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//x//EN\r\nEND:VCALENDAR\r\n'
	assert_status 400 403 415 422 "a VCALENDAR without any component is invalid client input (RFC 4791 valid-calendar-object-resource), not a server error"
}

t_cd_vtodo() {
	local uid obj
	uid="$(uniq todo)@e2e" obj="${CD_COL}$(uniq todo).ics"
	dav PUT "$obj" --ics "$(ics_todo "$uid" "Buy milk")"
	assert_status 201 204 || return
	dav GET "$obj"
	assert_status 200 || return
	assert_contains "BEGIN:VTODO"
	assert_contains "SUMMARY:Buy milk"
	CD_TODO_UID=$uid
}

t_cd_override() {
	local uid obj ics
	uid="$(uniq series)@e2e" obj="${CD_COL}$(uniq series).ics"
	ics=$(printf 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300701T100000Z\r\nDTEND:20300701T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nSUMMARY:DAV series\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20260101T000000Z\r\nRECURRENCE-ID:20300702T100000Z\r\nDTSTART:20300702T150000Z\r\nDTEND:20300702T160000Z\r\nSUMMARY:DAV series (moved)\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n' "$uid" "$uid")
	dav PUT "$obj" --ics "$ics"
	assert_status 201 204 || return
	dav GET "$obj"
	assert_contains "RECURRENCE-ID:20300702T100000Z" "the override must be stored with the master"
	assert_contains "RRULE:FREQ=DAILY;COUNT=3"
	api GET "/calendars/$CD_CAL_UUID/events?start=2030-06-30T00:00:00Z&end=2030-07-10T00:00:00Z&expand=true"
	assert_status 200 || return
	assert_jq "[.events[] | select(.uid == \"$uid\")] | length == 3" "3 instances"
	assert_jq "[.events[] | select(.uid == \"$uid\" and .recurrence_id == \"20300702T100000Z\")][0] | .summary == \"DAV series (moved)\" and .start == \"2030-07-02T15:00:00Z\"" \
		"the REST expansion must apply the override"
}

_cd_query() {
	dav REPORT "$CD_COL" --depth 1 --xml "<?xml version=\"1.0\" encoding=\"utf-8\"?>
<c:calendar-query xmlns:d=\"DAV:\" xmlns:c=\"urn:ietf:params:xml:ns:caldav\">
<d:prop><d:getetag/><c:calendar-data/></d:prop>
<c:filter><c:comp-filter name=\"VCALENDAR\">$1</c:comp-filter></c:filter></c:calendar-query>"
}

t_cd_query_timerange() {
	local u
	for u in in1:20310510T090000Z in2:20310515T140000Z out:20320101T090000Z; do
		dav PUT "${CD_COL}$(uniq "q-${u%%:*}").ics" --ics "$(ics_event "$(uniq "q-${u%%:*}")" "Query ${u%%:*}" "${u#*:}" "${u#*:}")"
		assert_status 201 204 || return
	done
	_cd_query '<c:comp-filter name="VEVENT"><c:time-range start="20310501T000000Z" end="20310601T000000Z"/></c:comp-filter>'
	assert_status 207 || return
	assert_contains "$(uniq q-in1)" "in-range event 1"
	assert_contains "$(uniq q-in2)" "in-range event 2"
	assert_not_contains "$(uniq q-out)" "out-of-range event must not match"
	assert_not_contains "$CD_UID" "the 2030 event must not match"
	assert_xpath "$(lp response propstat prop calendar-data)" "calendar-data must be returned when requested"
}

t_cd_query_vtodo() {
	need CD_TODO_UID || return
	_cd_query '<c:comp-filter name="VTODO"/>'
	assert_status 207 || return
	assert_contains "$CD_TODO_UID"
	assert_not_contains "$CD_UID" "VEVENTs must not match a VTODO filter"
}

t_cd_multiget() {
	need CD_OBJ || return
	local todo missing
	todo="${CD_COL}$(uniq todo).ics" missing="${CD_COL}$(uniq missing).ics"
	dav REPORT "$CD_COL" --depth 1 --xml "<?xml version=\"1.0\" encoding=\"utf-8\"?>
<c:calendar-multiget xmlns:d=\"DAV:\" xmlns:c=\"urn:ietf:params:xml:ns:caldav\">
<d:prop><d:getetag/><c:calendar-data/></d:prop>
<d:href>$CD_OBJ</d:href><d:href>$todo</d:href><d:href>$missing</d:href></c:calendar-multiget>"
	assert_status 207 || return
	assert_xpath "$(lp response)[$(el href)='$CD_OBJ']//$(el getetag)" "the event is returned with its ETag"
	assert_xpath "$(lp response)[$(el href)='$todo']//$(el calendar-data)[contains(.,'VTODO')]" "the todo is returned with calendar-data"
	assert_xpath "$(lp response)[$(el href)='$missing']//$(el status)[contains(.,'404')]" "a missing href is reported 404 inside the multistatus"
	local tag
	tag=$(xpath "string($(lp response)[$(el href)='$CD_OBJ']//$(el getetag))")
	[[ -z $tag || $tag == "$CD_ETAG" ]] || t_fail "multiget ETag $tag differs from GET ETag $CD_ETAG"
}

t_cd_list_objects() {
	need CD_OBJ || return
	dav PROPFIND "$CD_COL" --depth 1 --xml "$PROPFIND_LIST"
	assert_status 207 || return
	assert_xpath "$(lp response)[$(el href)='$CD_OBJ']//$(el getetag)" "the object is listed with its ETag"
	assert_xpath "count($(lp response)) >= 5" "collection + objects"
}

t_cd_rest_crosscheck() {
	need CD_UID || return
	api GET "/calendars/$CD_CAL_UUID/events?start=2030-01-01T00:00:00Z&end=2030-12-31T00:00:00Z&expand=false"
	assert_jq "[.events[] | select(.uid == \"$CD_UID\" and .summary == \"CalDAV PUT event v2\")] | length == 1" \
		"an object PUT over DAV must appear in the REST event list" || return
	local id uid
	mk_event id "$CD_CAL_UUID" "Made over REST $E2E_RUN_ID" 2030-08-01T10:00:00Z 2030-08-01T11:00:00Z || {
		t_fail "REST create failed" with-response
		return
	}
	uid=$(jget .uid)
	dav PROPFIND "$CD_COL" --depth 1 --xml "$PROPFIND_LIST"
	local href
	href=$(xpath "string(($(lp response href))[contains(.,'$id') or contains(.,'${uid%%@*}')])")
	if [[ -z $href ]]; then
		_cd_query '<c:comp-filter name="VEVENT"><c:time-range start="20300801T000000Z" end="20300802T000000Z"/></c:comp-filter>'
		assert_contains "Made over REST $E2E_RUN_ID" "a REST-created event must be visible over CalDAV"
		return
	fi
	dav GET "$href"
	assert_status 200 || return
	assert_contains "SUMMARY:Made over REST $E2E_RUN_ID"
	assert_contains "UID:$uid"
}

t_cd_options_object() {
	need CD_OBJ || return
	dav OPTIONS "$CD_OBJ"
	assert_status 200 204 || return
	assert_header Allow GET
	assert_header Allow PUT
	assert_header Allow DELETE
}

t_cd_copy_move() {
	need CD_OBJ || return
	dav COPY "$CD_OBJ" -H "Destination: $BASE_URL${CD_COL}$(uniq copy).ics"
	assert_status 201 204 501 403 405 "COPY" || return
	[[ $RESP_STATUS == 501 ]] && info "      note: COPY is not implemented by the server (501)"
	dav MOVE "$CD_OBJ" -H "Destination: $BASE_URL${CD_COL}$(uniq moved).ics"
	assert_status 201 204 501 403 405 "MOVE" || return
	if [[ $RESP_STATUS == 501 || ${RESP_STATUS:0:1} == 4 ]]; then
		dav GET "$CD_OBJ"
		assert_status 200 "a refused MOVE must leave the source in place"
	fi
}

t_cd_delete_stale() {
	need CD_OBJ || return
	dav DELETE "$CD_OBJ" -H 'If-Match: "stale-etag"'
	assert_status 412 || return
	dav GET "$CD_OBJ"
	assert_status 200 "the object must survive a failed conditional DELETE"
}

t_cd_delete() {
	need CD_OBJ || return
	dav DELETE "$CD_OBJ" -H "If-Match: $CD_ETAG"
	assert_status 204 200 || return
	dav GET "$CD_OBJ"
	assert_status 404
	dav DELETE "$CD_OBJ"
	assert_status 404
	api GET "/calendars/$CD_CAL_UUID/events?start=2030-01-01T00:00:00Z&end=2030-12-31T00:00:00Z&expand=false"
	assert_jq "[.events[] | select(.uid == \"$CD_UID\")] | length == 0" "a DAV delete must remove the REST event too"
}

t_cd_proppatch() {
	dav PROPPATCH "$CD_COL" --xml '<?xml version="1.0" encoding="utf-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:a="http://apple.com/ns/ical/"><d:set><d:prop><d:displayname>Renamed over DAV</d:displayname><a:calendar-color>#FF8800FF</a:calendar-color></d:prop></d:set></d:propertyupdate>'
	assert_status 207 || return
	assert_xpath "$(lp response propstat)[contains($(el status),'200')]//$(el displayname)" "displayname must be acknowledged with 200"
	api GET "/calendars/$CD_CAL_UUID"
	assert_json .name "Renamed over DAV"
	assert_json .color "#FF8800" "Apple's #RRGGBBAA colour is stored as #RRGGBB"
}

t_cd_color_readback() {
	t_known F07
	dav PROPFIND "$CD_COL" --depth 0 --xml '<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:a="http://apple.com/ns/ical/"><d:prop><a:calendar-color/></d:prop></d:propfind>'
	assert_status 207 || return
	assert_xpath "$(lp response propstat)[contains($(el status),'200')]//$(el calendar-color)[contains(.,'FF8800') or contains(.,'ff8800')]" \
		"a client must be able to read the colour it (or the web UI) set"
}

t_cd_proppatch_atomic() {
	t_known F08
	dav PROPPATCH "$CD_COL" --xml '<?xml version="1.0" encoding="utf-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:x="urn:example:e2e"><d:set><d:prop><d:displayname>Should not stick</d:displayname><x:unsupported>1</x:unsupported></d:prop></d:set></d:propertyupdate>'
	assert_status 207 || return
	assert_xpath "$(lp response propstat)[contains($(el status),'424')]//$(el displayname)" \
		"when one property fails, the others must be reported 424 Failed Dependency"
	api GET "/calendars/$CD_CAL_UUID"
	assert_json .name "Renamed over DAV" "a PROPPATCH that partly failed must not apply anything"
}

t_cd_delete_collection() {
	local slug
	slug="$(uniq doomed)"
	dav MKCALENDAR "$(dav_home_cal)$slug/" --xml "$(CD_MKCAL_BODY Doomed)"
	assert_status 201 || return
	_cd_cleanup_path "$slug"
	dav PUT "$(dav_home_cal)$slug/x.ics" --ics "$(ics_event "$(uniq doomed-ev)" doomed 20300101T100000Z 20300101T110000Z)"
	dav DELETE "$(dav_home_cal)$slug/"
	assert_status 204 200 || return
	_cd_find_cal_by_path "$slug"
	assert_eq "$CD_FOUND" 0 "the calendar must be gone from REST"
	dav PROPFIND "$(dav_home_cal)$slug/" --depth 0 --xml "$PROPFIND_LIST"
	assert_status 404
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main dav_caldav "$@"
fi
