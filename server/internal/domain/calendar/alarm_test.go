package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	return loc
}

// parseComp decodes the first VEVENT or VTODO of a calendar body made of the
// given component lines.
func parseComp(t *testing.T, lines ...string) *ical.Component {
	t.Helper()
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCALENDAR\r\n"
	cal, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	require.NoError(t, err)
	for _, c := range cal.Children {
		if c.Name == ical.CompEvent || c.Name == ical.CompToDo {
			return c
		}
	}
	t.Fatal("no component")
	return nil
}

func alarmOf(t *testing.T, alarmLines ...string) AlarmSpec {
	t.Helper()
	lines := append([]string{"BEGIN:VEVENT", "UID:a", "DTSTAMP:20260101T000000Z", "BEGIN:VALARM"}, alarmLines...)
	lines = append(lines, "END:VALARM", "END:VEVENT")
	specs := ParseAlarms(parseComp(t, lines...))
	require.Len(t, specs, 1)
	return specs[0]
}

func TestParseAlarmDuration(t *testing.T) {
	ok := []struct {
		in   string
		neg  bool
		days int
		secs int64
	}{
		{"PT15M", false, 0, 900},
		{"-PT15M", true, 0, 900},
		{"+PT0S", false, 0, 0},
		{"pt1h30m", false, 0, 5400},
		{"-P1D", true, 1, 0},
		{"P1W", false, 7, 0},
		{"P1W2D", false, 9, 0},
		{"-P1DT15H", true, 1, 54000},
		{"P400D", false, 400, 0},
		{"PT1H2M3S", false, 0, 3723},
	}
	for _, c := range ok {
		neg, d, s, err := parseAlarmDuration(c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, c.neg, neg, c.in)
		assert.Equal(t, c.days, d, c.in)
		assert.Equal(t, c.secs, s, c.in)
	}
	for _, in := range []string{"", "P", "-P", "PT", "P1DT", "PT1.5H", "-P15M", "PT1M1H", "P1D1W", "1D", "PT15", "P401D", "PT9999999H", "P1H", "PT1D", "P99999999999D", "PX"} {
		_, _, _, err := parseAlarmDuration(in)
		assert.Error(t, err, in)
	}
}

func TestParseAlarmsTriggerForms(t *testing.T) {
	a := alarmOf(t, "ACTION:DISPLAY", "TRIGGER:-PT15M")
	assert.True(t, a.Valid && a.Relative && a.Deliverable)
	assert.Equal(t, "START", a.Related)
	assert.Equal(t, AlarmOffset{Neg: true, Secs: 900}, a.Offset)

	a = alarmOf(t, "TRIGGER;VALUE=DURATION:-PT15M")
	assert.True(t, a.Valid && a.Relative)

	a = alarmOf(t, "TRIGGER;RELATED=end:PT0S")
	assert.True(t, a.Valid)
	assert.Equal(t, "END", a.Related)

	assert.False(t, alarmOf(t, "TRIGGER;RELATED=FOO:-PT5M").Valid)
	assert.False(t, alarmOf(t, "TRIGGER;VALUE=DATE:20261104").Valid)
	assert.False(t, alarmOf(t, "TRIGGER:-P15M").Valid)

	a = alarmOf(t, "TRIGGER;VALUE=DATE-TIME:20261104T080000Z")
	assert.True(t, a.Valid && !a.Relative && !a.Floating)
	assert.Equal(t, time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC), *a.Absolute)

	a = alarmOf(t, "TRIGGER;VALUE=DATE-TIME;TZID=Europe/Berlin:20261104T090000")
	require.True(t, a.Valid)
	assert.Equal(t, time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC), *a.Absolute)
	assert.False(t, a.Floating)

	assert.False(t, alarmOf(t, "TRIGGER;VALUE=DATE-TIME;TZID=Nowhere/Land:20261104T090000").Valid)

	a = alarmOf(t, "TRIGGER;VALUE=DATE-TIME:20261104T090000")
	assert.True(t, a.Valid && a.Floating)

	a = alarmOf(t, "TRIGGER;VALUE=DATE-TIME;RELATED=FOO:20261104T080000Z")
	assert.True(t, a.Valid, "RELATED is ignored on an absolute trigger")

	a = alarmOf(t, "ACTION:DISPLAY")
	assert.False(t, a.Valid)
	assert.False(t, a.Deliverable)

	a = alarmOf(t, "TRIGGER:-PT5M", "TRIGGER:-PT10M")
	assert.Equal(t, int64(300), a.Offset.Secs, "first TRIGGER wins")

	a = alarmOf(t, "TRIGGER:-PT5M", "REPEAT:2", "DURATION:PT5M", "RELATED-TO;RELTYPE=SNOOZE:abc")
	assert.True(t, a.Repeat && a.Snooze)
}

