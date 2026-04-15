//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarExportEndpoint drives GET /calendars/:uuid/export. The full
// backup (ZIP of everything) is covered by TestExportImportRoundtrip —
// this one just proves the per-calendar .ics variant works end-to-end:
// the response must parse as an iCalendar feed and contain the seeded
// event's UID / SUMMARY.
func TestCalendarExportEndpoint(t *testing.T) {
	email := "calexport@example.test"
	password := "exportSecret!123"
	token := registerAndLogin(t, email, password, "Export User")

	calID, calUUID := createCalendar(t, token, "Exportable", "#112233")

	// Seed one event so we have something to look for in the feed.
	start := time.Date(2032, 4, 15, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	var ev struct {
		UID     string `json:"uid"`
		Summary string `json:"summary"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/events/", token,
		map[string]any{
			"summary":  "Exportable Event",
			"start":    start.Format(time.RFC3339),
			"end":      end.Format(time.RFC3339),
			"timezone": "UTC",
			"all_day":  false,
		}, &ev)
	require.Equal(t, http.StatusCreated, code)

	// Hit the export endpoint (uses calendar UUID, not numeric id).
	status, raw := restCall(t, http.MethodGet, "/calendars/"+calUUID+"/export", token, nil)
	require.Equalf(t, http.StatusOK, status, "export: %s", string(raw))
	body := string(raw)
	assert.True(t, strings.HasPrefix(body, "BEGIN:VCALENDAR"),
		"export body must start with BEGIN:VCALENDAR, got: %q", body[:min(40, len(body))])
	assert.Contains(t, body, "END:VCALENDAR")
	assert.Contains(t, body, "UID:"+ev.UID, "feed must include the seeded event's UID")
	assert.Contains(t, body, "SUMMARY:Exportable Event", "feed must include the seeded event's summary")
}

// TestDeleteLastCalendarGuard asserts the server refuses to delete the
// user's only remaining calendar — that would leave the user with no
// calendar to write events to, and the use case explicitly guards against
// it. TestDeleteLastAddressBookGuard does the same for address books.
//
// We rely on registration auto-creating the default "Personal" calendar,
// then delete every other calendar to get into the "one left" state.
func TestDeleteLastCalendarGuard(t *testing.T) {
	email := "cal-last@example.test"
	password := "lastSecret!123"
	token := registerAndLogin(t, email, password, "Last Cal User")

	// List current calendars. If there's more than the default Personal,
	// delete the extras so we land in the one-left state.
	idx := listCalendarsIndex(t, token)
	require.Contains(t, idx, "Personal", "registration should seed a Personal calendar")
	var personalUUID string
	for name, entry := range idx {
		if name == "Personal" {
			personalUUID = entry.UUID
			continue
		}
		// Delete everything else just to be safe.
		status, _ := restCall(t, http.MethodDelete, "/calendars/"+entry.UUID, token,
			map[string]string{"confirmation": "DELETE"})
		require.Equal(t, http.StatusNoContent, status)
	}
	require.NotEmpty(t, personalUUID)

	// Attempting to delete the last calendar must fail — any 4xx is fine
	// (the use case returns the error as 400 today; the point is it is NOT
	// silently accepted).
	status, raw := restCall(t, http.MethodDelete, "/calendars/"+personalUUID, token,
		map[string]string{"confirmation": "DELETE"})
	assert.GreaterOrEqualf(t, status, 400,
		"deleting the last calendar must NOT succeed (got %d, body: %s)", status, string(raw))
	assert.Less(t, status, 500, "must return a client error, not 5xx")

	// And the calendar is still there.
	idx = listCalendarsIndex(t, token)
	require.Contains(t, idx, "Personal", "Personal calendar must survive the rejected delete")
}

// TestDeleteLastAddressBookGuard is the same shape for address books.
func TestDeleteLastAddressBookGuard(t *testing.T) {
	email := "ab-last@example.test"
	password := "lastSecret!123"
	token := registerAndLogin(t, email, password, "Last AB User")

	idx := listAddressBooksIndex(t, token)
	require.Contains(t, idx, "Contacts", "registration should seed a Contacts address book")
	var onlyID uint
	for name, id := range idx {
		if name == "Contacts" {
			onlyID = id
			continue
		}
		status, _ := restCall(t, http.MethodDelete, "/addressbooks/"+uintStr(id), token,
			map[string]string{"confirmation": "DELETE"})
		require.Equal(t, http.StatusNoContent, status)
	}
	require.NotZero(t, onlyID)

	status, raw := restCall(t, http.MethodDelete, "/addressbooks/"+uintStr(onlyID), token,
		map[string]string{"confirmation": "DELETE"})
	assert.GreaterOrEqualf(t, status, 400,
		"deleting the last address book must NOT succeed (got %d, body: %s)", status, string(raw))
	assert.Less(t, status, 500)

	idx = listAddressBooksIndex(t, token)
	assert.Contains(t, idx, "Contacts")
}

// min is Go 1.21+, inlined here for clarity.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
