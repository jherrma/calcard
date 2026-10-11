# shellcheck shell=bash
# Assertions. Each returns 0 on success and 1 on failure; a failure marks the
# running test FAILED (with the last request/response printed) but never
# aborts the run. Chain `|| return` when the rest of a test depends on it.

[[ -n ${_E2E_ASSERT_LOADED:-} ]] && return 0
_E2E_ASSERT_LOADED=1

# assert_status CODE [CODE...] [MESSAGE] — last response status is one of the
# codes. The first non-numeric argument is the failure message.
assert_status() {
	local -a codes=()
	local msg='' c
	for c in "$@"; do
		if [[ $c =~ ^[0-9]{3}$ ]]; then codes+=("$c"); else msg=$c; fi
	done
	for c in "${codes[@]}"; do
		[[ $RESP_STATUS == "$c" ]] && return 0
	done
	t_fail "${msg:+$msg: }expected HTTP $(_join ' or ' "${codes[@]}"), got $RESP_STATUS" with-response
	return 1
}

# assert_status_class 4xx [MESSAGE] — status is in the class (2xx/3xx/4xx/5xx).
assert_status_class() {
	local cls=${1:0:1}
	[[ ${RESP_STATUS:0:1} == "$cls" ]] && return 0
	t_fail "${2:+$2: }expected a ${cls}xx status, got $RESP_STATUS" with-response
	return 1
}

# assert_status_not CODE... [MESSAGE] — status is none of the codes.
assert_status_not() {
	local c msg=''
	for c in "$@"; do
		[[ $c =~ ^[0-9]{3}$ ]] || msg=$c
	done
	for c in "$@"; do
		if [[ $RESP_STATUS == "$c" ]]; then
			t_fail "${msg:+$msg: }status must not be $c" with-response
			return 1
		fi
	done
	return 0
}

# assert_json FILTER EXPECTED [MSG] — `jq -r FILTER` of the body equals EXPECTED.
assert_json() {
	local got
	if ! got=$(jq -r "$1" 2>/dev/null <<<"$RESP_BODY"); then
		t_fail "${3:-response is not JSON or filter '$1' failed}" with-response
		return 1
	fi
	[[ $got == "$2" ]] && return 0
	t_fail "${3:-JSON $1}: expected '$2', got '${got:0:200}'" with-response
	return 1
}

# assert_jq EXPR [MSG] — `jq -e EXPR` over the body is true.
assert_jq() {
	if jq -e "$1" >/dev/null 2>&1 <<<"$RESP_BODY"; then
		return 0
	fi
	t_fail "${2:-expected jq condition to hold}: $1" with-response
	return 1
}

# assert_json_nonempty FILTER [MSG]
assert_json_nonempty() {
	local got
	got=$(jq -r "$1 // empty" 2>/dev/null <<<"$RESP_BODY")
	[[ -n $got && $got != null ]] && return 0
	t_fail "${2:-expected non-empty $1}" with-response
	return 1
}

# assert_contains NEEDLE [MSG] / assert_not_contains NEEDLE [MSG] — body substring.
assert_contains() {
	[[ $RESP_BODY == *"$1"* ]] && return 0
	t_fail "${2:-body should contain '$1'}" with-response
	return 1
}
assert_not_contains() {
	[[ $RESP_BODY != *"$1"* ]] && return 0
	t_fail "${2:-body must not contain '$1'}" with-response
	return 1
}

# assert_match REGEX [MSG] — body matches an extended regex (bash =~).
assert_match() {
	[[ $RESP_BODY =~ $1 ]] && return 0
	t_fail "${2:-body should match /$1/}" with-response
	return 1
}

# assert_header NAME [SUBSTRING] [MSG] — header present (and contains SUBSTRING).
assert_header() {
	local v
	v=$(hdr "$1")
	if [[ -z $v ]]; then
		t_fail "${3:-missing response header $1}" with-response
		return 1
	fi
	if [[ -n ${2:-} && ${v,,} != *"${2,,}"* ]]; then
		t_fail "${3:-header $1 should contain '$2', got '$v'}" with-response
		return 1
	fi
	return 0
}

