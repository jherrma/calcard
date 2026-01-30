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
