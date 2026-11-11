package event

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #169 story 11: the reminders field of the event create and update use cases.

func rOff(n int64) calendar.ReminderInput { return calendar.ReminderInput{OffsetSeconds: &n} }

func rAt(s string) calendar.ReminderInput {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return calendar.ReminderInput{At: &t}
}

func rems(in ...calendar.ReminderInput) *[]calendar.ReminderInput { return &in }

func createWith(t *testing.T, in CreateEventInput) (*calendar.CalendarObject, error) {
	t.Helper()
	repo := &stubCalendarRepo{}
	in.CalendarID = 1
	in.Summary = "S"
	if in.Start.IsZero() {
		in.Start = time.Date(2030, 11, 4, 10, 0, 0, 0, time.UTC)
		in.End = in.Start.Add(time.Hour)
	}
	obj, err := NewCreateEventUseCase(repo).Execute(context.Background(), in)
	if err != nil {
		assert.Nil(t, repo.created, "nothing is written on an error")
	}
	return obj, err
}

func alarmLines(ics string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(ics, "\r\n ", ""), "\r\n") {
		switch {
		case l == "BEGIN:VALARM":
			in = true
		case l == "END:VALARM":
			in = false
		case in:
			out = append(out, l)
		}
	}
	return out
}

func TestCreateEvent_Reminders(t *testing.T) {
	obj, err := createWith(t, CreateEventInput{Reminders: []calendar.ReminderInput{rOff(-900), rAt("2030-11-04T09:00:00+01:00")}})
	require.NoError(t, err)
	lines := alarmLines(obj.ICalData)
	assert.Contains(t, lines, "ACTION:DISPLAY")
	assert.Contains(t, lines, "DESCRIPTION:Reminder")
	assert.Contains(t, lines, "TRIGGER:-PT15M")
	assert.Contains(t, lines, "TRIGGER;VALUE=DATE-TIME:20301104T080000Z")
	assert.Equal(t, 2, strings.Count(obj.ICalData, "BEGIN:VALARM"))
	assert.Equal(t, 2, strings.Count(obj.ICalData, "\r\nUID:")-1, "each VALARM has its own UID")
	// Parsed back, both are editable.
	cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
	require.NoError(t, err)
	for _, s := range calendar.ParseAlarms(cal.Events()[0].Component) {
		assert.True(t, s.Editable(calendar.ReminderKindEvent))
	}
}

func TestCreateEvent_NoRemindersWritesNoVALARM(t *testing.T) {
	obj, err := createWith(t, CreateEventInput{})
	require.NoError(t, err)
	assert.NotContains(t, obj.ICalData, "VALARM")
}

func TestCreateEvent_AllDayReminderIsRelative(t *testing.T) {
	obj, err := createWith(t, CreateEventInput{
		Start: time.Date(2030, 11, 4, 0, 0, 0, 0, time.UTC), End: time.Date(2030, 11, 5, 0, 0, 0, 0, time.UTC),
		IsAllDay: true, Reminders: []calendar.ReminderInput{rOff(32400), rOff(-54000)},
	})
	require.NoError(t, err)
	lines := alarmLines(obj.ICalData)
	assert.Contains(t, lines, "TRIGGER:PT9H")
	assert.Contains(t, lines, "TRIGGER:-PT15H")
}

func TestCreateEvent_ReminderValidation(t *testing.T) {
	start := time.Date(2030, 11, 4, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   []calendar.ReminderInput
		want string
	}{
		{"eleven", make([]calendar.ReminderInput, 11), "reminders must contain at most 10 entries"},
		{"empty entry", []calendar.ReminderInput{{}}, "a reminder needs exactly one of offset_seconds and at"},
		{"both", []calendar.ReminderInput{{OffsetSeconds: new(int64), At: &start}}, "a reminder needs exactly one of offset_seconds and at"},
		{"offset high", []calendar.ReminderInput{rOff(400*86400 + 1)}, "reminder offset out of range"},
		{"offset low", []calendar.ReminderInput{rOff(-400*86400 - 1)}, "reminder offset out of range"},
		{"at late", []calendar.ReminderInput{rAt("2031-12-10T10:00:00Z")}, "reminder time out of range"},
		{"at early", []calendar.ReminderInput{rAt("2029-09-01T10:00:00Z")}, "reminder time out of range"},
	}
	for _, c := range cases {
		_, err := createWith(t, CreateEventInput{Reminders: c.in})
		require.ErrorIs(t, err, ErrInvalidInput, c.name)
		assert.Equal(t, c.want, err.Error(), c.name)
	}
	// Ten and the boundaries are fine.
	ten := make([]calendar.ReminderInput, 10)
	for i := range ten {
		ten[i] = rOff(int64(-60 * (i + 1)))
	}
	_, err := createWith(t, CreateEventInput{Reminders: append(ten[:9:9], rOff(400*86400))})
	require.NoError(t, err)
}

