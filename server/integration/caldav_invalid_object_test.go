//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A calendar object resource may hold only one UID and one component type
// (RFC 4791 §4.1). A PUT that breaks that is the client's mistake and must be
// answered with a 4xx precondition error, not a 500.
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

	cases := map[string]string{
		"two different UIDs":    wrap(vevent("uid-one") + vevent("uid-two")),
		"mixed component types": wrap(vevent("one") + vtodo),
	}
	put := func(name, body string) (int, []byte) {
		status, _, resp := davCall(t, "PUT", collection+name+".ics", email, appPassword, body,
			map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
		return status, resp
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			status, resp := put("invalid", body)
			assert.GreaterOrEqual(t, status, 400, "must be a client error: %s", string(resp))
			assert.Less(t, status, 500, "must not be a server error: %s", string(resp))
			assert.True(t, strings.Contains(string(resp), "valid-calendar-object-resource"),
				"body should name the failed precondition: %s", string(resp))
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
