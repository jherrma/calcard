package importexport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// mockCalendarRepo is a minimal in-memory calendar.CalendarRepository used to
// exercise CalendarImportUseCase.Execute. It keeps a live object store so
// creates/deletes performed during a single import are observable, and counts
// GetCalendarObjects calls so we can assert the fetch is hoisted out of the
// per-event loop (was O(N*M), now O(1) per import).
type mockCalendarRepo struct {
	cal *calendar.Calendar

	objects []*calendar.CalendarObject

	getObjectsCalls int
	getObjectsErr   error

	created []*calendar.CalendarObject
	deleted []*calendar.CalendarObject

	nextID uint
}

func (m *mockCalendarRepo) GetByUUID(_ context.Context, _ string) (*calendar.Calendar, error) {
	return m.cal, nil
}

func (m *mockCalendarRepo) GetCalendarObjects(_ context.Context, _ uint) ([]*calendar.CalendarObject, error) {
	m.getObjectsCalls++
	if m.getObjectsErr != nil {
		return nil, m.getObjectsErr
	}
	// Return a snapshot copy, matching a real repository (callers must not
	// mutate the backing store through the returned slice).
	out := make([]*calendar.CalendarObject, len(m.objects))
	copy(out, m.objects)
	return out, nil
}

func (m *mockCalendarRepo) CreateCalendarObject(_ context.Context, obj *calendar.CalendarObject) error {
	m.nextID++
	obj.ID = m.nextID
	m.objects = append(m.objects, obj)
	m.created = append(m.created, obj)
	return nil
}

func (m *mockCalendarRepo) DeleteCalendarObject(_ context.Context, obj *calendar.CalendarObject) error {
	for i, o := range m.objects {
		if o == obj {
			m.objects = append(m.objects[:i], m.objects[i+1:]...)
			break
		}
	}
	m.deleted = append(m.deleted, obj)
	return nil
}

