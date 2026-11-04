package task

import (
	"context"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// freshRepo hands out a copy of the template on every lookup, like a database
// that is read again, so a retried attempt sees the stored task and not the
// edited one of the previous attempt.
type freshRepo struct {
	*stubRepo
	template *calendar.CalendarObject
}

func (r freshRepo) GetCalendarObjectByUUID(context.Context, string) (*calendar.CalendarObject, error) {
	cp := *r.template
	return &cp, nil
}

// TestLoadTaskMapsRecordNotFound (MN1): a task deleted between the handler's
// permission check and the use case's lookup is ErrNotFound (404), not the
// repository's raw "record not found" (500), on every use case.
func TestLoadTaskMapsRecordNotFound(t *testing.T) {
	ctx := context.Background()
	repo := &stubRepo{lookErr: gorm.ErrRecordNotFound}
	cal := taskListCalendar()

	_, err := updateUC(repo).Execute(ctx, UpdateTaskInput{Calendar: cal, UUID: "t1", Summary: strPtr("x")})
	assert.ErrorIs(t, err, ErrNotFound, "update")
	_, err = completeUC(repo).Execute(ctx, CompleteTaskInput{Calendar: cal, UUID: "t1"})
	assert.ErrorIs(t, err, ErrNotFound, "complete")
	_, err = uncompleteUC(repo).Execute(ctx, CompleteTaskInput{Calendar: cal, UUID: "t1"})
	assert.ErrorIs(t, err, ErrNotFound, "uncomplete")
	err = NewDeleteTaskUseCase(repo).Execute(ctx, DeleteTaskInput{CalendarID: 7, UUID: "t1"})
	assert.ErrorIs(t, err, ErrNotFound, "delete")
	_, err = NewGetTaskUseCase(repo).Execute(ctx, GetTaskInput{CalendarID: 7, UUID: "t1"})
	assert.ErrorIs(t, err, ErrNotFound, "get")
}

// TestTaskWritesAreConditional (MJ1, MN3): every edit is stored over the ETag
// it loaded; a task deleted meanwhile is ErrNotFound; a task changed meanwhile
// is retried without an If-Match and ErrPreconditionFailed with one.
func TestTaskWritesAreConditional(t *testing.T) {
	ctx := context.Background()
	cal := taskListCalendar()
	runs := map[string]func(repo *stubRepo, ifMatch string) error{
		"update": func(repo *stubRepo, ifMatch string) error {
			_, err := updateUC(repo).Execute(ctx, UpdateTaskInput{Calendar: cal, UUID: "t1", Summary: strPtr("changed"), IfMatch: ifMatch})
			return err
		},
		"complete": func(repo *stubRepo, ifMatch string) error {
			_, err := completeUC(repo).Execute(ctx, CompleteTaskInput{Calendar: cal, UUID: "t1", IfMatch: ifMatch})
			return err
		},
	}
	for name, run := range runs {
		t.Run(name+" stores over the loaded ETag", func(t *testing.T) {
			repo := repoWith(simpleTask(t, "t1"))
			require.NoError(t, run(repo, ""))
			assert.Equal(t, []string{"etag-before"}, repo.loadedETags)
			require.Len(t, repo.updated, 1)
			assert.NotEqual(t, "etag-before", repo.updated[0].ETag)
		})
		t.Run(name+" on a task deleted meanwhile is not found", func(t *testing.T) {
			repo := repoWith(simpleTask(t, "t1"))
			repo.updateErr = calendar.ErrNotFound
			assert.ErrorIs(t, run(repo, ""), ErrNotFound)
			assert.Empty(t, repo.updated)
		})
		t.Run(name+" with an If-Match is refused when the task changed", func(t *testing.T) {
			repo := repoWith(simpleTask(t, "t1"))
			repo.updateErr = calendar.ErrETagMismatch
			assert.ErrorIs(t, run(repo, "etag-before"), ErrPreconditionFailed)
			assert.Len(t, repo.loadedETags, 1, "not retried")
		})
		t.Run(name+" with an If-Match naming another ETag writes nothing", func(t *testing.T) {
			repo := repoWith(simpleTask(t, "t1"))
			assert.ErrorIs(t, run(repo, "etag-other"), ErrPreconditionFailed)
			assert.Empty(t, repo.loadedETags)
		})
	}

	t.Run("retries are bounded", func(t *testing.T) {
		stub := repoWith()
		stub.updateErr = calendar.ErrETagMismatch
		repo := freshRepo{stubRepo: stub, template: simpleTask(t, "t1")}
		_, err := NewCompleteTaskUseCase(repo, nil).Execute(ctx, CompleteTaskInput{Calendar: cal, UUID: "t1"})
		assert.ErrorIs(t, err, ErrPreconditionFailed)
		assert.Len(t, stub.loadedETags, maxWriteAttempts)
	})

	t.Run("a retry stores the second attempt", func(t *testing.T) {
		stub := repoWith()
		stub.updateErrs = []error{calendar.ErrETagMismatch, nil}
		repo := freshRepo{stubRepo: stub, template: simpleTask(t, "t1")}
		task, err := NewCompleteTaskUseCase(repo, nil).Execute(ctx, CompleteTaskInput{Calendar: cal, UUID: "t1"})
		require.NoError(t, err)
		assert.Equal(t, "COMPLETED", task.Object.Status)
		assert.Len(t, stub.loadedETags, 2)
		assert.Len(t, stub.updated, 1)
	})

	t.Run("an update is retried on the fresh task", func(t *testing.T) {
		stub := repoWith()
		stub.updateErrs = []error{calendar.ErrETagMismatch, nil}
		repo := freshRepo{stubRepo: stub, template: simpleTask(t, "t1")}
		task, err := NewUpdateTaskUseCase(repo, nil).Execute(ctx, UpdateTaskInput{Calendar: cal, UUID: "t1", Summary: strPtr("changed")})
		require.NoError(t, err)
		assert.Equal(t, "changed", task.Object.Summary)
		assert.Len(t, stub.loadedETags, 2)
	})

	t.Run("uncomplete is conditional too", func(t *testing.T) {
		repo := repoWith(simpleTask(t, "t1", "STATUS:COMPLETED"))
		repo.updateErr = calendar.ErrNotFound
		_, err := uncompleteUC(repo).Execute(ctx, CompleteTaskInput{Calendar: cal, UUID: "t1"})
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Equal(t, []string{"etag-before"}, repo.loadedETags)
	})
}

// TestTaskTextControlCharacters (MN6): C0 control characters other than TAB
// (and the line breaks, which become \n) are a 400 in summary and
// description, on create and update.
func TestTaskTextControlCharacters(t *testing.T) {
	for _, bad := range []string{"a\x00b", "a\x07b", "a\x1bb", "a\x0bb"} {
		_, _, err := runCreate(CreateTaskInput{Calendar: taskListCalendar(), Summary: bad})
		assert.ErrorIs(t, err, ErrInvalidInput, "summary %q", bad)
		assert.Contains(t, err.Error(), "summary must not contain control characters")

		_, _, err = runCreate(CreateTaskInput{Calendar: taskListCalendar(), Summary: "ok", Description: bad})
		assert.ErrorIs(t, err, ErrInvalidInput, "description %q", bad)
		assert.Contains(t, err.Error(), "description must not contain control characters")

		_, _, err = runUpdate(t, simpleTask(t, "t1"), UpdateTaskInput{Summary: strPtr(bad)})
		assert.ErrorIs(t, err, ErrInvalidInput, "update summary %q", bad)
		_, _, err = runUpdate(t, simpleTask(t, "t1"), UpdateTaskInput{Description: change(bad)})
		assert.ErrorIs(t, err, ErrInvalidInput, "update description %q", bad)
	}

	t.Run("tab and line breaks are fine", func(t *testing.T) {
		task, _, err := runCreate(CreateTaskInput{Calendar: taskListCalendar(), Summary: "a\tb", Description: "one\r\ntwo\nthree"})
		require.NoError(t, err)
		assert.Equal(t, "a\tb", task.Object.Summary)
		assert.Equal(t, "one\ntwo\nthree", task.Object.Description)
	})
}

// TestUpdateTask_DateTypeWithOverrides (MN7): turning the DTSTART of a repeating
// master from a date into a date-time (or back) while RECURRENCE-ID overrides
// exist is a 400; without overrides it stays allowed.
func TestUpdateTask_DateTypeWithOverrides(t *testing.T) {
	series := func(withOverride bool) string {
		lines := []string{"BEGIN:VCALENDAR", "PRODID:-//test//EN", "VERSION:2.0"}
		if withOverride {
			lines = append(lines, "BEGIN:VTODO", "DTSTAMP:20260103T080000Z", "RECURRENCE-ID;VALUE=DATE:20261112",
				"SUMMARY:moved", "UID:s@example.com", "END:VTODO")
		}
		lines = append(lines, "BEGIN:VTODO", "DTSTAMP:20260102T080000Z", "DTSTART;VALUE=DATE:20261105",
			"DUE;VALUE=DATE:20261105", "RRULE:FREQ=WEEKLY", "SUMMARY:series", "UID:s@example.com", "END:VTODO", "END:VCALENDAR")
		return vcal(lines...)
	}
	toTimed := UpdateTaskInput{DueAllDay: boolPtr(false), Due: change("2026-11-06T10:00:00Z"), Start: change("2026-11-06T09:00:00Z")}

	_, repo, err := runUpdate(t, storedTask(t, "s", 7, series(true)), toTimed)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "changed occurrences")
	assert.Empty(t, repo.updated)

	_, repo, err = runUpdate(t, storedTask(t, "s", 7, series(false)), toTimed)
	require.NoError(t, err, "no overrides, nothing to orphan")
	assert.Len(t, repo.updated, 1)

	// The same value type, with overrides, is an ordinary edit.
	sameType := UpdateTaskInput{Start: change("2026-11-06"), Due: change("2026-11-07")}
	_, repo, err = runUpdate(t, storedTask(t, "s", 7, series(true)), sameType)
	require.NoError(t, err)
	assert.Len(t, repo.updated, 1)
	assert.Equal(t, 1, strings.Count(repo.updated[0].ICalData, "RECURRENCE-ID;VALUE=DATE:20261112"))
}
