//go:build integration

package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// New accounts get a calendar and a task list (#170, Phase 2): registration
// and first-admin setup create "Personal" (VEVENT) and "Tasks" (VTODO), and
// each advertises exactly its component over PROPFIND, so calendar apps no
// longer treat the calendar as a task list and Apple Reminders finds a task
// list on the first sync. A REST calendar created without kind is VEVENT; a
// collection a DAV client creates without a component set stays mixed.

// defaultCalendarJSON is one item of GET /calendars as these tests read it.
type defaultCalendarJSON struct {
	UUID                string `json:"uuid"`
	Path                string `json:"path"`
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	SupportedComponents string `json:"supported_components"`
	Subscribed          bool   `json:"subscribed"`
}

// ownCalendarsOn lists the caller's calendars on the server at base, by name.
func ownCalendarsOn(t *testing.T, base, token string) map[string]defaultCalendarJSON {
	t.Helper()
	status, raw := rawCall(t, http.MethodGet, base+"/api/v1/calendars/", token, nil, nil)
	require.Equalf(t, http.StatusOK, status, "list calendars: %s", string(raw))
	var wrap struct {
		Calendars []defaultCalendarJSON `json:"calendars"`
	}
	require.NoError(t, json.Unmarshal(raw, &wrap))
	out := map[string]defaultCalendarJSON{}
	for _, c := range wrap.Calendars {
		require.NotContainsf(t, out, c.Name, "two calendars named %q", c.Name)
		out[c.Name] = c
	}
	return out
}

// assertDefaultCollections checks that cals are exactly the two defaults and
// returns them.
func assertDefaultCollections(t *testing.T, cals map[string]defaultCalendarJSON) (personal, tasks defaultCalendarJSON) {
	t.Helper()
	require.Len(t, cals, 2, "exactly Personal and Tasks: %v", cals)
	personal, tasks = cals["Personal"], cals["Tasks"]
	assert.Equal(t, "events", personal.Kind)
	assert.Equal(t, "VEVENT", personal.SupportedComponents)
	assert.Equal(t, "tasks", tasks.Kind)
	assert.Equal(t, "VTODO", tasks.SupportedComponents)
	for _, c := range []defaultCalendarJSON{personal, tasks} {
		assert.Equal(t, c.UUID+".ics", c.Path, c.Name)
		assert.False(t, c.Subscribed, c.Name)
	}
	return personal, tasks
}

// advertisedComponents PROPFINDs a collection (Depth 0) on the server at base
// with Basic auth and returns which of VEVENT and VTODO it advertises.
func advertisedComponents(t *testing.T, base, collection, user, pass string) []string {
	t.Helper()
	status, body := rawCall(t, "PROPFIND", base+collection, "", propfindComponentsBody, map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)),
		"Content-Type":  "application/xml; charset=utf-8",
		"Depth":         "0",
	})
	require.Equalf(t, http.StatusMultiStatus, status, "PROPFIND %s: %s", collection, string(body))
	var out []string
	for _, comp := range []string{"VEVENT", "VTODO"} {
		if strings.Contains(string(body), `name="`+comp+`"`) {
			out = append(out, comp)
		}
	}
	return out
}

