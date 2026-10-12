package webdav

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestOwnedSubscriptionCollectionListing is the regression test for F21: the
// owner of a subscribed calendar (story 100) must be able to list its members
// with a Depth 1 PROPFIND. GetCalendar used to map the collection with the
// read-only-capped permission, so it was addressed like a share (by UUID), and
// emersion's member listing under that unresolvable URL turned the whole
// PROPFIND into a 404. The read-only guarantees must survive the fix.
func TestOwnedSubscriptionCollectionListing(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())

	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{UUID: "u", Email: "u@example.com", Username: "u", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, u))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("u@example.com:password"))

	cal := &calendar.Calendar{UserID: u.ID, UUID: "sub-uuid", Path: "feed", Name: "Feed", Timezone: "UTC", Subscribed: true}
	require.NoError(t, calRepo.Create(ctx, cal))
	obj := &calendar.CalendarObject{
		UUID: "obj-uuid", CalendarID: cal.ID, Path: "one.ics", UID: "one@feed", ETag: calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VEVENT\r\nUID:one@feed\r\n" +
			"DTSTAMP:20260101T000000Z\r\nDTSTART:20260301T100000Z\r\nDTEND:20260301T110000Z\r\n" +
			"SUMMARY:Feed event\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.NoError(t, calRepo.CreateCalendarObject(ctx, obj))

	const collection = "/dav/u/calendars/feed/"

	do := func(method, path, depth, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", authHeader)
		if depth != "" {
			req.Header.Set("Depth", depth)
		}
		if strings.HasPrefix(body, "BEGIN:") {
			req.Header.Set("Content-Type", "text/calendar")
		} else if body != "" {
			req.Header.Set("Content-Type", "application/xml")
		}
		resp := davDo(t, app, req)
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	t.Run("the backend addresses the owned subscription by its owner path", func(t *testing.T) {
		backend := NewCalDAVBackend(calRepo, userRepo, shareRepo)
		got, err := backend.GetCalendar(WithUser(ctx, u), collection)
		require.NoError(t, err)
		assert.Equal(t, collection, got.Path, "an owned subscription is not a share and must keep its own URL")
	})

	propfind := `<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:prop><D:getetag/><D:resourcetype/></D:prop></D:propfind>`
	for _, depth := range []string{"1", "infinity"} {
		t.Run("PROPFIND Depth "+depth+" lists the members", func(t *testing.T) {
			status, body := do("PROPFIND", collection, depth, propfind)
			require.Equal(t, http.StatusMultiStatus, status, "%s", body)
			assert.Contains(t, body, "<href>"+collection+"</href>", "the collection's own href is the request URL")
			assert.Contains(t, body, "<href>"+collection+"one.ics</href>", "the member is listed")
			assert.NotContains(t, body, cal.UUID, "an owned collection must not be addressed by UUID")
		})
	}

	t.Run("REPORT calendar-query and GET still read", func(t *testing.T) {
		status, body := do("REPORT", collection, "1",
			`<?xml version="1.0" encoding="utf-8"?><C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`+
				`<D:prop><D:getetag/></D:prop><C:filter><C:comp-filter name="VCALENDAR"/></C:filter></C:calendar-query>`)
		require.Equal(t, http.StatusMultiStatus, status, "%s", body)
		assert.Contains(t, body, collection+"one.ics")

		status, body = do(http.MethodGet, collection+"one.ics", "", "")
		require.Equal(t, http.StatusOK, status, "%s", body)
		assert.Contains(t, body, "Feed event")
	})

	t.Run("the collection stays read-only", func(t *testing.T) {
		status, body := do("PROPFIND", collection, "0",
			`<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:prop><D:current-user-privilege-set/></D:prop></D:propfind>`)
		require.Equal(t, http.StatusMultiStatus, status, "%s", body)
		assert.Contains(t, body, "<read/>")
		assert.NotContains(t, body, "<write/>", "a subscription must not advertise write")

		status, _ = do(http.MethodPut, collection+"intruder.ics", "",
			"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VEVENT\r\nUID:intruder@x\r\n"+
				"DTSTAMP:20260101T000000Z\r\nDTSTART:20260301T100000Z\r\nSUMMARY:Intruder\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
		assert.Equal(t, http.StatusForbidden, status, "PUT into a subscription")

		status, _ = do(http.MethodDelete, collection+"one.ics", "", "")
		assert.Equal(t, http.StatusForbidden, status, "DELETE of a mirrored object")

		objs, err := calRepo.GetCalendarObjects(ctx, cal.ID)
		require.NoError(t, err)
		assert.Len(t, objs, 1, "nothing may have been written or deleted")
	})
}
