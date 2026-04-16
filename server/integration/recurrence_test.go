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

// TestRecurringEventUpdateThis edits a single occurrence of a weekly event
// with scope=this&recurrence_id=<occ2>. The server must persist the edit as
// an exception VEVENT (RECURRENCE-ID property) on the master series. When
// the list is expanded, only occurrence 2 carries the edited summary; the
// other three instances still show the original.
func TestRecurringEventUpdateThis(t *testing.T) {
	email := "rrule-update-this@example.test"
	password := "rruleSecret!123"
	token := registerAndLogin(t, email, password, "Rrule Update This")

	calID, _ := createCalendar(t, token, "Update This", "#aa11ee")

	start := time.Date(2033, 8, 1, 9, 0, 0, 0, time.UTC)
	count := 4
	var ev struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/events/", token,
		map[string]any{
			"summary":  "Original weekly",
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

	rangeQS := "?start=2033-07-15T00:00:00Z&end=2033-09-15T00:00:00Z&expand=true"

	// Target the SECOND occurrence (one week after the first).
	targetRID := start.Add(7 * 24 * time.Hour).UTC().Format("20060102T150405Z")

	// PATCH with scope=this — only the targeted occurrence should change.
	newSummary := "Edited just this one"
	status, raw := restCall(t, http.MethodPatch,
		fmt.Sprintf("/calendars/%d/events/%s?scope=this&recurrence_id=%s",
			calID, ev.ID, targetRID),
		token, map[string]any{"summary": newSummary})
	require.Equalf(t, http.StatusOK, status, "update occurrence: %s", errorMessage(raw))

	after := listEventsDetailed(t, token, calID, rangeQS)
	assert.Lenf(t, after, count,
		"scope=this must not change instance count (got %d)", len(after))

	// Exactly the target RID should have the new summary; the other three
	// instances must still be "Original weekly".
	var editedSummaries, originalSummaries int
	for _, inst := range after {
		if inst.RecurrenceID == targetRID {
			assert.Equalf(t, newSummary, inst.Summary,
				"occurrence %s should carry the edited summary", targetRID)
			editedSummaries++
		} else {
			assert.Equalf(t, "Original weekly", inst.Summary,
				"non-target occurrence %s should keep the master summary", inst.RecurrenceID)
			originalSummaries++
		}
	}
	assert.Equalf(t, 1, editedSummaries,
		"exactly one occurrence should carry the edited summary (got %d)", editedSummaries)
	assert.Equalf(t, count-1, originalSummaries,
		"remaining %d occurrences should keep the original summary (got %d)", count-1, originalSummaries)
}

// TestRecurringEventUpdateThisAndFuture splits a weekly series at occurrence 3
// with scope=this_and_future. The first two instances keep the original
// summary (they live on the OLD master with UNTIL truncating the RRULE),
// while occurrences 3–4 are served from the NEW master with the edited
// summary. The total instance count stays at 4 because the new master's
// COUNT is reduced by the number of instances the old (truncated) master
// produces.
func TestRecurringEventUpdateThisAndFuture(t *testing.T) {
	email := "rrule-update-future@example.test"
	password := "rruleSecret!123"
	token := registerAndLogin(t, email, password, "Rrule Update Future")

	calID, _ := createCalendar(t, token, "Update Future", "#3344aa")

	start := time.Date(2033, 10, 3, 15, 0, 0, 0, time.UTC) // a Monday
	var ev struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/events/", token,
		map[string]any{
			"summary":  "Team sync",
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

	rangeQS := "?start=2033-09-20T00:00:00Z&end=2033-11-15T00:00:00Z&expand=true"

	// Split at occurrence 3 (two weeks after start) — that instance and the
	// fourth should take the new summary; the first two remain untouched.
	splitRID := start.Add(14 * 24 * time.Hour).UTC().Format("20060102T150405Z")
	newSummary := "Renamed from here on"

	status, raw := restCall(t, http.MethodPatch,
		fmt.Sprintf("/calendars/%d/events/%s?scope=this_and_future&recurrence_id=%s",
			calID, ev.ID, splitRID),
		token, map[string]any{"summary": newSummary})
	require.Equalf(t, http.StatusOK, status, "split & update future: %s", errorMessage(raw))

	after := listEventsDetailed(t, token, calID, rangeQS)
	assert.Lenf(t, after, 4,
		"split must preserve the original instance count (got %d)", len(after))

	// Before the split (recurrence_id < splitRID) → original summary.
	// At / after the split                     → new summary.
	splitT, err := time.Parse("20060102T150405Z", splitRID)
	require.NoError(t, err)

	var pre, post int
	for _, inst := range after {
		ridT, err := time.Parse("20060102T150405Z", inst.RecurrenceID)
		if !assert.NoErrorf(t, err, "parse rid=%s", inst.RecurrenceID) {
			continue
		}
		if ridT.Before(splitT) {
			pre++
			assert.Equalf(t, "Team sync", inst.Summary,
				"pre-split occurrence %s must keep original summary", inst.RecurrenceID)
		} else {
			post++
			assert.Equalf(t, newSummary, inst.Summary,
				"post-split occurrence %s must carry edited summary", inst.RecurrenceID)
		}
	}
	assert.Equalf(t, 2, pre, "exactly 2 pre-split instances expected (got %d)", pre)
	assert.Equalf(t, 2, post, "exactly 2 post-split instances expected (got %d)", post)
}

// listEventsDetailed mirrors listEvents but also surfaces the Summary field
// so the scoped-update tests can assert per-instance values.
func listEventsDetailed(t *testing.T, token string, calID uint, rangeQS string) []struct {
	UID          string `json:"uid"`
	RecurrenceID string `json:"recurrence_id"`
	Summary      string `json:"summary"`
} {
	t.Helper()
	var resp struct {
		Events []struct {
			UID          string `json:"uid"`
			RecurrenceID string `json:"recurrence_id"`
			Summary      string `json:"summary"`
		} `json:"events"`
	}
	code := doJSONRaw(t, http.MethodGet,
		"/calendars/"+uintStr(calID)+"/events/"+rangeQS, token, nil, &resp)
	require.Equal(t, http.StatusOK, code)
	return resp.Events
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
