package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// summaryAsOf is the as_of of most summary tests: 15:00 on 2030-11-05 at
// +01:00, i.e. 14:00Z; the user's day is [2030-11-04T23:00Z,
// 2030-11-05T23:00Z).
const summaryAsOf = "2030-11-05T15:00:00+01:00"

// getSummary sends GET /api/v1/tasks/summary<rawQuery> as is (nothing is
// encoded, so a test can send an unencoded "+").
func (f *taskFixture) getSummary(t *testing.T, token, rawQuery string) (int, string) {
	t.Helper()
	return serveGet(t, f.app, "/api/v1/tasks/summary"+rawQuery, token)
}

// summary fetches the counts at asOf, sent properly encoded (as the SPA's
// query object does), and decodes them.
func (f *taskFixture) summary(t *testing.T, token, asOf string) dto.TaskSummaryResponse {
	t.Helper()
	code, body := f.getSummary(t, token, "?"+url.Values{"as_of": {asOf}}.Encode())
	require.Equal(t, fiber.StatusOK, code, body)
	var out dto.TaskSummaryResponse
	require.NoError(t, json.Unmarshal([]byte(body), &out), body)
	return out
}

func (r countingRepo) CountTasks(ctx context.Context, calendarIDs []uint, asOf time.Time) (calendar.TaskCounts, error) {
	r.calls["CountTasks"]++
	return r.CalendarRepository.CountTasks(ctx, calendarIDs, asOf)
}

// TestTaskHandlerSummaryRequiresAsOf: as_of is required (400 when missing or
// empty) and must be RFC 3339 with an offset (400 otherwise, an unencoded
// "+" included: it arrives as a space). The 400s come before anything is
// read, for any caller.
func TestTaskHandlerSummaryRequiresAsOf(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "a", "own list", "DUE:20301101T090000Z")

	required := errorBody("Query parameter 'as_of' is required")
	for _, query := range []string{"", "?as_of=", "?as_of", "?asof=" + url.QueryEscape(summaryAsOf), "?AS_OF=" + url.QueryEscape(summaryAsOf)} {
		for _, token := range []string{f.ownerToken, f.outsiderToken} {
			code, body := f.getSummary(t, token, query)
			assert.Equal(t, fiber.StatusBadRequest, code, query)
			assert.JSONEq(t, required, body, query)
		}
	}

	invalid := errorBody("Invalid 'as_of' value")
	for _, raw := range []string{
		"?as_of=2030-11-05T15:00:00+01:00", // unencoded "+": a space
		"?as_of=2030-11-05T15:00:00",       // no offset
		"?as_of=2030-11-05",                // a date alone
		"?as_of=tomorrow",
		"?as_of=1920000000",
		"?as_of=2030-11-05T15:00:00%2B0100", // offset without a colon
		"?as_of=2030-11-05%2015:00:00Z",     // no T
		"?as_of=2030-13-05T15:00:00Z",
		"?as_of=%20",
		"?as_of=%20" + url.QueryEscape(summaryAsOf),
	} {
		code, body := f.getSummary(t, f.ownerToken, raw)
		assert.Equal(t, fiber.StatusBadRequest, code, raw)
		assert.JSONEq(t, invalid, body, raw)
	}

	for _, asOf := range []string{summaryAsOf, "2030-11-05T14:00:00Z", "2030-11-05T06:00:00-08:00", "2030-11-05T15:00:00.123456+01:00"} {
		code, body := f.getSummary(t, f.ownerToken, "?"+url.Values{"as_of": {asOf}}.Encode())
		assert.Equal(t, fiber.StatusOK, code, asOf+": "+body)
	}
}

// TestTaskHandlerSummaryAuth: without a valid token the route is the
// middleware's 401, and a handler reached without the middleware answers 401
// too instead of panicking on the missing user.
func TestTaskHandlerSummaryAuth(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "a", "own list")
	query := "?as_of=" + url.QueryEscape(summaryAsOf)

	code, body := f.getSummary(t, "", query)
	assert.Equal(t, fiber.StatusUnauthorized, code)
	assert.JSONEq(t, `{"status":"error","message":"missing authentication token"}`, body)
	code, _ = f.getSummary(t, "not.a.jwt", query)
	assert.Equal(t, fiber.StatusUnauthorized, code)

	handler := NewTaskHandler(nil, nil, nil, nil, nil, nil, nil, nil, taskuc.NewTaskSummaryUseCase(f.repo), f.repo)
	app := fiber.New()
	app.Get("/api/v1/tasks/summary", handler.Summary) // no Authenticate
	code, body = serveGet(t, app, "/api/v1/tasks/summary"+query, f.ownerToken)
	assert.Equal(t, fiber.StatusUnauthorized, code)
	assert.JSONEq(t, `{"status":"error","message":"missing authentication token"}`, body)
}

