package event

import (
	"context"
	"strings"
	"testing"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// stubCalendarRepo embeds the interface so only the methods exercised by the
// update use case need real implementations; any unimplemented method would
// panic on a nil call, which is fine because these tests never reach them.
type stubCalendarRepo struct {
	calendar.CalendarRepository
	obj     *calendar.CalendarObject
	updated *calendar.CalendarObject
}

func (s *stubCalendarRepo) GetCalendarObjectByUUID(_ context.Context, _ string) (*calendar.CalendarObject, error) {
	return s.obj, nil
}

func (s *stubCalendarRepo) UpdateCalendarObject(_ context.Context, obj *calendar.CalendarObject) error {
	s.updated = obj
	return nil
}

// TestUpdateEvent_DurationBackedEvent verifies that editing an event that was
// stored with DURATION (and no DTEND) — as native DAV clients often create —
// does not fail to re-encode. go-ical's encoder rejects a component carrying
// both DTEND and DURATION, so the update must drop DURATION before writing
// DTEND. Before the fix, Execute returned a "failed to encode iCalendar" error.
func TestUpdateEvent_DurationBackedEvent(t *testing.T) {
	const durationICal = "BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"PRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:dur-event-1\r\n" +
		"DTSTAMP:20240101T000000Z\r\n" +
		"DTSTART:20240101T100000Z\r\n" +
		"DURATION:PT1H\r\n" +
		"SUMMARY:Original\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	repo := &stubCalendarRepo{
		obj: &calendar.CalendarObject{
			UUID:          "obj-uuid",
			UID:           "dur-event-1",
			ComponentType: "VEVENT",
			ICalData:      durationICal,
		},
	}

	uc := NewUpdateEventUseCase(repo)
	newSummary := "Edited"
	got, err := uc.Execute(context.Background(), UpdateEventInput{
		UUID:    "obj-uuid",
		Summary: &newSummary,
	})
	if err != nil {
		t.Fatalf("Execute returned error for DURATION-backed event: %v", err)
	}
	if repo.updated == nil {
		t.Fatal("expected the object to be persisted via UpdateCalendarObject")
	}

	// Re-decode the persisted iCal and assert the encoded component carries a
	// DTEND and no longer carries DURATION (the two must never coexist).
	cal, err := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	if err != nil {
		t.Fatalf("failed to re-decode updated iCal: %v", err)
	}
	events := cal.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 VEVENT, got %d", len(events))
	}
	ev := events[0]
	if ev.Props.Get(ical.PropDuration) != nil {
		t.Errorf("DURATION should have been removed, but it is still present:\n%s", got.ICalData)
	}
	if ev.Props.Get(ical.PropDateTimeEnd) == nil {
		t.Errorf("expected DTEND to be present after update, iCal:\n%s", got.ICalData)
	}
	if summary := ev.Props.Get(ical.PropSummary); summary == nil || summary.Value != "Edited" {
		t.Errorf("expected SUMMARY \"Edited\", got %v", summary)
	}
}

const allDaySeriesICal = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:allday-series-1\r\n" +
	"DTSTAMP:20240101T000000Z\r\n" +
	"DTSTART;VALUE=DATE:20240101\r\n" +
	"DTEND;VALUE=DATE:20240102\r\n" +
	"RRULE:FREQ=DAILY;COUNT=10\r\n" +
	"SUMMARY:Daily\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// rruleUntil returns the UNTIL= token value from a VEVENT's RRULE, and whether
// the event has an RRULE carrying an UNTIL at all.
func rruleUntil(ev *ical.Event) (string, bool) {
	p := ev.Props.Get(ical.PropRecurrenceRule)
	if p == nil {
		return "", false
	}
	for _, part := range strings.Split(p.Value, ";") {
		if strings.HasPrefix(part, "UNTIL=") {
			return strings.TrimPrefix(part, "UNTIL="), true
		}
	}
	return "", false
}

