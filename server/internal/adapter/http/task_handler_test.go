package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	taskNotFoundBody     = `{"status":"error","message":"Task not found"}`
	calendarNotFoundBody = `{"status":"error","message":"Calendar not found"}`
	internalErrorBody    = `{"status":"error","message":"Internal server error"}`
	invalidStatusBody    = `{"status":"error","message":"status must be \"open\", \"completed\" or \"all\""}`
)

// taskFixture is the world every task handler test runs in: an owner with a
// task list, a calendar that stores events only, a subscribed calendar and
// two lists shared with a sharee (read and read-write), plus an outsider.
type taskFixture struct {
	app  *fiber.App
	db   database.Database
	repo *repository.CalendarRepository
	jwt  *authadapter.JWTManager

	owner, sharee, outsider                 *user.User
	ownerToken, shareeToken, outsiderToken  string
	taskList, otherList, eventCal, subbed   *calendar.Calendar
	sharedRead, sharedRW, shareeOwnTaskList *calendar.Calendar
}

func setupTaskHandlerTest(t *testing.T) *taskFixture {
	t.Helper()
	cfg := &config.Config{
		DataDir:  t.TempDir(),
		Database: config.DatabaseConfig{Driver: "sqlite"},
		JWT:      config.JWTConfig{Secret: "test-secret", AccessExpiry: time.Hour},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Migrate(database.Models()...))

	f := &taskFixture{db: db, repo: repository.NewCalendarRepository(db.DB()), jwt: authadapter.NewJWTManager(&cfg.JWT)}
	userRepo := repository.NewUserRepository(db.DB())
	mkUser := func(name string) (*user.User, string) {
		u := &user.User{UUID: name + "-uuid", Email: name + "@example.com", Username: name, IsActive: true}
		require.NoError(t, userRepo.Create(context.Background(), u))
		token, _, err := f.jwt.GenerateAccessToken(u.UUID, u.Email)
		require.NoError(t, err)
		return u, token
	}
	f.owner, f.ownerToken = mkUser("owner")
	f.sharee, f.shareeToken = mkUser("sharee")
	f.outsider, f.outsiderToken = mkUser("outsider")

	mkCal := func(owner *user.User, uuid, components string, subscribed bool) *calendar.Calendar {
		cal := &calendar.Calendar{UUID: uuid, UserID: owner.ID, Name: uuid, Path: uuid + ".ics", Color: "#112233",
			Timezone: "UTC", SupportedComponents: components, Subscribed: subscribed}
		require.NoError(t, f.repo.Create(context.Background(), cal))
		return cal
	}
	share := func(cal *calendar.Calendar, permission string) {
		require.NoError(t, db.DB().Create(&sharing.CalendarShare{
			UUID: cal.UUID + "-share", CalendarID: cal.ID, SharedWithID: f.sharee.ID, Permission: permission,
		}).Error)
	}
	f.taskList = mkCal(f.owner, "11111111-0000-4000-8000-000000000001", "VTODO", false)
	f.otherList = mkCal(f.owner, "11111111-0000-4000-8000-000000000002", "VTODO", false)
	f.eventCal = mkCal(f.owner, "11111111-0000-4000-8000-000000000003", "VEVENT", false)
	f.subbed = mkCal(f.owner, "11111111-0000-4000-8000-000000000004", "VEVENT,VTODO", true)
	f.sharedRead = mkCal(f.owner, "11111111-0000-4000-8000-000000000005", "VTODO", false)
	share(f.sharedRead, "read")
	f.sharedRW = mkCal(f.owner, "11111111-0000-4000-8000-000000000006", "VEVENT,VTODO", false)
	share(f.sharedRW, "read-write")
	f.shareeOwnTaskList = mkCal(f.sharee, "11111111-0000-4000-8000-000000000007", "VTODO", false)

	handler := NewTaskHandler(
		taskuc.NewListTasksUseCase(f.repo),
		taskuc.NewGetTaskUseCase(f.repo),
		taskuc.NewCreateTaskUseCase(f.repo, nil),
		taskuc.NewUpdateTaskUseCase(f.repo, func() time.Time { return taskHandlerClock }),
		taskuc.NewDeleteTaskUseCase(f.repo),
		taskuc.NewCompleteTaskUseCase(f.repo, func() time.Time { return taskHandlerClock }),
		taskuc.NewUncompleteTaskUseCase(f.repo, func() time.Time { return taskHandlerClock }),
		taskuc.NewListAllTasksUseCase(f.repo),
		taskuc.NewTaskSummaryUseCase(f.repo),
		f.repo,
	)
	f.app = fiber.New()
	v1 := f.app.Group("/api/v1")
	// As routes.go registers it: on v1 itself, with its own Authenticate.
	v1.Get("/tasks", Authenticate(f.jwt, userRepo), handler.ListAll)
	v1.Get("/tasks/summary", Authenticate(f.jwt, userRepo), handler.Summary)
	tasks := v1.Group("/calendars", Authenticate(f.jwt, userRepo)).Group("/:calendar_id/tasks")
	tasks.Get("/", handler.List)
	tasks.Post("/", handler.Create)
	tasks.Get("/:task_id", handler.Get)
	tasks.Patch("/:task_id", handler.Update)
	tasks.Delete("/:task_id", handler.Delete)
	tasks.Post("/:task_id/complete", handler.Complete)
	tasks.Post("/:task_id/uncomplete", handler.Uncomplete)
	return f
}

