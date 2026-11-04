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

// #169 story 09: a REST scope=this edit of a DAV-written series keeps the
// series' relative VALARM on the new override and leaves the absolute one with
// the master.
func TestRestScopeThisKeepsRelativeAlarm(t *testing.T) {
	email := "alarm-split@example.test"
	password := "alarmSplit!123"
	token, username := registerAndLoginFull(t, email, password, "Alarm Split")
	_, appPassword := createAppPassword(t, token, "alarm-split")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	series := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Integration Tests//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:alarm169-series\r\nDTSTAMP:20340101T000000Z\r\n" +
		"DTSTART:20340102T090000Z\r\nDTEND:20340102T100000Z\r\nRRULE:FREQ=WEEKLY;COUNT=6\r\nSUMMARY:Weekly sync\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Soon\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Once\r\nTRIGGER;VALUE=DATE-TIME:20340103T080000Z\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	status, _, body := davCall(t, "PUT", collection+"alarm169-series.ics", email, appPassword, series,
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT: %s", string(body))

	var list struct {
		Events []struct {
			ID  string `json:"id"`
			UID string `json:"uid"`
		} `json:"events"`
	}
	code := doJSONRaw(t, http.MethodGet, "/calendars/"+personal.UUID+"/events/?start=2034-01-01T00:00:00Z&end=2034-03-01T00:00:00Z&expand=false", token, nil, &list)
	require.Equal(t, http.StatusOK, code)
	id := ""
	for _, ev := range list.Events {
		if ev.UID == "alarm169-series" {
			id = ev.ID
		}
	}
	require.NotEmpty(t, id)

	st, raw := restCall(t, http.MethodPatch,
		"/calendars/"+personal.UUID+"/events/"+id+"?scope=this&recurrence_id=20340109T090000Z", token,
		map[string]any{"summary": "Edited once"})
	require.Equal(t, http.StatusOK, st, string(raw))

	st, _, got := davCall(t, "GET", collection+"alarm169-series.ics", email, appPassword, "", nil)
	require.Equal(t, http.StatusOK, st)
	ics := strings.ReplaceAll(string(got), "\r\n ", "")
	assert.Equal(t, 3, strings.Count(ics, "BEGIN:VALARM"))
	assert.Equal(t, 2, strings.Count(ics, "TRIGGER:-PT15M"), "override carries the relative alarm")
	assert.Equal(t, 1, strings.Count(ics, "VALUE=DATE-TIME"), "absolute alarm stays with the master only")
}

// #169 story 10: a DAV-written event with a relative, an absolute and an EMAIL
// VALARM reports them as `reminders` on GET one and in the expanded list.
func TestRestEventRemindersFromDav(t *testing.T) {
	email := "alarm-read@example.test"
	password := "alarmRead!123"
	token, username := registerAndLoginFull(t, email, password, "Alarm Read")
	_, appPassword := createAppPassword(t, token, "alarm-read")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Integration Tests//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:alarm169-read\r\nDTSTAMP:20340101T000000Z\r\n" +
		"DTSTART:20340104T090000Z\r\nDTEND:20340104T100000Z\r\nSUMMARY:Reminders\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20340104T080000Z\r\nEND:VALARM\r\n" +
		"BEGIN:VALARM\r\nACTION:EMAIL\r\nTRIGGER:-P1D\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	status, _, body := davCall(t, "PUT", collection+"alarm169-read.ics", email, appPassword, ics,
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT: %s", string(body))

	want := `[{"index":0,"action":"display","related":"start","offset_seconds":-900,"at":null,"editable":true},
	{"index":1,"action":"display","related":null,"offset_seconds":null,"at":"2034-01-04T08:00:00Z","editable":true},
	{"index":2,"action":"email","related":"start","offset_seconds":-86400,"at":null,"editable":false}]`

	type listed struct {
		Events []struct {
			ID        string          `json:"id"`
			UID       string          `json:"uid"`
			Reminders json.RawMessage `json:"reminders"`
		} `json:"events"`
	}
	for _, expand := range []string{"true", "false"} {
		var list listed
		code := doJSONRaw(t, http.MethodGet, "/calendars/"+personal.UUID+"/events/?start=2034-01-01T00:00:00Z&end=2034-02-01T00:00:00Z&expand="+expand, token, nil, &list)
		require.Equal(t, http.StatusOK, code)
		id := ""
		for _, ev := range list.Events {
			if ev.UID == "alarm169-read" {
				id = ev.ID
				assert.JSONEq(t, want, string(ev.Reminders), "list expand="+expand)
			}
		}
		require.NotEmpty(t, id)

		var one struct {
			Reminders json.RawMessage `json:"reminders"`
		}
		code = doJSONRaw(t, http.MethodGet, "/calendars/"+personal.UUID+"/events/"+id, token, nil, &one)
		require.Equal(t, http.StatusOK, code)
		assert.JSONEq(t, want, string(one.Reminders), "get")
	}
}
