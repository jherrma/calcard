# shellcheck shell=bash
# HTTP layer: one curl call per request, results captured in globals so the
# assertion helpers can inspect (and print) the last exchange.
#
#   RESP_STATUS   numeric status ("000" when curl itself failed)
#   RESP_HEADERS  raw response header block (last block when curl saw several)
#   RESP_BODY     response body as text (binary bodies: use RESP_BODY_FILE)
#   RESP_BODY_FILE path of the raw body bytes (overwritten by the next request)
#   REQ_LINE      "METHOD URL" of the last request
#   REQ_BODY      request body (text) of the last request
#
# NEVER call these helpers inside $(...): they set globals (and may refresh
# tokens), which a subshell would throw away.

[[ -n ${_E2E_HTTP_LOADED:-} ]] && return 0
_E2E_HTTP_LOADED=1

E2E_RATE_LIMIT_HITS=0
RESP_STATUS='' RESP_HEADERS='' RESP_BODY='' REQ_LINE='' REQ_BODY='' RESP_CURL_ERR=''

e2e_tmp_init() {
	E2E_TMP=$(mktemp -d "${TMPDIR:-/tmp}/calcard-e2e.XXXXXX") || die "mktemp failed"
	RESP_BODY_FILE="$E2E_TMP/resp.body"
}

# req METHOD URL [options]
#   --bearer TOKEN        Authorization: Bearer
#   --basic USER:PASS     HTTP Basic auth
#   --json BODY           JSON body (sets Content-Type)
#   --xml BODY            XML body (Content-Type application/xml; charset=utf-8)
#   --ics BODY            iCalendar body (text/calendar; charset=utf-8)
#   --vcf BODY            vCard body (text/vcard; charset=utf-8)
#   --data BODY           raw body, no Content-Type
#   --file PATH           raw body from file (binary safe)
#   --ctype TYPE          Content-Type header
#   --depth N             Depth header
#   -H 'Name: value'      extra header
#   --form 'k=@file'      multipart form field (curl -F)
#   --no-retry            do not retry on 429
#   anything else is passed to curl verbatim
req() {
	local method=$1 url=$2
	shift 2
	local -a args=(-sS --max-time "$E2E_TIMEOUT" --connect-timeout "$E2E_CONNECT_TIMEOUT"
		-o "$RESP_BODY_FILE" -D "$E2E_TMP/resp.headers" -w '%{http_code}')
	is_true "$E2E_INSECURE" && args+=(-k)
	local body='' has_body=0 retry=1 ctype='' auth_desc=''
	while (($#)); do
		case $1 in
		--bearer)
			args+=(-H "Authorization: Bearer $2")
			auth_desc='bearer'
			shift 2
			;;
		--basic)
			args+=(-u "$2")
			auth_desc="basic ${2%%:*}"
			shift 2
			;;
		--json) body=$2 has_body=1 ctype='application/json'; shift 2 ;;
		--xml) body=$2 has_body=1 ctype='application/xml; charset=utf-8'; shift 2 ;;
		--ics) body=$2 has_body=1 ctype='text/calendar; charset=utf-8'; shift 2 ;;
		--vcf) body=$2 has_body=1 ctype='text/vcard; charset=utf-8'; shift 2 ;;
		--data) body=$2 has_body=1; shift 2 ;;
		--file)
			args+=(--data-binary "@$2")
			body="<file $2>"
			shift 2
			;;
		--ctype) ctype=$2; shift 2 ;;
		--depth) args+=(-H "Depth: $2"); shift 2 ;;
		-H | --header) args+=(-H "$2"); shift 2 ;;
		--form)
			args+=(-F "$2")
			body="<multipart $2>"
			shift 2
			;;
		--no-retry) retry=0; shift ;;
		*) args+=("$1"); shift ;;
		esac
	done
	if ((has_body)); then
		# iCalendar/vCard builders are usually captured with $(...), which
		# strips the final LF of the last CRLF — put it back.
		if [[ $body == *$'\r' && ($ctype == text/calendar* || $ctype == text/vcard*) ]]; then
			body+=$'\n'
		fi
		printf '%s' "$body" >"$E2E_TMP/req.body"
		args+=(--data-binary "@$E2E_TMP/req.body")
	fi
	[[ -n $ctype ]] && args+=(-H "Content-Type: $ctype")
	if [[ $method == HEAD ]]; then
		args+=(--head)
	else
		args+=(-X "$method")
	fi
	REQ_LINE="$method $url${auth_desc:+  [$auth_desc]}"
	REQ_BODY=$body

	local attempt=0 code wait
	while :; do
		: >"$E2E_TMP/resp.headers"
		: >"$RESP_BODY_FILE"
		code=$(curl "${args[@]}" "$url" 2>"$E2E_TMP/curl.err")
		RESP_CURL_ERR=''
		if [[ -z $code || $code == 000 ]]; then
			code=000
			RESP_CURL_ERR=$(<"$E2E_TMP/curl.err")
		fi
		RESP_STATUS=$code
		_resp_load
		# With --head curl writes the header block into the body file.
		[[ $method == HEAD ]] && RESP_BODY=''
		vlog 1 "$REQ_LINE -> $RESP_STATUS"
		if ((${E2E_VERBOSE:-0} >= 2)); then
			[[ -n $REQ_BODY ]] && vlog 2 ">> ${REQ_BODY:0:600}"
			vlog 2 "<< ${RESP_BODY:0:600}"
		fi
		if [[ $RESP_STATUS == 429 ]] && ((retry)) && ((attempt < E2E_RATE_LIMIT_RETRIES)); then
			attempt=$((attempt + 1))
			E2E_RATE_LIMIT_HITS=$((E2E_RATE_LIMIT_HITS + 1))
			wait=$(hdr Retry-After)
			[[ $wait =~ ^[0-9]+$ ]] || wait=10
			((wait < 1)) && wait=1
			((wait > E2E_RATE_LIMIT_MAX_WAIT)) && wait=$E2E_RATE_LIMIT_MAX_WAIT
			info "      rate limited (429) on $method ${url#"$BASE_URL"}; waiting ${wait}s (retry $attempt/$E2E_RATE_LIMIT_RETRIES)"
			sleep "$wait"
			continue
		fi
		break
	done
	return 0
}

