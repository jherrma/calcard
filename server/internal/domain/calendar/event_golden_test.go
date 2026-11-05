package calendar

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// goldenSeries pins the output of ExpandRecurringEvent byte for byte across the
// iterator refactor (#169 story 04). Regenerate only on purpose:
// UPDATE_GOLDEN=1 go test ./internal/domain/calendar -run TestExpandRecurringEventGolden
var goldenSeries = []struct {
	name       string
	ical       string
	start, end time.Time
}{
	{"weekly_exdate", `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:g1
DTSTART:20261102T090000Z
DTEND:20261102T100000Z
RRULE:FREQ=WEEKLY;COUNT=8
EXDATE:20261116T090000Z
SUMMARY:Weekly
END:VEVENT
END:VCALENDAR`, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)},
	{"daily_override_and_stray", `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:g2
DTSTART:20261102T090000Z
DTEND:20261102T093000Z
RRULE:FREQ=DAILY;COUNT=6
SUMMARY:Daily
LOCATION:Room 1
END:VEVENT
BEGIN:VEVENT
UID:g2
RECURRENCE-ID:20261104T090000Z
DTSTART:20261104T110000Z
DTEND:20261104T120000Z
SUMMARY:Moved
END:VEVENT
BEGIN:VEVENT
UID:g2
RECURRENCE-ID:20261120T090000Z
DTSTART:20261105T150000Z
DTEND:20261105T160000Z
SUMMARY:Stray
END:VEVENT
END:VCALENDAR`, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)},
	{"all_day", `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:g3
DTSTART;VALUE=DATE:20261103
DTEND;VALUE=DATE:20261104
RRULE:FREQ=WEEKLY;COUNT=5
EXDATE;VALUE=DATE:20261110
SUMMARY:All day
END:VEVENT
BEGIN:VEVENT
UID:g3
RECURRENCE-ID;VALUE=DATE:20261117
DTSTART;VALUE=DATE:20261118
DTEND;VALUE=DATE:20261119
SUMMARY:All day moved
END:VEVENT
END:VCALENDAR`, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)},
	{"tzid_across_dst", `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:g4
DTSTART;TZID=Europe/Berlin:20261022T090000
DTEND;TZID=Europe/Berlin:20261022T100000
RRULE:FREQ=DAILY;COUNT=10
SUMMARY:Berlin
END:VEVENT
BEGIN:VEVENT
UID:g4
RECURRENCE-ID;TZID=Europe/Berlin:20261026T090000
DTSTART;TZID=Europe/Berlin:20261026T130000
DTEND;TZID=Europe/Berlin:20261026T140000
SUMMARY:Berlin moved
END:VEVENT
END:VCALENDAR`, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)},
	{"floating", `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:g5
DTSTART:20261102T120000
DURATION:PT2H
RRULE:FREQ=DAILY;COUNT=5
SUMMARY:Floating
END:VEVENT
END:VCALENDAR`, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)},
	{"unbounded_late_window", `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:g6
DTSTART:20260101T080000Z
DTEND:20260101T090000Z
RRULE:FREQ=DAILY
SUMMARY:Unbounded
END:VEVENT
END:VCALENDAR`, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)},
	{"single_with_override", `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:g7
DTSTART:20261104T080000Z
DTEND:20261104T090000Z
SUMMARY:Single
END:VEVENT
BEGIN:VEVENT
UID:g7
RECURRENCE-ID:20261104T080000Z
DTSTART:20261104T100000Z
DTEND:20261104T110000Z
SUMMARY:Single moved
END:VEVENT
END:VCALENDAR`, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)},
}

func TestExpandRecurringEventGolden(t *testing.T) {
	got := map[string]json.RawMessage{}
	for _, s := range goldenSeries {
		obj := &CalendarObject{UUID: "u-" + s.name, UID: s.name, CalendarID: 3, Summary: "obj", ICalData: s.ical}
		inst, err := ExpandRecurringEvent(obj, s.start, s.end)
		require.NoError(t, err, s.name)
		b, err := json.Marshal(inst)
		require.NoError(t, err, s.name)
		got[s.name] = b
	}
	out, err := json.MarshalIndent(got, "", " ")
	require.NoError(t, err)
	const path = "testdata/expand_recurring_event_golden.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, out, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(out))
}