func TestParseAlarmsActionsAndIndex(t *testing.T) {
	for action, want := range map[string]bool{"DISPLAY": true, "display": true, "AUDIO": true, "EMAIL": false, "PROCEDURE": false, "NONE": false, "X-FOO": false} {
		a := alarmOf(t, "ACTION:"+action, "TRIGGER:-PT5M")
		assert.Equal(t, want, a.Deliverable, action)
	}
	assert.True(t, alarmOf(t, "TRIGGER:-PT5M").Deliverable, "missing ACTION is DISPLAY")
	// ACKNOWLEDGED is never read.
	assert.True(t, alarmOf(t, "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:20261104T080000Z", "ACKNOWLEDGED:20261104T090000Z").Deliverable)

	comp := parseComp(t, "BEGIN:VEVENT", "UID:a", "DTSTAMP:20260101T000000Z",
		"BEGIN:VALARM", "TRIGGER:-PT5M", "END:VALARM",
		"BEGIN:VALARM", "ACTION:EMAIL", "END:VALARM",
		"END:VEVENT")
	specs := ParseAlarms(comp)
	require.Len(t, specs, 2)
	assert.Equal(t, 0, specs[0].Index)
	assert.Equal(t, 1, specs[1].Index)
	assert.False(t, specs[1].Valid)
	assert.Nil(t, ParseAlarms(nil))
}

func eventBases(t *testing.T, lines ...string) (*TriggerBase, *TriggerBase) {
	t.Helper()
	all := append([]string{"BEGIN:VEVENT", "UID:a", "DTSTAMP:20260101T000000Z"}, lines...)
	all = append(all, "END:VEVENT")
	return ComponentTriggerBases(parseComp(t, all...))
}

func todoBases(t *testing.T, lines ...string) (*TriggerBase, *TriggerBase) {
	t.Helper()
	all := append([]string{"BEGIN:VTODO", "UID:a", "DTSTAMP:20260101T000000Z"}, lines...)
	all = append(all, "END:VTODO")
	return ComponentTriggerBases(parseComp(t, all...))
}

func rel(t *testing.T, trigger string) AlarmSpec {
	t.Helper()
	return alarmOf(t, trigger)
}

func at(t *testing.T, a AlarmSpec, s, e *TriggerBase, loc *time.Location) string {
	t.Helper()
	got, ok := a.TriggerAt(s, e, loc)
	require.True(t, ok)
	return got.Format(time.RFC3339)
}

