#!/usr/bin/env bash
# MCP (story 104): access-token CRUD and the JSON-RPC endpoint at /mcp —
# handshake, sessions, tools (cross-checked through REST), resources,
# protocol errors, isolation between users, revocation.
set -u
SUITE_DESC_mcp="MCP tokens + POST /mcp: handshake, tools, resources, sessions, isolation"

suite_mcp() {
	ensure_user U1
	ensure_user U2
	raw POST /mcp --json '{"jsonrpc":"2.0","id":1,"method":"ping"}'
	if [[ $RESP_STATUS == 404 || $RESP_STATUS == 405 ]]; then
		skip_test "MCP endpoint" "POST /mcp is not routed (HTTP $RESP_STATUS) — MCP disabled on the target (CALDAV_MCP_ENABLED=false)"
		return 0
	fi
	mk_calendar MC_CAL "$(uniq mcp-cal)" || die "mcp: cannot create a calendar"
	mk_addressbook MC_AB "$(uniq mcp-book)" || die "mcp: cannot create an address book"
	t_run "token: create returns the secret once (calcard_mcp_…)" t_mc_token_create
	t_run "token: list shows the prefix, never the secret" t_mc_token_list
	t_run "token: validation and cross-user revoke" t_mc_token_validation
	t_run "unauthenticated /mcp is 401 with a Bearer challenge" t_mc_unauth
	t_run "initialize returns protocol version, capabilities and a session" t_mc_initialize
	t_run "notifications/initialized is accepted with 202 and no body" t_mc_initialized
	t_run "ping answers an empty result" t_mc_ping
	t_run "tools/list advertises the 13 tools with object schemas" t_mc_tools_list
	t_run "create_event over MCP is visible over REST" t_mc_create_event
	t_run "search_events and get_events find it" t_mc_search_events
	t_run "find_free_slots skips the busy time" t_mc_free_slots
	t_run "update_event / delete_event round trip" t_mc_update_delete_event
	t_run "contact tools: create, search, update, delete" t_mc_contacts
	t_run "resources/list and resources/read" t_mc_resources
	t_run "protocol errors: parse error, unknown method, unknown tool" t_mc_errors
	t_run "JSON-RPC batch requests are answered as a batch" t_mc_batch
	t_run "another user's calendar is unreachable through MCP" t_mc_isolation
	t_run "a session id is bound to its user" t_mc_session_binding
	t_run "GET /mcp returns the manifest" t_mc_manifest
	t_run "DELETE /mcp ends the session; unknown session is 404" t_mc_session_end
	t_run "a JWT access token is accepted as bearer" t_mc_jwt
	t_run "a revoked token is rejected on the next request" t_mc_revoke
}

MC_TOKEN='' MC_TOKEN_ID='' MC_SESSION=''

# mcp_call BODY [TOKEN] [SESSION] — POST /mcp.
mcp_call() {
	local body=$1 tok=${2-$MC_TOKEN} sess=${3-$MC_SESSION}
	local -a extra=(-H 'Accept: application/json, text/event-stream')
	[[ -n $tok ]] && extra+=(--bearer "$tok")
	[[ -n $sess ]] && extra+=(-H "Mcp-Session-Id: $sess")
	req POST "$BASE_URL/mcp" --json "$body" "${extra[@]}"
}

# mcp_tool NAME ARGS_JSON [ID] — tools/call; sets MC_PAYLOAD (parsed tool JSON) and MC_ISERR.
mcp_tool() {
	local id=${3:-$RANDOM}
	mcp_call "$(jq -nc --arg n "$1" --argjson a "$2" --argjson id "$id" '{jsonrpc:"2.0",id:$id,method:"tools/call",params:{name:$n,arguments:$a}}')"
	MC_ISERR=$(jget '.result.isError // false')
	MC_PAYLOAD=$(jget '.result.content[0].text // empty')
}

t_mc_token_create() {
	api POST /mcp-tokens/ --json "$(jobj name="$(uniq mcp-client)")"
	assert_status 201 || return
	MC_TOKEN=$(jget .token) MC_TOKEN_ID=$(jget .id)
	defer "revoke MCP token" cleanup_api U1 DELETE "/mcp-tokens/$MC_TOKEN_ID"
	[[ $MC_TOKEN == calcard_mcp_* ]] || t_fail "token must start with calcard_mcp_, got '${MC_TOKEN:0:16}…'"
	assert_json_nonempty .token_prefix
}

t_mc_token_list() {
	need MC_TOKEN_ID || return
	api GET /mcp-tokens/
	assert_status 200 || return
	assert_jq "[.tokens[] | select(.id == \"$MC_TOKEN_ID\")] | length == 1"
	assert_not_contains "$MC_TOKEN" "the secret must never be listed"
}

