"""MCP (story 104): access-token CRUD and the JSON-RPC endpoint at /mcp —
handshake, sessions, tools (cross-checked through REST), resources,
protocol errors, isolation between users, revocation."""

import json
import random
from types import SimpleNamespace

import pytest

from harness.checks import jpath
from harness.http import NOT_JSON

DESCRIPTION = "MCP tokens + POST /mcp: handshake, tools, resources, sessions, isolation"

pytestmark = pytest.mark.usefixtures("u1", "u2")

_DEFAULT = object()


@pytest.fixture(scope="module", autouse=True)
def mc(e2e, u1, u2):
    """Feature detection, then one calendar and one address book for the tools.
    (The bash suite reported a disabled MCP as ONE skipped test, "MCP
    endpoint"; here every test of the suite is skipped with that reason.)"""
    r = e2e.raw("POST", "/mcp", json='{"jsonrpc":"2.0","id":1,"method":"ping"}')
    if r.status in (404, 405):
        pytest.skip(f"POST /mcp is not routed (HTTP {r.status}) — MCP disabled on the target (CALDAV_MCP_ENABLED=false)")
    cal = u1.mk_calendar(e2e.uniq("mcp-cal"))
    ab = u1.mk_addressbook(e2e.uniq("mcp-book"))
    return SimpleNamespace(cal=cal["uuid"], ab=ab["UUID"])


def mcp_call(e2e, st, body, tok=_DEFAULT, sess=_DEFAULT):
    """POST /mcp (token / session default to the suite's current ones; pass '' for none)."""
    tok = getattr(st, "mc_token", "") if tok is _DEFAULT else tok
    sess = getattr(st, "mc_session", "") if sess is _DEFAULT else sess
    headers = {"Accept": "application/json, text/event-stream"}
    if sess:
        headers["Mcp-Session-Id"] = sess
    return e2e.req("POST", f"{e2e.base}/mcp", json=body, bearer=tok or None, headers=headers)


def mcp_tool(e2e, st, name, args, rid=None):
    """tools/call → (resp, is_error, payload text). is_error is None when the
    response is not JSON-RPC at all."""
    rid = random.randrange(32768) if rid is None else rid
    r = mcp_call(e2e, st, {"jsonrpc": "2.0", "id": rid, "method": "tools/call",
                           "params": {"name": name, "arguments": args}})
    j = r.json()
    if j is NOT_JSON:
        return r, None, ""
    is_err = jpath(j, "result.isError") is True
    text = jpath(j, "result.content[0].text")
    return r, is_err, text if isinstance(text, str) else ""


def pj(payload, path):
    """jq-like read of the tool payload (JSON text)."""
    try:
        return jpath(json.loads(payload), path)
    except ValueError:
        return None


def test_token_create(t, e2e, u1, st):
    """token: create returns the secret once (calcard_mcp_…)"""
    r = u1.api("POST", "/mcp-tokens/", json={"name": e2e.uniq("mcp-client")})
    if not t.status(r, 201):
        return
    st.mc_token, st.mc_token_id = r.jdict().get("token") or "", r.jdict().get("id") or ""
    e2e.defer("revoke MCP token", e2e.cleanup_api, u1, "DELETE", f"/mcp-tokens/{st.mc_token_id}")
    if not st.mc_token.startswith("calcard_mcp_"):
        t.fail(f"token must start with calcard_mcp_, got '{st.mc_token[:16]}…'")
    t.nonempty(r, "token_prefix")