# assert_header_eq NAME VALUE [MSG]
assert_header_eq() {
	local v
	v=$(hdr "$1")
	[[ $v == "$2" ]] && return 0
	t_fail "${3:-header $1: expected '$2', got '$v'}" with-response
	return 1
}

assert_header_absent() {
	[[ -z $(hdr "$1") ]] && return 0
	t_fail "${2:-header $1 must be absent, got '$(hdr "$1")'}" with-response
	return 1
}

# assert_eq ACTUAL EXPECTED MSG / assert_ne ACTUAL UNEXPECTED MSG
assert_eq() {
	[[ $1 == "$2" ]] && return 0
	t_fail "${3:-values differ}: expected '$2', got '$1'"
	return 1
}
assert_ne() {
	[[ $1 != "$2" ]] && return 0
	t_fail "${3:-values must differ}: both are '$1'"
	return 1
}
# assert_true COND_DESCRIPTION COMMAND... — run a command, fail if it fails.
assert_true() {
	local msg=$1
	shift
	"$@" && return 0
	t_fail "$msg"
	return 1
}

# ---------------------------------------------------------------------------
# XML (WebDAV multistatus). XPath evaluation needs xmllint; without it the
# helpers degrade to substring checks on the raw body.
# ---------------------------------------------------------------------------

# lp a b c — namespace-agnostic XPath //*[local-name()='a']/*[local-name()='b']/...
# A '**' segment means "any depth" between its neighbours.
lp() {
	local out='' sep='//' seg
	for seg in "$@"; do
		if [[ $seg == '**' ]]; then
			sep='//'
			continue
		fi
		out+="$sep*[local-name()='$seg']"
		sep='/'
	done
	printf '%s' "$out"
}

# el NAME — a relative, namespace-agnostic step: *[local-name()='NAME'].
el() { printf "*[local-name()='%s']" "$1"; }

# xpath EXPR [XML] — evaluate an XPath (string result) against XML (default:
# last response body). Empty output when xmllint is absent or nothing matches.
xpath() {
	local expr=$1 xml=${2-$RESP_BODY}
	$E2E_HAVE_XMLLINT || return 0
	xmllint --nonet --xpath "$expr" - 2>/dev/null <<<"$xml" || true
}

# xml_text NAME [XML] — text of the first element whose local name is NAME.
xml_text() {
	local name=$1 xml=${2-$RESP_BODY}
	if $E2E_HAVE_XMLLINT; then
		xpath "string(($(lp '**' "$name"))[1])" "$xml"
	else
		sed -n "s/.*<\([A-Za-z0-9_-]*:\)\{0,1\}$name\(  *[^>]*\)\{0,1\}>\([^<]*\)<.*/\3/p" <<<"$xml" | head -n1
	fi
}

# assert_xml_wellformed — body parses as XML (skipped without xmllint).
assert_xml_wellformed() {
	$E2E_HAVE_XMLLINT || return 0
	if xmllint --nonet --noout - 2>/dev/null <<<"$RESP_BODY"; then
		return 0
	fi
	t_fail "response body is not well-formed XML" with-response
	return 1
}

# assert_xpath EXPR [MSG] — XPath (boolean) holds. Without xmllint the check
# is skipped (counts as pass) — the substring assertions around it still run.
assert_xpath() {
	$E2E_HAVE_XMLLINT || return 0
	local r
	r=$(xpath "boolean($1)")
	[[ $r == true ]] && return 0
	t_fail "${2:-XPath does not hold}: $1" with-response
	return 1
}

# assert_xpath_count EXPR N [MSG]
assert_xpath_count() {
	$E2E_HAVE_XMLLINT || return 0
	local r
	r=$(xpath "count($1)")
	[[ $r == "$2" ]] && return 0
	t_fail "${3:-XPath count of $1}: expected $2, got ${r:-?}" with-response
	return 1
}

# assert_xml_contains NEEDLE [MSG] — substring of the (XML) body, whitespace
# between tags ignored. Works with or without xmllint.
assert_xml_contains() { assert_contains "$@"; }

# ---------------------------------------------------------------------------
_join() {
	local sep=$1
	shift
	local out='' x
	for x in "$@"; do out+="${out:+$sep}$x"; done
	printf '%s' "$out"
}
