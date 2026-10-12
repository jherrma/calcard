//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportedTimes returns the DTSTART/DTEND lines of the event with the given
// SUMMARY in the calendar's exported feed.
func exportedTimes(t *testing.T, token, calUUID, summary string) []string {
	t.Helper()
	status, raw := restCall(t, http.MethodGet, "/calendars/"+calUUID+"/export", token, nil)
	require.Equalf(t, http.StatusOK, status, "export: %s", string(raw))
	for _, vevent := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "BEGIN:VEVENT") {
		if !strings.Contains(vevent, "\nSUMMARY:"+summary+"\n") {
			continue
		}
		var lines []string
		for _, ln := range strings.Split(vevent, "\n") {
			if strings.HasPrefix(ln, "DTSTART") || strings.HasPrefix(ln, "DTEND") {
				lines = append(lines, ln)
			}
		}
		return lines
	}
	t.Fatalf("no VEVENT with SUMMARY %q in the export:\n%s", summary, raw)
	return nil
}

// TestEventOffsetTimesStoredAsUTC is the F22 regression over the real socket: a
// REST or MCP write whose start/end carry a numeric UTC offset and no
// `timezone` used to be stored as DTSTART;TZID=:<wall clock> — an empty TZID
// every reader takes for UTC, so the event moved by the offset. It must be the
// same instant in UTC; a `timezone` still yields its TZID.
func TestEventOffsetTimesStoredAsUTC(t *testing.T) {
	token := registerAndLogin(t, "offset-times@example.test", "OffsetTimes123!", "Offset Times")
	_, calUUID := createCalendar(t, token, "Offsets", "#445566")
	utcLine := regexp.MustCompile(`^DT(START|END):\d{8}T\d{6}Z$`)

	t.Run("RESTCreateAndPatch", func(t *testing.T) {
		var ev struct {
			ID    string `json:"id"`
			Start string `json:"start"`
		}
		code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/events/", token, map[string]any{
			"summary": "rest offset", "start": "2030-03-01T10:00:00+05:00", "end": "2030-03-01T11:00:00+05:00",
		}, &ev)
		require.Equal(t, http.StatusCreated, code)
		assert.Equal(t, "2030-03-01T05:00:00Z", ev.Start, "the response must carry the sent instant")
		assert.ElementsMatch(t, []string{"DTSTART:20300301T050000Z", "DTEND:20300301T060000Z"},
			exportedTimes(t, token, calUUID, "rest offset"))

		code = doJSONRaw(t, http.MethodPatch, "/calendars/"+calUUID+"/events/"+ev.ID, token, map[string]any{
			"start": "2030-03-02T10:00:00-03:30", "end": "2030-03-02T11:00:00-03:30",
		}, &ev)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "2030-03-02T13:30:00Z", ev.Start)
		assert.ElementsMatch(t, []string{"DTSTART:20300302T133000Z", "DTEND:20300302T143000Z"},
			exportedTimes(t, token, calUUID, "rest offset"))
	})

	t.Run("RESTTimezoneKeepsTZID", func(t *testing.T) {
		code, raw := restCall(t, http.MethodPost, "/calendars/"+calUUID+"/events/", token, map[string]any{
			"summary": "rest berlin", "start": "2030-03-01T10:00:00+05:00", "end": "2030-03-01T11:00:00+05:00",
			"timezone": "Europe/Berlin",
		})
		require.Equalf(t, http.StatusCreated, code, "create: %s", raw)
		assert.Contains(t, exportedTimes(t, token, calUUID, "rest berlin"),
			"DTSTART;TZID=Europe/Berlin:20300301T060000")
	})

	t.Run("MCPCreateAndUpdate", func(t *testing.T) {
		mcp := mcpToken(t, token, "offset times")
		status, headers, _ := mcpCall(t, mcp, "",
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"integration","version":"1"}}}`)
		require.Equal(t, http.StatusOK, status)
		session := headers.Get("Mcp-Session-Id")

		status, _, body := mcpCall(t, mcp, session, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_event","arguments":{`+
				`"calendar_id":%q,"title":"mcp offset","start":"2030-03-05T09:00:00+02:00","end":"2030-03-05T09:30:00+02:00"}}}`,
			calUUID))
		require.Equal(t, http.StatusOK, status)
		payload, isError := toolResultText(t, body)
		require.False(t, isError, "create_event failed: %v", payload)
		eventID := payload["event"].(map[string]any)["id"].(string)
		assert.ElementsMatch(t, []string{"DTSTART:20300305T070000Z", "DTEND:20300305T073000Z"},
			exportedTimes(t, token, calUUID, "mcp offset"))

		status, _, body = mcpCall(t, mcp, session, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"update_event","arguments":{`+
				`"event_id":%q,"start":"2030-03-06T09:00:00+09:00","end":"2030-03-06T10:00:00+09:00"}}}`, eventID))
		require.Equal(t, http.StatusOK, status)
		payload, isError = toolResultText(t, body)
		require.False(t, isError, "update_event failed: %v", payload)
		for _, ln := range exportedTimes(t, token, calUUID, "mcp offset") {
			assert.Regexp(t, utcLine, ln)
		}
		assert.Contains(t, exportedTimes(t, token, calUUID, "mcp offset"), "DTSTART:20300306T000000Z")
	})
}
