package calendar_test

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vcal wraps component lines in a VCALENDAR.
func vcal(lines ...string) string {
	return strings.Join(append(append([]string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//test//EN"}, lines...),
		"END:VCALENDAR"), "\r\n") + "\r\n"
}

func vtodo(props ...string) []string {
	return append(append([]string{"BEGIN:VTODO", "UID:t@x", "DTSTAMP:20300101T000000Z", "SUMMARY:Task"}, props...), "END:VTODO")
}

func timePtr(t time.Time) *time.Time { return &t }

// TestTaskDetails pins the task fields that have no denormalized column (#170
// story 08): DTSTART with its value type, and the recurrence. They are read
// from the master VTODO; a recurring task is never expanded.
func TestTaskDetails(t *testing.T) {
	cases := []struct {
		name string
		data string
		want calendar.TaskDetails
	}{
		{"no DTSTART, no recurrence", vcal(vtodo("DUE:20301105T160000Z")...), calendar.TaskDetails{}},
		{"UTC DTSTART", vcal(vtodo("DTSTART:20301105T090000Z")...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 11, 5, 9, 0, 0, 0, time.UTC))}},
		{"DTSTART with TZID is converted to UTC", vcal(vtodo("DTSTART;TZID=Europe/Berlin:20300705T090000")...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 7, 5, 7, 0, 0, 0, time.UTC))}},
		{"floating DTSTART is read as UTC", vcal(vtodo("DTSTART:20301105T090000")...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 11, 5, 9, 0, 0, 0, time.UTC))}},
		{"date-only DTSTART", vcal(vtodo("DTSTART;VALUE=DATE:20301105")...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 11, 5, 0, 0, 0, 0, time.UTC)), StartAllDay: true}},
		{"malformed DTSTART", vcal(vtodo("DTSTART:yesterday")...), calendar.TaskDetails{}},
		{"RRULE without DTSTART (Tasks.org)", vcal(vtodo("DUE:20301105T160000Z", "RRULE:FREQ=WEEKLY;BYDAY=MO")...),
			calendar.TaskDetails{RRule: "FREQ=WEEKLY;BYDAY=MO", Recurring: true}},
		{"RRULE with DTSTART", vcal(vtodo("DTSTART;VALUE=DATE:20301105", "DUE;VALUE=DATE:20301106", "RRULE:FREQ=DAILY;COUNT=3")...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 11, 5, 0, 0, 0, 0, time.UTC)), StartAllDay: true,
				RRule: "FREQ=DAILY;COUNT=3", Recurring: true}},
		{"RDATE only", vcal(vtodo("DTSTART:20301105T090000Z", "RDATE:20301112T090000Z")...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 11, 5, 9, 0, 0, 0, time.UTC)), Recurring: true}},
		{"unparseable RRULE still recurs", vcal(vtodo("RRULE:FREQ=SOMETIMES")...),
			calendar.TaskDetails{RRule: "FREQ=SOMETIMES", Recurring: true}},
		{"master is read, not the override in front of it", vcal(append(
			vtodo("RECURRENCE-ID:20301112T090000Z", "DTSTART:20301112T100000Z"),
			vtodo("DTSTART:20301105T090000Z", "RRULE:FREQ=WEEKLY")...)...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 11, 5, 9, 0, 0, 0, time.UTC)), RRule: "FREQ=WEEKLY", Recurring: true}},
		{"only overrides: the first one", vcal(vtodo("RECURRENCE-ID:20301112T090000Z", "DTSTART:20301112T100000Z")...),
			calendar.TaskDetails{Start: timePtr(time.Date(2030, 11, 12, 10, 0, 0, 0, time.UTC))}},
		{"an event is not a task", vcal("BEGIN:VEVENT", "UID:e@x", "DTSTAMP:20300101T000000Z",
			"DTSTART:20301105T090000Z", "RRULE:FREQ=DAILY", "END:VEVENT"), calendar.TaskDetails{}},
		{"undecodable data", "BEGIN:VCALENDAR\r\nnot ical", calendar.TaskDetails{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (&calendar.CalendarObject{ICalData: tc.data}).TaskDetails()
			assert.Equal(t, tc.want.StartAllDay, got.StartAllDay, "StartAllDay")
			assert.Equal(t, tc.want.RRule, got.RRule, "RRule")
			assert.Equal(t, tc.want.Recurring, got.Recurring, "Recurring")
			if tc.want.Start == nil {
				assert.Nil(t, got.Start, "Start")
				return
			}
			require.NotNil(t, got.Start, "Start")
			assert.True(t, tc.want.Start.Equal(*got.Start), "Start = %s, want %s", got.Start, tc.want.Start)
			assert.Equal(t, time.UTC, got.Start.Location(), "Start must be in UTC")
		})
	}
}

