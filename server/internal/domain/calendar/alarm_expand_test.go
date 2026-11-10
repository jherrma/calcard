package calendar

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tm(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.UTC)
}

func evObj(componentType string, lines ...string) *CalendarObject {
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCALENDAR\r\n"
	return &CalendarObject{UUID: "u1", UID: "uid@example", CalendarID: 1, ComponentType: componentType, ICalData: body}
}

func rids(in []AlarmInstance) []string {
	var out []string
	for _, i := range in {
		out = append(out, i.RecurrenceID)
	}
	return out
}

func triggers(in []AlarmInstance) []string {
	var out []string
	for _, i := range in {
		out = append(out, i.TriggerAt.Format("0102T1504"))
	}
	return out
}

const alarm15 = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Soon\r\nTRIGGER:-PT15M\r\nEND:VALARM"

func TestAlarmKeyVector(t *testing.T) {
	trig := tm(2026, 11, 4, 8, 45, 0)
	k := AlarmKey("uid@example", "", trig)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), k)
	// printf 'calcard-alarm-v1|20261104T084500Z||uid@example' | sha256sum | cut -c1-32
	assert.Equal(t, "2cacbf100d88173e22845a631c4b862d", k)
	// ... |20261104T080000Z|uid@example
	assert.Equal(t, "d0586173fe62b182d0ed66554e761913", AlarmKey("uid@example", "20261104T080000Z", trig))
}

func TestAlarmKeyInputs(t *testing.T) {
	trig := tm(2026, 11, 4, 8, 45, 0)
	base := AlarmKey("u", "r", trig)
	assert.NotEqual(t, base, AlarmKey("u2", "r", trig))
	assert.NotEqual(t, base, AlarmKey("u", "r2", trig))
	assert.NotEqual(t, base, AlarmKey("u", "", trig))
	assert.NotEqual(t, base, AlarmKey("u", "r", trig.Add(time.Second)))
	// sub-second precision and the zone do not matter
	assert.Equal(t, base, AlarmKey("u", "r", trig.Add(900*time.Millisecond)))
	assert.Equal(t, base, AlarmKey("u", "r", trig.In(berlin(t))))
}

func TestExpandAlarmsWeeklyEXDATE(t *testing.T) {
	obj := evObj("VEVENT",
		"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20260101T000000Z",
		"DTSTART:20261102T090000Z", "DTEND:20261102T100000Z", "RRULE:FREQ=WEEKLY;COUNT=6",
		"EXDATE:20261116T090000Z", alarm15, "END:VEVENT")
	got, capped, err := ExpandAlarms(obj, tm(2026, 11, 1, 0, 0, 0), tm(2026, 12, 6, 0, 0, 0), nil)
	require.NoError(t, err)
	assert.False(t, capped)
	assert.Equal(t, []string{"20261102T090000Z", "20261109T090000Z", "20261123T090000Z", "20261130T090000Z"}, rids(got))
	assert.Equal(t, []string{"1102T0845", "1109T0845", "1123T0845", "1130T0845"}, triggers(got))
	seen := map[string]bool{}
	for _, g := range got {
		assert.Equal(t, AlarmKey("uid@example", g.RecurrenceID, g.TriggerAt), g.Key)
		seen[g.Key] = true
		assert.Equal(t, "Soon", g.Description)
		assert.Equal(t, "DISPLAY", g.Action)
	}
	assert.Len(t, seen, 4)

	// the trigger window is exact, not the widened one
	got, _, err = ExpandAlarms(obj, tm(2026, 11, 9, 8, 45, 0), tm(2026, 11, 9, 8, 45, 0), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"20261109T090000Z"}, rids(got))
	got, _, _ = ExpandAlarms(obj, tm(2026, 11, 9, 8, 45, 1), tm(2026, 11, 9, 9, 0, 0), nil)
	assert.Empty(t, got)
}

