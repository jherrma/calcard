package importexport

import (
	"context"
	"regexp"
	"strings"
	"testing"

	ical "github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// uuidPath matches the generated "<uuid>.ics" resource names.
var uuidPath = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.ics$`)

// icalEvent builds a one-VEVENT VCALENDAR; withStamp controls DTSTAMP.
func icalEvent(uid string, withStamp bool) []byte {
	out := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\n"
	if withStamp {
		out += "DTSTAMP:20251231T000000Z\r\n"
	}
	out += "SUMMARY:Event\r\nDTSTART:20260101T120000Z\r\nDTEND:20260101T130000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	return []byte(out)
}

// TestImport_MissingDTStampIsAdded (F23): many exports omit DTSTAMP. The import
// must store such an event with a UTC DTSTAMP instead of failing to encode it.
func TestImport_MissingDTStampIsAdded(t *testing.T) {
	uc, repo := newImportFixture()

	res, err := uc.Execute(context.Background(), 42, "cal-uuid", icalEvent("nostamp@test", false), ImportOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 1 || res.Failed != 0 {
		t.Fatalf("an event without DTSTAMP must import: %+v", res)
	}

	stored, err := ical.NewDecoder(strings.NewReader(repo.created[0].ICalData)).Decode()
	if err != nil {
		t.Fatalf("stored data does not parse: %v", err)
	}
	stamp := stored.Children[0].Props.Get(ical.PropDateTimeStamp)
	if stamp == nil {
		t.Fatalf("stored event has no DTSTAMP:\n%s", repo.created[0].ICalData)
	}
	if !strings.HasSuffix(stamp.Value, "Z") || stamp.Params.Get(ical.ParamTimezoneID) != "" {
		t.Fatalf("DTSTAMP must be UTC (RFC 5545 3.8.7.2), got %q params %v", stamp.Value, stamp.Params)
	}
}

// TestImport_ObjectPathIsURLSafe (F23): a UID is opaque and may contain '/',
// spaces, '?' or "..". Such a UID must not become the DAV resource name; a
// URL-safe UID keeps the familiar "<uid>.ics".
func TestImport_ObjectPathIsURLSafe(t *testing.T) {
	unsafe := []string{"a/b@test", "../up@test", "with space@test", "q?x@test", "pct%2F@test", strings.Repeat("x", 250)}
	for _, uid := range unsafe {
		uc, repo := newImportFixture()
		res, err := uc.Execute(context.Background(), 42, "cal-uuid", icalEvent(uid, true), ImportOptions{})
		if err != nil || res.Imported != 1 {
			t.Fatalf("uid %q: import failed: %v %+v", uid, err, res)
		}
		if p := repo.created[0].Path; !uuidPath.MatchString(p) {
			t.Errorf("uid %q: want a <uuid>.ics path, got %q", uid, p)
		}
		if repo.created[0].UID != uid {
			t.Errorf("uid %q: the UID itself must be kept, got %q", uid, repo.created[0].UID)
		}
	}

	uc, repo := newImportFixture()
	if _, err := uc.Execute(context.Background(), 42, "cal-uuid", icalEvent("Abc-123_x.y~z@host.example", true), ImportOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p := repo.created[0].Path; p != "Abc-123_x.y~z@host.example.ics" {
		t.Errorf("a URL-safe UID keeps <uid>.ics, got %q", p)
	}
}

// TestImport_OccupiedPathIsNotReused (F23): a DAV client may have stored a
// different UID at "<uid>.ics". Importing that UID must not put a second
// object at the same path, which would make lookups by path ambiguous.
func TestImport_OccupiedPathIsNotReused(t *testing.T) {
	uc, repo := newImportFixture()
	repo.objects = []*calendar.CalendarObject{{ID: 100, CalendarID: 1, UID: "something-else", Path: "taken@test.ics"}}

	res, err := uc.Execute(context.Background(), 42, "cal-uuid", icalEvent("taken@test", true), ImportOptions{})
	if err != nil || res.Imported != 1 {
		t.Fatalf("import failed: %v %+v", err, res)
	}
	if p := repo.created[0].Path; p == "taken@test.ics" || !uuidPath.MatchString(p) {
		t.Fatalf("the occupied path must not be reused, got %q", p)
	}
}

// TestImport_ReimportMatchesByUID (F23): objects whose path is not "<uid>.ics"
// (UUID-named, or stored by a DAV client) are still found by UID on re-import;
// replace keeps the object's href, unless it is unusable (a legacy "a/b.ics"
// row), in which case it is moved to a usable one.
func TestImport_ReimportMatchesByUID(t *testing.T) {
	const davPath = "0b9c6f7e-8a0d-4b55-9d4b-5a3e8a1f0c11.ics"

	uc, repo := newImportFixture()
	repo.objects = []*calendar.CalendarObject{{ID: 100, CalendarID: 1, UID: "a/b@test", Path: davPath}}
	res, err := uc.Execute(context.Background(), 42, "cal-uuid", icalEvent("a/b@test", true), ImportOptions{DuplicateHandling: "skip"})
	if err != nil || res.Skipped != 1 || res.Imported != 0 {
		t.Fatalf("skip must find the existing object by UID: %v %+v", err, res)
	}

	res, err = uc.Execute(context.Background(), 42, "cal-uuid", icalEvent("a/b@test", true), ImportOptions{DuplicateHandling: "replace"})
	if err != nil || res.Imported != 1 || len(repo.deleted) != 1 {
		t.Fatalf("replace must delete and recreate: %v %+v", err, res)
	}
	if p := repo.created[0].Path; p != davPath {
		t.Fatalf("replace must keep the existing href %q, got %q", davPath, p)
	}

	uc, repo = newImportFixture()
	repo.objects = []*calendar.CalendarObject{{ID: 100, CalendarID: 1, UID: "a/b@test", Path: "a/b@test.ics"}}
	res, err = uc.Execute(context.Background(), 42, "cal-uuid", icalEvent("a/b@test", true), ImportOptions{DuplicateHandling: "replace"})
	if err != nil || res.Imported != 1 || len(repo.deleted) != 1 {
		t.Fatalf("replace must delete and recreate: %v %+v", err, res)
	}
	if p := repo.created[0].Path; !uuidPath.MatchString(p) {
		t.Fatalf("replace of an unreachable legacy path must move it to <uuid>.ics, got %q", p)
	}
}

func TestImportObjectPathHelpers(t *testing.T) {
	for uid, want := range map[string]bool{
		"0b9c6f7e-8a0d-4b55-9d4b-5a3e8a1f0c11": true,
		"abc@google.com":                       true,
		"040000008200E00074C5B7101A82E008":     true,
		"":                                     false,
		"a/b":                                  false,
		"..":                                   true, // "...ics" is a plain segment
		"a b":                                  false,
		"a?b":                                  false,
		"a%2Fb":                                false,
		"{braces}":                             false,
		"ü@example.com":                        false,
		strings.Repeat("x", maxUIDPathLen):     true,
		strings.Repeat("x", maxUIDPathLen+1):   false,
	} {
		if got := isPathSafeUID(uid); got != want {
			t.Errorf("isPathSafeUID(%q) = %v, want %v", uid, got, want)
		}
	}
	for seg, want := range map[string]bool{
		"x.ics": true, "with space.ics": true, "": false, ".": false, "..": false,
		"a/b.ics": false, `a\b.ics`: false, strings.Repeat("x", 256): false,
	} {
		if got := isUsableSegment(seg); got != want {
			t.Errorf("isUsableSegment(%q) = %v, want %v", seg, got, want)
		}
	}
}
