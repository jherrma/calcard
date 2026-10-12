package calendar

import (
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// vtzOnset is one moment a generated VTIMEZONE switches to a new offset.
type vtzOnset struct {
	at  time.Time
	off int
}

func parseUTCOffset(t *testing.T, v string) int {
	t.Helper()
	sign := 1
	switch v[0] {
	case '-':
		sign = -1
	case '+':
	default:
		t.Fatalf("bad UTC offset %q", v)
	}
	h, _ := strconv.Atoi(v[1:3])
	m, _ := strconv.Atoi(v[3:5])
	s := 0
	if len(v) == 7 {
		s, _ = strconv.Atoi(v[5:7])
	}
	return sign * (h*3600 + m*60 + s)
}

// vtzOnsets evaluates a VTIMEZONE the way RFC 5545 §3.6.5 reads it: each
// observance's DTSTART (and RRULE expansion) is local time in TZOFFSETFROM,
// and UNTIL is compared as a UTC instant.
func vtzOnsets(t *testing.T, vtz *ical.Component, until time.Time) []vtzOnset {
	t.Helper()
	var out []vtzOnset
	for _, obs := range vtz.Children {
		from := parseUTCOffset(t, obs.Props.Get(ical.PropTimezoneOffsetFrom).Value)
		to := parseUTCOffset(t, obs.Props.Get(ical.PropTimezoneOffsetTo).Value)
		fromZone := time.FixedZone("from", from)
		start, err := time.ParseInLocation(icalLocalLayout, obs.Props.Get(ical.PropDateTimeStart).Value, fromZone)
		if err != nil {
			t.Fatalf("DTSTART: %v", err)
		}
		p := obs.Props.Get(ical.PropRecurrenceRule)
		if p == nil {
			out = append(out, vtzOnset{start, to})
			continue
		}
		r, err := rrule.StrToRRule(p.Value)
		if err != nil {
			t.Fatalf("RRULE %q: %v", p.Value, err)
		}
		r.DTStart(start)
		occ := r.Between(start.Add(-time.Second), until, true)
		if len(occ) == 0 || !occ[0].Equal(start) {
			t.Fatalf("RRULE %q does not produce its DTSTART %s first (got %v)", p.Value, start, occ[:min(1, len(occ))])
		}
		for _, o := range occ {
			out = append(out, vtzOnset{o, to})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

func offsetAt(onsets []vtzOnset, t time.Time) (int, bool) {
	i := sort.Search(len(onsets), func(i int) bool { return onsets[i].at.After(t) })
	if i == 0 {
		return 0, false
	}
	return onsets[i-1].off, true
}

// checkVTimezone compares the generated VTIMEZONE against Go's own zone data
// around every transition and twice a month from `from` to `until`.
func checkVTimezone(t *testing.T, name string, from, until time.Time) *ical.Component {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone %s not available: %v", name, err)
	}
	vtz := GenerateVTimezone(name, loc, from)

	// It must encode (go-ical validates the component structure) and survive a round trip.
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//test//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Children = append(cal.Children, vtz)
	var sb strings.Builder
	if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
		t.Fatalf("%s: encode: %v", name, err)
	}
	decoded, err := ical.NewDecoder(strings.NewReader(sb.String())).Decode()
	if err != nil {
		t.Fatalf("%s: decode: %v", name, err)
	}
	vtz = decoded.Children[0]

	onsets := vtzOnsets(t, vtz, until)
	// The reference: Go's offset, scanned every 12 hours, each change pinned to
	// the second by bisection (independent of ZoneBounds, which the generator uses).
	offset := func(t time.Time) int { _, off := t.In(loc).Zone(); return off }
	var samples []time.Time
	prev := from
	for cur := from.Add(12 * time.Hour); cur.Before(until); prev, cur = cur, cur.Add(12*time.Hour) {
		samples = append(samples, cur)
		if offset(cur) == offset(prev) {
			continue
		}
		lo, hi := prev, cur // offset(lo) != offset(hi)
		for hi.Sub(lo) > time.Second {
			mid := lo.Add(hi.Sub(lo) / 2).Truncate(time.Second)
			if offset(mid) == offset(lo) {
				lo = mid
			} else {
				hi = mid
			}
		}
		samples = append(samples, lo, hi)
	}
	for _, s := range samples {
		if s.Before(from) {
			continue
		}
		_, want := s.In(loc).Zone()
		got, ok := offsetAt(onsets, s)
		if !ok || got != want {
			t.Fatalf("%s at %s: VTIMEZONE says %d (defined %v), Go says %d\n%s", name, s, got, ok, want, sb.String())
		}
	}
	return vtz
}

func TestGenerateVTimezoneMatchesGoZoneData(t *testing.T) {
	from := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2160, 1, 1, 0, 0, 0, 0, time.UTC) // past the window: open-ended rules must hold
	for _, name := range []string{
		"Europe/Berlin", "Europe/London", "Europe/Dublin", "America/New_York", "America/Sao_Paulo",
		"America/Santiago", "America/Nuuk", "America/Havana", "Australia/Sydney", "Australia/Lord_Howe",
		"Pacific/Auckland", "Pacific/Chatham", "Asia/Tokyo", "Asia/Kolkata", "Asia/Jerusalem", "Asia/Tehran",
		"Africa/Casablanca", "Asia/Gaza", "Antarctica/Troll", "UTC", "Etc/GMT+5",
	} {
		t.Run(name, func(t *testing.T) { checkVTimezone(t, name, from, until) })
	}
}

func TestGenerateVTimezoneBefore1970(t *testing.T) {
	// A pre-1970 event widens the window back to the event's own date.
	from := time.Date(1950, 6, 1, 0, 0, 0, 0, time.UTC)
	vtz := checkVTimezone(t, "Europe/Berlin", from, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if got := vtz.Children[0].Props.Get(ical.PropDateTimeStart).Value; got > "19500601T000000" {
		t.Fatalf("first observance starts %s, after the event", got)
	}
}

func TestGenerateVTimezoneBerlinUsesRules(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip(err)
	}
	vtz := GenerateVTimezone("Europe/Berlin", loc, time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC))
	var cal = ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//test//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Children = []*ical.Component{vtz}
	var sb strings.Builder
	if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	// Since 1996 the EU rules: last Sunday of March / October, open-ended.
	for _, want := range []string{
		"TZID:Europe/Berlin",
		"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU\r\n",
		"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU\r\n",
		"TZNAME:CEST", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0200",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if len(vtz.Children) > 6 {
		t.Errorf("expected a handful of observances, got %d:\n%s", len(vtz.Children), out)
	}
}

