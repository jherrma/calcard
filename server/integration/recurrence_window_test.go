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

// TestRecurringEventVisibleInLaterWindow is the regression test for C1: a
// recurring event whose first occurrence is far in the past must still appear
// in a listing window months later. Before the fix, ListEvents filtered on
// end_time (the first occurrence's end only), so the event vanished from every
// window past its first occurrence.
func TestRecurringEventVisibleInLaterWindow(t *testing.T) {
	token := registerAndLogin(t, "rrule-window@example.test", "rruleSecret!123", "Rrule Window User")
	calID, _ := createCalendar(t, token, "Recurring Window", "#123456")

	start := time.Date(2033, 1, 3, 9, 0, 0, 0, time.UTC) // a Monday
	var ev struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/events/", token,
		map[string]any{
			"summary":    "Weekly standup",
			"start":      start.Format(time.RFC3339),
			"end":        start.Add(time.Hour).Format(time.RFC3339),
			"timezone":   "UTC",
			"all_day":    false,
			"recurrence": map[string]any{"frequency": "WEEKLY"}, // unbounded
		}, &ev)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, ev.ID)

	// Tight window around the occurrence 8 weeks after the first one.
	occ8 := start.Add(56 * 24 * time.Hour)
	rangeQS := fmt.Sprintf("?start=%s&end=%s&expand=true",
		occ8.Add(-time.Hour).Format(time.RFC3339), occ8.Add(2*time.Hour).Format(time.RFC3339))
	got := listEvents(t, token, calID, rangeQS)
	require.Lenf(t, got, 1, "recurring event must remain visible 8 weeks later, got %d", len(got))

	// scope=this_and_future at start+14d should shrink recurrence_end_time so
	// the later window now returns nothing.
	splitRID := start.Add(14 * 24 * time.Hour).UTC().Format("20060102T150405Z")
	status, raw := restCall(t, http.MethodDelete,
		fmt.Sprintf("/calendars/%d/events/%s?scope=this_and_future&recurrence_id=%s", calID, ev.ID, splitRID),
		token, nil)
	require.Equalf(t, http.StatusNoContent, status, "this_and_future delete: %s", errorMessage(raw))

	after := listEvents(t, token, calID, rangeQS)
	assert.Lenf(t, after, 0, "after terminating the series, later window must be empty (got %d)", len(after))
}

// TestNonDTENDEventsVisibleInWindow covers the gap the C1 window test above left
// open: REST create always writes a DTEND, so it never exercises the denorm
// paths that derive end_time when DTEND is absent. A DURATION-only event
// (end = start+duration) and an all-day VALUE=DATE event with no DTEND
// (end = start+1 day) must both derive a non-nil end_time; if that regressed to
// nil they would vanish from the listing filter. They must be IMPORTED (not
// REST-created) to reach that gap.
func TestNonDTENDEventsVisibleInWindow(t *testing.T) {
	token := registerAndLogin(t, "no-dtend-window@example.test", "rruleSecret!123", "NoDtend User")
	calID, calUUID := createCalendar(t, token, "No DTEND Cal", "#0f0f0f")

	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		// DURATION only, no DTEND.
		"BEGIN:VEVENT\r\nUID:dur-1@x\r\nDTSTAMP:20340101T000000Z\r\nSUMMARY:Duration only\r\nDTSTART:20350601T100000Z\r\nDURATION:PT1H\r\nEND:VEVENT\r\n" +
		// All-day VALUE=DATE, no DTEND (RFC 5545 default end = start + 1 day).
		"BEGIN:VEVENT\r\nUID:allday-1@x\r\nDTSTAMP:20340101T000000Z\r\nSUMMARY:All day no dtend\r\nDTSTART;VALUE=DATE:20350602\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	status, raw := rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+calUUID+"/import",
		token, []byte(ics), map[string]string{"Content-Type": "text/calendar"})
	require.Equalf(t, http.StatusOK, status, "import: %s", errorMessage(raw))

	rangeQS := "?start=2035-06-01T00:00:00Z&end=2035-06-04T00:00:00Z&expand=true"
	got := listEvents(t, token, calID, rangeQS)
	require.Lenf(t, got, 2, "both non-DTEND events must be listable in a covering window, got %d", len(got))
}

