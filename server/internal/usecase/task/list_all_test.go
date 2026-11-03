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

// readableSet is what ListReadableCalendarIDs answers for user 42 in these
// tests: own, shared and subscribed calendars of every component set.
func readableSet() []calendar.ReadableCalendar {
	return []calendar.ReadableCalendar{
		{ID: 1, UUID: "own-tasks", Name: "Tasks", Color: "#111111", SupportedComponents: "VTODO"},
		{ID: 2, UUID: "own-events", Name: "Personal", Color: "#222222", SupportedComponents: "VEVENT"},
		{ID: 3, UUID: "mixed", Name: "Mixed", Color: "#333333", SupportedComponents: "VEVENT,VTODO"},
		{ID: 4, UUID: "legacy", Name: "Legacy", Color: "#444444", SupportedComponents: ""},
		{ID: 5, UUID: "shared-tasks", Name: "Team", Color: "#555555", SupportedComponents: "VTODO"},
		{ID: 6, UUID: "journal", Name: "Journal", Color: "#666666", SupportedComponents: "VJOURNAL"},
	}
}

// taskListIDs are the ids of readableSet that store VTODO, in its order.
var taskListIDs = []uint{1, 3, 4, 5}

func runListAll(t *testing.T, repo *stubRepo, in ListAllTasksInput) (*ListAllTasksOutput, error) {
	t.Helper()
	return NewListAllTasksUseCase(repo).Execute(context.Background(), in)
}

// TestListAllTasks_Query: one ListReadableCalendarIDs for the user and ONE
// ListTasks over every readable calendar that stores tasks — however many
// there are — with the status filter ("" is open), the trimmed text, the due
// bounds in UTC and the page.
func TestListAllTasks_Query(t *testing.T) {
	plus5 := time.FixedZone("+05:00", 5*3600)
	after := time.Date(2026, 11, 3, 5, 0, 0, 0, plus5)
	before := time.Date(2026, 11, 10, 0, 0, 0, 0, time.FixedZone("-08:00", -8*3600))
	afterUTC := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	beforeUTC := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		in   ListAllTasksInput
		want calendar.TaskQuery
	}{
		{"defaults", ListAllTasksInput{},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: DefaultLimit}},
		{"completed, paged", ListAllTasksInput{Status: "completed", Limit: 20, Offset: 40},
			calendar.TaskQuery{Status: calendar.TaskFilterCompleted, Limit: 20, Offset: 40}},
		{"all", ListAllTasksInput{Status: "all"},
			calendar.TaskQuery{Status: calendar.TaskFilterAll, Limit: DefaultLimit}},
		{"text is trimmed", ListAllTasksInput{Query: "  passport \t"},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Text: "passport", Limit: DefaultLimit}},
		{"limit is capped", ListAllTasksInput{Limit: 5000},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: MaxLimit}},
		{"negative limit and offset", ListAllTasksInput{Limit: -3, Offset: -9},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, Limit: DefaultLimit}},
		{"due bounds in UTC", ListAllTasksInput{DueAfter: &after, DueBefore: &before},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, DueAfter: &afterUTC, DueBefore: &beforeUTC, Limit: DefaultLimit}},
		{"due after only", ListAllTasksInput{DueAfter: &after},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, DueAfter: &afterUTC, Limit: DefaultLimit}},
		{"due before only", ListAllTasksInput{DueBefore: &before},
			calendar.TaskQuery{Status: calendar.TaskFilterOpen, DueBefore: &beforeUTC, Limit: DefaultLimit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepo{readable: readableSet()}
			tc.in.UserID = 42
			out, err := runListAll(t, repo, tc.in)
			require.NoError(t, err)
			assert.Equal(t, []uint{42}, repo.readableUsers, "one ListReadableCalendarIDs for the caller")
			require.Len(t, repo.queries, 1, "exactly one ListTasks query")
			tc.want.CalendarIDs = taskListIDs
			assert.Equal(t, tc.want, repo.queries[0])
			assert.NotNil(t, out.Tasks)
		})
	}

	// The caller's times are not changed.
	assert.Equal(t, plus5, after.Location())
}

