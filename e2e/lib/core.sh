# shellcheck shell=bash
# Core of the CalCard end-to-end suite: configuration, output, the test
# lifecycle (PASS/FAIL/SKIP/KFAIL/XPASS bookkeeping) and the cleanup registry.
#
# Sourced by lib/e2e.sh — never executed directly.

[[ -n ${_E2E_CORE_LOADED:-} ]] && return 0
_E2E_CORE_LOADED=1

E2E_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$E2E_LIB_DIR")"

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

e2e_init_colors() {
	if [[ -t 1 && -z ${NO_COLOR:-} && ${TERM:-dumb} != dumb ]]; then
		C_RED=$'\e[31m' C_GREEN=$'\e[32m' C_YELLOW=$'\e[33m' C_BLUE=$'\e[34m'
		C_MAGENTA=$'\e[35m' C_CYAN=$'\e[36m' C_DIM=$'\e[2m' C_BOLD=$'\e[1m' C_RESET=$'\e[0m'
	else
		C_RED='' C_GREEN='' C_YELLOW='' C_BLUE='' C_MAGENTA='' C_CYAN='' C_DIM='' C_BOLD='' C_RESET=''
	fi
}
e2e_init_colors

info() { printf '%s\n' "${C_DIM}$*${C_RESET}"; }
note() { printf '%s\n' "$*"; }
warn() { printf '%s\n' "${C_YELLOW}WARNING:${C_RESET} $*" >&2; }
die() {
	printf '%s\n' "${C_RED}${C_BOLD}ERROR:${C_RESET} $*" >&2
	exit 2
}
# vlog LEVEL MESSAGE — printed when E2E_VERBOSE >= LEVEL.
vlog() {
	local level=$1
	shift
	((${E2E_VERBOSE:-0} >= level)) && printf '%s\n' "${C_DIM}      $*${C_RESET}"
	return 0
}

now_s() { printf '%(%s)T' -1; }
now_ms() {
	if [[ -n ${EPOCHREALTIME:-} ]]; then
		local t=${EPOCHREALTIME/[.,]/}
		printf '%s' "${t:0:${#t}-3}"
	else
		printf '%s000' "$(now_s)"
	fi
}

# ---------------------------------------------------------------------------
# Configuration: e2e/.env (KEY=VALUE) with real environment variables winning.
# ---------------------------------------------------------------------------

