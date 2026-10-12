package event

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// vtimezoneIDs lists the TZIDs of an object's VTIMEZONE components and checks
// every TZID parameter in it names one of them (RFC 5545 §3.2.19).
func vtimezoneIDs(t *testing.T, data string) []string {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, data)
	}
	defined := map[string]bool{}
	var ids []string
	for _, c := range cal.Children {
		if c.Name == ical.CompTimezone {
			id := c.Props.Get(ical.PropTimezoneID).Value
			defined[id] = true
			ids = append(ids, id)
		}
	}
	for _, ev := range cal.Events() {
		for name, props := range ev.Props {
			if name == ical.PropDateTimeStamp {
				continue // TZID=Local here is F04, fixed separately (DTSTAMP must be UTC)
			}
			for _, p := range props {
				if tzid := p.Params.Get(ical.PropTimezoneID); tzid != "" && !defined[tzid] {
					t.Errorf("%s;TZID=%s has no VTIMEZONE in\n%s", name, tzid, data)
				}
			}
		}
	}
	return ids
}

// F20: REST and MCP event create with a timezone wrote DTSTART;TZID=… without
// the VTIMEZONE the TZID must reference.
func TestCreateEvent_TimezoneAddsVTimezone(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skip(err)
	}
	uc := NewCreateEventUseCase(&stubCalendarRepo{})
	obj, err := uc.Execute(context.Background(), CreateEventInput{
		CalendarID: 1,
		Summary:    "Berlin",
		Start:      time.Date(2030, 6, 1, 8, 0, 0, 0, time.UTC),
		End:        time.Date(2030, 6, 1, 9, 0, 0, 0, time.UTC),
		RRule:      "FREQ=WEEKLY",
		Timezone:   "Europe/Berlin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(obj.ICalData, "DTSTART;TZID=Europe/Berlin:20300601T100000") {
		t.Fatalf("expected a Berlin DTSTART:\n%s", obj.ICalData)
	}
	if ids := vtimezoneIDs(t, obj.ICalData); len(ids) != 1 || ids[0] != "Europe/Berlin" {
		t.Fatalf("VTIMEZONEs = %v, want [Europe/Berlin]:\n%s", ids, obj.ICalData)
	}
	if !strings.Contains(obj.ICalData, "RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU") {
		t.Errorf("expected the EU summer-time rule:\n%s", obj.ICalData)
	}
	// The denormalized times are unchanged by the extra component.
	if obj.StartTime == nil || !obj.StartTime.Equal(time.Date(2030, 6, 1, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("StartTime = %v", obj.StartTime)
	}
}

func TestCreateEvent_UTCAndLocalGetNoVTimezone(t *testing.T) {
	uc := NewCreateEventUseCase(&stubCalendarRepo{})
	for _, tz := range []string{"", "UTC", "Local", "Not/AZone"} {
		obj, err := uc.Execute(context.Background(), CreateEventInput{
			CalendarID: 1,
			Summary:    "utc",
			Start:      time.Date(2030, 6, 1, 8, 0, 0, 0, time.UTC),
			End:        time.Date(2030, 6, 1, 9, 0, 0, 0, time.UTC),
			Timezone:   tz,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(obj.ICalData, "DTSTART:20300601T080000Z") {
			t.Errorf("timezone %q: expected a UTC DTSTART:\n%s", tz, obj.ICalData)
		}
		if ids := vtimezoneIDs(t, obj.ICalData); len(ids) != 0 {
			t.Errorf("timezone %q: unexpected VTIMEZONEs %v", tz, ids)
		}
	}
}

// Changing the zone on update swaps the VTIMEZONE: the new zone gets one, the
// old one (no longer referenced) goes.
func TestUpdateEvent_TimezoneChangeSwapsVTimezone(t *testing.T) {
	if _, err := time.LoadLocation("America/New_York"); err != nil {
		t.Skip(err)
	}
	created, err := NewCreateEventUseCase(&stubCalendarRepo{}).Execute(context.Background(), CreateEventInput{
		CalendarID: 1,
		Summary:    "Berlin",
		Start:      time.Date(2030, 6, 1, 8, 0, 0, 0, time.UTC),
		End:        time.Date(2030, 6, 1, 9, 0, 0, 0, time.UTC),
		Timezone:   "Europe/Berlin",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := &stubCalendarRepo{obj: created}
	uc := NewUpdateEventUseCase(repo)

	tz := "America/New_York"
	got, err := uc.Execute(context.Background(), UpdateEventInput{UUID: created.UUID, Timezone: &tz, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.ICalData, "DTSTART;TZID=America/New_York:20300601T040000") {
		t.Fatalf("expected a New York DTSTART:\n%s", got.ICalData)
	}
	if ids := vtimezoneIDs(t, got.ICalData); len(ids) != 1 || ids[0] != "America/New_York" {
		t.Fatalf("VTIMEZONEs = %v, want [America/New_York]:\n%s", ids, got.ICalData)
	}

	// An update that keeps the zone (summary only) keeps its VTIMEZONE.
	summary := "renamed"
	got, err = uc.Execute(context.Background(), UpdateEventInput{UUID: created.UUID, Summary: &summary, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := vtimezoneIDs(t, got.ICalData); len(ids) != 1 || ids[0] != "America/New_York" {
		t.Fatalf("VTIMEZONEs after a summary edit = %v:\n%s", ids, got.ICalData)
	}
}

// A stored object without VTIMEZONE (written before F20 was fixed) heals on
// its next edit.
func TestUpdateEvent_AddsMissingVTimezone(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skip(err)
	}
	const legacy = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:legacy\r\nDTSTAMP:20260101T000000Z\r\nDTSTART;TZID=Europe/Berlin:20300601T100000\r\n" +
		"DTEND;TZID=Europe/Berlin:20300601T110000\r\nSUMMARY:old\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	repo := &stubCalendarRepo{obj: &calendar.CalendarObject{UUID: "legacy", UID: "legacy", ICalData: legacy}}
	summary := "new"
	got, err := NewUpdateEventUseCase(repo).Execute(context.Background(), UpdateEventInput{UUID: "legacy", Summary: &summary, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.ICalData, "DTSTART;TZID=Europe/Berlin:20300601T100000") {
		t.Fatalf("zone lost:\n%s", got.ICalData)
	}
	if ids := vtimezoneIDs(t, got.ICalData); len(ids) != 1 || ids[0] != "Europe/Berlin" {
		t.Fatalf("VTIMEZONEs = %v:\n%s", ids, got.ICalData)
	}
}
