package webdav

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestCalDAVPutUnsupportedComponent is the regression test for F27 (a): a
// calendar collection stores VEVENT and VTODO only, so a VJOURNAL or VFREEBUSY
// PUT must fail with the CALDAV:supported-calendar-component precondition
// (RFC 4791 §5.3.2.1) as a 403 — it used to be a plain-text 400 from the
// denormalisation step — and store nothing.
func TestCalDAVPutUnsupportedComponent(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	require.NoError(t, userRepo.Create(context.Background(), &user.User{
		UUID: "comp-set-user", Email: "compset@example.com", Username: "compsetuser",
		PasswordHash: string(hash), IsActive: true,
	}))
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("compset@example.com:password"))

	do := func(method, url, body string) (int, string) {
		req, _ := http.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		if body != "" {
			req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
		}
		resp := davDo(t, app, req)
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}

	status, _ := do("MKCOL", "/dav/compsetuser/calendars/work/", "")
	require.Equal(t, fiber.StatusCreated, status)

	wrap := func(comp string) string {
		return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + comp + "END:VCALENDAR\r\n"
	}
	unsupported := map[string]string{
		"VJOURNAL": wrap("BEGIN:VJOURNAL\r\nUID:journal-1\r\nDTSTAMP:20300101T000000Z\r\n" +
			"SUMMARY:notes\r\nEND:VJOURNAL\r\n"),
		"VFREEBUSY": wrap("BEGIN:VFREEBUSY\r\nUID:fb-1\r\nDTSTAMP:20300101T000000Z\r\n" +
			"DTSTART:20300101T000000Z\r\nDTEND:20300102T000000Z\r\nEND:VFREEBUSY\r\n"),
	}
	for name, body := range unsupported {
		t.Run(name, func(t *testing.T) {
			url := "/dav/compsetuser/calendars/work/" + strings.ToLower(name) + ".ics"
			status, resp := do("PUT", url, body)
			assert.Equal(t, http.StatusForbidden, status, resp)
			assert.Contains(t, resp, "supported-calendar-component",
				"the DAV:error body must name the failed precondition")
			assert.Contains(t, resp, "urn:ietf:params:xml:ns:caldav")

			status, _ = do("GET", url, "")
			assert.Equal(t, http.StatusNotFound, status, "nothing may have been stored")
		})
	}

	t.Run("VEVENT with its VTIMEZONE is still accepted", func(t *testing.T) {
		body := wrap("BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\nBEGIN:STANDARD\r\n" +
			"DTSTART:19701025T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
			"BEGIN:VEVENT\r\nUID:event-1\r\nDTSTAMP:20300101T000000Z\r\n" +
			"DTSTART;TZID=Europe/Berlin:20300101T100000\r\nSUMMARY:x\r\nEND:VEVENT\r\n")
		status, resp := do("PUT", "/dav/compsetuser/calendars/work/event-1.ics", body)
		assert.Equal(t, http.StatusCreated, status, resp)
	})

	t.Run("VTODO is still accepted", func(t *testing.T) {
		body := wrap("BEGIN:VTODO\r\nUID:todo-1\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:t\r\nEND:VTODO\r\n")
		status, resp := do("PUT", "/dav/compsetuser/calendars/work/todo-1.ics", body)
		assert.Equal(t, http.StatusCreated, status, resp)
	})
}

// TestCalendarComponentSetAdvertised pins that the component set a collection
// advertises (supported-calendar-component-set) is exactly the one a PUT
// enforces.
func TestCalendarComponentSetAdvertised(t *testing.T) {
	b := &CalDAVBackend{}
	got := b.mapCalendar("someone", &calendar.Calendar{Path: "work"}, calendar.PermissionOwner)
	assert.Equal(t, calendarComponentSet(""), got.SupportedComponentSet)
	assert.ElementsMatch(t, []string{"VEVENT", "VTODO"}, got.SupportedComponentSet)
}

// TestCalendarComponentSetPerStoredValue pins that calendarComponentSet, now
// delegating to calendar.SupportsComponent (#170), reports exactly what it did
// before for every value supported_components holds, and agrees with
// SupportsComponent component by component.
func TestCalendarComponentSetPerStoredValue(t *testing.T) {
	for stored, want := range map[string][]string{
		"VEVENT":       {"VEVENT"},
		"VTODO":        {"VTODO"},
		"VEVENT,VTODO": {"VEVENT", "VTODO"},
		"":             {"VEVENT", "VTODO"},
	} {
		t.Run("stored="+stored, func(t *testing.T) {
			assert.Equal(t, want, calendarComponentSet(stored))

			cal := &calendar.Calendar{Path: "work", SupportedComponents: stored}
			got := (&CalDAVBackend{}).mapCalendar("someone", cal, calendar.PermissionOwner)
			assert.Equal(t, want, got.SupportedComponentSet, "advertised set")
			for _, comp := range []string{calendar.ComponentEvent, calendar.ComponentToDo} {
				assert.Equal(t, calendar.SupportsComponent(cal, comp), slices.Contains(want, comp), comp)
			}
		})
	}
}

