package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// storeTodo stores a VTODO with the given extra property lines in cal through
// the repository, the way every write path does. created, when not zero, is
// its created_at (the last sort key).
func storeTodo(t *testing.T, repo *repository.CalendarRepository, cal *calendar.Calendar, summary string, created time.Time, props ...string) *calendar.CalendarObject {
	t.Helper()
	uid := uuid.New().String()
	body := "BEGIN:VTODO\r\nUID:" + uid + "\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:" + summary + "\r\n"
	for _, p := range props {
		body += p + "\r\n"
	}
	obj := &calendar.CalendarObject{
		UUID:       uuid.New().String(),
		CalendarID: cal.ID,
		UID:        uid,
		Path:       uid + ".ics",
		ETag:       calendar.NewETag(),
		ICalData:   "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" + body + "END:VTODO\r\nEND:VCALENDAR\r\n",
		CreatedAt:  created,
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.Equal(t, calendar.ComponentToDo, obj.ComponentType)
	require.NoError(t, repo.CreateCalendarObject(context.Background(), obj))
	return obj
}

func summaries(objs []*calendar.CalendarObject) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Summary)
	}
	return out
}

// listTasks runs ListTasks and fails the test on an error.
func listTasks(t *testing.T, repo *repository.CalendarRepository, q calendar.TaskQuery) ([]*calendar.CalendarObject, int64) {
	t.Helper()
	got, total, err := repo.ListTasks(context.Background(), q)
	require.NoError(t, err)
	return got, total
}

// TestListTasksOrder pins the server order of a task list (#170): due date
// ascending with undated tasks LAST, then priority ascending with unset
// priorities LAST, then created_at, then id. SQLite sorts NULLs first by
// default (PostgreSQL last), so a plain "ORDER BY due_at, priority" would put
// undated and unprioritised tasks first here.
func TestListTasksOrder(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	cal := seedCalendar(t, repo, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Stored in an order that matches none of the sort keys.
	storeTodo(t, repo, cal, "undated, no priority, older", t0)
	storeTodo(t, repo, cal, "undated, priority 9", t0.Add(time.Minute), "PRIORITY:9")
	storeTodo(t, repo, cal, "due 11-06", t0.Add(2*time.Minute), "DUE:20301106T090000Z")
	storeTodo(t, repo, cal, "due 11-05, no priority", t0.Add(3*time.Minute), "DUE:20301105T090000Z")
	storeTodo(t, repo, cal, "due 11-05, priority 5", t0.Add(4*time.Minute), "DUE:20301105T090000Z", "PRIORITY:5")
	storeTodo(t, repo, cal, "due 11-05, priority 1, newer", t0.Add(6*time.Minute), "DUE:20301105T090000Z", "PRIORITY:1")
	storeTodo(t, repo, cal, "due 11-05, priority 1, older", t0.Add(5*time.Minute), "DUE:20301105T090000Z", "PRIORITY:1")
	storeTodo(t, repo, cal, "undated, priority 1", t0.Add(7*time.Minute), "PRIORITY:1")
	storeTodo(t, repo, cal, "undated, no priority, newer", t0.Add(8*time.Minute), "PRIORITY:0") // 0 = none
	storeTodo(t, repo, cal, "due 2030-11-04 all day", t0.Add(9*time.Minute), "DUE;VALUE=DATE:20301104")
	// Same due, priority and created_at: id decides.
	twinA := storeTodo(t, repo, cal, "twin A", t0, "DUE:20301107T090000Z")
	twinB := storeTodo(t, repo, cal, "twin B", t0, "DUE:20301107T090000Z")
	require.Less(t, twinA.ID, twinB.ID)

	want := []string{
		"due 2030-11-04 all day",
		"due 11-05, priority 1, older",
		"due 11-05, priority 1, newer",
		"due 11-05, priority 5",
		"due 11-05, no priority",
		"due 11-06",
		"twin A",
		"twin B",
		"undated, priority 1",
		"undated, priority 9",
		"undated, no priority, older",
		"undated, no priority, newer",
	}
	got, total := listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{cal.ID}, Status: calendar.TaskFilterAll, Limit: 100})
	assert.Equal(t, want, summaries(got))
	assert.Equal(t, int64(len(want)), total)

	// Paging walks the same order without gaps or repeats.
	var paged []string
	for offset := 0; offset < len(want); offset += 5 {
		page, pageTotal := listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{cal.ID}, Status: calendar.TaskFilterAll, Limit: 5, Offset: offset})
		assert.Equal(t, int64(len(want)), pageTotal, "offset %d", offset)
		paged = append(paged, summaries(page)...)
	}
	assert.Equal(t, want, paged)
}