def test_token_list(t, u1, st):
    """token: list shows the prefix, never the secret"""
    t.need(st, "mc_token_id")
    r = u1.api("GET", "/mcp-tokens/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len([x for x in j["tokens"] if x["id"] == st.mc_token_id]) == 1)
    t.not_contains(r, st.mc_token, "the secret must never be listed")


def test_token_validation(t, e2e, u1, u2, st):
    """token: validation and cross-user revoke"""
    t.status(u1.api("POST", "/mcp-tokens/", json='{"name":""}'), 400, msg="a name is required")
    t.status(u1.api("POST", "/mcp-tokens/", json='{"name":"x","expires_at":"next week"}'), 400,
             msg="expires_at must be RFC 3339")
    t.need(st, "mc_token_id")
    t.status(u2.api("DELETE", f"/mcp-tokens/{st.mc_token_id}"), 404, msg="another user cannot revoke the token")
    t.status(e2e.anon("GET", "/mcp-tokens/"), 401)


def test_unauth(t, e2e, st):
    """unauthenticated /mcp is 401 with a Bearer challenge"""
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":1,"method":"initialize"}', "", "")
    if not t.status(r, 401):
        return
    t.header(r, "WWW-Authenticate", "Bearer")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":1,"method":"initialize"}', "calcard_mcp_not-a-real-token", "")
    t.status(r, 401, msg="an unknown token")


def test_initialize(t, e2e, st):
    """initialize returns protocol version, capabilities and a session"""
    t.need(st, "mc_token")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",'
                          '"capabilities":{},"clientInfo":{"name":"calcard-e2e","version":"1"}}}', st.mc_token, "")
    if not t.status(r, 200):
        return
    st.mc_session = r.hdr("Mcp-Session-Id")
    if not st.mc_session:
        t.fail("initialize must return an Mcp-Session-Id header", r)
    t.json(r, "jsonrpc", "2.0")
    t.json(r, "id", 1)
    t.json(r, "result.protocolVersion", "2025-06-18")
    t.json(r, "result.serverInfo.name", "calcard")
    t.jq(r, lambda j: "tools" in j["result"]["capabilities"] and "resources" in j["result"]["capabilities"])


def test_initialized(t, e2e, st):
    """notifications/initialized is accepted with 202 and no body"""
    t.need(st, "mc_session")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","method":"notifications/initialized"}')
    if not t.status(r, 202):
        return
    if r.text:
        t.fail(f"a notification must get an empty body, got '{r.text[:80]}'")


def test_ping(t, e2e, st):
    """ping answers an empty result"""
    t.need(st, "mc_session")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":2,"method":"ping"}')
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: j["result"] == {} and "error" not in j)


TOOLS = ["list_calendars", "get_events", "create_event", "update_event", "delete_event", "search_events",
         "list_address_books", "get_contacts", "search_contacts", "create_contact", "update_contact",
         "delete_contact", "find_free_slots"]


def test_tools_list(t, e2e, st):
    """tools/list advertises the 13 tools with object schemas"""
    t.need(st, "mc_session")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":3,"method":"tools/list"}')
    if not t.status(r, 200):
        return
    for name in TOOLS:
        t.jq(r, lambda j, n=name: n in [x["name"] for x in j["result"]["tools"]], f"tool {name} must be advertised")
    t.jq(r, lambda j: all(x["inputSchema"]["type"] == "object" and len(x["description"]) > 0
                          for x in j["result"]["tools"]),
         "every tool needs a description and an object input schema")


def test_create_event(t, e2e, u1, st, mc):
    """create_event over MCP is visible over REST"""
    t.need(st, "mc_session")
    r, err, payload = mcp_tool(e2e, st, "create_event", {
        "calendar_id": mc.cal, "title": "MCP standup", "start": "2030-06-05T09:00:00Z",
        "end": "2030-06-05T09:30:00Z", "location": "Room 1"})
    if not t.status(r, 200):
        return
    if not t.eq(err, False, f"create_event must succeed: {payload[:200]}"):
        return
    st.mc_event_id = pj(payload, "event.id")
    if not st.mc_event_id:
        t.fail(f"create_event must return the event id: {payload}")
    r = u1.api("GET", f"/calendars/{mc.cal}/events?start=2030-06-01T00:00:00Z&end=2030-07-01T00:00:00Z")
    t.jq(r, lambda j: len([e for e in j["events"] if e["summary"] == "MCP standup" and e["location"] == "Room 1"]) == 1,
         "the event written through MCP must be visible over REST")


