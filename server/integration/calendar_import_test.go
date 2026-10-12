//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarImportKeepsRecurrenceOverrides is the regression test for F14: a
// recurring master and its RECURRENCE-ID override share one UID, so an import
// must store them as ONE calendar object (RFC 4791 §4.1), exactly like a DAV PUT
// of the same data. Before the fix the override was handled as a second object
// and skipped as a duplicate UID, so the moved instance was lost.
func TestCalendarImportKeepsRecurrenceOverrides(t *testing.T) {
	email := "import-overrides@example.test"
	password := "importSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Import Overrides")
	_, appPassword := createAppPassword(t, token, "import-overrides")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok, "default Personal calendar should exist")

	const uid = "series-f14@example.test"
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20300101T000000Z\r\n" +
		"DTSTART:20300701T100000Z\r\nDTEND:20300701T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\n" +
		"SUMMARY:Series\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20300101T000000Z\r\n" +
		"RECURRENCE-ID:20300702T100000Z\r\nDTSTART:20300702T150000Z\r\nDTEND:20300702T160000Z\r\n" +
		"SUMMARY:Series (moved)\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	status, raw := rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+personal.UUID+"/import",
		token, []byte(ics), map[string]string{"Content-Type": "text/calendar"})
	require.Equalf(t, http.StatusOK, status, "import: %s", errorMessage(raw))
	var result struct {
		Total    int `json:"total"`
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
		Failed   int `json:"failed"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	assert.Equal(t, 1, result.Total, "a series with its override is one calendar object")
	assert.Equal(t, 1, result.Imported)
	assert.Equal(t, 0, result.Skipped, "the override is part of the series, not a duplicate")
	assert.Equal(t, 0, result.Failed)

	var resp struct {
		Events []struct {
			UID          string `json:"uid"`
			RecurrenceID string `json:"recurrence_id"`
			Summary      string `json:"summary"`
			Start        string `json:"start"`
		} `json:"events"`
	}
	code := doJSONRaw(t, http.MethodGet, "/calendars/"+personal.UUID+
		"/events/?start=2030-06-30T00:00:00Z&end=2030-07-10T00:00:00Z&expand=true", token, nil, &resp)
	require.Equal(t, http.StatusOK, code)
	var instances int
	for _, ev := range resp.Events {
		if ev.UID != uid {
			continue
		}
		instances++
		if ev.RecurrenceID == "20300702T100000Z" {
			assert.Equal(t, "Series (moved)", ev.Summary, "the overridden instance keeps its summary")
			assert.Equal(t, "2030-07-02T15:00:00Z", ev.Start, "the overridden instance keeps its moved time")
		}
	}
	assert.Equal(t, 3, instances, "the series has three instances")

	// DAV clients see one resource holding master and override.
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"
	status, _, body := davCall(t, "GET", collection+uid+".ics", email, appPassword, "", nil)
	require.Equalf(t, http.StatusOK, status, "DAV GET: %s", string(body))
	assert.Equal(t, 2, strings.Count(string(body), "BEGIN:VEVENT"), "master and override in one object:\n%s", body)
	assert.Contains(t, string(body), "RECURRENCE-ID:20300702T100000Z")

	// Re-importing skips the whole series once, not once per component.
	status, raw = rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+personal.UUID+"/import",
		token, []byte(ics), map[string]string{"Content-Type": "text/calendar"})
	require.Equalf(t, http.StatusOK, status, "re-import: %s", errorMessage(raw))
	require.NoError(t, json.Unmarshal(raw, &result))
	assert.Equal(t, 1, result.Skipped, "re-import skips the series as one duplicate")
	assert.Equal(t, 0, result.Imported)
}