func TestExpandAlarmsOverrides(t *testing.T) {
	obj := evObj("VEVENT",
		"BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261102T090000Z", "DTEND:20261102T100000Z",
		"RRULE:FREQ=DAILY;COUNT=5", alarm15, "END:VEVENT",
		// override with its own alarm (-PT5M) on the 3rd
		"BEGIN:VEVENT", "UID:uid@example", "RECURRENCE-ID:20261103T090000Z", "DTSTART:20261103T120000Z", "DTEND:20261103T130000Z",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT5M", "END:VALARM", "END:VEVENT",
		// override without VALARM on the 4th
		"BEGIN:VEVENT", "UID:uid@example", "RECURRENCE-ID:20261104T090000Z", "DTSTART:20261104T090000Z", "DTEND:20261104T100000Z", "END:VEVENT",
		// stray override with its own alarm
		"BEGIN:VEVENT", "UID:uid@example", "RECURRENCE-ID:20261201T090000Z", "DTSTART:20261106T150000Z", "DTEND:20261106T160000Z",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT1H", "END:VALARM", "END:VEVENT")
	got, _, err := ExpandAlarms(obj, tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 10, 0, 0, 0), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"20261102T090000Z", "20261103T090000Z", "20261105T090000Z", "20261106T090000Z", "20261201T090000Z"}, rids(got))
	assert.Equal(t, []string{"1102T0845", "1103T1155", "1105T0845", "1106T0845", "1106T1400"}, triggers(got))
}

func TestExpandAlarmsAbsolute(t *testing.T) {
	abs := "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261104T070000Z\r\nEND:VALARM"
	obj := evObj("VEVENT",
		"BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261102T090000Z", "DTEND:20261102T100000Z",
		"RRULE:FREQ=DAILY;COUNT=5", abs, "END:VEVENT",
		"BEGIN:VEVENT", "UID:uid@example", "RECURRENCE-ID:20261105T090000Z", "DTSTART:20261105T090000Z", "DTEND:20261105T100000Z",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:20261105T060000Z", "END:VALARM", "END:VEVENT")
	got, _, err := ExpandAlarms(obj, tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 10, 0, 0, 0), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"", "20261105T090000Z"}, rids(got))
	// an override's absolute alarm is found even when its occurrence is far from the window
	got, _, _ = ExpandAlarms(obj, tm(2026, 11, 5, 5, 0, 0), tm(2026, 11, 5, 7, 0, 0), nil)
	assert.Equal(t, []string{"20261105T090000Z"}, rids(got))
}

func TestExpandAlarmsFloatingOwnerZoneAndKey(t *testing.T) {
	ev := func(start string) *CalendarObject {
		return evObj("VEVENT", "BEGIN:VEVENT", "UID:uid@example", "DTSTART:"+start, "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT15M", "END:VALARM", "END:VEVENT")
	}
	from, until := tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 10, 0, 0, 0)
	a, _, err := ExpandAlarms(ev("20261104T090000"), from, until, berlin(t))
	require.NoError(t, err)
	require.Len(t, a, 1)
	assert.Equal(t, tm(2026, 11, 4, 7, 45, 0), a[0].TriggerAt)
	b, _, _ := ExpandAlarms(ev("20261104T090000"), from, until, time.UTC)
	assert.NotEqual(t, a[0].Key, b[0].Key, "owner zone changes the key of a floating alarm")
	moved, _, _ := ExpandAlarms(ev("20261104T090100"), from, until, berlin(t))
	assert.NotEqual(t, a[0].Key, moved[0].Key, "moving by a minute changes the key")
	other := ev("20261104T090000")
	other.CalendarID = 99
	c, _, _ := ExpandAlarms(other, from, until, berlin(t))
	assert.Equal(t, a[0].Key, c[0].Key, "the calendar is not part of the key")
}