def test_search_events(t, e2e, st, mc):
    """search_events and get_events find it"""
    t.need(st, "mc_event_id")
    _, err, payload = mcp_tool(e2e, st, "search_events", {"query": "standup"})
    if not t.eq(err, False):
        return
    titles = pj(payload, "matches")
    try:
        found = "MCP standup" in [m["event"]["title"] for m in titles]
    except (TypeError, KeyError):
        found = False
    if not found:
        t.fail(f"search_events must find the event: {payload[:300]}")
    _, err, payload = mcp_tool(e2e, st, "get_events", {"calendar_id": mc.cal, "start": "2030-06-01T00:00:00Z",
                                                        "end": "2030-07-01T00:00:00Z"})
    if not t.eq(err, False, f"get_events: {payload[:200]}"):
        return
    if "MCP standup" not in payload:
        t.fail(f"get_events must list the event: {payload[:300]}")
    _, err, payload = mcp_tool(e2e, st, "list_calendars", {})
    if mc.cal not in payload:
        t.fail("list_calendars must include the calendar")


def test_free_slots(t, e2e, st):
    """find_free_slots skips the busy time"""
    t.need(st, "mc_event_id")
    _, err, payload = mcp_tool(e2e, st, "find_free_slots", {"start": "2030-06-05T09:00:00Z",
                                                             "end": "2030-06-05T12:00:00Z", "duration_minutes": 60})
    if not t.eq(err, False, f"find_free_slots: {payload[:200]}"):
        return
    slots = pj(payload, "free_slots")
    ok = isinstance(slots, list) and len(slots) == 1 and isinstance(slots[0], dict) \
        and slots[0].get("start") == "2030-06-05T09:30:00Z"
    if not ok:
        t.fail(f"the 09:00–09:30 event must leave exactly 09:30–12:00 free: {payload}")


def test_update_delete_event(t, e2e, u1, st, mc):
    """update_event / delete_event round trip"""
    t.need(st, "mc_event_id")
    _, err, payload = mcp_tool(e2e, st, "update_event", {"event_id": st.mc_event_id, "title": "MCP standup (moved)"})
    if not t.eq(err, False, f"update_event: {payload[:200]}"):
        return
    r = u1.api("GET", f"/calendars/{mc.cal}/events/{st.mc_event_id}")
    t.json(r, "summary", "MCP standup (moved)", "REST must see the MCP update")
    _, err, payload = mcp_tool(e2e, st, "delete_event", {"event_id": st.mc_event_id})
    if not t.eq(err, False, f"delete_event: {payload[:200]}"):
        return
    r = u1.api("GET", f"/calendars/{mc.cal}/events/{st.mc_event_id}")
    t.status(r, 404, msg="the event deleted over MCP is gone")


def test_contacts(t, e2e, u1, st, mc):
    """contact tools: create, search, update, delete"""
    t.need(st, "mc_session")
    _, err, payload = mcp_tool(e2e, st, "create_contact", {"address_book_id": mc.ab, "first_name": "Jane",
                                                            "last_name": "Mcpsmith", "email": "jane.mcp@example.test"})
    if not t.eq(err, False, f"create_contact: {payload[:200]}"):
        return
    cid = pj(payload, "contact.id")
    r = u1.api("GET", f"/addressbooks/{mc.ab}/contacts/{cid}")
    if not t.status(r, 200, msg="the contact created over MCP exists in REST"):
        return
    _, err, payload = mcp_tool(e2e, st, "search_contacts", {"query": "mcpsmith"})
    matches = pj(payload, "matches")
    if not (isinstance(matches, list) and len(matches) >= 1):
        t.fail(f"search_contacts must find it: {payload[:200]}")
    _, err, payload = mcp_tool(e2e, st, "update_contact", {"contact_id": cid, "last_name": "Mcpjones"})
    t.eq(err, False, f"update_contact: {payload[:200]}")
    _, err, payload = mcp_tool(e2e, st, "get_contacts", {"address_book_id": mc.ab})
    if "Mcpjones" not in payload:
        t.fail(f"get_contacts must show the update: {payload[:200]}")
    _, err, payload = mcp_tool(e2e, st, "delete_contact", {"contact_id": cid})
    t.eq(err, False, f"delete_contact: {payload[:200]}")
    r = u1.api("GET", f"/addressbooks/{mc.ab}/contacts/{cid}")
    t.status(r, 404, msg="the contact deleted over MCP is gone")


