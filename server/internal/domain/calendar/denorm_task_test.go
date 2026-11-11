package calendar

import (
	"strings"
	"testing"
	"time"
)

// todo wraps VTODO property lines in a VCALENDAR with a fixed UID.
func todo(props ...string) string {
	body := append([]string{"BEGIN:VTODO", "UID:t@x", "SUMMARY:Task"}, props...)
	return wrap(strings.Join(append(body, "END:VTODO"), "\r\n"))
}

func utc(y int, m time.Month, d, h, min int) *time.Time {
	t := time.Date(y, m, d, h, min, 0, 0, time.UTC)
	return &t
}

func intPtr(v int) *int { return &v }

// taskFields is the expected value of every task column plus IsAllDay.
type taskFields struct {
	Status          string
	DueAt           *time.Time
	CompletedAt     *time.Time
	PercentComplete *int
	Priority        *int
	IsAllDay        bool
}

func assertTaskFields(t *testing.T, o *CalendarObject, want taskFields) {
	t.Helper()
	if o.Status != want.Status {
		t.Errorf("Status = %q, want %q", o.Status, want.Status)
	}
	assertUTCTime(t, "DueAt", o.DueAt, want.DueAt)
	assertUTCTime(t, "CompletedAt", o.CompletedAt, want.CompletedAt)
	assertIntPtr(t, "PercentComplete", o.PercentComplete, want.PercentComplete)
	assertIntPtr(t, "Priority", o.Priority, want.Priority)
	if o.IsAllDay != want.IsAllDay {
		t.Errorf("IsAllDay = %v, want %v", o.IsAllDay, want.IsAllDay)
	}
}

func assertUTCTime(t *testing.T, name string, got, want *time.Time) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s = %v, want %v", name, got, want)
	case !got.Equal(*want):
		t.Errorf("%s = %v, want %v", name, *got, *want)
	case got.Location() != time.UTC:
		t.Errorf("%s location = %v, want UTC", name, got.Location())
	}
}

