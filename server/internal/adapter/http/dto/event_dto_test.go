package dto

import "testing"

func ptr[T any](v T) *T { return &v }

func TestRecurrenceRuleDTO_ToRRule_NormalizesRFC3339Until(t *testing.T) {
	r := &RecurrenceRuleDTO{Frequency: "WEEKLY", Until: ptr("2026-08-01T00:00:00+02:00")}
	got := r.ToRRule(false)
	// +02:00 offset → UTC is two hours earlier: 2026-07-31T22:00:00Z.
	want := "FREQ=WEEKLY;UNTIL=20260731T220000Z"
	if got != want {
		t.Fatalf("ToRRule() = %q, want %q", got, want)
	}
}

// For an all-day series, UNTIL must be a bare DATE matching DTSTART;VALUE=DATE,
// and it must keep the sender's local calendar date so the chosen end date's own
// (inclusive) occurrence is not dropped east of UTC. The same +02:00 input that
// the timed case renders as 20260731T220000Z must render as 20260801 here.
func TestRecurrenceRuleDTO_ToRRule_AllDayUntilIsDateValue(t *testing.T) {
	r := &RecurrenceRuleDTO{Frequency: "WEEKLY", Until: ptr("2026-08-01T00:00:00+02:00")}
	got := r.ToRRule(true)
	want := "FREQ=WEEKLY;UNTIL=20260801"
	if got != want {
		t.Fatalf("ToRRule(true) = %q, want %q", got, want)
	}
}

func TestRecurrenceRuleDTO_ToRRule_PassesThroughICalBasicUntil(t *testing.T) {
	// An already-iCal value must be forwarded unchanged.
	r := &RecurrenceRuleDTO{Frequency: "WEEKLY", Until: ptr("20260801T000000Z")}
	got := r.ToRRule(false)
	want := "FREQ=WEEKLY;UNTIL=20260801T000000Z"
	if got != want {
		t.Fatalf("ToRRule() = %q, want %q", got, want)
	}
}

func TestRecurrenceRuleDTO_ToRRule_EmptyUntilOmitted(t *testing.T) {
	r := &RecurrenceRuleDTO{Frequency: "DAILY", Until: ptr("")}
	got := r.ToRRule(false)
	want := "FREQ=DAILY"
	if got != want {
		t.Fatalf("ToRRule() = %q, want %q", got, want)
	}
}

func TestRecurrenceRuleDTO_ToRRule_UTCInputUnchanged(t *testing.T) {
	// RFC 3339 already in UTC round-trips to the same instant.
	r := &RecurrenceRuleDTO{Frequency: "MONTHLY", Interval: 2, Until: ptr("2026-08-01T12:30:00Z")}
	got := r.ToRRule(false)
	want := "FREQ=MONTHLY;INTERVAL=2;UNTIL=20260801T123000Z"
	if got != want {
		t.Fatalf("ToRRule() = %q, want %q", got, want)
	}
}