def test_resources(t, e2e, st, mc):
    """resources/list and resources/read"""
    t.need(st, "mc_session")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":10,"method":"resources/list"}')
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: {"calendars://list", "contacts://list"} <= {x["uri"] for x in j["result"]["resources"]})
    t.jq(r, lambda j: f"calendars://{mc.cal}/events" in [x["uri"] for x in j["result"]["resources"]])
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":11,"method":"resources/read","params":{"uri":"calendars://list"}}')
    if not t.status(r, 200):
        return
    t.json(r, "result.contents[0].mimeType", "application/json")
    name = e2e.uniq("mcp-cal")
    t.jq(r, lambda j: name in j["result"]["contents"][0]["text"])
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":12,"method":"resources/read","params":'
                          '{"uri":"calendars://00000000-0000-0000-0000-000000000000/events"}}')
    t.jq(r, lambda j: "error" in j, "reading an unknown resource is a JSON-RPC error")


def test_errors(t, e2e, st, mc):
    """protocol errors: parse error, unknown method, unknown tool"""
    t.need(st, "mc_session")
    r = e2e.req("POST", f"{e2e.base}/mcp", bearer=st.mc_token, headers={"Mcp-Session-Id": st.mc_session},
                data='{"jsonrpc":', ctype="application/json")
    if not t.status(r, 400):
        return
    t.json(r, "error.code", -32700, "parse error")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":20,"method":"no/such/method"}')
    t.json(r, "error.code", -32601, "method not found")
    r, _, _ = mcp_tool(e2e, st, "no_such_tool", {}, 21)
    t.jq(r, lambda j: "error" in j or jpath(j, "result.isError") is True,
         "an unknown tool must be reported as an error")
    r, _, _ = mcp_tool(e2e, st, "create_event", {"calendar_id": "x"}, 22)
    t.jq(r, lambda j: "error" in j or jpath(j, "result.isError") is True,
         "invalid tool arguments must be reported as an error")


def test_batch(t, e2e, st):
    """JSON-RPC batch requests are answered as a batch"""
    t.need(st, "mc_session")
    r = mcp_call(e2e, st, '[{"jsonrpc":"2.0","id":31,"method":"ping"},{"jsonrpc":"2.0","method":'
                          '"notifications/initialized"},{"jsonrpc":"2.0","id":32,"method":"tools/list"}]')
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: isinstance(j, list) and len(j) == 2 and sorted(x["id"] for x in j) == [31, 32],
         "two responses (the notification gets none), matched by id")


