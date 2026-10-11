# shellcheck shell=bash
# Fixtures: test users, DAV credentials, resource factories (each registers its
# own cleanup) and iCalendar / vCard / JSON builders.
#
# Users live in plain variables named <KEY>_<FIELD>, e.g. U1_EMAIL, U1_TOKEN:
#   EMAIL PASSWORD USERNAME ID TOKEN REFRESH EXP APPPW APPPW_ID DELETED
# Keys used by the suites: U1 (main user), U2 (peer for sharing / isolation
# tests); suites may create more (U3, ...) through user_create.

[[ -n ${_E2E_FIXTURES_LOADED:-} ]] && return 0
_E2E_FIXTURES_LOADED=1

uget() { local v="${1}_${2}"; printf '%s' "${!v:-}"; }
uset() { printf -v "${1}_${2}" '%s' "$3"; }

# uniq LABEL — run-unique name, e.g. "e2e-202610111015004821-LABEL".
uniq() { printf '%s-%s' "$E2E_RUN_TAG" "$1"; }

# jobj k=v k2:=json ... — build a JSON object safely with jq (prints).
jobj() {
	local -a args=()
	local expr='{}' i=0 kv k v
	for kv in "$@"; do
		i=$((i + 1))
		if [[ $kv == *:=* && ${kv%%:=*} != *=* ]]; then
			k=${kv%%:=*} v=${kv#*:=}
			args+=(--argjson "v$i" "$v")
		else
			k=${kv%%=*} v=${kv#*=}
			args+=(--arg "v$i" "$v")
		fi
		args+=(--arg "k$i" "$k")
		expr+=" | .[\$k$i] = \$v$i"
	done
	jq -nc "${args[@]}" "$expr"
}

# ---------------------------------------------------------------------------
# Server preflight: registration must be possible without e-mail verification.
# ---------------------------------------------------------------------------

e2e_preflight() {
	api_anon GET /system/settings
	if [[ $RESP_STATUS != 200 ]]; then
		die "GET $BASE_URL/api/v1/system/settings answered HTTP $RESP_STATUS — is CALCARD_URL pointing at a CalCard server?$(printf '\n')$(resp_dump)"
	fi
	E2E_SERVER_REGISTRATION=$(jget '.data.registration_enabled')
	E2E_SERVER_SMTP=$(jget '.data.smtp_enabled')
	if [[ $E2E_SERVER_REGISTRATION != true ]]; then
		die "Self-registration is disabled on the target (registration_enabled=false).
       The suite registers a fresh, uniquely named test user on every run and needs it enabled:
       set CALDAV_REGISTRATION_DISABLED=false (registration.disabled: false) on the server, restart it, and re-run."
	fi
	if [[ $E2E_SERVER_SMTP == true ]]; then
		die "The target has SMTP configured (smtp_enabled=true), so new accounts stay inactive until their
       e-mail address is verified — the suite cannot read that mailbox. Run it against an instance
       without SMTP (unset CALDAV_SMTP_HOST), e.g. a staging container of the same image."
	fi
}

# ---------------------------------------------------------------------------
# Users
# ---------------------------------------------------------------------------

_gen_password() {
	local r
	r=$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)
	printf 'E2e!%s9a' "$r"
}

# user_create KEY [DISPLAY_NAME] — register + log in a fresh user. Registers
# the account deletion as a cleanup action. Returns 1 (and prints why) on error.
user_create() {
	local key=$1 display=${2:-"E2E $1 $E2E_RUN_ID"}
	local lkey=${key,,}
	local email="${E2E_RUN_TAG}-${lkey}@${E2E_EMAIL_DOMAIN}"
	local password=${E2E_PASSWORD:-$(_gen_password)}
	uset "$key" EMAIL "$email"
	uset "$key" PASSWORD "$password"
	uset "$key" DISPLAY "$display"
	api_anon POST /auth/register --json "$(jobj email="$email" password="$password" display_name="$display")"
	case $RESP_STATUS in
	200 | 201) ;;
	403)
		die "Registration was refused (HTTP 403: $(jget '.message')). Enable self-registration on the target (CALDAV_REGISTRATION_DISABLED=false)."
		;;
	*)
		warn "registering $email failed with HTTP $RESP_STATUS"
		resp_dump >&2
		return 1
		;;
	esac
	uset "$key" ID "$(jget '.data.id')"
	uset "$key" USERNAME "$(jget '.data.username')"
	if [[ $(jget '.data.is_active') != true ]]; then
		die "The new account $email is inactive (e-mail verification required). Run the suite against a server without SMTP so registrations auto-activate."
	fi
	# Deleting the account is the very last cleanup for this user.
	defer "delete test account $email" user_delete "$key"
	user_login "$key" || return 1
	E2E_USERS+=("$key")
	return 0
}