// TestTaskHandlerSummaryCounts: the counts cover every task list the caller
// can read — own lists, a subscribed calendar, lists shared read-only and
// read-write, a legacy "" calendar — and nothing else (a stray task in an
// events-only calendar, an event, another user's list). by_calendar is keyed
// by calendar UUID and has an entry for every readable task list, zeros
// included. open and completed equal the totals of the status-filtered lists.
func TestTaskHandlerSummaryCounts(t *testing.T) {
	f := setupTaskHandlerTest(t)
	legacy := f.mkTaskCalendar(t, f.owner, "11111111-0000-4000-8000-000000000008", "Legacy", "#abcdef", "")

	// The owner's list, at 15:00 on the 5th (+01:00).
	f.todo(t, f.taskList, "a1", "timed, this morning", "DUE:20301105T090000Z")      // overdue, today
	f.todo(t, f.taskList, "a2", "timed, tonight", "DUE:20301105T200000Z")           // today
	f.todo(t, f.taskList, "a3", "all-day today", "DUE;VALUE=DATE:20301105")         // today, NOT overdue
	f.todo(t, f.taskList, "a4", "all-day yesterday", "DUE;VALUE=DATE:20301104")     // overdue
	f.todo(t, f.taskList, "a5", "undated")                                          // open only
	f.todo(t, f.taskList, "a6", "done", "STATUS:COMPLETED", "DUE:20301101T090000Z") // completed
	f.todo(t, f.taskList, "a7", "cancelled, overdue and due today", "STATUS:CANCELLED", "DUE:20301105T090000Z")
	f.todo(t, f.otherList, "b1", "in process, overdue", "STATUS:IN-PROCESS", "DUE:20301101T090000Z")
	f.todo(t, f.eventCal, "c1", "stray in an events calendar", "DUE:20301101T090000Z")
	f.todo(t, f.subbed, "d1", "subscribed, undated")
	f.todo(t, f.sharedRead, "e1", "shared read, overdue all-day", "DUE;VALUE=DATE:20301101")
	f.todo(t, f.sharedRW, "f1", "shared read-write, done", "STATUS:COMPLETED")
	f.event(t, f.sharedRW, "an-event")
	f.todo(t, f.shareeOwnTaskList, "g1", "sharee's own, tonight", "DUE:20301105T200000Z")
	outsiderList := f.mkTaskCalendar(t, f.outsider, "11111111-0000-4000-8000-000000000009", "Mine", "#000000", "VTODO")
	f.todo(t, outsiderList, "h1", "outsider's, overdue", "DUE:20301101T090000Z")

	type counts struct{ open, overdue, completed int }
	entry := func(c counts) string {
		return fmt.Sprintf(`{"open":%d,"overdue":%d,"completed":%d}`, c.open, c.overdue, c.completed)
	}
	for _, tc := range []struct {
		name                              string
		token                             string
		open, overdue, dueToday, complete int
		byCalendar                        map[*calendar.Calendar]counts
	}{
		{"owner", f.ownerToken, 8, 4, 3, 3, map[*calendar.Calendar]counts{
			f.taskList: {5, 2, 2}, f.otherList: {1, 1, 0}, f.subbed: {1, 0, 0}, f.sharedRead: {1, 1, 0}, f.sharedRW: {0, 0, 1}, legacy: {0, 0, 0},
		}},
		{"sharee", f.shareeToken, 2, 1, 1, 1, map[*calendar.Calendar]counts{
			f.sharedRead: {1, 1, 0}, f.sharedRW: {0, 0, 1}, f.shareeOwnTaskList: {1, 0, 0},
		}},
		{"outsider", f.outsiderToken, 1, 1, 0, 0, map[*calendar.Calendar]counts{outsiderList: {1, 1, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			byCalendar := map[string]json.RawMessage{}
			for cal, c := range tc.byCalendar {
				byCalendar[cal.UUID] = json.RawMessage(entry(c))
			}
			wantByCalendar, err := json.Marshal(byCalendar)
			require.NoError(t, err)
			code, body := f.getSummary(t, tc.token, "?as_of="+url.QueryEscape(summaryAsOf))
			require.Equal(t, fiber.StatusOK, code, body)
			assert.JSONEq(t, fmt.Sprintf(`{"open":%d,"overdue":%d,"due_today":%d,"completed":%d,"by_calendar":%s}`,
				tc.open, tc.overdue, tc.dueToday, tc.complete, wantByCalendar), body)

			// The header counts agree with the lists: open and completed
			// (done: completed and cancelled) are the totals of the
			// status-filtered cross-list query, a list's open count the
			// total of its own open list.
			assert.Equal(t, int64(tc.open), f.listAll(t, tc.token, "?status=open").Total)
			assert.Equal(t, int64(tc.complete), f.listAll(t, tc.token, "?status=completed").Total)
			for cal, c := range tc.byCalendar {
				assert.Equal(t, int64(c.open), f.list(t, tc.token, cal, "?status=open").Total, cal.Name)
			}
		})
	}

	// A user who reads no task list: zeros and an empty by_calendar.
	require.NoError(t, f.db.DB().Delete(&calendar.Calendar{}, outsiderList.ID).Error)
	code, body := f.getSummary(t, f.outsiderToken, "?as_of="+url.QueryEscape(summaryAsOf))
	require.Equal(t, fiber.StatusOK, code)
	assert.JSONEq(t, `{"open":0,"overdue":0,"due_today":0,"completed":0,"by_calendar":{}}`, body)
}

// TestTaskHandlerSummaryCancelledIsDone pins the cancelled rule: a cancelled
// task counts as completed (done) and never as open, overdue or due today — whatever its due date — so summary.completed always equals
// the total of ?status=completed.
func TestTaskHandlerSummaryCancelledIsDone(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "a", "overdue and due today", "DUE:20301105T090000Z")
	f.todo(t, f.taskList, "b", "all-day yesterday", "DUE;VALUE=DATE:20301104")
	byCalendar := func(open, overdue, completed int64) map[string]dto.TaskListCountsResponse {
		return map[string]dto.TaskListCountsResponse{
			f.taskList.UUID: {Open: open, Overdue: overdue, Completed: completed}, f.otherList.UUID: {}, f.subbed.UUID: {},
			f.sharedRead.UUID: {}, f.sharedRW.UUID: {},
		}
	}

	assert.Equal(t, dto.TaskSummaryResponse{Open: 2, Overdue: 2, DueToday: 1, Completed: 0, ByCalendar: byCalendar(2, 2, 0)},
		f.summary(t, f.ownerToken, summaryAsOf))

	for _, step := range []struct {
		body string
		want dto.TaskSummaryResponse
	}{
		{`{"status":"cancelled"}`, dto.TaskSummaryResponse{Open: 1, Overdue: 1, DueToday: 0, Completed: 1, ByCalendar: byCalendar(1, 1, 1)}},
		{`{"status":"in-process"}`, dto.TaskSummaryResponse{Open: 2, Overdue: 2, DueToday: 1, Completed: 0, ByCalendar: byCalendar(2, 2, 0)}},
		{`{"status":"completed"}`, dto.TaskSummaryResponse{Open: 1, Overdue: 1, DueToday: 0, Completed: 1, ByCalendar: byCalendar(1, 1, 1)}},
		{`{"status":"needs-action"}`, dto.TaskSummaryResponse{Open: 2, Overdue: 2, DueToday: 1, Completed: 0, ByCalendar: byCalendar(2, 2, 0)}},
	} {
		code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, task), step.body, nil)
		require.Equal(t, fiber.StatusOK, code, body)
		got := f.summary(t, f.ownerToken, summaryAsOf)
		assert.Equal(t, step.want, got, step.body)
		assert.Equal(t, got.Completed, f.listAll(t, f.ownerToken, "?status=completed").Total, step.body)
		assert.Equal(t, got.Open, f.listAll(t, f.ownerToken, "?status=open").Total, step.body)
	}
}