e2e_load_env() {
	local file=${E2E_ENV_FILE:-$E2E_DIR/.env}
	if [[ ! -f $file ]]; then
		[[ -n ${E2E_ENV_FILE:-} ]] && die "E2E_ENV_FILE=$E2E_ENV_FILE does not exist"
		return 0
	fi
	local line key val n=0
	while IFS= read -r line || [[ -n $line ]]; do
		n=$((n + 1))
		line=${line%$'\r'}
		[[ $line =~ ^[[:space:]]*(#|$) ]] && continue
		line=${line#"${line%%[![:space:]]*}"}
		line=${line#export }
		if [[ $line =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
			key=${BASH_REMATCH[1]}
			val=${BASH_REMATCH[2]}
			if [[ $val =~ ^\"(.*)\"[[:space:]]*$ ]]; then
				val=${BASH_REMATCH[1]}
			elif [[ $val =~ ^\'(.*)\'[[:space:]]*$ ]]; then
				val=${BASH_REMATCH[1]}
			else
				val=${val%%[[:space:]]#*}
				val=${val%"${val##*[![:space:]]}"}
			fi
			# The shell environment always wins over the file.
			if [[ -z ${!key+x} ]]; then
				export "$key=$val"
			fi
		else
			warn "$file:$n: ignoring malformed line (expected KEY=VALUE)"
		fi
	done <"$file"
	E2E_ENV_FILE_USED=$file
}

is_true() {
	case "${1,,}" in
	1 | true | yes | on | y) return 0 ;;
	*) return 1 ;;
	esac
}

# Derive BASE_URL from CALCARD_URL, or from CALCARD_HOST (+ CALCARD_PORT,
# CALCARD_SCHEME). CALCARD_HOST may itself be a full URL or "host:port".
e2e_config() {
	local url=${CALCARD_URL:-} host=${CALCARD_HOST:-} port=${CALCARD_PORT:-} scheme=${CALCARD_SCHEME:-}
	if [[ -z $url ]]; then
		[[ -n $host ]] || die "No target configured. Set CALCARD_URL (e.g. http://192.168.1.10:8080) or CALCARD_HOST in e2e/.env or the environment — see e2e/.env.example."
		if [[ $host == *://* ]]; then
			url=$host
		else
			# Bare IPv6 literal → bracket it.
			if [[ $host == *:*:* && $host != \[* ]]; then
				host="[$host]"
			fi
			if [[ -z $scheme ]]; then
				if [[ $port == 443 ]]; then scheme=https; else scheme=http; fi
			fi
			url="$scheme://$host"
			if [[ -n $port ]]; then
				if [[ $host =~ :[0-9]+$ ]]; then
					warn "CALCARD_HOST already carries a port; ignoring CALCARD_PORT=$port"
				else
					url+=":$port"
				fi
			fi
		fi
	fi
	url=${url%/}
	[[ $url =~ ^(https?)://([^/]+)(/.*)?$ ]] || die "Cannot parse target URL '$url' (expected http(s)://host[:port])"
	BASE_URL=$url
	E2E_TARGET_SCHEME=${BASH_REMATCH[1]}
	E2E_TARGET_AUTHORITY=${BASH_REMATCH[2]}
	local hostpart=${E2E_TARGET_AUTHORITY##*@}
	if [[ $hostpart == \[* ]]; then
		E2E_TARGET_HOST=${hostpart%%]*}
		E2E_TARGET_HOST=${E2E_TARGET_HOST#[}
	else
		E2E_TARGET_HOST=${hostpart%%:*}
	fi
	if [[ -n ${BASH_REMATCH[3]} ]]; then
		warn "Target URL has a path component (${BASH_REMATCH[3]}). CalCard serves /api, /dav and /.well-known from the root; sub-path deployments are not supported by the server's absolute redirects."
	fi

	: "${E2E_TIMEOUT:=30}"
	: "${E2E_CONNECT_TIMEOUT:=10}"
	: "${E2E_HEALTH_WAIT:=60}"
	: "${E2E_INSECURE:=false}"
	: "${E2E_VERBOSE:=0}"
	: "${E2E_USER_PREFIX:=e2e}"
	: "${E2E_EMAIL_DOMAIN:=example.test}"
	: "${E2E_PASSWORD:=}"
	: "${E2E_SUITES:=}"
	: "${E2E_SKIP_SUITES:=}"
	: "${E2E_KEEP_DATA:=false}"
	: "${E2E_STRICT:=false}"
	: "${E2E_RATE_LIMIT_RETRIES:=6}"
	: "${E2E_RATE_LIMIT_MAX_WAIT:=90}"
	: "${E2E_RATE_LIMIT_BURST:=false}"
	: "${E2E_BODY_PREVIEW:=1200}"
	: "${E2E_FEED:=auto}"
	: "${E2E_FEED_HOST:=}"
	: "${E2E_FEED_BIND:=}"
	: "${E2E_FEED_PORT:=}"
	[[ $E2E_VERBOSE =~ ^[0-9]+$ ]] || E2E_VERBOSE=0
	[[ $E2E_USER_PREFIX =~ ^[a-z0-9][a-z0-9-]*$ ]] || die "E2E_USER_PREFIX must be lowercase letters, digits and '-' (got '$E2E_USER_PREFIX')"

	# One id per run: every name the suite creates carries it, so runs never
	# collide with each other or with real data.
	if [[ -z ${E2E_RUN_ID:-} ]]; then
		E2E_RUN_ID="$(printf '%(%Y%m%d%H%M%S)T' -1)$(printf '%04d' $((RANDOM % 10000)))"
	fi
	E2E_RUN_TAG="${E2E_USER_PREFIX}-${E2E_RUN_ID}"
}

# ---------------------------------------------------------------------------
# Tools
# ---------------------------------------------------------------------------

e2e_check_tools() {
	if ((BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 4))); then
		die "bash >= 4.4 is required (found $BASH_VERSION). On macOS: brew install bash, then run with that bash."
	fi
	local missing=() t
	for t in curl jq; do
		command -v "$t" >/dev/null 2>&1 || missing+=("$t")
	done
	for t in mktemp base64 od tr sed grep sort head; do
		command -v "$t" >/dev/null 2>&1 || missing+=("$t (coreutils)")
	done
	((${#missing[@]} == 0)) || die "Required tools missing: ${missing[*]}. Install them and retry."

	E2E_HAVE_XMLLINT=false
	command -v xmllint >/dev/null 2>&1 && E2E_HAVE_XMLLINT=true
	E2E_HAVE_PYTHON=false
	command -v python3 >/dev/null 2>&1 && E2E_HAVE_PYTHON=true
	E2E_HAVE_UNZIP=false
	command -v unzip >/dev/null 2>&1 && E2E_HAVE_UNZIP=true
}

# ---------------------------------------------------------------------------
# Test lifecycle
# ---------------------------------------------------------------------------

E2E_N_PASS=0 E2E_N_FAIL=0 E2E_N_SKIP=0 E2E_N_KFAIL=0 E2E_N_XPASS=0
E2E_FAILED_LIST=() E2E_KFAIL_LIST=() E2E_XPASS_LIST=() E2E_SKIP_LIST=()
E2E_SUITE=${E2E_SUITE:-misc}
E2E_CUR_TEST=''
E2E_CUR_FAILED=0
E2E_CUR_SKIP=''
E2E_CUR_KNOWN=''
E2E_CUR_DETAIL=''
E2E_CUR_START=0

suite_header() {
	printf '\n%s\n' "${C_BOLD}${C_BLUE}== $E2E_SUITE${C_RESET}${C_BOLD}: $*${C_RESET}"
}

t_begin() {
	E2E_CUR_TEST="$E2E_SUITE/$1"
	E2E_CUR_FAILED=0
	E2E_CUR_SKIP=''
	E2E_CUR_KNOWN=''
	E2E_CUR_DETAIL=''
	E2E_CUR_START=$(now_ms)
	vlog 1 "-- $E2E_CUR_TEST"
}

# t_known FINDING-ID — the current test is expected to fail because of a
# documented server finding (see e2e/FINDINGS.md). A failure is then reported
# as KFAIL; a pass as XPASS (the finding seems fixed — drop the marker).
t_known() { E2E_CUR_KNOWN=$1; }

# t_skip REASON — mark the running test as skipped (unless it already failed).
t_skip() { E2E_CUR_SKIP=${1:-skipped}; }

# t_fail MESSAGE [with-response] — record a failure in the running test.
t_fail() {
	E2E_CUR_FAILED=1
	E2E_CUR_DETAIL+="        ${C_RED}✗${C_RESET} $1"$'\n'
	if [[ ${2:-} == with-response ]]; then
		E2E_CUR_DETAIL+="$(resp_dump)"$'\n'
	fi
}

t_end() {
	local elapsed=$(($(now_ms) - E2E_CUR_START)) status label color extra=''
	if [[ -n $E2E_CUR_SKIP && $E2E_CUR_FAILED == 0 ]]; then
		status=SKIP color=$C_YELLOW extra=" — $E2E_CUR_SKIP"
		E2E_N_SKIP=$((E2E_N_SKIP + 1))
		E2E_SKIP_LIST+=("$E2E_CUR_TEST — $E2E_CUR_SKIP")
	elif [[ $E2E_CUR_FAILED == 1 && -n $E2E_CUR_KNOWN ]]; then
		if is_true "$E2E_STRICT"; then
			status=FAIL color=$C_RED
			E2E_N_FAIL=$((E2E_N_FAIL + 1))
			E2E_FAILED_LIST+=("$E2E_CUR_TEST [$E2E_CUR_KNOWN]")
		else
			status=KFAIL color=$C_MAGENTA
			E2E_N_KFAIL=$((E2E_N_KFAIL + 1))
			E2E_KFAIL_LIST+=("$E2E_CUR_TEST [$E2E_CUR_KNOWN]")
		fi
		extra=" [known server bug $E2E_CUR_KNOWN, see e2e/FINDINGS.md]"
	elif [[ $E2E_CUR_FAILED == 1 ]]; then
		status=FAIL color=$C_RED
		E2E_N_FAIL=$((E2E_N_FAIL + 1))
		E2E_FAILED_LIST+=("$E2E_CUR_TEST")
	elif [[ -n $E2E_CUR_KNOWN ]]; then
		status=XPASS color=$C_CYAN extra=" [expected failure $E2E_CUR_KNOWN no longer reproduces — update FINDINGS.md]"
		E2E_N_XPASS=$((E2E_N_XPASS + 1))
		E2E_XPASS_LIST+=("$E2E_CUR_TEST [$E2E_CUR_KNOWN]")
	else
		status=PASS color=$C_GREEN
		E2E_N_PASS=$((E2E_N_PASS + 1))
	fi
	label=$(printf '%-5s' "$status")
	printf '  %s%s%s %s%s %s\n' "$color" "$label" "$C_RESET" "$E2E_CUR_TEST" "$extra" "${C_DIM}(${elapsed}ms)${C_RESET}"
	if [[ -n $E2E_CUR_DETAIL && ($status == FAIL || $status == KFAIL || ${E2E_VERBOSE} -ge 1) ]]; then
		printf '%s' "$E2E_CUR_DETAIL"
	fi
	E2E_CUR_TEST=''
}

# t_run NAME FUNCTION [ARGS...] — run one test function between t_begin/t_end.
# The function uses assert_* helpers; a failing assertion does not abort the
# run, it only marks this test failed. Use `|| return` after an assertion
# when later steps make no sense without it.
t_run() {
	local name=$1
	shift
	t_begin "$name"
	"$@"
	t_end
}

# skip_test NAME REASON — report a test that is not run at all.
skip_test() {
	t_begin "$1"
	t_skip "$2"
	t_end
}

# need VAR... — precondition guard: skip the running test when a value an
# earlier step should have produced is empty.
need() {
	local v
	for v in "$@"; do
		if [[ -z ${!v:-} ]]; then
			t_skip "precondition not met: $v is empty (an earlier step failed)"
			return 1
		fi
	done
	return 0
}

# ---------------------------------------------------------------------------
# Cleanup registry (LIFO), executed on EXIT — also after failures and Ctrl-C.
# ---------------------------------------------------------------------------

E2E_CLEANUPS=()
E2E_CLEANUP_WARNINGS=()

# defer DESCRIPTION COMMAND [ARGS...] — register an undo action. Commands are
# shell-quoted now and evaluated at exit, so pass identifiers, not secrets that
# may change later (look those up inside the cleanup function instead).
defer() {
	local desc=$1 cmd
	shift
	printf -v cmd '%q ' "$@"
	E2E_CLEANUPS+=("$desc"$'\t'"$cmd")
}

run_cleanups() {
	local n=${#E2E_CLEANUPS[@]} i entry desc cmd
	((n == 0)) && return 0
	if is_true "$E2E_KEEP_DATA"; then
		warn "E2E_KEEP_DATA=true: skipping $n cleanup action(s); test data stays on the server."
		E2E_CLEANUPS=()
		return 0
	fi
	printf '\n%s\n' "${C_BOLD}== cleanup${C_RESET} ${C_DIM}($n action(s))${C_RESET}"
	E2E_IN_CLEANUP=1
	for ((i = n - 1; i >= 0; i--)); do
		entry=${E2E_CLEANUPS[i]}
		desc=${entry%%$'\t'*}
		cmd=${entry#*$'\t'}
		vlog 1 "cleanup: $desc"
		if ! eval "$cmd"; then
			E2E_CLEANUP_WARNINGS+=("$desc (last status ${RESP_STATUS:-?})")
			warn "cleanup failed: $desc (last HTTP status ${RESP_STATUS:-?})"
		fi
	done
	E2E_CLEANUPS=()
	E2E_IN_CLEANUP=0
	if ((${#E2E_CLEANUP_WARNINGS[@]} == 0)); then
		info "  all test data removed"
	fi
}

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

e2e_summary() {
	[[ -n ${E2E_SUMMARY_DONE:-} ]] && return 0
	E2E_SUMMARY_DONE=1
	local x
	printf '\n%s\n' "${C_BOLD}== summary${C_RESET} ${C_DIM}(target $BASE_URL, run $E2E_RUN_TAG)${C_RESET}"
	if ((${#E2E_SKIP_LIST[@]})); then
		printf '%s\n' "${C_YELLOW}Skipped:${C_RESET}"
		for x in "${E2E_SKIP_LIST[@]}"; do printf '  - %s\n' "$x"; done
	fi
	if ((${#E2E_KFAIL_LIST[@]})); then
		printf '%s\n' "${C_MAGENTA}Known server bugs (KFAIL, see e2e/FINDINGS.md):${C_RESET}"
		for x in "${E2E_KFAIL_LIST[@]}"; do printf '  - %s\n' "$x"; done
	fi
	if ((${#E2E_XPASS_LIST[@]})); then
		printf '%s\n' "${C_CYAN}Unexpected passes (XPASS — finding fixed? update FINDINGS.md):${C_RESET}"
		for x in "${E2E_XPASS_LIST[@]}"; do printf '  - %s\n' "$x"; done
	fi
	if ((${#E2E_FAILED_LIST[@]})); then
		printf '%s\n' "${C_RED}Failed:${C_RESET}"
		for x in "${E2E_FAILED_LIST[@]}"; do printf '  - %s\n' "$x"; done
	fi
	if ((${#E2E_CLEANUP_WARNINGS[@]})); then
		printf '%s\n' "${C_YELLOW}Cleanup problems (data may be left on the server):${C_RESET}"
		for x in "${E2E_CLEANUP_WARNINGS[@]}"; do printf '  - %s\n' "$x"; done
	fi
	printf '\n%sPASS %d%s  %sFAIL %d%s  %sSKIP %d%s  %sKFAIL %d%s  %sXPASS %d%s' \
		"$C_GREEN" "$E2E_N_PASS" "$C_RESET" "$C_RED" "$E2E_N_FAIL" "$C_RESET" \
		"$C_YELLOW" "$E2E_N_SKIP" "$C_RESET" "$C_MAGENTA" "$E2E_N_KFAIL" "$C_RESET" \
		"$C_CYAN" "$E2E_N_XPASS" "$C_RESET"
	if ((${E2E_RATE_LIMIT_HITS:-0} > 0)); then
		printf '  %s(429 retries: %d)%s' "$C_DIM" "$E2E_RATE_LIMIT_HITS" "$C_RESET"
	fi
	printf '\n'
}

e2e_exit_code() {
	if ((E2E_N_FAIL > 0)); then return 1; fi
	return 0
}