const remBase = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:rem-1\r\nDTSTAMP:20301101T000000Z\r\n" +
	"DTSTART:20301104T100000Z\r\nDTEND:20301104T110000Z\r\nSUMMARY:S\r\n"

const (
	remEmail = "BEGIN:VALARM\r\nACTION:EMAIL\r\nDESCRIPTION:d\r\nSUMMARY:s\r\nTRIGGER:-PT30M\r\nEND:VALARM\r\n"
	remAudio = "BEGIN:VALARM\r\nACTION:AUDIO\r\nATTACH:http://x/y.wav\r\nTRIGGER:-PT20M\r\nX-FOO:bar\r\nEND:VALARM\r\n"
	remRep   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDURATION:PT1M\r\nREPEAT:2\r\nTRIGGER:-PT10M\r\nEND:VALARM\r\n"
	remEnd   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:-PT5M\r\nEND:VALARM\r\n"
	remM15   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Soon\r\nTRIGGER:-PT15M\r\nUID:k1\r\nX-KEEP:1\r\nEND:VALARM\r\n"
	remAbs   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20330101T080000Z\r\nEND:VALARM\r\n"
)

func remEvent(alarms string) string { return remBase + alarms + "END:VEVENT\r\nEND:VCALENDAR\r\n" }

func updateRem(t *testing.T, ics string, in UpdateEventInput) (*stubCalendarRepo, *calendar.CalendarObject, error) {
	t.Helper()
	obj := &calendar.CalendarObject{UUID: "u", UID: "rem-1", ComponentType: "VEVENT", ETag: "etag-1", ICalData: ics}
	repo := &stubCalendarRepo{obj: obj}
	in.UUID = "u"
	if in.Scope == "" {
		in.Scope = "all"
	}
	got, err := NewUpdateEventUseCase(repo).Execute(context.Background(), in)
	return repo, got, err
}

func TestUpdateEvent_RemindersAbsentLeavesVALARMsByteForByte(t *testing.T) {
	ics := remEvent(remM15 + remEmail + remAudio + remRep + remEnd)
	repo, got, err := updateRem(t, ics, UpdateEventInput{Summary: strp("New")})
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	assert.Equal(t, alarmLines(ics), alarmLines(got.ICalData))
}

func TestUpdateEvent_SameRemindersWritesNothing(t *testing.T) {
	ics := remEvent(remM15 + remAbs + remEmail)
	repo, got, err := updateRem(t, ics, UpdateEventInput{Reminders: rems(rAt("2033-01-01T09:00:00+01:00"), rOff(-900))})
	require.NoError(t, err)
	assert.Nil(t, repo.updated, "no write")
	assert.Equal(t, "etag-1", got.ETag)
	assert.Equal(t, ics, got.ICalData)

	// With another field changed, the object is written and the kept VALARMs stay.
	repo, got, err = updateRem(t, ics, UpdateEventInput{Summary: strp("X"), Reminders: rems(rOff(-900), rAt("2033-01-01T08:00:00Z"))})
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	assert.Equal(t, alarmLines(ics), alarmLines(got.ICalData))
}

func TestUpdateEvent_RemindersEmptyRemovesOnlyEditable(t *testing.T) {
	ics := remEvent(remM15 + remEmail + remAudio + remRep + remEnd + remAbs)
	for _, in := range []*[]calendar.ReminderInput{rems(), {}} {
		_, got, err := updateRem(t, ics, UpdateEventInput{Reminders: in})
		require.NoError(t, err)
		want := alarmLines(remEvent(remEmail + remRep + remEnd))
		assert.Equal(t, want, alarmLines(got.ICalData))
		assert.NotEqual(t, "etag-1", got.ETag)
	}
}

func TestUpdateEvent_RemindersAppendAfterExisting(t *testing.T) {
	ics := remEvent(remM15 + remEmail)
	_, got, err := updateRem(t, ics, UpdateEventInput{Reminders: rems(rOff(-900), rOff(-3600))})
	require.NoError(t, err)
	cal, err := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	require.NoError(t, err)
	specs := calendar.ParseAlarms(cal.Events()[0].Component)
	require.Len(t, specs, 3)
	assert.Equal(t, int64(-900), int64(-specs[0].Offset.Secs))
	assert.Equal(t, "EMAIL", specs[1].Action)
	assert.Equal(t, int64(3600), specs[2].Offset.Secs)
	assert.True(t, specs[2].Offset.Neg)
	assert.Contains(t, got.ICalData, "UID:k1", "the matched VALARM keeps its UID")
	assert.Contains(t, got.ICalData, "X-KEEP:1")
}