func TestExpandAlarmsFloatingWindowEdges(t *testing.T) {
	// Berlin owner: 00:30 local on 2026-11-05 is 23:30Z on the 4th; the wall
	// clock (UTC) is a day later than the window edge.
	obj := evObj("VEVENT", "BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261105T003000", "RRULE:FREQ=DAILY;COUNT=3",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:PT0S", "END:VALARM", "END:VEVENT")
	got, _, err := ExpandAlarms(obj, tm(2026, 11, 4, 23, 0, 0), tm(2026, 11, 4, 23, 59, 0), berlin(t))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, tm(2026, 11, 4, 23, 30, 0), got[0].TriggerAt)
}

func TestExpandAlarmsEndRelated(t *testing.T) {
	end := func(a string) string {
		return "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:" + a + "\r\nEND:VALARM"
	}
	from, until := tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 10, 0, 0, 0)
	cases := map[string]struct {
		lines []string
		want  time.Time
	}{
		"dtend":    {[]string{"DTSTART:20261104T090000Z", "DTEND:20261104T103000Z"}, tm(2026, 11, 4, 10, 25, 0)},
		"duration": {[]string{"DTSTART:20261104T090000Z", "DURATION:PT2H"}, tm(2026, 11, 4, 10, 55, 0)},
		"date":     {[]string{"DTSTART;VALUE=DATE:20261104"}, tm(2026, 11, 4, 23, 55, 0)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			lines := append([]string{"BEGIN:VEVENT", "UID:uid@example"}, c.lines...)
			lines = append(lines, end("-PT5M"), "END:VEVENT")
			got, _, err := ExpandAlarms(evObj("VEVENT", lines...), from, until, time.UTC)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, c.want, got[0].TriggerAt)
		})
	}
}

func TestExpandAlarmsWindowWidening(t *testing.T) {
	obj := evObj("VEVENT", "BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261110T090000Z", "DTEND:20261110T100000Z",
		"RRULE:FREQ=WEEKLY;COUNT=4",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-P1W", "END:VALARM", "END:VEVENT")
	until := tm(2026, 11, 3, 12, 0, 0)
	// the 2026-11-10 occurrence starts 6 days 21 hours after until
	got, _, err := ExpandAlarms(obj, tm(2026, 11, 3, 0, 0, 0), until, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, []string{"20261110T090000Z"}, rids(got))
	assert.Equal(t, tm(2026, 11, 3, 9, 0, 0), got[0].TriggerAt)
	// a trigger a minute after until is not returned
	got, _, _ = ExpandAlarms(obj, tm(2026, 11, 2, 0, 0, 0), tm(2026, 11, 3, 8, 59, 0), time.UTC)
	assert.Empty(t, got)
}

func TestExpandAlarmsKeyStabilityAndCollapse(t *testing.T) {
	a := "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:A\r\nTRIGGER:-PT15M\r\nEND:VALARM"
	b := "BEGIN:VALARM\r\nACTION:AUDIO\r\nDESCRIPTION:B\r\nTRIGGER:-PT1H\r\nEND:VALARM"
	same := "BEGIN:VALARM\r\nACTION:AUDIO\r\nDESCRIPTION:Same\r\nTRIGGER:-PT15M\r\nEND:VALARM"
	mk := func(alarms ...string) *CalendarObject {
		lines := []string{"BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261104T090000Z"}
		lines = append(lines, alarms...)
		return evObj("VEVENT", append(lines, "END:VEVENT")...)
	}
	from, until := tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 10, 0, 0, 0)
	keys := func(o *CalendarObject) map[string]int {
		got, _, err := ExpandAlarms(o, from, until, nil)
		require.NoError(t, err)
		m := map[string]int{}
		for _, g := range got {
			m[g.TriggerAt.Format("1504")] = 0
			m[g.Key] = g.Index
		}
		return m
	}
	ab, ba, onlyA := keys(mk(a, b)), keys(mk(b, a)), keys(mk(a))
	ka := AlarmKey("uid@example", "", tm(2026, 11, 4, 8, 45, 0))
	kb := AlarmKey("uid@example", "", tm(2026, 11, 4, 8, 0, 0))
	for _, m := range []map[string]int{ab, ba, onlyA} {
		_, ok := m[ka]
		assert.True(t, ok, "alarm A key is stable")
	}
	_, ok := ab[kb]
	_, ok2 := ba[kb]
	assert.True(t, ok && ok2)
	assert.Equal(t, 0, ab[ka])
	assert.Equal(t, 1, ba[ka], "the index is informational and follows the position")

	// two VALARMs at the same instant: one instance, the lower index, its action/description
	got, _, err := ExpandAlarms(mk(b, a, same), from, until, nil)
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, g := range got {
		if g.Key == ka {
			assert.Equal(t, 1, g.Index)
			assert.Equal(t, "A", g.Description)
		}
	}
	got, _, _ = ExpandAlarms(mk(same, a), from, until, nil)
	require.Len(t, got, 1)
	assert.Equal(t, 0, got[0].Index)
	assert.Equal(t, "Same", got[0].Description)
	assert.Equal(t, "AUDIO", got[0].Action)
}