// TestRESTCreatedEventHasETag is the regression test for H10: REST-created and
// REST-updated events must carry a non-empty, changing ETag visible to DAV
// clients (verified by reading the ETag header on a DAV GET).
func TestRESTCreatedEventHasETag(t *testing.T) {
	email := "rest-etag@example.test"
	token, username := registerAndLoginFull(t, email, "rruleSecret!123", "Rest Etag User")
	_, appPassword := createAppPassword(t, token, "etag-test")

	calID, calUUID := createCalendar(t, token, "Etag Cal", "#654321")
	calPath := calUUID + ".ics"

	start := time.Date(2034, 2, 1, 9, 0, 0, 0, time.UTC)
	var ev struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost,
		"/calendars/"+uintStr(calID)+"/events/", token,
		map[string]any{
			"summary": "Etag test", "start": start.Format(time.RFC3339),
			"end": start.Add(time.Hour).Format(time.RFC3339), "timezone": "UTC", "all_day": false,
		}, &ev)
	require.Equal(t, http.StatusCreated, code)

	davPath := fmt.Sprintf("/dav/%s/calendars/%s/%s.ics", username, calPath, ev.ID)
	etag1 := davGetETag(t, davPath, email, appPassword)
	require.NotEmpty(t, etag1, "REST-created event must expose a non-empty ETag over DAV")

	// Update the summary; ETag must change.
	status, raw := restCall(t, http.MethodPatch,
		fmt.Sprintf("/calendars/%d/events/%s?scope=all", calID, ev.ID), token,
		map[string]any{"summary": "Etag test edited"})
	require.Equalf(t, http.StatusOK, status, "update: %s", errorMessage(raw))

	etag2 := davGetETag(t, davPath, email, appPassword)
	require.NotEmpty(t, etag2)
	assert.NotEqualf(t, etag1, etag2, "ETag must change after a REST edit (%s)", etag1)
}

// TestImportedEventReadableEverywhere is the regression test for C2: imported
// events were stored as bare VEVENT blocks that the strict VCALENDAR decoder
// could not read, so expanded REST lists 500'd. They must now be fully
// readable.
func TestImportedEventReadableEverywhere(t *testing.T) {
	token := registerAndLogin(t, "import-c2@example.test", "rruleSecret!123", "Import C2 User")
	calID, calUUID := createCalendar(t, token, "Import C2 Cal", "#abcdef")

	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:c2-one@x\r\nDTSTAMP:20340101T000000Z\r\nSUMMARY:First\r\nDTSTART:20350101T100000Z\r\nDTEND:20350101T110000Z\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:c2-two@x\r\nDTSTAMP:20340101T000000Z\r\nSUMMARY:Second\r\nDTSTART:20350102T100000Z\r\nDTEND:20350102T110000Z\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	status, raw := rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+calUUID+"/import",
		token, []byte(ics), map[string]string{"Content-Type": "text/calendar"})
	require.Equalf(t, http.StatusOK, status, "import: %s", errorMessage(raw))

	// Expanded list over a window containing both events must be 200 (was 500)
	// and contain both.
	rangeQS := "?start=2035-01-01T00:00:00Z&end=2035-01-03T00:00:00Z&expand=true"
	got := listEvents(t, token, calID, rangeQS)
	require.Lenf(t, got, 2, "both imported events must be listable when expanded, got %d", len(got))
}

// davGetETag does a DAV GET and returns the ETag response header.
func davGetETag(t *testing.T, davPath, user, pass string) string {
	t.Helper()
	status, hdrs, body := davCall(t, "GET", davPath, user, pass, "", nil)
	require.Equalf(t, http.StatusOK, status, "DAV GET %s: %s", davPath, string(body))
	return hdrs.Get("ETag")
}