// TestListTasksStatusFilter: one cancelled rule, "done" = COMPLETED or
// CANCELLED. open = NEEDS-ACTION + IN-PROCESS, completed = done, all = every
// task. A VTODO row whose status column is empty (one the backfill could not
// decode; the API reports it as needs-action) counts as open.
func TestListTasksStatusFilter(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	cal := seedCalendar(t, repo, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	storeTodo(t, repo, cal, "needs action", t0)
	storeTodo(t, repo, cal, "in process", t0.Add(time.Minute), "STATUS:IN-PROCESS")
	storeTodo(t, repo, cal, "completed", t0.Add(2*time.Minute), "STATUS:COMPLETED")
	storeTodo(t, repo, cal, "completed by COMPLETED", t0.Add(3*time.Minute), "COMPLETED:20301101T100000Z")
	storeTodo(t, repo, cal, "cancelled", t0.Add(4*time.Minute), "STATUS:CANCELLED")
	storeTodo(t, repo, cal, "unknown status", t0.Add(5*time.Minute), "STATUS:WAITING")
	empty := storeTodo(t, repo, cal, "empty status column", t0.Add(6*time.Minute))
	require.NoError(t, db.Model(&calendar.CalendarObject{}).Where("id = ?", empty.ID).UpdateColumn("status", "").Error)
	null := storeTodo(t, repo, cal, "null status column", t0.Add(7*time.Minute))
	require.NoError(t, db.Model(&calendar.CalendarObject{}).Where("id = ?", null.ID).UpdateColumn("status", gorm.Expr("NULL")).Error)
	// An event in the same calendar is never a task.
	componentObject(t, repo, cal, "event-in-list",
		"BEGIN:VEVENT\r\nUID:event-in-list\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20301105T090000Z\r\nSUMMARY:needs action too\r\nEND:VEVENT\r\n")

	for _, tc := range []struct {
		status string
		want   []string
	}{
		{calendar.TaskFilterOpen, []string{"needs action", "in process", "unknown status", "empty status column", "null status column"}},
		{calendar.TaskFilterCompleted, []string{"completed", "completed by COMPLETED", "cancelled"}},
		{calendar.TaskFilterAll, []string{"needs action", "in process", "completed", "completed by COMPLETED", "cancelled",
			"unknown status", "empty status column", "null status column"}},
	} {
		t.Run(tc.status, func(t *testing.T) {
			got, total := listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{cal.ID}, Status: tc.status, Limit: 100})
			assert.Equal(t, tc.want, summaries(got))
			assert.Equal(t, int64(len(tc.want)), total)
		})
	}

	for _, bad := range []string{"", "OPEN", "done", "cancelled"} {
		_, _, err := repo.ListTasks(context.Background(), calendar.TaskQuery{CalendarIDs: []uint{cal.ID}, Status: bad, Limit: 100})
		assert.Error(t, err, "status %q must be refused, not read as a filter", bad)
	}
}

// TestListTasksScope: only the VTODOs of the given calendars, never a
// soft-deleted one, never an event; an empty CalendarIDs matches nothing.
func TestListTasksScope(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	listA := seedCalendar(t, repo, false)
	listB := seedCalendar(t, repo, false)
	other := seedCalendar(t, repo, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	storeTodo(t, repo, listA, "in A", t0)
	storeTodo(t, repo, listB, "in B", t0.Add(time.Minute))
	storeTodo(t, repo, other, "in another calendar", t0.Add(2*time.Minute))
	deleted := storeTodo(t, repo, listA, "deleted", t0.Add(3*time.Minute))
	require.NoError(t, repo.DeleteCalendarObject(ctx, deleted))
	componentObject(t, repo, listA, "event-a",
		"BEGIN:VEVENT\r\nUID:event-a\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20301105T090000Z\r\nSUMMARY:an event\r\nEND:VEVENT\r\n")

	got, total := listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{listA.ID}, Status: calendar.TaskFilterAll, Limit: 100})
	assert.Equal(t, []string{"in A"}, summaries(got), "no event, no soft-deleted task")
	assert.Equal(t, int64(1), total)

	got, total = listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{listA.ID, listB.ID}, Status: calendar.TaskFilterAll, Limit: 100})
	assert.Equal(t, []string{"in A", "in B"}, summaries(got), "several calendars in one query")
	assert.Equal(t, int64(2), total)

	for _, ids := range [][]uint{nil, {}} {
		got, total = listTasks(t, repo, calendar.TaskQuery{CalendarIDs: ids, Status: calendar.TaskFilterAll, Limit: 100})
		assert.Empty(t, got, "an empty CalendarIDs is NOT all calendars")
		assert.Zero(t, total)
	}
}

