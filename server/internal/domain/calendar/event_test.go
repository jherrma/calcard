package calendar

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandRecurringEvent(t *testing.T) {
	startTime := time.Date(2024, 1, 22, 9, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC)

	obj := &CalendarObject{
		UUID:      "test-uuid",
		Summary:   "Weekly Meeting",
		StartTime: &startTime,
		EndTime:   &endTime,
		ICalData: `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:test-uid
DTSTART:20240122T090000Z
DTEND:20240122T100000Z
RRULE:FREQ=WEEKLY;COUNT=3
SUMMARY:Weekly Meeting
END:VEVENT
END:VCALENDAR`,
	}

	t.Run("Expand weekly event", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC)

		instances, err := ExpandRecurringEvent(obj, start, end)
		require.NoError(t, err)
		assert.Len(t, instances, 3)

		assert.Equal(t, "20240122T090000Z", instances[0].RecurrenceID)
		assert.Equal(t, "20240129T090000Z", instances[1].RecurrenceID)
		assert.Equal(t, "20240205T090000Z", instances[2].RecurrenceID)
	})

	t.Run("Expand with time range filter", func(t *testing.T) {
		start := time.Date(2024, 1, 25, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

		instances, err := ExpandRecurringEvent(obj, start, end)
		require.NoError(t, err)
		assert.Len(t, instances, 1)
		assert.Equal(t, "20240129T090000Z", instances[0].RecurrenceID)
	})
}

// TestExpandRecurringEventTZIDMatching is the regression test for M8: a
// RECURRENCE-ID / EXDATE written with TZID= or VALUE=DATE must normalize to the
// same canonical UTC key the generated occurrences use, so overrides match
// exactly once and EXDATEs suppress the right days.
func TestExpandRecurringEventTZIDMatching(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC)

	t.Run("TZID RECURRENCE-ID override matches exactly once", func(t *testing.T) {
		obj := &CalendarObject{
			UUID:    "tz-uuid",
			Summary: "Weekly Meeting",
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
				"BEGIN:VEVENT\r\nUID:tz-uid\r\n" +
				"DTSTART;TZID=Europe/Berlin:20240122T090000\r\n" +
				"DTEND;TZID=Europe/Berlin:20240122T100000\r\n" +
				"RRULE:FREQ=WEEKLY;COUNT=3\r\nSUMMARY:Weekly Meeting\r\nEND:VEVENT\r\n" +
				"BEGIN:VEVENT\r\nUID:tz-uid\r\n" +
				"RECURRENCE-ID;TZID=Europe/Berlin:20240129T090000\r\n" +
				"DTSTART;TZID=Europe/Berlin:20240129T110000\r\n" +
				"DTEND;TZID=Europe/Berlin:20240129T120000\r\n" +
				"SUMMARY:Moved Meeting\r\nEND:VEVENT\r\n" +
				"END:VCALENDAR\r\n",
		}
		instances, err := ExpandRecurringEvent(obj, start, end)
		require.NoError(t, err)
		require.Len(t, instances, 3, "a 3-occurrence series with one override must stay 3, not duplicate the override")
		moved := 0
		for _, inst := range instances {
			if inst.Summary == "Moved Meeting" {
				moved++
			}
		}
		assert.Equal(t, 1, moved, "the TZID-qualified RECURRENCE-ID must match exactly one occurrence")
	})

	t.Run("EXDATE with TZID suppresses an occurrence", func(t *testing.T) {
		obj := &CalendarObject{
			UUID:    "ex-uuid",
			Summary: "Weekly",
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ex-uid\r\n" +
				"DTSTART;TZID=Europe/Berlin:20240122T090000\r\nDTEND;TZID=Europe/Berlin:20240122T100000\r\n" +
				"RRULE:FREQ=WEEKLY;COUNT=3\r\n" +
				"EXDATE;TZID=Europe/Berlin:20240129T090000\r\n" +
				"SUMMARY:Weekly\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		}
		instances, err := ExpandRecurringEvent(obj, start, end)
		require.NoError(t, err)
		assert.Len(t, instances, 2, "EXDATE;TZID must remove the matching occurrence")
	})

	t.Run("multi-value EXDATE;VALUE=DATE suppresses both listed days", func(t *testing.T) {
		obj := &CalendarObject{
			UUID:     "ad-uuid",
			Summary:  "Daily Standup",
			IsAllDay: true,
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ad-uid\r\n" +
				"DTSTART;VALUE=DATE:20240122\r\nDTEND;VALUE=DATE:20240123\r\n" +
				"RRULE:FREQ=DAILY;COUNT=5\r\n" +
				"EXDATE;VALUE=DATE:20240123,20240124\r\n" +
				"SUMMARY:Daily Standup\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		}
		instances, err := ExpandRecurringEvent(obj, start, end)
		require.NoError(t, err)
		assert.Len(t, instances, 3, "a 5-day series minus a 2-value EXDATE must yield 3 days")
	})
}

// TestExpandRecurringEventWindowWidening is the regression test for M11: the
// expansion window is widened by the event's duration so a long occurrence
// that started before the window but is still running inside it is produced,
// while occurrences that don't actually overlap (incl. zero-duration at the
// exact window start) are filtered out.
func TestExpandRecurringEventWindowWidening(t *testing.T) {
	t.Run("multi-day occurrence visible when window starts mid-occurrence", func(t *testing.T) {
		obj := &CalendarObject{
			UUID:    "wide-uuid",
			Summary: "Conference",
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:wide-uid\r\n" +
				"DTSTART:20240101T000000Z\r\nDTEND:20240104T000000Z\r\n" + // 3-day event
				"RRULE:FREQ=WEEKLY;COUNT=3\r\nSUMMARY:Conference\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		}
		// Window sits entirely inside the first occurrence (Jan 1–4), on day 2.
		start := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
		end := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
		instances, err := ExpandRecurringEvent(obj, start, end)
		require.NoError(t, err)
		require.Len(t, instances, 1, "a multi-day occurrence must be visible when the window opens mid-occurrence")
		assert.Equal(t, "20240101T000000Z", instances[0].RecurrenceID)
	})

	t.Run("zero-duration occurrence exactly at window start is excluded", func(t *testing.T) {
		obj := &CalendarObject{
			UUID:    "zero-uuid",
			Summary: "Ping",
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:zero-uid\r\n" +
				"DTSTART:20240102T090000Z\r\nDTEND:20240102T090000Z\r\n" + // zero duration
				"RRULE:FREQ=DAILY;COUNT=3\r\nSUMMARY:Ping\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		}
		// Window opens exactly at the Jan 2 occurrence's instant.
		start := time.Date(2024, 1, 2, 9, 0, 0, 0, time.UTC)
		end := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
		instances, err := ExpandRecurringEvent(obj, start, end)
		require.NoError(t, err)
		assert.Len(t, instances, 0, "a zero-duration occurrence exactly at the window start must be excluded")
	})
}
