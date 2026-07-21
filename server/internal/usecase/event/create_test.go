package event

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

func (s *stubCalendarRepo) CreateCalendarObject(_ context.Context, _ *calendar.CalendarObject) error {
	return nil
}

// TestCreateEvent_AllDaySeriesUntilIsDateValueAndIncludesEndDate verifies the
// API create path for an all-day recurring series (issue #118). The handler
// renders the chosen "Ends on" date as a bare-DATE UNTIL via
// dto.RecurrenceRuleDTO.ToRRule(true) — e.g. UNTIL=20260801 — so the use case
// must persist an RRULE whose UNTIL is a DATE matching DTSTART;VALUE=DATE (RFC
// 5545 §3.3.10), and expansion must include the chosen end date's own
// occurrence (UNTIL is inclusive).
func TestCreateEvent_AllDaySeriesUntilIsDateValueAndIncludesEndDate(t *testing.T) {
	repo := &stubCalendarRepo{}
	uc := NewCreateEventUseCase(repo)

	// Weekly all-day series. DTSTART 2026-07-04 and the chosen end date
	// 2026-08-01 are 28 days (4 weeks) apart, so they share a weekday and the
	// end date is a genuine occurrence of the series.
	start := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	obj, err := uc.Execute(context.Background(), CreateEventInput{
		CalendarID: 1,
		Summary:    "All-day weekly",
		Start:      start,
		End:        end,
		IsAllDay:   true,
		RRule:      "FREQ=WEEKLY;UNTIL=20260801",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
	if err != nil {
		t.Fatalf("failed to decode persisted iCal: %v", err)
	}
	events := cal.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 VEVENT, got %d", len(events))
	}
	ev := events[0]

	// DTSTART must be a VALUE=DATE property.
	dtstart := ev.Props.Get(ical.PropDateTimeStart)
	if dtstart == nil {
		t.Fatalf("no DTSTART found, iCal:\n%s", obj.ICalData)
	}
	if vt := dtstart.Params.Get(ical.ParamValue); vt != string(ical.ValueDate) {
		t.Errorf("expected DTSTART VALUE=DATE, got VALUE=%q (iCal:\n%s)", vt, obj.ICalData)
	}

	// UNTIL must be a bare DATE matching DTSTART's value type, not a UTC
	// DATE-TIME, and must carry the chosen end date unchanged.
	until, ok := rruleUntil(&ev)
	if !ok {
		t.Fatalf("no RRULE with UNTIL found, iCal:\n%s", obj.ICalData)
	}
	if until != "20260801" {
		t.Errorf("expected DATE-value UNTIL %q, got %q (iCal:\n%s)", "20260801", until, obj.ICalData)
	}

	// Expansion across a window covering the end date must include an occurrence
	// on 2026-08-01 itself — the inclusive UNTIL boundary.
	winStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	instances, err := calendar.ExpandRecurringEvent(obj, winStart, winEnd)
	if err != nil {
		t.Fatalf("ExpandRecurringEvent returned error: %v", err)
	}
	foundEndDate := false
	for _, inst := range instances {
		d := inst.Start.UTC()
		if d.Year() == 2026 && d.Month() == time.August && d.Day() == 1 {
			foundEndDate = true
			break
		}
	}
	if !foundEndDate {
		t.Errorf("expected an expanded occurrence on the chosen end date 2026-08-01, got %d instances: %v", len(instances), instances)
	}
}
