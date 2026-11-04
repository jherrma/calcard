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

func runSummary(t *testing.T, repo *stubRepo, in TaskSummaryInput) (*TaskSummary, error) {
	t.Helper()
	return NewTaskSummaryUseCase(repo).Execute(context.Background(), in)
}

// summaryAsOf is a user's "now" at +01:00; the use case must hand it to the
// repository as is (the offset decides the date).
var summaryAsOf = time.Date(2026, 11, 3, 15, 0, 0, 0, time.FixedZone("CET", 3600))

// TestTaskSummary_Query: one ListReadableCalendarIDs for the user and ONE
// CountTasks over every readable calendar that stores tasks (own, mixed,
// legacy "", shared) — never an events-only or journal calendar — at the
// caller's as_of, offset and all.
func TestTaskSummary_Query(t *testing.T) {
	repo := &stubRepo{readable: readableSet()}
	_, err := runSummary(t, repo, TaskSummaryInput{UserID: 42, AsOf: summaryAsOf})
	require.NoError(t, err)

	assert.Equal(t, []uint{42}, repo.readableUsers)
	require.Len(t, repo.countCalls, 1)
	assert.Equal(t, taskListIDs, repo.countCalls[0].ids)
	assert.Equal(t, summaryAsOf, repo.countCalls[0].asOf)
	_, offset := repo.countCalls[0].asOf.Zone()
	assert.Equal(t, 3600, offset, "as_of is not converted: its offset decides the date")
	assert.Empty(t, repo.queries, "no task is listed")
}

// TestTaskSummary_Result: the totals are the repository's, and by_calendar
// has an entry for EVERY readable task list, keyed by calendar UUID: the
// repository's counts where it has some, zeros for a list without tasks.
// Events-only and journal calendars have no entry.
func TestTaskSummary_Result(t *testing.T) {
	repo := &stubRepo{readable: readableSet(), counts: calendar.TaskCounts{
		Total: calendar.TaskCount{Open: 7, Overdue: 3, DueToday: 2, Completed: 41},
		ByCalendar: map[uint]calendar.TaskCount{
			1: {Open: 5, Overdue: 2, DueToday: 1, Completed: 40},
			5: {Open: 2, Overdue: 1, DueToday: 1, Completed: 1},
		},
	}}
	out, err := runSummary(t, repo, TaskSummaryInput{UserID: 42, AsOf: summaryAsOf})
	require.NoError(t, err)

	assert.Equal(t, calendar.TaskCount{Open: 7, Overdue: 3, DueToday: 2, Completed: 41}, out.Total)
	assert.Equal(t, map[string]calendar.TaskCount{
		"own-tasks":    {Open: 5, Overdue: 2, DueToday: 1, Completed: 40},
		"mixed":        {},
		"legacy":       {},
		"shared-tasks": {Open: 2, Overdue: 1, DueToday: 1, Completed: 1},
	}, out.ByCalendar)
}

// TestTaskSummary_NoTaskLists: a user who reads no task list gets zero counts
// and an empty (not nil) by_calendar, without a count query.
func TestTaskSummary_NoTaskLists(t *testing.T) {
	for name, readable := range map[string][]calendar.ReadableCalendar{
		"nothing readable": nil,
		"events and a journal only": {
			{ID: 2, UUID: "own-events", SupportedComponents: "VEVENT"},
			{ID: 6, UUID: "journal", SupportedComponents: "VJOURNAL"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &stubRepo{readable: readable}
			out, err := runSummary(t, repo, TaskSummaryInput{UserID: 42, AsOf: summaryAsOf})
			require.NoError(t, err)
			assert.Equal(t, calendar.TaskCount{}, out.Total)
			assert.NotNil(t, out.ByCalendar)
			assert.Empty(t, out.ByCalendar)
			assert.Empty(t, repo.countCalls, "no count query")
		})
	}
}

// TestTaskSummary_Failures: a storage failure is passed on untyped (a 500,
// never a 400 or 404); counts for a calendar outside the readable task lists
// are an error, never shown.
func TestTaskSummary_Failures(t *testing.T) {
	storage := errors.New("disk I/O error")

	t.Run("readable calendars", func(t *testing.T) {
		repo := &stubRepo{readableErr: storage}
		out, err := runSummary(t, repo, TaskSummaryInput{UserID: 42, AsOf: summaryAsOf})
		assert.Nil(t, out)
		assert.ErrorIs(t, err, storage)
		assert.Empty(t, repo.countCalls)
	})
	t.Run("counts", func(t *testing.T) {
		repo := &stubRepo{readable: readableSet(), countsErr: storage}
		out, err := runSummary(t, repo, TaskSummaryInput{UserID: 42, AsOf: summaryAsOf})
		assert.Nil(t, out)
		assert.ErrorIs(t, err, storage)
	})
	for _, id := range []uint{2, 6, 99} {
		repo := &stubRepo{readable: readableSet(), counts: calendar.TaskCounts{
			Total:      calendar.TaskCount{Open: 2},
			ByCalendar: map[uint]calendar.TaskCount{1: {Open: 1}, id: {Open: 1}},
		}}
		out, err := runSummary(t, repo, TaskSummaryInput{UserID: 42, AsOf: summaryAsOf})
		assert.Nil(t, out, "calendar %d", id)
		require.Error(t, err, "calendar %d", id)
		assert.NotErrorIs(t, err, ErrInvalidInput)
		assert.NotErrorIs(t, err, ErrCalendarNotFound)
	}
}