// TestTaskHandlerSummaryAllDayVsTimed: a timed task is overdue once as_of
// passes its due instant; an all-day task only once as_of's DATE, read in
// as_of's own offset, is after its due date — all day long it is due today.
// The same instant sent with two offsets can be two dates.
func TestTaskHandlerSummaryAllDayVsTimed(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "a", "all-day on the 5th", "DUE;VALUE=DATE:20301105")
	f.todo(t, f.taskList, "b", "timed, 09:00 on the 5th at +01:00", "DUE:20301105T080000Z")
	f.todo(t, f.taskList, "c", "timed, 23:00 on the 5th at +01:00", "DUE:20301105T220000Z")

	for _, tc := range []struct {
		asOf              string
		overdue, dueToday int64
	}{
		{"2030-11-05T00:00:01+01:00", 0, 3},
		{"2030-11-05T09:00:00+01:00", 0, 3}, // due exactly now: not yet overdue
		{"2030-11-05T09:00:01+01:00", 1, 3},
		{"2030-11-05T15:00:00+01:00", 1, 3},
		{"2030-11-05T23:59:59+01:00", 2, 3}, // the all-day task is still not overdue
		{"2030-11-06T00:00:00+01:00", 3, 0},
		// 2030-11-06T04:00Z: the user's date is still the 5th at -08:00 …
		{"2030-11-05T20:00:00-08:00", 2, 3},
		// … but already the 6th at UTC and at +01:00.
		{"2030-11-06T04:00:00Z", 3, 0},
		{"2030-11-06T05:00:00+01:00", 3, 0},
		// The 5th at +14:00 is [2030-11-04T10:00Z, 2030-11-05T10:00Z):
		// nothing is late yet, and the 23:00 (+01:00) task falls on the 6th.
		{"2030-11-05T00:30:00+14:00", 0, 2},
	} {
		got := f.summary(t, f.ownerToken, tc.asOf)
		assert.Equal(t, int64(3), got.Open, tc.asOf)
		assert.Equal(t, tc.overdue, got.Overdue, tc.asOf)
		assert.Equal(t, tc.dueToday, got.DueToday, tc.asOf)
		assert.Equal(t, dto.TaskListCountsResponse{Open: 3, Overdue: tc.overdue}, got.ByCalendar[f.taskList.UUID], tc.asOf)
	}
}

