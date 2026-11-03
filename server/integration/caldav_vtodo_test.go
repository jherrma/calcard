//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalDAVTasksStayOutOfEventReads is the end-to-end regression test for
// #170. A DAV client (Apple Reminders, Tasks.org) writes a VTODO into a
// calendar collection; it shares calendar_objects with the events, and the
// REST event list and the unified search used to return it as an event. The
// task carries a DTSTART inside the listing window and matches the search
// text, so nothing but the component type can keep it out. CalDAV itself must
// keep serving the task: GET, a Depth 1 PROPFIND, a VTODO calendar-query and
// a sync-collection REPORT all still see it.
func TestCalDAVTasksStayOutOfEventReads(t *testing.T) {
	email := "dav-vtodo-170@example.test"
	password := "vtodoSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Vtodo User")
	_, appPass := createAppPassword(t, token, "vtodo-test")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	start := time.Date(2034, 4, 10, 10, 0, 0, 0, time.UTC)
	putEvent(t, collection, email, appPass, "vtodo170-event", "Harbour inspection", start)

	task := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Integration Tests//EN\r\n" +
		"BEGIN:VTODO\r\nUID:vtodo170-task\r\nDTSTAMP:20340101T000000Z\r\n" +
		"DTSTART:20340410T090000Z\r\nDUE:20340410T170000Z\r\n" +
		"SUMMARY:Harbour inspection paperwork\r\nSTATUS:NEEDS-ACTION\r\n" +
		"END:VTODO\r\nEND:VCALENDAR\r\n"
	taskPath := collection + "vtodo170-task.ics"
	status, _, body := davCall(t, "PUT", taskPath, email, appPass, task, map[string]string{
		"Content-Type": "text/calendar; charset=utf-8",
	})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT VTODO: %s", string(body))

	t.Run("REST event list leaves the task out", func(t *testing.T) {
		window := fmt.Sprintf("?start=%s&end=%s",
			start.Add(-24*time.Hour).Format(time.RFC3339), start.Add(24*time.Hour).Format(time.RFC3339))
		for _, expand := range []string{"true", "false"} {
			got := listEvents(t, token, personal.UUID, window+"&expand="+expand)
			uids := make([]string, 0, len(got))
			for _, ev := range got {
				uids = append(uids, ev.UID)
			}
			assert.Equalf(t, []string{"vtodo170-event"}, uids, "expand=%s", expand)
		}
	})

	t.Run("unified search leaves the task out", func(t *testing.T) {
		code, resp := search(t, token, url.Values{"q": {"harbour inspection"}, "types": {"events"}}.Encode())
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, []string{"Harbour inspection"}, eventTitles(resp))
	})

	t.Run("CalDAV still serves the task", func(t *testing.T) {
		status, _, body := davCall(t, "GET", taskPath, email, appPass, "", nil)
		require.Equalf(t, http.StatusOK, status, "GET VTODO: %s", string(body))
		assert.Contains(t, string(body), "BEGIN:VTODO")
		assert.Contains(t, string(body), "UID:vtodo170-task")

		status, _, body = davCall(t, "PROPFIND", collection, email, appPass, propfindCalendarBody, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "PROPFIND: %s", string(body))
		assert.Contains(t, string(body), "vtodo170-task.ics", "Depth 1 PROPFIND must list the task")
		assert.Contains(t, string(body), "vtodo170-event.ics")

		query := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:getetag/>
    <C:calendar-data/>
  </D:prop>
  <C:filter>
    <C:comp-filter name="VCALENDAR">
      <C:comp-filter name="VTODO"/>
    </C:comp-filter>
  </C:filter>
</C:calendar-query>`
		status, _, body = davCall(t, "REPORT", collection, email, appPass, query, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "VTODO calendar-query: %s", string(body))
		assert.Contains(t, string(body), "vtodo170-task", "a VTODO calendar-query must return the task")

		status, _, body = davCall(t, "REPORT", collection, email, appPass, syncCollectionBody, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "sync-collection: %s", string(body))
		assert.Contains(t, string(body), "vtodo170-task.ics", "initial sync must deliver the task")
		assert.Contains(t, string(body), "vtodo170-event.ics")
	})
}
