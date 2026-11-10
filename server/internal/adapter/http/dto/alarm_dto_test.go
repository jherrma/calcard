package dto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	alarmuc "github.com/jherrma/caldav-server/internal/usecase/alarm"
)

// A daily series whose 3 Nov occurrence is an override carrying an absolute
// alarm: the expansion leaves Start zero for it, and the response must name the
// occurrence (its RECURRENCE-ID), not the series' first start.
func TestUpcomingAlarmAbsoluteOnLaterOccurrence(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:u@x\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\nRRULE:FREQ=DAILY;COUNT=5\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:u@x\r\nDTSTAMP:20261101T000000Z\r\nRECURRENCE-ID:20261103T090000Z\r\nDTSTART:20261103T090000Z\r\nDTEND:20261103T100000Z\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:x\r\nTRIGGER;VALUE=DATE-TIME:20261103T084500Z\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	first := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	firstEnd := first.Add(time.Hour)
	obj := &calendar.CalendarObject{UUID: "o1", UID: "u@x", CalendarID: 1, ComponentType: calendar.ComponentEvent,
		ICalData: ics, StartTime: &first, EndTime: &firstEnd}

	got, _, err := calendar.ExpandAlarms(obj, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.False(t, got[0].Start.IsZero(), "an override's absolute alarm carries the override's anchors")

	r := newUpcomingAlarmResponse(alarmuc.Upcoming{AlarmInstance: got[0]})
	require.NotNil(t, r.RecurrenceID)
	require.NotNil(t, r.Start)
	assert.Equal(t, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC), *r.Start)
	require.NotNil(t, r.End)
	assert.Equal(t, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), *r.End)
	assert.Nil(t, r.StartDate)

	// The expansion sets the anchors of this override; clear Start/End by hand to
	// exercise the fallback: the RECURRENCE-ID occurrence.
	bare := got[0]
	bare.Start, bare.End = time.Time{}, time.Time{}
	r = newUpcomingAlarmResponse(alarmuc.Upcoming{AlarmInstance: bare})
	require.NotNil(t, r.Start)
	assert.Equal(t, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC), *r.Start)
	require.NotNil(t, r.End)
	assert.Equal(t, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), *r.End)

	// All-day override: the date of the occurrence.
	allDay := *obj
	allDay.IsAllDay = true
	inst := bare
	inst.Object = &allDay
	inst.RecurrenceID = "20261103T000000Z"
	r = newUpcomingAlarmResponse(alarmuc.Upcoming{AlarmInstance: inst})
	require.NotNil(t, r.StartDate)
	assert.Equal(t, "2026-11-03", *r.StartDate)

	// Not a series alarm (rid ""): the object's own start.
	inst = bare
	inst.RecurrenceID = ""
	r = newUpcomingAlarmResponse(alarmuc.Upcoming{AlarmInstance: inst})
	require.NotNil(t, r.Start)
	assert.Equal(t, first, *r.Start)
	assert.Nil(t, r.RecurrenceID)
}