# user_login KEY — password login; stores TOKEN/REFRESH/EXP.
user_login() {
	local key=$1
	api_anon POST /auth/login --json "$(jobj email="$(uget "$key" EMAIL)" password="$(uget "$key" PASSWORD)")"
	if [[ $RESP_STATUS != 200 ]]; then
		[[ ${E2E_IN_CLEANUP:-0} == 1 ]] || {
			warn "login for $(uget "$key" EMAIL) failed with HTTP $RESP_STATUS"
			resp_dump >&2
		}
		return 1
	fi
	uset "$key" TOKEN "$(jget '.data.access_token')"
	uset "$key" REFRESH "$(jget '.data.refresh_token')"
	uset "$key" EXP "$(jget '.data.expires_at')"
	return 0
}

# user_ensure_token KEY — keep the access token fresh (refresh 60s before
# expiry, fall back to a password login). Runs before every api call.
user_ensure_token() {
	local key=$1 exp tok now
	tok=$(uget "$key" TOKEN)
	exp=$(uget "$key" EXP)
	[[ -n $(uget "$key" EMAIL) ]] || return 1
	[[ $(uget "$key" DELETED) == 1 ]] && return 1
	now=$(now_s)
	if [[ -n $tok && $exp =~ ^[0-9]+$ ]] && ((now < exp - 60)); then
		return 0
	fi
	local saved_status=$RESP_STATUS saved_body=$RESP_BODY saved_headers=$RESP_HEADERS saved_line=$REQ_LINE saved_req=$REQ_BODY
	local refresh
	refresh=$(uget "$key" REFRESH)
	if [[ -n $refresh ]]; then
		api_anon POST /auth/refresh --json "$(jobj refresh_token="$refresh")"
		if [[ $RESP_STATUS == 200 ]]; then
			uset "$key" TOKEN "$(jget '.data.access_token')"
			uset "$key" REFRESH "$(jget '.data.refresh_token')"
			uset "$key" EXP "$(jget '.data.expires_at')"
			vlog 1 "refreshed access token for $key"
		else
			user_login "$key" || true
		fi
	else
		user_login "$key" || true
	fi
	RESP_STATUS=$saved_status RESP_BODY=$saved_body RESP_HEADERS=$saved_headers REQ_LINE=$saved_line REQ_BODY=$saved_req
	return 0
}

# user_ensure_dav KEY — create the user's DAV app password on first use.
user_ensure_dav() {
	local key=$1
	[[ -n $(uget "$key" APPPW) ]] && return 0
	local saved_status=$RESP_STATUS saved_body=$RESP_BODY saved_headers=$RESP_HEADERS saved_line=$REQ_LINE saved_req=$REQ_BODY
	as "$key" api POST /app-passwords/ --json "$(jobj name="$(uniq dav)" 'scopes:=["caldav","carddav"]')"
	if [[ $RESP_STATUS == 200 || $RESP_STATUS == 201 ]]; then
		uset "$key" APPPW "$(jget '.data.credentials.password')"
		uset "$key" APPPW_ID "$(jget '.data.id')"
		defer "revoke app password of $key" cleanup_api "$key" DELETE "/app-passwords/$(uget "$key" APPPW_ID)"
	else
		warn "could not create a DAV app password for $key (HTTP $RESP_STATUS)"
	fi
	RESP_STATUS=$saved_status RESP_BODY=$saved_body RESP_HEADERS=$saved_headers REQ_LINE=$saved_line REQ_BODY=$saved_req
	[[ -n $(uget "$key" APPPW) ]]
}