// taskHandlerClock is the clock of the complete and uncomplete use cases in
// the handler tests, in a non-UTC zone so a timestamp not converted to UTC
// would show.
var taskHandlerClock = time.Date(2026, 11, 3, 10, 30, 15, 0, time.FixedZone("CET", 3600))

// store writes an object into cal the way a CalDAV PUT does. lines are the
// component lines inside the VCALENDAR (BEGIN:VTODO … END:VTODO, possibly
// several components).
func (f *taskFixture) store(t *testing.T, cal *calendar.Calendar, uid string, lines ...string) *calendar.CalendarObject {
	t.Helper()
	obj := &calendar.CalendarObject{
		UUID:       uuid.NewString(),
		CalendarID: cal.ID,
		Path:       uid + ".ics",
		UID:        uid,
		ETag:       calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
			strings.Join(lines, "\r\n") + "\r\nEND:VCALENDAR\r\n",
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.NoError(t, f.repo.CreateCalendarObject(context.Background(), obj))
	return obj
}

// todo stores a VTODO with the given extra property lines.
func (f *taskFixture) todo(t *testing.T, cal *calendar.Calendar, uid, summary string, props ...string) *calendar.CalendarObject {
	t.Helper()
	lines := append([]string{"BEGIN:VTODO", "UID:" + uid, "DTSTAMP:20300101T000000Z", "SUMMARY:" + summary}, props...)
	return f.store(t, cal, uid, append(lines, "END:VTODO")...)
}

func (f *taskFixture) event(t *testing.T, cal *calendar.Calendar, uid string) *calendar.CalendarObject {
	t.Helper()
	return f.store(t, cal, uid, "BEGIN:VEVENT", "UID:"+uid, "DTSTAMP:20300101T000000Z",
		"DTSTART:20301105T090000Z", "DTEND:20301105T100000Z", "SUMMARY:Standup", "END:VEVENT")
}

// get sends GET /api/v1/calendars/<path> and returns status and body.
func (f *taskFixture) get(t *testing.T, token, path string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/calendars/"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// list fetches a task list page and decodes it.
func (f *taskFixture) list(t *testing.T, token string, cal *calendar.Calendar, query string) dto.TaskListResponse {
	t.Helper()
	code, body := f.get(t, token, cal.UUID+"/tasks"+query)
	require.Equal(t, fiber.StatusOK, code, body)
	var out dto.TaskListResponse
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	return out
}

func taskSummaries(list dto.TaskListResponse) []string {
	out := make([]string, 0, len(list.Tasks))
	for _, tk := range list.Tasks {
		out = append(out, tk.Summary)
	}
	return out
}

// TestTaskHandlerReadAccess: everyone who can read a calendar can list and get
// its tasks — owner, read-write sharee, read sharee, and the owner of a
// subscribed (read-only) calendar.
func TestTaskHandlerReadAccess(t *testing.T) {
	f := setupTaskHandlerTest(t)
	for _, tc := range []struct {
		name  string
		token string
		cal   *calendar.Calendar
	}{
		{"owner", f.ownerToken, f.taskList},
		{"read-write sharee", f.shareeToken, f.sharedRW},
		{"read sharee", f.shareeToken, f.sharedRead},
		{"subscribed calendar owner", f.ownerToken, f.subbed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := f.todo(t, tc.cal, "task-in-"+tc.cal.UUID, "Renew passport")

			list := f.list(t, tc.token, tc.cal, "")
			require.Len(t, list.Tasks, 1)
			assert.Equal(t, task.UUID, list.Tasks[0].ID)
			assert.Equal(t, 1, list.Count)
			assert.Equal(t, int64(1), list.Total)

			code, body := f.get(t, tc.token, tc.cal.UUID+"/tasks/"+task.UUID)
			require.Equal(t, fiber.StatusOK, code, body)
			var got dto.TaskResponse
			require.NoError(t, json.Unmarshal([]byte(body), &got))
			assert.Equal(t, task.UUID, got.ID)
			assert.Equal(t, tc.cal.ID, got.CalendarID)
			assert.Equal(t, tc.cal.UUID, got.CalendarUUID)
			assert.Equal(t, "Renew passport", got.Summary)
		})
	}
}

// TestTaskHandlerNotFound: the F05/F09 rule on the task routes. The list route
// answers an unknown or unreadable calendar with 404 "Calendar not found"; a
// task route answers every miss with the same 404 "Task not found" as an
// unknown task: an unknown or unreadable calendar, a task under another
// calendar UUID (readable or not), and an event's UUID.
func TestTaskHandlerNotFound(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "owner-task", "Owner's task")
	readTask := f.todo(t, f.sharedRead, "read-task", "Shared task")
	shareeTask := f.todo(t, f.shareeOwnTaskList, "sharee-task", "Sharee's own task")
	eventInEvents := f.event(t, f.eventCal, "event-1")
	eventInMixed := f.event(t, f.sharedRW, "event-2")
	const unknownCalendar = "00000000-0000-4000-8000-00000000dead"

	t.Run("list route", func(t *testing.T) {
		for _, tc := range []struct{ name, token, calendar, query string }{
			{"outsider", f.outsiderToken, f.taskList.UUID, ""},
			{"sharee on an unshared list", f.shareeToken, f.taskList.UUID, ""},
			{"owner on the sharee's list", f.ownerToken, f.shareeOwnTaskList.UUID, ""},
			{"unknown calendar", f.ownerToken, unknownCalendar, ""},
			// The calendar is resolved before the query is looked at.
			{"outsider with a bad status", f.outsiderToken, f.taskList.UUID, "?status=bogus"},
			{"outsider with a bad limit", f.outsiderToken, f.taskList.UUID, "?limit=-1"},
		} {
			code, body := f.get(t, tc.token, tc.calendar+"/tasks"+tc.query)
			assert.Equal(t, fiber.StatusNotFound, code, tc.name)
			assert.JSONEq(t, calendarNotFoundBody, body, tc.name)
		}
	})

	t.Run("task route", func(t *testing.T) {
		// The reference: an unknown task in a calendar the caller reads.
		code, reference := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks/00000000-0000-4000-8000-000000000001")
		require.Equal(t, fiber.StatusNotFound, code)
		require.JSONEq(t, taskNotFoundBody, reference)

		for _, tc := range []struct{ name, token, path string }{
			{"outsider", f.outsiderToken, f.taskList.UUID + "/tasks/" + task.UUID},
			{"sharee on an unshared list", f.shareeToken, f.taskList.UUID + "/tasks/" + task.UUID},
			{"unknown calendar", f.ownerToken, unknownCalendar + "/tasks/" + task.UUID},
			{"all-zero calendar", f.ownerToken, "00000000-0000-0000-0000-000000000000/tasks/" + task.UUID},
			{"task under another readable list", f.ownerToken, f.otherList.UUID + "/tasks/" + task.UUID},
			{"task under a readable list it is not in", f.shareeToken, f.sharedRW.UUID + "/tasks/" + readTask.UUID},
			{"task under an unreadable list", f.ownerToken, f.shareeOwnTaskList.UUID + "/tasks/" + task.UUID},
			{"another user's task under the caller's list", f.ownerToken, f.taskList.UUID + "/tasks/" + shareeTask.UUID},
			{"event UUID in its own calendar", f.ownerToken, f.eventCal.UUID + "/tasks/" + eventInEvents.UUID},
			{"event UUID in a mixed calendar", f.shareeToken, f.sharedRW.UUID + "/tasks/" + eventInMixed.UUID},
			{"event UUID under a task list", f.ownerToken, f.taskList.UUID + "/tasks/" + eventInEvents.UUID},
		} {
			code, body := f.get(t, tc.token, tc.path)
			assert.Equal(t, fiber.StatusNotFound, code, tc.name)
			assert.JSONEq(t, reference, body, "%s must look exactly like an unknown task", tc.name)
		}
	})
}

