package event

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// componentSetEventICal is a plain timed event, as the REST create stores it.
const componentSetEventICal = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:cs-event-1\r\nDTSTAMP:20300101T000000Z\r\n" +
	"DTSTART:20300115T100000Z\r\nDTEND:20300115T110000Z\r\nSUMMARY:Planning\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

// componentSetTarget is the calendar id the tests file under a given
// supported_components value.
const componentSetTarget uint = 7

// componentSetCases lists every stored supported_components value and whether
// it stores events. "" is a legacy row and means both components.
var componentSetCases = []struct {
	stored      string
	storeEvents bool
}{
	{"VTODO", false},
	{"VEVENT", true},
	{"VEVENT,VTODO", true},
	{"", true},
}

func componentSetRepo(stored string) *stubCalendarRepo {
	return &stubCalendarRepo{calendars: map[uint]*calendar.Calendar{
		componentSetTarget: {ID: componentSetTarget, SupportedComponents: stored},
	}}
}

// assertNotStoredRefusal checks the one refusal of #170: an ErrInvalidInput
// (400 over REST, a tool error over MCP) whose message is exactly the REST body.
func assertNotStoredRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the write to be refused, got nil error")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error must be ErrInvalidInput (a 400), got %v", err)
	}
	if !errors.Is(err, calendar.ErrComponentNotStored) {
		t.Errorf("error must be calendar.ErrComponentNotStored, got %v", err)
	}
	if got, want := err.Error(), "This calendar does not store events"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// TestCreateEvent_CalendarMustStoreEvents: a REST/MCP event create into a
// calendar that does not store VEVENT (a task list) is refused and stores
// nothing; every other stored set behaves as before.
func TestCreateEvent_CalendarMustStoreEvents(t *testing.T) {
	for _, tc := range componentSetCases {
		t.Run("stored="+tc.stored, func(t *testing.T) {
			repo := componentSetRepo(tc.stored)
			obj, err := NewCreateEventUseCase(repo).Execute(context.Background(), CreateEventInput{
				CalendarID: componentSetTarget,
				Summary:    "Planning",
				Start:      time.Date(2030, 1, 15, 10, 0, 0, 0, time.UTC),
				End:        time.Date(2030, 1, 15, 11, 0, 0, 0, time.UTC),
			})
			if !tc.storeEvents {
				assertNotStoredRefusal(t, err)
				if obj != nil || repo.created != nil {
					t.Fatal("a refused create must store nothing")
				}
				return
			}
			if err != nil {
				t.Fatalf("create into %q: %v", tc.stored, err)
			}
			if repo.created == nil || repo.created.CalendarID != componentSetTarget {
				t.Fatalf("event not stored in calendar %d: %+v", componentSetTarget, repo.created)
			}
		})
	}
}

// TestCreateEvent_CalendarCheckComesFirst: a task list refuses an event even
// when the request is invalid otherwise; no correction of the times would help.
func TestCreateEvent_CalendarCheckComesFirst(t *testing.T) {
	repo := componentSetRepo("VTODO")
	_, err := NewCreateEventUseCase(repo).Execute(context.Background(), CreateEventInput{
		CalendarID: componentSetTarget,
		Summary:    "No times",
	})
	assertNotStoredRefusal(t, err)
}

// TestUpdateEvent_CalendarMustStoreEvents: rewriting an event that sits in a
// calendar which does not store events (it can only have got there before #170)
// is refused like a create, and nothing is written.
func TestUpdateEvent_CalendarMustStoreEvents(t *testing.T) {
	for _, tc := range componentSetCases {
		t.Run("stored="+tc.stored, func(t *testing.T) {
			repo := componentSetRepo(tc.stored)
			repo.obj = &calendar.CalendarObject{
				UUID:       "cs-event-uuid",
				CalendarID: componentSetTarget,
				UID:        "cs-event-1",
				ICalData:   componentSetEventICal,
			}
			if err := repo.obj.PopulateDenormFieldsFromICal(); err != nil {
				t.Fatal(err)
			}
			summary := "Renamed"
			updated, err := NewUpdateEventUseCase(repo).Execute(context.Background(), UpdateEventInput{
				UUID:    "cs-event-uuid",
				Summary: &summary,
				Scope:   "all",
			})
			if !tc.storeEvents {
				assertNotStoredRefusal(t, err)
				if updated != nil || repo.updated != nil {
					t.Fatal("a refused update must write nothing")
				}
				return
			}
			if err != nil {
				t.Fatalf("update in %q: %v", tc.stored, err)
			}
			if repo.updated == nil || repo.updated.Summary != "Renamed" {
				t.Fatalf("update not written: %+v", repo.updated)
			}
		})
	}
}

