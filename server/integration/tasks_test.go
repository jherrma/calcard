//go:build integration

package integration_test

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tasks (#170, Phase 1) end to end: the task routes over the real socket, the
// round trip between REST and CalDAV, sharing, the component-set rule on every
// write path, and what a REST edit must leave alone in a VTODO a phone wrote.
// Every date lies in 2030 so no assertion depends on the clock.

// taskJSON is one task as the task routes answer it (dto.TaskResponse). Times
// stay strings so the wire form ("…Z", "YYYY-MM-DD") is what gets asserted.
// CalendarName and CalendarColor are only sent by the cross-list GET /tasks.
type taskJSON struct {
	ID              string  `json:"id"`
	CalendarID      uint    `json:"calendar_id"`
	CalendarUUID    string  `json:"calendar_uuid"`
	UID             string  `json:"uid"`
	Summary         string  `json:"summary"`
	Description     string  `json:"description"`
	Status          string  `json:"status"`
	Completed       bool    `json:"completed"`
	CompletedAt     *string `json:"completed_at"`
	PercentComplete *int    `json:"percent_complete"`
	Priority        *int    `json:"priority"`
	Due             *string `json:"due"`
	DueDate         *string `json:"due_date"`
	DueAllDay       bool    `json:"due_all_day"`
	Start           *string `json:"start"`
	StartDate       *string `json:"start_date"`
	IsRecurring     bool    `json:"is_recurring"`
	RRule           *string `json:"rrule"`
	ETag            string  `json:"etag"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	CalendarName    string  `json:"calendar_name"`
	CalendarColor   string  `json:"calendar_color"`
}

type taskPageJSON struct {
	Tasks []taskJSON `json:"tasks"`
	Count int        `json:"count"`
	Total int64      `json:"total"`
}

type taskSummaryJSON struct {
	Open       int64 `json:"open"`
	Overdue    int64 `json:"overdue"`
	DueToday   int64 `json:"due_today"`
	Completed  int64 `json:"completed"`
	ByCalendar map[string]struct {
		Open    int64 `json:"open"`
		Overdue int64 `json:"overdue"`
	} `json:"by_calendar"`
}

// taskCalendar is a calendar created through POST /calendars with a kind.
type taskCalendar struct {
	ID                  uint   `json:"id"`
	UUID                string `json:"uuid"`
	Path                string `json:"path"`
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	SupportedComponents string `json:"supported_components"`
	CTag                string `json:"ctag"`
}

const (
	readOnlyMessage     = "You have read-only access to this calendar"
	recurringRefusal    = "Repeating tasks cannot be completed here yet. Complete it in your task app."
	icsContentType      = "text/calendar; charset=utf-8"
	emptyTaskPage       = `{"tasks":[],"count":0,"total":0}`
	eventsOnlyRefusal   = "This calendar does not store events"
	tasksOnlyRefusal    = "This calendar does not store tasks"
	narrowingRefusal    = "Cannot narrow a calendar's component set"
	unknownKindMessage  = `kind must be "events", "tasks" or "both"`
	invalidTaskStatus   = `status must be "needs-action", "in-process", "completed" or "cancelled"`
	invalidStatusFilter = `status must be "open", "completed" or "all"`
)

// errorJSON is the ErrorResponse envelope with message msg.
func errorJSON(msg string) string {
	b, _ := json.Marshal(map[string]string{"status": "error", "message": msg})
	return string(b)
}

// assertErrorBody checks a refusal's status and its exact envelope.
func assertErrorBody(t *testing.T, wantStatus int, wantMsg string, status int, raw []byte, msgAndArgs ...any) {
	t.Helper()
	assert.Equal(t, wantStatus, status, msgAndArgs...)
	assert.JSONEq(t, errorJSON(wantMsg), string(raw), msgAndArgs...)
}

func tasksURL(calUUID string) string { return "/calendars/" + calUUID + "/tasks" }

func taskURL(calUUID, taskID string) string { return tasksURL(calUUID) + "/" + taskID }

// apiCall is restCall with extra headers (If-Match).
func apiCall(t *testing.T, method, path, token string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	return rawCall(t, method, baseURL+"/api/v1"+path, token, body, headers)
}

func ifMatch(etag string) map[string]string { return map[string]string{"If-Match": `"` + etag + `"`} }

func decodeTask(t *testing.T, raw []byte) taskJSON {
	t.Helper()
	var out taskJSON
	require.NoErrorf(t, json.Unmarshal(raw, &out), "decode task: %s", string(raw))
	return out
}

func createTaskCalendar(t *testing.T, token, name, kind string) taskCalendar {
	t.Helper()
	body := map[string]string{"name": name, "color": "#16a34a"}
	if kind != "" {
		body["kind"] = kind
	}
	status, raw := restCall(t, http.MethodPost, "/calendars/", token, body)
	require.Equalf(t, http.StatusCreated, status, "create calendar %s: %s", name, string(raw))
	var cal taskCalendar
	require.NoError(t, json.Unmarshal(raw, &cal))
	require.NotEmpty(t, cal.UUID)
	require.NotEmpty(t, cal.Path)
	return cal
}

func getTaskCalendar(t *testing.T, token, calUUID string) taskCalendar {
	t.Helper()
	var cal taskCalendar
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID, token, nil, &cal))
	return cal
}

func createTask(t *testing.T, token, calUUID string, body map[string]any) taskJSON {
	t.Helper()
	status, raw := restCall(t, http.MethodPost, tasksURL(calUUID), token, body)
	require.Equalf(t, http.StatusCreated, status, "create task: %s", string(raw))
	return decodeTask(t, raw)
}

func getTask(t *testing.T, token, calUUID, taskID string) taskJSON {
	t.Helper()
	status, raw := restCall(t, http.MethodGet, taskURL(calUUID, taskID), token, nil)
	require.Equalf(t, http.StatusOK, status, "get task: %s", string(raw))
	return decodeTask(t, raw)
}

// taskPage GETs a task list route (per list or /tasks) and requires a 200.
func taskPage(t *testing.T, token, path string) taskPageJSON {
	t.Helper()
	status, raw := restCall(t, http.MethodGet, path, token, nil)
	require.Equalf(t, http.StatusOK, status, "GET %s: %s", path, string(raw))
	var page taskPageJSON
	require.NoError(t, json.Unmarshal(raw, &page))
	require.NotNil(t, page.Tasks, "tasks is [] when empty, never null: %s", string(raw))
	assert.Equal(t, len(page.Tasks), page.Count, "count is the page length")
	return page
}

func taskSummaries(page taskPageJSON) []string {
	out := make([]string, 0, len(page.Tasks))
	for _, task := range page.Tasks {
		out = append(out, task.Summary)
	}
	return out
}

// findTaskByUID returns the task with uid from a list's tasks (all statuses);
// a DAV-written task gets its REST id from the server, not from its href.
func findTaskByUID(t *testing.T, token, calUUID, uid string) taskJSON {
	t.Helper()
	for _, task := range taskPage(t, token, tasksURL(calUUID)+"?status=all").Tasks {
		if task.UID == uid {
			return task
		}
	}
	t.Fatalf("no task with UID %s in calendar %s", uid, calUUID)
	return taskJSON{}
}

// assertSameTask compares two task bodies. created_at and updated_at are
// compared as instants within a millisecond: a write answers with the time it
// held in memory, a later read with what the database kept, and PostgreSQL
// keeps microseconds.
func assertSameTask(t *testing.T, want, got taskJSON, msgAndArgs ...any) {
	t.Helper()
	for _, pair := range [][2]string{{want.CreatedAt, got.CreatedAt}, {want.UpdatedAt, got.UpdatedAt}} {
		w, err := time.Parse(time.RFC3339Nano, pair[0])
		require.NoError(t, err)
		g, err := time.Parse(time.RFC3339Nano, pair[1])
		require.NoError(t, err)
		assert.WithinDuration(t, w, g, time.Millisecond, msgAndArgs...)
	}
	want.CreatedAt, want.UpdatedAt, got.CreatedAt, got.UpdatedAt = "", "", "", ""
	assert.Equal(t, want, got, msgAndArgs...)
}

func strp(s string) *string { return &s }

func intp(n int) *int { return &n }

// ics joins iCalendar content lines with CRLF.
func ics(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

// vcalendar wraps components into a VCALENDAR as a phone would write it.
func vcalendar(components ...string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Integration Tests//EN\r\n" +
		strings.Join(components, "") + "END:VCALENDAR\r\n"
}

var berlinVTimezone = ics(
	"BEGIN:VTIMEZONE", "TZID:Europe/Berlin",
	"BEGIN:STANDARD", "DTSTART:19701025T030000", "TZOFFSETFROM:+0200", "TZOFFSETTO:+0100",
	"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU", "END:STANDARD",
	"BEGIN:DAYLIGHT", "DTSTART:19700329T020000", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0200",
	"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU", "END:DAYLIGHT",
	"END:VTIMEZONE")

func davPut(t *testing.T, objectPath, email, pass, body string) {
	t.Helper()
	status, _, resp := davCall(t, "PUT", objectPath, email, pass, body, map[string]string{"Content-Type": icsContentType})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT %s: %s", objectPath, string(resp))
}

// davGet GETs an object and returns its body and ETag header.
func davGet(t *testing.T, objectPath, email, pass string) (string, string) {
	t.Helper()
	status, hdr, body := davCall(t, "GET", objectPath, email, pass, "", nil)
	require.Equalf(t, http.StatusOK, status, "GET %s: %s", objectPath, string(body))
	return string(body), hdr.Get("ETag")
}

// icalLines unfolds an iCalendar body into its content lines (RFC 5545 §3.1).
func icalLines(body string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// propName is the name of a content line: what precedes its first ';' or ':'.
func propName(line string) string {
	if i := strings.IndexAny(line, ";:"); i >= 0 {
		return line[:i]
	}
	return line
}

// block returns the lines from BEGIN:<name> through END:<name> (first one).
func block(lines []string, name string) []string {
	for i, line := range lines {
		if line != "BEGIN:"+name {
			continue
		}
		for j := i; j < len(lines); j++ {
			if lines[j] == "END:"+name {
				return lines[i : j+1]
			}
		}
	}
	return nil
}

const vtodoQueryBody = `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VTODO"/></C:comp-filter></C:filter>
</C:calendar-query>`

const veventQueryBody = `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"/></C:comp-filter></C:filter>
</C:calendar-query>`

const propfindComponentsBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CS="http://calendarserver.org/ns/">
  <D:prop><CS:getctag/><C:supported-calendar-component-set/></D:prop>
</D:propfind>`

var getctagPattern = regexp.MustCompile(`getctag[^>]*>([^<]*)<`)

// davComponents PROPFINDs a collection (Depth 0) and returns which of VEVENT
// and VTODO its supported-calendar-component-set advertises, and its getctag.
func davComponents(t *testing.T, collection, email, pass string) (advertised []string, ctag string) {
	t.Helper()
	status, _, body := davCall(t, "PROPFIND", collection, email, pass, propfindComponentsBody, depthHeader("0"))
	require.Equalf(t, http.StatusMultiStatus, status, "PROPFIND %s: %s", collection, string(body))
	for _, comp := range []string{"VEVENT", "VTODO"} {
		if strings.Contains(string(body), `name="`+comp+`"`) {
			advertised = append(advertised, comp)
		}
	}
	m := getctagPattern.FindStringSubmatch(string(body))
	require.NotNilf(t, m, "no getctag in %s", string(body))
	return advertised, m[1]
}

// syncChanges runs an incremental sync-collection REPORT from tok and returns
// the members it reports — the last segment of each href, mapped to true when
// it is reported as removed (404) — and the new sync token.
func syncChanges(t *testing.T, collection, email, pass, tok string) (map[string]bool, string) {
	t.Helper()
	status, _, body := davCall(t, "REPORT", collection, email, pass, incrementalSyncBody(tok), depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "sync-collection: %s", string(body))
	var ms struct {
		Responses []struct {
			Href   string `xml:"href"`
			Status string `xml:"status"`
		} `xml:"response"`
		SyncToken string `xml:"sync-token"`
	}
	require.NoErrorf(t, xml.Unmarshal(body, &ms), "parse multistatus: %s", string(body))
	out := map[string]bool{}
	for _, r := range ms.Responses {
		href, err := url.PathUnescape(r.Href)
		require.NoError(t, err)
		out[path.Base(href)] = strings.Contains(r.Status, "404")
	}
	require.NotEmpty(t, ms.SyncToken)
	return out, ms.SyncToken
}

func initialSyncToken(t *testing.T, collection, email, pass string) string {
	t.Helper()
	status, _, body := davCall(t, "REPORT", collection, email, pass, syncCollectionBody, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "initial sync: %s", string(body))
	tok := extractSyncToken(string(body))
	require.NotEmpty(t, tok)
	return tok
}

func shareCalendarWith(t *testing.T, ownerToken, calUUID, email, permission string) {
	t.Helper()
	status, raw := restCall(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken,
		map[string]string{"user_identifier": email, "permission": permission})
	require.Equalf(t, http.StatusCreated, status, "share (%s): %s", permission, string(raw))
}

func summaryAsOf(t *testing.T, token, asOf string) taskSummaryJSON {
	t.Helper()
	path := "/tasks/summary?" + url.Values{"as_of": {asOf}}.Encode()
	status, raw := restCall(t, http.MethodGet, path, token, nil)
	require.Equalf(t, http.StatusOK, status, "summary: %s", string(raw))
	var out taskSummaryJSON
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// ----------------------------------------------------------------------------

// TestTasksCRUD walks every task route over REST on one task list: create
// (incl. validation), list (order, filters, paging), get, the same 404 for
// every miss, patch (incl. If-Match and the no-op), complete and uncomplete
// (idempotent, percent handling), the cross-list GET /tasks and the summary,
// and delete. Subtests share a *taskFlowState like flow_test.go.
func TestTasksCRUD(t *testing.T) {
	s := &taskFlowState{}
	t.Run("CreateTaskList", s.createList)
	t.Run("CreateTasks", s.createTasks)
	t.Run("CreateIsValidated", s.createIsValidated)
	t.Run("ListAndFilter", s.listAndFilter)
	t.Run("Get", s.get)
	t.Run("EveryMissIsTheSame404", s.notFound)
	t.Run("Patch", s.patch)
	t.Run("Complete", s.complete)
	t.Run("Uncomplete", s.uncomplete)
	t.Run("CrossListAndSummary", s.crossListAndSummary)
	t.Run("Delete", s.delete)
}

type taskFlowState struct {
	token string
	list  taskCalendar // kind tasks
	work  taskCalendar // kind events (no kind sent)

	passport, garage, stamps, rent, attic taskJSON
}

func (s *taskFlowState) createList(t *testing.T) {
	s.token = registerAndLogin(t, "tasks-crud@example.test", "tasksSecret!123", "Tasks CRUD")

	s.list = createTaskCalendar(t, s.token, "Errands", "tasks")
	assert.Equal(t, "tasks", s.list.Kind)
	assert.Equal(t, "VTODO", s.list.SupportedComponents)

	s.work = createTaskCalendar(t, s.token, "Work", "")
	assert.Equal(t, "events", s.work.Kind, "no kind is an event calendar")
	assert.Equal(t, "VEVENT", s.work.SupportedComponents)

	got := getTaskCalendar(t, s.token, s.list.UUID)
	assert.Equal(t, "tasks", got.Kind)
	var listed struct {
		Calendars []taskCalendar `json:"calendars"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/", s.token, nil, &listed))
	kinds := map[string]string{}
	for _, c := range listed.Calendars {
		kinds[c.UUID] = c.Kind
	}
	assert.Equal(t, "tasks", kinds[s.list.UUID])
	assert.Equal(t, "events", kinds[s.work.UUID])
}

func (s *taskFlowState) createTasks(t *testing.T) {
	s.passport = createTask(t, s.token, s.list.UUID, map[string]any{
		"summary": "Renew passport", "description": "Bring two photos",
		"due": "2030-11-05T17:00:00+01:00", "start": "2030-11-01T09:00:00+01:00",
		"priority": 3, "percent_complete": 0, "status": "needs-action", "timezone": "Europe/Berlin",
	})
	p := s.passport
	assert.NotEmpty(t, p.ID)
	assert.Equal(t, p.ID+"@calcard.io", p.UID)
	assert.Equal(t, s.list.UUID, p.CalendarUUID)
	assert.Equal(t, s.list.ID, p.CalendarID)
	assert.Equal(t, "Renew passport", p.Summary)
	assert.Equal(t, "Bring two photos", p.Description)
	assert.Equal(t, "needs-action", p.Status)
	assert.False(t, p.Completed)
	assert.Nil(t, p.CompletedAt)
	assert.Equal(t, intp(0), p.PercentComplete)
	assert.Equal(t, intp(3), p.Priority)
	assert.Equal(t, strp("2030-11-05T16:00:00Z"), p.Due, "due is answered in UTC")
	assert.Nil(t, p.DueDate)
	assert.False(t, p.DueAllDay)
	assert.Equal(t, strp("2030-11-01T08:00:00Z"), p.Start)
	assert.Nil(t, p.StartDate)
	assert.False(t, p.IsRecurring)
	assert.Nil(t, p.RRule)
	assert.NotEmpty(t, p.ETag)

	s.garage = createTask(t, s.token, s.list.UUID, map[string]any{
		"summary": "Clean the garage", "status": "in-process", "percent_complete": 40,
		"due": "2030-11-05T16:00:00Z", "priority": 5,
	})
	assert.Equal(t, "in-process", s.garage.Status)
	assert.Equal(t, intp(40), s.garage.PercentComplete)

	s.stamps = createTask(t, s.token, s.list.UUID, map[string]any{
		"summary": "Buy stamps", "due": "2030-11-05T16:00:00Z",
	})
	assert.Nil(t, s.stamps.Priority)

	s.rent = createTask(t, s.token, s.list.UUID, map[string]any{
		"summary": "Pay rent", "due": "2030-11-07", "due_all_day": true, "priority": 1,
	})
	assert.Nil(t, s.rent.Due, "an all-day due has no instant")
	assert.Equal(t, strp("2030-11-07"), s.rent.DueDate)
	assert.True(t, s.rent.DueAllDay)

	s.attic = createTask(t, s.token, s.list.UUID, map[string]any{"summary": "Sort the attic"})
	a := s.attic
	assert.Equal(t, "needs-action", a.Status)
	assert.Equal(t, "", a.Description)
	assert.Nil(t, a.Due)
	assert.Nil(t, a.DueDate)
	assert.Nil(t, a.Priority)
	assert.Nil(t, a.PercentComplete)
	assert.Nil(t, a.Start)
}

func (s *taskFlowState) createIsValidated(t *testing.T) {
	for _, tc := range []struct {
		name string
		body any
		want string
	}{
		{"blank summary", map[string]any{"summary": "   "}, "summary is required"},
		{"501 runes", map[string]any{"summary": strings.Repeat("ä", 501)}, "summary must be at most 500 characters"},
		{"due before start", map[string]any{"summary": "x", "due": "2030-11-01T09:00:00Z", "start": "2030-11-02T09:00:00Z"}, "due must not be before start"},
		{"priority 10", map[string]any{"summary": "x", "priority": 10}, "priority must be between 0 and 9"},
		{"percent 101", map[string]any{"summary": "x", "percent_complete": 101}, "percent_complete must be between 0 and 100"},
		{"unknown status", map[string]any{"summary": "x", "status": "done"}, invalidTaskStatus},
		{"all-day with a timed due", map[string]any{"summary": "x", "due": "2030-11-05T17:00:00Z", "due_all_day": true}, "due must be a date (YYYY-MM-DD) when due_all_day is true"},
		{"timed with a date due", map[string]any{"summary": "x", "due": "2030-11-05"}, "due must be an RFC 3339 date-time unless due_all_day is true"},
		{"broken JSON", `{"summary":`, "Invalid request body"},
		{"wrong type", map[string]any{"summary": 5}, "Invalid request body"},
	} {
		status, raw := restCall(t, http.MethodPost, tasksURL(s.list.UUID), s.token, tc.body)
		assertErrorBody(t, http.StatusBadRequest, tc.want, status, raw, tc.name)
	}

	status, raw := restCall(t, http.MethodPost, tasksURL(s.work.UUID), s.token, map[string]any{"summary": "Not here"})
	assertErrorBody(t, http.StatusBadRequest, tasksOnlyRefusal, status, raw, "an event calendar stores no tasks")

	assert.EqualValues(t, 5, taskPage(t, s.token, tasksURL(s.list.UUID)+"?status=all").Total, "no refused create stored anything")
	assert.JSONEq(t, emptyTaskPage, string(mustGet(t, s.token, tasksURL(s.work.UUID))))
}

// mustGet GETs path and requires a 200.
func mustGet(t *testing.T, token, path string) []byte {
	t.Helper()
	status, raw := restCall(t, http.MethodGet, path, token, nil)
	require.Equalf(t, http.StatusOK, status, "GET %s: %s", path, string(raw))
	return raw
}

func (s *taskFlowState) listAndFilter(t *testing.T) {
	ordered := []string{"Renew passport", "Clean the garage", "Buy stamps", "Pay rent", "Sort the attic"}

	page := taskPage(t, s.token, tasksURL(s.list.UUID))
	assert.Equal(t, ordered, taskSummaries(page),
		"open by default; due ascending (all-day at midnight UTC), then priority with none last, undated last")
	assert.EqualValues(t, 5, page.Total)
	assert.Equal(t, ordered, taskSummaries(taskPage(t, s.token, tasksURL(s.list.UUID)+"?status=all")))
	assert.JSONEq(t, emptyTaskPage, string(mustGet(t, s.token, tasksURL(s.list.UUID)+"?status=completed")))

	status, raw := restCall(t, http.MethodGet, tasksURL(s.list.UUID)+"?status=done", s.token, nil)
	assertErrorBody(t, http.StatusBadRequest, invalidStatusFilter, status, raw)

	assert.Equal(t, []string{"Renew passport"}, taskSummaries(taskPage(t, s.token, tasksURL(s.list.UUID)+"?q=PASSPORT")))
	assert.Equal(t, []string{"Renew passport"}, taskSummaries(taskPage(t, s.token, tasksURL(s.list.UUID)+"?q=photos")),
		"q matches the description too")

	first := taskPage(t, s.token, tasksURL(s.list.UUID)+"?limit=2")
	assert.Equal(t, ordered[:2], taskSummaries(first))
	assert.EqualValues(t, 5, first.Total)
	second := taskPage(t, s.token, tasksURL(s.list.UUID)+"?limit=2&offset=2")
	assert.Equal(t, ordered[2:4], taskSummaries(second))
	assert.EqualValues(t, 5, second.Total)

	for i, task := range page.Tasks {
		want := []taskJSON{s.passport, s.garage, s.stamps, s.rent, s.attic}[i]
		assertSameTask(t, want, task, "list item %d equals its create answer", i)
	}
}

func (s *taskFlowState) get(t *testing.T) {
	for _, want := range []taskJSON{s.passport, s.garage, s.stamps, s.rent, s.attic} {
		assertSameTask(t, want, getTask(t, s.token, s.list.UUID, want.ID), want.Summary)
	}
}

func (s *taskFlowState) notFound(t *testing.T) {
	status, unknownTask := restCall(t, http.MethodGet, taskURL(s.list.UUID, uuid.NewString()), s.token, nil)
	assertErrorBody(t, http.StatusNotFound, "Task not found", status, unknownTask)

	var event struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendars/"+s.work.UUID+"/events/", s.token, map[string]any{
		"summary": "Standup", "start": "2030-11-05T09:00:00Z", "end": "2030-11-05T09:15:00Z",
	}, &event))

	for _, tc := range []struct{ name, calUUID, taskID string }{
		{"a task under another of the caller's calendars", s.work.UUID, s.passport.ID},
		{"a task under the all-zero calendar", zeroUUID, s.passport.ID},
		{"a task under an unknown calendar", uuid.NewString(), s.passport.ID},
		{"an event's UUID under its own calendar", s.work.UUID, event.ID},
		{"an event's UUID under the task list", s.list.UUID, event.ID},
	} {
		for _, req := range []struct {
			method, suffix string
			body           any
		}{
			{http.MethodGet, "", nil},
			{http.MethodPatch, "", map[string]any{"summary": "hijacked"}},
			{http.MethodDelete, "", nil},
			{http.MethodPost, "/complete", nil},
			{http.MethodPost, "/uncomplete", nil},
		} {
			status, raw := restCall(t, req.method, taskURL(tc.calUUID, tc.taskID)+req.suffix, s.token, req.body)
			assert.Equal(t, http.StatusNotFound, status, "%s: %s%s", tc.name, req.method, req.suffix)
			assert.JSONEq(t, string(unknownTask), string(raw), "%s: %s%s looks exactly like an unknown task", tc.name, req.method, req.suffix)
		}
	}

	status, raw := restCall(t, http.MethodGet, tasksURL(uuid.NewString()), s.token, nil)
	assertErrorBody(t, http.StatusNotFound, "Calendar not found", status, raw, "list route of an unknown calendar")
	status, raw = restCall(t, http.MethodPost, tasksURL(zeroUUID), s.token, map[string]any{"summary": "x"})
	assertErrorBody(t, http.StatusNotFound, "Calendar not found", status, raw, "create in an unknown calendar")

	assertSameTask(t, s.passport, getTask(t, s.token, s.list.UUID, s.passport.ID), "no miss changed the task")
}

func (s *taskFlowState) patch(t *testing.T) {
	before := s.passport

	status, raw := apiCall(t, http.MethodPatch, taskURL(s.list.UUID, before.ID), s.token, map[string]any{
		"summary": "Renew passport and ID card", "description": nil, "priority": nil, "due": "2030-11-06T10:00:00Z",
	}, ifMatch(before.ETag))
	require.Equalf(t, http.StatusOK, status, "patch: %s", string(raw))
	s.passport = decodeTask(t, raw)
	p := s.passport
	assert.Equal(t, "Renew passport and ID card", p.Summary)
	assert.Equal(t, "", p.Description, "null removes the description")
	assert.Nil(t, p.Priority, "null removes the priority")
	assert.Equal(t, strp("2030-11-06T10:00:00Z"), p.Due)
	assert.Equal(t, before.Start, p.Start, "an absent field is kept")
	assert.Equal(t, before.PercentComplete, p.PercentComplete)
	assert.Equal(t, before.ID, p.ID)
	assert.NotEqual(t, before.ETag, p.ETag, "a write gets a new ETag")
	assertSameTask(t, p, getTask(t, s.token, s.list.UUID, p.ID))

	status, raw = apiCall(t, http.MethodPatch, taskURL(s.list.UUID, p.ID), s.token, map[string]any{}, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Equal(t, p.ETag, decodeTask(t, raw).ETag, "an empty PATCH writes nothing")

	status, raw = apiCall(t, http.MethodPatch, taskURL(s.list.UUID, p.ID), s.token,
		map[string]any{"summary": "stale"}, ifMatch(before.ETag))
	assertErrorBody(t, http.StatusPreconditionFailed, "ETag precondition failed", status, raw)

	for _, tc := range []struct {
		name string
		body any
		want string
	}{
		{"priority 12", map[string]any{"priority": 12}, "priority must be between 0 and 9"},
		{"unknown status", map[string]any{"status": "done"}, invalidTaskStatus},
		{"blank summary", map[string]any{"summary": ""}, "summary is required"},
		{"due before the stored start", map[string]any{"due": "2030-10-01T00:00:00Z"}, "due must not be before start"},
		{"broken JSON", "not json", "Invalid request body"},
	} {
		status, raw := restCall(t, http.MethodPatch, taskURL(s.list.UUID, p.ID), s.token, tc.body)
		assertErrorBody(t, http.StatusBadRequest, tc.want, status, raw, tc.name)
	}
	assertSameTask(t, p, getTask(t, s.token, s.list.UUID, p.ID), "no refused PATCH changed the task")
}

func (s *taskFlowState) complete(t *testing.T) {
	before := s.passport
	startedAt := time.Now()

	status, raw := apiCall(t, http.MethodPost, taskURL(s.list.UUID, before.ID)+"/complete", s.token, nil, ifMatch(before.ETag))
	require.Equalf(t, http.StatusOK, status, "complete: %s", string(raw))
	done := decodeTask(t, raw)
	assert.True(t, done.Completed)
	assert.Equal(t, "completed", done.Status)
	assert.Equal(t, intp(100), done.PercentComplete)
	require.NotNil(t, done.CompletedAt)
	assert.True(t, strings.HasSuffix(*done.CompletedAt, "Z"), "completed_at is UTC: %s", *done.CompletedAt)
	completedAt, err := time.Parse(time.RFC3339Nano, *done.CompletedAt)
	require.NoError(t, err)
	assert.WithinRange(t, completedAt, startedAt.Add(-2*time.Second), time.Now().Add(2*time.Second))
	assert.NotEqual(t, before.ETag, done.ETag)
	s.passport = done

	// Idempotent: no change, same ETag, a body is ignored.
	status, raw = restCall(t, http.MethodPost, taskURL(s.list.UUID, done.ID)+"/complete", s.token, map[string]any{"anything": 1})
	require.Equal(t, http.StatusOK, status, string(raw))
	assertSameTask(t, done, decodeTask(t, raw), "completing a completed task changes nothing")

	status, raw = apiCall(t, http.MethodPost, taskURL(s.list.UUID, done.ID)+"/complete", s.token, nil, ifMatch(before.ETag))
	assertErrorBody(t, http.StatusPreconditionFailed, "ETag precondition failed", status, raw)

	status, raw = restCall(t, http.MethodPost, taskURL(s.list.UUID, s.garage.ID)+"/complete", s.token, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	s.garage = decodeTask(t, raw)
	assert.Equal(t, intp(100), s.garage.PercentComplete, "40% becomes 100% on completion")

	assert.Equal(t, []string{"Clean the garage", "Renew passport and ID card"},
		taskSummaries(taskPage(t, s.token, tasksURL(s.list.UUID)+"?status=completed")),
		"the garage is due on 5 Nov, the passport (patched) on 6 Nov")
	assert.Equal(t, []string{"Buy stamps", "Pay rent", "Sort the attic"},
		taskSummaries(taskPage(t, s.token, tasksURL(s.list.UUID))))
}

func (s *taskFlowState) uncomplete(t *testing.T) {
	status, raw := restCall(t, http.MethodPost, taskURL(s.list.UUID, s.passport.ID)+"/uncomplete", s.token, nil)
	require.Equalf(t, http.StatusOK, status, "uncomplete: %s", string(raw))
	open := decodeTask(t, raw)
	assert.Equal(t, "needs-action", open.Status)
	assert.False(t, open.Completed)
	assert.Nil(t, open.CompletedAt)
	assert.Equal(t, intp(0), open.PercentComplete, "100% goes back to 0")
	assert.NotEqual(t, s.passport.ETag, open.ETag)
	s.passport = open

	status, raw = restCall(t, http.MethodPost, taskURL(s.list.UUID, open.ID)+"/uncomplete", s.token, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assertSameTask(t, open, decodeTask(t, raw), "uncompleting an open task changes nothing")

	status, raw = restCall(t, http.MethodPost, taskURL(s.list.UUID, s.garage.ID)+"/uncomplete", s.token, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	s.garage = decodeTask(t, raw)
	assert.Equal(t, intp(0), s.garage.PercentComplete, "the 40% from before completing is not restored")
	assert.Equal(t, "needs-action", s.garage.Status)

	status, raw = restCall(t, http.MethodPost, taskURL(s.list.UUID, s.stamps.ID)+"/uncomplete", s.token, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assertSameTask(t, s.stamps, decodeTask(t, raw), "a task that was never completed is left alone")
}

func (s *taskFlowState) crossListAndSummary(t *testing.T) {
	status, raw := restCall(t, http.MethodPatch, taskURL(s.list.UUID, s.attic.ID), s.token, map[string]any{"status": "cancelled"})
	require.Equal(t, http.StatusOK, status, string(raw))
	s.attic = decodeTask(t, raw)
	assert.Equal(t, "cancelled", s.attic.Status)
	assert.False(t, s.attic.Completed)

	all := taskPage(t, s.token, "/tasks?status=all")
	assert.EqualValues(t, 5, all.Total, "only this list holds tasks of this user")
	byID := map[string]taskJSON{}
	for _, task := range all.Tasks {
		assert.Equal(t, "Errands", task.CalendarName)
		assert.Equal(t, "#16a34a", task.CalendarColor)
		task.CalendarName, task.CalendarColor = "", ""
		byID[task.ID] = task
	}
	for _, want := range []taskJSON{s.passport, s.garage, s.stamps, s.rent, s.attic} {
		got, ok := byID[want.ID]
		if assert.True(t, ok, "%s is listed by GET /tasks", want.Summary) {
			assertSameTask(t, want, got, want.Summary)
		}
	}

	assert.Equal(t, 5, taskPage(t, s.token, "/tasks?status=all&calendar_uuid="+s.list.UUID).Count)
	assert.JSONEq(t, emptyTaskPage, string(mustGet(t, s.token, "/tasks?calendar_uuid="+s.work.UUID)),
		"a readable calendar without tasks is an empty page")
	status, raw = restCall(t, http.MethodGet, "/tasks?calendar_uuid="+uuid.NewString(), s.token, nil)
	assertErrorBody(t, http.StatusNotFound, "Calendar not found", status, raw)

	window := "/tasks?" + url.Values{"status": {"all"}, "due_after": {"2030-11-06T00:00:00Z"}, "due_before": {"2030-11-08T00:00:00Z"}}.Encode()
	assert.Equal(t, []string{"Renew passport and ID card", "Pay rent"}, taskSummaries(taskPage(t, s.token, window)))
	assert.Equal(t, []string{"Sort the attic"}, taskSummaries(taskPage(t, s.token, "/tasks?status=completed")),
		"cancelled is done")

	// as_of 2030-11-06 12:00 at +01:00: passport (due 10:00Z that day) is
	// overdue and due today; garage and stamps (5 Nov) are overdue; rent
	// (all-day 7 Nov) is neither; attic is cancelled, so done.
	sum := summaryAsOf(t, s.token, "2030-11-06T12:00:00+01:00")
	assert.EqualValues(t, 4, sum.Open)
	assert.EqualValues(t, 3, sum.Overdue)
	assert.EqualValues(t, 1, sum.DueToday)
	assert.EqualValues(t, 1, sum.Completed)
	if assert.Contains(t, sum.ByCalendar, s.list.UUID) {
		assert.EqualValues(t, 4, sum.ByCalendar[s.list.UUID].Open)
		assert.EqualValues(t, 3, sum.ByCalendar[s.list.UUID].Overdue)
	}
	assert.NotContains(t, sum.ByCalendar, s.work.UUID, "an event calendar is not a task list")
	assert.EqualValues(t, sum.Open, taskPage(t, s.token, "/tasks").Total, "open agrees with the list")
	assert.EqualValues(t, sum.Completed, taskPage(t, s.token, "/tasks?status=completed").Total)

	status, raw = restCall(t, http.MethodGet, "/tasks/summary", s.token, nil)
	assertErrorBody(t, http.StatusBadRequest, "Query parameter 'as_of' is required", status, raw)
	status, raw = restCall(t, http.MethodGet, "/tasks/summary?as_of=2030-11-06T12:00:00+01:00", s.token, nil)
	assertErrorBody(t, http.StatusBadRequest, "Invalid 'as_of' value", status, raw, "an unencoded + arrives as a space")
	status, raw = restCall(t, http.MethodGet, "/tasks", "", nil)
	assertErrorBody(t, http.StatusUnauthorized, "missing authentication token", status, raw)
}

func (s *taskFlowState) delete(t *testing.T) {
	status, raw := apiCall(t, http.MethodDelete, taskURL(s.list.UUID, s.rent.ID), s.token, nil, ifMatch(s.passport.ETag))
	assertErrorBody(t, http.StatusPreconditionFailed, "ETag precondition failed", status, raw, "another task's ETag")
	getTask(t, s.token, s.list.UUID, s.rent.ID)

	status, raw = apiCall(t, http.MethodDelete, taskURL(s.list.UUID, s.rent.ID), s.token, nil, ifMatch(s.rent.ETag))
	require.Equal(t, http.StatusNoContent, status, string(raw))
	assert.Empty(t, raw)

	status, raw = restCall(t, http.MethodGet, taskURL(s.list.UUID, s.rent.ID), s.token, nil)
	assertErrorBody(t, http.StatusNotFound, "Task not found", status, raw)
	status, raw = restCall(t, http.MethodDelete, taskURL(s.list.UUID, s.rent.ID), s.token, nil)
	assertErrorBody(t, http.StatusNotFound, "Task not found", status, raw, "a second DELETE")
	assert.NotContains(t, taskSummaries(taskPage(t, s.token, tasksURL(s.list.UUID)+"?status=all")), "Pay rent")

	// A task DELETE is a hard delete: no soft-deleted row stays behind (a 404
	// cannot tell the two apart, so ask the database).
	var rows int64
	require.NoError(t, testDB.Unscoped().Model(&calendar.CalendarObject{}).Where("uuid = ?", s.rent.ID).Count(&rows).Error)
	assert.Zero(t, rows, "the deleted task's row is gone for good")
}

// ----------------------------------------------------------------------------

// TestTaskListSharing: a read-write sharee reads and writes a shared task
// list, a read sharee reads it and every write is 403, an outsider gets the
// same 404 as for a list or task that does not exist, and the cross-list
// routes follow the shares. A subscribed calendar is read-only to its own
// owner, tasks included.
func TestTaskListSharing(t *testing.T) {
	owner := registerAndLogin(t, "task-share-owner@example.test", "shareSecret!123", "Task Owner")
	rwEmail, roEmail := "task-share-rw@example.test", "task-share-ro@example.test"
	rw := registerAndLogin(t, rwEmail, "shareSecret!123", "Task Read-Write")
	ro := registerAndLogin(t, roEmail, "shareSecret!123", "Task Read-Only")
	outsider := registerAndLogin(t, "task-share-out@example.test", "shareSecret!123", "Task Outsider")

	list := createTaskCalendar(t, owner, "Household", "tasks")
	shareCalendarWith(t, owner, list.UUID, rwEmail, "read-write")
	shareCalendarWith(t, owner, list.UUID, roEmail, "read")

	plants := createTask(t, owner, list.UUID, map[string]any{"summary": "Water the plants", "due": "2030-11-04T08:00:00Z"})
	bike := createTask(t, owner, list.UUID, map[string]any{"summary": "Fix the bike"})

	t.Run("read-write sharee reads and writes", func(t *testing.T) {
		assert.Equal(t, []string{"Water the plants", "Fix the bike"}, taskSummaries(taskPage(t, rw, tasksURL(list.UUID))))
		assertSameTask(t, plants, getTask(t, rw, list.UUID, plants.ID))

		status, raw := apiCall(t, http.MethodPost, taskURL(list.UUID, plants.ID)+"/complete", rw, nil, ifMatch(plants.ETag))
		require.Equal(t, http.StatusOK, status, string(raw))
		plants = decodeTask(t, raw)
		assert.True(t, plants.Completed)
		assertSameTask(t, plants, getTask(t, owner, list.UUID, plants.ID), "the owner sees the sharee's completion")

		status, raw = restCall(t, http.MethodPatch, taskURL(list.UUID, bike.ID), rw, map[string]any{"summary": "Fix the bike brakes"})
		require.Equal(t, http.StatusOK, status, string(raw))
		bike = decodeTask(t, raw)
		assert.Equal(t, "Fix the bike brakes", bike.Summary)

		extra := createTask(t, rw, list.UUID, map[string]any{"summary": "Sharee's task"})
		assert.Equal(t, list.UUID, extra.CalendarUUID)
		status, raw = restCall(t, http.MethodDelete, taskURL(list.UUID, extra.ID), rw, nil)
		require.Equal(t, http.StatusNoContent, status, string(raw))
	})

	t.Run("read sharee reads but every write is 403", func(t *testing.T) {
		assert.Equal(t, []string{"Fix the bike brakes"}, taskSummaries(taskPage(t, ro, tasksURL(list.UUID))))
		assertSameTask(t, bike, getTask(t, ro, list.UUID, bike.ID))

		for _, tc := range []struct {
			name, method, path string
			body               any
			headers            map[string]string
		}{
			{"complete", http.MethodPost, taskURL(list.UUID, bike.ID) + "/complete", nil, nil},
			{"complete with a stale If-Match", http.MethodPost, taskURL(list.UUID, bike.ID) + "/complete", nil, ifMatch("stale")},
			{"uncomplete", http.MethodPost, taskURL(list.UUID, plants.ID) + "/uncomplete", nil, nil},
			{"patch", http.MethodPatch, taskURL(list.UUID, bike.ID), map[string]any{"summary": "nope"}, nil},
			{"delete", http.MethodDelete, taskURL(list.UUID, bike.ID), nil, nil},
			{"create", http.MethodPost, tasksURL(list.UUID), map[string]any{"summary": "nope"}, nil},
		} {
			status, raw := apiCall(t, tc.method, tc.path, ro, tc.body, tc.headers)
			assertErrorBody(t, http.StatusForbidden, readOnlyMessage, status, raw, tc.name)
		}
		assertSameTask(t, bike, getTask(t, owner, list.UUID, bike.ID), "nothing changed")
		assertSameTask(t, plants, getTask(t, owner, list.UUID, plants.ID), "nothing changed")
		assert.EqualValues(t, 2, taskPage(t, owner, tasksURL(list.UUID)+"?status=all").Total)
	})

	t.Run("outsider gets the 404 of something that does not exist", func(t *testing.T) {
		status, unknownTask := restCall(t, http.MethodGet, taskURL(uuid.NewString(), uuid.NewString()), outsider, nil)
		assertErrorBody(t, http.StatusNotFound, "Task not found", status, unknownTask)
		status, unknownList := restCall(t, http.MethodGet, tasksURL(uuid.NewString()), outsider, nil)
		assertErrorBody(t, http.StatusNotFound, "Calendar not found", status, unknownList)

		for _, tc := range []struct {
			name, method, path string
			body               any
			want               []byte
		}{
			{"get", http.MethodGet, taskURL(list.UUID, bike.ID), nil, unknownTask},
			{"patch", http.MethodPatch, taskURL(list.UUID, bike.ID), map[string]any{"summary": "x"}, unknownTask},
			{"patch with a broken body", http.MethodPatch, taskURL(list.UUID, bike.ID), "not json", unknownTask},
			{"delete", http.MethodDelete, taskURL(list.UUID, bike.ID), nil, unknownTask},
			{"complete", http.MethodPost, taskURL(list.UUID, bike.ID) + "/complete", nil, unknownTask},
			{"uncomplete", http.MethodPost, taskURL(list.UUID, plants.ID) + "/uncomplete", nil, unknownTask},
			{"list", http.MethodGet, tasksURL(list.UUID), nil, unknownList},
			{"list with a bad status", http.MethodGet, tasksURL(list.UUID) + "?status=bogus", nil, unknownList},
			{"create", http.MethodPost, tasksURL(list.UUID), map[string]any{"summary": "x"}, unknownList},
		} {
			status, raw := restCall(t, tc.method, tc.path, outsider, tc.body)
			assert.Equal(t, http.StatusNotFound, status, tc.name)
			assert.JSONEq(t, string(tc.want), string(raw), tc.name)
		}
		assertSameTask(t, bike, getTask(t, owner, list.UUID, bike.ID), "nothing changed")
	})

	t.Run("cross-list routes follow the shares", func(t *testing.T) {
		for name, token := range map[string]string{"read-write sharee": rw, "read sharee": ro} {
			page := taskPage(t, token, "/tasks?status=all")
			got := map[string]string{}
			for _, task := range page.Tasks {
				got[task.ID] = task.CalendarName
			}
			assert.Equal(t, map[string]string{plants.ID: "Household", bike.ID: "Household"}, got, name)
			assert.Contains(t, summaryAsOf(t, token, "2030-11-01T12:00:00Z").ByCalendar, list.UUID, name)
		}

		assert.JSONEq(t, emptyTaskPage, string(mustGet(t, outsider, "/tasks?status=all")))
		status, raw := restCall(t, http.MethodGet, "/tasks?calendar_uuid="+list.UUID, outsider, nil)
		assertErrorBody(t, http.StatusNotFound, "Calendar not found", status, raw, "an unreadable list is a 404, never an empty page")
		assert.NotContains(t, summaryAsOf(t, outsider, "2030-11-01T12:00:00Z").ByCalendar, list.UUID)
	})

	t.Run("a subscribed list is read-only to its owner", func(t *testing.T) {
		feed := newTestFeed(t)
		feed.set("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Test//EN\r\nX-WR-CALNAME:Chores feed\r\n" +
			icsEvent("feed-event@feed.test", "Feed event", "20301101T100000Z") +
			ics("BEGIN:VTODO", "UID:feed-task@feed.test", "DTSTAMP:20300101T000000Z",
				"DUE:20301110T090000Z", "SUMMARY:Feed chore", "STATUS:NEEDS-ACTION", "END:VTODO") +
			"END:VCALENDAR\r\n")
		var sub subscriptionPayload
		require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendar-subscriptions/", owner,
			map[string]any{"url": feed.URL, "refresh_interval": "1h"}, &sub))

		page := taskPage(t, owner, tasksURL(sub.CalendarID))
		require.Len(t, page.Tasks, 1)
		chore := page.Tasks[0]
		assert.Equal(t, "Feed chore", chore.Summary)
		assert.Equal(t, strp("2030-11-10T09:00:00Z"), chore.Due)

		for _, tc := range []struct {
			name, method, path string
			body               any
		}{
			{"complete", http.MethodPost, taskURL(sub.CalendarID, chore.ID) + "/complete", nil},
			{"patch", http.MethodPatch, taskURL(sub.CalendarID, chore.ID), map[string]any{"summary": "x"}},
			{"delete", http.MethodDelete, taskURL(sub.CalendarID, chore.ID), nil},
			{"create", http.MethodPost, tasksURL(sub.CalendarID), map[string]any{"summary": "x"}},
		} {
			status, raw := restCall(t, tc.method, tc.path, owner, tc.body)
			assertErrorBody(t, http.StatusForbidden, readOnlyMessage, status, raw, tc.name)
		}
		assertSameTask(t, chore, getTask(t, owner, sub.CalendarID, chore.ID), "nothing changed")
	})
}

// ----------------------------------------------------------------------------

// TestTasksRESTDAVRoundTrip: what a DAV client writes, REST reads with the
// right status/due/priority/percent/completed — also after an update PUT —
// and what REST writes, DAV serves through GET, calendar-query and
// sync-collection, with the same ETag on both sides.
func TestTasksRESTDAVRoundTrip(t *testing.T) {
	email := "task-roundtrip@example.test"
	token, username := registerAndLoginFull(t, email, "roundtripSecret!123", "Task Round Trip")
	_, appPass := createAppPassword(t, token, "task-roundtrip")
	list := createTaskCalendar(t, token, "Phone reminders", "tasks")
	collection := "/dav/" + username + "/calendars/" + list.Path + "/"

	openTodo := func(status, extra string) string {
		return vcalendar(berlinVTimezone, ics(
			"BEGIN:VTODO", "UID:rt-open@phone", "DTSTAMP:20301101T080000Z",
			"DTSTART:20301101T080000Z", "DUE;TZID=Europe/Berlin:20301105T170000",
			"SUMMARY:Call the plumber", "DESCRIPTION:Kitchen sink",
			"STATUS:"+status, extra, "END:VTODO"))
	}
	davPut(t, collection+"rt-open.ics", email, appPass, openTodo("IN-PROCESS", "PRIORITY:1\r\nPERCENT-COMPLETE:40"))
	davPut(t, collection+"rt-done.ics", email, appPass, vcalendar(ics(
		"BEGIN:VTODO", "UID:rt-done@phone", "DTSTAMP:20301101T080000Z",
		"DUE;VALUE=DATE:20301104", "SUMMARY:Return the library books",
		"STATUS:COMPLETED", "COMPLETED:20301101T080000Z", "PERCENT-COMPLETE:100", "PRIORITY:9",
		"END:VTODO")))

	t.Run("a DAV-written VTODO is listed with its fields", func(t *testing.T) {
		open := taskPage(t, token, tasksURL(list.UUID))
		require.Len(t, open.Tasks, 1)
		o := open.Tasks[0]
		assert.Equal(t, "rt-open@phone", o.UID)
		assert.Equal(t, "Call the plumber", o.Summary)
		assert.Equal(t, "Kitchen sink", o.Description)
		assert.Equal(t, "in-process", o.Status)
		assert.False(t, o.Completed)
		assert.Nil(t, o.CompletedAt)
		assert.Equal(t, strp("2030-11-05T16:00:00Z"), o.Due, "a TZID due is answered in UTC")
		assert.False(t, o.DueAllDay)
		assert.Equal(t, strp("2030-11-01T08:00:00Z"), o.Start)
		assert.Equal(t, intp(1), o.Priority)
		assert.Equal(t, intp(40), o.PercentComplete)
		assert.Equal(t, list.UUID, o.CalendarUUID)

		done := taskPage(t, token, tasksURL(list.UUID)+"?status=completed")
		require.Len(t, done.Tasks, 1)
		d := done.Tasks[0]
		assert.Equal(t, "rt-done@phone", d.UID)
		assert.Equal(t, "completed", d.Status)
		assert.True(t, d.Completed)
		assert.Equal(t, strp("2030-11-01T08:00:00Z"), d.CompletedAt)
		assert.Equal(t, intp(100), d.PercentComplete)
		assert.Equal(t, intp(9), d.Priority)
		assert.Nil(t, d.Due)
		assert.Equal(t, strp("2030-11-04"), d.DueDate)
		assert.True(t, d.DueAllDay)

		for _, want := range []taskJSON{o, d} {
			assertSameTask(t, want, getTask(t, token, list.UUID, want.ID), want.UID)
		}

		all := taskPage(t, token, "/tasks?status=all")
		uids := map[string]string{}
		for _, task := range all.Tasks {
			uids[task.UID] = task.CalendarName
		}
		assert.Equal(t, map[string]string{"rt-open@phone": "Phone reminders", "rt-done@phone": "Phone reminders"}, uids,
			"GET /tasks lists both")

		_, davETag := davGet(t, collection+"rt-open.ics", email, appPass)
		assert.Equal(t, `"`+o.ETag+`"`, davETag, "REST and DAV report the same ETag")
	})

	t.Run("an update PUT is picked up", func(t *testing.T) {
		before := findTaskByUID(t, token, list.UUID, "rt-open@phone")
		davPut(t, collection+"rt-open.ics", email, appPass,
			openTodo("COMPLETED", "COMPLETED:20301103T120000Z\r\nPERCENT-COMPLETE:100\r\nPRIORITY:5"))

		after := getTask(t, token, list.UUID, before.ID)
		assert.Equal(t, before.ID, after.ID, "the REST id survives a PUT")
		assert.Equal(t, "completed", after.Status)
		assert.True(t, after.Completed)
		assert.Equal(t, strp("2030-11-03T12:00:00Z"), after.CompletedAt)
		assert.Equal(t, intp(100), after.PercentComplete)
		assert.Equal(t, intp(5), after.Priority)
		assert.NotEqual(t, before.ETag, after.ETag)
		_, davETag := davGet(t, collection+"rt-open.ics", email, appPass)
		assert.Equal(t, `"`+after.ETag+`"`, davETag)
		assert.JSONEq(t, emptyTaskPage, string(mustGet(t, token, tasksURL(list.UUID))), "no open task left")
	})

	t.Run("a REST-created task is served over DAV", func(t *testing.T) {
		tok := initialSyncToken(t, collection, email, appPass)

		created := createTask(t, token, list.UUID, map[string]any{
			"summary": "Book flights", "due": "2030-12-01T09:30:00+01:00", "timezone": "Europe/Berlin", "priority": 2,
		})
		href := created.ID + ".ics"
		object := collection + href

		body, davETag := davGet(t, object, email, appPass)
		assert.Equal(t, `"`+created.ETag+`"`, davETag)
		lines := icalLines(body)
		for _, want := range []string{
			"BEGIN:VTODO", "UID:" + created.UID, "SUMMARY:Book flights",
			"DUE;TZID=Europe/Berlin:20301201T093000", "PRIORITY:2", "STATUS:NEEDS-ACTION", "SEQUENCE:0",
			"BEGIN:VTIMEZONE", "TZID:Europe/Berlin",
		} {
			assert.Contains(t, lines, want, "DAV GET of the REST-created task")
		}

		status, _, query := davCall(t, "REPORT", collection, email, appPass, vtodoQueryBody, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "VTODO calendar-query: %s", string(query))
		assert.Contains(t, string(query), href, "a VTODO calendar-query returns the REST-created task")
		assert.Contains(t, string(query), "UID:"+created.UID)
		status, _, query = davCall(t, "REPORT", collection, email, appPass, veventQueryBody, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "VEVENT calendar-query: %s", string(query))
		assert.NotContains(t, string(query), href, "a VEVENT calendar-query does not")

		changes, tok := syncChanges(t, collection, email, appPass, tok)
		assert.Equal(t, map[string]bool{href: false}, changes, "sync-collection delivers the created task")

		status, raw := restCall(t, http.MethodPost, taskURL(list.UUID, created.ID)+"/complete", token, nil)
		require.Equal(t, http.StatusOK, status, string(raw))
		completed := decodeTask(t, raw)
		changes, tok = syncChanges(t, collection, email, appPass, tok)
		assert.Equal(t, map[string]bool{href: false}, changes, "sync-collection delivers the completion")
		body, davETag = davGet(t, object, email, appPass)
		assert.Equal(t, `"`+completed.ETag+`"`, davETag)
		assert.Contains(t, icalLines(body), "STATUS:COMPLETED")
		assert.Contains(t, icalLines(body), "PERCENT-COMPLETE:100")

		status, raw = restCall(t, http.MethodDelete, taskURL(list.UUID, created.ID), token, nil)
		require.Equal(t, http.StatusNoContent, status, string(raw))
		changes, tok = syncChanges(t, collection, email, appPass, tok)
		assert.Equal(t, map[string]bool{href: true}, changes, "sync-collection reports the (hard) deletion as a 404")
		status, _, _ = davCall(t, "GET", object, email, appPass, "", nil)
		assert.Equal(t, http.StatusNotFound, status)
		var rows int64
		require.NoError(t, testDB.Unscoped().Model(&calendar.CalendarObject{}).Where("uuid = ?", created.ID).Count(&rows).Error)
		assert.Zero(t, rows, "no tombstone row: the change log alone carries the removal")
		changes, _ = syncChanges(t, collection, email, appPass, tok)
		assert.Empty(t, changes, "the removal is reported once")
	})

	t.Run("a DAV client's DELETE of a task is a hard delete", func(t *testing.T) {
		// A phone deletes a task: the same hard delete as the REST DELETE — no
		// tombstone row, the removal reported once by sync, then 404 everywhere.
		done := findTaskByUID(t, token, list.UUID, "rt-done@phone")
		object := collection + "rt-done.ics"
		tok := initialSyncToken(t, collection, email, appPass)

		status, _, body := davCall(t, "DELETE", object, email, appPass, "", nil)
		require.Equalf(t, http.StatusNoContent, status, "DAV DELETE: %s", string(body))

		var rows int64
		require.NoError(t, testDB.Unscoped().Model(&calendar.CalendarObject{}).Where("uuid = ?", done.ID).Count(&rows).Error)
		assert.Zero(t, rows, "no tombstone row: the change log alone carries the removal")

		changes, tok := syncChanges(t, collection, email, appPass, tok)
		assert.Equal(t, map[string]bool{"rt-done.ics": true}, changes, "sync-collection reports the deletion as a 404")

		status, raw := restCall(t, http.MethodGet, taskURL(list.UUID, done.ID), token, nil)
		assertErrorBody(t, http.StatusNotFound, "Task not found", status, raw, "REST no longer knows the task")
		status, _, _ = davCall(t, "GET", object, email, appPass, "", nil)
		assert.Equal(t, http.StatusNotFound, status)
		status, _, _ = davCall(t, "DELETE", object, email, appPass, "", nil)
		assert.Equal(t, http.StatusNotFound, status, "a second DAV DELETE")

		changes, _ = syncChanges(t, collection, email, appPass, tok)
		assert.Empty(t, changes, "the removal is reported once; the second DELETE recorded nothing")
		assert.Equal(t, []string{"Call the plumber"}, taskSummaries(taskPage(t, token, tasksURL(list.UUID)+"?status=all")),
			"the other task of the list is untouched")
	})
}

// ----------------------------------------------------------------------------

// TestTaskPreservesForeignProperties: a phone writes a repeating VTODO with an
// alarm, a parent link and X- properties. Completing it over REST (complete,
// or PATCH status/percent) is a 409 that leaves the object byte-identical; a
// REST edit of its title, notes, priority and due date changes exactly those
// lines plus SEQUENCE/LAST-MODIFIED/DTSTAMP and keeps every other line, the
// VALARM block included, byte for byte (compared through DAV GET).
func TestTaskPreservesForeignProperties(t *testing.T) {
	email := "task-foreign@example.test"
	token, username := registerAndLoginFull(t, email, "foreignSecret!123", "Task Foreign Props")
	_, appPass := createAppPassword(t, token, "task-foreign")
	list := createTaskCalendar(t, token, "Garden", "tasks")
	object := "/dav/" + username + "/calendars/" + list.Path + "/water-plants.ics"

	foreign := []string{
		"RRULE:FREQ=WEEKLY;BYDAY=MO",
		"RELATED-TO;RELTYPE=PARENT:garden-project@phone",
		"X-APPLE-SORT-ORDER:731492317",
		"X-CALCARD-TEST;X-PARAM=kept:client value",
	}
	alarm := []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Reminder", "TRIGGER:-PT15M", "END:VALARM"}
	vtodo := append([]string{
		"BEGIN:VTODO", "UID:water-plants@phone", "DTSTAMP:20301101T080000Z",
		"CREATED:20301101T080000Z", "LAST-MODIFIED:20301101T080000Z", "SEQUENCE:2",
		"SUMMARY:Water the plants", "DESCRIPTION:Balcony first", "PRIORITY:5",
		"DTSTART:20301104T080000Z", "DUE:20301104T090000Z", "STATUS:NEEDS-ACTION",
	}, foreign...)
	vtodo = append(append(vtodo, alarm...), "END:VTODO")
	davPut(t, object, email, appPass, vcalendar(ics(vtodo...)))

	task := findTaskByUID(t, token, list.UUID, "water-plants@phone")
	require.True(t, task.IsRecurring)
	require.Equal(t, strp("FREQ=WEEKLY;BYDAY=MO"), task.RRule)

	t.Run("completing a repeating task is refused and changes nothing", func(t *testing.T) {
		beforeBody, beforeETag := davGet(t, object, email, appPass)

		for _, tc := range []struct {
			name, method, suffix string
			body                 any
		}{
			{"PATCH status completed", http.MethodPatch, "", map[string]any{"status": "completed"}},
			{"PATCH status cancelled", http.MethodPatch, "", map[string]any{"status": "cancelled"}},
			{"PATCH percent_complete 100", http.MethodPatch, "", map[string]any{"percent_complete": 100}},
			{"POST complete", http.MethodPost, "/complete", nil},
		} {
			status, raw := restCall(t, tc.method, taskURL(list.UUID, task.ID)+tc.suffix, token, tc.body)
			assertErrorBody(t, http.StatusConflict, recurringRefusal, status, raw, tc.name)
		}

		afterBody, afterETag := davGet(t, object, email, appPass)
		assert.Equal(t, beforeBody, afterBody, "the object is byte-identical")
		assert.Equal(t, beforeETag, afterETag)
		assertSameTask(t, task, getTask(t, token, list.UUID, task.ID))
	})

	t.Run("a REST edit keeps VALARM, RELATED-TO, RRULE and X- properties", func(t *testing.T) {
		beforeBody, _ := davGet(t, object, email, appPass)
		before := icalLines(beforeBody)
		for _, line := range append(append([]string{}, foreign...), alarm...) {
			require.Contains(t, before, line, "the PUT stored the client's line")
		}

		status, raw := apiCall(t, http.MethodPatch, taskURL(list.UUID, task.ID), token, map[string]any{
			"summary": "Water the balcony plants", "description": "Herbs first", "priority": 1,
			"due": "2030-11-04T10:00:00Z",
		}, ifMatch(task.ETag))
		require.Equalf(t, http.StatusOK, status, "patch: %s", string(raw))
		edited := decodeTask(t, raw)
		assert.Equal(t, "Water the balcony plants", edited.Summary)
		assert.True(t, edited.IsRecurring, "still a repeating task")

		afterBody, afterETag := davGet(t, object, email, appPass)
		assert.Equal(t, `"`+edited.ETag+`"`, afterETag)
		after := icalLines(afterBody)

		for _, line := range foreign {
			assert.Contains(t, after, line, "kept byte for byte")
		}
		assert.Equal(t, alarm, block(after, "VALARM"), "the VALARM block is kept byte for byte")
		assert.Equal(t, block(before, "VALARM"), block(after, "VALARM"))

		// Line for line: only the edited properties and the bookkeeping differ.
		changed := map[string]string{
			"SUMMARY": "SUMMARY:Water the balcony plants", "DESCRIPTION": "DESCRIPTION:Herbs first",
			"PRIORITY": "PRIORITY:1", "DUE": "DUE:20301104T100000Z", "SEQUENCE": "SEQUENCE:3",
			"LAST-MODIFIED": "", "DTSTAMP": "",
		}
		require.Equal(t, len(before), len(after), "no line added or removed:\nbefore %q\nafter  %q", before, after)
		seen := map[string]bool{}
		for i := range before {
			if before[i] == after[i] {
				continue
			}
			name := propName(before[i])
			want, ok := changed[name]
			if !assert.Truef(t, ok, "line %d changed: %q -> %q", i, before[i], after[i]) {
				continue
			}
			assert.Equal(t, name, propName(after[i]))
			if want != "" {
				assert.Equal(t, want, after[i])
			}
			seen[name] = true
		}
		for name := range changed {
			assert.Truef(t, seen[name], "%s was rewritten", name)
		}
	})
}

// ----------------------------------------------------------------------------

// TestTaskComponentSets: the component set is enforced on every write path
// end to end — MKCALENDAR with only VTODO, POST /calendars with a kind (and
// what PROPFIND advertises), the widen-only kind PATCH, calendar import, event
// create and move — and the event routes and MCP event tools treat a task's
// UUID exactly like an unknown event.
func TestTaskComponentSets(t *testing.T) {
	email := "task-components@example.test"
	token, username := registerAndLoginFull(t, email, "componentSecret!123", "Task Components")
	_, appPass := createAppPassword(t, token, "task-components")
	home := "/dav/" + username + "/calendars/"

	tasks := createTaskCalendar(t, token, "Chores", "tasks")
	events := createTaskCalendar(t, token, "Meetings", "events")
	mixed := createTaskCalendar(t, token, "Everything", "both")

	vevent := func(uid string) string {
		return vcalendar(ics("BEGIN:VEVENT", "UID:"+uid, "DTSTAMP:20300101T000000Z",
			"DTSTART:20301110T090000Z", "DTEND:20301110T100000Z", "SUMMARY:Event "+uid, "END:VEVENT"))
	}
	vtodo := func(uid string) string {
		return vcalendar(ics("BEGIN:VTODO", "UID:"+uid, "DTSTAMP:20300101T000000Z",
			"DUE:20301110T170000Z", "SUMMARY:Task "+uid, "END:VTODO"))
	}

	t.Run("MKCALENDAR with only VTODO refuses a VEVENT", func(t *testing.T) {
		collection := home + "dav-task-list/"
		mk := `<?xml version="1.0" encoding="utf-8"?>
<C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:set><D:prop>
  <D:displayname>DAV task list</D:displayname>
  <C:supported-calendar-component-set><C:comp name="VTODO"/></C:supported-calendar-component-set>
</D:prop></D:set></C:mkcalendar>`
		status, _, body := davCall(t, "MKCALENDAR", collection, email, appPass, mk, nil)
		require.Equalf(t, http.StatusCreated, status, "MKCALENDAR: %s", string(body))

		status, _, body = davCall(t, "PUT", collection+"meeting.ics", email, appPass, vevent("mk-event"),
			map[string]string{"Content-Type": icsContentType})
		assert.Equal(t, http.StatusForbidden, status, string(body))
		assert.Contains(t, string(body), "supported-calendar-component")
		status, _, _ = davCall(t, "GET", collection+"meeting.ics", email, appPass, "", nil)
		assert.Equal(t, http.StatusNotFound, status, "nothing was stored")

		davPut(t, collection+"chore.ics", email, appPass, vtodo("mk-task"))

		var listed struct {
			Calendars []taskCalendar `json:"calendars"`
		}
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/", token, nil, &listed))
		var found *taskCalendar
		for i := range listed.Calendars {
			if listed.Calendars[i].Path == "dav-task-list" {
				found = &listed.Calendars[i]
			}
		}
		require.NotNil(t, found, "the DAV-made list is listed over REST")
		assert.Equal(t, "tasks", found.Kind)
		assert.Equal(t, "VTODO", found.SupportedComponents)
		assert.Equal(t, []string{"Task mk-task"}, taskSummaries(taskPage(t, token, tasksURL(found.UUID))))
	})

	t.Run("POST /calendars with a kind advertises exactly that kind", func(t *testing.T) {
		for _, tc := range []struct {
			cal  taskCalendar
			kind string
			set  string
			want []string
		}{
			{tasks, "tasks", "VTODO", []string{"VTODO"}},
			{events, "events", "VEVENT", []string{"VEVENT"}},
			{mixed, "both", "VEVENT,VTODO", []string{"VEVENT", "VTODO"}},
		} {
			assert.Equal(t, tc.kind, tc.cal.Kind)
			assert.Equal(t, tc.set, tc.cal.SupportedComponents)
			advertised, _ := davComponents(t, home+tc.cal.Path+"/", email, appPass)
			assert.Equal(t, tc.want, advertised, "PROPFIND of a %s calendar", tc.kind)
		}

		status, raw := restCall(t, http.MethodPost, "/calendars/", token, map[string]string{"name": "Bad", "color": "#16a34a", "kind": "todo"})
		assertErrorBody(t, http.StatusBadRequest, unknownKindMessage, status, raw)

		status, raw = restCall(t, http.MethodPost, "/calendars/"+tasks.UUID+"/events/", token, map[string]any{
			"summary": "Not here", "start": "2030-11-10T09:00:00Z", "end": "2030-11-10T10:00:00Z",
		})
		assertErrorBody(t, http.StatusBadRequest, eventsOnlyRefusal, status, raw, "event create on a task list")
		assert.Empty(t, listEvents(t, token, tasks.UUID, "?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z&expand=false"))
	})

	t.Run("kind widens but never narrows", func(t *testing.T) {
		status, raw := restCall(t, http.MethodPatch, "/calendars/"+tasks.UUID, token, map[string]string{"kind": "events"})
		assertErrorBody(t, http.StatusConflict, narrowingRefusal, status, raw)
		assert.Equal(t, "VTODO", getTaskCalendar(t, token, tasks.UUID).SupportedComponents)

		widened := createTaskCalendar(t, token, "Widened", "events")
		collection := home + widened.Path + "/"
		status, _, body := davCall(t, "PUT", collection+"todo.ics", email, appPass, vtodo("widen-task"),
			map[string]string{"Content-Type": icsContentType})
		require.Equalf(t, http.StatusForbidden, status, "an events calendar refuses a VTODO: %s", string(body))
		_, ctagBefore := davComponents(t, collection, email, appPass)

		var patched taskCalendar
		require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodPatch, "/calendars/"+widened.UUID, token,
			map[string]string{"kind": "both"}, &patched))
		assert.Equal(t, "both", patched.Kind)
		assert.Equal(t, "VEVENT,VTODO", patched.SupportedComponents)

		advertised, ctagAfter := davComponents(t, collection, email, appPass)
		assert.Equal(t, []string{"VEVENT", "VTODO"}, advertised)
		assert.NotEqual(t, ctagBefore, ctagAfter, "widening advances the ctag")
		davPut(t, collection+"todo.ics", email, appPass, vtodo("widen-task"))
	})

	importFile := []byte(vcalendar(ics(
		"BEGIN:VEVENT", "UID:imp-event@test", "DTSTAMP:20300101T000000Z",
		"DTSTART:20301110T090000Z", "DTEND:20301110T100000Z", "SUMMARY:Imported meeting", "END:VEVENT",
		"BEGIN:VTODO", "UID:imp-task@test", "DTSTAMP:20300101T000000Z",
		"DUE:20301112T170000Z", "SUMMARY:Imported chore", "END:VTODO",
		"BEGIN:VEVENT", "UID:imp-series@test", "DTSTAMP:20300101T000000Z",
		"DTSTART:20301111T090000Z", "DTEND:20301111T100000Z", "RRULE:FREQ=DAILY;COUNT=3",
		"SUMMARY:Imported standup", "END:VEVENT",
		"BEGIN:VEVENT", "UID:imp-series@test", "DTSTAMP:20300101T000000Z", "RECURRENCE-ID:20301112T090000Z",
		"DTSTART:20301112T110000Z", "DTEND:20301112T120000Z", "SUMMARY:Imported standup (moved)", "END:VEVENT")))
	type importError struct {
		Index   int    `json:"index"`
		UID     string `json:"uid"`
		Summary string `json:"summary"`
		Error   string `json:"error"`
	}
	type importResult struct {
		Total    int           `json:"total"`
		Imported int           `json:"imported"`
		Skipped  int           `json:"skipped"`
		Failed   int           `json:"failed"`
		Errors   []importError `json:"errors"`
	}
	importInto := func(t *testing.T, calUUID string) importResult {
		t.Helper()
		status, raw := rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+calUUID+"/import",
			token, importFile, map[string]string{"Content-Type": "text/calendar"})
		require.Equalf(t, http.StatusOK, status, "import: %s", string(raw))
		var res importResult
		require.NoError(t, json.Unmarshal(raw, &res))
		return res
	}

	t.Run("import into a task list skips the events and reports them", func(t *testing.T) {
		assert.Equal(t, importResult{Total: 3, Imported: 1, Skipped: 2, Failed: 0, Errors: []importError{
			{Index: 0, UID: "imp-event@test", Summary: "Imported meeting", Error: eventsOnlyRefusal},
			{Index: 2, UID: "imp-series@test", Summary: "Imported standup", Error: eventsOnlyRefusal},
		}}, importInto(t, tasks.UUID))
		assert.Contains(t, taskSummaries(taskPage(t, token, tasksURL(tasks.UUID))), "Imported chore")
		assert.Empty(t, listEvents(t, token, tasks.UUID, "?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z&expand=false"))

		assert.Equal(t, importResult{Total: 3, Imported: 2, Skipped: 1, Failed: 0, Errors: []importError{
			{Index: 1, UID: "imp-task@test", Summary: "Imported chore", Error: tasksOnlyRefusal},
		}}, importInto(t, events.UUID), "and an event calendar skips the tasks")
	})

	t.Run("event move into a task list is 400", func(t *testing.T) {
		var ev struct {
			ID string `json:"id"`
		}
		require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendars/"+events.UUID+"/events/", token, map[string]any{
			"summary": "Planning", "start": "2030-11-20T09:00:00Z", "end": "2030-11-20T10:00:00Z",
		}, &ev))
		status, raw := restCall(t, http.MethodPost, "/calendars/"+events.UUID+"/events/"+ev.ID+"/move", token,
			map[string]string{"target_calendar_id": tasks.UUID})
		assertErrorBody(t, http.StatusBadRequest, eventsOnlyRefusal, status, raw)

		rangeQS := "?start=2030-11-20T00:00:00Z&end=2030-11-21T00:00:00Z&expand=false"
		assert.Contains(t, collectEventUIDs(t, token, events.UUID, rangeQS), mustEventUID(t, token, events.UUID, ev.ID),
			"the event stays where it was")
		assert.Empty(t, listEvents(t, token, tasks.UUID, rangeQS))
	})

	t.Run("event routes and MCP event tools do not serve tasks", func(t *testing.T) {
		restTask := createTask(t, token, tasks.UUID, map[string]any{"summary": "REST chore"})
		davPut(t, home+mixed.Path+"/dav-chore.ics", email, appPass, vtodo("dav-chore"))
		davTask := findTaskByUID(t, token, mixed.UUID, "dav-chore")

		status, unknownEvent := restCall(t, http.MethodGet, "/calendars/"+mixed.UUID+"/events/"+uuid.NewString(), token, nil)
		assertErrorBody(t, http.StatusNotFound, "Event not found", status, unknownEvent)

		for _, task := range []struct {
			name string
			cal  taskCalendar
			task taskJSON
		}{
			{"a REST task in a task list", tasks, restTask},
			{"a DAV task in a mixed calendar", mixed, davTask},
		} {
			base := "/calendars/" + task.cal.UUID + "/events/" + task.task.ID
			for _, req := range []struct {
				name, method, path string
				body               any
				headers            map[string]string
			}{
				{"get", http.MethodGet, base, nil, nil},
				{"patch", http.MethodPatch, base, map[string]any{"summary": "hijacked"}, nil},
				{"patch with the task's own ETag", http.MethodPatch, base, map[string]any{"summary": "hijacked"}, ifMatch(task.task.ETag)},
				{"delete", http.MethodDelete, base, nil, nil},
				{"move", http.MethodPost, base + "/move", map[string]string{"target_calendar_id": events.UUID}, nil},
			} {
				status, raw := apiCall(t, req.method, req.path, token, req.body, req.headers)
				assert.Equal(t, http.StatusNotFound, status, "%s: %s", task.name, req.name)
				assert.JSONEq(t, string(unknownEvent), string(raw), "%s: %s looks like an unknown event", task.name, req.name)
			}
			assertSameTask(t, task.task, getTask(t, token, task.cal.UUID, task.task.ID), "%s is unchanged", task.name)
		}

		mcp := mcpToken(t, token, "task component sets")
		unknownID := uuid.NewString()
		for _, tool := range []struct {
			name string
			args func(id string) map[string]any
		}{
			{"update_event", func(id string) map[string]any { return map[string]any{"event_id": id, "title": "hijacked"} }},
			{"delete_event", func(id string) map[string]any { return map[string]any{"event_id": id} }},
		} {
			unknown, isErr := mcpTool(t, mcp, tool.name, tool.args(unknownID))
			require.True(t, isErr, "%s of an unknown id", tool.name)
			for _, task := range []taskJSON{restTask, davTask} {
				got, isErr := mcpTool(t, mcp, tool.name, tool.args(task.ID))
				assert.True(t, isErr, "%s of a task is a tool error", tool.name)
				assert.Equal(t, strings.ReplaceAll(unknown["message"].(string), unknownID, task.ID), got["message"],
					"%s answers a task exactly like an unknown event", tool.name)
			}
		}
		assertSameTask(t, restTask, getTask(t, token, tasks.UUID, restTask.ID), "MCP changed nothing")
		assertSameTask(t, davTask, getTask(t, token, mixed.UUID, davTask.ID), "MCP changed nothing")

		// Events keep working next to the task in the mixed calendar.
		var ev struct {
			ID string `json:"id"`
		}
		require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendars/"+mixed.UUID+"/events/", token, map[string]any{
			"summary": "Mixed meeting", "start": "2030-11-10T13:00:00Z", "end": "2030-11-10T14:00:00Z",
		}, &ev))
		listed, isErr := mcpTool(t, mcp, "get_events", map[string]any{
			"calendar_id": mixed.UUID, "start": "2030-11-01T00:00:00Z", "end": "2030-12-01T00:00:00Z",
		})
		require.False(t, isErr, "get_events: %v", listed)
		assert.Equal(t, []string{ev.ID}, mcpListIDs(listed, "events"), "get_events lists the event, not the task")
		status, raw := restCall(t, http.MethodPatch, "/calendars/"+mixed.UUID+"/events/"+ev.ID, token, map[string]any{"summary": "Mixed meeting (moved)"})
		assert.Equal(t, http.StatusOK, status, string(raw))
		status, raw = restCall(t, http.MethodDelete, "/calendars/"+mixed.UUID+"/events/"+ev.ID, token, nil)
		assert.Equal(t, http.StatusNoContent, status, string(raw))
	})
}

// mustEventUID GETs an event and returns its UID.
func mustEventUID(t *testing.T, token, calUUID, eventID string) string {
	t.Helper()
	var ev struct {
		UID string `json:"uid"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/calendars/"+calUUID+"/events/"+eventID, token, nil, &ev))
	return ev.UID
}
