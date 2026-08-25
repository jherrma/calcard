package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitializeNegotiatesProtocolVersion(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	for _, tc := range []struct{ requested, expected string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"2024-11-05", "2024-11-05"},
		// An unknown version is answered with ours rather than refused: a client
		// released after this code was written must still be able to connect.
		{"2099-01-01", PreferredProtocolVersion},
		{"", PreferredProtocolVersion},
	} {
		resp, sessionID := e.server.Handle(e.ctx(), alice.ID, &Request{
			JSONRPC: jsonRPCVersion,
			ID:      json.RawMessage(`1`),
			Method:  MethodInitialize,
			Params:  json.RawMessage(`{"protocolVersion":"` + tc.requested + `","clientInfo":{"name":"test","version":"1"}}`),
		})
		require.NotNil(t, resp)
		require.Nil(t, resp.Error)
		require.NotEmpty(t, sessionID, "initialize must open a session")

		result := resp.Result.(initializeResult)
		assert.Equal(t, tc.expected, result.ProtocolVersion, "requested %q", tc.requested)
		assert.Equal(t, ServerName, result.ServerInfo.Name)
		require.NotNil(t, result.Capabilities.Tools)
		require.NotNil(t, result.Capabilities.Resources)
	}
}

func TestSessionIsScopedToItsUser(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	_, sessionID := e.server.Handle(e.ctx(), alice.ID, &Request{
		JSONRPC: jsonRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  MethodInitialize,
	})
	require.NotEmpty(t, sessionID)

	assert.NotNil(t, e.server.Sessions().Get(sessionID, alice.ID))
	// A leaked session id must not let anyone else act as its owner.
	assert.Nil(t, e.server.Sessions().Get(sessionID, bob.ID))
	assert.False(t, e.server.Sessions().Delete(sessionID, bob.ID))
	assert.True(t, e.server.Sessions().Delete(sessionID, alice.ID))
	assert.Nil(t, e.server.Sessions().Get(sessionID, alice.ID))
}

func TestNotificationsGetNoResponse(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	for _, method := range []string{MethodInitialized, "notifications/something-unknown"} {
		resp, _ := e.server.Handle(e.ctx(), alice.ID, &Request{
			JSONRPC: jsonRPCVersion,
			Method:  method,
		})
		assert.Nil(t, resp, "%s is a notification and must not be answered", method)
	}
}

func TestPingReturnsEmptyObject(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	resp := e.rpc(alice.ID, MethodPing, nil)
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	// Some clients treat a null result as malformed, so the shape matters.
	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"result":{}`)
}

func TestToolsListAdvertisesEveryToolWithASchema(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	resp := e.rpc(alice.ID, MethodToolsList, nil)
	require.Nil(t, resp.Error)
	tools := resp.Result.(toolsListResult).Tools

	names := map[string]bool{}
	for _, tool := range tools {
		assert.NotEmpty(t, tool.Description, "%s needs a description; it is what the model reads", tool.Name)

		var schema map[string]interface{}
		require.NoError(t, json.Unmarshal(tool.InputSchema, &schema),
			"%s advertises an unparseable input schema", tool.Name)
		assert.Equal(t, "object", schema["type"], "%s: MCP input schemas must be objects", tool.Name)
		names[tool.Name] = true
	}

	// The surface story 104 specifies, asserted by name so a rename or a
	// dropped registration fails loudly rather than silently shrinking what an
	// assistant can do.
	for _, want := range []string{
		"list_calendars", "get_events", "create_event", "update_event", "delete_event",
		"search_events", "list_address_books", "get_contacts", "search_contacts",
		"create_contact", "update_contact", "delete_contact", "find_free_slots",
	} {
		assert.True(t, names[want], "tool %s is missing", want)
	}
	assert.Len(t, tools, 13)
}

func TestToolsListIsSortedForStability(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	tools := e.rpc(alice.ID, MethodToolsList, nil).Result.(toolsListResult).Tools
	for i := 1; i < len(tools); i++ {
		assert.Less(t, tools[i-1].Name, tools[i].Name, "tools/list must be stably ordered")
	}
}

func TestUnknownMethodAndToolAreProtocolErrors(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	resp := e.rpc(alice.ID, "does/not/exist", nil)
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeMethodNotFound, resp.Error.Code)

	resp = e.rpc(alice.ID, MethodToolsCall, map[string]interface{}{"name": "no_such_tool"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeMethodNotFound, resp.Error.Code)
}

func TestBadJSONRPCVersionIsRejected(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	resp, _ := e.server.Handle(e.ctx(), alice.ID, &Request{
		JSONRPC: "1.0",
		ID:      json.RawMessage(`7`),
		Method:  MethodPing,
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeInvalidRequest, resp.Error.Code)
	assert.Equal(t, json.RawMessage(`7`), resp.ID, "the id must be echoed back verbatim")
}

func TestToolCallWithoutArgumentsWorksForZeroArgTools(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	e.newCalendar(alice.ID, "Work")

	// `arguments` omitted entirely — legal for a tool whose schema requires
	// nothing, and a shape real clients send.
	resp := e.rpc(alice.ID, MethodToolsCall, map[string]interface{}{"name": "list_calendars"})
	require.Nil(t, resp.Error)
	result := resp.Result.(*toolCallResult)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Work")
}

func TestToolErrorsAreResultsNotProtocolErrors(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	// An unreadable calendar is something the model should read and react to,
	// so it must arrive as a result with isError — not as a JSON-RPC error,
	// which most clients surface as a transport failure instead.
	resp := e.rpc(alice.ID, MethodToolsCall, map[string]interface{}{
		"name":      "get_events",
		"arguments": map[string]interface{}{"calendar_id": "00000000-0000-0000-0000-000000000000"},
	})
	require.Nil(t, resp.Error, "a refused tool must not be a protocol error")
	result := resp.Result.(*toolCallResult)
	assert.True(t, result.IsError)
}

func TestMalformedToolArgumentsAreProtocolErrors(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	resp, _ := e.server.Handle(e.ctx(), alice.ID, &Request{
		JSONRPC: jsonRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  MethodToolsCall,
		// limit is declared as an integer; a string violates the client's own
		// advertised schema, which the model cannot act on.
		Params: json.RawMessage(`{"name":"get_contacts","arguments":{"address_book_id":"x","limit":"lots"}}`),
	})
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeInvalidParams, resp.Error.Code)
}