def test_isolation(t, e2e, u1, u2, st, mc):
    """another user's calendar is unreachable through MCP"""
    t.need(st, "mc_token")
    r = u2.api("POST", "/mcp-tokens/", json={"name": e2e.uniq("outsider")})
    if not t.status(r, 201):
        return
    tok, tid = r.jdict().get("token") or "", r.jdict().get("id") or ""
    e2e.defer("revoke U2 MCP token", e2e.cleanup_api, u2, "DELETE", f"/mcp-tokens/{tid}")
    st.mc_u2_token = tok
    # An event that exists while U2 looks (the MCP standup is deleted by now),
    # plus a positive control: U1's own token does see it.
    probe = f"MCP isolation probe {e2e.run_id}"
    u1.mk_event(mc.cal, probe, "2030-06-07T09:00:00Z", "2030-06-07T10:00:00Z")
    get_events = {"jsonrpc": "2.0", "id": 40, "method": "tools/call",
                  "params": {"name": "get_events", "arguments": {"calendar_id": mc.cal, "start": "2030-06-01T00:00:00Z",
                                                                 "end": "2030-07-01T00:00:00Z"}}}
    r = mcp_call(e2e, st, get_events, st.mc_token, "")
    if not t.contains(r, probe, "control: U1's own get_events lists the probe event"):
        return
    r = mcp_call(e2e, st, get_events, tok, "")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: jpath(j, "result.isError") is True or "error" in j, "U2 reading U1's calendar must be refused")
    t.not_contains(r, probe, "U2 must not see U1's event")
    r = mcp_call(e2e, st, {"jsonrpc": "2.0", "id": 41, "method": "tools/call", "params": {
        "name": "create_event", "arguments": {"calendar_id": mc.cal, "title": "planted",
                                              "start": "2030-06-06T09:00:00Z", "end": "2030-06-06T10:00:00Z"}}},
                 tok, "")
    t.jq(r, lambda j: jpath(j, "result.isError") is True or "error" in j, "U2 writing into U1's calendar must be refused")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":42,"method":"resources/read","params":{"uri":"calendars://list"}}',
                 tok, "")
    t.status(r, 200, msg="U2 reads its own calendar list")
    t.not_contains(r, e2e.uniq("mcp-cal"), "U2's calendar list must not include U1's calendar")
    r = u1.api("GET", f"/calendars/{mc.cal}/events?start=2030-06-01T00:00:00Z&end=2030-07-01T00:00:00Z")
    t.jq(r, lambda j: len([e for e in j["events"] or [] if e["summary"] == "planted"]) == 0, "nothing may have been planted")


def test_session_binding(t, e2e, st):
    """a session id is bound to its user"""
    t.need(st, "mc_session", "mc_u2_token")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":50,"method":"ping"}', st.mc_u2_token, st.mc_session)
    t.status(r, 404, 403, 401, msg="U1's session id presented with U2's token")


def test_manifest(t, e2e, st):
    """GET /mcp returns the manifest"""
    t.need(st, "mc_token")
    r = e2e.req("GET", f"{e2e.base}/mcp", bearer=st.mc_token)
    if not t.status(r, 200):
        return
    t.json(r, "name", "calcard")
    t.json(r, "transport", "streamable-http")
    t.jq(r, lambda j: len(j["tools"]) == 13)


def test_session_end(t, e2e, st):
    """DELETE /mcp ends the session; unknown session is 404"""
    t.need(st, "mc_session")
    r = e2e.req("DELETE", f"{e2e.base}/mcp", bearer=st.mc_token, headers={"Mcp-Session-Id": st.mc_session})
    if not t.status(r, 204, 200):
        return
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":60,"method":"ping"}')
    t.status(r, 404, msg="a terminated session must 404 (the client's cue to re-initialize)")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":61,"method":"ping"}', st.mc_token, f"not-a-real-session-{e2e.run_id}")
    t.status(r, 404, msg="an unknown session id")
    st.mc_session = ""


def test_jwt(t, e2e, u1, st):
    """a JWT access token is accepted as bearer"""
    u1.ensure_token()
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":70,"method":"initialize","params":{"protocolVersion":"2025-06-18",'
                          '"capabilities":{},"clientInfo":{"name":"e2e-jwt","version":"1"}}}', u1.token, "")
    if not t.status(r, 200):
        return
    t.json(r, "result.serverInfo.name", "calcard")


def test_revoke(t, e2e, u1, st):
    """a revoked token is rejected on the next request"""
    r = u1.api("POST", "/mcp-tokens/", json={"name": e2e.uniq("short-lived")})
    if not t.status(r, 201):
        return
    tok, tid = r.jdict().get("token") or "", r.jdict().get("id") or ""
    e2e.defer("revoke MCP token", e2e.cleanup_api, u1, "DELETE", f"/mcp-tokens/{tid}")
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":80,"method":"ping"}', tok, "")
    if not t.status(r, 200, msg="the token works before revocation"):
        return
    if not t.status(u1.api("DELETE", f"/mcp-tokens/{tid}"), 204):
        return
    r = mcp_call(e2e, st, '{"jsonrpc":"2.0","id":81,"method":"ping"}', tok, "")
    t.status(r, 401, msg="revocation must take effect on the very next request")
