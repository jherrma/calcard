package event

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #169 story 13: the all-day date shift of the event update (spec §7.5 step 4).

type fakeZones struct {
	loc       *time.Location // nil: the owner has no preference
	calls     int
	owner     uint
	fallbacks int
}

func (f *fakeZones) One(_ context.Context, id uint) (*time.Location, bool, error) {
	f.calls++
	f.owner = id
	if f.loc == nil {
		return time.UTC, false, nil
	}
	return f.loc, true, nil
}

func (f *fakeZones) Fallback(context.Context, uint) { f.fallbacks++ }

const (
	shiftAbs = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261104T080000Z\r\nEND:VALARM\r\n"
	shiftRel = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:PT9H\r\nEND:VALARM\r\n"
	shiftFlt = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20261104T090000\r\nEND:VALARM\r\n"
)

func allDayEvent(alarms string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:rem-1\r\nDTSTAMP:20261101T000000Z\r\n" +
		"DTSTART;VALUE=DATE:20261105\r\nDTEND;VALUE=DATE:20261106\r\nSUMMARY:S\r\n" + alarms + "END:VEVENT\r\nEND:VCALENDAR\r\n"
}

func timedEvent(alarms string) string {
	return strings.Replace(strings.Replace(allDayEvent(alarms), "DTSTART;VALUE=DATE:20261105", "DTSTART:20261105T100000Z", 1),
		"DTEND;VALUE=DATE:20261106", "DTEND:20261105T110000Z", 1)
}

func shiftUpdate(t *testing.T, ics string, z *fakeZones, in UpdateEventInput) (*stubCalendarRepo, *calendar.CalendarObject, error) {
	t.Helper()
	obj := &calendar.CalendarObject{UUID: "u", UID: "rem-1", ComponentType: "VEVENT", ETag: "etag-1", ICalData: ics}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	repo := &stubCalendarRepo{obj: obj, calendars: map[uint]*calendar.Calendar{0: {UserID: 42}}}
	in.UUID = "u"
	if in.Scope == "" {
		in.Scope = "all"
	}
	uc := NewUpdateEventUseCase(repo)
	if z != nil {
		uc.WithOwnerZones(z)
	}
	got, err := uc.Execute(context.Background(), in)
	return repo, got, err
}

func triggerValues(ics string) []string {
	var out []string
	for _, l := range alarmLines(ics) {
		if strings.HasPrefix(l, "TRIGGER") {
			out = append(out, l)
		}
	}
	return out
}

func TestUpdateEvent_AllDayDateMoveShiftsAbsoluteTriggers(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	cases := map[string]struct {
		start, end string
		want       string
	}{
		"+7 days":   {"2026-11-12T00:00:00Z", "2026-11-13T00:00:00Z", "TRIGGER;VALUE=DATE-TIME:20261111T080000Z"},
		"into CEST": {"2027-04-02T00:00:00Z", "2027-04-03T00:00:00Z", "TRIGGER;VALUE=DATE-TIME:20270401T070000Z"},
		"-1 day":    {"2026-11-04T00:00:00Z", "2026-11-05T00:00:00Z", "TRIGGER;VALUE=DATE-TIME:20261103T080000Z"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			z := &fakeZones{loc: berlin}
			repo, got, err := shiftUpdate(t, allDayEvent(shiftAbs+shiftRel+shiftFlt), z, UpdateEventInput{Start: &c.start, End: &c.end})
			require.NoError(t, err)
			require.NotNil(t, repo.updated)
			assert.Equal(t, []string{c.want, "TRIGGER:PT9H", "TRIGGER;VALUE=DATE-TIME:20261104T090000"}, triggerValues(got.ICalData),
				"relative and floating triggers are untouched")
			assert.NotEqual(t, "etag-1", got.ETag)
			assert.Equal(t, 1, z.calls)
			assert.Equal(t, uint(42), z.owner, "the calendar owner's zone")
		})
	}
}