// TestNewAccountDefaultCollections: a registered user owns "Personal"
// (events, VEVENT) and "Tasks" (tasks, VTODO); over CalDAV each advertises and
// accepts only its component, and over REST the task routes see only Tasks.
func TestNewAccountDefaultCollections(t *testing.T) {
	email := "defaults-170@example.test"
	password := "defaultsSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Defaults User")
	_, appPass := createAppPassword(t, token, "defaults-170")

	personal, tasks := assertDefaultCollections(t, ownCalendarsOn(t, baseURL, token))
	personalDAV := "/dav/" + username + "/calendars/" + personal.Path + "/"
	tasksDAV := "/dav/" + username + "/calendars/" + tasks.Path + "/"

	t.Run("each collection advertises its own component", func(t *testing.T) {
		got, _ := davComponents(t, personalDAV, email, appPass)
		assert.Equal(t, []string{"VEVENT"}, got, "Personal")
		got, _ = davComponents(t, tasksDAV, email, appPass)
		assert.Equal(t, []string{"VTODO"}, got, "Tasks")

		// The calendar home lists both with the same sets (Depth 1).
		status, _, body := davCall(t, "PROPFIND", "/dav/"+username+"/calendars/", email, appPass,
			propfindComponentsBody, depthHeader("1"))
		require.Equalf(t, http.StatusMultiStatus, status, "home PROPFIND: %s", string(body))
		for _, c := range []struct {
			cal       defaultCalendarJSON
			advertise string
			withhold  string
		}{{personal, "VEVENT", "VTODO"}, {tasks, "VTODO", "VEVENT"}} {
			resp := responseFor(string(body), c.cal.Path)
			require.NotEmptyf(t, resp, "home PROPFIND lists %s: %s", c.cal.Name, string(body))
			assert.Contains(t, resp, `name="`+c.advertise+`"`, c.cal.Name)
			assert.NotContains(t, resp, `name="`+c.withhold+`"`, c.cal.Name)
		}
	})

	start := time.Date(2030, 3, 4, 9, 0, 0, 0, time.UTC)
	event := buildMinimalVEvent("defaults-event", "Dentist", start)
	todo := vcalendar(ics("BEGIN:VTODO", "UID:defaults-task", "DTSTAMP:20300101T000000Z",
		"SUMMARY:Call the dentist", "DUE:20300303T170000Z", "END:VTODO"))

	t.Run("Personal takes events and refuses tasks", func(t *testing.T) {
		davPut(t, personalDAV+"defaults-event.ics", email, appPass, event)
		status, _, body := davCall(t, "PUT", personalDAV+"defaults-task.ics", email, appPass, todo,
			map[string]string{"Content-Type": icsContentType})
		assert.Equalf(t, http.StatusForbidden, status, "VTODO into Personal: %s", string(body))
		assert.Contains(t, string(body), "supported-calendar-component")

		status, raw := restCall(t, http.MethodPost, tasksURL(personal.UUID), token, map[string]any{"summary": "x"})
		assertErrorBody(t, http.StatusBadRequest, tasksOnlyRefusal, status, raw, "REST task into Personal")
		assert.JSONEq(t, emptyTaskPage, string(mustGet(t, token, tasksURL(personal.UUID)+"?status=all")))
	})

	t.Run("Tasks takes tasks and refuses events", func(t *testing.T) {
		davPut(t, tasksDAV+"defaults-task.ics", email, appPass, todo)
		status, _, body := davCall(t, "PUT", tasksDAV+"defaults-event.ics", email, appPass, event,
			map[string]string{"Content-Type": icsContentType})
		assert.Equalf(t, http.StatusForbidden, status, "VEVENT into Tasks: %s", string(body))
		assert.Contains(t, string(body), "supported-calendar-component")

		got := findTaskByUID(t, token, tasks.UUID, "defaults-task")
		assert.Equal(t, "Call the dentist", got.Summary)
		created := createTask(t, token, tasks.UUID, map[string]any{"summary": "Buy floss"})
		assert.Equal(t, tasks.UUID, created.CalendarUUID)

		status, raw := restCall(t, http.MethodPost, "/calendars/"+tasks.UUID+"/events/", token, map[string]any{
			"summary": "x", "start": start.Format(time.RFC3339), "end": start.Add(time.Hour).Format(time.RFC3339),
		})
		assertErrorBody(t, http.StatusBadRequest, eventsOnlyRefusal, status, raw, "REST event into Tasks")
	})

	t.Run("the cross-list task routes see only the task list", func(t *testing.T) {
		page := taskPage(t, token, "/tasks?status=all")
		assert.ElementsMatch(t, []string{"Call the dentist", "Buy floss"}, taskSummaries(page))
		for _, task := range page.Tasks {
			assert.Equal(t, "Tasks", task.CalendarName)
		}
		summary := summaryAsOf(t, token, "2030-03-04T12:00:00Z")
		assert.Equal(t, int64(2), summary.Open)
		assert.Equal(t, int64(1), summary.Overdue, "the DAV task was due yesterday")
		require.Len(t, summary.ByCalendar, 1, "Personal stores no tasks: %v", summary.ByCalendar)
		assert.Contains(t, summary.ByCalendar, tasks.UUID)
	})

	t.Run("a REST calendar without kind is an event calendar", func(t *testing.T) {
		plain := createTaskCalendar(t, token, "Plain", "")
		assert.Equal(t, "events", plain.Kind)
		assert.Equal(t, "VEVENT", plain.SupportedComponents)
		got, _ := davComponents(t, "/dav/"+username+"/calendars/"+plain.Path+"/", email, appPass)
		assert.Equal(t, []string{"VEVENT"}, got)
	})

	t.Run("a DAV collection without a component set stays mixed", func(t *testing.T) {
		slug := "dav-mixed-170"
		collection := "/dav/" + username + "/calendars/" + slug + "/"
		status, _, body := davCall(t, "MKCALENDAR", collection, email, appPass, "", nil)
		require.Equalf(t, http.StatusCreated, status, "bare MKCALENDAR: %s", string(body))

		var mixed defaultCalendarJSON
		for _, c := range ownCalendarsOn(t, baseURL, token) {
			if c.Path == slug {
				mixed = c
			}
		}
		require.NotEmpty(t, mixed.UUID, "the MKCALENDAR collection is listed")
		assert.Equal(t, "both", mixed.Kind)
		assert.Equal(t, "VEVENT,VTODO", mixed.SupportedComponents)
		got, _ := davComponents(t, collection, email, appPass)
		assert.Equal(t, []string{"VEVENT", "VTODO"}, got)
		davPut(t, collection+"defaults-event.ics", email, appPass, event)
		davPut(t, collection+"defaults-task.ics", email, appPass, todo)
	})
}

