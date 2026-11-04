package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RFC 5545 TEXT escaping (§3.3.11) on the REST wire: summary, description and
// location are plain text. Before the fix the API answered the stored escaped
// value (`Buy milk\, eggs`) and a PATCH of that value escaped it again, adding
// backslashes on every save.

const (
	escapingSummary     = `Buy milk, eggs; bread \ more`
	escapingDescription = "Line one\nLine two, C:\\path; done"
	escapingLocation    = `Market, Hall 2; Gate \ B`
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// TestTaskHandlerTextRoundTrip: a task created with , ; \ and a newline is
// answered with exactly that text, stored escaped exactly once, and PATCHing
// the text a GET answered writes nothing (same ETag, no change-log row). A
// DAV-written escaped title reads as plain text the same way.
func TestTaskHandlerTextRoundTrip(t *testing.T) {
	f := setupTaskHandlerTest(t)

	created, _ := f.create(t, f.ownerToken, f.taskList,
		mustJSON(t, map[string]any{"summary": escapingSummary, "description": escapingDescription}))
	assert.Equal(t, escapingSummary, created.Summary)
	assert.Equal(t, escapingDescription, created.Description)

	stored := f.reload(t, &calendar.CalendarObject{UUID: created.ID})
	assert.Contains(t, stored.ICalData, "\r\nSUMMARY:Buy milk\\, eggs\\; bread \\\\ more\r\n", "escaped exactly once")
	assert.Contains(t, stored.ICalData, "\r\nDESCRIPTION:Line one\\nLine two\\, C:\\\\path\\; done\r\n")

	path := f.taskList.UUID + "/tasks/" + created.ID
	code, raw := f.get(t, f.ownerToken, path)
	require.Equal(t, fiber.StatusOK, code, raw)
	got := decodeTask(t, raw)
	assert.Equal(t, escapingSummary, got.Summary)
	assert.Equal(t, escapingDescription, got.Description)
	assert.Equal(t, []string{escapingSummary}, taskSummaries(f.list(t, f.ownerToken, f.taskList, "?status=all")))

	// The web form's save: the text as answered, sent back. Twice.
	_, _, changes := f.syncState(t, f.taskList)
	for range 2 {
		code, raw = f.patch(t, f.ownerToken, path,
			mustJSON(t, map[string]any{"summary": got.Summary, "description": got.Description}), nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		again := decodeTask(t, raw)
		assert.Equal(t, created.ETag, again.ETag, "nothing written")
		assert.Equal(t, escapingSummary, again.Summary)
		assert.Equal(t, escapingDescription, again.Description)
	}
	_, _, changesAfter := f.syncState(t, f.taskList)
	assert.Equal(t, changes, changesAfter, "no change-log row")
	assert.Equal(t, stored.ICalData, f.reload(t, &calendar.CalendarObject{UUID: created.ID}).ICalData)

	t.Run("a DAV-written escaped task", func(t *testing.T) {
		phone := f.todo(t, f.taskList, "phone", `Call Bob\, Alice\; maybe`, `DESCRIPTION:Agenda:\n1. budget\, 2. hiring`)
		code, raw := f.get(t, f.ownerToken, taskPath(f.taskList, phone))
		require.Equal(t, fiber.StatusOK, code, raw)
		got := decodeTask(t, raw)
		assert.Equal(t, "Call Bob, Alice; maybe", got.Summary)
		assert.Equal(t, "Agenda:\n1. budget, 2. hiring", got.Description)

		code, raw = f.patch(t, f.ownerToken, taskPath(f.taskList, phone),
			mustJSON(t, map[string]any{"summary": got.Summary, "description": got.Description}), nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		assert.Equal(t, phone.ETag, decodeTask(t, raw).ETag, "the read text sent back writes nothing")

		code, raw = f.patch(t, f.ownerToken, taskPath(f.taskList, phone),
			mustJSON(t, map[string]any{"summary": "Call Bob, Alice; tomorrow"}), nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		assert.Equal(t, "Call Bob, Alice; tomorrow", decodeTask(t, raw).Summary)
		data := f.reload(t, phone).ICalData
		assert.Contains(t, data, "\r\nSUMMARY:Call Bob\\, Alice\\; tomorrow\r\n")
		assert.Contains(t, data, "\r\nDESCRIPTION:Agenda:\\n1. budget\\, 2. hiring\r\n", "the description was not sent and is untouched")
	})
}

// TestEventHandlerTextRoundTrip: the same for events, which share the
// denormalization and the escaping helpers. An event created with , ; \ and a
// newline in summary, description and location is answered (create, GET, list)
// with that text, stored escaped once, and a PATCH of the answered text stores
// the same bytes again instead of doubling the backslashes. (An override's own
// text in an expanded list: TestExpandRecurringEvent_TextIsUnescaped.)
func TestEventHandlerTextRoundTrip(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	send := func(method, path string, body any) (int, []byte) {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			reader = bytes.NewReader([]byte(mustJSON(t, body)))
		} else {
			reader = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, "/api/v1/calendars/"+cal.UUID+path, reader)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.Bytes()
	}
	decode := func(raw []byte) dto.EventResponse {
		t.Helper()
		var ev dto.EventResponse
		require.NoError(t, json.Unmarshal(raw, &ev), string(raw))
		return ev
	}
	storedData := func(id string) string {
		t.Helper()
		var obj calendar.CalendarObject
		require.NoError(t, db.DB().Where("uuid = ?", id).First(&obj).Error)
		return obj.ICalData
	}
	assertText := func(ev dto.EventResponse, msg string) {
		t.Helper()
		assert.Equal(t, escapingSummary, ev.Summary, msg)
		assert.Equal(t, escapingDescription, ev.Description, msg)
		assert.Equal(t, escapingLocation, ev.Location, msg)
	}

	start := time.Date(2030, 3, 4, 9, 0, 0, 0, time.UTC)
	code, raw := send("POST", "/events", map[string]any{
		"summary": escapingSummary, "description": escapingDescription, "location": escapingLocation,
		"start": start, "end": start.Add(time.Hour),
	})
	require.Equal(t, fiber.StatusCreated, code, string(raw))
	created := decode(raw)
	assertText(created, "create")

	data := storedData(created.ID)
	assert.Contains(t, data, "\r\nSUMMARY:Buy milk\\, eggs\\; bread \\\\ more\r\n", "escaped exactly once")
	assert.Contains(t, data, "\r\nLOCATION:Market\\, Hall 2\\; Gate \\\\ B\r\n")

	code, raw = send("GET", "/events/"+created.ID, nil)
	require.Equal(t, fiber.StatusOK, code, string(raw))
	got := decode(raw)
	assertText(got, "GET")

	code, raw = send("GET", "/events?start=2030-03-01T00:00:00Z&end=2030-03-31T00:00:00Z", nil)
	require.Equal(t, fiber.StatusOK, code, string(raw))
	var listed struct {
		Events []dto.EventResponse `json:"events"`
	}
	require.NoError(t, json.Unmarshal(raw, &listed), string(raw))
	require.Len(t, listed.Events, 1)
	assertText(listed.Events[0], "list")

	for round := 1; round <= 2; round++ {
		code, raw = send("PATCH", "/events/"+created.ID, map[string]any{
			"summary": got.Summary, "description": got.Description, "location": got.Location,
		})
		require.Equal(t, fiber.StatusOK, code, string(raw))
		assertText(decode(raw), "PATCH response")
		after := storedData(created.ID)
		assert.Contains(t, after, "\r\nSUMMARY:Buy milk\\, eggs\\; bread \\\\ more\r\n", "round %d: no backslash added", round)
		assert.Contains(t, after, "\r\nDESCRIPTION:Line one\\nLine two\\, C:\\\\path\\; done\r\n", "round %d", round)
		assert.Contains(t, after, "\r\nLOCATION:Market\\, Hall 2\\; Gate \\\\ B\r\n", "round %d", round)
	}

	t.Run("a CRLF line break is stored as \\n", func(t *testing.T) {
		// go-ical's SetText copied the CR into the value and its encoder then
		// refused the event.
		code, raw := send("POST", "/events", map[string]any{
			"summary": "Notes", "description": "Line one\r\nLine two", "start": start, "end": start.Add(time.Hour),
		})
		require.Equal(t, fiber.StatusCreated, code, string(raw))
		ev := decode(raw)
		assert.Equal(t, "Line one\nLine two", ev.Description)
		assert.Contains(t, storedData(ev.ID), "\r\nDESCRIPTION:Line one\\nLine two\r\n")

		code, raw = send("PATCH", "/events/"+ev.ID, map[string]any{"location": "Room 1\r\nFloor 2"})
		require.Equal(t, fiber.StatusOK, code, string(raw))
		assert.Equal(t, "Room 1\nFloor 2", decode(raw).Location)
	})
}