func encodeTestCal(t *testing.T, cal *ical.Calendar) string {
	t.Helper()
	var sb strings.Builder
	if err := ical.NewEncoder(&sb).Encode(cal); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func TestSyncVTimezones(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skip(err)
	}
	const client = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Custom Zone\r\nBEGIN:STANDARD\r\nDTSTART:16010101T000000\r\n" +
		"TZOFFSETFROM:+0300\r\nTZOFFSETTO:+0300\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Unused/Zone\r\nBEGIN:STANDARD\r\nDTSTART:19700101T000000\r\n" +
		"TZOFFSETFROM:+0100\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
		"BEGIN:VEVENT\r\nUID:x\r\nDTSTAMP:20260101T000000Z\r\n" +
		"DTSTART;TZID=Europe/Berlin:20300601T100000\r\nDTEND;TZID=Custom Zone:20300601T120000\r\n" +
		"EXDATE;TZID=America/New_York:20300608T040000\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:x\r\nTRIGGER;VALUE=DATE-TIME;TZID=Asia/Tokyo:20300601T090000\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := ical.NewDecoder(strings.NewReader(client)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	SyncVTimezones(cal)

	var tzids []string
	for _, c := range cal.Children {
		if c.Name == ical.CompTimezone {
			tzids = append(tzids, c.Props.Get(ical.PropTimezoneID).Value)
		}
	}
	// Referenced zones only, sorted; nested VALARM and EXDATE count; the
	// client's "Custom Zone" definition is kept although no such IANA zone exists.
	want := []string{"America/New_York", "Asia/Tokyo", "Custom Zone", "Europe/Berlin"}
	if strings.Join(tzids, "|") != strings.Join(want, "|") {
		t.Fatalf("VTIMEZONEs = %v, want %v", tzids, want)
	}
	if cal.Children[len(cal.Children)-1].Name != ical.CompEvent {
		t.Fatal("VTIMEZONEs must come before the VEVENT")
	}
	out := encodeTestCal(t, cal)
	if !strings.Contains(out, "TZOFFSETTO:+0300") {
		t.Errorf("client VTIMEZONE replaced:\n%s", out)
	}

	// Dropping the last use of a zone drops its VTIMEZONE; running it twice is stable.
	ev := cal.Events()[0]
	ev.Props.Del(ical.PropExceptionDates)
	SyncVTimezones(cal)
	again := encodeTestCal(t, cal)
	if strings.Contains(again, "TZID:America/New_York") {
		t.Errorf("unused VTIMEZONE kept:\n%s", again)
	}
	SyncVTimezones(cal)
	if encodeTestCal(t, cal) != again {
		t.Error("SyncVTimezones is not idempotent")
	}
}

func TestSyncVTimezonesRegeneratesTooLateDefinition(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skip(err)
	}
	// A Berlin definition that starts in 2030 leaves a 1960 event undefined.
	const data = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\nBEGIN:STANDARD\r\nDTSTART:20301027T030000\r\n" +
		"TZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
		"BEGIN:VEVENT\r\nUID:x\r\nDTSTAMP:20260101T000000Z\r\nDTSTART;TZID=Europe/Berlin:19600601T100000\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	SyncVTimezones(cal)
	vtz := cal.Children[0]
	if first := vtz.Children[0].Props.Get(ical.PropDateTimeStart).Value; first > "19600601T100000" {
		t.Fatalf("VTIMEZONE still starts at %s", first)
	}
}

func TestLoadEventLocationRefusesLocal(t *testing.T) {
	if _, ok := LoadEventLocation("Local"); ok {
		t.Error(`"Local" must not be accepted as an event time zone`)
	}
	if _, ok := LoadEventLocation(""); ok {
		t.Error("empty zone accepted")
	}
	if _, ok := LoadEventLocation("Not/AZone"); ok {
		t.Error("unknown zone accepted")
	}
}