// TestTaskHandlerSummaryBeyondListLimit: the counts are taken in the
// database, so they are right when a list page cannot hold every task (a
// page stops at 1000).
func TestTaskHandlerSummaryBeyondListLimit(t *testing.T) {
	f := setupTaskHandlerTest(t)
	const n = 1001
	objs := make([]*calendar.CalendarObject, 0, n)
	for i := range n {
		uid := fmt.Sprintf("bulk-%04d", i)
		obj := &calendar.CalendarObject{
			UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), CalendarID: f.taskList.ID, Path: uid + ".ics", UID: uid,
			ETag: calendar.NewETag(),
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:" + uid +
				"\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:" + uid + "\r\nDUE:20301101T090000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n",
		}
		require.NoError(t, obj.PopulateDenormFieldsFromICal())
		objs = append(objs, obj)
	}
	require.NoError(t, f.db.DB().CreateInBatches(objs, 200).Error)
	f.todo(t, f.sharedRW, "x", "one more, shared", "DUE:20301101T090000Z")

	page := f.listAll(t, f.ownerToken, "?limit=5000")
	assert.Equal(t, 1000, page.Count)
	assert.Equal(t, int64(n+1), page.Total)

	got := f.summary(t, f.ownerToken, summaryAsOf)
	assert.Equal(t, int64(n+1), got.Open)
	assert.Equal(t, int64(n+1), got.Overdue)
	assert.Equal(t, dto.TaskListCountsResponse{Open: n, Overdue: n}, got.ByCalendar[f.taskList.UUID])
	assert.Equal(t, dto.TaskListCountsResponse{Open: 1, Overdue: 1}, got.ByCalendar[f.sharedRW.UUID])
}