# user_delete KEY — DELETE /users/me (password + "DELETE" confirmation).
user_delete() {
	local key=$1
	[[ $(uget "$key" DELETED) == 1 ]] && return 0
	user_ensure_token "$key"
	as "$key" api DELETE /users/me --json "$(jobj password="$(uget "$key" PASSWORD)" confirmation=DELETE)"
	case $RESP_STATUS in
	204 | 200)
		uset "$key" DELETED 1
		return 0
		;;
	401)
		# Token no longer valid: maybe already deleted. Confirm by login.
		if ! user_login "$key"; then
			uset "$key" DELETED 1
			return 0
		fi
		as "$key" api DELETE /users/me --json "$(jobj password="$(uget "$key" PASSWORD)" confirmation=DELETE)"
		[[ $RESP_STATUS == 204 ]] && uset "$key" DELETED 1 && return 0
		;;
	esac
	return 1
}

# ensure_user KEY — create the user once per run (U1 / U2 are shared by suites).
E2E_USERS=()
ensure_user() {
	local key=$1
	[[ -n $(uget "$key" TOKEN) ]] && return 0
	user_create "$key" "${2:-}" || die "could not create test user $key — see above"
}

# cleanup_api KEY METHOD PATH [opts] — cleanup helper: 2xx or 404 (already
# gone) count as success.
cleanup_api() {
	local key=$1 method=$2 path=$3
	shift 3
	[[ $(uget "$key" DELETED) == 1 ]] && return 0
	as "$key" api "$method" "$path" "$@"
	[[ $RESP_STATUS == 2* || $RESP_STATUS == 404 ]] && return 0
	# Some handlers answer "not found" with a 400 (see FINDINGS.md F12).
	[[ $RESP_STATUS == 400 && ${RESP_BODY,,} == *"not found"* ]]
}

# ---------------------------------------------------------------------------
# Resource factories. They set globals named by the caller (never echo), so
# the cleanup registration survives. All register their own cleanup.
# ---------------------------------------------------------------------------