// TestTaskMaster: the VTODO a task is read from and edited in.
func TestTaskMaster(t *testing.T) {
	decode := func(data string) *ical.Calendar {
		cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
		require.NoError(t, err)
		return cal
	}
	summary := func(c *ical.Component) string {
		require.NotNil(t, c)
		return c.Props.Get(ical.PropSummary).Value
	}
	named := func(name string, props ...string) []string {
		return append([]string{"BEGIN:VTODO", "UID:t@x", "DTSTAMP:20300101T000000Z", "SUMMARY:" + name}, append(props, "END:VTODO")...)
	}

	assert.Equal(t, "master", summary(calendar.TaskMaster(decode(vcal(append(
		named("override", "RECURRENCE-ID:20301112T090000Z"), named("master")...)...)))))
	assert.Equal(t, "first", summary(calendar.TaskMaster(decode(vcal(append(
		named("first", "RECURRENCE-ID:20301112T090000Z"), named("second", "RECURRENCE-ID:20301119T090000Z")...)...)))))
	assert.Nil(t, calendar.TaskMaster(decode(vcal("BEGIN:VEVENT", "UID:e@x", "DTSTAMP:20300101T000000Z", "END:VEVENT"))),
		"no VTODO, no master")
}

// TestCalendarObjectIsTask mirrors TestCalendarObjectIsEvent.
func TestCalendarObjectIsTask(t *testing.T) {
	assert.True(t, (&calendar.CalendarObject{ComponentType: calendar.ComponentToDo}).IsTask())
	for _, ct := range []string{calendar.ComponentEvent, "VJOURNAL", "", "vtodo"} {
		assert.False(t, (&calendar.CalendarObject{ComponentType: ct}).IsTask(), ct)
	}
	var nilObj *calendar.CalendarObject
	assert.False(t, nilObj.IsTask())
}

// TestTaskDoneStatuses pins the one cancelled rule: done is COMPLETED or
// CANCELLED, and no caller can change it for the others.
func TestTaskDoneStatuses(t *testing.T) {
	done := calendar.TaskDoneStatuses()
	assert.Equal(t, []string{calendar.TaskStatusCompleted, calendar.TaskStatusCancelled}, done)
	done[0] = "MUTATED"
	assert.Equal(t, calendar.TaskStatusCompleted, calendar.TaskDoneStatuses()[0])
}

