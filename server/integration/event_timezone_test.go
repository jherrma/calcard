//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F20: an event created or updated with a `timezone` (REST or MCP) is stored
// with DTSTART;TZID=<zone>, so the object must carry the VTIMEZONE that TZID
// names (RFC 5545 §3.2.19) — that is what DAV clients receive.
func TestEventTimezoneCarriesVTimezone(t *testing.T) {
	email := "event-tz@example.test"
	password := "eventTzSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Event TZ")
	_, appPassword := createAppPassword(t, token, "event-tz")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok, "default Personal calendar should exist")
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	var ev struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+personal.UUID+"/events", token, map[string]any{
		"summary":    "Berlin weekly",
		"start":      "2030-06-01T08:00:00Z",
		"end":        "2030-06-01T09:00:00Z",
		"timezone":   "Europe/Berlin",
		"recurrence": map[string]any{"frequency": "weekly", "interval": 1},
	}, &ev)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, ev.ID)

	davGet := func() string {
		status, _, body := davCall(t, "GET", collection+ev.ID+".ics", email, appPassword, "", nil)
		require.Equal(t, http.StatusOK, status, string(body))
		return string(body)
	}

	t.Run("create", func(t *testing.T) {
		body := davGet()
		assert.Contains(t, body, "DTSTART;TZID=Europe/Berlin:20300601T100000")
		assert.Contains(t, body, "BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\n", "a VTIMEZONE must be stored")
		assert.Contains(t, body, "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU")
	})

	t.Run("update to another zone swaps the VTIMEZONE", func(t *testing.T) {
		status, raw := restCall(t, http.MethodPatch, "/calendars/"+personal.UUID+"/events/"+ev.ID, token,
			map[string]any{"timezone": "America/New_York"})
		require.Equal(t, http.StatusOK, status, string(raw))

		body := davGet()
		assert.Contains(t, body, "DTSTART;TZID=America/New_York:20300601T040000")
		assert.Contains(t, body, "TZID:America/New_York\r\n")
		assert.NotContains(t, body, "Europe/Berlin", "the unused Berlin VTIMEZONE must be dropped")
		assert.Equal(t, 1, strings.Count(body, "BEGIN:VTIMEZONE"))
	})

	t.Run("MCP create_event", func(t *testing.T) {
		mcp := mcpToken(t, token, "event-tz")
		status, headers, _ := mcpCall(t, mcp, "",
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"integration","version":"1"}}}`)
		require.Equal(t, http.StatusOK, status)
		session := headers.Get("Mcp-Session-Id")

		call := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_event","arguments":{`+
			`"calendar_id":%q,"title":"Tokyo call","start":"2030-07-01T01:00:00Z","end":"2030-07-01T02:00:00Z",`+
			`"timezone":"Asia/Tokyo"}}}`, personal.UUID)
		status, _, resp := mcpCall(t, mcp, session, call)
		require.Equal(t, http.StatusOK, status)
		payload, isError := toolResultText(t, resp)
		require.False(t, isError, "create_event failed: %v", payload)
		id := payload["event"].(map[string]any)["id"].(string)

		st, _, body := davCall(t, "GET", collection+id+".ics", email, appPassword, "", nil)
		require.Equal(t, http.StatusOK, st)
		assert.Contains(t, string(body), "DTSTART;TZID=Asia/Tokyo:20300701T100000")
		assert.Contains(t, string(body), "TZID:Asia/Tokyo\r\n")
	})
}
