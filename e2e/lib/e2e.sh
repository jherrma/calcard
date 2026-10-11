# shellcheck shell=bash
# Entry point of the shared library: sources the parts and provides session
# setup/teardown used by run.sh and by every suite script run standalone.

[[ -n ${_E2E_LOADED:-} ]] && return 0
_E2E_LOADED=1

_e2e_here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/core.sh
source "$_e2e_here/core.sh"
# shellcheck source=lib/http.sh
source "$_e2e_here/http.sh"
# shellcheck source=lib/assert.sh
source "$_e2e_here/assert.sh"
# shellcheck source=lib/fixtures.sh
source "$_e2e_here/fixtures.sh"
# shellcheck source=lib/feed.sh
source "$_e2e_here/feed.sh"
unset _e2e_here

# e2e_session_start — config, tool check, temp dir, exit trap, wait for
# /health, server preflight. Idempotent.
e2e_session_start() {
	[[ -n ${E2E_SESSION_STARTED:-} ]] && return 0
	E2E_SESSION_STARTED=1
	e2e_check_tools
	e2e_load_env
	e2e_init_colors
	e2e_config
	e2e_tmp_init
	trap 'e2e_session_end' EXIT
	trap 'printf "\n"; warn "interrupted — cleaning up"; exit 130' INT TERM

	printf '%s\n' "${C_BOLD}CalCard e2e${C_RESET} — target ${C_BOLD}$BASE_URL${C_RESET}  run ${E2E_RUN_TAG}${E2E_ENV_FILE_USED:+  ${C_DIM}(config: $E2E_ENV_FILE_USED)${C_RESET}}"
	info "tools: curl $(curl --version | head -n1 | cut -d' ' -f2), jq $(jq --version 2>/dev/null | tr -d 'jq-'), xmllint: $E2E_HAVE_XMLLINT, python3: $E2E_HAVE_PYTHON"
	$E2E_HAVE_XMLLINT || warn "xmllint not found: XPath checks on DAV responses degrade to substring checks."
	e2e_wait_for_health
	e2e_preflight
}

e2e_wait_for_health() {
	local deadline=$(($(now_s) + E2E_HEALTH_WAIT)) shown=0
	while :; do
		raw GET /health --no-retry
		if [[ $RESP_STATUS == 200 && $(jget .status) == ok ]]; then
			info "server healthy (GET /health)"
			return 0
		fi
		if (($(now_s) >= deadline)); then
			die "server at $BASE_URL did not become healthy within ${E2E_HEALTH_WAIT}s (last: HTTP $RESP_STATUS${RESP_CURL_ERR:+, $RESP_CURL_ERR}).
       Check CALCARD_URL / E2E_INSECURE (self-signed TLS) / network reachability."
		fi
		if ((!shown)); then
			info "waiting for $BASE_URL/health (up to ${E2E_HEALTH_WAIT}s)…"
			shown=1
		fi
		sleep 2
	done
}

e2e_session_end() {
	local rc=$?
	trap - EXIT
	run_cleanups
	feed_stop
	[[ -n ${E2E_TMP:-} && -d $E2E_TMP ]] && rm -rf "$E2E_TMP"
	# A run that aborted before any test ran has nothing to summarize.
	if ((rc != 2 || E2E_N_PASS + E2E_N_FAIL + E2E_N_SKIP + E2E_N_KFAIL + E2E_N_XPASS > 0)); then
		e2e_summary
	fi
	if ((rc == 130)); then exit 130; fi
	if ((rc == 2)); then exit 2; fi
	e2e_exit_code
	exit $?
}

# Suite registry --------------------------------------------------------------
# A suite file NN_name.sh defines:  SUITE_DESC_name="…"  and  suite_name() { … }
# and ends with:  e2e_suite_main name "$@"   (runs it when executed directly).

# e2e_run_suite NAME — run one suite (defined function suite_NAME).
e2e_run_suite() {
	local name=$1 descvar="SUITE_DESC_$1"
	E2E_SUITE=$name
	suite_header "${!descvar:-}"
	E2E_ACTOR=U1
	"suite_$name"
	E2E_ACTOR=U1
}

# e2e_suite_main NAME "$@" — standalone execution of a single suite file.
e2e_suite_main() {
	local name=$1
	shift
	case ${1:-} in
	-h | --help)
		printf 'Usage: %s\nRuns the "%s" suite alone against CALCARD_URL (see e2e/README.md).\n' "$0" "$name"
		exit 0
		;;
	-v) E2E_VERBOSE=1 ;;
	-vv) E2E_VERBOSE=2 ;;
	esac
	e2e_session_start
	e2e_run_suite "$name"
	exit 0 # the EXIT trap prints the summary and sets the real exit code
}
