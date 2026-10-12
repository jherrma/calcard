package importexport

import (
	"context"
	"reflect"
	"strings"
	"testing"

	ical "github.com/emersion/go-ical"
)

func vtimezoneBlock(tzid, offset string) string {
	return "BEGIN:VTIMEZONE\r\nTZID:" + tzid + "\r\n" +
		"BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:" + offset + "\r\n" +
		"TZOFFSETTO:" + offset + "\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n"
}

// storedTimezones returns the TZIDs of the VTIMEZONEs a stored object carries,
// in document order.
func storedTimezones(t *testing.T, data string) []string {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	if err != nil {
		t.Fatalf("stored object does not decode: %v\n%s", err, data)
	}
	var ids []string
	for _, child := range cal.Children {
		if child.Name == ical.CompTimezone {
			ids = append(ids, child.Props.Get(ical.PropTimezoneID).Value)
		}
	}
	return ids
}

// TestImport_KeepsReferencedVTimezones (F15): import re-wraps each component in
// its own VCALENDAR, and used to drop the file's VTIMEZONEs on the way, leaving
// stored objects with a TZID that resolves to nothing (RFC 5545 §3.2.19). Each
// object must carry exactly the zones it references — including one named only
// in a VALARM — and none it does not.
func TestImport_KeepsReferencedVTimezones(t *testing.T) {
	uc, repo := newImportFixture()

	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" +
		vtimezoneBlock("Europe/Berlin", "+0100") +
		vtimezoneBlock("America/New_York", "-0500") +
		vtimezoneBlock("Asia/Tokyo", "+0900") + // declared, used by nobody
		vtimezoneBlock("Custom/Zone", "+0300") +
		"BEGIN:VEVENT\r\nUID:berlin@test\r\nDTSTAMP:20251231T000000Z\r\nSUMMARY:Berlin\r\n" +
		"DTSTART;TZID=Europe/Berlin:20300601T100000\r\nDTEND;TZID=Europe/Berlin:20300601T110000\r\n" +
		"END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:flight@test\r\nDTSTAMP:20251231T000000Z\r\nSUMMARY:Flight\r\n" +
		"DTSTART;TZID=Europe/Berlin:20300602T100000\r\nDTEND;TZID=America/New_York:20300602T130000\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Go\r\n" +
		"TRIGGER;VALUE=DATE-TIME;TZID=Custom/Zone:20300602T070000\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:utc@test\r\nDTSTAMP:20251231T000000Z\r\nSUMMARY:UTC\r\n" +
		"DTSTART:20300603T100000Z\r\nDTEND:20300603T110000Z\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	res, err := uc.Execute(context.Background(), 42, "cal-uuid", []byte(data), ImportOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 3 || res.Failed != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}

	want := map[string][]string{
		"berlin@test": {"Europe/Berlin"},
		// Sorted by TZID, so the encoding is the same on every run.
		"flight@test": {"America/New_York", "Custom/Zone", "Europe/Berlin"},
		"utc@test":    nil,
	}
	for _, obj := range repo.created {
		got := storedTimezones(t, obj.ICalData)
		if !reflect.DeepEqual(got, want[obj.UID]) {
			t.Errorf("%s: stored VTIMEZONEs = %v, want %v\n%s", obj.UID, got, want[obj.UID], obj.ICalData)
		}
	}
}