// TestTaskHandlerCalendarWithoutTasks: a calendar that does not store VTODO is
// not a task list; its list is 200 with no tasks, even when a stray VTODO sits
// in it.
func TestTaskHandlerCalendarWithoutTasks(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.event(t, f.eventCal, "an-event")
	// A VTODO written before the calendar was narrowed (only possible by hand).
	stray := &calendar.CalendarObject{UUID: "stray-uuid", CalendarID: f.eventCal.ID, Path: "stray.ics", UID: "stray",
		ETag: "e", ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:stray\r\n" +
			"DTSTAMP:20300101T000000Z\r\nSUMMARY:stray\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"}
	require.NoError(t, stray.PopulateDenormFieldsFromICal())
	require.NoError(t, f.db.DB().Create(stray).Error)

	for _, query := range []string{"", "?status=all", "?status=completed&q=stray"} {
		code, body := f.get(t, f.ownerToken, f.eventCal.UUID+"/tasks"+query)
		assert.Equal(t, fiber.StatusOK, code, query)
		assert.JSONEq(t, `{"tasks":[],"count":0,"total":0}`, body, query)
	}
	code, body := f.get(t, f.ownerToken, f.eventCal.UUID+"/tasks?status=done")
	assert.Equal(t, fiber.StatusBadRequest, code)
	assert.JSONEq(t, invalidStatusBody, body)
}

