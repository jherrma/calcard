package importexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// stubCalendarRepo implements calendar.CalendarRepository for backup-export
// tests. Only ListByUserID and GetCalendarObjects are exercised; the rest are
// unimplemented no-ops to satisfy the interface.
type stubCalendarRepo struct {
	calendar.CalendarRepository
	calendars []*calendar.Calendar
	objects   map[uint][]*calendar.CalendarObject
}

func (s *stubCalendarRepo) ListByUserID(ctx context.Context, userID uint) ([]*calendar.Calendar, error) {
	return s.calendars, nil
}

func (s *stubCalendarRepo) GetCalendarObjects(ctx context.Context, calendarID uint) ([]*calendar.CalendarObject, error) {
	return s.objects[calendarID], nil
}

// stubAddressBookRepo implements addressbook.Repository for backup-export tests.
// Only ListByUserID and ListObjects are exercised.
type stubAddressBookRepo struct {
	addressbook.Repository
	books   []addressbook.AddressBook
	objects map[uint][]addressbook.AddressObject
}

func (s *stubAddressBookRepo) ListByUserID(ctx context.Context, userID uint) ([]addressbook.AddressBook, error) {
	return s.books, nil
}

// ListObjects honors limit/offset so tests can exercise the export's paging
// loop (limit -1 still means "everything from offset", matching the repo).
func (s *stubAddressBookRepo) ListObjects(ctx context.Context, addressBookID uint, limit, offset int, sortField, order string) ([]addressbook.AddressObject, int64, error) {
	objs := s.objects[addressBookID]
	total := int64(len(objs))
	if offset >= len(objs) {
		return nil, total, nil
	}
	end := len(objs)
	if limit >= 0 && offset+limit < end {
		end = offset + limit
	}
	return objs[offset:end], total, nil
}

// readZipEntries unzips raw ZIP bytes into a name->content map.
func readZipEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("failed to open ZIP: %v", err)
	}
	entries := make(map[string]string)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open entry %q: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("failed to read entry %q: %v", f.Name, err)
		}
		if _, dup := entries[f.Name]; dup {
			t.Fatalf("ZIP contains duplicate entry name %q", f.Name)
		}
		entries[f.Name] = string(content)
	}
	return entries
}

// TestBackupExportDedupesCollidingNames verifies that two calendars named
// "Personal" and two address books named "Friends" each produce distinct ZIP
// entries with their own payloads, rather than overwriting one another.
func TestBackupExportDedupesCollidingNames(t *testing.T) {
	calRepo := &stubCalendarRepo{
		calendars: []*calendar.Calendar{
			{ID: 1, Name: "Personal"},
			{ID: 2, Name: "Personal"},
		},
		objects: map[uint][]*calendar.CalendarObject{
			1: {{UID: "cal1-evt", ICalData: "BEGIN:VEVENT\r\nUID:cal1-evt\r\nEND:VEVENT\r\n"}},
			2: {{UID: "cal2-evt", ICalData: "BEGIN:VEVENT\r\nUID:cal2-evt\r\nEND:VEVENT\r\n"}},
		},
	}
	abRepo := &stubAddressBookRepo{
		books: []addressbook.AddressBook{
			{ID: 1, Name: "Friends"},
			{ID: 2, Name: "Friends"},
		},
		objects: map[uint][]addressbook.AddressObject{
			1: {{UID: "ab1-contact", VCardData: "BEGIN:VCARD\r\nUID:ab1-contact\r\nEND:VCARD\r\n"}},
			2: {{UID: "ab2-contact", VCardData: "BEGIN:VCARD\r\nUID:ab2-contact\r\nEND:VCARD\r\n"}},
		},
	}

	uc := NewBackupExportUseCase(calRepo, abRepo)
	var buf bytes.Buffer
	name, err := uc.Execute(context.Background(), 42, &buf)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if name == "" {
		t.Fatalf("Execute returned an empty filename")
	}

	entries := readZipEntries(t, buf.Bytes())

	// Collect the calendar and address book entry names.
	var calNames, abNames []string
	for name := range entries {
		switch {
		case strings.HasPrefix(name, "calendars/"):
			calNames = append(calNames, name)
		case strings.HasPrefix(name, "addressbooks/"):
			abNames = append(abNames, name)
		}
	}
	sort.Strings(calNames)
	sort.Strings(abNames)

	if len(calNames) != 2 {
		t.Fatalf("expected 2 distinct calendar entries, got %d: %v", len(calNames), calNames)
	}
	if len(abNames) != 2 {
		t.Fatalf("expected 2 distinct address book entries, got %d: %v", len(abNames), abNames)
	}

	// Both calendars' payloads must be present, one per entry.
	if !strings.Contains(entries[calNames[0]], "cal1-evt") && !strings.Contains(entries[calNames[1]], "cal1-evt") {
		t.Errorf("cal1-evt payload missing from calendar entries")
	}
	if !strings.Contains(entries[calNames[0]], "cal2-evt") && !strings.Contains(entries[calNames[1]], "cal2-evt") {
		t.Errorf("cal2-evt payload missing from calendar entries")
	}
	// The two calendar entries must not carry identical content.
	if entries[calNames[0]] == entries[calNames[1]] {
		t.Errorf("both calendar entries have identical content; one calendar overwrote the other")
	}

	// Both address books' payloads must be present, one per entry.
	if !strings.Contains(entries[abNames[0]], "ab1-contact") && !strings.Contains(entries[abNames[1]], "ab1-contact") {
		t.Errorf("ab1-contact payload missing from address book entries")
	}
	if !strings.Contains(entries[abNames[0]], "ab2-contact") && !strings.Contains(entries[abNames[1]], "ab2-contact") {
		t.Errorf("ab2-contact payload missing from address book entries")
	}
	if entries[abNames[0]] == entries[abNames[1]] {
		t.Errorf("both address book entries have identical content; one book overwrote the other")
	}

	// Sanity: the expected deterministic names are present.
	for _, want := range []string{"calendars/Personal.ics", "calendars/Personal-2.ics", "addressbooks/Friends.vcf", "addressbooks/Friends-2.vcf"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("expected ZIP entry %q not found; entries: %v", want, keysOf(entries))
		}
	}
}