// TestListTasksText: q matches summary or description, case-insensitively,
// with LIKE wildcards taken literally.
func TestListTasksText(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	cal := seedCalendar(t, repo, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	storeTodo(t, repo, cal, "Renew Passport", t0)
	storeTodo(t, repo, cal, "Photos", t0.Add(time.Minute), "DESCRIPTION:for the passport office")
	storeTodo(t, repo, cal, "Save 100% of it", t0.Add(2*time.Minute))
	storeTodo(t, repo, cal, "Save 1000 of it", t0.Add(3*time.Minute))
	storeTodo(t, repo, cal, "file_name", t0.Add(4*time.Minute))
	storeTodo(t, repo, cal, "filename", t0.Add(5*time.Minute))

	for _, tc := range []struct {
		text string
		want []string
	}{
		{"passport", []string{"Renew Passport", "Photos"}},
		{"PASSPORT", []string{"Renew Passport", "Photos"}},
		{"100%", []string{"Save 100% of it"}},
		{"%", []string{"Save 100% of it"}},
		{"file_name", []string{"file_name"}},
		{"_", []string{"file_name"}},
		{"nothing like this", []string{}},
	} {
		got, total := listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{cal.ID}, Status: calendar.TaskFilterAll, Text: tc.text, Limit: 100})
		assert.Equal(t, tc.want, summaries(got), "q=%q", tc.text)
		assert.Equal(t, int64(len(tc.want)), total, "q=%q", tc.text)
	}
}

// TestListTasksDueBounds: DueAfter/DueBefore bound due_at to [after, before),
// normalised to UTC first. SQLite compares these TEXT columns
// lexicographically, so a bound with a +05:00 offset would otherwise be read
// five hours off. Undated tasks never match a bound.
func TestListTasksDueBounds(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	cal := seedCalendar(t, repo, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	storeTodo(t, repo, cal, "08:00Z", t0, "DUE:20300310T080000Z")
	storeTodo(t, repo, cal, "10:00Z", t0.Add(time.Minute), "DUE:20300310T100000Z")
	storeTodo(t, repo, cal, "12:00Z", t0.Add(2*time.Minute), "DUE:20300310T120000Z")
	storeTodo(t, repo, cal, "undated", t0.Add(3*time.Minute))

	plus5 := time.FixedZone("+05:00", 5*3600)
	at := func(h int) *time.Time {
		v := time.Date(2030, 3, 10, h+5, 0, 0, 0, plus5) // h o'clock UTC, written as +05:00
		return &v
	}
	for _, tc := range []struct {
		name          string
		after, before *time.Time
		want          []string
	}{
		{"before is exclusive", nil, at(10), []string{"08:00Z"}},
		{"after is inclusive", at(10), nil, []string{"10:00Z", "12:00Z"}},
		{"both", at(9), at(12), []string{"10:00Z"}},
		{"empty range", at(11), at(11), []string{}},
		{"no bound keeps undated tasks", nil, nil, []string{"08:00Z", "10:00Z", "12:00Z", "undated"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, total := listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{cal.ID}, Status: calendar.TaskFilterAll,
				DueAfter: tc.after, DueBefore: tc.before, Limit: 100})
			assert.Equal(t, tc.want, summaries(got))
			assert.Equal(t, int64(len(tc.want)), total)
		})
	}
}

// TestListTasksPaging: the total is right on every page, whether the page
// proves it (short page) or a COUNT has to (full page, offset past the end),
// and Limit <= 0 returns no rows but still the total.
func TestListTasksPaging(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	cal := seedCalendar(t, repo, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 7 {
		storeTodo(t, repo, cal, fmt.Sprintf("task %d", i), t0.Add(time.Duration(i)*time.Minute))
	}
	all := calendar.TaskQuery{CalendarIDs: []uint{cal.ID}, Status: calendar.TaskFilterOpen}

	for _, tc := range []struct {
		limit, offset int
		want          []string
	}{
		{3, 0, []string{"task 0", "task 1", "task 2"}},
		{3, 3, []string{"task 3", "task 4", "task 5"}},
		{3, 6, []string{"task 6"}},
		{7, 0, []string{"task 0", "task 1", "task 2", "task 3", "task 4", "task 5", "task 6"}},
		{3, 7, []string{}},
		{3, 50, []string{}},
		{100, -4, []string{"task 0", "task 1", "task 2", "task 3", "task 4", "task 5", "task 6"}},
		{0, 0, []string{}},
		{-1, 2, []string{}},
	} {
		q := all
		q.Limit, q.Offset = tc.limit, tc.offset
		got, total := listTasks(t, repo, q)
		assert.Equal(t, tc.want, summaries(got), "limit %d offset %d", tc.limit, tc.offset)
		assert.Equal(t, int64(7), total, "limit %d offset %d", tc.limit, tc.offset)
	}

	// An empty list is total 0 on the first page and past it.
	empty := seedCalendar(t, repo, false)
	for _, offset := range []int{0, 5} {
		got, total := listTasks(t, repo, calendar.TaskQuery{CalendarIDs: []uint{empty.ID}, Status: calendar.TaskFilterOpen, Limit: 10, Offset: offset})
		assert.Empty(t, got)
		assert.Zero(t, total)
	}
}
