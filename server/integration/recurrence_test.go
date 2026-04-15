//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecurringEventDeleteThis creates a weekly recurring event with 4
// occurrences, deletes the second with scope=this&recurrence_id=..., and
// confirms the list view now reports only 3 instances (the second is
// replaced by an EXDATE on the master VEVENT).
func TestRecurringEventDeleteThis(t *testing.T) {
	email := "rrule-this@example.test"
	password := "rruleSecret!123"
	token := registerAndLogin(t, email, password, "Rrule This User")

	calID, _ := createCalendar(t, token, "Recurring This", "#998877")

	// Start on a specific Monday so recurrence-id timestamps stay aligned.
	start := time.Date(2033, 5, 2, 10, 0, 0, 0, time.UTC)
	count := 4
	var ev struct {
		ID  string `json:"id"`  // internal DB UUID — used in URL paths
		UID string `json:"uid"` // iCalendar UID — returned for reference
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/events/", token,
		map[string]any{
			"summary":  "Weekly standup",
			"start":    start.Format(time.RFC3339),
			"end":      start.Add(time.Hour).Format(time.RFC3339),
			"timezone": "UTC",
			"all_day":  false,
			"recurrence": map[string]any{
				"frequency": "WEEKLY",
				"count":     count,
			},
		}, &ev)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, ev.ID)

	// List across a window that covers all four instances.
	rangeQS := "?start=2033-05-01T00:00:00Z&end=2033-06-01T00:00:00Z&expand=true"
	before := listEvents(t, token, calID, rangeQS)
	require.Lenf(t, before, count,
		"seeded recurrence should produce %d instances, got %d", count, len(before))

	// Pick the SECOND occurrence's recurrence_id (one week after start).
	targetRID := start.Add(7 * 24 * time.Hour).UTC().Format("20060102T150405Z")

	// Sanity check: our target rid really matches an instance the server
	// returned.
	foundTargetRID := false
	for _, inst := range before {
		if inst.RecurrenceID == targetRID {
			foundTargetRID = true
			break
		}
	}
	require.Truef(t, foundTargetRID,
		"expected instance with recurrence_id=%s, got %+v", targetRID, before)

	// DELETE scope=this&recurrence_id=<occurrence 2>.
	status, raw := restCall(t, http.MethodDelete,
		fmt.Sprintf("/calendars/%d/events/%s?scope=this&recurrence_id=%s",
			calID, ev.ID, targetRID),
		token, nil)
	require.Equalf(t, http.StatusNoContent, status, "delete occurrence: %s", errorMessage(raw))

	// After the delete, the list must return exactly count-1 instances,
	// and the specific recurrence_id we excluded must not show up.
	after := listEvents(t, token, calID, rangeQS)
	assert.Lenf(t, after, count-1,
		"after scope=this delete one instance must be removed (got %d)", len(after))
	for _, inst := range after {
		assert.NotEqual(t, targetRID, inst.RecurrenceID,
			"excluded occurrence must not reappear")
	}
}

// TestRecurringEventDeleteThisAndFuture creates a weekly recurring event
// with 4 occurrences and terminates the series starting at occurrence 3
// (scope=this_and_future). Only the first two instances must remain.
func TestRecurringEventDeleteThisAndFuture(t *testing.T) {
	email := "rrule-future@example.test"
	password := "rruleSecret!123"
	token := registerAndLogin(t, email, password, "Rrule Future User")

	calID, _ := createCalendar(t, token, "Recurring Future", "#aabbcc")

	start := time.Date(2033, 7, 4, 14, 0, 0, 0, time.UTC)
	var ev struct {
		ID  string `json:"id"`  // internal DB UUID — used in URL paths
		UID string `json:"uid"` // iCalendar UID — returned for reference
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/events/", token,
		map[string]any{
			"summary":  "Sprint review",
			"start":    start.Format(time.RFC3339),
			"end":      start.Add(30 * time.Minute).Format(time.RFC3339),
			"timezone": "UTC",
			"all_day":  false,
			"recurrence": map[string]any{
				"frequency": "WEEKLY",
				"count":     4,
			},
		}, &ev)
	require.Equal(t, http.StatusCreated, code)

	rangeQS := "?start=2033-07-01T00:00:00Z&end=2033-08-10T00:00:00Z&expand=true"
	before := listEvents(t, token, calID, rangeQS)
	require.Len(t, before, 4)

	// Terminate at occurrence 3 (two weeks after start).
	splitRID := start.Add(14 * 24 * time.Hour).UTC().Format("20060102T150405Z")

	status, raw := restCall(t, http.MethodDelete,
		fmt.Sprintf("/calendars/%d/events/%s?scope=this_and_future&recurrence_id=%s",
			calID, ev.ID, splitRID),
		token, nil)
	require.Equalf(t, http.StatusNoContent, status, "delete future: %s", errorMessage(raw))

	// After the truncation only the first two occurrences should remain.
	after := listEvents(t, token, calID, rangeQS)
	assert.Lenf(t, after, 2,
		"scope=this_and_future must retain only the two occurrences before the split (got %d)",
		len(after))
	for _, inst := range after {
		start, _ := time.Parse("20060102T150405Z", inst.RecurrenceID)
		// The retained instances must start strictly before the split.
		splitT, _ := time.Parse("20060102T150405Z", splitRID)
		assert.Truef(t, start.Before(splitT),
			"instance with rid=%s must be before split rid=%s", inst.RecurrenceID, splitRID)
	}
}

// listEvents is a thin wrapper around GET /calendars/:id/events returning
// the decoded events payload. Used by the recurrence tests to count the
// remaining instances after a scoped delete.
func listEvents(t *testing.T, token string, calID uint, rangeQS string) []struct {
	UID          string `json:"uid"`
	RecurrenceID string `json:"recurrence_id"`
} {
	t.Helper()
	var resp struct {
		Events []struct {
			UID          string `json:"uid"`
			RecurrenceID string `json:"recurrence_id"`
		} `json:"events"`
	}
	code := doJSONRaw(t, http.MethodGet,
		"/calendars/"+uintStr(calID)+"/events/"+rangeQS, token, nil, &resp)
	require.Equal(t, http.StatusOK, code)
	return resp.Events
}