func assertIntPtr(t *testing.T, name string, got, want *int) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s = %v, want %v", name, deref(got), deref(want))
	case *got != *want:
		t.Errorf("%s = %d, want %d", name, *got, *want)
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestPopulateTaskFields(t *testing.T) {
	cases := []struct {
		name  string
		props []string
		want  taskFields
	}{
		{
			name:  "bare task: no STATUS, no dates",
			props: nil,
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "absent STATUS without COMPLETED is NEEDS-ACTION",
			props: []string{"DUE:20260115T100000Z"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 10, 0)},
		},
		{
			name:  "absent STATUS with COMPLETED is COMPLETED",
			props: []string{"COMPLETED:20260110T083000Z"},
			want:  taskFields{Status: TaskStatusCompleted, CompletedAt: utc(2026, 1, 10, 8, 30)},
		},
		{
			name:  "absent STATUS with an unparseable COMPLETED is still COMPLETED",
			props: []string{"COMPLETED:yesterday"},
			want:  taskFields{Status: TaskStatusCompleted},
		},
		{
			name:  "empty STATUS counts as absent",
			props: []string{"STATUS:", "COMPLETED:20260110T083000Z"},
			want:  taskFields{Status: TaskStatusCompleted, CompletedAt: utc(2026, 1, 10, 8, 30)},
		},
		{
			name:  "explicit STATUS wins over COMPLETED",
			props: []string{"STATUS:NEEDS-ACTION", "COMPLETED:20260110T083000Z"},
			want:  taskFields{Status: TaskStatusNeedsAction, CompletedAt: utc(2026, 1, 10, 8, 30)},
		},
		{
			name:  "lower-case STATUS is upper-cased",
			props: []string{"STATUS:in-process"},
			want:  taskFields{Status: TaskStatusInProcess},
		},
		{
			name:  "mixed-case STATUS cancelled",
			props: []string{"STATUS:Cancelled"},
			want:  taskFields{Status: TaskStatusCancelled},
		},
		{
			name:  "STATUS COMPLETED",
			props: []string{"STATUS:COMPLETED", "COMPLETED:20260110T083000Z", "PERCENT-COMPLETE:100"},
			want:  taskFields{Status: TaskStatusCompleted, CompletedAt: utc(2026, 1, 10, 8, 30), PercentComplete: intPtr(100)},
		},
		{
			name:  "unknown STATUS is normalised to NEEDS-ACTION",
			props: []string{"STATUS:WAITING"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "VEVENT-only STATUS on a task is normalised to NEEDS-ACTION",
			props: []string{"STATUS:TENTATIVE", "COMPLETED:20260110T083000Z"},
			want:  taskFields{Status: TaskStatusNeedsAction, CompletedAt: utc(2026, 1, 10, 8, 30)},
		},
		{
			name:  "DUE;VALUE=DATE is midnight UTC and all-day",
			props: []string{"DUE;VALUE=DATE:20260115"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 0, 0), IsAllDay: true},
		},
		{
			name:  "DUE;TZID=Europe/Berlin is converted to UTC (CET, winter)",
			props: []string{"DUE;TZID=Europe/Berlin:20260115T100000"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 9, 0)},
		},
		{
			name:  "DUE;TZID=Europe/Berlin is converted to UTC (CEST, summer)",
			props: []string{"DUE;TZID=Europe/Berlin:20260715T100000"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 7, 15, 8, 0)},
		},
		{
			name:  "floating DUE is read as UTC wall time",
			props: []string{"DUE:20260115T100000"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 10, 0)},
		},
		{
			name:  "UTC DUE",
			props: []string{"DUE:20260115T100000Z"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 10, 0)},
		},
		{
			name:  "DUE wins over DTSTART",
			props: []string{"DTSTART:20260110T080000Z", "DUE:20260115T100000Z"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 10, 0)},
		},
		{
			name:  "DTSTART+DURATION without DUE",
			props: []string{"DTSTART:20260115T080000Z", "DURATION:PT2H30M"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 10, 30)},
		},
		{
			name:  "DTSTART;TZID+DURATION without DUE is UTC",
			props: []string{"DTSTART;TZID=Europe/Berlin:20260115T080000", "DURATION:PT1H"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 8, 0)},
		},
		{
			name:  "all-day DTSTART+DURATION without DUE",
			props: []string{"DTSTART;VALUE=DATE:20260115", "DURATION:P2D"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 17, 0, 0), IsAllDay: true},
		},
		{
			name:  "DTSTART without DUE or DURATION has no due date",
			props: []string{"DTSTART:20260115T080000Z"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "all-day follows DTSTART when there is no DUE",
			props: []string{"DTSTART;VALUE=DATE:20260115"},
			want:  taskFields{Status: TaskStatusNeedsAction, IsAllDay: true},
		},
		{
			name:  "all-day follows DUE, not DTSTART",
			props: []string{"DTSTART;VALUE=DATE:20260110", "DUE:20260115T100000Z"},
			want:  taskFields{Status: TaskStatusNeedsAction, DueAt: utc(2026, 1, 15, 10, 0)},
		},
		{
			name:  "COMPLETED with TZID is converted to UTC",
			props: []string{"STATUS:COMPLETED", "COMPLETED;TZID=Europe/Berlin:20260110T093000"},
			want:  taskFields{Status: TaskStatusCompleted, CompletedAt: utc(2026, 1, 10, 8, 30)},
		},
		{
			name:  "PERCENT-COMPLETE:150 is clamped to 100",
			props: []string{"PERCENT-COMPLETE:150"},
			want:  taskFields{Status: TaskStatusNeedsAction, PercentComplete: intPtr(100)},
		},
		{
			name:  "negative PERCENT-COMPLETE is clamped to 0",
			props: []string{"PERCENT-COMPLETE:-5"},
			want:  taskFields{Status: TaskStatusNeedsAction, PercentComplete: intPtr(0)},
		},
		{
			name:  "PERCENT-COMPLETE in range is kept",
			props: []string{"PERCENT-COMPLETE:40"},
			want:  taskFields{Status: TaskStatusNeedsAction, PercentComplete: intPtr(40)},
		},
		{
			name:  "PERCENT-COMPLETE:0 is kept as 0, not nil",
			props: []string{"PERCENT-COMPLETE:0"},
			want:  taskFields{Status: TaskStatusNeedsAction, PercentComplete: intPtr(0)},
		},
		{
			name:  "PRIORITY:0 (undefined) is nil",
			props: []string{"PRIORITY:0"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "PRIORITY:1 is kept",
			props: []string{"PRIORITY:1"},
			want:  taskFields{Status: TaskStatusNeedsAction, Priority: intPtr(1)},
		},
		{
			name:  "PRIORITY:9 is kept",
			props: []string{"PRIORITY:9"},
			want:  taskFields{Status: TaskStatusNeedsAction, Priority: intPtr(9)},
		},
		{
			name: "malformed values leave every field nil",
			props: []string{
				"STATUS:in-process",
				"DUE:not-a-date",
				"COMPLETED:yesterday",
				"PERCENT-COMPLETE:half",
				"PRIORITY:high",
			},
			want: taskFields{Status: TaskStatusInProcess},
		},
		{
			name:  "DUE with an unknown TZID is nil",
			props: []string{"DUE;TZID=Not/A_Zone:20260115T100000"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "malformed DUE does not fall back to DTSTART+DURATION",
			props: []string{"DTSTART:20260115T080000Z", "DURATION:PT1H", "DUE:soon"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "malformed DURATION leaves DueAt nil",
			props: []string{"DTSTART:20260115T080000Z", "DURATION:two hours"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "negative DURATION leaves DueAt nil",
			props: []string{"DTSTART:20260115T080000Z", "DURATION:-PT1H"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "out-of-range PRIORITY is nil",
			props: []string{"PRIORITY:12"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
		{
			name:  "negative PRIORITY is nil",
			props: []string{"PRIORITY:-1"},
			want:  taskFields{Status: TaskStatusNeedsAction},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &CalendarObject{ICalData: todo(tc.props...)}
			if err := o.PopulateDenormFieldsFromICal(); err != nil {
				t.Fatalf("PopulateDenormFieldsFromICal() error = %v, want nil", err)
			}
			if o.ComponentType != ComponentToDo {
				t.Fatalf("ComponentType = %q, want %q", o.ComponentType, ComponentToDo)
			}
			assertTaskFields(t, o, tc.want)
		})
	}
}

func TestPopulateTaskFields_EventKeepsColumnsEmpty(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:e@x\r\nSTATUS:CONFIRMED\r\nPRIORITY:1\r\nDTSTART;VALUE=DATE:20260115\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	// VEVENT IsAllDay is unchanged: it follows DTSTART;VALUE=DATE.
	assertTaskFields(t, o, taskFields{IsAllDay: true})
}

func TestPopulateTaskFields_EventClearsStaleTaskColumns(t *testing.T) {
	// A PUT may replace a task with an event under the same object: the
	// re-derivation must drop every task value the row carried.
	o := &CalendarObject{ICalData: todo(
		"STATUS:COMPLETED",
		"DUE;VALUE=DATE:20260115",
		"COMPLETED:20260110T083000Z",
		"PERCENT-COMPLETE:100",
		"PRIORITY:5",
	)}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	assertTaskFields(t, o, taskFields{
		Status:          TaskStatusCompleted,
		DueAt:           utc(2026, 1, 15, 0, 0),
		CompletedAt:     utc(2026, 1, 10, 8, 30),
		PercentComplete: intPtr(100),
		Priority:        intPtr(5),
		IsAllDay:        true,
	})

	o.ICalData = wrap("BEGIN:VEVENT\r\nUID:t@x\r\nDTSTART:20260115T100000Z\r\nDTEND:20260115T110000Z\r\nEND:VEVENT")
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.ComponentType != ComponentEvent {
		t.Fatalf("ComponentType = %q, want %q", o.ComponentType, ComponentEvent)
	}
	assertTaskFields(t, o, taskFields{})
}

func TestPopulateTaskFields_UsesMasterNotOverride(t *testing.T) {
	// The task columns come from the master (no RECURRENCE-ID), even when an
	// override is listed first.
	o := &CalendarObject{ICalData: wrap(strings.Join([]string{
		"BEGIN:VTODO", "UID:r@x", "RECURRENCE-ID:20260108T090000Z", "DTSTART:20260108T090000Z",
		"STATUS:COMPLETED", "COMPLETED:20260108T100000Z", "PRIORITY:1", "END:VTODO",
		"BEGIN:VTODO", "UID:r@x", "DTSTART:20260101T090000Z", "DUE:20260101T100000Z",
		"RRULE:FREQ=WEEKLY;COUNT=3", "PRIORITY:5", "END:VTODO",
	}, "\r\n"))}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	assertTaskFields(t, o, taskFields{
		Status:   TaskStatusNeedsAction,
		DueAt:    utc(2026, 1, 1, 10, 0),
		Priority: intPtr(5),
	})
}

func TestPopulateTaskFields_RecurrenceEndTimeStillComputed(t *testing.T) {
	// Story 17 matches recurring VTODOs in CalDAV time-range queries through
	// RecurrenceEndTime, so the task path must keep computing it.
	o := &CalendarObject{ICalData: todo(
		"DTSTART:20260101T090000Z",
		"DUE:20260101T100000Z",
		"RRULE:FREQ=DAILY;COUNT=3",
	)}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	// 3 daily occurrences from 2026-01-01T09:00; the last starts 2026-01-03T09:00.
	// (componentEnd ignores DUE when DTSTART is present, so the occurrence length
	// is not added today; the bound only has to cover the last occurrence.)
	lastStart := time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)
	if o.RecurrenceEndTime == nil || o.RecurrenceEndTime.Before(lastStart) || !o.RecurrenceEndTime.Before(farFuture) {
		t.Errorf("RecurrenceEndTime = %v, want a bounded instant at or after %v", o.RecurrenceEndTime, lastStart)
	}

	unbounded := &CalendarObject{ICalData: todo("DTSTART:20260101T090000Z", "RRULE:FREQ=WEEKLY")}
	if err := unbounded.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if unbounded.RecurrenceEndTime == nil || !unbounded.RecurrenceEndTime.Equal(farFuture) {
		t.Errorf("unbounded RecurrenceEndTime = %v, want %v", unbounded.RecurrenceEndTime, farFuture)
	}
}