func TestTriggerAtTZIDNominalDays(t *testing.T) {
	s, e := eventBases(t, "DTSTART;TZID=Europe/Berlin:20261026T090000", "DTEND;TZID=Europe/Berlin:20261026T100000")
	// 2026-10-25 09:00 Berlin (CET, the day clocks fell back) = 08:00Z.
	assert.Equal(t, "2026-10-25T08:00:00Z", at(t, rel(t, "TRIGGER:-P1D"), s, e, time.UTC))
	assert.Equal(t, "2026-10-26T08:30:00Z", at(t, rel(t, "TRIGGER;RELATED=END:-PT30M"), s, e, berlin(t)))
	assert.Equal(t, "2026-10-26T07:45:00Z", at(t, rel(t, "TRIGGER:-PT15M"), s, e, nil))
	assert.Equal(t, "2026-10-26T08:10:00Z", at(t, rel(t, "TRIGGER:PT10M"), s, e, nil), "positive offset")

	zs, _ := eventBases(t, "DTSTART:20261026T080000Z")
	assert.Equal(t, "2026-10-26T07:45:00Z", at(t, rel(t, "TRIGGER:-PT15M"), zs, nil, nil))
}

func TestTriggerAtWallFirst(t *testing.T) {
	loc := berlin(t)
	s, e := eventBases(t, "DTSTART;VALUE=DATE:20261025", "DTEND;VALUE=DATE:20261026")
	assert.Equal(t, "2026-10-25T08:00:00Z", at(t, rel(t, "TRIGGER:PT9H"), s, e, loc))
	s, e = eventBases(t, "DTSTART;VALUE=DATE:20260329")
	assert.Equal(t, "2026-03-29T07:00:00Z", at(t, rel(t, "TRIGGER:PT9H"), s, e, loc))
	// spring-forward gap: pin what Go does today.
	assert.Equal(t, "2026-03-29T01:30:00Z", at(t, rel(t, "TRIGGER:PT2H30M"), s, e, loc))
	s, _ = eventBases(t, "DTSTART;VALUE=DATE:20261105")
	assert.Equal(t, "2026-11-04T08:00:00Z", at(t, rel(t, "TRIGGER:-PT15H"), s, nil, loc))
	assert.Equal(t, "2026-11-05T08:00:00Z", at(t, rel(t, "TRIGGER:PT9H"), s, nil, loc))
	assert.Equal(t, "2026-11-05T09:00:00Z", at(t, rel(t, "TRIGGER:PT9H"), s, nil, nil), "nil zone is UTC")
	assert.Equal(t, "2026-11-05T09:00:00Z", at(t, rel(t, "TRIGGER:PT9H"), s, nil, time.UTC))

	fs, _ := eventBases(t, "DTSTART:20260329T120000")
	assert.Equal(t, "2026-03-28T23:00:00Z", at(t, rel(t, "TRIGGER:-PT12H"), fs, nil, loc))
}

func TestTriggerAtAbsolute(t *testing.T) {
	loc := berlin(t)
	assert.Equal(t, "2026-11-04T08:00:00Z", at(t, rel(t, "TRIGGER;VALUE=DATE-TIME:20261104T080000Z"), nil, nil, loc))
	assert.Equal(t, "2026-11-04T08:00:00Z", at(t, rel(t, "TRIGGER;VALUE=DATE-TIME:20261104T090000"), nil, nil, loc), "floating placed in owner zone")
	assert.Equal(t, "2026-11-04T09:00:00Z", at(t, rel(t, "TRIGGER;VALUE=DATE-TIME:20261104T090000"), nil, nil, nil))
	assert.Equal(t, "1976-04-01T00:55:45Z", at(t, rel(t, "TRIGGER;VALUE=DATE-TIME:19760401T005545Z"), nil, nil, loc))
	assert.Equal(t, "2026-11-04T08:00:00Z", at(t, rel(t, "TRIGGER;VALUE=DATE-TIME;TZID=Europe/Berlin:20261104T090000"), nil, nil, time.UTC))
}