func TestExpandAlarmsTasks(t *testing.T) {
	task := func(status string, extra ...string) *CalendarObject {
		lines := []string{"BEGIN:VTODO", "UID:uid@example", "DUE:20261105T100000Z"}
		lines = append(lines, extra...)
		lines = append(lines, "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;RELATED=END:-PT30M", "END:VALARM", "END:VTODO")
		o := evObj("VTODO", lines...)
		o.Status = status
		return o
	}
	from, until := tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 10, 0, 0, 0)
	got, _, err := ExpandAlarms(task("NEEDS-ACTION"), from, until, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, tm(2026, 11, 5, 9, 30, 0), got[0].TriggerAt)
	for _, s := range TaskDoneStatuses() {
		got, _, _ = ExpandAlarms(task(s), from, until, nil)
		assert.Empty(t, got, s)
	}
	// recurring task: master's alarms once; overrides ignored
	rec := task("NEEDS-ACTION", "RRULE:FREQ=DAILY;COUNT=5", "DTSTART:20261104T100000Z")
	rec.ICalData = strings.Replace(rec.ICalData, "END:VCALENDAR", "BEGIN:VTODO\r\nUID:uid@example\r\nRECURRENCE-ID:20261106T100000Z\r\nDUE:20261106T120000Z\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:-PT1H\r\nEND:VALARM\r\nEND:VTODO\r\nEND:VCALENDAR", 1)
	got, _, _ = ExpandAlarms(rec, from, until, nil)
	require.Len(t, got, 1)
	assert.Equal(t, "", got[0].RecurrenceID)
}

func TestExpandAlarmsCancelled(t *testing.T) {
	mk := func(masterStatus string) *CalendarObject {
		return evObj("VEVENT",
			"BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261102T090000Z", "DTEND:20261102T100000Z", "RRULE:FREQ=DAILY;COUNT=3", masterStatus, alarm15, "END:VEVENT",
			"BEGIN:VEVENT", "UID:uid@example", "RECURRENCE-ID:20261103T090000Z", "DTSTART:20261103T090000Z", "DTEND:20261103T100000Z", alarm15, "END:VEVENT",
			"BEGIN:VEVENT", "UID:uid@example", "RECURRENCE-ID:20261104T090000Z", "DTSTART:20261104T090000Z", "DTEND:20261104T100000Z", "STATUS:CANCELLED", alarm15, "END:VEVENT")
	}
	from, until := tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 10, 0, 0, 0)
	got, _, err := ExpandAlarms(mk("STATUS:CANCELLED"), from, until, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"20261103T090000Z"}, rids(got))
	got, _, _ = ExpandAlarms(mk("STATUS:CONFIRMED"), from, until, nil)
	assert.Equal(t, []string{"20261102T090000Z", "20261103T090000Z"}, rids(got))
}

