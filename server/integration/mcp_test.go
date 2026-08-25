//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpCall posts one JSON-RPC message to /mcp with the given bearer credential
// and returns the status, response headers and decoded body.
func mcpCall(t *testing.T, credential, sessionID, body string) (int, http.Header, map[string]any) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var decoded map[string]any
	raw := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	return resp.StatusCode, resp.Header, decoded
}

// mcpToken mints an MCP access token over the REST API for the given user.
func mcpToken(t *testing.T, jwt, name string) string {
	t.Helper()
	var out struct {
		ID          string `json:"id"`
		Token       string `json:"token"`
		TokenPrefix string `json:"token_prefix"`
	}
	code := doJSONRaw(t, http.MethodPost, "/mcp-tokens/", jwt, map[string]string{"name": name}, &out)
	require.Equal(t, http.StatusCreated, code, "create mcp token")
	require.NotEmpty(t, out.Token)
	return out.Token
}

// toolResultText pulls the single text block out of a tools/call result.
//
// A successful tool answers with JSON, so its payload is decoded. A tool ERROR
// answers with a sentence meant for the model to read, so it is returned as
// text — decoding it would fail, which is exactly what a first version of this
// helper did.
func toolResultText(t *testing.T, body map[string]any) (map[string]any, bool) {
	t.Helper()
	result, ok := body["result"].(map[string]any)
	require.True(t, ok, "no result in %v", body)

	content, ok := result["content"].([]any)
	require.True(t, ok, "no content in %v", result)
	require.NotEmpty(t, content)

	text := content[0].(map[string]any)["text"].(string)
	if isError, _ := result["isError"].(bool); isError {
		return map[string]any{"message": text}, true
	}

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &payload), "tool text was not JSON: %s", text)
	return payload, false
}

