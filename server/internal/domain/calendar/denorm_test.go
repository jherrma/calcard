package calendar

import (
	"testing"
	"time"
)

func wrap(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + body + "\r\nEND:VCALENDAR\r\n"
}

func TestPopulateDenormFields_PlainEvent(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nSUMMARY:Hi\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.ComponentType != "VEVENT" {
		t.Errorf("ComponentType = %q", o.ComponentType)
	}
	if o.Summary != "Hi" || o.UID != "a@x" {
		t.Errorf("summary/uid = %q/%q", o.Summary, o.UID)
	}
	if o.RecurrenceEndTime != nil {
		t.Errorf("RecurrenceEndTime should be nil for non-recurring, got %v", o.RecurrenceEndTime)
	}
	if o.StartTime == nil || o.EndTime == nil {
		t.Fatal("start/end should be set")
	}
}

func TestPopulateDenormFields_UnboundedRecurring(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=WEEKLY\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.RecurrenceEndTime == nil || !o.RecurrenceEndTime.Equal(farFuture) {
		t.Errorf("unbounded RRULE should map to farFuture, got %v", o.RecurrenceEndTime)
	}
}

func TestPopulateDenormFields_CountRecurring(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=5\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	// 5 daily occurrences: last starts 2026-01-05T10, ends 11:00.
	want := time.Date(2026, 1, 5, 11, 0, 0, 0, time.UTC)
	if o.RecurrenceEndTime == nil || !o.RecurrenceEndTime.Equal(want) {
		t.Errorf("RecurrenceEndTime = %v, want %v", o.RecurrenceEndTime, want)
	}
}

func TestPopulateDenormFields_UntilRecurring(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;UNTIL=20260110T100000Z\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 10, 11, 0, 0, 0, time.UTC)
	if o.RecurrenceEndTime == nil || !o.RecurrenceEndTime.Equal(want) {
		t.Errorf("RecurrenceEndTime = %v, want %v", o.RecurrenceEndTime, want)
	}
}

func TestPopulateDenormFields_VTodo(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VTODO\r\nUID:t@x\r\nSUMMARY:Task\r\nEND:VTODO")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.ComponentType != "VTODO" {
		t.Errorf("ComponentType = %q, want VTODO", o.ComponentType)
	}
	if o.StartTime != nil || o.EndTime != nil {
		t.Errorf("VTODO without dates should have nil times")
	}
}

func TestPopulateDenormFields_AllDay(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART;VALUE=DATE:20260101\r\nDTEND;VALUE=DATE:20260102\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if !o.IsAllDay {
		t.Error("expected IsAllDay true for VALUE=DATE DTSTART")
	}
}

func TestPopulateDenormFields_DurationNoDtend(t *testing.T) {
	// DTSTART + DURATION, no DTEND: end must resolve to start+duration.
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20260101T100000Z\r\nDURATION:PT1H30M\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.EndTime == nil {
		t.Fatal("EndTime must be derived from DURATION, got nil")
	}
	want := time.Date(2026, 1, 1, 11, 30, 0, 0, time.UTC)
	if !o.EndTime.Equal(want) {
		t.Errorf("EndTime = %v, want %v", o.EndTime, want)
	}
}

func TestPopulateDenormFields_AllDayDefaultEnd(t *testing.T) {
	// All-day DTSTART;VALUE=DATE with DTEND omitted: RFC 5545 default is +1 day.
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART;VALUE=DATE:20260101\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.EndTime == nil {
		t.Fatal("EndTime must default to +1 day for all-day DTSTART, got nil")
	}
	want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if !o.EndTime.Equal(want) {
		t.Errorf("EndTime = %v, want %v", o.EndTime, want)
	}
}

func TestPopulateDenormFields_VTodoDueOnly(t *testing.T) {
	// A VTODO carrying only DUE (no DTSTART/DTEND) must expose an EndTime so the
	// REST listing window does not drop it.
	o := &CalendarObject{ICalData: wrap("BEGIN:VTODO\r\nUID:t@x\r\nSUMMARY:Task\r\nDUE:20260101T170000Z\r\nEND:VTODO")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.EndTime == nil {
		t.Fatal("EndTime must fall back to VTODO DUE, got nil")
	}
	want := time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC)
	if !o.EndTime.Equal(want) {
		t.Errorf("EndTime = %v, want %v", o.EndTime, want)
	}
}

func TestPopulateDenormFields_CountRecurringWithDuration(t *testing.T) {
	// Bounded COUNT series using DURATION (no DTEND): the final occurrence's
	// tail must be included in RecurrenceEndTime.
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20260101T100000Z\r\nDURATION:PT1H\r\nRRULE:FREQ=DAILY;COUNT=5\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	// 5 daily occurrences: last starts 2026-01-05T10, +1h duration = 11:00.
	want := time.Date(2026, 1, 5, 11, 0, 0, 0, time.UTC)
	if o.RecurrenceEndTime == nil || !o.RecurrenceEndTime.Equal(want) {
		t.Errorf("RecurrenceEndTime = %v, want %v", o.RecurrenceEndTime, want)
	}
}

func TestPopulateDenormFields_BareComponentErrors(t *testing.T) {
	o := &CalendarObject{ICalData: "BEGIN:VEVENT\r\nUID:a@x\r\nEND:VEVENT\r\n"}
	if err := o.PopulateDenormFieldsFromICal(); err == nil {
		t.Error("expected error for bare (non-VCALENDAR) input")
	}
}