// TestListAllTasks_CalendarUUID: calendar_uuid narrows the readable set to
// that calendar and never widens it. A readable task list (own, shared,
// mixed, legacy) is queried alone; a readable calendar that does not store
// tasks gives an empty page without a task query; a UUID outside the readable
// set (unknown, another user's, another spelling) is ErrCalendarNotFound —
// never an empty page — again without a task query.
func TestListAllTasks_CalendarUUID(t *testing.T) {
	for _, tc := range []struct {
		uuid string
		ids  []uint
	}{
		{"own-tasks", []uint{1}},
		{"mixed", []uint{3}},
		{"legacy", []uint{4}},
		{"shared-tasks", []uint{5}},
		{"", taskListIDs},
	} {
		repo := &stubRepo{readable: readableSet()}
		_, err := runListAll(t, repo, ListAllTasksInput{UserID: 42, CalendarUUID: tc.uuid})
		require.NoError(t, err, tc.uuid)
		require.Len(t, repo.queries, 1, tc.uuid)
		assert.Equal(t, tc.ids, repo.queries[0].CalendarIDs, tc.uuid)
	}

	for _, uuid := range []string{"own-events", "journal"} {
		repo := &stubRepo{readable: readableSet()}
		out, err := runListAll(t, repo, ListAllTasksInput{UserID: 42, CalendarUUID: uuid})
		require.NoError(t, err, uuid)
		require.NotNil(t, out.Tasks, uuid)
		assert.Empty(t, out.Tasks, uuid)
		assert.Zero(t, out.Total, uuid)
		assert.Empty(t, repo.queries, "%s: no task query", uuid)
	}

	for _, tc := range []struct {
		name     string
		readable []calendar.ReadableCalendar
		uuid     string
	}{
		{"unknown", readableSet(), "00000000-0000-4000-8000-000000000000"},
		{"another spelling", readableSet(), "OWN-TASKS"},
		{"padded", readableSet(), " own-tasks"},
		{"no readable calendar at all", nil, "own-tasks"},
	} {
		repo := &stubRepo{readable: tc.readable}
		out, err := runListAll(t, repo, ListAllTasksInput{UserID: 42, CalendarUUID: tc.uuid, Status: "all"})
		require.ErrorIs(t, err, ErrCalendarNotFound, tc.name)
		assert.NotErrorIs(t, err, ErrInvalidInput, tc.name)
		assert.Equal(t, "calendar not found", err.Error())
		assert.Nil(t, out, tc.name)
		assert.Empty(t, repo.queries, "%s: no task query", tc.name)
	}
}

// TestListAllTasks_InvalidStatus: the per-list status rule; an invalid one is
// ErrInvalidInput with the bare message before anything is read — also when
// the calendar_uuid filter names no readable calendar.
func TestListAllTasks_InvalidStatus(t *testing.T) {
	for _, status := range []string{"OPEN", " open", "done", "cancelled", "needs-action", "*"} {
		for _, uuid := range []string{"", "own-tasks", "unknown"} {
			repo := &stubRepo{readable: readableSet()}
			out, err := runListAll(t, repo, ListAllTasksInput{UserID: 42, Status: status, CalendarUUID: uuid})
			require.ErrorIs(t, err, ErrInvalidInput, "status %q", status)
			assert.Equal(t, `status must be "open", "completed" or "all"`, err.Error())
			assert.Nil(t, out)
			assert.Empty(t, repo.readableUsers, "nothing read")
			assert.Empty(t, repo.queries)
		}
	}
}