// TestCalDAVPutRespectsNarrowedComponentSet pins that DAV PUT keeps enforcing a
// narrowed set after calendarComponentSet delegates to the domain rule (#170):
// a VEVENT into a VTODO-only collection and a VTODO into a VEVENT-only one are
// 403 supported-calendar-component and store nothing; the matching component
// is still accepted.
func TestCalDAVPutRespectsNarrowedComponentSet(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	require.NoError(t, userRepo.Create(context.Background(), &user.User{
		UUID: "narrow-set-user", Email: "narrowset@example.com", Username: "narrowsetuser",
		PasswordHash: string(hash), IsActive: true,
	}))
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("narrowset@example.com:password"))

	do := func(method, url, contentType, body string) (int, string) {
		req, _ := http.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp := davDo(t, app, req)
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	componentSet := func(comp string) string {
		return mkcalendarWith(`<CAL:supported-calendar-component-set><CAL:comp name="` + comp +
			`"/></CAL:supported-calendar-component-set>`)
	}
	wrap := func(comp string) string {
		return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + comp + "END:VCALENDAR\r\n"
	}
	event := wrap("BEGIN:VEVENT\r\nUID:narrow-event\r\nDTSTAMP:20300101T000000Z\r\n" +
		"DTSTART:20300101T100000Z\r\nDTEND:20300101T110000Z\r\nSUMMARY:e\r\nEND:VEVENT\r\n")
	todo := wrap("BEGIN:VTODO\r\nUID:narrow-todo\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:t\r\nEND:VTODO\r\n")

	for _, tc := range []struct {
		collection, stores string
		refused, accepted  string
	}{
		{"tasks", "VTODO", event, todo},
		{"events", "VEVENT", todo, event},
	} {
		t.Run(tc.stores+" only", func(t *testing.T) {
			base := "/dav/narrowsetuser/calendars/" + tc.collection + "/"
			status, body := do("MKCALENDAR", base, "application/xml; charset=utf-8", componentSet(tc.stores))
			require.Equal(t, http.StatusCreated, status, body)

			status, body = do("PUT", base+"refused.ics", "text/calendar; charset=utf-8", tc.refused)
			assert.Equal(t, http.StatusForbidden, status, body)
			assert.Contains(t, body, "supported-calendar-component")
			status, _ = do("GET", base+"refused.ics", "", "")
			assert.Equal(t, http.StatusNotFound, status, "nothing may have been stored")

			status, body = do("PUT", base+"accepted.ics", "text/calendar; charset=utf-8", tc.accepted)
			assert.Equal(t, http.StatusCreated, status, body)
		})
	}
}

