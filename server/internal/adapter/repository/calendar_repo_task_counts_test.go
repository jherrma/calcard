package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newTaskCountsDB is the database of the CountTasks tests (SQLite, as every
// repository test; one name, so a PostgreSQL run only swaps this function).
func newTaskCountsDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newFeedDB(t)
}

func countTasks(t *testing.T, repo *repository.CalendarRepository, ids []uint, asOf time.Time) calendar.TaskCounts {
	t.Helper()
	counts, err := repo.CountTasks(context.Background(), ids, asOf)
	require.NoError(t, err)
	return counts
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return v
}

// TestCountTasks pins the counts behind GET /api/v1/tasks/summary (#170 story
// 13) at as_of 2030-11-05T15:00:00+01:00: now is 14:00Z, the local day is
// [2030-11-04T23:00Z, 2030-11-05T23:00Z), the date 2030-11-05. A timed task
// is overdue when due before now (instants), an all-day task when due on an
// earlier date; due today is the local day (timed) or the date (all-day).
// Done is COMPLETED or CANCELLED and only ever counts as completed. Events,
// deleted tasks and calendars not asked for are not counted.
func TestCountTasks(t *testing.T) {
	db := newTaskCountsDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	listA := seedCalendar(t, repo, false)
	listB := seedCalendar(t, repo, false)
	other := seedCalendar(t, repo, false)
	eventsOnly := seedCalendar(t, repo, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// What each task of listA must count as: o(pen), d(ue) o(verdue), t(oday),
	// c(ompleted).
	type want struct{ open, overdue, today, completed bool }
	tasksA := []struct {
		summary string
		want    want
		props   []string
	}{
		{"timed, a second before as_of", want{true, true, true, false}, []string{"DUE:20301105T135959Z"}},
		{"timed, exactly as_of", want{true, false, true, false}, []string{"DUE:20301105T140000Z"}},
		{"timed, last second of the local day", want{true, false, true, false}, []string{"DUE:20301105T225959Z"}},
		{"timed, first second of the next local day", want{true, false, false, false}, []string{"DUE:20301105T230000Z"}},
		{"timed, first second of the local day (UTC yesterday)", want{true, true, true, false}, []string{"DUE:20301104T230000Z"}},
		{"timed, last second of the local yesterday", want{true, true, false, false}, []string{"DUE:20301104T225959Z"}},
		{"all-day yesterday", want{true, true, false, false}, []string{"DUE;VALUE=DATE:20301104"}},
		// Its stored due_at (midnight UTC) lies before as_of: an all-day
		// task compares dates, so it is due today and NOT overdue.
		{"all-day today", want{true, false, true, false}, []string{"DUE;VALUE=DATE:20301105"}},
		{"all-day tomorrow", want{true, false, false, false}, []string{"DUE;VALUE=DATE:20301106"}},
		{"TZID due this morning", want{true, true, true, false}, []string{"DUE;TZID=Europe/Berlin:20301105T080000"}},
		// Floating: stored as UTC wall time 14:30Z, after as_of (14:00Z).
		// Compared without UTC-normalising as_of, SQLite's TEXT order would
		// put "…14:30:00+00:00" before "…15:00:00+01:00": overdue.
		{"floating due 14:30, read as UTC", want{true, false, true, false}, []string{"DUE:20301105T143000"}},
		{"undated", want{true, false, false, false}, nil},
		{"in-process, overdue", want{true, true, false, false}, []string{"STATUS:IN-PROCESS", "DUE:20301101T090000Z"}},
		{"completed, overdue", want{false, false, false, true}, []string{"STATUS:COMPLETED", "DUE:20301101T090000Z"}},
		{"cancelled, overdue and due today", want{false, false, false, true}, []string{"STATUS:CANCELLED", "DUE:20301105T090000Z"}},
		{"cancelled, all-day yesterday", want{false, false, false, true}, []string{"STATUS:CANCELLED", "DUE;VALUE=DATE:20301104"}},
		{"completed by its COMPLETED property", want{false, false, false, true}, []string{"COMPLETED:20301102T100000Z", "DUE:20301101T090000Z"}},
		{"unknown status reads needs-action", want{true, true, false, false}, []string{"STATUS:WAITING", "DUE:20301101T090000Z"}},
		{"due from DTSTART+DURATION this morning", want{true, true, true, false}, []string{"DTSTART:20301105T100000Z", "DURATION:PT1H"}},
		{"all-day start, no due", want{true, false, false, false}, []string{"DTSTART;VALUE=DATE:20301101"}},
		{"all-day start + P1D: due today", want{true, false, true, false}, []string{"DTSTART;VALUE=DATE:20301104", "DURATION:P1D"}},
		{"needs-action, due in 2040", want{true, false, false, false}, []string{"STATUS:NEEDS-ACTION", "DUE:20401231T000000Z"}},
	}
	var wantA calendar.TaskCount
	for i, tk := range tasksA {
		storeTodo(t, repo, listA, tk.summary, t0.Add(time.Duration(i)*time.Minute), tk.props...)
		for _, c := range []struct {
			on bool
			n  *int64
		}{{tk.want.open, &wantA.Open}, {tk.want.overdue, &wantA.Overdue}, {tk.want.today, &wantA.DueToday}, {tk.want.completed, &wantA.Completed}} {
			if c.on {
				*c.n++
			}
		}
	}
	require.Equal(t, calendar.TaskCount{Open: 18, Overdue: 8, DueToday: 9, Completed: 4}, wantA, "fixture arithmetic")

	deleted := storeTodo(t, repo, listA, "deleted, overdue", t0, "DUE:20301101T090000Z")
	require.NoError(t, repo.DeleteCalendarObject(ctx, deleted))
	componentObject(t, repo, listA, "event-a",
		"BEGIN:VEVENT\r\nUID:event-a\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20301101T090000Z\r\nSUMMARY:an event\r\nEND:VEVENT\r\n")

	storeTodo(t, repo, listB, "B overdue", t0, "DUE:20301101T090000Z")
	storeTodo(t, repo, listB, "B completed", t0, "STATUS:COMPLETED")
	storeTodo(t, repo, listB, "B cancelled", t0, "STATUS:CANCELLED")
	// Columns no write path leaves behind, but a row the backfill could not
	// decode has: a NULL or "" status is open (as in ListTasks), a NULL
	// is_all_day is a timed task.
	nullStatus := storeTodo(t, repo, listB, "B status NULL, overdue", t0, "DUE:20301101T090000Z")
	require.NoError(t, db.Model(&calendar.CalendarObject{}).Where("id = ?", nullStatus.ID).UpdateColumn("status", gorm.Expr("NULL")).Error)
	emptyStatus := storeTodo(t, repo, listB, "B status empty, undated", t0)
	require.NoError(t, db.Model(&calendar.CalendarObject{}).Where("id = ?", emptyStatus.ID).UpdateColumn("status", "").Error)
	nullAllDay := storeTodo(t, repo, listB, "B is_all_day NULL, due at noon", t0, "DUE:20301105T120000Z")
	require.NoError(t, db.Model(&calendar.CalendarObject{}).Where("id = ?", nullAllDay.ID).UpdateColumn("is_all_day", gorm.Expr("NULL")).Error)
	wantB := calendar.TaskCount{Open: 4, Overdue: 3, DueToday: 1, Completed: 2}

	storeTodo(t, repo, other, "not asked for", t0, "DUE:20301101T090000Z")
	componentObject(t, repo, eventsOnly, "event-e",
		"BEGIN:VEVENT\r\nUID:event-e\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20301101T090000Z\r\nSUMMARY:an event\r\nEND:VEVENT\r\n")

	asOf := mustParse(t, "2030-11-05T15:00:00+01:00")
	counts := countTasks(t, repo, []uint{listA.ID, listB.ID, eventsOnly.ID}, asOf)
	assert.Equal(t, map[uint]calendar.TaskCount{listA.ID: wantA, listB.ID: wantB}, counts.ByCalendar,
		"one entry per calendar holding tasks; none for the events-only one or the one not asked for")
	assert.Equal(t, calendar.TaskCount{Open: 22, Overdue: 11, DueToday: 10, Completed: 6}, counts.Total)

	// One calendar at a time gives the same per-calendar numbers.
	for _, tc := range []struct {
		cal  *calendar.Calendar
		want calendar.TaskCount
	}{{listA, wantA}, {listB, wantB}} {
		one := countTasks(t, repo, []uint{tc.cal.ID}, asOf)
		assert.Equal(t, map[uint]calendar.TaskCount{tc.cal.ID: tc.want}, one.ByCalendar)
		assert.Equal(t, tc.want, one.Total)
	}

	t.Run("each task alone", func(t *testing.T) {
		// Every listA task in a calendar of its own, so a wrong count names
		// the task instead of a total.
		for _, tk := range tasksA {
			cal := seedCalendar(t, repo, false)
			storeTodo(t, repo, cal, tk.summary, t0, tk.props...)
			var w calendar.TaskCount
			if tk.want.open {
				w.Open = 1
			}
			if tk.want.overdue {
				w.Overdue = 1
			}
			if tk.want.today {
				w.DueToday = 1
			}
			if tk.want.completed {
				w.Completed = 1
			}
			assert.Equal(t, w, countTasks(t, repo, []uint{cal.ID}, asOf).Total, tk.summary)
		}
	})

	t.Run("the offset of as_of decides the date", func(t *testing.T) {
		// 2030-11-05T20:00:00-08:00 is 2030-11-06T04:00Z: UTC's date is the
		// 6th, the user's is still the 5th. The local day is
		// [2030-11-05T08:00Z, 2030-11-06T08:00Z).
		west := countTasks(t, repo, []uint{listA.ID}, mustParse(t, "2030-11-05T20:00:00-08:00"))
		assert.Equal(t, calendar.TaskCount{Open: 18, Overdue: 12, DueToday: 8, Completed: 4}, west.Total)
		// The same instant at +01:00 is the 6th: every all-day task of the
		// 5th is overdue now, and due today are only the all-day task of the
		// 6th and the timed one due 23:00Z on the 5th (00:00 on the 6th
		// local, also overdue).
		east := countTasks(t, repo, []uint{listA.ID}, mustParse(t, "2030-11-06T05:00:00+01:00"))
		assert.Equal(t, calendar.TaskCount{Open: 18, Overdue: 14, DueToday: 2, Completed: 4}, east.Total)
	})

	t.Run("nothing asked for counts nothing", func(t *testing.T) {
		// Find runs the Query callbacks, Scan the Row callbacks: count both.
		var statements int
		count := func(tx *gorm.DB) {
			if !tx.DryRun {
				statements++
			}
		}
		require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:count_tasks_statements", count))
		require.NoError(t, db.Callback().Row().After("gorm:row").Register("test:count_tasks_statements", count))
		t.Cleanup(func() {
			_ = db.Callback().Query().Remove("test:count_tasks_statements")
			_ = db.Callback().Row().Remove("test:count_tasks_statements")
		})

		for _, ids := range [][]uint{nil, {}} {
			counts := countTasks(t, repo, ids, asOf)
			assert.Equal(t, calendar.TaskCount{}, counts.Total, "an empty calendarIDs is NOT all calendars")
			assert.NotNil(t, counts.ByCalendar)
			assert.Empty(t, counts.ByCalendar)
		}
		assert.Zero(t, statements, "no query without calendars")

		counts := countTasks(t, repo, []uint{listA.ID, listB.ID, other.ID}, asOf)
		assert.Len(t, counts.ByCalendar, 3)
		assert.Equal(t, 1, statements, "one query whatever the number of calendars")
	})
}

// TestCountTasksFailure: a storage error is returned, with no counts.
func TestCountTasksFailure(t *testing.T) {
	db := newTaskCountsDB(t)
	repo := repository.NewCalendarRepository(db)
	cal := seedCalendar(t, repo, false)
	storeTodo(t, repo, cal, "a task", time.Time{})
	require.NoError(t, db.Exec("ALTER TABLE calendar_objects RENAME TO calendar_objects_gone").Error)

	counts, err := repo.CountTasks(context.Background(), []uint{cal.ID}, time.Now())
	require.Error(t, err)
	assert.Equal(t, calendar.TaskCounts{}, counts)
}
