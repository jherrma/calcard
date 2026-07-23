//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventRoutesRequireUUID locks the #52 contract for the events route:
// /calendars/:calendar_id/events takes the calendar UUID (matching the sibling
// CRUD routes), and the old numeric id is rejected with 404 (not leaked as
// 400/403), so the identifier form can't silently drift back to numeric.
func TestEventRoutesRequireUUID(t *testing.T) {
	token := registerAndLogin(t, "event-uuid@example.test", "eventUUID!123", "Event UUID User")
	calID, calUUID := createCalendar(t, token, "Event UUID Cal", "#2233ff")

	body := map[string]any{
		"summary":  "UUID contract event",
		"start":    "2033-05-01T09:00:00Z",
		"end":      "2033-05-01T10:00:00Z",
		"timezone": "UTC",
		"all_day":  false,
	}

	// Create via the UUID → 201.
	var ev struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/events/", token, body, &ev)
	require.Equal(t, http.StatusCreated, code, "create event via UUID must succeed")
	require.NotEmpty(t, ev.ID)

	// Create via the numeric id → 404 (calendar not found).
	status, _ := restCall(t, http.MethodPost, "/calendars/"+uintStr(calID)+"/events/", token, body)
	assert.Equal(t, http.StatusNotFound, status, "numeric calendar id must 404 on event create")

	// List via the numeric id → 404.
	rangeQS := "?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z&expand=false"
	status, _ = restCall(t, http.MethodGet, "/calendars/"+uintStr(calID)+"/events/"+rangeQS, token, nil)
	assert.Equal(t, http.StatusNotFound, status, "numeric calendar id must 404 on event list")
}
