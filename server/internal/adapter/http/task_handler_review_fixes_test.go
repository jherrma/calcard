package http

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	taskuc "github.com/jherrma/caldav-server/internal/usecase/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for the review fixes MJ1, MN1, MN3, MN5 and MN6 of PR 256.

// appOver serves every task route over repo, which wraps f.repo.
func (f *taskFixture) appOver(repo calendar.CalendarRepository) *fiber.App {
	handler := NewTaskHandler(taskuc.NewListTasksUseCase(repo), taskuc.NewGetTaskUseCase(repo),
		taskuc.NewCreateTaskUseCase(repo, nil), taskuc.NewUpdateTaskUseCase(repo, nil), taskuc.NewDeleteTaskUseCase(repo),
		taskuc.NewCompleteTaskUseCase(repo, nil), taskuc.NewUncompleteTaskUseCase(repo, nil), taskuc.NewListAllTasksUseCase(repo),
		taskuc.NewTaskSummaryUseCase(repo), repo)
	app := fiber.New()
	g := app.Group("/api/v1").Group("/calendars", Authenticate(f.jwt, repository.NewUserRepository(f.db.DB()))).Group("/:calendar_id/tasks")
	g.Get("/", handler.List)
	g.Get("/:task_id", handler.Get)
	g.Patch("/:task_id", handler.Update)
	g.Delete("/:task_id", handler.Delete)
	g.Post("/:task_id/complete", handler.Complete)
	g.Post("/:task_id/uncomplete", handler.Uncomplete)
	return app
}

func (f *taskFixture) call(t *testing.T, app *fiber.App, method, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, "/api/v1/calendars/"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.ownerToken)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// racingRepo runs hook right after the n-th object lookup returned, which is
// where a concurrent writer gets in between a use case's load and its write.
type racingRepo struct {
	calendar.CalendarRepository
	n     int
	calls int
	hook  func()
}

func (r *racingRepo) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	obj, err := r.CalendarRepository.GetCalendarObjectByUUID(ctx, uuid)
	r.calls++
	if r.calls == r.n && r.hook != nil {
		r.hook()
	}
	return obj, err
}

// TestTaskHandlerWriteRacingDelete (MJ1): a PATCH, complete or uncomplete that
// loses a race against a DELETE is the 404 of an unknown task and never brings
// the row back. Lookup 1 is the handler's access check, lookup 2 the use
// case's load.
func TestTaskHandlerWriteRacingDelete(t *testing.T) {
	f := setupTaskHandlerTest(t)
	routes := []struct{ name, method, suffix, body, status string }{
		{"patch", http.MethodPatch, "", `{"summary":"late"}`, ""},
		{"complete", http.MethodPost, "/complete", "", ""},
		{"uncomplete", http.MethodPost, "/uncomplete", "", "STATUS:COMPLETED"},
	}
	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			var props []string
			if r.status != "" {
				props = append(props, r.status)
			}
			task := f.todo(t, f.taskList, "race-"+r.name, "Renew passport", props...)
			repo := &racingRepo{CalendarRepository: f.repo, n: 2}
			repo.hook = func() { require.NoError(t, f.repo.HardDeleteCalendarObject(context.Background(), task)) }
			_, _, changesBefore := f.syncState(t, f.taskList)

			code, body := f.call(t, f.appOver(repo), r.method, taskPath(f.taskList, task)+r.suffix, r.body, nil)
			assert.Equal(t, fiber.StatusNotFound, code, body)
			assert.JSONEq(t, taskNotFoundBody, body)

			var rows int64
			require.NoError(t, f.db.DB().Unscoped().Model(&calendar.CalendarObject{}).Where("uuid = ?", task.UUID).Count(&rows).Error)
			assert.Zero(t, rows, "the deleted task stays deleted")
			_, _, changesAfter := f.syncState(t, f.taskList)
			assert.Equal(t, changesBefore+1, changesAfter, "only the delete is logged")
		})
	}
}

// TestTaskHandlerIfMatchIsAtomic (MN3): of two writers holding the same ETag
// only the first wins. Lookup 1 is the access check, 2 the If-Match check, 3
// the use case's load; the other writer gets in after lookup 3.
func TestTaskHandlerIfMatchIsAtomic(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "atomic", "Renew passport")
	ifMatch := map[string]string{"If-Match": `"` + task.ETag + `"`}
	repo := &racingRepo{CalendarRepository: f.repo, n: 3}
	repo.hook = func() {
		other, err := f.repo.GetCalendarObjectByUUID(context.Background(), task.UUID)
		require.NoError(t, err)
		code, body := f.send(t, http.MethodPatch, f.ownerToken, taskPath(f.taskList, other),
			`{"summary":"the other writer"}`, map[string]string{"Content-Type": "application/json"})
		require.Equal(t, fiber.StatusOK, code, body)
	}

	code, body := f.call(t, f.appOver(repo), http.MethodPatch, taskPath(f.taskList, task), `{"summary":"me"}`, ifMatch)
	assert.Equal(t, fiber.StatusPreconditionFailed, code, body)
	assert.JSONEq(t, preconditionFailedBody, body)
	assert.Equal(t, "the other writer", f.reload(t, task).Summary)
}