// TestUpdateEvent_AllDaySplitUntilIsDateValue verifies that a this_and_future
// edit of an all-day (VALUE=DATE) series writes the old master's RRULE UNTIL as
// a bare DATE (20240104), not a UTC DATE-TIME (20240104T...Z). RFC 5545 requires
// UNTIL to share DTSTART's value type; a DATE-TIME UNTIL against a DATE series
// makes strict clients (Apple, DAVx5) reject the whole RRULE.
func TestUpdateEvent_AllDaySplitUntilIsDateValue(t *testing.T) {
	repo := &stubCalendarRepo{
		obj: &calendar.CalendarObject{
			UUID:          "obj-uuid",
			UID:           "allday-series-1",
			ComponentType: "VEVENT",
			ICalData:      allDaySeriesICal,
			IsAllDay:      true,
		},
	}

	uc := NewUpdateEventUseCase(repo)
	newSummary := "Edited future"
	got, err := uc.Execute(context.Background(), UpdateEventInput{
		UUID:         "obj-uuid",
		Summary:      &newSummary,
		Scope:        "this_and_future",
		RecurrenceID: "20240105",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cal, err := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	if err != nil {
		t.Fatalf("failed to re-decode updated iCal: %v", err)
	}

	var until string
	found := false
	for _, ev := range cal.Events() {
		if u, ok := rruleUntil(&ev); ok {
			until = u
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no RRULE with UNTIL found after split, iCal:\n%s", got.ICalData)
	}
	if until != "20240104" {
		t.Errorf("expected DATE-value UNTIL %q, got %q (iCal:\n%s)", "20240104", until, got.ICalData)
	}
}

const timedSeriesICal = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:timed-series-1\r\n" +
	"DTSTAMP:20240101T000000Z\r\n" +
	"DTSTART:20240101T100000Z\r\n" +
	"DTEND:20240101T110000Z\r\n" +
	"RRULE:FREQ=DAILY;COUNT=10\r\n" +
	"SUMMARY:Daily\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// TestUpdateEvent_AllDaySeriesUntilRenderedFromEffectiveAllDay is the #118
// hardening regression: an API/MCP client edits a stored all-day series and
// resends the recurrence WITHOUT all_day (a partial update). Because the
// effective all-day state (obj.IsAllDay, still true) is only known in the use
// case, the RRULE must be rendered THERE from the raw recurrence — emitting a
// bare DATE UNTIL that matches DTSTART;VALUE=DATE, on the correct day.
//
// The raw Until carries a +02:00 offset (local midnight of the chosen end
// date). Rendering from the effective all-day state keeps the sender's calendar
// date: UNTIL=20260801. The pre-fix path rendered in the handler with
// allDay=false (all_day omitted → default false), producing UNTIL=20260731T220000Z
// — wrong value TYPE (DATE-TIME against a DATE series) AND wrong DAY.
func TestUpdateEvent_AllDaySeriesUntilRenderedFromEffectiveAllDay(t *testing.T) {
	repo := &stubCalendarRepo{
		obj: &calendar.CalendarObject{
			UUID:          "obj-uuid",
			UID:           "allday-series-1",
			ComponentType: "VEVENT",
			ICalData:      allDaySeriesICal,
			IsAllDay:      true,
		},
	}

	uc := NewUpdateEventUseCase(repo)
	until := "2026-08-01T00:00:00+02:00"
	got, err := uc.Execute(context.Background(), UpdateEventInput{
		UUID:  "obj-uuid",
		Scope: "all",
		// IsAllDay deliberately nil: the client omitted all_day on this partial
		// update, so the effective state must come from the stored object.
		Recurrence: &calendar.RecurrenceRule{Frequency: "WEEKLY", Until: &until},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cal, err := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	if err != nil {
		t.Fatalf("failed to re-decode updated iCal: %v", err)
	}
	events := cal.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 VEVENT, got %d", len(events))
	}
	ev := events[0]

	// DTSTART must remain a VALUE=DATE property (the series is all-day).
	dtstart := ev.Props.Get(ical.PropDateTimeStart)
	if dtstart == nil {
		t.Fatalf("no DTSTART found, iCal:\n%s", got.ICalData)
	}
	if vt := dtstart.Params.Get(ical.ParamValue); vt != string(ical.ValueDate) {
		t.Errorf("expected DTSTART VALUE=DATE, got VALUE=%q (iCal:\n%s)", vt, got.ICalData)
	}

	until2, ok := rruleUntil(&ev)
	if !ok {
		t.Fatalf("no RRULE with UNTIL found, iCal:\n%s", got.ICalData)
	}
	// Bare DATE matching DTSTART;VALUE=DATE, and the sender's own calendar date —
	// no DATE-TIME, no off-by-one.
	if until2 != "20260801" {
		t.Errorf("expected DATE-value UNTIL %q rendered from effective all-day state, got %q (iCal:\n%s)", "20260801", until2, got.ICalData)
	}
}

// TestUpdateEvent_TimedSeriesUntilStaysDateTime is the counterpart to the
// all-day case: a stored timed series edited on the same update path must keep
// its UNTIL as a UTC DATE-TIME. The same +02:00 input renders to 20260731T220000Z.
func TestUpdateEvent_TimedSeriesUntilStaysDateTime(t *testing.T) {
	repo := &stubCalendarRepo{
		obj: &calendar.CalendarObject{
			UUID:          "obj-uuid",
			UID:           "timed-series-1",
			ComponentType: "VEVENT",
			ICalData:      timedSeriesICal,
			IsAllDay:      false,
		},
	}

	uc := NewUpdateEventUseCase(repo)
	until := "2026-08-01T00:00:00+02:00"
	got, err := uc.Execute(context.Background(), UpdateEventInput{
		UUID:       "obj-uuid",
		Scope:      "all",
		Recurrence: &calendar.RecurrenceRule{Frequency: "WEEKLY", Until: &until},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cal, err := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	if err != nil {
		t.Fatalf("failed to re-decode updated iCal: %v", err)
	}
	events := cal.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 VEVENT, got %d", len(events))
	}
	ev := events[0]

	got2, ok := rruleUntil(&ev)
	if !ok {
		t.Fatalf("no RRULE with UNTIL found, iCal:\n%s", got.ICalData)
	}
	if got2 != "20260731T220000Z" {
		t.Errorf("expected UTC DATE-TIME UNTIL %q, got %q (iCal:\n%s)", "20260731T220000Z", got2, got.ICalData)
	}
}

// TestUpdateEvent_AllDayExceptionRecurrenceIDIsDateValue verifies that editing a
// single instance ("this" scope) of an all-day series creates the exception
// VEVENT with a VALUE=DATE RECURRENCE-ID matching the series' DTSTART type,
// rather than a UTC DATE-TIME that strict clients would fail to correlate.
func TestUpdateEvent_AllDayExceptionRecurrenceIDIsDateValue(t *testing.T) {
	repo := &stubCalendarRepo{
		obj: &calendar.CalendarObject{
			UUID:          "obj-uuid",
			UID:           "allday-series-1",
			ComponentType: "VEVENT",
			ICalData:      allDaySeriesICal,
			IsAllDay:      true,
		},
	}

	uc := NewUpdateEventUseCase(repo)
	newSummary := "Edited one"
	got, err := uc.Execute(context.Background(), UpdateEventInput{
		UUID:         "obj-uuid",
		Summary:      &newSummary,
		Scope:        "this",
		RecurrenceID: "20240105",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cal, err := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	if err != nil {
		t.Fatalf("failed to re-decode updated iCal: %v", err)
	}

	var rid *ical.Prop
	for _, ev := range cal.Events() {
		if p := ev.Props.Get(ical.PropRecurrenceID); p != nil {
			rid = p
			break
		}
	}
	if rid == nil {
		t.Fatalf("no exception VEVENT with RECURRENCE-ID found, iCal:\n%s", got.ICalData)
	}
	if vt := rid.Params.Get(ical.ParamValue); vt != string(ical.ValueDate) {
		t.Errorf("expected RECURRENCE-ID VALUE=DATE, got VALUE=%q (iCal:\n%s)", vt, got.ICalData)
	}
	if rid.Value != "20240105" {
		t.Errorf("expected RECURRENCE-ID value %q, got %q", "20240105", rid.Value)
	}
}
