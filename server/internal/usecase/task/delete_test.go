package task

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteTask_DeletesTheObject: the task's object goes to
// HardDeleteCalendarObject as loaded, whatever it holds (a recurring task with
// an override is one object); nothing else is written. The stub has no
// DeleteCalendarObject, so a soft delete would panic.
func TestDeleteTask_DeletesTheObject(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
	}{
		{"plain task", nil},
		{"completed task", []string{"STATUS:COMPLETED"}},
		{"recurring task", []string{"DTSTART:20261105T080000Z", "RRULE:FREQ=WEEKLY"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := simpleTask(t, "t1", tc.props...)
			repo := repoWith(obj)
			require.NoError(t, NewDeleteTaskUseCase(repo).Execute(context.Background(), DeleteTaskInput{CalendarID: 7, UUID: "t1"}))
			require.Len(t, repo.deleted, 1)
			assert.Equal(t, *obj, repo.deleted[0], "the loaded object, unchanged")
			assert.Empty(t, repo.updated)
		})
	}
}

// TestDeleteTask_NotATaskOfThisCalendar: an event's UUID and a task filed
// under another calendar are ErrNotFound and nothing is deleted; a failed
// lookup is passed on as is.
func TestDeleteTask_NotATaskOfThisCalendar(t *testing.T) {
	event := stubEvent("event", 7)
	elsewhere := simpleTask(t, "elsewhere")
	elsewhere.CalendarID = 8
	repo := repoWith(event, elsewhere)
	uc := NewDeleteTaskUseCase(repo)

	for _, uuid := range []string{"event", "elsewhere"} {
		err := uc.Execute(context.Background(), DeleteTaskInput{CalendarID: 7, UUID: uuid})
		assert.ErrorIs(t, err, ErrNotFound, uuid)
	}
	err := uc.Execute(context.Background(), DeleteTaskInput{CalendarID: 7, UUID: "unknown"})
	assert.ErrorIs(t, err, errStubNotFound)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.Empty(t, repo.deleted)

	err = NewDeleteTaskUseCase(&stubRepo{lookErr: errors.New("disk I/O error")}).
		Execute(context.Background(), DeleteTaskInput{CalendarID: 7, UUID: "t1"})
	assert.EqualError(t, err, "disk I/O error")
}

// TestDeleteTask_Failure: a storage error from the delete is passed on.
func TestDeleteTask_Failure(t *testing.T) {
	repo := repoWith(simpleTask(t, "t1"))
	repo.deleteErr = errors.New("database is locked")
	err := NewDeleteTaskUseCase(repo).Execute(context.Background(), DeleteTaskInput{CalendarID: 7, UUID: "t1"})
	assert.EqualError(t, err, "database is locked")
	assert.NotErrorIs(t, err, ErrNotFound)
}

// TestDeleteTask_AlreadyGone: a row that vanished between the lookup and the
// delete (a concurrent DELETE won; the repository answers
// calendar.ErrNotFound) is the 404 of an unknown task, not a server error.
func TestDeleteTask_AlreadyGone(t *testing.T) {
	repo := repoWith(simpleTask(t, "t1"))
	repo.deleteErr = fmt.Errorf("hard delete: %w", calendar.ErrNotFound)
	err := NewDeleteTaskUseCase(repo).Execute(context.Background(), DeleteTaskInput{CalendarID: 7, UUID: "t1"})
	assert.ErrorIs(t, err, ErrNotFound)
}
