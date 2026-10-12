package event

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// storedProp decodes the persisted iCalendar and returns the master VEVENT's
// property, failing the test if it is missing.
func storedProp(t *testing.T, icalData, name string) *ical.Prop {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(icalData)).Decode()
	if err != nil {
		t.Fatalf("failed to decode persisted iCal: %v", err)
	}
	ev := masterEvent(cal.Events())
	if ev == nil {
		t.Fatalf("no master VEVENT in:\n%s", icalData)
	}
	p := ev.Props.Get(name)
	if p == nil {
		t.Fatalf("no %s in:\n%s", name, icalData)
	}
	return p
}

// assertUTCDateTime checks that a timed property is written in the UTC form
// (no TZID parameter, trailing Z) with the expected value.
func assertUTCDateTime(t *testing.T, p *ical.Prop, want string) {
	t.Helper()
	// Check the raw map: an empty TZID (the F22 symptom) reads back as "" via Get.
	if tzid, ok := p.Params[ical.PropTimezoneID]; ok {
		t.Errorf("%s must carry no TZID parameter, got TZID=%q", p.Name, tzid)
	}
	if p.Value != want {
		t.Errorf("%s = %q, want %q", p.Name, p.Value, want)
	}
}

// mustParseRFC3339 parses the way the REST handler (encoding/json) and the MCP
// tools do, so the value carries the same time.Location a client's request does.
func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestCreateEvent_OffsetWithoutTimezoneIsStoredAsUTC is the F22 regression: a
// start/end with a numeric offset and no `timezone` parses into an unnamed
// FixedZone, which go-ical wrote as DTSTART;TZID=:20300301T100000 — an empty
// TZID every reader takes for UTC, so the event moved by the offset. It must be
// stored as the same instant in UTC.
func TestCreateEvent_OffsetWithoutTimezoneIsStoredAsUTC(t *testing.T) {
	uc := NewCreateEventUseCase(&stubCalendarRepo{})
	obj, err := uc.Execute(context.Background(), CreateEventInput{
		CalendarID: 1,
		Summary:    "Offset",
		Start:      mustParseRFC3339(t, "2030-03-01T10:00:00+05:00"),
		End:        mustParseRFC3339(t, "2030-03-01T11:00:00+05:00"),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	assertUTCDateTime(t, storedProp(t, obj.ICalData, ical.PropDateTimeStart), "20300301T050000Z")
	assertUTCDateTime(t, storedProp(t, obj.ICalData, ical.PropDateTimeEnd), "20300301T060000Z")
	if want := time.Date(2030, 3, 1, 5, 0, 0, 0, time.UTC); obj.StartTime == nil || !obj.StartTime.Equal(want) {
		t.Errorf("denormalized start = %v, want %v", obj.StartTime, want)
	}
}

// TestCreateEvent_LocalZoneIsStoredAsUTC covers the other half of F22: an
// offset equal to the server's own parses into time.Local, which go-ical wrote
// as TZID=Local — a zone no client can resolve.
func TestCreateEvent_LocalZoneIsStoredAsUTC(t *testing.T) {
	start := time.Date(2030, 3, 1, 10, 0, 0, 0, time.Local)
	uc := NewCreateEventUseCase(&stubCalendarRepo{})
	obj, err := uc.Execute(context.Background(), CreateEventInput{
		CalendarID: 1,
		Summary:    "Local",
		Start:      start,
		End:        start.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	assertUTCDateTime(t, storedProp(t, obj.ICalData, ical.PropDateTimeStart), start.UTC().Format("20060102T150405Z"))
}

// TestCreateEvent_TimezoneKeepsTZID guards the path F22 must not change: with a
// `timezone` the instant is converted into that zone and written with its TZID.
func TestCreateEvent_TimezoneKeepsTZID(t *testing.T) {
	uc := NewCreateEventUseCase(&stubCalendarRepo{})
	obj, err := uc.Execute(context.Background(), CreateEventInput{
		CalendarID: 1,
		Summary:    "Berlin",
		Start:      mustParseRFC3339(t, "2030-03-01T10:00:00+05:00"),
		End:        mustParseRFC3339(t, "2030-03-01T11:00:00+05:00"),
		Timezone:   "Europe/Berlin",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	p := storedProp(t, obj.ICalData, ical.PropDateTimeStart)
	if got := p.Params.Get(ical.PropTimezoneID); got != "Europe/Berlin" {
		t.Errorf("DTSTART TZID = %q, want Europe/Berlin", got)
	}
	if p.Value != "20300301T060000" {
		t.Errorf("DTSTART = %q, want 20300301T060000 (05:00Z in CET)", p.Value)
	}
}

// TestCreateEvent_AllDayOffsetKeepsWallDate: all-day values are DATEs taken from
// the sender's wall clock; converting them to UTC would move the day.
func TestCreateEvent_AllDayOffsetKeepsWallDate(t *testing.T) {
	uc := NewCreateEventUseCase(&stubCalendarRepo{})
	obj, err := uc.Execute(context.Background(), CreateEventInput{
		CalendarID: 1,
		Summary:    "All day",
		Start:      mustParseRFC3339(t, "2030-04-01T00:00:00+05:00"),
		End:        mustParseRFC3339(t, "2030-04-02T00:00:00+05:00"),
		IsAllDay:   true,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if p := storedProp(t, obj.ICalData, ical.PropDateTimeStart); p.Value != "20300401" {
		t.Errorf("all-day DTSTART = %q, want 20300401", p.Value)
	}
}

const berlinEventICal = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:berlin-1\r\n" +
	"DTSTAMP:20240101T000000Z\r\n" +
	"DTSTART;TZID=Europe/Berlin:20300301T100000\r\n" +
	"DTEND;TZID=Europe/Berlin:20300301T110000\r\n" +
	"SUMMARY:Berlin\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// TestUpdateEvent_OffsetWithoutTimezoneIsStoredAsUTC: PATCH start/end are RFC
// 3339 strings parsed with time.Parse, so an offset produced the same empty
// TZID as on create (F22).
func TestUpdateEvent_OffsetWithoutTimezoneIsStoredAsUTC(t *testing.T) {
	repo := &stubCalendarRepo{obj: &calendar.CalendarObject{
		UUID: "obj-uuid", UID: "berlin-1", ComponentType: "VEVENT", ICalData: berlinEventICal,
	}}
	start, end := "2030-03-02T10:00:00-03:30", "2030-03-02T11:00:00-03:30"
	got, err := NewUpdateEventUseCase(repo).Execute(context.Background(), UpdateEventInput{
		UUID: "obj-uuid", Start: &start, End: &end, Scope: "all",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	assertUTCDateTime(t, storedProp(t, got.ICalData, ical.PropDateTimeStart), "20300302T133000Z")
	assertUTCDateTime(t, storedProp(t, got.ICalData, ical.PropDateTimeEnd), "20300302T143000Z")
}

// TestUpdateEvent_SummaryOnlyKeepsStoredTZID: the normalisation must leave a
// stored IANA zone alone — a summary edit may not turn a Europe/Berlin series
// into UTC (its occurrences would stop following DST).
func TestUpdateEvent_SummaryOnlyKeepsStoredTZID(t *testing.T) {
	repo := &stubCalendarRepo{obj: &calendar.CalendarObject{
		UUID: "obj-uuid", UID: "berlin-1", ComponentType: "VEVENT", ICalData: berlinEventICal,
	}}
	summary := "Renamed"
	got, err := NewUpdateEventUseCase(repo).Execute(context.Background(), UpdateEventInput{
		UUID: "obj-uuid", Summary: &summary, Scope: "all",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	p := storedProp(t, got.ICalData, ical.PropDateTimeStart)
	if p.Params.Get(ical.PropTimezoneID) != "Europe/Berlin" || p.Value != "20300301T100000" {
		t.Errorf("DTSTART changed: TZID=%q value=%q, want Europe/Berlin 20300301T100000",
			p.Params.Get(ical.PropTimezoneID), p.Value)
	}
}

// TestUpdateEvent_FallbackTimeIsStoredAsUTC: when the stored DTSTART cannot be
// parsed (a TZID Go does not know), the update falls back to the denormalized
// column — which a database driver may hand back in time.Local (pgx does) —
// and wrote it as TZID=Local.
func TestUpdateEvent_FallbackTimeIsStoredAsUTC(t *testing.T) {
	const unknownTZ = "BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"PRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:custom-tz-1\r\n" +
		"DTSTAMP:20240101T000000Z\r\n" +
		"DTSTART;TZID=Custom Zone:20300301T100000\r\n" +
		"DTEND;TZID=Custom Zone:20300301T110000\r\n" +
		"SUMMARY:Custom\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	dbStart := time.Date(2030, 3, 1, 9, 0, 0, 0, time.UTC).In(time.Local)
	dbEnd := dbStart.Add(time.Hour)
	repo := &stubCalendarRepo{obj: &calendar.CalendarObject{
		UUID: "obj-uuid", UID: "custom-tz-1", ComponentType: "VEVENT", ICalData: unknownTZ,
		StartTime: &dbStart, EndTime: &dbEnd,
	}}
	summary := "Renamed"
	got, err := NewUpdateEventUseCase(repo).Execute(context.Background(), UpdateEventInput{
		UUID: "obj-uuid", Summary: &summary, Scope: "all",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	assertUTCDateTime(t, storedProp(t, got.ICalData, ical.PropDateTimeStart), "20300301T090000Z")
	assertUTCDateTime(t, storedProp(t, got.ICalData, ical.PropDateTimeEnd), "20300301T100000Z")
}
