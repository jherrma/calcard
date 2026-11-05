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

func TestPopulateDenormFields_LargeCountRecurring(t *testing.T) {
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=SECONDLY;COUNT=1000000000\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	if o.RecurrenceEndTime == nil || !o.RecurrenceEndTime.Equal(farFuture) {
		t.Errorf("large COUNT should map to farFuture, got %v", o.RecurrenceEndTime)
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
	// The explicit VALUE=DATE DTEND must resolve to midnight of that date.
	if o.EndTime == nil {
		t.Fatal("EndTime must be set for an all-day event with an explicit DTEND")
	}
	if want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC); !o.EndTime.Equal(want) {
		t.Errorf("EndTime = %v, want %v", o.EndTime, want)
	}
}

func TestPopulateDenormFields_RDateOnly(t *testing.T) {
	// An event whose recurrence is expressed only via RDATE (no RRULE) must not
	// be treated as non-recurring: RecurrenceEndTime has to reach the latest
	// RDATE (+duration) so later listing windows still surface it.
	o := &CalendarObject{ICalData: wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRDATE:20260115T100000Z,20260201T100000Z\r\nEND:VEVENT")}
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	// Latest RDATE 2026-02-01T10:00 + 1h duration = 11:00.
	want := time.Date(2026, 2, 1, 11, 0, 0, 0, time.UTC)
	if o.RecurrenceEndTime == nil || !o.RecurrenceEndTime.Equal(want) {
		t.Errorf("RecurrenceEndTime = %v, want %v", o.RecurrenceEndTime, want)
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

func alarmEv(extra string) string {
	return wrap("BEGIN:VEVENT\r\nUID:a@x\r\n" + extra + "END:VEVENT")
}

func valarm(lines ...string) string {
	s := "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:x\r\n"
	for _, l := range lines {
		s += l + "\r\n"
	}
	return s + "END:VALARM\r\n"
}

func shift(t *time.Time, hours int) *time.Time {
	r := t.Add(time.Duration(hours) * time.Hour)
	return &r
}

func mustBounds(t *testing.T, o *CalendarObject, lo, hi *time.Time) {
	t.Helper()
	if err := o.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	check := func(name string, got, want *time.Time) {
		if (got == nil) != (want == nil) || (got != nil && (!got.Equal(*want) || got.Location() != time.UTC)) {
			t.Errorf("%s = %v, want %v (UTC)", name, got, want)
		}
	}
	check("AlarmFirstAt", o.AlarmFirstAt, lo)
	check("AlarmLastAt", o.AlarmLastAt, hi)
}

func TestAlarmBounds_SetAndCleared(t *testing.T) {
	ev := "DTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\n"
	o := &CalendarObject{ICalData: alarmEv(ev + valarm("TRIGGER:-PT15M"))}
	mustBounds(t, o, utc(2026, 11, 1, 8, 45), utc(2026, 11, 1, 8, 45))
	// Remove the VALARM: bounds cleared on re-derivation.
	o.ICalData = alarmEv(ev)
	mustBounds(t, o, nil, nil)
	// Add it back, with an absolute and a relative-to-end one.
	o.ICalData = alarmEv(ev + valarm("TRIGGER:-PT15M") + valarm("TRIGGER;RELATED=END:PT1H") + valarm("TRIGGER;VALUE=DATE-TIME:20261031T000000Z"))
	mustBounds(t, o, utc(2026, 10, 31, 0, 0), utc(2026, 11, 1, 11, 0))
}

func TestAlarmBounds_NullRules(t *testing.T) {
	ev := "DTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\n"
	for name, data := range map[string]string{
		"no alarm":          alarmEv(ev),
		"action none":       alarmEv(ev + "BEGIN:VALARM\r\nACTION:NONE\r\nTRIGGER:-PT5M\r\nEND:VALARM\r\n"),
		"malformed trigger": alarmEv(ev + valarm("TRIGGER:-P15M") + valarm("TRIGGER:garbage")),
		"no trigger":        alarmEv(ev + valarm()),
		"cancelled":         alarmEv(ev + "STATUS:CANCELLED\r\n" + valarm("TRIGGER:-PT5M")),
		"no anchor":         alarmEv(valarm("TRIGGER:-PT5M")),
	} {
		t.Run(name, func(t *testing.T) {
			mustBounds(t, &CalendarObject{ICalData: data}, nil, nil)
		})
	}
}

func TestAlarmBounds_Task(t *testing.T) {
	task := func(extra string) string {
		return wrap("BEGIN:VTODO\r\nUID:t@x\r\nDUE:20261105T090000Z\r\n" + extra + valarm("TRIGGER;RELATED=END:-PT1H") + "END:VTODO")
	}
	mustBounds(t, &CalendarObject{ICalData: task("")}, utc(2026, 11, 5, 8, 0), utc(2026, 11, 5, 8, 0))
	// Complete, then uncomplete.
	mustBounds(t, &CalendarObject{ICalData: task("STATUS:COMPLETED\r\n")}, nil, nil)
	mustBounds(t, &CalendarObject{ICalData: task("STATUS:CANCELLED\r\n")}, nil, nil)
	mustBounds(t, &CalendarObject{ICalData: task("COMPLETED:20261104T000000Z\r\n")}, nil, nil)
	mustBounds(t, &CalendarObject{ICalData: task("STATUS:NEEDS-ACTION\r\n")}, utc(2026, 11, 5, 8, 0), utc(2026, 11, 5, 8, 0))
}

func TestAlarmBounds_Floating(t *testing.T) {
	// Floating DTSTART: +-14 h around the wall clock read as UTC.
	o := &CalendarObject{ICalData: alarmEv("DTSTART:20261101T090000\r\nDTEND:20261101T100000\r\n" + valarm("TRIGGER:-PT1H"))}
	mustBounds(t, o, shift(utc(2026, 11, 1, 8, 0), -15), shift(utc(2026, 11, 1, 8, 0), 15))
	// Floating absolute trigger: exactly +-14 h.
	o = &CalendarObject{ICalData: alarmEv("DTSTART:20261101T090000Z\r\n" + valarm("TRIGGER;VALUE=DATE-TIME:20261101T080000"))}
	mustBounds(t, o, shift(utc(2026, 11, 1, 8, 0), -14), shift(utc(2026, 11, 1, 8, 0), 14))
}

func TestAlarmBounds_TZID(t *testing.T) {
	// 09:00 Berlin (CET, +1) -> 08:00Z; -PT15M -> 07:45Z.
	o := &CalendarObject{ICalData: alarmEv("DTSTART;TZID=Europe/Berlin:20261201T090000\r\nDTEND;TZID=Europe/Berlin:20261201T100000\r\n" + valarm("TRIGGER:-PT15M"))}
	mustBounds(t, o, utc(2026, 12, 1, 7, 45), utc(2026, 12, 1, 7, 45))
}

func TestAlarmBounds_Recurring(t *testing.T) {
	ev := "DTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\n"
	// Unbounded: farFuture.
	o := &CalendarObject{ICalData: alarmEv(ev + "RRULE:FREQ=DAILY\r\n" + valarm("TRIGGER:-PT15M"))}
	mustBounds(t, o, utc(2026, 11, 1, 8, 45), &farFuture)
	// COUNT=3 daily: last end 2026-11-03 10:00, hi = last end + max(o,0)=0.
	o = &CalendarObject{ICalData: alarmEv(ev + "RRULE:FREQ=DAILY;COUNT=3\r\n" + valarm("TRIGGER:-PT15M"))}
	mustBounds(t, o, utc(2026, 11, 1, 8, 45), utc(2026, 11, 3, 10, 0))
	// Positive offset extends hi.
	o = &CalendarObject{ICalData: alarmEv(ev + "RRULE:FREQ=DAILY;COUNT=3\r\n" + valarm("TRIGGER;RELATED=END:PT2H"))}
	mustBounds(t, o, utc(2026, 11, 1, 12, 0), utc(2026, 11, 3, 12, 0))
	// Nominal day: 1 h slack both sides.
	o = &CalendarObject{ICalData: alarmEv(ev + "RRULE:FREQ=DAILY;COUNT=3\r\n" + valarm("TRIGGER:-P1D"))}
	mustBounds(t, o, utc(2026, 10, 31, 8, 0), utc(2026, 11, 3, 11, 0))
	// Absolute alarm on a master: once.
	o = &CalendarObject{ICalData: alarmEv(ev + "RRULE:FREQ=DAILY\r\n" + valarm("TRIGGER;VALUE=DATE-TIME:20261031T000000Z"))}
	mustBounds(t, o, utc(2026, 10, 31, 0, 0), utc(2026, 10, 31, 0, 0))
}

func TestAlarmBounds_RDateOnly(t *testing.T) {
	// RecurrenceEndTime counts RDATE (last end 2026-11-10 10:00).
	o := &CalendarObject{ICalData: alarmEv("DTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\nRDATE:20261110T090000Z\r\n" + valarm("TRIGGER:-PT15M"))}
	mustBounds(t, o, utc(2026, 11, 1, 8, 45), utc(2026, 11, 10, 10, 0))
}

func TestAlarmBounds_Override(t *testing.T) {
	// An override's own alarm contributes [a+o, a+o]; the master's covers the series.
	data := wrap("BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\nRRULE:FREQ=DAILY;COUNT=2\r\n" + valarm("TRIGGER:-PT15M") + "END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:a@x\r\nRECURRENCE-ID:20261102T090000Z\r\nDTSTART:20261102T150000Z\r\nDTEND:20261102T160000Z\r\n" + valarm("TRIGGER:-PT1H") + "END:VEVENT")
	mustBounds(t, &CalendarObject{ICalData: data}, utc(2026, 11, 1, 8, 45), utc(2026, 11, 2, 14, 0))
}

func TestAlarmBounds_AllDay(t *testing.T) {
	// DATE anchor: 00:00 wall + -PT15H = previous day 09:00 wall; +-15 h (14 h + 1 h).
	o := &CalendarObject{ICalData: alarmEv("DTSTART;VALUE=DATE:20261101\r\n" + valarm("TRIGGER:-PT15H"))}
	mid := utc(2026, 10, 31, 9, 0)
	mustBounds(t, o, shift(mid, -15), shift(mid, 15))
}

// TestAlarmBounds_ContainExpandedInstants checks the over-include rule: every
// instant ExpandAlarms returns, for several owner zones, lies inside the bounds.
func TestAlarmBounds_ContainExpandedInstants(t *testing.T) {
	zones := []string{"UTC", "Europe/Berlin", "America/Los_Angeles", "Pacific/Kiritimati", "Pacific/Pago_Pago"}
	starts := []string{
		"DTSTART:20261024T090000Z", "DTSTART;TZID=Europe/Berlin:20261024T083000", "DTSTART:20261024T090000",
		"DTSTART;VALUE=DATE:20261024", "DTSTART;TZID=America/New_York:20261024T230000",
	}
	rules := []string{"", "RRULE:FREQ=DAILY;COUNT=12", "RRULE:FREQ=WEEKLY;UNTIL=20261231T000000Z", "RRULE:FREQ=DAILY"}
	triggers := []string{"TRIGGER:-PT15M", "TRIGGER:-P1D", "TRIGGER:-P1DT15H", "TRIGGER:PT9H", "TRIGGER;RELATED=END:PT1H",
		"TRIGGER;RELATED=END:-P2D", "TRIGGER;VALUE=DATE-TIME:20261023T120000Z", "TRIGGER;VALUE=DATE-TIME:20261023T120000"}
	from, until := *utc(2026, 1, 1, 0, 0), *utc(2028, 1, 1, 0, 0)
	for _, s := range starts {
		for _, r := range rules {
			for _, tr := range triggers {
				data := alarmEv(s + "\r\n" + func() string {
					if r == "" {
						return ""
					}
					return r + "\r\n"
				}() + valarm(tr))
				o := &CalendarObject{ICalData: data}
				if err := o.PopulateDenormFieldsFromICal(); err != nil {
					t.Fatal(err)
				}
				for _, z := range zones {
					loc, _ := time.LoadLocation(z)
					inst, _, err := ExpandAlarms(o, from, until, loc)
					if err != nil {
						t.Fatal(err)
					}
					for _, in := range inst {
						if o.AlarmFirstAt == nil || in.TriggerAt.Before(*o.AlarmFirstAt) || in.TriggerAt.After(*o.AlarmLastAt) {
							t.Errorf("%q %q %q zone %s: instant %v outside [%v, %v]", s, r, tr, z, in.TriggerAt, o.AlarmFirstAt, o.AlarmLastAt)
						}
					}
				}
			}
		}
	}
}