// TestTaskHandlerSummaryOneQuery: whatever the number of readable lists and
// tasks, the summary is one ListReadableCalendarIDs and one CountTasks — not
// the calendar list use case, no per-list query, no task listed. On the wire:
// one SELECT of calendars and one of calendar_objects.
func TestTaskHandlerSummaryOneQuery(t *testing.T) {
	f := setupTaskHandlerTest(t)
	for i := range 5 {
		cal := f.mkTaskCalendar(t, f.owner, fmt.Sprintf("22222222-0000-4000-8000-%012d", i), "extra", "#000000", "VTODO")
		f.todo(t, cal, fmt.Sprintf("extra-%d", i), "extra", "DUE:20301101T090000Z")
	}
	f.todo(t, f.taskList, "a", "own list")
	f.todo(t, f.sharedRW, "b", "shared")

	repo := countingRepo{CalendarRepository: f.repo, calls: map[string]int{}}
	handler := NewTaskHandler(nil, nil, nil, nil, nil, nil, nil, nil, taskuc.NewTaskSummaryUseCase(repo), repo)
	app := fiber.New()
	app.Get("/api/v1/tasks/summary", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB())), handler.Summary)

	// Find runs the Query callbacks, Scan (the count) the Row callbacks.
	var tables []string
	record := func(tx *gorm.DB) {
		if !tx.DryRun { // the share subquery is built in dry-run mode
			tables = append(tables, tx.Statement.Table)
		}
	}
	gdb := f.db.DB()
	require.NoError(t, gdb.Callback().Query().After("gorm:query").Register("test:summary_tables", record))
	require.NoError(t, gdb.Callback().Row().After("gorm:row").Register("test:summary_tables", record))
	t.Cleanup(func() {
		_ = gdb.Callback().Query().Remove("test:summary_tables")
		_ = gdb.Callback().Row().Remove("test:summary_tables")
	})

	code, body := serveGet(t, app, "/api/v1/tasks/summary?as_of="+url.QueryEscape(summaryAsOf), f.ownerToken)
	require.Equal(t, fiber.StatusOK, code, body)
	assert.Equal(t, map[string]int{"ListReadableCalendarIDs": 1, "CountTasks": 1}, repo.calls)
	assert.Equal(t, []string{"users", "calendars", "calendar_objects"}, tables)
	var got dto.TaskSummaryResponse
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	assert.Equal(t, int64(7), got.Open)
	assert.Equal(t, int64(5), got.Overdue)
	assert.Len(t, got.ByCalendar, 10)

	// A bad as_of reads nothing.
	clear(repo.calls)
	tables = nil
	code, _ = serveGet(t, app, "/api/v1/tasks/summary?as_of=x", f.ownerToken)
	assert.Equal(t, fiber.StatusBadRequest, code)
	assert.Empty(t, repo.calls)
	assert.Equal(t, []string{"users"}, tables, "only the authentication")
}

// TestTaskHandlerSummaryServerErrors (F28): a storage failure — of the
// calendar lookup or of the count — is a 500 with a generic body, the cause
// in the server log only.
func TestTaskHandlerSummaryServerErrors(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "a", "own list")
	query := "?as_of=" + url.QueryEscape(summaryAsOf)

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	for _, table := range []string{"calendar_objects", "calendar_shares"} {
		t.Run("missing "+table, func(t *testing.T) {
			logBuf.Reset()
			require.NoError(t, f.db.DB().Exec("ALTER TABLE "+table+" RENAME TO "+table+"_gone").Error)
			defer func() {
				require.NoError(t, f.db.DB().Exec("ALTER TABLE "+table+"_gone RENAME TO "+table).Error)
			}()
			code, body := f.getSummary(t, f.ownerToken, query)
			assert.Equal(t, fiber.StatusInternalServerError, code)
			assert.JSONEq(t, internalErrorBody, body)
			assert.NotContains(t, body, table)
			assert.Contains(t, logBuf.String(), "no such table: "+table)
			assert.Contains(t, logBuf.String(), "task request failed")
			assert.Contains(t, logBuf.String(), "path=/api/v1/tasks/summary")
		})
	}

	t.Run("failing lookup", func(t *testing.T) {
		logBuf.Reset()
		handler := NewTaskHandler(nil, nil, nil, nil, nil, nil, nil, nil,
			taskuc.NewTaskSummaryUseCase(failingReadableRepo{CalendarRepository: f.repo, err: errors.New("disk I/O error (injected)")}), f.repo)
		app := fiber.New()
		app.Get("/api/v1/tasks/summary", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB())), handler.Summary)
		code, body := serveGet(t, app, "/api/v1/tasks/summary"+query, f.ownerToken)
		assert.Equal(t, fiber.StatusInternalServerError, code)
		assert.JSONEq(t, internalErrorBody, body)
		assert.Contains(t, logBuf.String(), "disk I/O error (injected)")
	})
}
