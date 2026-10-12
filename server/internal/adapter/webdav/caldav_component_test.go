package webdav

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
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
	assert.Equal(t, calendarComponentSet, got.SupportedComponentSet)
	assert.ElementsMatch(t, []string{"VEVENT", "VTODO"}, got.SupportedComponentSet)
}
