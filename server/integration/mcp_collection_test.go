//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpTool calls one tool over POST /mcp (no session: the transport accepts
// stateless calls) and returns the decoded payload, or {"message": text} with
// true for a tool error.
func mcpTool(t *testing.T, credential, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	require.NoError(t, err)
	status, _, decoded := mcpCall(t, credential, "", string(body))
	require.Equal(t, http.StatusOK, status, "tools/call %s", name)
	return toolResultText(t, decoded)
}

// TestMCPCollectionTools drives the calendar and address-book collection tools
// over the real socket and cross-checks every write through REST: the two
// surfaces share one use case, so what MCP creates, renames or deletes must be
// exactly what REST then reports.
func TestMCPCollectionTools(t *testing.T) {
	jwt, _ := registerAndLoginFull(t, "mcp-collections@example.com", "MCPpassword123!", "MCP Collections")
	token := mcpToken(t, jwt, "collection client")

	t.Run("CalendarRoundTrip", func(t *testing.T) {
		created, isErr := mcpTool(t, token, "create_calendar", map[string]any{
			"name": "MCP Climbing", "description": "Gym sessions",
			"color": "#10b981", "timezone": "Europe/Berlin",
		})
		require.False(t, isErr, "create_calendar failed: %v", created)
		calUUID := created["calendar"].(map[string]any)["id"].(string)
		require.NotEmpty(t, calUUID)

		// REST sees the calendar exactly as created.
		var rest struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Color       string `json:"color"`
			Timezone    string `json:"timezone"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID, jwt, nil, &rest))
		assert.Equal(t, "MCP Climbing", rest.Name)
		assert.Equal(t, "Gym sessions", rest.Description)
		assert.Equal(t, "#10b981", rest.Color)
		assert.Equal(t, "Europe/Berlin", rest.Timezone)

		listed, isErr := mcpTool(t, token, "list_calendars", map[string]any{})
		require.False(t, isErr)
		assert.Contains(t, mcpListIDs(listed, "calendars"), calUUID)

		// Partial update: only the name moves.
		updated, isErr := mcpTool(t, token, "update_calendar", map[string]any{
			"calendar_id": calUUID, "name": "MCP Bouldering",
		})
		require.False(t, isErr, "update_calendar failed: %v", updated)
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID, jwt, nil, &rest))
		assert.Equal(t, "MCP Bouldering", rest.Name)
		assert.Equal(t, "#10b981", rest.Color, "an omitted field keeps its value")
		assert.Equal(t, "Europe/Berlin", rest.Timezone, "an omitted field keeps its value")

		// Invalid input is refused with the REST 400 text.
		refused, isErr := mcpTool(t, token, "update_calendar", map[string]any{
			"calendar_id": calUUID, "color": "green",
		})
		require.True(t, isErr)
		assert.Equal(t, "invalid hex color format, expected #RRGGBB", refused["message"])

		// Put an event in it, then delete the calendar.
		event, isErr := mcpTool(t, token, "create_event", map[string]any{
			"calendar_id": calUUID, "title": "Lead session",
			"start": "2030-07-01T18:00:00Z", "end": "2030-07-01T20:00:00Z",
		})
		require.False(t, isErr, "create_event failed: %v", event)
		eventID := event["event"].(map[string]any)["id"].(string)

		notConfirmed, isErr := mcpTool(t, token, "delete_calendar", map[string]any{
			"calendar_id": calUUID, "confirmation": "yes",
		})
		require.True(t, isErr)
		assert.Equal(t, "please type DELETE to confirm calendar deletion", notConfirmed["message"])
		status, _ := restCall(t, http.MethodGet, "/calendars/"+calUUID, jwt, nil)
		require.Equal(t, http.StatusOK, status, "an unconfirmed delete must leave the calendar alone")

		deleted, isErr := mcpTool(t, token, "delete_calendar", map[string]any{
			"calendar_id": calUUID, "confirmation": "DELETE",
		})
		require.False(t, isErr, "delete_calendar failed: %v", deleted)
		assert.Equal(t, true, deleted["deleted"])

		status, _ = restCall(t, http.MethodGet, "/calendars/"+calUUID, jwt, nil)
		assert.Equal(t, http.StatusNotFound, status, "the calendar deleted over MCP is gone over REST")
		status, _ = restCall(t, http.MethodGet, "/calendars/"+calUUID+"/events/"+eventID, jwt, nil)
		assert.Equal(t, http.StatusNotFound, status, "its event went with it")
		listed, _ = mcpTool(t, token, "list_calendars", map[string]any{})
		assert.NotContains(t, mcpListIDs(listed, "calendars"), calUUID)
	})

	t.Run("AddressBookRoundTrip", func(t *testing.T) {
		created, isErr := mcpTool(t, token, "create_address_book", map[string]any{
			"name": "MCP Partners", "description": "Belay buddies",
		})
		require.False(t, isErr, "create_address_book failed: %v", created)
		abUUID := created["address_book"].(map[string]any)["id"].(string)
		require.NotEmpty(t, abUUID)

		var rest struct {
			Name        string `json:"Name"`
			Description string `json:"Description"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/"+abUUID, jwt, nil, &rest))
		assert.Equal(t, "MCP Partners", rest.Name)
		assert.Equal(t, "Belay buddies", rest.Description)

		listed, isErr := mcpTool(t, token, "list_address_books", map[string]any{})
		require.False(t, isErr)
		assert.Contains(t, mcpListIDs(listed, "address_books"), abUUID)

		updated, isErr := mcpTool(t, token, "update_address_book", map[string]any{
			"address_book_id": abUUID, "description": "Rope team",
		})
		require.False(t, isErr, "update_address_book failed: %v", updated)
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/"+abUUID, jwt, nil, &rest))
		assert.Equal(t, "MCP Partners", rest.Name, "an omitted field keeps its value")
		assert.Equal(t, "Rope team", rest.Description)

		contact, isErr := mcpTool(t, token, "create_contact", map[string]any{
			"address_book_id": abUUID, "first_name": "Lynn", "last_name": "Hill",
		})
		require.False(t, isErr, "create_contact failed: %v", contact)
		contactID := contact["contact"].(map[string]any)["id"].(string)

		deleted, isErr := mcpTool(t, token, "delete_address_book", map[string]any{
			"address_book_id": abUUID, "confirmation": "DELETE",
		})
		require.False(t, isErr, "delete_address_book failed: %v", deleted)
		assert.Equal(t, true, deleted["deleted"])

		status, _ := restCall(t, http.MethodGet, "/addressbooks/"+abUUID, jwt, nil)
		assert.Equal(t, http.StatusNotFound, status, "the book deleted over MCP is gone over REST")
		status, _ = restCall(t, http.MethodGet, "/addressbooks/"+abUUID+"/contacts/"+contactID, jwt, nil)
		assert.Equal(t, http.StatusNotFound, status, "its contact went with it")
		listed, _ = mcpTool(t, token, "list_address_books", map[string]any{})
		assert.NotContains(t, mcpListIDs(listed, "address_books"), abUUID)
	})

	t.Run("AnotherUserCannotManageThem", func(t *testing.T) {
		// The owner's collections, one of them shared read-write with the
		// outsider: managing a collection is the owner's alone either way.
		_, calUUID := createCalendar(t, jwt, "MCP Owner calendar", "#334455")
		_, abUUID := createAddressBook(t, jwt, "MCP Owner book")

		outsiderEmail := "mcp-collections-outsider@example.com"
		outsiderJWT, _ := registerAndLoginFull(t, outsiderEmail, "MCPpassword123!", "Outsider")
		outsider := mcpToken(t, outsiderJWT, "outsider client")

		sharedCalID, sharedCal := createCalendar(t, jwt, "MCP Owner shared calendar", "#556677")
		require.NotZero(t, sharedCalID)
		code := doJSONRaw(t, http.MethodPost, "/calendars/"+sharedCal+"/shares", jwt,
			map[string]string{"user_identifier": outsiderEmail, "permission": "read-write"}, nil)
		require.Equal(t, http.StatusCreated, code)
		_, sharedAB := createAddressBook(t, jwt, "MCP Owner shared book")
		shareAB(t, jwt, sharedAB, outsiderEmail, "read-write")

		// Control: the outsider really does see the shares.
		listed, _ := mcpTool(t, outsider, "list_calendars", map[string]any{})
		require.Contains(t, mcpListIDs(listed, "calendars"), sharedCal)
		listed, _ = mcpTool(t, outsider, "list_address_books", map[string]any{})
		require.Contains(t, mcpListIDs(listed, "address_books"), sharedAB)

		unknown := uuid.New().String()
		answers := map[string]string{}
		for label, call := range map[string]struct {
			tool string
			args map[string]any
			id   string
		}{
			"rename foreign calendar":   {"update_calendar", map[string]any{"calendar_id": calUUID, "name": "Hijacked"}, calUUID},
			"delete foreign calendar":   {"delete_calendar", map[string]any{"calendar_id": calUUID, "confirmation": "DELETE"}, calUUID},
			"rename shared calendar":    {"update_calendar", map[string]any{"calendar_id": sharedCal, "name": "Hijacked"}, sharedCal},
			"delete shared calendar":    {"delete_calendar", map[string]any{"calendar_id": sharedCal, "confirmation": "DELETE"}, sharedCal},
			"rename unknown calendar":   {"update_calendar", map[string]any{"calendar_id": unknown, "name": "Hijacked"}, unknown},
			"rename foreign book":       {"update_address_book", map[string]any{"address_book_id": abUUID, "name": "Hijacked"}, abUUID},
			"delete foreign book":       {"delete_address_book", map[string]any{"address_book_id": abUUID, "confirmation": "DELETE"}, abUUID},
			"rename shared book":        {"update_address_book", map[string]any{"address_book_id": sharedAB, "name": "Hijacked"}, sharedAB},
			"delete shared book":        {"delete_address_book", map[string]any{"address_book_id": sharedAB, "confirmation": "DELETE"}, sharedAB},
			"delete unknown book":       {"delete_address_book", map[string]any{"address_book_id": unknown, "confirmation": "DELETE"}, unknown},
			"delete foreign book, nope": {"delete_address_book", map[string]any{"address_book_id": abUUID, "confirmation": "nope"}, abUUID},
		} {
			payload, isErr := mcpTool(t, outsider, call.tool, call.args)
			require.True(t, isErr, "%s must be refused: %v", label, payload)
			text := strings.ReplaceAll(payload["message"].(string), call.id, "<id>")
			assert.Contains(t, text, "is owned by you", label)
			answers[strings.SplitN(call.tool, "_", 2)[1]+"/"+label] = text
		}
		// Within one collection type, every refusal reads the same, whether
		// the collection is foreign, shared or does not exist at all.
		calendarAnswers, bookAnswers := map[string]bool{}, map[string]bool{}
		for key, text := range answers {
			if strings.HasPrefix(key, "calendar/") {
				calendarAnswers[text] = true
			} else {
				bookAnswers[text] = true
			}
		}
		assert.Len(t, calendarAnswers, 1, "calendar refusals must be indistinguishable: %v", calendarAnswers)
		assert.Len(t, bookAnswers, 1, "address book refusals must be indistinguishable: %v", bookAnswers)

		// Nothing changed for the owner.
		for _, id := range []string{calUUID, sharedCal} {
			var cal struct {
				Name string `json:"name"`
			}
			require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+id, jwt, nil, &cal))
			assert.NotEqual(t, "Hijacked", cal.Name)
		}
		for _, id := range []string{abUUID, sharedAB} {
			var ab struct {
				Name string `json:"Name"`
			}
			require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/"+id, jwt, nil, &ab))
			assert.NotEqual(t, "Hijacked", ab.Name)
		}
	})
}

// mcpListIDs collects the ids of a list_calendars / list_address_books payload.
func mcpListIDs(payload map[string]any, key string) []string {
	var ids []string
	items, _ := payload[key].([]any)
	for _, raw := range items {
		ids = append(ids, raw.(map[string]any)["id"].(string))
	}
	return ids
}
