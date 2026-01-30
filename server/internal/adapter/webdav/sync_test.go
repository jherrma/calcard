package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestWebDAVSync(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())

	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{
		UUID:         "sync-user-uuid",
		Email:        "sync@example.com",
		Username:     "syncuser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	cal := &calendar.Calendar{
		UserID:    u.ID,
		UUID:      "sync-cal-uuid",
		Path:      "sync-test",
		Name:      "Sync Test",
		Color:     "#3788d8",
		Timezone:  "UTC",
		SyncToken: "", // Start with no token
		CTag:      "",
	}
	err = calRepo.Create(context.Background(), cal)
	require.NoError(t, err)

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("sync@example.com:password"))

	var token1 string
	t.Run("Initial Sync (no token)", func(t *testing.T) {
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token/>
  <D:sync-level>1</D:sync-level>
  <D:prop>
    <D:getetag/>
  </D:prop>
</D:sync-collection>`
		req, _ := http.NewRequest("REPORT", "/dav/syncuser/calendars/sync-test/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, 207, resp.StatusCode)

		var ms SyncMultiStatus
		err = xml.NewDecoder(resp.Body).Decode(&ms)
		require.NoError(t, err)
		// Since there are no changes yet, token remains empty or is the current empty token
		token1 = ms.SyncToken
		assert.Equal(t, 0, len(ms.Responses))
	})

	t.Run("Create Item", func(t *testing.T) {
		icalData := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//CalCard//EN
BEGIN:VEVENT
UID:sync-event-1
DTSTAMP:20240122T090000Z
DTSTART:20240122T090000Z
DTEND:20240122T100000Z
SUMMARY:Sync Event 1
END:VEVENT
END:VCALENDAR`
		req, _ := http.NewRequest("PUT", "/dav/syncuser/calendars/sync-test/item1.ics", bytes.NewReader([]byte(icalData)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, 201, resp.StatusCode)
	})

	var token2 string
	t.Run("Incremental Sync (Created)", func(t *testing.T) {
		body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:sync-level>1</D:sync-level>
  <D:prop>
    <D:getetag/>
  </D:prop>
</D:sync-collection>`, token1) // token1 is "" or the initial token
		req, _ := http.NewRequest("REPORT", "/dav/syncuser/calendars/sync-test/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, 207, resp.StatusCode)

		var ms SyncMultiStatus
		err = xml.NewDecoder(resp.Body).Decode(&ms)
		require.NoError(t, err)
		assert.Equal(t, 1, len(ms.Responses))
		assert.Contains(t, ms.Responses[0].Href, "item1.ics")
		token2 = ms.SyncToken
		assert.NotEmpty(t, token2)
	})

	t.Run("No Changes Sync", func(t *testing.T) {
		body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:sync-level>1</D:sync-level>
</D:sync-collection>`, token2)
		req, _ := http.NewRequest("REPORT", "/dav/syncuser/calendars/sync-test/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, _ := app.Test(req)
		assert.Equal(t, 207, resp.StatusCode)

		var ms SyncMultiStatus
		xml.NewDecoder(resp.Body).Decode(&ms)
		assert.Equal(t, 0, len(ms.Responses))
		assert.Equal(t, token2, ms.SyncToken)
	})

	t.Run("Incremental Sync (Modified)", func(t *testing.T) {
		icalData := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//CalCard//EN
BEGIN:VEVENT
UID:sync-event-1
DTSTAMP:20240122T100000Z
DTSTART:20240122T090000Z
DTEND:20240122T100000Z
SUMMARY:Sync Event 1 Modified
END:VEVENT
END:VCALENDAR`
		req, _ := http.NewRequest("PUT", "/dav/syncuser/calendars/sync-test/item1.ics", bytes.NewReader([]byte(icalData)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		app.Test(req)

		body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:sync-level>1</D:sync-level>
</D:sync-collection>`, token2)
		req, _ = http.NewRequest("REPORT", "/dav/syncuser/calendars/sync-test/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, _ := app.Test(req)

		var ms SyncMultiStatus
		xml.NewDecoder(resp.Body).Decode(&ms)
		assert.Equal(t, 1, len(ms.Responses))
		assert.Contains(t, ms.Responses[0].Href, "item1.ics")
		token2 = ms.SyncToken // Update to token3
	})

	t.Run("Incremental Sync (Deleted)", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/dav/syncuser/calendars/sync-test/item1.ics", nil)
		req.Header.Set("Authorization", authHeader)
		app.Test(req)

		body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:sync-level>1</D:sync-level>
</D:sync-collection>`, token2)
		req, _ = http.NewRequest("REPORT", "/dav/syncuser/calendars/sync-test/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, _ := app.Test(req)

		var ms SyncMultiStatus
		xml.NewDecoder(resp.Body).Decode(&ms)
		assert.Equal(t, 1, len(ms.Responses))
		assert.Equal(t, "HTTP/1.1 404 Not Found", ms.Responses[0].Status)
	})

	t.Run("Invalid Token", func(t *testing.T) {
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:">
  <D:sync-token>non-existent-token</D:sync-token>
</D:sync-collection>`
		req, _ := http.NewRequest("REPORT", "/dav/syncuser/calendars/sync-test/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, _ := app.Test(req)
		assert.Equal(t, 403, resp.StatusCode)
	})
}