// TestCalDAVCollectionWithoutComponentSetStoresBoth pins the CalDAV default
// (#170): a collection a client creates without naming a
// supported-calendar-component-set stays "VEVENT,VTODO" (RFC 4791 §5.2.3:
// absent means every component; some clients rely on it), unlike a REST
// calendar, which defaults to VEVENT. It advertises both and takes both.
func TestCalDAVCollectionWithoutComponentSetStoresBoth(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	u := &user.User{
		UUID: "bare-set-user", Email: "bareset@example.com", Username: "baresetuser",
		PasswordHash: string(hash), IsActive: true,
	}
	require.NoError(t, userRepo.Create(ctx, u))
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("bareset@example.com:password"))

	do := func(method, url, contentType, body string, headers ...string) (int, string) {
		req, _ := http.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		resp := davDo(t, app, req)
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	const xmlType = "application/xml; charset=utf-8"
	const propfind = `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
		`<d:prop><c:supported-calendar-component-set/></d:prop></d:propfind>`
	wrap := func(comp string) string {
		return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + comp + "END:VCALENDAR\r\n"
	}

	for _, tc := range []struct {
		name, method, slug, contentType, body string
	}{
		{"MKCALENDAR without a body", "MKCALENDAR", "bare", "", ""},
		{"MKCALENDAR naming only a displayname", "MKCALENDAR", "named", xmlType,
			mkcalendarWith(`<displayname>Named</displayname>`)},
		{"extended MKCOL without a component set", "MKCOL", "extended", xmlType,
			`<?xml version="1.0" encoding="UTF-8"?><mkcol ` + davx5NS + `><set><prop>` +
				`<resourcetype><collection/><CAL:calendar/></resourcetype><displayname>Extended</displayname>` +
				`</prop></set></mkcol>`},
		{"plain MKCOL", "MKCOL", "plain", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := "/dav/baresetuser/calendars/" + tc.slug + "/"
			status, body := do(tc.method, base, tc.contentType, tc.body)
			require.Equal(t, http.StatusCreated, status, body)

			cal, err := calRepo.GetByPath(ctx, u.ID, tc.slug)
			require.NoError(t, err)
			assert.Equal(t, "VEVENT,VTODO", cal.SupportedComponents, "stored set")
			assert.Equal(t, calendar.KindBoth, calendar.KindOf(cal))

			status, body = do("PROPFIND", base, xmlType, propfind, "Depth", "0")
			require.Equal(t, http.StatusMultiStatus, status, body)
			assert.Contains(t, body, `name="VEVENT"`)
			assert.Contains(t, body, `name="VTODO"`)

			status, body = do("PUT", base+"event.ics", "text/calendar; charset=utf-8",
				wrap("BEGIN:VEVENT\r\nUID:bare-event-"+tc.slug+"\r\nDTSTAMP:20300101T000000Z\r\n"+
					"DTSTART:20300101T100000Z\r\nDTEND:20300101T110000Z\r\nSUMMARY:e\r\nEND:VEVENT\r\n"))
			assert.Equal(t, http.StatusCreated, status, body)
			status, body = do("PUT", base+"todo.ics", "text/calendar; charset=utf-8",
				wrap("BEGIN:VTODO\r\nUID:bare-todo-"+tc.slug+"\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:t\r\nEND:VTODO\r\n"))
			assert.Equal(t, http.StatusCreated, status, body)
		})
	}
}

// TestCalDAVAdvertisesTheKindOfARESTCalendar pins the DAV side of `kind` on
// POST /api/v1/calendars (#170): a calendar created through the REST create
// use case advertises exactly its kind's components in
// supported-calendar-component-set — a task list only VTODO, so Reminders and
// Tasks.org list it as a task list and calendar apps do not — and the default
// (no kind) is an event calendar. No DAV code is involved beyond mapCalendar.
func TestCalDAVAdvertisesTheKindOfARESTCalendar(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	u := &user.User{
		UUID: "kind-user", Email: "kind@example.com", Username: "kinduser",
		PasswordHash: string(hash), IsActive: true,
	}
	require.NoError(t, userRepo.Create(ctx, u))
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("kind@example.com:password"))
	create := calendaruc.NewCreateCalendarUseCase(repository.NewCalendarRepository(db.DB()))

	const propfind = `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
		`<d:prop><c:supported-calendar-component-set/></d:prop></d:propfind>`

	for _, tc := range []struct {
		kind      string
		advertise []string
		withhold  []string
	}{
		{"tasks", []string{"VTODO"}, []string{"VEVENT"}},
		{"events", []string{"VEVENT"}, []string{"VTODO"}},
		{"both", []string{"VEVENT", "VTODO"}, nil},
		{"", []string{"VEVENT"}, []string{"VTODO"}},
	} {
		t.Run("kind="+tc.kind, func(t *testing.T) {
			cal, err := create.Execute(ctx, u.ID, calendaruc.CreateCalendarRequest{Name: "Kind " + tc.kind, Kind: tc.kind})
			require.NoError(t, err)

			for _, depth := range []string{"0", "1"} {
				req, _ := http.NewRequest("PROPFIND", "/dav/kinduser/calendars/"+cal.Path+"/", strings.NewReader(propfind))
				req.Header.Set("Authorization", auth)
				req.Header.Set("Content-Type", "application/xml; charset=utf-8")
				req.Header.Set("Depth", depth)
				resp := davDo(t, app, req)
				data, _ := io.ReadAll(resp.Body)
				body := string(data)
				require.Equal(t, http.StatusMultiStatus, resp.StatusCode, body)
				for _, comp := range tc.advertise {
					assert.Contains(t, body, `name="`+comp+`"`, "Depth %s advertises %s", depth, comp)
				}
				for _, comp := range tc.withhold {
					assert.NotContains(t, body, `name="`+comp+`"`, "Depth %s must not advertise %s", depth, comp)
				}
			}
		})
	}
}