func TestUpdateEvent_ReminderRangeAppliesOnlyToUnmatched(t *testing.T) {
	// The stored absolute trigger is more than 400 days from the start.
	ics := remEvent(remAbs)
	repo, _, err := updateRem(t, ics, UpdateEventInput{Reminders: rems(rAt("2033-01-01T08:00:00Z"))})
	require.NoError(t, err, "a matched entry is never refused")
	assert.Nil(t, repo.updated)

	repo, _, err = updateRem(t, ics, UpdateEventInput{Reminders: rems(rAt("2033-01-01T08:00:00Z"), rAt("2033-01-02T08:00:00Z"))})
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Equal(t, "reminder time out of range", err.Error())
	assert.Nil(t, repo.updated, "nothing written on a 400")

	repo, _, err = updateRem(t, remEvent(remM15), UpdateEventInput{Reminders: rems(rOff(-900), rOff(401*86400))})
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Equal(t, "reminder offset out of range", err.Error())
	assert.Nil(t, repo.updated)

	repo, _, err = updateRem(t, ics, UpdateEventInput{Reminders: func() *[]calendar.ReminderInput {
		r := make([]calendar.ReminderInput, 11)
		return &r
	}()})
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Equal(t, "reminders must contain at most 10 entries", err.Error())
	assert.Nil(t, repo.updated)
}

func TestUpdateEvent_ReminderRangeUsesTheEditedStart(t *testing.T) {
	ics := remEvent("")
	// The new start moves 2 years out: an absolute reminder near it is in range.
	_, got, err := updateRem(t, ics, UpdateEventInput{
		Start: strp("2032-01-01T10:00:00Z"), End: strp("2032-01-01T11:00:00Z"),
		Reminders: rems(rAt("2032-01-01T09:00:00Z")),
	})
	require.NoError(t, err)
	assert.Contains(t, got.ICalData, "TRIGGER;VALUE=DATE-TIME:20320101T090000Z")
}

const remSeries = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:rem-1\r\nDTSTAMP:20301101T000000Z\r\n" +
	"DTSTART:20301104T100000Z\r\nDTEND:20301104T110000Z\r\nRRULE:FREQ=DAILY;COUNT=5\r\nSUMMARY:S\r\n"

func TestUpdateEvent_ScopeThisRemindersMatchCopiedAlarms(t *testing.T) {
	ics := remSeries + remM15 + remAbs + "END:VEVENT\r\nEND:VCALENDAR\r\n"
	in := UpdateEventInput{Scope: "this", RecurrenceID: "20301106T100000Z"}

	// Unchanged list: the override is still created (it is a change), carries the
	// copied relative VALARM with its UID, and nothing else.
	in.Reminders = rems(rOff(-900))
	repo, got, err := updateRem(t, ics, in)
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	cal, _ := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	require.Len(t, cal.Events(), 2)
	var ov *ical.Component
	for _, e := range cal.Events() {
		if e.Props.Get(ical.PropRecurrenceID) != nil {
			ov = e.Component
		}
	}
	require.NotNil(t, ov)
	require.Len(t, ov.Children, 1)
	assert.Equal(t, "k1", ov.Children[0].Props.Get(ical.PropUID).Value, "copied, then matched: kept untouched")
	assert.Equal(t, "1", ov.Children[0].Props.Get("X-KEEP").Value)

	// An added reminder lands on the override only; the master keeps its own.
	in.Reminders = rems(rOff(-900), rOff(-60))
	_, got, err = updateRem(t, ics, in)
	require.NoError(t, err)
	assert.Equal(t, 2+1+1, strings.Count(got.ICalData, "BEGIN:VALARM"), "master 2, override 2 (copied + new)")

	// An empty list removes the copied one from the override; the master stays.
	in.Reminders = rems()
	_, got, err = updateRem(t, ics, in)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(got.ICalData, "BEGIN:VALARM"))

	// scope=this on an existing override with the same reminders and nothing else: no write.
	ics2 := got.ICalData
	repo2, got2, err := updateRem(t, ics2, UpdateEventInput{Scope: "this", RecurrenceID: "20301106T100000Z", Reminders: rems()})
	require.NoError(t, err)
	assert.Nil(t, repo2.updated)
	assert.Equal(t, "etag-1", got2.ETag)
}

func TestUpdateEvent_SplitMatchesCopiedAlarms(t *testing.T) {
	ics := remSeries + remM15 + "END:VEVENT\r\nEND:VCALENDAR\r\n"
	_, got, err := updateRem(t, ics, UpdateEventInput{
		Scope: "this_and_future", RecurrenceID: "20301106T100000Z", Reminders: rems(rOff(-900), rOff(-3600)),
	})
	require.NoError(t, err)
	assert.Equal(t, 1+2, strings.Count(got.ICalData, "BEGIN:VALARM"), "old master keeps one, new master 1 copied + 1 added")
	assert.Equal(t, 2, strings.Count(got.ICalData, "UID:k1"))
}

func strp(s string) *string { return &s }