// Unused interface methods — stubs to satisfy calendar.CalendarRepository.
func (m *mockCalendarRepo) Create(context.Context, *calendar.Calendar) error { return nil }
func (m *mockCalendarRepo) GetByID(context.Context, uint) (*calendar.Calendar, error) {
	return nil, nil
}
func (m *mockCalendarRepo) ListByUserID(context.Context, uint) ([]*calendar.Calendar, error) {
	return nil, nil
}
func (m *mockCalendarRepo) Update(context.Context, *calendar.Calendar) error         { return nil }
func (m *mockCalendarRepo) UpdateMetadata(context.Context, *calendar.Calendar) error { return nil }
func (m *mockCalendarRepo) Delete(context.Context, uint) error                       { return nil }
func (m *mockCalendarRepo) CountByUserID(context.Context, uint) (int64, error)       { return 0, nil }
func (m *mockCalendarRepo) GetEventCount(context.Context, uint) (int64, error)       { return 0, nil }
func (m *mockCalendarRepo) GetByPath(context.Context, uint, string) (*calendar.Calendar, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetCalendarObjectByPath(context.Context, uint, string) (*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) UpdateCalendarObject(context.Context, *calendar.CalendarObject) error {
	return nil
}
func (m *mockCalendarRepo) MoveCalendarObject(context.Context, *calendar.CalendarObject, uint) error {
	return nil
}
func (m *mockCalendarRepo) GetChangesSinceToken(context.Context, uint, string) ([]*calendar.SyncChangeLog, error) {
	return nil, nil
}
func (m *mockCalendarRepo) RecordChange(context.Context, uint, string, string, string) error {
	return nil
}
func (m *mockCalendarRepo) ListEvents(context.Context, uint, time.Time, time.Time) ([]*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) SearchEvents(context.Context, calendar.EventSearchQuery) ([]*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetCalendarObjectByUUID(context.Context, string) (*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetCalendarObjectByUID(context.Context, uint, string) (*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetUserPermission(context.Context, uint, uint) (calendar.CalendarPermission, error) {
	return calendar.PermissionNone, nil
}
func (m *mockCalendarRepo) FindByPublicToken(context.Context, string) (*calendar.Calendar, error) {
	return nil, nil
}
func (m *mockCalendarRepo) ReplaceFeedObjects(context.Context, uint, []*calendar.CalendarObject) (calendar.FeedSyncStats, error) {
	return calendar.FeedSyncStats{}, nil
}

// icalWithDuplicateUID builds a VCALENDAR carrying `count` VEVENTs that all
// share the same UID (a duplicate-in-file scenario).
func icalWithDuplicateUID(uid string, count int) []byte {
	out := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n"
	for i := 0; i < count; i++ {
		out += "BEGIN:VEVENT\r\n"
		out += "UID:" + uid + "\r\n"
		out += "DTSTAMP:20251231T000000Z\r\n"
		out += fmt.Sprintf("SUMMARY:Event %d\r\n", i)
		out += "DTSTART:20260101T120000Z\r\n"
		out += "DTEND:20260101T130000Z\r\n"
		out += "END:VEVENT\r\n"
	}
	out += "END:VCALENDAR\r\n"
	return []byte(out)
}

func newImportFixture() (*CalendarImportUseCase, *mockCalendarRepo) {
	repo := &mockCalendarRepo{cal: &calendar.Calendar{ID: 1, UUID: "cal-uuid", UserID: 42}}
	return NewCalendarImportUseCase(repo), repo
}

// TestImport_DuplicateInFile_Skip: two VEVENTs sharing a UID with skip handling
// must import the first and skip the second — identical to the pre-fix
// per-iteration re-fetch — while fetching the object list only once.
func TestImport_DuplicateInFile_Skip(t *testing.T) {
	uc, repo := newImportFixture()

	res, err := uc.Execute(context.Background(), 42, "cal-uuid",
		icalWithDuplicateUID("dup@test", 2), ImportOptions{DuplicateHandling: "skip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Total != 2 || res.Imported != 1 || res.Skipped != 1 || res.Failed != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(repo.created))
	}
	if len(repo.deleted) != 0 {
		t.Fatalf("expected 0 deletes, got %d", len(repo.deleted))
	}
	if repo.getObjectsCalls != 1 {
		t.Fatalf("GetCalendarObjects should be called once, got %d", repo.getObjectsCalls)
	}
}

// TestImport_DuplicateInFile_Replace: two VEVENTs sharing a UID with replace
// handling. The second replaces the first: both count as imported, the first
// is deleted, and exactly one object remains — identical to the pre-fix
// re-fetch behavior — while fetching the object list only once.
func TestImport_DuplicateInFile_Replace(t *testing.T) {
	uc, repo := newImportFixture()

	res, err := uc.Execute(context.Background(), 42, "cal-uuid",
		icalWithDuplicateUID("dup@test", 2), ImportOptions{DuplicateHandling: "replace"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Total != 2 || res.Imported != 2 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(repo.created) != 2 {
		t.Fatalf("expected 2 creates, got %d", len(repo.created))
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("expected 1 delete, got %d", len(repo.deleted))
	}
	if len(repo.objects) != 1 {
		t.Fatalf("expected 1 surviving object, got %d", len(repo.objects))
	}
	// The survivor must be the second (last-wins) create.
	if repo.objects[0] != repo.created[1] {
		t.Fatalf("expected the last created object to survive")
	}
	if repo.getObjectsCalls != 1 {
		t.Fatalf("GetCalendarObjects should be called once, got %d", repo.getObjectsCalls)
	}
}

// TestImport_GetCalendarObjectsErrorSurfaced: a failing duplicate-check fetch
// must abort the import with the error instead of being silently discarded
// (which previously degraded into duplicate creation).
func TestImport_GetCalendarObjectsErrorSurfaced(t *testing.T) {
	uc, repo := newImportFixture()
	sentinel := errors.New("db exploded")
	repo.getObjectsErr = sentinel

	res, err := uc.Execute(context.Background(), 42, "cal-uuid",
		icalWithDuplicateUID("dup@test", 2), ImportOptions{DuplicateHandling: "skip"})
	if err == nil {
		t.Fatalf("expected error to be surfaced, got nil (result: %+v)", res)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result on error, got %+v", res)
	}
	if len(repo.created) != 0 {
		t.Fatalf("no objects should be created when the fetch fails, got %d", len(repo.created))
	}
}