t_mc_token_validation() {
	api POST /mcp-tokens/ --json '{"name":""}'
	assert_status 400 "a name is required"
	api POST /mcp-tokens/ --json '{"name":"x","expires_at":"next week"}'
	assert_status 400 "expires_at must be RFC 3339"
	need MC_TOKEN_ID || return
	as U2 api DELETE "/mcp-tokens/$MC_TOKEN_ID"
	assert_status 404 "another user cannot revoke the token"
	api_anon GET /mcp-tokens/
	assert_status 401
}

t_mc_unauth() {
	mcp_call '{"jsonrpc":"2.0","id":1,"method":"initialize"}' '' ''
	assert_status 401 || return
	assert_header WWW-Authenticate Bearer
	mcp_call '{"jsonrpc":"2.0","id":1,"method":"initialize"}' "calcard_mcp_not-a-real-token" ''
	assert_status 401 "an unknown token"
}

t_mc_initialize() {
	need MC_TOKEN || return
	mcp_call '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"calcard-e2e","version":"1"}}}' "$MC_TOKEN" ''
	assert_status 200 || return
	MC_SESSION=$(hdr Mcp-Session-Id)
	[[ -n $MC_SESSION ]] || t_fail "initialize must return an Mcp-Session-Id header" with-response
	assert_json .jsonrpc 2.0
	assert_json .id 1
	assert_json .result.protocolVersion 2025-06-18
	assert_json .result.serverInfo.name calcard
	assert_jq '.result.capabilities | has("tools") and has("resources")'
}

t_mc_initialized() {
	need MC_SESSION || return
	mcp_call '{"jsonrpc":"2.0","method":"notifications/initialized"}'
	assert_status 202 || return
	[[ -z $RESP_BODY ]] || t_fail "a notification must get an empty body, got '${RESP_BODY:0:80}'"
}

t_mc_ping() {
	need MC_SESSION || return
	mcp_call '{"jsonrpc":"2.0","id":2,"method":"ping"}'
	assert_status 200 || return
	assert_jq '.result == {} and (has("error") | not)'
}

t_mc_tools_list() {
	need MC_SESSION || return
	mcp_call '{"jsonrpc":"2.0","id":3,"method":"tools/list"}'
	assert_status 200 || return
	local t
	for t in list_calendars get_events create_event update_event delete_event search_events \
		list_address_books get_contacts search_contacts create_contact update_contact delete_contact find_free_slots; do
		assert_jq "[.result.tools[].name] | index(\"$t\") != null" "tool $t must be advertised"
	done
	assert_jq '[.result.tools[] | .inputSchema.type == "object" and (.description | length > 0)] | all' \
		"every tool needs a description and an object input schema"
}

MC_EVENT_ID=''
t_mc_create_event() {
	need MC_SESSION || return
	mcp_tool create_event "$(jq -nc --arg c "$MC_CAL_UUID" '{calendar_id:$c,title:"MCP standup",start:"2030-06-05T09:00:00Z",end:"2030-06-05T09:30:00Z",location:"Room 1"}')"
	assert_status 200 || return
	assert_eq "$MC_ISERR" false "create_event must succeed: ${MC_PAYLOAD:0:200}" || return
	MC_EVENT_ID=$(jq -r '.event.id' <<<"$MC_PAYLOAD")
	[[ -n $MC_EVENT_ID && $MC_EVENT_ID != null ]] || t_fail "create_event must return the event id: $MC_PAYLOAD"
	api GET "/calendars/$MC_CAL_UUID/events?start=2030-06-01T00:00:00Z&end=2030-07-01T00:00:00Z"
	assert_jq '[.events[] | select(.summary == "MCP standup" and .location == "Room 1")] | length == 1' \
		"the event written through MCP must be visible over REST"
}

t_mc_search_events() {
	need MC_EVENT_ID || return
	mcp_tool search_events '{"query":"standup"}'
	assert_eq "$MC_ISERR" false || return
	jq -e '[.matches[].event.title] | index("MCP standup") != null' >/dev/null 2>&1 <<<"$MC_PAYLOAD" ||
		t_fail "search_events must find the event: ${MC_PAYLOAD:0:300}"
	mcp_tool get_events "$(jq -nc --arg c "$MC_CAL_UUID" '{calendar_id:$c,start:"2030-06-01T00:00:00Z",end:"2030-07-01T00:00:00Z"}')"
	assert_eq "$MC_ISERR" false "get_events: ${MC_PAYLOAD:0:200}" || return
	[[ $MC_PAYLOAD == *"MCP standup"* ]] || t_fail "get_events must list the event: ${MC_PAYLOAD:0:300}"
	mcp_tool list_calendars '{}'
	[[ $MC_PAYLOAD == *"$MC_CAL_UUID"* ]] || t_fail "list_calendars must include the calendar"
}