// TestMCPServer drives the whole story-104 surface over the real socket: mint a
// token through REST, complete the MCP handshake, discover tools and resources,
// call tools that read and write, and confirm the endpoint refuses everything
// it should.
func TestMCPServer(t *testing.T) {
	jwt, _ := registerAndLoginFull(t, "mcp-user@example.com", "MCPpassword123!", "MCP User")
	_, calUUID := createCalendar(t, jwt, "MCP Work", "#3b82f6")
	_, abUUID := createAddressBook(t, jwt, "MCP Contacts")

	token := mcpToken(t, jwt, "integration client")

	var sessionID string

	t.Run("UnauthenticatedIsRejected", func(t *testing.T) {
		status, headers, _ := mcpCall(t, "", "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
		assert.Equal(t, http.StatusUnauthorized, status)
		assert.Contains(t, headers.Get("WWW-Authenticate"), "Bearer",
			"clients rely on this header to discover the auth scheme")
	})

	t.Run("Initialize", func(t *testing.T) {
		status, headers, body := mcpCall(t, token, "",
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"integration","version":"1"}}}`)
		require.Equal(t, http.StatusOK, status)

		sessionID = headers.Get("Mcp-Session-Id")
		require.NotEmpty(t, sessionID, "initialize must hand back a session id")

		result := body["result"].(map[string]any)
		assert.Equal(t, "2025-06-18", result["protocolVersion"])
		assert.Equal(t, "calcard", result["serverInfo"].(map[string]any)["name"])
		caps := result["capabilities"].(map[string]any)
		assert.Contains(t, caps, "tools")
		assert.Contains(t, caps, "resources")
	})

	t.Run("InitializedNotificationIsAccepted", func(t *testing.T) {
		status, _, _ := mcpCall(t, token, sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		assert.Equal(t, http.StatusAccepted, status, "a notification gets no body")
	})

	t.Run("Ping", func(t *testing.T) {
		status, _, body := mcpCall(t, token, sessionID, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
		require.Equal(t, http.StatusOK, status)
		assert.NotNil(t, body["result"])
		assert.Nil(t, body["error"])
	})

	t.Run("ToolsList", func(t *testing.T) {
		status, _, body := mcpCall(t, token, sessionID, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
		require.Equal(t, http.StatusOK, status)

		tools := body["result"].(map[string]any)["tools"].([]any)
		names := map[string]bool{}
		for _, raw := range tools {
			tool := raw.(map[string]any)
			names[tool["name"].(string)] = true
			assert.NotEmpty(t, tool["description"], "%v needs a description", tool["name"])
			assert.Equal(t, "object", tool["inputSchema"].(map[string]any)["type"])
		}
		for _, want := range []string{
			"list_calendars", "get_events", "create_event", "update_event", "delete_event",
			"search_events", "list_address_books", "get_contacts", "search_contacts",
			"create_contact", "update_contact", "delete_contact", "find_free_slots",
		} {
			assert.True(t, names[want], "tool %s must be advertised", want)
		}
	})

	var eventID string

	t.Run("CreateAndReadEvent", func(t *testing.T) {
		call := fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_event","arguments":{`+
			`"calendar_id":%q,"title":"Team Standup","start":"2030-06-05T09:00:00Z","end":"2030-06-05T09:30:00Z",`+
			`"location":"Room 1"}}}`, calUUID)
		status, _, body := mcpCall(t, token, sessionID, call)
		require.Equal(t, http.StatusOK, status)

		payload, isError := toolResultText(t, body)
		require.False(t, isError, "create_event failed: %v", payload)
		event := payload["event"].(map[string]any)
		eventID = event["id"].(string)
		require.NotEmpty(t, eventID)
		assert.Equal(t, calUUID, event["calendar_id"])

		// The write must be visible through REST — the two surfaces share one
		// use case, and this is the assertion that proves it.
		var rest struct {
			Events []struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
			} `json:"events"`
		}
		code := doJSONRaw(t, http.MethodGet,
			"/calendars/"+calUUID+"/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z",
			jwt, nil, &rest)
		require.Equal(t, http.StatusOK, code)
		found := false
		for _, e := range rest.Events {
			if e.Summary == "Team Standup" {
				found = true
			}
		}
		assert.True(t, found, "an event created over MCP must be visible over REST")
	})

	t.Run("SearchEvents", func(t *testing.T) {
		status, _, body := mcpCall(t, token, sessionID,
			`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"search_events","arguments":{"query":"standup"}}}`)
		require.Equal(t, http.StatusOK, status)

		payload, isError := toolResultText(t, body)
		require.False(t, isError, "search_events failed: %v", payload)
		matches := payload["matches"].([]any)
		require.NotEmpty(t, matches, "the event created above must be findable")
		assert.Equal(t, "Team Standup",
			matches[0].(map[string]any)["event"].(map[string]any)["title"])
	})

	t.Run("FindFreeSlots", func(t *testing.T) {
		status, _, body := mcpCall(t, token, sessionID,
			`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"find_free_slots","arguments":{`+
				`"start":"2030-06-05T09:00:00Z","end":"2030-06-05T12:00:00Z","duration_minutes":60}}}`)
		require.Equal(t, http.StatusOK, status)

		payload, isError := toolResultText(t, body)
		require.False(t, isError, "find_free_slots failed: %v", payload)
		slots := payload["free_slots"].([]any)
		require.Len(t, slots, 1, "the standup blocks 09:00–09:30, leaving 09:30–12:00")
		assert.Equal(t, "2030-06-05T09:30:00Z", slots[0].(map[string]any)["start"])
	})

	t.Run("ContactRoundTrip", func(t *testing.T) {
		call := fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create_contact","arguments":{`+
			`"address_book_id":%q,"first_name":"Jane","last_name":"Smith","email":"jane@example.com"}}}`, abUUID)
		status, _, body := mcpCall(t, token, sessionID, call)
		require.Equal(t, http.StatusOK, status)

		payload, isError := toolResultText(t, body)
		require.False(t, isError, "create_contact failed: %v", payload)
		contactID := payload["contact"].(map[string]any)["id"].(string)
		require.NotEmpty(t, contactID)

		status, _, body = mcpCall(t, token, sessionID,
			`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"search_contacts","arguments":{"query":"smith"}}}`)
		require.Equal(t, http.StatusOK, status)
		payload, isError = toolResultText(t, body)
		require.False(t, isError)
		require.NotEmpty(t, payload["matches"])

		call = fmt.Sprintf(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"delete_contact","arguments":{"contact_id":%q}}}`, contactID)
		status, _, body = mcpCall(t, token, sessionID, call)
		require.Equal(t, http.StatusOK, status)
		payload, isError = toolResultText(t, body)
		require.False(t, isError, "delete_contact failed: %v", payload)
	})

	t.Run("ResourcesListAndRead", func(t *testing.T) {
		status, _, body := mcpCall(t, token, sessionID, `{"jsonrpc":"2.0","id":10,"method":"resources/list"}`)
		require.Equal(t, http.StatusOK, status)

		resources := body["result"].(map[string]any)["resources"].([]any)
		uris := map[string]bool{}
		for _, raw := range resources {
			uris[raw.(map[string]any)["uri"].(string)] = true
		}
		assert.True(t, uris["calendars://list"])
		assert.True(t, uris["contacts://list"])
		assert.True(t, uris["calendars://"+calUUID+"/events"])

		read := fmt.Sprintf(`{"jsonrpc":"2.0","id":11,"method":"resources/read","params":{"uri":%q}}`, "calendars://list")
		status, _, body = mcpCall(t, token, sessionID, read)
		require.Equal(t, http.StatusOK, status)
		contents := body["result"].(map[string]any)["contents"].([]any)
		require.Len(t, contents, 1)
		assert.Equal(t, "application/json", contents[0].(map[string]any)["mimeType"])
		assert.Contains(t, contents[0].(map[string]any)["text"], "MCP Work")
	})

	t.Run("AnotherUsersDataIsUnreachable", func(t *testing.T) {
		otherJWT, _ := registerAndLoginFull(t, "mcp-outsider@example.com", "MCPpassword123!", "Outsider")
		otherToken := mcpToken(t, otherJWT, "outsider client")

		call := fmt.Sprintf(`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"get_events","arguments":{"calendar_id":%q}}}`, calUUID)
		status, _, body := mcpCall(t, otherToken, "", call)
		require.Equal(t, http.StatusOK, status)

		payload, isError := toolResultText(t, body)
		require.True(t, isError, "another user's calendar must be refused: %v", payload)
		assert.Contains(t, payload["message"], "readable by you")
	})

	t.Run("UnknownSessionIs404", func(t *testing.T) {
		status, _, _ := mcpCall(t, token, "not-a-real-session", `{"jsonrpc":"2.0","id":13,"method":"ping"}`)
		assert.Equal(t, http.StatusNotFound, status,
			"404 is the spec's signal to re-initialize")
	})

	t.Run("Manifest", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/mcp", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var manifest map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&manifest))
		// Asserting on content, not just the status: the SPA catch-all would
		// otherwise answer 200 with index.html if route ordering regressed.
		assert.Equal(t, "calcard", manifest["name"])
		assert.Equal(t, "streamable-http", manifest["transport"])
		assert.Len(t, manifest["tools"], 13)
	})

	t.Run("DeleteTerminatesTheSession", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/mcp", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Mcp-Session-Id", sessionID)
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusNoContent, resp.StatusCode)

		status, _, _ := mcpCall(t, token, sessionID, `{"jsonrpc":"2.0","id":14,"method":"ping"}`)
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("RevokedTokenStopsWorking", func(t *testing.T) {
		revocableJWT, _ := registerAndLoginFull(t, "mcp-revoke@example.com", "MCPpassword123!", "Revoke Me")

		var created struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}
		code := doJSONRaw(t, http.MethodPost, "/mcp-tokens/", revocableJWT,
			map[string]string{"name": "short lived"}, &created)
		require.Equal(t, http.StatusCreated, code)

		status, _, _ := mcpCall(t, created.Token, "", `{"jsonrpc":"2.0","id":15,"method":"initialize"}`)
		require.Equal(t, http.StatusOK, status, "the token must work before revocation")

		code, _ = restCall(t, http.MethodDelete, "/mcp-tokens/"+created.ID, revocableJWT, nil)
		require.Equal(t, http.StatusNoContent, code)

		status, _, _ = mcpCall(t, created.Token, "", `{"jsonrpc":"2.0","id":16,"method":"initialize"}`)
		assert.Equal(t, http.StatusUnauthorized, status,
			"revocation must take effect on the very next request")
	})

	t.Run("DeleteEventCleansUp", func(t *testing.T) {
		call := fmt.Sprintf(`{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"delete_event","arguments":{"event_id":%q}}}`, eventID)
		status, _, body := mcpCall(t, token, "", call)
		require.Equal(t, http.StatusOK, status)
		payload, isError := toolResultText(t, body)
		require.False(t, isError, "delete_event failed: %v", payload)
	})
}