func TestExpandAlarmsCap(t *testing.T) {
	// every 10 minutes for a day: 144 occurrences, 4 VALARMs each = 576 > 500
	var alarms []string
	for _, off := range []string{"-PT1M", "-PT2M", "-PT3M", "-PT4M"} {
		alarms = append(alarms, "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:"+off, "END:VALARM")
	}
	lines := append([]string{"BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261104T000000Z", "DTEND:20261104T000500Z", "RRULE:FREQ=MINUTELY;INTERVAL=10;COUNT=144"}, alarms...)
	obj := evObj("VEVENT", append(lines, "END:VEVENT")...)
	got, capped, err := ExpandAlarms(obj, tm(2026, 11, 3, 0, 0, 0), tm(2026, 11, 6, 0, 0, 0), nil)
	require.NoError(t, err)
	assert.True(t, capped)
	assert.Len(t, got, AlarmInstanceCap)

	// exactly at the cap is not capped: 125 occurrences x 4 alarms
	exact := evObj("VEVENT", append(append([]string{"BEGIN:VEVENT", "UID:uid@example", "DTSTART:20261104T000000Z", "DTEND:20261104T000500Z", "RRULE:FREQ=MINUTELY;INTERVAL=10;COUNT=125"}, alarms...), "END:VEVENT")...)
	got, capped, err = ExpandAlarms(exact, tm(2026, 11, 3, 0, 0, 0), tm(2026, 11, 6, 0, 0, 0), nil)
	require.NoError(t, err)
	assert.False(t, capped)
	assert.Len(t, got, AlarmInstanceCap)
}

func TestExpandAlarmsUndecodable(t *testing.T) {
	obj := &CalendarObject{UID: "x", ComponentType: "VEVENT", ICalData: "garbage"}
	assert.NotPanics(t, func() {
		_, _, err := ExpandAlarms(obj, tm(2026, 1, 1, 0, 0, 0), tm(2026, 1, 2, 0, 0, 0), nil)
		assert.Error(t, err)
	})
	_, _, err := ExpandAlarms(nil, tm(2026, 1, 1, 0, 0, 0), tm(2026, 1, 2, 0, 0, 0), nil)
	assert.Error(t, err)
}

// An absolute alarm of an override reports the override's own start and end, as a
// relative one does: a moved override shows the moved time, a lengthened one its new end.
func TestExpandAlarmsAbsoluteOverrideAnchors(t *testing.T) {
	series := func(override ...string) *CalendarObject {
		lines := []string{
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20261101T000000Z",
			"DTSTART:20261101T090000Z", "DTEND:20261101T100000Z", "RRULE:FREQ=DAILY;COUNT=5", "END:VEVENT",
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20261101T000000Z", "RECURRENCE-ID:20261103T090000Z",
		}
		lines = append(lines, override...)
		lines = append(lines, "BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:x",
			"TRIGGER;VALUE=DATE-TIME:20261103T084500Z", "END:VALARM", "END:VEVENT")
		return evObj("VEVENT", lines...)
	}
	from, until := tm(2026, 11, 1, 0, 0, 0), tm(2026, 11, 6, 0, 0, 0)

	got, _, err := ExpandAlarms(series("DTSTART:20261103T140000Z", "DTEND:20261103T160000Z"), from, until, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "20261103T090000Z", got[0].RecurrenceID)
	assert.Equal(t, tm(2026, 11, 3, 14, 0, 0), got[0].Start)
	assert.Equal(t, tm(2026, 11, 3, 16, 0, 0), got[0].End)

	got, _, err = ExpandAlarms(series("DTSTART:20261103T090000Z", "DTEND:20261103T113000Z"), from, until, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, tm(2026, 11, 3, 9, 0, 0), got[0].Start)
	assert.Equal(t, tm(2026, 11, 3, 11, 30, 0), got[0].End)
}