# mk_calendar OUTPREFIX NAME [COLOR] — as $E2E_ACTOR. Sets <OUTPREFIX>_UUID,
# <OUTPREFIX>_ID (numeric), <OUTPREFIX>_PATH (DAV path segment).
mk_calendar() {
	local out=$1 name=$2 color=${3:-#3366cc}
	api POST /calendars/ --json "$(jobj name="$name" color="$color" description="e2e $E2E_RUN_TAG")"
	if [[ $RESP_STATUS != 201 ]]; then
		printf -v "${out}_UUID" '%s' ''
		return 1
	fi
	local uuid
	uuid=$(jget .uuid)
	printf -v "${out}_UUID" '%s' "$uuid"
	printf -v "${out}_ID" '%s' "$(jget .id)"
	printf -v "${out}_PATH" '%s' "$(jget .path)"
	defer "delete calendar '$name'" cleanup_api "$E2E_ACTOR" DELETE "/calendars/$uuid" --json '{"confirmation":"DELETE"}'
	return 0
}

# mk_addressbook OUTPREFIX NAME — sets <OUTPREFIX>_UUID, _ID, _PATH.
mk_addressbook() {
	local out=$1 name=$2
	api POST /addressbooks/ --json "$(jobj name="$name" description="e2e $E2E_RUN_TAG")"
	if [[ $RESP_STATUS != 201 ]]; then
		printf -v "${out}_UUID" '%s' ''
		return 1
	fi
	local uuid
	uuid=$(jget .UUID)
	printf -v "${out}_UUID" '%s' "$uuid"
	printf -v "${out}_ID" '%s' "$(jget .ID)"
	printf -v "${out}_PATH" '%s' "$(jget .Path)"
	defer "delete address book '$name'" cleanup_api "$E2E_ACTOR" DELETE "/addressbooks/$uuid" --json '{"confirmation":"DELETE"}'
	return 0
}

# mk_event OUTVAR CAL_UUID SUMMARY START END [extra JSON fields as k=v / k:=json]
# Sets OUTVAR to the event id (UUID). Deleted with its calendar.
mk_event() {
	local out=$1 cal=$2 summary=$3 start=$4 end=$5
	shift 5
	api POST "/calendars/$cal/events" --json "$(jobj summary="$summary" start="$start" end="$end" timezone=UTC 'all_day:=false' "$@")"
	if [[ $RESP_STATUS != 201 ]]; then
		printf -v "$out" '%s' ''
		return 1
	fi
	printf -v "$out" '%s' "$(jget .id)"
}

# mk_contact OUTVAR AB_UUID FORMATTED_NAME [extra k=v...]
mk_contact() {
	local out=$1 ab=$2 fn=$3
	shift 3
	api POST "/addressbooks/$ab/contacts" --json "$(jobj formatted_name="$fn" "$@")"
	if [[ $RESP_STATUS != 201 ]]; then
		printf -v "$out" '%s' ''
		return 1
	fi
	printf -v "$out" '%s' "$(jget .id)"
}

# DAV paths for the acting user.
dav_home_cal() { printf '/dav/%s/calendars/' "$(uget "${1:-$E2E_ACTOR}" USERNAME)"; }
dav_home_ab() { printf '/dav/%s/addressbooks/' "$(uget "${1:-$E2E_ACTOR}" USERNAME)"; }

# ---------------------------------------------------------------------------
# iCalendar / vCard builders (CRLF line endings, as RFC 5545 / 6350 require)
# ---------------------------------------------------------------------------

# ics_event UID SUMMARY DTSTART DTEND [EXTRA_PROPERTY_LINES...]
# DTSTART/DTEND in UTC basic format, e.g. 20300601T100000Z.
ics_event() {
	local uid=$1 summary=$2 start=$3 end=$4 l
	shift 4
	printf 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VEVENT\r\n'
	printf 'UID:%s\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:%s\r\nDTEND:%s\r\nSUMMARY:%s\r\n' "$uid" "$start" "$end" "$summary"
	for l in "$@"; do printf '%s\r\n' "$l"; done
	printf 'END:VEVENT\r\nEND:VCALENDAR\r\n'
}

# ics_todo UID SUMMARY
ics_todo() {
	printf 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nBEGIN:VTODO\r\n'
	printf 'UID:%s\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:%s\r\nSTATUS:NEEDS-ACTION\r\n' "$1" "$2"
	printf 'END:VTODO\r\nEND:VCALENDAR\r\n'
}

# vcard UID FN [EXTRA_PROPERTY_LINES...]  (vCard 3.0)
vcard() {
	local uid=$1 fn=$2 l given family
	shift 2
	given=${fn%% *}
	family=${fn#* }
	printf 'BEGIN:VCARD\r\nVERSION:3.0\r\nUID:%s\r\nFN:%s\r\nN:%s;%s;;;\r\n' "$uid" "$fn" "$family" "$given"
	for l in "$@"; do printf '%s\r\n' "$l"; done
	printf 'END:VCARD\r\n'
}

# 1x1 transparent PNG (written to a file because bash strings drop NUL bytes).
png_fixture() {
	local f="$E2E_TMP/pixel.png"
	[[ -s $f ]] || printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==' | base64 -d >"$f"
	printf '%s' "$f"
}

# XML bodies used by several suites.
PROPFIND_PRINCIPAL='<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:a="urn:ietf:params:xml:ns:carddav">
  <d:prop><d:current-user-principal/><d:resourcetype/><c:calendar-home-set/><a:addressbook-home-set/></d:prop>
</d:propfind>'
PROPFIND_COLLECTION='<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:prop><d:resourcetype/><d:displayname/><d:sync-token/><cs:getctag/><d:supported-report-set/><d:current-user-privilege-set/></d:prop>
</d:propfind>'
PROPFIND_LIST='<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:displayname/><d:getetag/></d:prop></d:propfind>'

# sync_body [TOKEN] [EXTRA_XML] — WebDAV-Sync REPORT body (RFC 6578).
sync_body() {
	printf '<?xml version="1.0" encoding="utf-8"?>\n<d:sync-collection xmlns:d="DAV:"><d:sync-token>%s</d:sync-token><d:sync-level>1</d:sync-level>%s<d:prop><d:getetag/></d:prop></d:sync-collection>' "${1:-}" "${2:-}"
}
