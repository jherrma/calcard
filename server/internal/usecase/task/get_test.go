package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetTask_ReturnsTheTask: a task of the addressed calendar comes back with
// its object and the details that have no column.
func TestGetTask_ReturnsTheTask(t *testing.T) {
	task := stubTask("task-1", 7, "DTSTART;VALUE=DATE:20301104", "DUE;VALUE=DATE:20301105", "RRULE:FREQ=WEEKLY")
	repo := &stubRepo{objects: map[string]*calendar.CalendarObject{"task-1": task}}

	got, err := NewGetTaskUseCase(repo).Execute(context.Background(), GetTaskInput{CalendarID: 7, UUID: "task-1"})
	require.NoError(t, err)
	assert.Same(t, task, got.Object)
	assert.True(t, got.Details.Recurring)
	assert.Equal(t, "FREQ=WEEKLY", got.Details.RRule)
	assert.True(t, got.Details.StartAllDay)
	require.NotNil(t, got.Details.Start)
	assert.True(t, got.Details.Start.Equal(time.Date(2030, 11, 4, 0, 0, 0, 0, time.UTC)))
}

// TestGetTask_NotATaskOfThisCalendar: the mirror of the event rule (#170). An
// event's UUID, a task filed under another calendar and a nil object are all
// ErrNotFound, the one 404 "Task not found".
func TestGetTask_NotATaskOfThisCalendar(t *testing.T) {
	repo := &stubRepo{objects: map[string]*calendar.CalendarObject{
		"event":         stubEvent("event", 7),
		"elsewhere":     stubTask("elsewhere", 8),
		"nil-object":    nil,
		"lower-case-ct": {UUID: "lower-case-ct", CalendarID: 7, ComponentType: "vtodo"},
	}}
	for _, uuid := range []string{"event", "elsewhere", "nil-object", "lower-case-ct"} {
		t.Run(uuid, func(t *testing.T) {
			got, err := NewGetTaskUseCase(repo).Execute(context.Background(), GetTaskInput{CalendarID: 7, UUID: uuid})
			assert.ErrorIs(t, err, ErrNotFound)
			assert.Nil(t, got)
		})
	}
}

// TestGetTask_LookupFailureIsPassedOn: a failed lookup is returned as is, not
// turned into ErrNotFound (the handler answers it with a logged 500; an
// unknown UUID never gets here, the handler's own check answered 404 first).
func TestGetTask_LookupFailureIsPassedOn(t *testing.T) {
	storage := errors.New("database is locked")
	repo := &stubRepo{lookErr: storage}
	got, err := NewGetTaskUseCase(repo).Execute(context.Background(), GetTaskInput{CalendarID: 7, UUID: "task-1"})
	assert.ErrorIs(t, err, storage)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.Nil(t, got)

	got, err = NewGetTaskUseCase(&stubRepo{}).Execute(context.Background(), GetTaskInput{CalendarID: 7, UUID: "unknown"})
	assert.ErrorIs(t, err, errStubNotFound)
	assert.Nil(t, got)
}
