# shellcheck shell=bash
# A throwaway local iCalendar feed for the subscription tests:
# `python3 -m http.server` serving one directory. The CalCard server must be
# able to reach it, so the URL uses an address of THIS machine that the server
# can route to (auto-detected, or E2E_FEED_HOST).

[[ -n ${_E2E_FEED_LOADED:-} ]] && return 0
_E2E_FEED_LOADED=1

E2E_FEED_PID=''
E2E_FEED_URL=''
E2E_FEED_DIR=''
E2E_FEED_UNAVAILABLE=''

_is_loopback() {
	case $1 in
	localhost | 127.* | ::1 | '[::1]') return 0 ;;
	*) return 1 ;;
	esac
}

# _feed_detect_host — sets E2E_FEED_HOST_RESOLVED or E2E_FEED_UNAVAILABLE.
_feed_detect_host() {
	if [[ -n $E2E_FEED_HOST ]]; then
		E2E_FEED_HOST_RESOLVED=$E2E_FEED_HOST
		return 0
	fi
	if _is_loopback "$E2E_TARGET_HOST"; then
		E2E_FEED_HOST_RESOLVED=127.0.0.1
		return 0
	fi
	local ip=$E2E_TARGET_HOST src=''
	if ! [[ $ip =~ ^[0-9.]+$ ]]; then
		ip=$(getent ahostsv4 "$E2E_TARGET_HOST" 2>/dev/null | head -n1 | cut -d' ' -f1)
	fi
	if [[ -n $ip ]] && command -v ip >/dev/null 2>&1; then
		src=$(ip -4 route get "$ip" 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -n1)
	fi
	if [[ -z $src ]]; then
		E2E_FEED_UNAVAILABLE="cannot work out which local address the server could reach (set E2E_FEED_HOST)"
		return 1
	fi
	E2E_FEED_HOST_RESOLVED=$src
}

# feed_start — start the feed server once. Returns 1 (with
# E2E_FEED_UNAVAILABLE set) when it cannot run here.
feed_start() {
	[[ -n $E2E_FEED_PID ]] && return 0
	[[ -n $E2E_FEED_UNAVAILABLE ]] && return 1
	if [[ ${E2E_FEED,,} == off ]]; then
		E2E_FEED_UNAVAILABLE="disabled by E2E_FEED=off"
		return 1
	fi
	if ! $E2E_HAVE_PYTHON; then
		E2E_FEED_UNAVAILABLE="python3 not installed (needed to serve a local feed)"
		return 1
	fi
	_feed_detect_host || return 1
	local bind=${E2E_FEED_BIND:-}
	if [[ -z $bind ]]; then
		if _is_loopback "$E2E_FEED_HOST_RESOLVED"; then bind=127.0.0.1; else bind=0.0.0.0; fi
	fi
	E2E_FEED_DIR="$E2E_TMP/feed"
	mkdir -p "$E2E_FEED_DIR"
	local port attempt
	for attempt in 1 2 3 4 5; do
		port=${E2E_FEED_PORT:-$((20000 + RANDOM % 30000))}
		python3 -m http.server --bind "$bind" --directory "$E2E_FEED_DIR" "$port" \
			>"$E2E_TMP/feed.log" 2>&1 &
		E2E_FEED_PID=$!
		local i
		for i in 1 2 3 4 5 6 7 8 9 10; do
			if curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$port/" 2>/dev/null ||
				curl -s -o /dev/null --max-time 2 "http://$E2E_FEED_HOST_RESOLVED:$port/" 2>/dev/null; then
				local h=$E2E_FEED_HOST_RESOLVED
				[[ $h == *:* ]] && h="[$h]"
				E2E_FEED_URL="http://$h:$port"
				info "      local feed server on $bind:$port, advertised as $E2E_FEED_URL"
				return 0
			fi
			kill -0 "$E2E_FEED_PID" 2>/dev/null || break
			sleep 0.3
		done
		kill "$E2E_FEED_PID" 2>/dev/null
		wait "$E2E_FEED_PID" 2>/dev/null
		E2E_FEED_PID=''
		[[ -n $E2E_FEED_PORT ]] && break
	done
	E2E_FEED_UNAVAILABLE="could not start python3 http.server (see $E2E_TMP/feed.log)"
	return 1
}

# feed_put NAME CONTENT — publish (or replace) a file on the feed server.
feed_put() { printf '%s' "$2" >"$E2E_FEED_DIR/$1"; }
# feed_rm NAME — make the file 404.
feed_rm() { rm -f "$E2E_FEED_DIR/$1"; }
# feed_hits NAME — how many GETs the feed server logged for NAME.
feed_hits() { grep -c "\"GET /$1 " "$E2E_TMP/feed.log" 2>/dev/null || true; }

feed_stop() {
	if [[ -n $E2E_FEED_PID ]]; then
		kill "$E2E_FEED_PID" 2>/dev/null
		wait "$E2E_FEED_PID" 2>/dev/null
		E2E_FEED_PID=''
	fi
	return 0
}
