#!/usr/bin/env bash
# CalCard end-to-end suite — black-box tests against a deployed server.
#
#   ./run.sh                 run every suite
#   ./run.sh dav_caldav auth run the named suites (in the given order)
#   ./run.sh --list          list the suites
#   ./run.sh -v | -vv        log every request (-vv: and bodies)
#
# Configuration: e2e/.env (copy .env.example) — environment variables win.
# See e2e/README.md.
set -u

E2E_HOME="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/e2e.sh
source "$E2E_HOME/lib/e2e.sh"

# Discover suites: NN_name.sh, in file-name order.
E2E_ALL_SUITES=()
for f in "$E2E_HOME"/[0-9][0-9]_*.sh; do
	[[ -e $f ]] || continue
	# shellcheck source=/dev/null
	source "$f"
	n=${f##*/}
	n=${n#[0-9][0-9]_}
	n=${n%.sh}
	E2E_ALL_SUITES+=("$n")
done
unset f n

usage() {
	cat <<EOF
Usage: ./run.sh [options] [suite...]

Options:
  --list       list available suites and exit
  -v, -vv      verbose: log each request (-vv also logs bodies)
  --strict     count known server bugs (KFAIL) as failures
  --keep-data  do not delete the test users / data at the end (debugging)
  -h, --help   this help

Suites run in the order given (default: all, in file order). Suite selection
can also come from E2E_SUITES / E2E_SKIP_SUITES (space or comma separated).
EOF
}

list_suites() {
	local s d
	for s in "${E2E_ALL_SUITES[@]}"; do
		d="SUITE_DESC_$s"
		printf '  %-16s %s\n' "$s" "${!d:-}"
	done
}

selected=()
while (($#)); do
	case $1 in
	--list)
		echo "Available suites:"
		list_suites
		exit 0
		;;
	-h | --help)
		usage
		exit 0
		;;
	-v) E2E_VERBOSE=1 ;;
	-vv) E2E_VERBOSE=2 ;;
	--strict) E2E_STRICT=true ;;
	--keep-data) E2E_KEEP_DATA=true ;;
	-*)
		usage >&2
		die "unknown option $1"
		;;
	*) selected+=("${1//,/ }") ;;
	esac
	shift
done

# Resolve the selection (args > E2E_SUITES > all), minus E2E_SKIP_SUITES —
# before contacting the server, so a typo fails fast.
e2e_load_env
if ((${#selected[@]} == 0)) && [[ -n ${E2E_SUITES:-} ]]; then
	read -r -a selected <<<"${E2E_SUITES//,/ }"
fi
if ((${#selected[@]} == 0)); then
	selected=("${E2E_ALL_SUITES[@]}")
else
	# Words may contain spaces from "a,b" splitting above.
	read -r -a selected <<<"${selected[*]}"
fi
skips=${E2E_SKIP_SUITES:-}
read -r -a skip <<<"${skips//,/ }"
plan=()
for s in "${selected[@]}"; do
	if ! declare -F "suite_$s" >/dev/null; then
		die "unknown suite '$s' — available: ${E2E_ALL_SUITES[*]}"
	fi
	for k in "${skip[@]}"; do
		[[ $k == "$s" ]] && continue 2
	done
	plan+=("$s")
done
((${#plan[@]})) || die "nothing to run (all selected suites are in E2E_SKIP_SUITES)"

e2e_session_start
info "suites: ${plan[*]}"

for s in "${plan[@]}"; do
	e2e_run_suite "$s"
done
exit 0 # the EXIT trap runs cleanup, prints the summary and sets the exit code