func TestUpdateEvent_ShiftNeedsAnAllDayDateMove(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	start, end := "2026-11-12T00:00:00Z", "2026-11-13T00:00:00Z"
	t.Run("a title edit shifts nothing and loads no zone", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		_, got, err := shiftUpdate(t, allDayEvent(shiftAbs), z, UpdateEventInput{Summary: strp("New")})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggerValues(got.ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("the same date shifts nothing", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		s, e := "2026-11-05T00:00:00Z", "2026-11-06T00:00:00Z"
		_, got, err := shiftUpdate(t, allDayEvent(shiftAbs), z, UpdateEventInput{Start: &s, End: &e})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggerValues(got.ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("a timed event never shifts", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		s, e := "2026-11-12T10:00:00Z", "2026-11-12T11:00:00Z"
		_, got, err := shiftUpdate(t, timedEvent(shiftAbs), z, UpdateEventInput{Start: &s, End: &e})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggerValues(got.ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("all-day to timed shifts nothing", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		s, e := "2026-11-12T10:00:00Z", "2026-11-12T11:00:00Z"
		_, got, err := shiftUpdate(t, allDayEvent(shiftAbs), z, UpdateEventInput{Start: &s, End: &e, IsAllDay: boolp(false)})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggerValues(got.ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("timed to all-day shifts nothing", func(t *testing.T) {
		z := &fakeZones{loc: berlin}
		_, got, err := shiftUpdate(t, timedEvent(shiftAbs), z, UpdateEventInput{Start: &start, End: &end, IsAllDay: boolp(true)})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggerValues(got.ICalData))
		assert.Zero(t, z.calls)
	})
	t.Run("no resolver is whole UTC days", func(t *testing.T) {
		_, got, err := shiftUpdate(t, allDayEvent(shiftAbs), nil, UpdateEventInput{Start: &start, End: &end})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z"}, triggerValues(got.ICalData))
	})
	t.Run("an owner without a preference is UTC and logs the fallback", func(t *testing.T) {
		z := &fakeZones{}
		_, got, err := shiftUpdate(t, allDayEvent(shiftAbs), z, UpdateEventInput{Start: &start, End: &end})
		require.NoError(t, err)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z"}, triggerValues(got.ICalData))
		assert.Equal(t, 1, z.fallbacks)
	})
}

func TestUpdateEvent_ShiftSkipsRemindersOfTheSameRequest(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	z := &fakeZones{loc: berlin}
	start, end := "2026-11-12T00:00:00Z", "2026-11-13T00:00:00Z"

	// An `at` equal to the stored trigger is matched, then shifted; a new `at`
	// of the same request is written as sent.
	_, got, err := shiftUpdate(t, allDayEvent(shiftAbs), z, UpdateEventInput{
		Start: &start, End: &end,
		Reminders: rems(rAt("2026-11-04T08:00:00Z"), rAt("2026-11-20T08:00:00Z"))})
	require.NoError(t, err)
	assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z", "TRIGGER;VALUE=DATE-TIME:20261120T080000Z"}, triggerValues(got.ICalData))

	// A new absolute reminder alone is not shifted, a new relative one just resolves.
	_, got, err = shiftUpdate(t, allDayEvent(""), z, UpdateEventInput{
		Start: &start, End: &end, Reminders: rems(rAt("2026-11-10T08:00:00Z"), rOff(32400))})
	require.NoError(t, err)
	assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261110T080000Z", "TRIGGER:PT9H"}, triggerValues(got.ICalData))
}

func TestUpdateEvent_ShiftOnAllDayOverrideOnly(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	z := &fakeZones{loc: berlin}
	// A new override has no earlier date: nothing shifts, the series master is untouched.
	ics := strings.Replace(allDayEvent(shiftAbs+shiftRel), "SUMMARY:S", "SUMMARY:S\r\nRRULE:FREQ=DAILY;COUNT=5", 1)
	s, e := "2026-11-08T00:00:00Z", "2026-11-09T00:00:00Z"
	_, got, err := shiftUpdate(t, ics, z, UpdateEventInput{Scope: "this", RecurrenceID: "20261106", Start: &s, End: &e, Summary: strp("X")})
	require.NoError(t, err)
	assert.Contains(t, got.ICalData, "TRIGGER;VALUE=DATE-TIME:20261104T080000Z")
	assert.NotContains(t, got.ICalData, "20261107T080000Z")
	assert.Zero(t, z.calls)
}

func boolp(b bool) *bool { return &b }
