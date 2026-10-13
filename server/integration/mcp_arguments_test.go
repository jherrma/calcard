//go:build integration

package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPRejectsUnknownArguments drives the strict argument decoding over the
// real socket: a tools/call with a misspelt or invented argument is a tool
// error that names it and the accepted arguments, and — checked through REST —
// nothing was changed. Before, the unknown key was dropped and the tool ran
// with its default: an update that changed nothing reported success, a delete
// with a typo'd option deleted anyway.
func TestMCPRejectsUnknownArguments(t *testing.T) {
	jwt, _ := registerAndLoginFull(t, "mcp-arguments@example.com", "MCPpassword123!", "MCP Arguments")
	token := mcpToken(t, jwt, "argument client")
	_, calUUID := createCalendar(t, jwt, "MCP Arguments calendar", "#3b82f6")
	_, abUUID := createAddressBook(t, jwt, "MCP Arguments book")
	contactID := createContactIn(t, jwt, abUUID, "Jane Original")

	var event struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/events", jwt, map[string]any{
		"summary": "Original", "start": "2030-06-05T09:00:00Z", "end": "2030-06-05T10:00:00Z",
	}, &event))
	eventPath := "/calendars/" + calUUID + "/events/" + event.ID

	restSummary := func() string {
		var out struct {
			Summary string `json:"summary"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, eventPath, jwt, nil, &out))
		return out.Summary
	}

	t.Run("UpdateWithAMisspeltFieldChangesNothing", func(t *testing.T) {
		payload, isErr := mcpTool(t, token, "update_event", map[string]any{"event_id": event.ID, "titel": "Changed"})
		require.True(t, isErr, "a misspelt argument must be refused, got %v", payload)
		assert.Equal(t, `Unknown argument "titel"; the tool did not run. Accepted arguments: event_id, title, `+
			`description, location, start, end, all_day, timezone, recurrence_id, scope.`, payload["message"])
		assert.Equal(t, "Original", restSummary(), "REST must still see the original title")
	})

	t.Run("DeleteWithAMisspeltOptionDeletesNothing", func(t *testing.T) {
		_, isErr := mcpTool(t, token, "delete_event", map[string]any{"event_id": event.ID, "scoep": "all"})
		require.True(t, isErr)
		status, _ := restCall(t, http.MethodGet, eventPath, jwt, nil)
		assert.Equal(t, http.StatusOK, status, "the event must survive a refused delete")
	})

	t.Run("CollectionAndContactUpdatesWithATypoChangeNothing", func(t *testing.T) {
		payload, isErr := mcpTool(t, token, "update_calendar", map[string]any{"calendar_id": calUUID, "nmae": "Hijacked"})
		require.True(t, isErr)
		assert.Contains(t, payload["message"], `Unknown argument "nmae"`)
		var cal struct {
			Name string `json:"name"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID, jwt, nil, &cal))
		assert.Equal(t, "MCP Arguments calendar", cal.Name)

		payload, isErr = mcpTool(t, token, "update_contact", map[string]any{"contact_id": contactID, "lastname": "Doe"})
		require.True(t, isErr)
		assert.Contains(t, payload["message"], `Unknown argument "lastname"`)
		assert.Contains(t, payload["message"], "last_name", "the answer lists the argument that was meant")
		var contact struct {
			FormattedName string `json:"formatted_name"`
			FamilyName    string `json:"family_name"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/"+abUUID+"/contacts/"+contactID, jwt, nil, &contact))
		assert.Equal(t, "Jane Original", contact.FormattedName)
		assert.Empty(t, contact.FamilyName)
	})

	t.Run("NamesAreCaseSensitive", func(t *testing.T) {
		payload, isErr := mcpTool(t, token, "get_events", map[string]any{"Calendar_ID": calUUID})
		require.True(t, isErr)
		assert.Equal(t, `Unknown argument "Calendar_ID"; the tool did not run. Accepted arguments: calendar_id, start, end.`,
			payload["message"])
	})

	t.Run("WithASessionAndInABatch", func(t *testing.T) {
		status, headers, _ := mcpCall(t, token, "",
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"integration","version":"1"}}}`)
		require.Equal(t, http.StatusOK, status)
		session := headers.Get("Mcp-Session-Id")
		require.NotEmpty(t, session)

		status, _, body := mcpCall(t, token, session,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_event","arguments":{"event_id":"`+event.ID+`","force":true}}}`)
		require.Equal(t, http.StatusOK, status)
		payload, isErr := toolResultText(t, body)
		require.True(t, isErr)
		assert.Contains(t, payload["message"], `Unknown argument "force"`)

		responses := mcpBatch(t, token, session, `[
			{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"update_event","arguments":{"event_id":"`+event.ID+`","titel":"Batch"}}},
			{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"list_calendars","arguments":{}}}
		]`)
		require.Len(t, responses, 2)
		byID := map[float64]map[string]any{}
		for _, r := range responses {
			byID[r["id"].(float64)] = r
		}
		refused, isErr := toolResultText(t, byID[10])
		require.True(t, isErr, "the typo'd call in a batch is refused")
		assert.Contains(t, refused["message"], `Unknown argument "titel"`)
		_, isErr = toolResultText(t, byID[11])
		assert.False(t, isErr, "and the rest of the batch runs normally")

		assert.Equal(t, "Original", restSummary())
		status, _ = restCall(t, http.MethodGet, eventPath, jwt, nil)
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("MetaNextToArgumentsIsAccepted", func(t *testing.T) {
		// Strictness is for the tool's arguments only: MCP puts request
		// metadata in params, next to them.
		status, _, body := mcpCall(t, token, "", `{"jsonrpc":"2.0","id":20,"method":"tools/call","params":`+
			`{"name":"list_calendars","arguments":{},"_meta":{"progressToken":"p-1"}}}`)
		require.Equal(t, http.StatusOK, status)
		payload, isErr := toolResultText(t, body)
		require.False(t, isErr, "%v", payload)
		assert.Contains(t, mcpListIDs(payload, "calendars"), calUUID)
	})

	t.Run("TheCorrectlySpeltCallsStillWork", func(t *testing.T) {
		_, isErr := mcpTool(t, token, "update_event", map[string]any{"event_id": event.ID, "title": "Changed"})
		require.False(t, isErr)
		assert.Equal(t, "Changed", restSummary())

		_, isErr = mcpTool(t, token, "delete_event", map[string]any{"event_id": event.ID, "scope": "all"})
		require.False(t, isErr)
		status, _ := restCall(t, http.MethodGet, eventPath, jwt, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})
}

// mcpBatch posts a JSON-RPC batch to /mcp and returns the decoded responses.
func mcpBatch(t *testing.T, credential, sessionID, body string) []map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+credential)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded), "batch response: %s", raw)
	return decoded
}
