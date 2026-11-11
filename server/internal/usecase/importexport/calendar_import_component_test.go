package importexport

import (
	"context"
	"reflect"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// mixedImportFile holds, in order: a single VEVENT, a VTODO, and a recurring
// VEVENT with one RECURRENCE-ID override (one item, two components).
var mixedImportFile = []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:meeting@test\r\nDTSTAMP:20251231T000000Z\r\n" +
	"DTSTART:20260105T090000Z\r\nDTEND:20260105T100000Z\r\nSUMMARY:Meeting\r\nEND:VEVENT\r\n" +
	"BEGIN:VTODO\r\nUID:todo@test\r\nDTSTAMP:20251231T000000Z\r\n" +
	"DUE:20260110T170000Z\r\nSUMMARY:Renew passport\r\nEND:VTODO\r\n" +
	"BEGIN:VEVENT\r\nUID:series@test\r\nDTSTAMP:20251231T000000Z\r\n" +
	"DTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\n" +
	"SUMMARY:Series\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:series@test\r\nDTSTAMP:20251231T000000Z\r\n" +
	"RECURRENCE-ID:20260102T100000Z\r\nDTSTART:20260102T150000Z\r\nDTEND:20260102T160000Z\r\n" +
	"SUMMARY:Series (moved)\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n")

func importFixtureStoring(stored string) (*CalendarImportUseCase, *mockCalendarRepo) {
	repo := &mockCalendarRepo{cal: &calendar.Calendar{ID: 1, UUID: "cal-uuid", UserID: 42, SupportedComponents: stored}}
	return NewCalendarImportUseCase(repo), repo
}

func createdUIDs(repo *mockCalendarRepo) []string {
	var uids []string
	for _, obj := range repo.created {
		uids = append(uids, obj.UID)
	}
	return uids
}

// TestImport_TaskListSkipsEvents (#170): importing into a VTODO-only calendar
// stores the VTODO, skips each VEVENT item (a series with its override is one)
// and lists every skipped item in ImportResult with the reason.
func TestImport_TaskListSkipsEvents(t *testing.T) {
	uc, repo := importFixtureStoring("VTODO")

	res, err := uc.Execute(context.Background(), 42, "cal-uuid", mixedImportFile, ImportOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 3 || res.Imported != 1 || res.Skipped != 2 || res.Failed != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	wantErrors := []ImportError{
		{Index: 0, UID: "meeting@test", Summary: "Meeting", Error: "This calendar does not store events"},
		{Index: 2, UID: "series@test", Summary: "Series", Error: "This calendar does not store events"},
	}
	if !reflect.DeepEqual(res.Errors, wantErrors) {
		t.Fatalf("skipped items not reported:\n got %+v\nwant %+v", res.Errors, wantErrors)
	}
	if got := createdUIDs(repo); !reflect.DeepEqual(got, []string{"todo@test"}) {
		t.Fatalf("only the task may be stored, created %v", got)
	}
	if repo.created[0].ComponentType != calendar.ComponentToDo {
		t.Fatalf("stored object is a %q", repo.created[0].ComponentType)
	}
}

// TestImport_EventCalendarSkipsTasks (#170): the same rule the other way round,
// a VEVENT-only calendar skips the VTODO.
func TestImport_EventCalendarSkipsTasks(t *testing.T) {
	uc, repo := importFixtureStoring("VEVENT")

	res, err := uc.Execute(context.Background(), 42, "cal-uuid", mixedImportFile, ImportOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 3 || res.Imported != 2 || res.Skipped != 1 || res.Failed != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	wantErrors := []ImportError{
		{Index: 1, UID: "todo@test", Summary: "Renew passport", Error: "This calendar does not store tasks"},
	}
	if !reflect.DeepEqual(res.Errors, wantErrors) {
		t.Fatalf("skipped item not reported:\n got %+v\nwant %+v", res.Errors, wantErrors)
	}
	if got := createdUIDs(repo); !reflect.DeepEqual(got, []string{"meeting@test", "series@test"}) {
		t.Fatalf("only the events may be stored, created %v", got)
	}
}

// TestImport_MixedCalendarsImportEverything: a "VEVENT,VTODO" calendar and a
// legacy "" one (both components) import every item, as before #170.
func TestImport_MixedCalendarsImportEverything(t *testing.T) {
	for _, stored := range []string{"VEVENT,VTODO", ""} {
		t.Run("stored="+stored, func(t *testing.T) {
			uc, repo := importFixtureStoring(stored)

			res, err := uc.Execute(context.Background(), 42, "cal-uuid", mixedImportFile, ImportOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Total != 3 || res.Imported != 3 || res.Skipped != 0 || res.Failed != 0 || len(res.Errors) != 0 {
				t.Fatalf("unexpected result: %+v", res)
			}
			if got := createdUIDs(repo); !reflect.DeepEqual(got, []string{"meeting@test", "todo@test", "series@test"}) {
				t.Fatalf("created %v", got)
			}
		})
	}
}

// TestImport_SkippedItemNeverReplaces: the component check runs before the
// duplicate handling, so "replace" does not delete the stored object of the
// same UID for an item the calendar refuses.
func TestImport_SkippedItemNeverReplaces(t *testing.T) {
	uc, repo := importFixtureStoring("VTODO")
	stored := &calendar.CalendarObject{ID: 99, CalendarID: 1, UID: "meeting@test", ComponentType: calendar.ComponentEvent}
	repo.objects = []*calendar.CalendarObject{stored}

	res, err := uc.Execute(context.Background(), 42, "cal-uuid", mixedImportFile, ImportOptions{DuplicateHandling: "replace"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 1 || res.Skipped != 2 || res.Failed != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(repo.deleted) != 0 {
		t.Fatalf("a refused item must not delete the stored object, deleted %d", len(repo.deleted))
	}
	if len(repo.objects) != 2 || repo.objects[0] != stored {
		t.Fatalf("the stored object must survive next to the imported task, objects: %d", len(repo.objects))
	}
}

// TestImport_ReportedSummaryIsPlainText: the summary an import reports for a
// skipped item is plain text, like everywhere else in the API — not the
// escaped iCalendar TEXT value.
func TestImport_ReportedSummaryIsPlainText(t *testing.T) {
	uc, _ := importFixtureStoring("VTODO")
	file := []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:lunch@test\r\nDTSTAMP:20251231T000000Z\r\n" +
		"DTSTART:20260105T120000Z\r\nDTEND:20260105T130000Z\r\nSUMMARY:Lunch\\, Bob\\; Alice\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	res, err := uc.Execute(context.Background(), 42, "cal-uuid", file, ImportOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []ImportError{{Index: 0, UID: "lunch@test", Summary: "Lunch, Bob; Alice", Error: "This calendar does not store events"}}
	if !reflect.DeepEqual(res.Errors, want) {
		t.Fatalf("got %+v\nwant %+v", res.Errors, want)
	}
}