// TestTaskHandlerStatusFilter: one cancelled rule, done = COMPLETED or
// CANCELLED. The default is open; completed lists both done states; an
// unknown status is 400.
func TestTaskHandlerStatusFilter(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "s1", "needs action", "DUE:20301101T090000Z")
	f.todo(t, f.taskList, "s2", "in process", "DUE:20301102T090000Z", "STATUS:IN-PROCESS")
	f.todo(t, f.taskList, "s3", "completed", "DUE:20301103T090000Z", "STATUS:COMPLETED", "COMPLETED:20301101T100000Z")
	f.todo(t, f.taskList, "s4", "cancelled", "DUE:20301104T090000Z", "STATUS:CANCELLED")
	f.todo(t, f.taskList, "s5", "lower-case in-process", "DUE:20301105T090000Z", "STATUS:in-process")

	open := []string{"needs action", "in process", "lower-case in-process"}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", open},
		{"?status=open", open},
		{"?status=completed", []string{"completed", "cancelled"}},
		{"?status=all", []string{"needs action", "in process", "completed", "cancelled", "lower-case in-process"}},
	} {
		list := f.list(t, f.ownerToken, f.taskList, tc.query)
		assert.Equal(t, tc.want, taskSummaries(list), tc.query)
		assert.Equal(t, len(tc.want), list.Count, tc.query)
		assert.Equal(t, int64(len(tc.want)), list.Total, tc.query)
	}

	statuses := map[string]string{}
	for _, tk := range f.list(t, f.ownerToken, f.taskList, "?status=all").Tasks {
		statuses[tk.Summary] = tk.Status
		assert.Equal(t, tk.Status == "completed", tk.Completed, tk.Summary)
	}
	assert.Equal(t, map[string]string{
		"needs action": "needs-action", "in process": "in-process", "completed": "completed",
		"cancelled": "cancelled", "lower-case in-process": "in-process",
	}, statuses)

	for _, bad := range []string{"bogus", "OPEN", "Completed", "done", "cancelled", "open,all", "%20open"} {
		code, body := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks?status="+bad)
		assert.Equal(t, fiber.StatusBadRequest, code, bad)
		assert.JSONEq(t, invalidStatusBody, body, bad)
	}
}