t_mc_free_slots() {
	need MC_EVENT_ID || return
	mcp_tool find_free_slots '{"start":"2030-06-05T09:00:00Z","end":"2030-06-05T12:00:00Z","duration_minutes":60}'
	assert_eq "$MC_ISERR" false "find_free_slots: ${MC_PAYLOAD:0:200}" || return
	jq -e '.free_slots | length == 1 and .[0].start == "2030-06-05T09:30:00Z"' >/dev/null 2>&1 <<<"$MC_PAYLOAD" ||
		t_fail "the 09:00–09:30 event must leave exactly 09:30–12:00 free: $MC_PAYLOAD"
}

t_mc_update_delete_event() {
	need MC_EVENT_ID || return
	mcp_tool update_event "$(jq -nc --arg e "$MC_EVENT_ID" '{event_id:$e,title:"MCP standup (moved)"}')"
	assert_eq "$MC_ISERR" false "update_event: ${MC_PAYLOAD:0:200}" || return
	api GET "/calendars/$MC_CAL_UUID/events/$MC_EVENT_ID"
	assert_json .summary "MCP standup (moved)" "REST must see the MCP update"
	mcp_tool delete_event "$(jq -nc --arg e "$MC_EVENT_ID" '{event_id:$e}')"
	assert_eq "$MC_ISERR" false "delete_event: ${MC_PAYLOAD:0:200}" || return
	api GET "/calendars/$MC_CAL_UUID/events/$MC_EVENT_ID"
	assert_status 404 "the event deleted over MCP is gone"
}

t_mc_contacts() {
	need MC_SESSION || return
	mcp_tool create_contact "$(jq -nc --arg a "$MC_AB_UUID" '{address_book_id:$a,first_name:"Jane",last_name:"Mcpsmith",email:"jane.mcp@example.test"}')"
	assert_eq "$MC_ISERR" false "create_contact: ${MC_PAYLOAD:0:200}" || return
	local cid
	cid=$(jq -r '.contact.id' <<<"$MC_PAYLOAD")
	api GET "/addressbooks/$MC_AB_UUID/contacts/$cid"
	assert_status 200 "the contact created over MCP exists in REST" || return
	mcp_tool search_contacts '{"query":"mcpsmith"}'
	jq -e '.matches | length >= 1' >/dev/null 2>&1 <<<"$MC_PAYLOAD" || t_fail "search_contacts must find it: ${MC_PAYLOAD:0:200}"
	mcp_tool update_contact "$(jq -nc --arg c "$cid" '{contact_id:$c,last_name:"Mcpjones"}')"
	assert_eq "$MC_ISERR" false "update_contact: ${MC_PAYLOAD:0:200}"
	mcp_tool get_contacts "$(jq -nc --arg a "$MC_AB_UUID" '{address_book_id:$a}')"
	[[ $MC_PAYLOAD == *Mcpjones* ]] || t_fail "get_contacts must show the update: ${MC_PAYLOAD:0:200}"
	mcp_tool delete_contact "$(jq -nc --arg c "$cid" '{contact_id:$c}')"
	assert_eq "$MC_ISERR" false "delete_contact: ${MC_PAYLOAD:0:200}"
	api GET "/addressbooks/$MC_AB_UUID/contacts/$cid"
	assert_status 404 "the contact deleted over MCP is gone"
}

t_mc_resources() {
	need MC_SESSION || return
	mcp_call '{"jsonrpc":"2.0","id":10,"method":"resources/list"}'
	assert_status 200 || return
	assert_jq '[.result.resources[].uri] | (index("calendars://list") != null) and (index("contacts://list") != null)'
	assert_jq "[.result.resources[].uri] | index(\"calendars://$MC_CAL_UUID/events\") != null"
	mcp_call '{"jsonrpc":"2.0","id":11,"method":"resources/read","params":{"uri":"calendars://list"}}'
	assert_status 200 || return
	assert_json '.result.contents[0].mimeType' application/json
	assert_jq ".result.contents[0].text | contains(\"$(uniq mcp-cal)\")"
	mcp_call '{"jsonrpc":"2.0","id":12,"method":"resources/read","params":{"uri":"calendars://00000000-0000-0000-0000-000000000000/events"}}'
	assert_jq 'has("error")' "reading an unknown resource is a JSON-RPC error"
}