_resp_load() {
	local raw
	raw=$(<"$E2E_TMP/resp.headers")
	raw=${raw//$'\r'/}
	# Keep only the last header block (curl prints one per 1xx/redirect).
	if [[ $raw == *$'\n\nHTTP/'* ]]; then
		raw="HTTP/${raw##*$'\n\nHTTP/'}"
	fi
	RESP_HEADERS=$raw
	# Text view of the body; NUL bytes (binary) are dropped by bash.
	RESP_BODY=$(tr -d '\000' <"$RESP_BODY_FILE")
}

# hdr NAME — value of response header NAME (case-insensitive, last occurrence).
hdr() {
	local name=${1,,} line val=''
	while IFS= read -r line; do
		if [[ ${line,,} == "$name:"* ]]; then
			val=${line#*:}
			val=${val#"${val%%[![:space:]]*}"}
		fi
	done <<<"$RESP_HEADERS"
	printf '%s' "$val"
}

# jget FILTER — jq -r FILTER over the last response body (prints; safe in $()).
jget() { jq -r "$1" 2>/dev/null <<<"$RESP_BODY"; }

# resp_dump — human-readable summary of the last exchange (for failures).
resp_dump() {
	local b=$RESP_BODY ct
	ct=$(hdr Content-Type)
	printf '          %s> %s%s\n' "$C_DIM" "$REQ_LINE" "$C_RESET"
	if [[ -n $REQ_BODY ]]; then
		local rb=${REQ_BODY:0:300}
		rb=${rb//$'\r'/}
		printf '          %s> body: %s%s\n' "$C_DIM" "${rb//$'\n'/\\n}" "$C_RESET"
	fi
	if [[ $RESP_STATUS == 000 ]]; then
		printf '          < curl error: %s\n' "${RESP_CURL_ERR:-unknown}"
		return 0
	fi
	printf '          < HTTP %s%s\n' "$RESP_STATUS" "${ct:+  ($ct)}"
	local h
	for h in Location ETag WWW-Authenticate DAV Allow Retry-After Mcp-Session-Id; do
		[[ -n $(hdr "$h") ]] && printf '          < %s: %s\n' "$h" "$(hdr "$h")"
	done
	if [[ -n $b ]]; then
		if ((${#b} > E2E_BODY_PREVIEW)); then
			b="${b:0:E2E_BODY_PREVIEW}… (${#RESP_BODY} chars, truncated)"
		fi
		b=${b//$'\r'/}
		printf '          < body: %s\n' "${b//$'\n'/$'\n'                  }"
	fi
}

# ---------------------------------------------------------------------------
# Convenience wrappers. The acting user is $E2E_ACTOR (default U1); switch it
# for one call with:  as U2 api GET /calendars/
# ---------------------------------------------------------------------------

E2E_ACTOR=${E2E_ACTOR:-U1}

as() {
	local prev=$E2E_ACTOR rc
	E2E_ACTOR=$1
	shift
	"$@"
	rc=$?
	E2E_ACTOR=$prev
	return $rc
}

# api METHOD PATH [opts] — REST call under /api/v1 as the acting user (JWT).
api() {
	local method=$1 path=$2
	shift 2
	user_ensure_token "$E2E_ACTOR" || true
	local tokvar="${E2E_ACTOR}_TOKEN"
	req "$method" "$BASE_URL/api/v1$path" --bearer "${!tokvar}" "$@"
}

# api_anon METHOD PATH [opts] — REST call without credentials.
api_anon() {
	local method=$1 path=$2
	shift 2
	req "$method" "$BASE_URL/api/v1$path" "$@"
}

# dav METHOD PATH [opts] — DAV call as the acting user (Basic: email + app password).
dav() {
	local method=$1 path=$2
	shift 2
	user_ensure_dav "$E2E_ACTOR" || true
	local e="${E2E_ACTOR}_EMAIL" p="${E2E_ACTOR}_APPPW"
	req "$method" "$BASE_URL$path" --basic "${!e}:${!p}" "$@"
}

# raw METHOD PATH [opts] — any path on the server, no credentials.
raw() {
	local method=$1 path=$2
	shift 2
	req "$method" "$BASE_URL$path" "$@"
}
