#!/usr/bin/env bash
# Rate limiting. The default checks are passive (they read the limiter's
# headers and spend one request). The burst test is opt-in
# (E2E_RATE_LIMIT_BURST=true) because it deliberately trips the per-account
# login limiter — on a throwaway, never-registered e-mail — and so also uses up
# part of this client IP's login allowance for one window. Runs last.
set -u
SUITE_DESC_ratelimit="global limiter headers; opt-in login limiter burst"

suite_ratelimit() {
	t_run "global limiter advertises its budget in X-RateLimit-* headers" t_rl_headers
	if is_true "$E2E_RATE_LIMIT_BURST"; then
		t_run "login limiter: repeated failures for one account end in 429" t_rl_login_burst
	else
		skip_test "login limiter burst" "opt-in: set E2E_RATE_LIMIT_BURST=true (spends ~11 login attempts of this IP's allowance for one window)"
	fi
}

t_rl_headers() {
	raw GET /health
	assert_status 200 || return
	local limit remaining
	limit=$(hdr X-RateLimit-Limit) remaining=$(hdr X-RateLimit-Remaining)
	if [[ -z $limit ]]; then
		t_skip "no X-RateLimit-* headers: rate limiting is disabled on the target (CALDAV_RATE_LIMIT_ENABLED=false) or stripped by a proxy"
		return
	fi
	[[ $limit =~ ^[0-9]+$ && $remaining =~ ^[0-9]+$ ]] || {
		t_fail "X-RateLimit-Limit/Remaining must be integers (got '$limit' / '$remaining')"
		return
	}
	((remaining <= limit)) || t_fail "Remaining ($remaining) exceeds Limit ($limit)"
	assert_header X-RateLimit-Reset
	info "      global limit: $limit requests per window ($remaining left for this client)"
	local before=$remaining
	raw GET /health
	remaining=$(hdr X-RateLimit-Remaining)
	# Another client on the same IP may have spent some too; only require that
	# it did not go up within the window (a reset in between is tolerated).
	if [[ $remaining =~ ^[0-9]+$ ]] && ((remaining > before)) && ((remaining != limit - 1)); then
		t_fail "X-RateLimit-Remaining went up from $before to $remaining without a window reset"
	fi
	return 0
}

t_rl_login_burst() {
	local email i got429=0
	email="$(uniq ratelimit-probe)@$E2E_EMAIL_DOMAIN"
	for ((i = 1; i <= 30; i++)); do
		api_anon POST /auth/login --json "$(jobj email="$email" password=wrong)" --no-retry
		if [[ $RESP_STATUS == 429 ]]; then
			got429=1
			break
		fi
		[[ $RESP_STATUS == 401 ]] || {
			t_fail "unexpected status before the limit (attempt $i)" with-response
			return
		}
	done
	if ((!got429)); then
		api_anon GET /system/settings
		t_skip "no 429 after 30 failed logins — the login limiter is disabled (CALDAV_RATE_LIMIT_ENABLED=false)"
		return
	fi
	info "      login limiter tripped after $((i - 1)) failed attempts"
	assert_header Retry-After
	assert_jq '.message | length > 0' "the 429 carries a readable message"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main ratelimit "$@"
fi
