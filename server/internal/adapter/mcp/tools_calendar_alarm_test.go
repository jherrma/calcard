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

// #169 story 13: MCP update_event and update_task go through the same use cases
// as REST, so an all-day date move shifts the absolute triggers (the harness has
// no owner-zone resolver: whole UTC days; the zone and DST are the integration
// suite's job). A relative trigger stays as it is.
func TestUpdateEventAllDayMoveShiftsAbsoluteTrigger(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")
	obj := &calendar.CalendarObject{
		UUID: "allday-uuid", CalendarID: cal.ID, Path: "allday.ics", UID: "allday", ETag: calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\nUID:allday\r\n" +
			"DTSTAMP:20261101T000000Z\r\nDTSTART;VALUE=DATE:20261105\r\nDTEND;VALUE=DATE:20261106\r\nSUMMARY:Day\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:PT9H\r\nEND:VALARM\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261104T080000Z\r\nEND:VALARM\r\n" +
			"END:VEVENT\r\nEND:VCALENDAR\r\n",
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.NoError(t, e.calRepo.CreateCalendarObject(e.ctx(), obj))

	_, isErr := e.call(alice.ID, "update_event", map[string]interface{}{
		"event_id": obj.UUID, "start": "2026-11-12T00:00:00Z", "end": "2026-11-13T00:00:00Z",
	})
	require.False(t, isErr)
	stored, err := e.calRepo.GetCalendarObjectByUUID(e.ctx(), obj.UUID)
	require.NoError(t, err)
	assert.Contains(t, stored.ICalData, "DTSTART;VALUE=DATE:20261112")
	assert.Contains(t, stored.ICalData, "TRIGGER;VALUE=DATE-TIME:20261111T080000Z")
	assert.Contains(t, stored.ICalData, "TRIGGER:PT9H")
	assert.NotEqual(t, obj.ETag, stored.ETag)

	// A title-only update leaves the trigger where it is.
	_, isErr = e.call(alice.ID, "update_event", map[string]interface{}{"event_id": obj.UUID, "title": "Renamed"})
	require.False(t, isErr)
	stored, _ = e.calRepo.GetCalendarObjectByUUID(e.ctx(), obj.UUID)
	assert.Contains(t, stored.ICalData, "TRIGGER;VALUE=DATE-TIME:20261111T080000Z")
}

func TestUpdateTaskAllDayDueMoveShiftsAbsoluteTrigger(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	list := e.newCalendarWithSet(alice.ID, "Chores", "VTODO")
	obj := &calendar.CalendarObject{
		UUID: "todo-uuid", CalendarID: list.ID, Path: "todo.ics", UID: "todo", ETag: calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\nUID:todo\r\n" +
			"DTSTAMP:20261101T000000Z\r\nDUE;VALUE=DATE:20261105\r\nSUMMARY:Pay\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:PT9H\r\nEND:VALARM\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261104T080000Z\r\nEND:VALARM\r\n" +
			"END:VTODO\r\nEND:VCALENDAR\r\n",
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.NoError(t, e.calRepo.CreateCalendarObject(e.ctx(), obj))

	e.mustTask(alice.ID, "update_task", map[string]interface{}{"task_id": obj.UUID, "due": "2026-11-12"})
	stored, err := e.calRepo.GetCalendarObjectByUUID(e.ctx(), obj.UUID)
	require.NoError(t, err)
	assert.Contains(t, stored.ICalData, "DUE;VALUE=DATE:20261112")
	assert.Contains(t, stored.ICalData, "TRIGGER;VALUE=DATE-TIME:20261111T080000Z")
	assert.Contains(t, stored.ICalData, "TRIGGER;RELATED=END:PT9H")
}