t_mc_errors() {
	need MC_SESSION || return
	req POST "$BASE_URL/mcp" --bearer "$MC_TOKEN" -H "Mcp-Session-Id: $MC_SESSION" --data '{"jsonrpc":' --ctype application/json
	assert_status 400 || return
	assert_json .error.code -32700 "parse error"
	mcp_call '{"jsonrpc":"2.0","id":20,"method":"no/such/method"}'
	assert_json .error.code -32601 "method not found"
	mcp_tool no_such_tool '{}' 21
	assert_jq 'has("error") or .result.isError == true' "an unknown tool must be reported as an error"
	mcp_tool create_event '{"calendar_id":"x"}' 22
	assert_jq 'has("error") or .result.isError == true' "invalid tool arguments must be reported as an error"
}

t_mc_batch() {
	need MC_SESSION || return
	mcp_call '[{"jsonrpc":"2.0","id":31,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":32,"method":"tools/list"}]'
	assert_status 200 || return
	assert_jq 'type == "array" and length == 2 and ([.[].id] | sort == [31,32])' \
		"two responses (the notification gets none), matched by id"
}

t_mc_isolation() {
	need MC_TOKEN || return
	as U2 api POST /mcp-tokens/ --json "$(jobj name="$(uniq outsider)")"
	assert_status 201 || return
	local tok id
	tok=$(jget .token) id=$(jget .id)
	defer "revoke U2 MCP token" cleanup_api U2 DELETE "/mcp-tokens/$id"
	MC_U2_TOKEN=$tok
	mcp_call "$(jq -nc --arg c "$MC_CAL_UUID" '{jsonrpc:"2.0",id:40,method:"tools/call",params:{name:"get_events",arguments:{calendar_id:$c}}}')" "$tok" ''
	assert_status 200 || return
	assert_jq '.result.isError == true or has("error")' "U2 reading U1's calendar must be refused"
	assert_not_contains "MCP standup"
	mcp_call "$(jq -nc --arg c "$MC_CAL_UUID" '{jsonrpc:"2.0",id:41,method:"tools/call",params:{name:"create_event",arguments:{calendar_id:$c,title:"planted",start:"2030-06-06T09:00:00Z",end:"2030-06-06T10:00:00Z"}}}')" "$tok" ''
	assert_jq '.result.isError == true or has("error")' "U2 writing into U1's calendar must be refused"
	mcp_call '{"jsonrpc":"2.0","id":42,"method":"resources/read","params":{"uri":"calendars://list"}}' "$tok" ''
	assert_not_contains "$(uniq mcp-cal)" "U2's calendar list must not include U1's calendar"
	api GET "/calendars/$MC_CAL_UUID/events?start=2030-06-01T00:00:00Z&end=2030-07-01T00:00:00Z"
	assert_jq '[.events[] | select(.summary == "planted")] | length == 0' "nothing may have been planted"
}

t_mc_session_binding() {
	need MC_SESSION MC_U2_TOKEN || return
	mcp_call '{"jsonrpc":"2.0","id":50,"method":"ping"}' "$MC_U2_TOKEN" "$MC_SESSION"
	assert_status 404 403 401 "U1's session id presented with U2's token"
}

t_mc_manifest() {
	need MC_TOKEN || return
	req GET "$BASE_URL/mcp" --bearer "$MC_TOKEN"
	assert_status 200 || return
	assert_json .name calcard
	assert_json .transport streamable-http
	assert_jq '.tools | length == 13'
}

t_mc_session_end() {
	need MC_SESSION || return
	req DELETE "$BASE_URL/mcp" --bearer "$MC_TOKEN" -H "Mcp-Session-Id: $MC_SESSION"
	assert_status 204 200 || return
	mcp_call '{"jsonrpc":"2.0","id":60,"method":"ping"}'
	assert_status 404 "a terminated session must 404 (the client's cue to re-initialize)"
	mcp_call '{"jsonrpc":"2.0","id":61,"method":"ping"}' "$MC_TOKEN" "not-a-real-session-$E2E_RUN_ID"
	assert_status 404 "an unknown session id"
	MC_SESSION=''
}

t_mc_jwt() {
	user_ensure_token U1
	mcp_call '{"jsonrpc":"2.0","id":70,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e-jwt","version":"1"}}}' "$(uget U1 TOKEN)" ''
	assert_status 200 || return
	assert_json .result.serverInfo.name calcard
}

t_mc_revoke() {
	api POST /mcp-tokens/ --json "$(jobj name="$(uniq short-lived)")"
	assert_status 201 || return
	local tok id
	tok=$(jget .token) id=$(jget .id)
	defer "revoke MCP token" cleanup_api U1 DELETE "/mcp-tokens/$id"
	mcp_call '{"jsonrpc":"2.0","id":80,"method":"ping"}' "$tok" ''
	assert_status 200 "the token works before revocation" || return
	api DELETE "/mcp-tokens/$id"
	assert_status 204 || return
	mcp_call '{"jsonrpc":"2.0","id":81,"method":"ping"}' "$tok" ''
	assert_status 401 "revocation must take effect on the very next request"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main mcp "$@"
fi
