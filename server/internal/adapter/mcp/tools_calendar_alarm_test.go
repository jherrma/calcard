package mcp

import (
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #169 story 09: update_event with scope=this on a DAV-written series keeps the
// series' relative VALARM on the new override and leaves the absolute one on
// the master.
func TestUpdateEventScopeThisKeepsRelativeAlarm(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")

	obj := &calendar.CalendarObject{
		UUID: "series-uuid", CalendarID: cal.ID, Path: "series.ics", UID: "series", ETag: calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\nUID:series\r\n" +
			"DTSTAMP:20260301T000000Z\r\nDTSTART:20260302T090000Z\r\nDTEND:20260302T100000Z\r\n" +
			"RRULE:FREQ=WEEKLY;COUNT=5\r\nSUMMARY:Weekly\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:r\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:a\r\nTRIGGER;VALUE=DATE-TIME:20260303T080000Z\r\nEND:VALARM\r\n" +
			"END:VEVENT\r\nEND:VCALENDAR\r\n",
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.NoError(t, e.calRepo.CreateCalendarObject(e.ctx(), obj))

	_, isErr := e.call(alice.ID, "update_event", map[string]interface{}{
		"event_id": obj.UUID, "title": "Moved topic", "scope": "this", "recurrence_id": "20260309T090000Z",
	})
	require.False(t, isErr)

	stored, err := e.calRepo.GetCalendarObjectByUUID(e.ctx(), obj.UUID)
	require.NoError(t, err)
	ics := strings.ReplaceAll(stored.ICalData, "\r\n ", "")
	assert.Equal(t, 3, strings.Count(ics, "BEGIN:VALARM"), "master keeps two, override gets the relative one")
	assert.Equal(t, 2, strings.Count(ics, "TRIGGER:-PT15M"))
	assert.Equal(t, 1, strings.Count(ics, "VALUE=DATE-TIME"), "absolute alarm is not copied")
}