// TestMoveEvent_TargetMustStoreEvents: a move into a calendar that does not
// store events is refused before anything is written, and the event keeps its
// calendar; a move into any other stored set behaves as before.
func TestMoveEvent_TargetMustStoreEvents(t *testing.T) {
	const source uint = 1 // not filed: a legacy calendar
	for _, tc := range componentSetCases {
		t.Run("stored="+tc.stored, func(t *testing.T) {
			repo := componentSetRepo(tc.stored)
			repo.obj = &calendar.CalendarObject{UUID: "cs-event-uuid", CalendarID: source, ComponentType: calendar.ComponentEvent}
			moved, err := NewMoveEventUseCase(repo).Execute(context.Background(), MoveEventInput{
				EventUUID:        "cs-event-uuid",
				TargetCalendarID: componentSetTarget,
			})
			if !tc.storeEvents {
				assertNotStoredRefusal(t, err)
				if moved != nil || repo.moved != nil {
					t.Fatal("a refused move must write nothing")
				}
				if repo.obj.CalendarID != source {
					t.Fatalf("the event must keep its calendar, now in %d", repo.obj.CalendarID)
				}
				return
			}
			if err != nil {
				t.Fatalf("move into %q: %v", tc.stored, err)
			}
			if repo.moved == nil || repo.moved.CalendarID != componentSetTarget {
				t.Fatalf("event not moved into %d: %+v", componentSetTarget, repo.moved)
			}
		})
	}
}

// TestMoveEvent_SameCalendarStillChecksTarget: the target check runs before the
// "already there" shortcut, so the answer depends on the target alone.
func TestMoveEvent_SameCalendarStillChecksTarget(t *testing.T) {
	repo := componentSetRepo("VTODO")
	repo.obj = &calendar.CalendarObject{UUID: "cs-event-uuid", CalendarID: componentSetTarget, ComponentType: calendar.ComponentEvent}
	_, err := NewMoveEventUseCase(repo).Execute(context.Background(), MoveEventInput{
		EventUUID:        "cs-event-uuid",
		TargetCalendarID: componentSetTarget,
	})
	assertNotStoredRefusal(t, err)
}

// componentSetTaskICal is a task as a CalDAV client (Apple Reminders,
// Tasks.org) writes it.
const componentSetTaskICal = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
	"BEGIN:VTODO\r\nUID:cs-task-1\r\nDTSTAMP:20300101T000000Z\r\n" +
	"DUE:20300115T100000Z\r\nSUMMARY:Renew passport\r\n" +
	"END:VTODO\r\nEND:VCALENDAR\r\n"