// TestTaskHandlerTextQuery: q is a case-insensitive substring of the summary
// or description; LIKE wildcards in it match literally.
func TestTaskHandlerTextQuery(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "q1", "Renew Passport", "DUE:20301101T090000Z")
	f.todo(t, f.taskList, "q2", "Photos", "DUE:20301102T090000Z", "DESCRIPTION:for the passport office")
	f.todo(t, f.taskList, "q3", "Pay 100% of the bill", "DUE:20301103T090000Z")
	f.todo(t, f.taskList, "q4", "Pay 1000 later", "DUE:20301104T090000Z")
	f.todo(t, f.taskList, "q5", "rename file_a", "DUE:20301105T090000Z")
	f.todo(t, f.taskList, "q6", "rename fileXa", "DUE:20301106T090000Z")
	f.todo(t, f.taskList, "q7", "Ärztin anrufen", "DUE:20301107T090000Z")

	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"passport", []string{"Renew Passport", "Photos"}},
		{"PASSPORT", []string{"Renew Passport", "Photos"}},
		{"  passport  ", []string{"Renew Passport", "Photos"}},
		{"100%", []string{"Pay 100% of the bill"}},
		{"%", []string{"Pay 100% of the bill"}},
		{"file_a", []string{"rename file_a"}},
		{"ÄRZTIN", []string{"Ärztin anrufen"}},
		{"no such task", []string{}},
	} {
		list := f.list(t, f.ownerToken, f.taskList, "?q="+url.QueryEscape(tc.q))
		assert.Equal(t, tc.want, taskSummaries(list), "q=%q", tc.q)
		assert.Equal(t, int64(len(tc.want)), list.Total, "q=%q", tc.q)
	}
}

// TestTaskHandlerPaging: limit defaults to 500 and is capped at 1000, offset
// pages, count is the page and total the whole list; a limit or offset that is
// not a non-negative integer is 400.
func TestTaskHandlerPaging(t *testing.T) {
	f := setupTaskHandlerTest(t)
	const n = 1001
	objs := make([]*calendar.CalendarObject, 0, n)
	for i := range n {
		uid := fmt.Sprintf("bulk-%04d", i)
		obj := &calendar.CalendarObject{
			UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), CalendarID: f.taskList.ID, Path: uid + ".ics", UID: uid,
			ETag: calendar.NewETag(),
			ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:" + uid +
				"\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:" + uid + "\r\nPRIORITY:5\r\n" +
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
		{"?limit=1000", 1000, "bulk-0000", "bulk-0999"},
		{"?limit=1000&offset=1000", 1, "bulk-1000", "bulk-1000"},
		{"?limit=10&offset=995", 6, "bulk-0995", "bulk-1000"},
		{"?offset=500", 500, "bulk-0500", "bulk-0999"},
		{"?offset=2000", 0, "", ""},
	} {
		list := f.list(t, f.ownerToken, f.taskList, tc.query)
		assert.Equal(t, tc.count, list.Count, tc.query)
		assert.Len(t, list.Tasks, tc.count, tc.query)
		assert.Equal(t, int64(n), list.Total, tc.query)
		if tc.count > 0 {
			assert.Equal(t, tc.first, list.Tasks[0].Summary, tc.query)
			assert.Equal(t, tc.last, list.Tasks[tc.count-1].Summary, tc.query)
		}
	}

	for _, tc := range []struct{ query, message string }{
		{"?limit=abc", "Invalid 'limit' value"},
		{"?limit=-1", "Invalid 'limit' value"},
		{"?limit=1.5", "Invalid 'limit' value"},
		{"?offset=-1", "Invalid 'offset' value"},
		{"?offset=x", "Invalid 'offset' value"},
	} {
		code, body := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks"+tc.query)
		assert.Equal(t, fiber.StatusBadRequest, code, tc.query)
		assert.JSONEq(t, `{"status":"error","message":"`+tc.message+`"}`, body, tc.query)
	}
}