// responseFor returns the DAV:response of a multistatus body whose href ends
// in segment + "/" (empty when there is none).
func responseFor(body, segment string) string {
	for _, part := range strings.Split(body, "<response")[1:] {
		end := strings.Index(part, "</response>")
		if end < 0 {
			continue
		}
		part = part[:end]
		i := strings.Index(part, "<href")
		if i < 0 {
			continue
		}
		href := part[i:]
		if j := strings.Index(href, "</href>"); j >= 0 && strings.HasSuffix(href[:j], segment+"/") {
			return part
		}
	}
	return ""
}

// TestFirstAdminDefaultCollections: the first admin, created by
// POST /auth/setup on a pristine server, owns the same two collections, and
// each advertises only its component.
func TestFirstAdminDefaultCollections(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, nil)
	t.Cleanup(shutdown)

	status, raw := rawCall(t, http.MethodPost, base+"/api/v1/auth/setup", "", setupBody("admin170", "admin170@example.test"), nil)
	require.Equalf(t, http.StatusOK, status, "setup: %s", string(raw))
	status, raw = rawCall(t, http.MethodPost, base+"/api/v1/auth/login", "", map[string]string{
		"email": "admin170@example.test", "password": "SecurePass123!",
	}, nil)
	require.Equalf(t, http.StatusOK, status, "login: %s", string(raw))
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
			User        struct {
				IsAdmin bool `json:"is_admin"`
			} `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &login))
	require.True(t, login.Data.User.IsAdmin)

	personal, tasks := assertDefaultCollections(t, ownCalendarsOn(t, base, login.Data.AccessToken))

	// The account password works for Basic auth, so no app password is needed.
	home := "/dav/admin170/calendars/"
	assert.Equal(t, []string{"VEVENT"},
		advertisedComponents(t, base, home+personal.Path+"/", "admin170@example.test", "SecurePass123!"), "Personal")
	assert.Equal(t, []string{"VTODO"},
		advertisedComponents(t, base, home+tasks.Path+"/", "admin170@example.test", "SecurePass123!"), "Tasks")
}