// TestEventUseCases_TaskIsNotFound (#170, spec §5 rule 7): get, update,
// delete and move answer a task's UUID with ErrNotFound, never with the task
// and never with a 400 or 500, and write nothing. The calendar's component
// set does not matter: a task in a task list is an unknown event, not a
// refused write.
func TestEventUseCases_TaskIsNotFound(t *testing.T) {
	newTask := func(t *testing.T) *calendar.CalendarObject {
		t.Helper()
		obj := &calendar.CalendarObject{UUID: "cs-task-uuid", CalendarID: componentSetTarget, UID: "cs-task-1", ICalData: componentSetTaskICal}
		if err := obj.PopulateDenormFieldsFromICal(); err != nil {
			t.Fatal(err)
		}
		if obj.ComponentType != calendar.ComponentToDo {
			t.Fatalf("fixture is not a task: %q", obj.ComponentType)
		}
		return obj
	}
	summary := "Renamed"
	ops := []struct {
		name string
		run  func(repo *stubCalendarRepo) (*calendar.CalendarObject, error)
	}{
		{"get", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return NewGetEventUseCase(repo).Execute(context.Background(), "cs-task-uuid")
		}},
		{"update", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return NewUpdateEventUseCase(repo).Execute(context.Background(), UpdateEventInput{UUID: "cs-task-uuid", Summary: &summary, Scope: "all"})
		}},
		{"update one occurrence", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return NewUpdateEventUseCase(repo).Execute(context.Background(), UpdateEventInput{
				UUID: "cs-task-uuid", Summary: &summary, Scope: "this", RecurrenceID: "20300115T100000Z",
			})
		}},
		{"delete", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return nil, NewDeleteEventUseCase(repo).Execute(context.Background(), "cs-task-uuid", "all", "")
		}},
		{"delete one occurrence", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return nil, NewDeleteEventUseCase(repo).Execute(context.Background(), "cs-task-uuid", "this", "20300115T100000Z")
		}},
		{"delete with a bad scope", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return nil, NewDeleteEventUseCase(repo).Execute(context.Background(), "cs-task-uuid", "future", "")
		}},
		{"move into another calendar", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return NewMoveEventUseCase(repo).Execute(context.Background(), MoveEventInput{EventUUID: "cs-task-uuid", TargetCalendarID: componentSetTarget + 1})
		}},
		{"move into its own calendar", func(repo *stubCalendarRepo) (*calendar.CalendarObject, error) {
			return NewMoveEventUseCase(repo).Execute(context.Background(), MoveEventInput{EventUUID: "cs-task-uuid", TargetCalendarID: componentSetTarget})
		}},
	}
	for _, tc := range componentSetCases {
		for _, op := range ops {
			t.Run("stored="+tc.stored+"/"+op.name, func(t *testing.T) {
				repo := componentSetRepo(tc.stored)
				repo.obj = newTask(t)
				before := *repo.obj

				got, err := op.run(repo)
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("error = %v, want ErrNotFound", err)
				}
				if errors.Is(err, ErrInvalidInput) {
					t.Fatalf("a task must not be a 400: %v", err)
				}
				if got != nil {
					t.Fatalf("the task must not be returned: %+v", got)
				}
				if repo.updated != nil || repo.deleted != nil || repo.moved != nil || repo.created != nil {
					t.Fatal("nothing may be written for a task")
				}
				if repo.obj.CalendarID != before.CalendarID || repo.obj.ICalData != before.ICalData || repo.obj.ETag != before.ETag {
					t.Fatal("the task must be left untouched")
				}
			})
		}
	}
}

// TestGetEvent_ReturnsEvents: the get use case still returns an event.
func TestGetEvent_ReturnsEvents(t *testing.T) {
	repo := componentSetRepo("VEVENT")
	repo.obj = &calendar.CalendarObject{UUID: "cs-event-uuid", CalendarID: componentSetTarget, UID: "cs-event-1", ICalData: componentSetEventICal}
	if err := repo.obj.PopulateDenormFieldsFromICal(); err != nil {
		t.Fatal(err)
	}
	got, err := NewGetEventUseCase(repo).Execute(context.Background(), "cs-event-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if got != repo.obj {
		t.Fatalf("got %+v, want the stored event", got)
	}
}

// TestEventUseCases_MissingObjectIsNotFound: a repository that answers a
// lookup with no object and no error (nothing here does, but the contract does
// not forbid it) is ErrNotFound too, never a nil dereference.
func TestEventUseCases_MissingObjectIsNotFound(t *testing.T) {
	repo := componentSetRepo("VEVENT")
	if _, err := NewGetEventUseCase(repo).Execute(context.Background(), "nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