// TestListAllTasks_NoTaskLists: a user who reads no calendar, or only
// calendars that do not store tasks, gets an empty page (never nil, so the
// response is "tasks":[]) and no task query.
func TestListAllTasks_NoTaskLists(t *testing.T) {
	for name, readable := range map[string][]calendar.ReadableCalendar{
		"no calendar":        nil,
		"events calendars":   {{ID: 2, UUID: "a", SupportedComponents: "VEVENT"}, {ID: 9, UUID: "b", SupportedComponents: "VEVENT"}},
		"a journal calendar": {{ID: 6, UUID: "j", SupportedComponents: "VJOURNAL"}},
	} {
		repo := &stubRepo{readable: readable}
		out, err := runListAll(t, repo, ListAllTasksInput{UserID: 7})
		require.NoError(t, err, name)
		require.NotNil(t, out.Tasks, name)
		assert.Empty(t, out.Tasks, name)
		assert.Zero(t, out.Total, name)
		assert.Equal(t, []uint{7}, repo.readableUsers, name)
		assert.Empty(t, repo.queries, "%s: no task query", name)
	}
}

// TestListAllTasks_Result: every task comes back in the repository's order
// with the calendar that holds it and its decoded details; the total is the
// repository's.
func TestListAllTasks_Result(t *testing.T) {
	inTeam := stubTask("t-team", 5, "DUE:20301105T090000Z")
	inMixed := stubTask("t-mixed", 3, "RRULE:FREQ=WEEKLY", "DTSTART;VALUE=DATE:20301101")
	inOwn := stubTask("t-own", 1)
	inLegacy := stubTask("t-legacy", 4)
	repo := &stubRepo{readable: readableSet(), listed: []*calendar.CalendarObject{inTeam, inMixed, inOwn, inLegacy}, listTotal: 42}

	out, err := runListAll(t, repo, ListAllTasksInput{UserID: 42, Limit: 4})
	require.NoError(t, err)
	assert.Equal(t, int64(42), out.Total)
	require.Len(t, out.Tasks, 4)
	byUUID := map[string]calendar.ReadableCalendar{}
	for _, c := range readableSet() {
		byUUID[c.UUID] = c
	}
	for i, want := range []struct {
		obj *calendar.CalendarObject
		cal string
	}{{inTeam, "shared-tasks"}, {inMixed, "mixed"}, {inOwn, "own-tasks"}, {inLegacy, "legacy"}} {
		assert.Same(t, want.obj, out.Tasks[i].Object)
		assert.Equal(t, byUUID[want.cal], out.Tasks[i].Calendar)
		assert.Equal(t, want.obj.TaskDetails(), out.Tasks[i].Details)
	}
	assert.True(t, out.Tasks[1].Details.Recurring)
	assert.True(t, out.Tasks[1].Details.StartAllDay)
}

// TestListAllTasks_Failures: storage errors are passed on untyped (a 500,
// never a 400 or 404), and a row of a calendar outside the queried set — a
// repository that widened CalendarIDs — is an error, not a silently shown
// task of someone else.
func TestListAllTasks_Failures(t *testing.T) {
	boom := errors.New("disk I/O error")
	check := func(name string, repo *stubRepo) error {
		out, err := runListAll(t, repo, ListAllTasksInput{UserID: 42})
		require.Error(t, err, name)
		assert.Nil(t, out, name)
		assert.NotErrorIs(t, err, ErrInvalidInput, name)
		assert.NotErrorIs(t, err, ErrCalendarNotFound, name)
		assert.NotErrorIs(t, err, ErrNotFound, name)
		return err
	}

	err := check("readable calendars", &stubRepo{readableErr: boom})
	assert.ErrorIs(t, err, boom)

	repo := &stubRepo{readable: readableSet(), listErr: boom}
	err = check("task query", repo)
	assert.ErrorIs(t, err, boom)

	for _, calID := range []uint{2, 6, 99} {
		err = check("foreign row", &stubRepo{readable: readableSet(), listed: []*calendar.CalendarObject{
			stubTask("mine", 1), stubTask("foreign", calID),
		}, listTotal: 2})
		assert.Contains(t, err.Error(), "outside the readable calendars")
	}
}