// TestTaskDayOf pins "now" and "today" of the task counts (#170 story 13):
// as_of's own UTC offset decides the date, never UTC's date and never the
// server's zone; every field is UTC; the day of a fixed offset is 24 hours.
func TestTaskDayOf(t *testing.T) {
	utc := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return v.UTC()
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)

	for _, tc := range []struct {
		name                            string
		asOf                            time.Time
		now, start, end, date, nextDate string
	}{
		{"afternoon at +01:00", time.Date(2026, 11, 3, 15, 0, 0, 0, time.FixedZone("", 3600)),
			"2026-11-03T14:00:00Z", "2026-11-02T23:00:00Z", "2026-11-03T23:00:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"just after midnight at +01:00: the UTC date is still yesterday", time.Date(2026, 11, 3, 0, 30, 0, 0, time.FixedZone("", 3600)),
			"2026-11-02T23:30:00Z", "2026-11-02T23:00:00Z", "2026-11-03T23:00:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"late evening at -08:00: the UTC date is already tomorrow", time.Date(2026, 11, 3, 23, 30, 0, 0, time.FixedZone("", -8*3600)),
			"2026-11-04T07:30:00Z", "2026-11-03T08:00:00Z", "2026-11-04T08:00:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"UTC midnight", time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
			"2026-11-03T00:00:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"a second before midnight at +05:45", time.Date(2026, 11, 3, 23, 59, 59, 0, time.FixedZone("", 5*3600+45*60)),
			"2026-11-03T18:14:59Z", "2026-11-02T18:15:00Z", "2026-11-03T18:15:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"+14:00", time.Date(2026, 11, 3, 1, 0, 0, 0, time.FixedZone("", 14*3600)),
			"2026-11-02T11:00:00Z", "2026-11-02T10:00:00Z", "2026-11-03T10:00:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z"},
		{"new year's eve at -05:00", time.Date(2026, 12, 31, 23, 59, 59, 0, time.FixedZone("", -5*3600)),
			"2027-01-01T04:59:59Z", "2026-12-31T05:00:00Z", "2027-01-01T05:00:00Z", "2026-12-31T00:00:00Z", "2027-01-01T00:00:00Z"},
		{"leap day", time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC),
			"2028-02-29T12:00:00Z", "2028-02-29T00:00:00Z", "2028-03-01T00:00:00Z", "2028-02-29T00:00:00Z", "2028-03-01T00:00:00Z"},
		// A zone with DST (time.Parse returns time.Local when the offset
		// matches the server's zone): the day is read in the offset the
		// instant carries (+02:00 after the switch), 24 hours long, not the
		// zone's 23-hour 29 March.
		{"DST switch day in a zone location", time.Date(2026, 3, 29, 15, 0, 0, 0, berlin),
			"2026-03-29T13:00:00Z", "2026-03-28T22:00:00Z", "2026-03-29T22:00:00Z", "2026-03-29T00:00:00Z", "2026-03-30T00:00:00Z"},
		{"fractional seconds kept", time.Date(2026, 11, 3, 15, 0, 0, 500_000_000, time.FixedZone("", 3600)),
			"2026-11-03T14:00:00.5Z", "2026-11-02T23:00:00Z", "2026-11-03T23:00:00Z", "2026-11-03T00:00:00Z", "2026-11-04T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			day := calendar.TaskDayOf(tc.asOf)
			assert.Equal(t, utc(tc.now), day.Now, "Now")
			assert.Equal(t, utc(tc.start), day.Start, "Start")
			assert.Equal(t, utc(tc.end), day.End, "End")
			assert.Equal(t, utc(tc.date), day.Date, "Date")
			assert.Equal(t, utc(tc.nextDate), day.NextDate, "NextDate")
			for name, v := range map[string]time.Time{"Now": day.Now, "Start": day.Start, "End": day.End, "Date": day.Date, "NextDate": day.NextDate} {
				assert.Equal(t, time.UTC, v.Location(), name)
			}
			assert.Equal(t, 24*time.Hour, day.End.Sub(day.Start))
			assert.True(t, !day.Now.Before(day.Start) && day.Now.Before(day.End), "as_of lies in its own day")
		})
	}

	// The same instant in two offsets is two different dates.
	instant := time.Date(2026, 11, 4, 4, 0, 0, 0, time.UTC)
	west := calendar.TaskDayOf(instant.In(time.FixedZone("", -8*3600)))
	east := calendar.TaskDayOf(instant.In(time.FixedZone("", 3600)))
	assert.Equal(t, west.Now, east.Now)
	assert.Equal(t, utc("2026-11-03T00:00:00Z"), west.Date)
	assert.Equal(t, utc("2026-11-04T00:00:00Z"), east.Date)
}
