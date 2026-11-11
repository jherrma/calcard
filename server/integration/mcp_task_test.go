//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpTaskOf decodes the "task" object of a task tool's answer into the REST
// task shape, so it can be compared with what the REST routes answer.
func mcpTaskOf(t *testing.T, payload map[string]any) taskJSON {
	t.Helper()
	raw, err := json.Marshal(payload["task"])
	require.NoError(t, err)
	return decodeTask(t, raw)
}

// mustMCPTool calls a tool that must succeed.
func mustMCPTool(t *testing.T, credential, name string, args map[string]any) map[string]any {
	t.Helper()
	payload, isErr := mcpTool(t, credential, name, args)
	require.False(t, isErr, "%s %v failed: %v", name, args, payload["message"])
	return payload
}

// TestMCPTaskTools drives the task tools (#170) over the real socket with an
// MCP token and cross-checks every write through REST and CalDAV: the tools
// call the use cases the REST task routes call, so what MCP writes is exactly
// what REST and a DAV client then read, and MCP refuses what REST refuses.
func TestMCPTaskTools(t *testing.T) {
	email := "mcp-tasks@example.test"
	jwt, username := registerAndLoginFull(t, email, "MCPpassword123!", "MCP Tasks")
	token := mcpToken(t, jwt, "task client")
	_, appPass := createAppPassword(t, jwt, "mcp-tasks")

	var list taskCalendar
	var collection, taskID string

	t.Run("create_calendar kind tasks makes a task list", func(t *testing.T) {
		created := mustMCPTool(t, token, "create_calendar", map[string]any{"name": "MCP Errands", "kind": "tasks"})
		view := created["calendar"].(map[string]any)
		assert.Equal(t, "tasks", view["kind"])

		list = getTaskCalendar(t, jwt, view["id"].(string))
		assert.Equal(t, "tasks", list.Kind, "REST reports the kind MCP created")
		assert.Equal(t, "VTODO", list.SupportedComponents)
		collection = "/dav/" + username + "/calendars/" + list.Path + "/"
	})
	require.NotEmpty(t, list.UUID)

	t.Run("create_task", func(t *testing.T) {
		payload := mustMCPTool(t, token, "create_task", map[string]any{
			"calendar_id": list.UUID, "summary": "Book flights, hotel", "due": "2030-12-01T09:30:00+01:00",
			"timezone": "Europe/Berlin", "priority": 2,
		})
		assert.Equal(t, true, payload["created"])
		task := mcpTaskOf(t, payload)
		taskID = task.ID
		assertSameTask(t, task, getTask(t, jwt, list.UUID, task.ID), "REST GET answers what create_task answered")
		assert.Equal(t, "Book flights, hotel", task.Summary)
		require.NotNil(t, task.Due)
		assert.Equal(t, "2030-12-01T08:30:00Z", *task.Due)
	})
	require.NotEmpty(t, taskID)

	t.Run("update_task", func(t *testing.T) {
		payload := mustMCPTool(t, token, "update_task", map[string]any{
			"task_id": taskID, "summary": "Book flights", "description": "Window seat", "priority": nil,
		})
		assert.Equal(t, true, payload["updated"])
		task := mcpTaskOf(t, payload)
		rest := getTask(t, jwt, list.UUID, taskID)
		assertSameTask(t, task, rest)
		assert.Equal(t, "Book flights", rest.Summary)
		assert.Equal(t, "Window seat", rest.Description)
		assert.Nil(t, rest.Priority, "null removed the priority")
	})

	t.Run("list_tasks", func(t *testing.T) {
		createTask(t, jwt, list.UUID, map[string]any{"summary": "Renew passport"})

		for _, tc := range []struct {
			args map[string]any
			rest string
		}{
			{map[string]any{"status": "all"}, "/tasks?status=all&limit=50"},
			{map[string]any{"calendar_id": list.UUID, "query": "passport"},
				"/tasks?" + url.Values{"calendar_uuid": {list.UUID}, "q": {"passport"}, "limit": {"50"}}.Encode()},
		} {
			payload := mustMCPTool(t, token, "list_tasks", tc.args)
			raw, err := json.Marshal(payload)
			require.NoError(t, err)
			assert.JSONEq(t, string(mustGet(t, jwt, tc.rest)), string(raw), "list_tasks %v is GET %s", tc.args, tc.rest)
		}
	})

	t.Run("complete_task", func(t *testing.T) {
		task := mcpTaskOf(t, mustMCPTool(t, token, "complete_task", map[string]any{"task_id": taskID}))
		assert.True(t, task.Completed)
		assertSameTask(t, task, getTask(t, jwt, list.UUID, taskID))

		body, davETag := davGet(t, collection+taskID+".ics", email, appPass)
		assert.Contains(t, icalLines(body), "STATUS:COMPLETED", "a DAV client sees the completion")
		assert.Equal(t, `"`+task.ETag+`"`, davETag)

		again := mcpTaskOf(t, mustMCPTool(t, token, "complete_task", map[string]any{"task_id": taskID}))
		assert.Equal(t, task.ETag, again.ETag, "completing twice writes once")
	})

	t.Run("uncomplete_task", func(t *testing.T) {
		task := mcpTaskOf(t, mustMCPTool(t, token, "uncomplete_task", map[string]any{"task_id": taskID}))
		assert.Equal(t, "needs-action", task.Status)
		assertSameTask(t, task, getTask(t, jwt, list.UUID, taskID))
	})

	t.Run("a read sharee cannot write", func(t *testing.T) {
		roEmail := "mcp-tasks-ro@example.test"
		roJWT, _ := registerAndLoginFull(t, roEmail, "MCPpassword123!", "MCP Tasks Reader")
		roToken := mcpToken(t, roJWT, "reader client")
		shareCalendarWith(t, jwt, list.UUID, roEmail, "read")
		before := getTask(t, jwt, list.UUID, taskID)

		for _, call := range []struct {
			tool string
			args map[string]any
		}{
			{"update_task", map[string]any{"task_id": taskID, "summary": "Hijacked"}},
			{"complete_task", map[string]any{"task_id": taskID}},
			{"uncomplete_task", map[string]any{"task_id": taskID}},
			{"delete_task", map[string]any{"task_id": taskID}},
		} {
			refused, isErr := mcpTool(t, roToken, call.tool, call.args)
			require.True(t, isErr, call.tool)
			assert.Equal(t, "You have read-only access to the task list holding that task.", refused["message"], call.tool)
		}
		refused, isErr := mcpTool(t, roToken, "create_task", map[string]any{"calendar_id": list.UUID, "summary": "x"})
		require.True(t, isErr)
		assert.Equal(t, "You have read-only access to that calendar, so tasks cannot be created in it.", refused["message"])

		assertSameTask(t, before, getTask(t, jwt, list.UUID, taskID), "REST shows the task unchanged")
		assert.EqualValues(t, 2, taskPage(t, jwt, tasksURL(list.UUID)+"?status=all").Total, "nothing created or deleted")

		listed := mustMCPTool(t, roToken, "list_tasks", map[string]any{"calendar_id": list.UUID, "status": "all"})
		assert.EqualValues(t, 2, listed["total"], "the sharee can read the list")
	})

	t.Run("get_task_summary", func(t *testing.T) {
		const asOf = "2030-12-01T10:00:00+01:00"
		payload := mustMCPTool(t, token, "get_task_summary", map[string]any{"as_of": asOf})
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		rest := mustGet(t, jwt, "/tasks/summary?"+url.Values{"as_of": {asOf}}.Encode())
		assert.JSONEq(t, string(rest), string(raw), "the REST body for the same as_of")
		assert.EqualValues(t, 1, payload["overdue"], "due 09:30+01:00, asked at 10:00+01:00")
	})

	t.Run("delete_task", func(t *testing.T) {
		payload := mustMCPTool(t, token, "delete_task", map[string]any{"task_id": taskID})
		assert.Equal(t, map[string]any{"deleted": true, "task_id": taskID}, payload)

		status, raw := restCall(t, http.MethodGet, taskURL(list.UUID, taskID), jwt, nil)
		assertErrorBody(t, http.StatusNotFound, "Task not found", status, raw)
		status, _, _ = davCall(t, "GET", collection+taskID+".ics", email, appPass, "", nil)
		assert.Equal(t, http.StatusNotFound, status, "DAV clients lose it too")
		var rows int64
		require.NoError(t, testDB.Unscoped().Model(&calendar.CalendarObject{}).Where("uuid = ?", taskID).Count(&rows).Error)
		assert.Zero(t, rows, "a hard delete: no row left")

		refused, isErr := mcpTool(t, token, "delete_task", map[string]any{"task_id": taskID})
		require.True(t, isErr)
		assert.Equal(t, "No task with id "+taskID+" is readable by you.", refused["message"])
	})
}
