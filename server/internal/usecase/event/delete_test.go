package event

import (
	"context"
	"strings"
	"testing"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// TestDeleteEvent_AllDayExdateIsDateValue verifies that deleting a single
// instance ("this" scope) of an all-day (VALUE=DATE) series writes the master's
// EXDATE as a bare DATE (20240105), not a UTC DATE-TIME. RFC 5545 requires
// EXDATE to share DTSTART's value type; a DATE-TIME EXDATE against a DATE series
// is silently ignored by strict clients (Apple, DAVx5), so the excluded day
// keeps showing up.
func TestDeleteEvent_AllDayExdateIsDateValue(t *testing.T) {
	repo := &stubCalendarRepo{
		obj: &calendar.CalendarObject{
			UUID:          "obj-uuid",
			UID:           "allday-series-1",
			ComponentType: "VEVENT",
			ICalData:      allDaySeriesICal,
			IsAllDay:      true,
		},
	}

	uc := NewDeleteEventUseCase(repo)
	if err := uc.Execute(context.Background(), "obj-uuid", "this", "20240105"); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if repo.updated == nil {
		t.Fatal("expected the object to be persisted via UpdateCalendarObject")
	}

	cal, err := ical.NewDecoder(strings.NewReader(repo.updated.ICalData)).Decode()
	if err != nil {
		t.Fatalf("failed to re-decode updated iCal: %v", err)
	}

	var exdate *ical.Prop
	for _, ev := range cal.Events() {
		if ev.Props.Get(ical.PropRecurrenceID) != nil {
			continue // master only
		}
		if p := ev.Props.Get("EXDATE"); p != nil {
			exdate = p
			break
		}
	}
	if exdate == nil {
		t.Fatalf("no EXDATE found on master after delete, iCal:\n%s", repo.updated.ICalData)
	}
	if vt := exdate.Params.Get(ical.ParamValue); vt != string(ical.ValueDate) {
		t.Errorf("expected EXDATE VALUE=DATE, got VALUE=%q (iCal:\n%s)", vt, repo.updated.ICalData)
	}
	if exdate.Value != "20240105" {
		t.Errorf("expected EXDATE value %q, got %q", "20240105", exdate.Value)
	}
}

// TestDeleteEvent_AllDayTerminateUntilIsDateValue verifies that a
// this_and_future delete of an all-day series writes the master's RRULE UNTIL as
// a bare DATE (20240104), not a UTC DATE-TIME.
func TestDeleteEvent_AllDayTerminateUntilIsDateValue(t *testing.T) {
	repo := &stubCalendarRepo{
		obj: &calendar.CalendarObject{
			UUID:          "obj-uuid",
			UID:           "allday-series-1",
			ComponentType: "VEVENT",
			ICalData:      allDaySeriesICal,
			IsAllDay:      true,
		},
	}

	uc := NewDeleteEventUseCase(repo)
	if err := uc.Execute(context.Background(), "obj-uuid", "this_and_future", "20240105"); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if repo.updated == nil {
		t.Fatal("expected the object to be persisted via UpdateCalendarObject")
	}

	cal, err := ical.NewDecoder(strings.NewReader(repo.updated.ICalData)).Decode()
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
		t.Fatalf("no RRULE with UNTIL found after terminate, iCal:\n%s", repo.updated.ICalData)
	}
	if until != "20240104" {
		t.Errorf("expected DATE-value UNTIL %q, got %q (iCal:\n%s)", "20240104", until, repo.updated.ICalData)
	}
}