// TestTaskHandlerOrder: the default server order, undated and unprioritised
// tasks last.
func TestTaskHandlerOrder(t *testing.T) {
	f := setupTaskHandlerTest(t)
	f.todo(t, f.taskList, "o1", "undated")
	f.todo(t, f.taskList, "o2", "undated, priority 2", "PRIORITY:2")
	f.todo(t, f.taskList, "o3", "due later", "DUE:20301110T090000Z", "PRIORITY:1")
	f.todo(t, f.taskList, "o4", "due soon, no priority", "DUE:20301105T090000Z")
	f.todo(t, f.taskList, "o5", "due soon, priority 9", "DUE:20301105T090000Z", "PRIORITY:9")
	f.todo(t, f.taskList, "o6", "due soon, priority 1", "DUE:20301105T090000Z", "PRIORITY:1")

	assert.Equal(t, []string{
		"due soon, priority 1", "due soon, priority 9", "due soon, no priority", "due later",
		"undated, priority 2", "undated",
	}, taskSummaries(f.list(t, f.ownerToken, f.taskList, "")))
}

// TestTaskHandlerResponse pins the TaskResponse of tasks as CalDAV clients
// write them, identical from GET one and from the list: UTC due/start, the
// all-day forms, the status, completion, percent, priority, and a recurring
// task listed once (its master) with its rule, also without DTSTART.
func TestTaskHandlerResponse(t *testing.T) {
	f := setupTaskHandlerTest(t)
	timed := f.todo(t, f.taskList, "timed", "Renew passport",
		"DESCRIPTION:Bring two photos", "STATUS:IN-PROCESS", "PRIORITY:3", "PERCENT-COMPLETE:40",
		"DTSTART;TZID=Europe/Berlin:20301104T090000", "DUE;TZID=Europe/Berlin:20301105T170000")
	allDay := f.todo(t, f.taskList, "all-day", "Water the plants",
		"DTSTART;VALUE=DATE:20301106", "DUE;VALUE=DATE:20301107", "RRULE:FREQ=WEEKLY;BYDAY=TH")
	tasksOrg := f.todo(t, f.taskList, "tasks-org", "Take out the bins",
		"DUE:20301108T060000Z", "RRULE:FREQ=WEEKLY", "X-TASKS-ORG-ORDER:3")
	completed := f.todo(t, f.taskList, "done", "File taxes",
		"STATUS:COMPLETED", "COMPLETED:20301101T100000Z", "PERCENT-COMPLETE:100", "PRIORITY:0", "DUE:20301109T090000Z")
	// A recurring task with an override: one object, listed once.
	withOverride := f.store(t, f.taskList, "series",
		"BEGIN:VTODO", "UID:series", "DTSTAMP:20300101T000000Z", "SUMMARY:Weekly review",
		"DTSTART:20301110T080000Z", "DUE:20301110T090000Z", "RRULE:FREQ=WEEKLY;COUNT=4", "END:VTODO",
		"BEGIN:VTODO", "UID:series", "DTSTAMP:20300101T000000Z", "SUMMARY:Weekly review (moved)",
		"RECURRENCE-ID:20301117T080000Z", "DTSTART:20301118T080000Z", "DUE:20301118T090000Z", "END:VTODO")

	want := map[string]map[string]any{
		timed.UUID: {
			"summary": "Renew passport", "description": "Bring two photos",
			"status": "in-process", "completed": false, "completed_at": nil,
			"percent_complete": float64(40), "priority": float64(3),
			"due": "2030-11-05T16:00:00Z", "due_date": nil, "due_all_day": false,
			"start": "2030-11-04T08:00:00Z", "start_date": nil, "is_recurring": false, "rrule": nil,
		},
		allDay.UUID: {
			"summary": "Water the plants", "description": "",
			"status": "needs-action", "completed": false, "completed_at": nil,
			"percent_complete": nil, "priority": nil,
			"due": nil, "due_date": "2030-11-07", "due_all_day": true,
			"start": nil, "start_date": "2030-11-06", "is_recurring": true, "rrule": "FREQ=WEEKLY;BYDAY=TH",
		},
		tasksOrg.UUID: {
			"summary": "Take out the bins", "description": "",
			"status": "needs-action", "completed": false, "completed_at": nil,
			"percent_complete": nil, "priority": nil,
			"due": "2030-11-08T06:00:00Z", "due_date": nil, "due_all_day": false,
			"start": nil, "start_date": nil, "is_recurring": true, "rrule": "FREQ=WEEKLY",
		},
		completed.UUID: {
			"summary": "File taxes", "description": "",
			"status": "completed", "completed": true, "completed_at": "2030-11-01T10:00:00Z",
			"percent_complete": float64(100), "priority": nil,
			"due": "2030-11-09T09:00:00Z", "due_date": nil, "due_all_day": false,
			"start": nil, "start_date": nil, "is_recurring": false, "rrule": nil,
		},
		withOverride.UUID: {
			"summary": "Weekly review", "description": "",
			"status": "needs-action", "completed": false, "completed_at": nil,
			"percent_complete": nil, "priority": nil,
			"due": "2030-11-10T09:00:00Z", "due_date": nil, "due_all_day": false,
			"start": "2030-11-10T08:00:00Z", "start_date": nil, "is_recurring": true, "rrule": "FREQ=WEEKLY;COUNT=4",
		},
	}
	stored := map[string]*calendar.CalendarObject{
		timed.UUID: timed, allDay.UUID: allDay, tasksOrg.UUID: tasksOrg, completed.UUID: completed, withOverride.UUID: withOverride,
	}

	code, listBody := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks?status=all")
	require.Equal(t, fiber.StatusOK, code, listBody)
	var list struct {
		Tasks []map[string]any `json:"tasks"`
		Count int              `json:"count"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(listBody), &list))
	require.Len(t, list.Tasks, len(want), "a recurring task is one row")
	assert.Equal(t, len(want), list.Count)
	assert.Equal(t, len(want), list.Total)

	for _, item := range list.Tasks {
		id, _ := item["id"].(string)
		expected, ok := want[id]
		require.True(t, ok, "unexpected task %v", item)
		obj := stored[id]
		t.Run(expected["summary"].(string), func(t *testing.T) {
			// Fields every task carries the same way.
			full := map[string]any{
				"id": obj.UUID, "calendar_id": float64(f.taskList.ID), "calendar_uuid": f.taskList.UUID,
				"uid": obj.UID, "etag": obj.ETag, "reminders": []any{},
			}
			for k, v := range expected {
				full[k] = v
			}
			for _, k := range []string{"created_at", "updated_at"} {
				ts, _ := item[k].(string)
				parsed, err := time.Parse(time.RFC3339Nano, ts)
				require.NoError(t, err, k)
				assert.True(t, strings.HasSuffix(ts, "Z"), "%s %q must be UTC", k, ts)
				assert.WithinDuration(t, time.Now(), parsed, time.Minute, k)
				full[k] = ts
			}
			assert.Equal(t, full, item, "list item")

			code, body := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks/"+id)
			require.Equal(t, fiber.StatusOK, code, body)
			var one map[string]any
			require.NoError(t, json.Unmarshal([]byte(body), &one))
			assert.Equal(t, item, one, "GET one answers exactly the list item")
		})
	}
}

// failingObjectRepo makes the task use cases' object lookup fail (or see an
// event) while the handler's own access check still works: a storage error
// after the check, or a CalDAV PUT that turned the task into an event.
type failingObjectRepo struct {
	calendar.CalendarRepository
	err     error
	asEvent bool
}

func (r failingObjectRepo) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	if r.err != nil {
		return nil, r.err
	}
	obj, err := r.CalendarRepository.GetCalendarObjectByUUID(ctx, uuid)
	if err == nil && r.asEvent {
		obj.ComponentType = calendar.ComponentEvent
	}
	return obj, err
}

// TestTaskHandlerServerErrors (F28): a failure the caller cannot fix is a 500
// with a generic body, the cause in the server log only. A task that became an
// event after the handler's check is still the 404 of an unknown task.
func TestTaskHandlerServerErrors(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "t", "Renew passport")

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	t.Run("list with a failing object table", func(t *testing.T) {
		logBuf.Reset()
		require.NoError(t, f.db.DB().Exec("ALTER TABLE calendar_objects RENAME TO calendar_objects_t").Error)
		defer func() {
			require.NoError(t, f.db.DB().Exec("ALTER TABLE calendar_objects_t RENAME TO calendar_objects").Error)
		}()
		code, body := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks")
		assert.Equal(t, fiber.StatusInternalServerError, code)
		assert.JSONEq(t, internalErrorBody, body)
		assert.NotContains(t, body, "calendar_objects")
		assert.Contains(t, logBuf.String(), "no such table: calendar_objects")
		assert.Contains(t, logBuf.String(), "task request failed")
	})

	serve := func(useCaseRepo calendar.CalendarRepository) *fiber.App {
		handler := NewTaskHandler(taskuc.NewListTasksUseCase(useCaseRepo), taskuc.NewGetTaskUseCase(useCaseRepo),
			taskuc.NewCreateTaskUseCase(useCaseRepo, nil), taskuc.NewUpdateTaskUseCase(useCaseRepo, nil), taskuc.NewDeleteTaskUseCase(useCaseRepo),
			taskuc.NewCompleteTaskUseCase(useCaseRepo, nil), taskuc.NewUncompleteTaskUseCase(useCaseRepo, nil), taskuc.NewListAllTasksUseCase(useCaseRepo),
			taskuc.NewTaskSummaryUseCase(useCaseRepo), f.repo)
		app := fiber.New()
		g := app.Group("/api/v1").Group("/calendars", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB()))).Group("/:calendar_id/tasks")
		g.Get("/:task_id", handler.Get)
		return app
	}
	call := func(app *fiber.App) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/calendars/"+f.taskList.UUID+"/tasks/"+task.UUID, nil)
		req.Header.Set("Authorization", "Bearer "+f.ownerToken)
		resp, err := app.Test(req)
		require.NoError(t, err)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}

	t.Run("get with a failing lookup in the use case", func(t *testing.T) {
		logBuf.Reset()
		code, body := call(serve(failingObjectRepo{CalendarRepository: f.repo, err: errors.New("disk I/O error (injected)")}))
		assert.Equal(t, fiber.StatusInternalServerError, code)
		assert.JSONEq(t, internalErrorBody, body)
		assert.Contains(t, logBuf.String(), "disk I/O error (injected)")
	})

	t.Run("get of a task that became an event meanwhile", func(t *testing.T) {
		code, body := call(serve(failingObjectRepo{CalendarRepository: f.repo, asEvent: true}))
		assert.Equal(t, fiber.StatusNotFound, code)
		assert.JSONEq(t, taskNotFoundBody, body)
	})
}

// TestTaskHandlerReminders (#169 story 10): VALARMs written by a DAV client
// come back as `reminders` from GET one and the list; an END-related alarm is
// editable on a task, a START-related one is not.
func TestTaskHandlerReminders(t *testing.T) {
	f := setupTaskHandlerTest(t)
	withAlarms := f.todo(t, f.taskList, "alarmed", "Pay rent", "DUE:20301105T170000Z",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;RELATED=END:-PT30M", "END:VALARM",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT30M", "END:VALARM")
	plain := f.todo(t, f.taskList, "plain", "No alarm", "DUE:20301106T170000Z")

	want := `[{"index":0,"action":"display","related":"end","offset_seconds":-1800,"at":null,"editable":true},
	{"index":1,"action":"display","related":"start","offset_seconds":-1800,"at":null,"editable":false}]`

	code, body := f.get(t, f.ownerToken, f.taskList.UUID+"/tasks/"+withAlarms.UUID)
	require.Equal(t, fiber.StatusOK, code, body)
	var one struct {
		Reminders json.RawMessage `json:"reminders"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &one))
	assert.JSONEq(t, want, string(one.Reminders))

	code, body = f.get(t, f.ownerToken, f.taskList.UUID+"/tasks?status=all")
	require.Equal(t, fiber.StatusOK, code, body)
	var list struct {
		Tasks []struct {
			ID        string          `json:"id"`
			Reminders json.RawMessage `json:"reminders"`
		} `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &list))
	require.Len(t, list.Tasks, 2)
	for _, task := range list.Tasks {
		if task.ID == withAlarms.UUID {
			assert.JSONEq(t, want, string(task.Reminders))
		} else {
			assert.Equal(t, plain.UUID, task.ID)
			assert.JSONEq(t, `[]`, string(task.Reminders))
		}
	}
}