func TestTriggerAtUnusable(t *testing.T) {
	_, ok := alarmOf(t, "ACTION:DISPLAY").TriggerAt(nil, nil, nil)
	assert.False(t, ok)
	// Event without DTSTART: relative alarms have no anchor.
	s, e := eventBases(t, "SUMMARY:x")
	assert.Nil(t, s)
	assert.Nil(t, e)
	_, ok = rel(t, "TRIGGER:-PT5M").TriggerAt(s, e, nil)
	assert.False(t, ok)
	// Task without DUE or DTSTART.
	s, e = todoBases(t, "SUMMARY:x")
	assert.Nil(t, s)
	assert.Nil(t, e)
	// Bad TZID base.
	s, _ = eventBases(t, "DTSTART;TZID=Nowhere/Land:20261104T090000")
	assert.Nil(t, s)
}

func TestEventAnchors(t *testing.T) {
	s, e := eventBases(t, "DTSTART:20261104T090000Z", "DURATION:PT1H")
	assert.Equal(t, time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC), e.At)
	assert.Equal(t, time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), s.At)
	s, e = eventBases(t, "DTSTART:20261104T090000Z")
	assert.Equal(t, s.At, e.At, "no DTEND/DURATION: DTSTART")
	s, e = eventBases(t, "DTSTART;VALUE=DATE:20261104")
	assert.Equal(t, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), e.At, "DATE start: +1 day")
	assert.True(t, s.Wall && e.Wall)
	s, e = eventBases(t, "DTSTART:20261104T090000", "DTEND:20261104T100000")
	assert.True(t, s.Wall && e.Wall)
}

func TestTaskAnchors(t *testing.T) {
	loc := berlin(t)
	// RELATED=END uses DUE even with DTSTART present.
	s, e := todoBases(t, "DTSTART:20261104T080000Z", "DUE:20261104T120000Z")
	assert.Equal(t, "2026-11-04T11:45:00Z", at(t, rel(t, "TRIGGER;RELATED=END:-PT15M"), s, e, loc))
	assert.Equal(t, "2026-11-04T07:45:00Z", at(t, rel(t, "TRIGGER:-PT15M"), s, e, loc))
	// START falls back to DUE.
	s, e = todoBases(t, "DUE:20261104T120000Z")
	assert.Equal(t, "2026-11-04T11:45:00Z", at(t, rel(t, "TRIGGER:-PT15M"), s, e, loc))
	// END falls back to DTSTART+DURATION, then DTSTART.
	s, e = todoBases(t, "DTSTART:20261104T080000Z", "DURATION:PT2H")
	assert.Equal(t, "2026-11-04T10:00:00Z", at(t, rel(t, "TRIGGER;RELATED=END:PT0S"), s, e, loc))
	s, e = todoBases(t, "DTSTART:20261104T080000Z")
	assert.Equal(t, "2026-11-04T08:00:00Z", at(t, rel(t, "TRIGGER;RELATED=END:PT0S"), s, e, loc))
	// All-day task: 09:00 on the due date in the owner's zone.
	s, e = todoBases(t, "DUE;VALUE=DATE:20261105")
	assert.Equal(t, "2026-11-05T08:00:00Z", at(t, rel(t, "TRIGGER;RELATED=END:PT9H"), s, e, loc))
}

// TestWallFirstMutation shows the rule is order-sensitive: placing the anchor in
// the zone first and adding exact hours gives different instants.
func TestWallFirstMutation(t *testing.T) {
	loc := berlin(t)
	for _, c := range []struct {
		date string
		d    time.Time
		want string
	}{
		{"20261025", time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), "2026-10-25T08:00:00Z"},
		{"20260329", time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC), "2026-03-29T07:00:00Z"},
	} {
		s, _ := eventBases(t, "DTSTART;VALUE=DATE:"+c.date)
		got := at(t, rel(t, "TRIGGER:PT9H"), s, nil, loc)
		placeFirst := time.Date(c.d.Year(), c.d.Month(), c.d.Day(), 0, 0, 0, 0, loc).Add(9 * time.Hour).UTC().Format(time.RFC3339)
		assert.Equal(t, c.want, got)
		assert.NotEqual(t, placeFirst, got, "place-first would be wrong for "+c.date)
	}
}