// A relative and an absolute alarm of one override report the same start and end,
// also when the override omits DTEND (master duration) or DTSTART (RECURRENCE-ID time,
// the override's DTEND).
func TestExpandAlarmsAbsoluteMatchesRelativeAnchors(t *testing.T) {
	build := func(override ...string) *CalendarObject {
		lines := []string{
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20261101T000000Z",
			"DTSTART:20261101T090000Z", "DTEND:20261101T100000Z", "RRULE:FREQ=DAILY;COUNT=5", "END:VEVENT",
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20261101T000000Z", "RECURRENCE-ID:20261103T090000Z",
		}
		lines = append(lines, override...)
		lines = append(lines, "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT15M", "END:VALARM",
			"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:20261103T083000Z", "END:VALARM", "END:VEVENT")
		return evObj("VEVENT", lines...)
	}
	cases := []struct {
		name       string
		override   []string
		start, end time.Time
	}{
		{"no DTEND", []string{"DTSTART:20261103T140000Z"}, tm(2026, 11, 3, 14, 0, 0), tm(2026, 11, 3, 15, 0, 0)},
		{"no DTSTART", []string{"DTEND:20261103T120000Z"}, tm(2026, 11, 3, 9, 0, 0), tm(2026, 11, 3, 12, 0, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := ExpandAlarms(build(tc.override...), tm(2026, 11, 3, 0, 0, 0), tm(2026, 11, 4, 0, 0, 0), nil)
			require.NoError(t, err)
			require.Len(t, got, 2, "one relative, one absolute alarm")
			for _, g := range got {
				assert.Equal(t, tc.start, g.Start, "start of %s", g.TriggerAt)
				assert.Equal(t, tc.end, g.End, "end of %s", g.TriggerAt)
			}
		})
	}
}

// A stray override (its RECURRENCE-ID is EXDATE'd, so no beat of the series) with a
// DTSTART and no DTEND: the absolute alarm reports what the relative one does (the
// override's own anchors; go-ical reads a missing DTEND as DTSTART), not the master's
// duration. A stray override without DTSTART yields no relative alarm at all.
func TestExpandAlarmsStrayOverrideAnchors(t *testing.T) {
	obj := evObj("VEVENT",
		"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20261101T000000Z",
		"DTSTART:20261101T090000Z", "DTEND:20261101T100000Z", "RRULE:FREQ=DAILY;COUNT=5",
		"EXDATE:20261103T090000Z", "END:VEVENT",
		"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20261101T000000Z", "RECURRENCE-ID:20261103T090000Z",
		"DTSTART:20261103T140000Z",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT15M", "END:VALARM",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:20261103T083000Z", "END:VALARM", "END:VEVENT")
	got, _, err := ExpandAlarms(obj, tm(2026, 11, 3, 0, 0, 0), tm(2026, 11, 4, 0, 0, 0), nil)
	require.NoError(t, err)
	require.Len(t, got, 2, "one relative, one absolute alarm")
	assert.Equal(t, got[0].Start, got[1].Start)
	assert.Equal(t, got[0].End, got[1].End)
	assert.Equal(t, tm(2026, 11, 3, 14, 0, 0), got[0].Start)
	assert.Equal(t, tm(2026, 11, 3, 14, 0, 0), got[0].End, "no DTEND: the end is the start, not start + master duration")
}

// A SECONDLY master from an old DTSTART must not make the beat check walk the series:
// the call is bounded by the iteration budget. A normal series still finds its beat.
func TestExpandAlarmsOverrideBeatCheckIsBounded(t *testing.T) {
	override := func(rrule, dtstart string) *CalendarObject {
		return evObj("VEVENT",
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20200101T000000Z",
			"DTSTART:"+dtstart, "DTEND:"+dtstart, "RRULE:"+rrule, "END:VEVENT",
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20200101T000000Z", "RECURRENCE-ID:20261103T090000Z",
			"DTSTART:20261103T140000Z", "DTEND:20261103T150000Z",
			"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:20261103T083000Z", "END:VALARM", "END:VEVENT")
	}
	from, until := tm(2026, 11, 3, 0, 0, 0), tm(2026, 11, 4, 0, 0, 0)

	began := time.Now()
	got, capped, err := ExpandAlarms(override("FREQ=SECONDLY", "20200101T000000Z"), from, until, nil)
	require.NoError(t, err)
	assert.Less(t, time.Since(began), 5*time.Second)
	assert.False(t, capped)
	require.Len(t, got, 1, "the override's absolute alarm is still produced")

	got, capped, err = ExpandAlarms(override("FREQ=DAILY", "20261101T090000Z"), from, until, nil)
	require.NoError(t, err)
	assert.False(t, capped)
	require.Len(t, got, 1)
	assert.Equal(t, tm(2026, 11, 3, 14, 0, 0), got[0].Start)
	assert.Equal(t, tm(2026, 11, 3, 15, 0, 0), got[0].End)
}

