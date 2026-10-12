//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A calendar object resource holds exactly one calendar component type and one
// UID (RFC 4791 §4.1), and must be valid iCalendar (§5.3.2.1). A PUT that breaks
// that is the client's mistake and must be answered with a 409 precondition
// error, not a 500.
func TestCalDAVPutRejectsInvalidCalendarObject(t *testing.T) {
	email := "caldav-invalid@example.test"
	password := "caldavSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CalDAV Invalid")
	_, appPassword := createAppPassword(t, token, "caldav-invalid")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok, "default Personal calendar should exist")
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	vevent := func(uid string) string {
		return "BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20300101T000000Z\r\n" +
			"DTSTART:20300701T090000Z\r\nSUMMARY:x\r\nEND:VEVENT\r\n"
	}
	wrap := func(body string) string {
		return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + body + "END:VCALENDAR\r\n"
	}
	vtodo := "BEGIN:VTODO\r\nUID:one\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:t\r\nEND:VTODO\r\n"
	vtimezone := "BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\nBEGIN:STANDARD\r\n" +
		"DTSTART:19701025T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n"
	withProp := func(uid, prop string) string {
		return strings.Replace(vevent(uid), "SUMMARY:x\r\n", "SUMMARY:x\r\n"+prop, 1)
	}

	const objectResource, calendarData = "valid-calendar-object-resource", "valid-calendar-data"
	// Each body parses, so it reaches the backend; the ones without a
	// component or a UID used to fail only when re-encoded, as a 500 (F16).
	cases := map[string]struct{ body, precondition string }{
		"two different UIDs":    {wrap(vevent("uid-one") + vevent("uid-two")), objectResource},
		"mixed component types": {wrap(vevent("one") + vtodo), objectResource},
		"no component":          {wrap(""), objectResource},
		"only a VTIMEZONE":      {wrap(vtimezone), objectResource},
		"event without UID": {wrap(strings.Replace(vevent("gone"), "UID:gone\r\n", "", 1)),
			objectResource},
		"DTEND and DURATION": {wrap(withProp("dur", "DTEND:20300701T100000Z\r\nDURATION:PT1H\r\n")),
			calendarData},
		"two SUMMARY properties": {wrap(withProp("sum", "SUMMARY:y\r\n")), calendarData},
	}
	put := func(name, body string) (int, []byte) {
		status, _, resp := davCall(t, "PUT", collection+name+".ics", email, appPassword, body,
			map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
		return status, resp
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status, resp := put("invalid", tc.body)
			assert.Equal(t, http.StatusConflict, status, "must be a precondition error: %s", string(resp))
			assert.True(t, strings.Contains(string(resp), tc.precondition),
				"body should name the failed precondition %s: %s", tc.precondition, string(resp))
		})
	}

	t.Run("nothing was stored", func(t *testing.T) {
		status, _, _ := davCall(t, "GET", collection+"invalid.ics", email, appPassword, "", nil)
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a valid object is still accepted", func(t *testing.T) {
		status, resp := put("ok", wrap(vevent("uid-ok")))
		assert.Contains(t, []int{http.StatusCreated, http.StatusNoContent}, status, string(resp))
	})
}
