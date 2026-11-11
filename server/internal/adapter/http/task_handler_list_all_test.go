package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const emptyTaskPage = `{"tasks":[],"count":0,"total":0}`

// getAll sends GET /api/v1/tasks<query> and returns status and body.
func (f *taskFixture) getAll(t *testing.T, token, query string) (int, string) {
	t.Helper()
	return serveGet(t, f.app, "/api/v1/tasks"+query, token)
}

// listAll fetches a cross-list page that must succeed and decodes it.
func (f *taskFixture) listAll(t *testing.T, token, query string) dto.TaskWithCalendarListResponse {
	t.Helper()
	code, body := f.getAll(t, token, query)
	require.Equal(t, fiber.StatusOK, code, body)
	var out dto.TaskWithCalendarListResponse
	require.NoError(t, json.Unmarshal([]byte(body), &out), body)
	return out
}

func serveGet(t *testing.T, app *fiber.App, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

func allSummaries(page dto.TaskWithCalendarListResponse) []string {
	out := make([]string, 0, len(page.Tasks))
	for _, tk := range page.Tasks {
		out = append(out, tk.Summary)
	}
	return out
}

// mkTaskCalendar stores one more calendar for owner.
func (f *taskFixture) mkTaskCalendar(t *testing.T, owner *user.User, uuid, name, color, components string) *calendar.Calendar {
	t.Helper()
	cal := &calendar.Calendar{UUID: uuid, UserID: owner.ID, Name: name, Path: uuid + ".ics", Color: color,
		Timezone: "UTC", SupportedComponents: components}
	require.NoError(t, f.repo.Create(context.Background(), cal))
	return cal
}

// TestTaskHandlerListAllScope: GET /tasks lists the tasks of every calendar
// the caller can read that stores tasks — own task lists, a mixed and a
// legacy "" calendar, a subscribed calendar, lists shared read-only and
// read-write — and nothing else: no task of a calendar they cannot read, no
// stray task in a VEVENT-only calendar, no event. Each item is the task's
// TaskResponse (equal to its per-list GET) plus the name and colour of its
// own list.
func TestTaskHandlerListAllScope(t *testing.T) {
	f := setupTaskHandlerTest(t)
	require.NoError(t, f.db.DB().Model(&calendar.Calendar{}).Where("id = ?", f.sharedRead.ID).
		Updates(map[string]any{"name": "Team", "color": "#00aa00"}).Error)
	f.sharedRead.Name, f.sharedRead.Color = "Team", "#00aa00"
	legacy := f.mkTaskCalendar(t, f.owner, "11111111-0000-4000-8000-000000000008", "Legacy", "#abcdef", "")

	// The outsider reads nothing: an empty page, not an error.
	code, body := f.getAll(t, f.outsiderToken, "")
	require.Equal(t, fiber.StatusOK, code)
	assert.JSONEq(t, emptyTaskPage, body)

	f.todo(t, f.taskList, "a", "own list", "DUE:20301101T090000Z")
	f.todo(t, f.otherList, "b", "own other list", "DUE:20301102T090000Z")
	f.todo(t, f.eventCal, "c", "stray in an events calendar", "DUE:20301103T090000Z")
	f.todo(t, f.subbed, "d", "subscribed", "DUE:20301104T090000Z")
	f.todo(t, f.sharedRead, "e", "shared read", "DUE:20301105T090000Z")
	f.todo(t, f.sharedRW, "f", "shared read-write", "DUE:20301106T090000Z")
	f.event(t, f.sharedRW, "an-event")
	f.todo(t, f.shareeOwnTaskList, "g", "sharee's own", "DUE:20301107T090000Z")
	f.todo(t, legacy, "h", "legacy", "DUE;VALUE=DATE:20301108")
	outsiderList := f.mkTaskCalendar(t, f.outsider, "11111111-0000-4000-8000-000000000009", "Mine", "#000000", "VTODO")
	f.todo(t, outsiderList, "i", "outsider's", "DUE:20301109T090000Z")

	for _, tc := range []struct {
		name  string
		token string
		want  []string
	}{
		{"owner", f.ownerToken, []string{"own list", "own other list", "subscribed", "shared read", "shared read-write", "legacy"}},
		{"sharee", f.shareeToken, []string{"shared read", "shared read-write", "sharee's own"}},
		{"outsider", f.outsiderToken, []string{"outsider's"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := f.listAll(t, tc.token, "")
			assert.Equal(t, tc.want, allSummaries(page))
			assert.Equal(t, len(tc.want), page.Count)
			assert.Equal(t, int64(len(tc.want)), page.Total)

			// Every item, as raw JSON: its per-list GET plus the two
			// calendar fields of the calendar it lives in.
			code, body := f.getAll(t, tc.token, "")
			require.Equal(t, fiber.StatusOK, code)
			var raw struct {
				Tasks []map[string]any `json:"tasks"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &raw))
			require.Len(t, raw.Tasks, len(tc.want))
			for _, item := range raw.Tasks {
				cal, err := f.repo.GetByUUID(context.Background(), item["calendar_uuid"].(string))
				require.NoError(t, err)
				code, one := f.get(t, tc.token, cal.UUID+"/tasks/"+item["id"].(string))
				require.Equal(t, fiber.StatusOK, code, one)
				want := decodeJSON(t, one)
				want["calendar_name"] = cal.Name
				want["calendar_color"] = cal.Color
				assert.Equal(t, want, item, item["summary"])
			}
		})
	}

	page := f.listAll(t, f.ownerToken, "")
	assert.Equal(t, "Team", page.Tasks[3].CalendarName)
	assert.Equal(t, "#00aa00", page.Tasks[3].CalendarColor)
	assert.Equal(t, f.sharedRead.UUID, page.Tasks[3].CalendarUUID)
	assert.Equal(t, f.sharedRead.ID, page.Tasks[3].CalendarID)
	assert.Equal(t, "Legacy", page.Tasks[5].CalendarName)
	assert.Equal(t, "#abcdef", page.Tasks[5].CalendarColor)
	assert.Equal(t, "2030-11-08", *page.Tasks[5].DueDate)
}

// TestTaskHandlerListAllCalendarUUID: calendar_uuid narrows to one readable
// list. A calendar the caller cannot read, or that does not exist, is the 404
// "Calendar not found" — never an empty 200 — whatever else the query says;
// a readable calendar that does not store tasks answers an empty page, like
// its own list.
func TestTaskHandlerListAllCalendarUUID(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "a", "own list")
	f.todo(t, f.otherList, "b", "own other list")
	f.todo(t, f.eventCal, "c", "stray in an events calendar")
	f.todo(t, f.subbed, "d", "subscribed")
	f.todo(t, f.sharedRead, "e", "shared read")
	f.todo(t, f.sharedRW, "f", "shared read-write")
	f.event(t, f.sharedRW, "an-event")
	f.todo(t, f.shareeOwnTaskList, "g", "sharee's own")
	lettered := f.mkTaskCalendar(t, f.owner, "abcdef00-0000-4000-8000-00000000abcd", "Lettered", "#000000", "VTODO")
	f.todo(t, lettered, "h", "lettered")

	for _, tc := range []struct {
		name  string
		token string
		cal   *calendar.Calendar
		want  []string
	}{
		{"owner, lettered list", f.ownerToken, lettered, []string{"lettered"}},
		{"owner, own list", f.ownerToken, f.taskList, []string{"own list"}},
		{"owner, subscribed", f.ownerToken, f.subbed, []string{"subscribed"}},
		{"owner, own mixed calendar", f.ownerToken, f.sharedRW, []string{"shared read-write"}},
		{"sharee, read share", f.shareeToken, f.sharedRead, []string{"shared read"}},
		{"sharee, read-write share", f.shareeToken, f.sharedRW, []string{"shared read-write"}},
		{"sharee, own list", f.shareeToken, f.shareeOwnTaskList, []string{"sharee's own"}},
	} {
		page := f.listAll(t, tc.token, "?calendar_uuid="+tc.cal.UUID)
		assert.Equal(t, tc.want, allSummaries(page), tc.name)
		assert.Equal(t, int64(len(tc.want)), page.Total, tc.name)
		for _, item := range page.Tasks {
			assert.Equal(t, tc.cal.UUID, item.CalendarUUID, tc.name)
		}
	}

	// Readable, but not a task list: no tasks (the stray VTODO stays
	// hidden, as in GET /calendars/:id/tasks).
	code, body := f.getAll(t, f.ownerToken, "?calendar_uuid="+f.eventCal.UUID)
	require.Equal(t, fiber.StatusOK, code)
	assert.JSONEq(t, emptyTaskPage, body)

	// An empty value is no filter.
	assert.Len(t, f.listAll(t, f.ownerToken, "?calendar_uuid=").Tasks, 6)

	for _, tc := range []struct{ name, token, uuid, extra string }{
		{"sharee, unshared list", f.shareeToken, f.taskList.UUID, ""},
		{"sharee, unshared events calendar", f.shareeToken, f.eventCal.UUID, ""},
		{"owner, the sharee's list", f.ownerToken, f.shareeOwnTaskList.UUID, ""},
		{"outsider, a list", f.outsiderToken, f.taskList.UUID, ""},
		{"outsider, a shared list", f.outsiderToken, f.sharedRead.UUID, ""},
		{"unknown calendar", f.ownerToken, "00000000-0000-4000-8000-00000000dead", ""},
		{"all-zero UUID", f.ownerToken, "00000000-0000-0000-0000-000000000000", ""},
		{"not a UUID", f.ownerToken, "not-a-uuid", ""},
		{"upper-case spelling", f.ownerToken, strings.ToUpper(lettered.UUID), ""},
		{"with status all", f.outsiderToken, f.taskList.UUID, "&status=all"},
		{"with a text query and a bound", f.shareeToken, f.taskList.UUID, "&q=own&due_after=2030-01-01T00:00:00Z"},
		{"with paging", f.outsiderToken, f.taskList.UUID, "&limit=5&offset=10"},
	} {
		code, body := f.getAll(t, tc.token, "?calendar_uuid="+tc.uuid+tc.extra)
		assert.Equal(t, fiber.StatusNotFound, code, tc.name)
		assert.JSONEq(t, calendarNotFoundBody, body, tc.name)
	}
}

// TestTaskHandlerListAllStatusAndText: status and q behave as on the per-list
// route, across lists. The parameters are checked before the calendar_uuid
// filter, so a bad one is the 400 even for a calendar the caller cannot read.
func TestTaskHandlerListAllStatusAndText(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "1", "Open in list", "DUE:20301101T090000Z")
	f.todo(t, f.sharedRW, "2", "In process in shared", "STATUS:IN-PROCESS", "DUE:20301102T090000Z")
	f.todo(t, f.taskList, "3", "Completed in list", "STATUS:COMPLETED", "DUE:20301103T090000Z")
	f.todo(t, f.sharedRW, "4", "Cancelled in shared", "STATUS:CANCELLED", "DUE:20301104T090000Z")
	f.todo(t, f.subbed, "5", "100% literal in feed", "DUE:20301105T090000Z", "DESCRIPTION:Buy MILK")
	f.todo(t, f.sharedRead, "6", "Completed by property", "COMPLETED:20301101T120000Z", "DUE:20301106T090000Z")

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", []string{"Open in list", "In process in shared", "100% literal in feed"}},
		{"?status=open", []string{"Open in list", "In process in shared", "100% literal in feed"}},
		{"?status=completed", []string{"Completed in list", "Cancelled in shared", "Completed by property"}},
		{"?status=all", []string{"Open in list", "In process in shared", "Completed in list", "Cancelled in shared", "100% literal in feed", "Completed by property"}},
		{"?status=all&q=IN%20", []string{"Open in list", "In process in shared", "Completed in list", "Cancelled in shared", "100% literal in feed"}},
		{"?status=all&q=shared", []string{"In process in shared", "Cancelled in shared"}},
		{"?q=milk", []string{"100% literal in feed"}},
		{"?status=all&q=100%25", []string{"100% literal in feed"}},
		{"?status=all&q=%25", []string{"100% literal in feed"}},
		{"?q=%20%20list%20", []string{"Open in list"}},
		{"?q=nothing", []string{}},
		{"?status=completed&calendar_uuid=" + f.taskList.UUID, []string{"Completed in list"}},
	} {
		page := f.listAll(t, f.ownerToken, tc.query)
		assert.Equal(t, tc.want, allSummaries(page), tc.query)
		assert.Equal(t, int64(len(tc.want)), page.Total, tc.query)
	}

	for _, query := range []string{
		"?status=bogus", "?status=OPEN", "?status=done", "?status=cancelled",
		"?status=bogus&calendar_uuid=00000000-0000-4000-8000-00000000dead",
	} {
		code, body := f.getAll(t, f.ownerToken, query)
		assert.Equal(t, fiber.StatusBadRequest, code, query)
		assert.JSONEq(t, invalidStatusBody, body, query)
	}
}

// TestTaskHandlerListAllDueBounds: due_after (inclusive) and due_before
// (exclusive) bound the due date as instants, whatever offset the bound is
// written in; an all-day due counts from midnight UTC of its date; a task
// without a due date never matches a bound. A bound that is not RFC 3339 is
// 400 — an unencoded "+" too, which arrives as a space.
func TestTaskHandlerListAllDueBounds(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "1", "due 05 09:00", "DUE:20301105T090000Z")
	f.todo(t, f.sharedRW, "2", "due 06 09:00", "DUE:20301106T090000Z")
	f.todo(t, f.taskList, "3", "due 07 all day", "DUE;VALUE=DATE:20301107")
	f.todo(t, f.otherList, "4", "due 08 09:00 Berlin", "DUE;TZID=Europe/Berlin:20301108T100000")
	f.todo(t, f.taskList, "5", "undated")

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", []string{"due 05 09:00", "due 06 09:00", "due 07 all day", "due 08 09:00 Berlin", "undated"}},
		{"?due_after=2030-11-05T09:00:00Z", []string{"due 05 09:00", "due 06 09:00", "due 07 all day", "due 08 09:00 Berlin"}},
		{"?due_after=2030-11-05T09:00:01Z", []string{"due 06 09:00", "due 07 all day", "due 08 09:00 Berlin"}},
		{"?due_before=2030-11-06T09:00:00Z", []string{"due 05 09:00"}},
		{"?due_after=2030-11-05T10:00:00Z&due_before=2030-11-08T09:00:00Z", []string{"due 06 09:00", "due 07 all day"}},
		{"?due_after=2030-11-07T00:00:00Z&due_before=2030-11-07T00:00:01Z", []string{"due 07 all day"}},
		// The same instants written with offsets (+ encoded as %2B).
		{"?due_after=2030-11-05T14:00:00%2B05:00", []string{"due 05 09:00", "due 06 09:00", "due 07 all day", "due 08 09:00 Berlin"}},
		{"?due_after=2030-11-05T14:00:01%2B05:00", []string{"due 06 09:00", "due 07 all day", "due 08 09:00 Berlin"}},
		{"?due_before=2030-11-06T01:00:00-08:00", []string{"due 05 09:00"}},
		{"?due_before=2030-11-06T01:00:01-08:00", []string{"due 05 09:00", "due 06 09:00"}},
		{"?due_after=2030-11-08T10:00:00%2B01:00&due_before=2030-11-08T10:00:01%2B01:00", []string{"due 08 09:00 Berlin"}},
		// An empty or inverted range is an empty page, not an error.
		{"?due_after=2030-11-06T09:00:00Z&due_before=2030-11-06T09:00:00Z", []string{}},
		{"?due_after=2030-11-07T00:00:00Z&due_before=2030-11-05T00:00:00Z", []string{}},
		{"?due_after=&due_before=", []string{"due 05 09:00", "due 06 09:00", "due 07 all day", "due 08 09:00 Berlin", "undated"}},
	} {
		page := f.listAll(t, f.ownerToken, tc.query)
		assert.Equal(t, tc.want, allSummaries(page), tc.query)
		assert.Equal(t, int64(len(tc.want)), page.Total, tc.query)
	}

	for _, tc := range []struct{ query, message string }{
		{"?due_after=tomorrow", "Invalid 'due_after' value"},
		{"?due_after=2030-11-05", "Invalid 'due_after' value"},
		{"?due_after=2030-11-05T09:00:00", "Invalid 'due_after' value"},
		{"?due_after=2030-11-05T14:00:00+05:00", "Invalid 'due_after' value"},
		{"?due_before=2030-13-01T00:00:00Z", "Invalid 'due_before' value"},
		{"?due_before=1730000000", "Invalid 'due_before' value"},
		// Checked before the calendar filter.
		{"?due_before=x&calendar_uuid=00000000-0000-4000-8000-00000000dead", "Invalid 'due_before' value"},
	} {
		code, body := f.getAll(t, f.ownerToken, tc.query)
		assert.Equal(t, fiber.StatusBadRequest, code, tc.query)
		assert.JSONEq(t, errorBody(tc.message), body, tc.query)
	}
}

// TestTaskHandlerListAllPaging: limit (default 500, capped at 1000) and
// offset page one ordering across lists; total counts every list.
func TestTaskHandlerListAllPaging(t *testing.T) {
	f := setupTaskHandlerTest(t)
	const n = 1001
	objs := make([]*calendar.CalendarObject, 0, n)
	for i := range n {
		cal := f.taskList
		if i%2 == 1 {
			cal = f.sharedRW
		}
		uid := fmt.Sprintf("bulk-%04d", i)
		obj := &calendar.CalendarObject{
			UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), CalendarID: cal.ID, Path: uid + ".ics", UID: uid,
			ETag: calendar.NewETag(),
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:" + uid +
				"\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:" + uid + "\r\n" +
				fmt.Sprintf("DUE:%s\r\n", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i)*time.Hour).Format("20060102T150405Z")) +
				"END:VTODO\r\nEND:VCALENDAR\r\n",
		}
		require.NoError(t, obj.PopulateDenormFieldsFromICal())
		objs = append(objs, obj)
	}
	require.NoError(t, f.db.DB().CreateInBatches(objs, 200).Error)

	for _, tc := range []struct {
		query       string
		count       int
		first, last string
	}{
		{"", 500, "bulk-0000", "bulk-0499"},
		{"?limit=0", 500, "bulk-0000", "bulk-0499"},
		{"?limit=5000", 1000, "bulk-0000", "bulk-0999"},
		{"?limit=1000&offset=1000", 1, "bulk-1000", "bulk-1000"},
		{"?limit=10&offset=995", 6, "bulk-0995", "bulk-1000"},
		{"?limit=3&offset=1", 3, "bulk-0001", "bulk-0003"},
		{"?offset=2000", 0, "", ""},
	} {
		page := f.listAll(t, f.ownerToken, tc.query)
		assert.Equal(t, tc.count, page.Count, tc.query)
		assert.Len(t, page.Tasks, tc.count, tc.query)
		assert.Equal(t, int64(n), page.Total, tc.query)
		if tc.count > 0 {
			assert.Equal(t, tc.first, page.Tasks[0].Summary, tc.query)
			assert.Equal(t, tc.last, page.Tasks[tc.count-1].Summary, tc.query)
		}
	}

	// One ordering across both lists: they alternate.
	page := f.listAll(t, f.ownerToken, "?limit=4")
	require.Len(t, page.Tasks, 4)
	for i, want := range []*calendar.Calendar{f.taskList, f.sharedRW, f.taskList, f.sharedRW} {
		assert.Equal(t, want.UUID, page.Tasks[i].CalendarUUID, i)
	}
	// The sharee reads only the shared half.
	shared := f.listAll(t, f.shareeToken, "?limit=1")
	assert.Equal(t, int64(500), shared.Total)
	assert.Equal(t, "bulk-0001", shared.Tasks[0].Summary)

	for _, tc := range []struct{ query, message string }{
		{"?limit=abc", "Invalid 'limit' value"},
		{"?limit=-1", "Invalid 'limit' value"},
		{"?limit=1.5", "Invalid 'limit' value"},
		{"?offset=-1", "Invalid 'offset' value"},
		{"?offset=x", "Invalid 'offset' value"},
	} {
		code, body := f.getAll(t, f.ownerToken, tc.query)
		assert.Equal(t, fiber.StatusBadRequest, code, tc.query)
		assert.JSONEq(t, errorBody(tc.message), body, tc.query)
	}
}

// countingRepo counts the repository calls of the cross-list use case.
type countingRepo struct {
	calendar.CalendarRepository
	calls map[string]int
}

func (r countingRepo) ListReadableCalendarIDs(ctx context.Context, userID uint) ([]calendar.ReadableCalendar, error) {
	r.calls["ListReadableCalendarIDs"]++
	return r.CalendarRepository.ListReadableCalendarIDs(ctx, userID)
}

func (r countingRepo) ListTasks(ctx context.Context, q calendar.TaskQuery) ([]*calendar.CalendarObject, int64, error) {
	r.calls["ListTasks"]++
	return r.CalendarRepository.ListTasks(ctx, q)
}

func (r countingRepo) ListByUserID(ctx context.Context, userID uint) ([]*calendar.Calendar, error) {
	r.calls["ListByUserID"]++
	return r.CalendarRepository.ListByUserID(ctx, userID)
}

func (r countingRepo) GetEventCount(ctx context.Context, id uint) (int64, error) {
	r.calls["GetEventCount"]++
	return r.CalendarRepository.GetEventCount(ctx, id)
}

func (r countingRepo) GetUserPermission(ctx context.Context, calendarID, userID uint) (calendar.CalendarPermission, error) {
	r.calls["GetUserPermission"]++
	return r.CalendarRepository.GetUserPermission(ctx, calendarID, userID)
}

func (r countingRepo) GetByID(ctx context.Context, id uint) (*calendar.Calendar, error) {
	r.calls["GetByID"]++
	return r.CalendarRepository.GetByID(ctx, id)
}

func (r countingRepo) GetByUUID(ctx context.Context, uuid string) (*calendar.Calendar, error) {
	r.calls["GetByUUID"]++
	return r.CalendarRepository.GetByUUID(ctx, uuid)
}

// TestTaskHandlerListAllOneQuery: whatever the number of readable lists, a
// page is one ListReadableCalendarIDs and one ListTasks — not the calendar
// list use case (ListByUserID + one GetEventCount per calendar), and no
// per-list lookup or permission check. On the wire that is one SELECT of
// calendars and one of calendar_objects (a short page needs no COUNT).
func TestTaskHandlerListAllOneQuery(t *testing.T) {
	f := setupTaskHandlerTest(t)
	for i := range 5 {
		cal := f.mkTaskCalendar(t, f.owner, fmt.Sprintf("22222222-0000-4000-8000-%012d", i), "extra", "#000000", "VTODO")
		f.todo(t, cal, fmt.Sprintf("extra-%d", i), "extra")
	}
	f.todo(t, f.taskList, "a", "own list")
	f.todo(t, f.sharedRW, "b", "shared")

	repo := countingRepo{CalendarRepository: f.repo, calls: map[string]int{}}
	handler := NewTaskHandler(nil, nil, nil, nil, nil, nil, nil, taskuc.NewListAllTasksUseCase(repo), nil, repo)
	app := fiber.New()
	app.Get("/api/v1/tasks", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB())), handler.ListAll)

	var tables []string
	gdb := f.db.DB()
	require.NoError(t, gdb.Callback().Query().After("gorm:query").Register("test:list_all_tables", func(tx *gorm.DB) {
		if !tx.DryRun { // the share subquery is built in dry-run mode
			tables = append(tables, tx.Statement.Table)
		}
	}))
	t.Cleanup(func() { _ = gdb.Callback().Query().Remove("test:list_all_tables") })

	for _, query := range []string{"", "?status=all&q=e", "?calendar_uuid=" + f.taskList.UUID} {
		clear(repo.calls)
		tables = nil
		code, body := serveGet(t, app, "/api/v1/tasks"+query, f.ownerToken)
		require.Equal(t, fiber.StatusOK, code, body)
		assert.Equal(t, map[string]int{"ListReadableCalendarIDs": 1, "ListTasks": 1}, repo.calls, query)
		assert.Equal(t, []string{"users", "calendars", "calendar_objects"}, tables, query)
	}
	page := f.listAll(t, f.ownerToken, "?status=all")
	assert.Equal(t, 7, page.Count)
}

// TestTaskHandlerListAllAuth: the route is authenticated; without a valid
// token it is the middleware's 401, and a handler reached without the
// middleware answers 401 too instead of panicking on the missing user.
func TestTaskHandlerListAllAuth(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "a", "own list")

	code, body := f.getAll(t, "", "")
	assert.Equal(t, fiber.StatusUnauthorized, code)
	assert.JSONEq(t, `{"status":"error","message":"missing authentication token"}`, body)

	code, body = f.getAll(t, "not.a.jwt", "")
	assert.Equal(t, fiber.StatusUnauthorized, code)
	assert.NotContains(t, body, "own list")

	handler := NewTaskHandler(nil, nil, nil, nil, nil, nil, nil, taskuc.NewListAllTasksUseCase(f.repo), nil, f.repo)
	app := fiber.New()
	app.Get("/api/v1/tasks", handler.ListAll) // no Authenticate
	code, body = serveGet(t, app, "/api/v1/tasks", f.ownerToken)
	assert.Equal(t, fiber.StatusUnauthorized, code)
	assert.JSONEq(t, `{"status":"error","message":"missing authentication token"}`, body)
}

// failingReadableRepo makes ListReadableCalendarIDs fail.
type failingReadableRepo struct {
	calendar.CalendarRepository
	err error
}

func (r failingReadableRepo) ListReadableCalendarIDs(context.Context, uint) ([]calendar.ReadableCalendar, error) {
	return nil, r.err
}

// TestTaskHandlerListAllServerErrors (F28): a storage failure — of the
// calendar lookup or of the task query — is a 500 with a generic body, the
// cause in the server log only.
func TestTaskHandlerListAllServerErrors(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "a", "own list")

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
			code, body := f.getAll(t, f.ownerToken, "")
			assert.Equal(t, fiber.StatusInternalServerError, code)
			assert.JSONEq(t, internalErrorBody, body)
			assert.NotContains(t, body, table)
			assert.Contains(t, logBuf.String(), "no such table: "+table)
			assert.Contains(t, logBuf.String(), "task request failed")
			assert.Contains(t, logBuf.String(), "path=/api/v1/tasks")
		})
	}

	t.Run("failing lookup, also with a calendar filter", func(t *testing.T) {
		handler := NewTaskHandler(nil, nil, nil, nil, nil, nil, nil,
			taskuc.NewListAllTasksUseCase(failingReadableRepo{CalendarRepository: f.repo, err: errors.New("disk I/O error (injected)")}), nil, f.repo)
		app := fiber.New()
		app.Get("/api/v1/tasks", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB())), handler.ListAll)
		for _, query := range []string{"", "?calendar_uuid=" + f.taskList.UUID, "?calendar_uuid=unknown"} {
			logBuf.Reset()
			code, body := serveGet(t, app, "/api/v1/tasks"+query, f.ownerToken)
			assert.Equal(t, fiber.StatusInternalServerError, code, query)
			assert.JSONEq(t, internalErrorBody, body, query)
			assert.Contains(t, logBuf.String(), "disk I/O error (injected)", query)
		}
	})
}
