package task

import (
	"context"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskList(components string) *calendar.Calendar {
	return &calendar.Calendar{ID: 7, UUID: "list-uuid", SupportedComponents: components}
}

// TestListTasks_Query: what the use case asks the repository for — the one
// calendar, the status filter ("" is open), the trimmed text and the page.
func TestListTasks_Query(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   ListTasksInput
		want calendar.TaskQuery
	}{
		{"defaults", ListTasksInput{},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: DefaultLimit}},
		{"open", ListTasksInput{Status: "open", Limit: 20, Offset: 40},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: 20, Offset: 40}},
		{"completed", ListTasksInput{Status: "completed"},
			calendar.TaskQuery{Status: calendar.TaskFilterCompleted, Limit: DefaultLimit}},
		{"all", ListTasksInput{Status: "all"},
			calendar.TaskQuery{Status: calendar.TaskFilterAll, Limit: DefaultLimit}},
		{"text is trimmed", ListTasksInput{Query: "  passport \t"},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Text: "passport", Limit: DefaultLimit}},
		{"limit is capped", ListTasksInput{Limit: 5000},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: MaxLimit}},
		{"limit at the cap", ListTasksInput{Limit: MaxLimit},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: MaxLimit}},
		{"limit 1", ListTasksInput{Limit: 1},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: 1}},
		{"negative limit and offset", ListTasksInput{Limit: -3, Offset: -9},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: DefaultLimit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepo{}
			tc.in.Calendar = taskList("VTODO")
			_, err := NewListTasksUseCase(repo).Execute(context.Background(), tc.in)
			require.NoError(t, err)
			require.Len(t, repo.queries, 1, "exactly one ListTasks query")
			tc.want.CalendarIDs = []uint{7}
			assert.Equal(t, tc.want, repo.queries[0])
		})
	}
	assert.Equal(t, 500, DefaultLimit)
	assert.Equal(t, 1000, MaxLimit)
}

// TestListTasks_InvalidStatus: the status values are a closed set compared
// exactly; anything else is ErrInvalidInput with the bare message (a 400), and
// no query runs — also on a calendar that does not store tasks.
func TestListTasks_InvalidStatus(t *testing.T) {
	for _, status := range []string{"OPEN", "Open", " open", "done", "cancelled", "needs-action", "open,completed", "*"} {
		for _, components := range []string{"VTODO", "VEVENT"} {
			repo := &stubRepo{}
			out, err := NewListTasksUseCase(repo).Execute(context.Background(), ListTasksInput{Calendar: taskList(components), Status: status})
			require.ErrorIs(t, err, ErrInvalidInput, "status %q", status)
			assert.Equal(t, `status must be "open", "completed" or "all"`, err.Error())
			assert.Nil(t, out)
			assert.Empty(t, repo.queries)
		}
	}
}

// TestListTasks_CalendarWithoutTasks: a calendar whose component set does not
// hold VTODO is not a task list; it answers an empty page (never nil, so the
// response is "tasks":[]) without a query. Mixed and legacy "" calendars store
// tasks and are queried.
func TestListTasks_CalendarWithoutTasks(t *testing.T) {
	for _, tc := range []struct {
		components string
		queried    bool
	}{
		{"VEVENT", false},
		{"VTODO", true},
		{"VEVENT,VTODO", true},
		{"", true},
	} {
		t.Run("stored="+tc.components, func(t *testing.T) {
			repo := &stubRepo{listed: []*calendar.CalendarObject{stubTask("t", 7)}, listTotal: 1}
			out, err := NewListTasksUseCase(repo).Execute(context.Background(), ListTasksInput{Calendar: taskList(tc.components), Status: "all"})
			require.NoError(t, err)
			require.NotNil(t, out.Tasks)
			if tc.queried {
				assert.Len(t, repo.queries, 1)
				assert.Len(t, out.Tasks, 1)
				assert.Equal(t, int64(1), out.Total)
				return
			}
			assert.Empty(t, repo.queries, "no query for a calendar that does not store tasks")
			assert.Empty(t, out.Tasks)
			assert.Zero(t, out.Total)
		})
	}
}

// TestListTasks_Result: the repository's page in its order, each task with its
// decoded details, and the repository's total (not the page length).
func TestListTasks_Result(t *testing.T) {
	first := stubTask("first", 7, "DUE:20301105T090000Z")
	recurring := stubTask("recurring", 7, "DUE:20301106T090000Z", "RRULE:FREQ=DAILY")
	repo := &stubRepo{listed: []*calendar.CalendarObject{first, recurring}, listTotal: 42}

	out, err := NewListTasksUseCase(repo).Execute(context.Background(), ListTasksInput{Calendar: taskList("VTODO"), Limit: 2})
	require.NoError(t, err)
	require.Len(t, out.Tasks, 2)
	assert.Same(t, first, out.Tasks[0].Object)
	assert.False(t, out.Tasks[0].Details.Recurring)
	assert.Same(t, recurring, out.Tasks[1].Object)
	assert.True(t, out.Tasks[1].Details.Recurring)
	assert.Equal(t, "FREQ=DAILY", out.Tasks[1].Details.RRule)
	assert.Equal(t, int64(42), out.Total)
}

// TestListTasks_Failures: a storage error is passed on (a logged 500), never
// as ErrInvalidInput; a missing calendar is a programming error, not an empty
// list.
func TestListTasks_Failures(t *testing.T) {
	storage := errors.New("no such table: calendar_objects")
	repo := &stubRepo{listErr: storage}
	out, err := NewListTasksUseCase(repo).Execute(context.Background(), ListTasksInput{Calendar: taskList("VTODO")})
	assert.ErrorIs(t, err, storage)
	assert.NotErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, out)

	out, err = NewListTasksUseCase(&stubRepo{}).Execute(context.Background(), ListTasksInput{})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, out)
}