// Many overrides with absolute alarms share one beat walk: 500 of them on a SECONDLY
// master from 2020 stay fast, and each override's alarm is produced.
func TestExpandAlarmsManyOverridesShareBeatBudget(t *testing.T) {
	lines := []string{
		"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20200101T000000Z",
		"DTSTART:20200101T000000Z", "DTEND:20200101T000001Z", "RRULE:FREQ=SECONDLY", "END:VEVENT",
	}
	for i := 0; i < 500; i++ {
		rid := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second).Format(RecurrenceIDLayout)
		lines = append(lines,
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20200101T000000Z", "RECURRENCE-ID:"+rid,
			"DTSTART:20261103T140000Z", "DTEND:20261103T150000Z",
			"BEGIN:VALARM", "ACTION:DISPLAY", fmt.Sprintf("TRIGGER;VALUE=DATE-TIME:20261103T0800%02dZ", i%60), "END:VALARM", "END:VEVENT")
	}
	began := time.Now()
	got, _, err := ExpandAlarms(evObj("VEVENT", lines...), tm(2026, 11, 3, 0, 0, 0), tm(2026, 11, 4, 0, 0, 0), nil)
	require.NoError(t, err)
	t.Logf("500 overrides, SECONDLY master: %s", time.Since(began))
	assert.Less(t, time.Since(began), 2*time.Second)
	assert.Len(t, got, 500, "one alarm per override")
}

// 600 beat overrides (DTSTART, no DTEND) of a DAILY master from 2025 share one beat walk
// and each reports the master's 1 h duration; a stray override would report 0.
func TestExpandAlarmsManyBeatOverridesKeepMasterDuration(t *testing.T) {
	lines := []string{
		"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20250101T000000Z",
		"DTSTART:20250101T090000Z", "DTEND:20250101T100000Z", "RRULE:FREQ=DAILY", "END:VEVENT",
	}
	first := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 600; i++ {
		d := first.AddDate(0, 0, i)
		lines = append(lines,
			"BEGIN:VEVENT", "UID:uid@example", "DTSTAMP:20250101T000000Z", "RECURRENCE-ID:"+d.Format(RecurrenceIDLayout),
			"DTSTART:"+d.Add(5*time.Hour).Format(RecurrenceIDLayout),
			"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:"+d.Add(-time.Hour).Format(RecurrenceIDLayout), "END:VALARM", "END:VEVENT")
	}
	began := time.Now()
	got, capped, err := ExpandAlarms(evObj("VEVENT", lines...), first.AddDate(0, 0, -1), first.AddDate(0, 0, 450), nil) // under the 500-instance cap; the walk still covers all 600
	require.NoError(t, err)
	t.Logf("600 beat overrides, DAILY master: %s", time.Since(began))
	assert.Less(t, time.Since(began), 2*time.Second)
	assert.False(t, capped)
	require.GreaterOrEqual(t, len(got), 440, "the alarms inside the window; all 600 overrides are beat-checked")
	require.Less(t, len(got), AlarmInstanceCap)
	for _, g := range got {
		assert.Equal(t, time.Hour, g.End.Sub(g.Start), "beat override without DTEND: master duration, %s", g.RecurrenceID)
	}
}