// TestUniqueZipEntrySanitizedCollision covers names that sanitize to the same
// string (e.g. "a/b" and "a:b" both become "a-b").
func TestUniqueZipEntrySanitizedCollision(t *testing.T) {
	used := make(map[string]bool)
	first := uniqueZipEntry(used, "calendars", sanitizeFilename("a/b"), "ics")
	second := uniqueZipEntry(used, "calendars", sanitizeFilename("a:b"), "ics")
	if first == second {
		t.Fatalf("expected distinct entry names for sanitized collision, got %q twice", first)
	}
	if first != "calendars/a-b.ics" {
		t.Errorf("expected first entry calendars/a-b.ics, got %q", first)
	}
	if second != "calendars/a-b-2.ics" {
		t.Errorf("expected second entry calendars/a-b-2.ics, got %q", second)
	}
}

// TestBackupExportPagesContacts exercises the streaming/paging rewrite: an
// address book larger than one page must be exported completely. The export
// pages ListObjects contactPageSize at a time and writes each vCard straight
// into the ZIP as it is read; this asserts the resulting single .vcf entry
// still contains every contact (including those past the first page and in the
// short final page) and that the metadata count matches. Before paging, the
// use case loaded everything with limit -1, so this is the guard that the new
// loop produces the same archive contents.
func TestBackupExportPagesContacts(t *testing.T) {
	const total = contactPageSize*2 + 37 // multiple pages, with a short last page

	objs := make([]addressbook.AddressObject, 0, total)
	for i := 0; i < total; i++ {
		uid := fmt.Sprintf("paged-%d", i)
		objs = append(objs, addressbook.AddressObject{
			UID:       uid,
			VCardData: fmt.Sprintf("BEGIN:VCARD\r\nUID:%s\r\nEND:VCARD\r\n", uid),
		})
	}

	calRepo := &stubCalendarRepo{}
	abRepo := &stubAddressBookRepo{
		books:   []addressbook.AddressBook{{ID: 1, Name: "Big"}},
		objects: map[uint][]addressbook.AddressObject{1: objs},
	}

	uc := NewBackupExportUseCase(calRepo, abRepo)
	var buf bytes.Buffer
	if _, err := uc.Execute(context.Background(), 7, &buf); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	entries := readZipEntries(t, buf.Bytes())
	vcf, ok := entries["addressbooks/Big.vcf"]
	if !ok {
		t.Fatalf("expected addressbooks/Big.vcf entry; got %v", keysOf(entries))
	}
	for i := 0; i < total; i++ {
		uid := fmt.Sprintf("paged-%d", i)
		if !strings.Contains(vcf, "UID:"+uid+"\r\n") {
			t.Errorf("exported vCard missing contact %q — paging dropped it", uid)
		}
	}

	// The metadata contact_count must reflect every paged contact, not just
	// the first page.
	var md ExportMetadata
	if err := json.Unmarshal([]byte(entries["metadata.json"]), &md); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	found := false
	for _, ab := range md.AddressBooks {
		if ab.Name == "Big" {
			found = true
			if ab.ContactCount != total {
				t.Errorf("metadata contact_count = %d, want %d", ab.ContactCount, total)
			}
		}
	}
	if !found {
		t.Errorf("metadata missing address book %q", "Big")
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