// TestTaskHandlerUnconditionalWriteRetries: without an If-Match a PATCH that
// meets a concurrent change is redone on the fresh task and succeeds.
func TestTaskHandlerUnconditionalWriteRetries(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "retry", "Renew passport")
	repo := &racingRepo{CalendarRepository: f.repo, n: 2}
	repo.hook = func() {
		code, body := f.send(t, http.MethodPatch, f.ownerToken, taskPath(f.taskList, task),
			`{"description":"from the other writer"}`, map[string]string{"Content-Type": "application/json"})
		require.Equal(t, fiber.StatusOK, code, body)
	}
	code, body := f.call(t, f.appOver(repo), http.MethodPatch, taskPath(f.taskList, task), `{"summary":"mine"}`, nil)
	require.Equal(t, fiber.StatusOK, code, body)
	got := f.reload(t, task)
	assert.Equal(t, "mine", got.Summary)
	assert.Equal(t, "from the other writer", got.Description, "the other change is kept")
}

// failingAccessRepo fails the lookups of the access check with err.
type failingAccessRepo struct {
	calendar.CalendarRepository
	userPerm, byUUID, object bool
	err                      error
}

func (r failingAccessRepo) GetUserPermission(ctx context.Context, calendarID, userID uint) (calendar.CalendarPermission, error) {
	if r.userPerm {
		return calendar.PermissionNone, r.err
	}
	return r.CalendarRepository.GetUserPermission(ctx, calendarID, userID)
}

func (r failingAccessRepo) GetByUUID(ctx context.Context, uuid string) (*calendar.Calendar, error) {
	if r.byUUID {
		return nil, r.err
	}
	return r.CalendarRepository.GetByUUID(ctx, uuid)
}

func (r failingAccessRepo) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	if r.object {
		return nil, r.err
	}
	return r.CalendarRepository.GetCalendarObjectByUUID(ctx, uuid)
}

// TestTaskHandlerAccessCheckFailure (MN5): a storage failure while checking
// access is a generic 500 with the cause in the log, not the 404 of an
// unknown task, on every route.
func TestTaskHandlerAccessCheckFailure(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "access", "Renew passport")

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	failures := map[string]failingAccessRepo{
		"permission lookup": {userPerm: true},
		"calendar lookup":   {byUUID: true},
		"object lookup":     {object: true},
	}
	routes := []struct{ method, suffix, body string }{
		{http.MethodGet, "", ""}, {http.MethodPatch, "", `{"summary":"x"}`}, {http.MethodDelete, "", ""},
		{http.MethodPost, "/complete", ""}, {http.MethodPost, "/uncomplete", ""},
	}
	for name, failing := range failures {
		failing.CalendarRepository = f.repo
		failing.err = errors.New("no such table: calendar_objects (injected)")
		app := f.appOver(failing)
		for _, r := range routes {
			logBuf.Reset()
			code, body := f.call(t, app, r.method, taskPath(f.taskList, task)+r.suffix, r.body, nil)
			assert.Equal(t, fiber.StatusInternalServerError, code, "%s %s %s", name, r.method, r.suffix)
			assert.JSONEq(t, internalErrorBody, body)
			assert.Contains(t, logBuf.String(), "injected", "the cause is logged")
		}
		logBuf.Reset()
		code, body := f.call(t, app, http.MethodGet, f.taskList.UUID+"/tasks/", "", nil)
		if failing.object {
			assert.Equal(t, fiber.StatusOK, code, "the list route does not look an object up: %s", body)
		} else {
			assert.Equal(t, fiber.StatusInternalServerError, code, name)
		}
	}
	assert.Equal(t, "Renew passport", f.reload(t, task).Summary)
}

// TestTaskHandlerRejectsControlCharacters (MN6): a NUL or another C0 control
// character in the summary or description is a 400; a TAB and line breaks are
// accepted.
func TestTaskHandlerRejectsControlCharacters(t *testing.T) {
	f := setupTaskHandlerTest(t)
	task := f.todo(t, f.taskList, "ctrl", "Renew passport")
	before, _, _ := f.syncState(t, f.taskList)

	for _, tc := range []struct{ body, msg string }{
		{`{"summary":"a\u0000b"}`, "summary must not contain control characters"},
		{`{"summary":"a\u0007b"}`, "summary must not contain control characters"},
		{`{"summary":"ok","description":"a\u0000b"}`, "description must not contain control characters"},
	} {
		code, body := f.post(t, f.ownerToken, f.taskList.UUID, tc.body)
		assert.Equal(t, fiber.StatusBadRequest, code, tc.body)
		assert.Contains(t, body, tc.msg)
	}
	code, body := f.patch(t, f.ownerToken, taskPath(f.taskList, task), `{"description":"x\u001fy"}`, nil)
	assert.Equal(t, fiber.StatusBadRequest, code, body)
	assert.Contains(t, body, "description must not contain control characters")

	code, body = f.patch(t, f.ownerToken, taskPath(f.taskList, task), `{"summary":"a\tb","description":"one\r\ntwo"}`, nil)
	assert.Equal(t, fiber.StatusOK, code, body)
	after, _, _ := f.syncState(t, f.taskList)
	assert.NotEqual(t, before, after)
}
