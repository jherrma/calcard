package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestCollectionProppatchRenameRecolor is the regression test for #43:
// PROPPATCH on a collection (which emersion 501s) must apply displayname and
// Apple calendar-color for the owner, and 403 a sharee. Mixing in a property
// the server cannot set is covered by TestCollectionProppatchAtomic (F08).
func TestCollectionProppatchRenameRecolor(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())

	pw, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	owner := &user.User{UUID: "owner", Email: "owner@example.com", Username: "owner", PasswordHash: string(pw), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	sharee := &user.User{UUID: "sharee", Email: "sharee@example.com", Username: "sharee", PasswordHash: string(pw), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, sharee))

	cal := &calendar.Calendar{UserID: owner.ID, UUID: uuid.New().String(), Path: "work", Name: "Work", Color: "#111111", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, cal))
	require.NoError(t, shareRepo.Create(ctx, &sharing.CalendarShare{
		UUID: uuid.New().String(), CalendarID: cal.ID, SharedWithID: sharee.ID, Permission: "read",
	}))

	proppatch := func(user, pass, path, body string) (*http.Response, SyncMultiStatus) {
		req, _ := http.NewRequest("PROPPATCH", path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		var ms SyncMultiStatus
		if resp.StatusCode == http.StatusMultiStatus {
			require.NoError(t, xml.NewDecoder(resp.Body).Decode(&ms))
		}
		return resp, ms
	}

	locals := func(ms SyncMultiStatus, code string) map[string]bool {
		out := map[string]bool{}
		if len(ms.Responses) == 0 {
			return out
		}
		for _, ps := range ms.Responses[0].PropStat {
			if strings.Contains(ps.Status, code) {
				for _, raw := range ps.Prop.Raw {
					out[raw.XMLName.Local] = true
				}
			}
		}
		return out
	}

	body := `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/">
  <D:set><D:prop>
    <D:displayname>Renamed Work</D:displayname>
    <A:calendar-color>#FF0000FF</A:calendar-color>
  </D:prop></D:set>
</D:propertyupdate>`

	t.Run("owner rename + recolor", func(t *testing.T) {
		resp, ms := proppatch("owner@example.com", "password", "/dav/owner/calendars/work/", body)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)

		ok := locals(ms, "200")
		assert.True(t, ok["displayname"], "displayname must be applied")
		assert.True(t, ok["calendar-color"], "calendar-color must be applied")
		assert.Len(t, ms.Responses[0].PropStat, 1, "a successful PROPPATCH reports only 200s")

		stored, err := calRepo.GetByPath(ctx, owner.ID, "work")
		require.NoError(t, err)
		assert.Equal(t, "Renamed Work", stored.Name)
		assert.Equal(t, "#FF0000", stored.Color, "Apple #RRGGBBAA must be truncated to #RRGGBB")
	})

	t.Run("sharee is forbidden", func(t *testing.T) {
		resp, _ := proppatch("sharee@example.com", "password", "/dav/sharee/calendars/work/", body)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

// TestCollectionProppatchAtomic is the regression test for F08: PROPPATCH is
// all-or-nothing (RFC 4918 §9.2). One property the server cannot set (403) or
// an unacceptable value (409) fails the whole request: every other property is
// reported 424 Failed Dependency and nothing is written — not the name, not the
// colour, and the sync token does not move.
func TestCollectionProppatchAtomic(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())

	pw, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	owner := &user.User{UUID: "owner", Email: "owner@example.com", Username: "owner", PasswordHash: string(pw), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	sharee := &user.User{UUID: "sharee", Email: "sharee@example.com", Username: "sharee", PasswordHash: string(pw), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, sharee))

	cal := &calendar.Calendar{UserID: owner.ID, UUID: uuid.New().String(), Path: "work", Name: "Work", Color: "#111111", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, cal))
	require.NoError(t, shareRepo.Create(ctx, &sharing.CalendarShare{
		UUID: uuid.New().String(), CalendarID: cal.ID, SharedWithID: sharee.ID, Permission: "read",
	}))
	ab := &addressbook.AddressBook{UUID: uuid.New().String(), UserID: owner.ID, Path: "contacts", Name: "Contacts"}
	require.NoError(t, abRepo.Create(ctx, ab))

	proppatch := func(user, path, body string) (*http.Response, map[string]string) {
		req, _ := http.NewRequest("PROPPATCH", path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":password")))
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		// property local name -> status line of the propstat it is reported in
		statuses := map[string]string{}
		if resp.StatusCode == http.StatusMultiStatus {
			var ms SyncMultiStatus
			require.NoError(t, xml.NewDecoder(resp.Body).Decode(&ms))
			require.Len(t, ms.Responses, 1)
			for _, ps := range ms.Responses[0].PropStat {
				for _, raw := range ps.Prop.Raw {
					statuses[raw.XMLName.Local] = ps.Status
				}
			}
		}
		return resp, statuses
	}

	unchangedCalendar := func(t *testing.T) {
		t.Helper()
		stored, err := calRepo.GetByPath(ctx, owner.ID, "work")
		require.NoError(t, err)
		assert.Equal(t, "Work", stored.Name, "a failed PROPPATCH must not rename")
		assert.Equal(t, "#111111", stored.Color, "a failed PROPPATCH must not recolour")
		assert.Equal(t, cal.SyncToken, stored.SyncToken, "a failed PROPPATCH must not advance the sync token")
	}

	const calURL = "/dav/owner/calendars/work/"

	t.Run("displayname with an unsupported property applies nothing", func(t *testing.T) {
		resp, st := proppatch("owner@example.com", calURL, `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/" xmlns:X="urn:example">
  <D:set><D:prop>
    <D:displayname>Should not stick</D:displayname>
    <A:calendar-color>#FF0000FF</A:calendar-color>
    <X:unsupported>1</X:unsupported>
  </D:prop></D:set>
</D:propertyupdate>`)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		assert.Equal(t, "HTTP/1.1 424 Failed Dependency", st["displayname"])
		assert.Equal(t, "HTTP/1.1 424 Failed Dependency", st["calendar-color"])
		assert.Equal(t, "HTTP/1.1 403 Forbidden", st["unsupported"])
		unchangedCalendar(t)
	})

	t.Run("an invalid colour fails the rename too", func(t *testing.T) {
		resp, st := proppatch("owner@example.com", calURL, `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/">
  <D:set><D:prop>
    <D:displayname>Should not stick</D:displayname>
    <A:calendar-color>red</A:calendar-color>
  </D:prop></D:set>
</D:propertyupdate>`)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		assert.Equal(t, "HTTP/1.1 409 Conflict", st["calendar-color"])
		assert.Equal(t, "HTTP/1.1 424 Failed Dependency", st["displayname"])
		unchangedCalendar(t)
	})

	t.Run("an empty displayname is a conflict", func(t *testing.T) {
		resp, st := proppatch("owner@example.com", calURL, `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/">
  <D:set><D:prop><D:displayname>  </D:displayname><A:calendar-color>#00FF00</A:calendar-color></D:prop></D:set>
</D:propertyupdate>`)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		assert.Equal(t, "HTTP/1.1 409 Conflict", st["displayname"])
		assert.Equal(t, "HTTP/1.1 424 Failed Dependency", st["calendar-color"])
		unchangedCalendar(t)
	})

	t.Run("a remove fails the set beside it", func(t *testing.T) {
		resp, st := proppatch("owner@example.com", calURL, `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/">
  <D:set><D:prop><D:displayname>Should not stick</D:displayname></D:prop></D:set>
  <D:remove><D:prop><A:calendar-color/></D:prop></D:remove>
</D:propertyupdate>`)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		assert.Equal(t, "HTTP/1.1 403 Forbidden", st["calendar-color"])
		assert.Equal(t, "HTTP/1.1 424 Failed Dependency", st["displayname"])
		unchangedCalendar(t)
	})

	t.Run("address book: displayname with an unsupported property applies nothing", func(t *testing.T) {
		resp, st := proppatch("owner@example.com", "/dav/owner/addressbooks/contacts/", `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/">
  <D:set><D:prop>
    <D:displayname>Should not stick</D:displayname>
    <A:calendar-color>#FF0000</A:calendar-color>
  </D:prop></D:set>
</D:propertyupdate>`)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		assert.Equal(t, "HTTP/1.1 424 Failed Dependency", st["displayname"])
		assert.Equal(t, "HTTP/1.1 403 Forbidden", st["calendar-color"], "an address book has no colour")
		stored, err := abRepo.GetByUserAndPath(ctx, owner.ID, "contacts")
		require.NoError(t, err)
		assert.Equal(t, "Contacts", stored.Name)
		assert.Equal(t, ab.SyncToken, stored.SyncToken)
	})

	t.Run("the target is checked before the properties", func(t *testing.T) {
		const unsupportedOnly = `<D:propertyupdate xmlns:D="DAV:" xmlns:X="urn:example"><D:set><D:prop><X:unsupported>1</X:unsupported></D:prop></D:set></D:propertyupdate>`
		resp, _ := proppatch("sharee@example.com", "/dav/sharee/calendars/work/", unsupportedOnly)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "a sharee may not PROPPATCH, whatever the properties")
		resp, _ = proppatch("owner@example.com", "/dav/owner/calendars/missing/", unsupportedOnly)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		resp, _ = proppatch("owner@example.com", "/dav/owner/addressbooks/missing/", unsupportedOnly)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("a valid request is applied with the sync token", func(t *testing.T) {
		resp, st := proppatch("owner@example.com", calURL, `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/">
  <D:set><D:prop><D:displayname>Renamed</D:displayname><A:calendar-color>#00FF00</A:calendar-color></D:prop></D:set>
</D:propertyupdate>`)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		assert.Equal(t, "HTTP/1.1 200 OK", st["displayname"])
		assert.Equal(t, "HTTP/1.1 200 OK", st["calendar-color"])
		stored, err := calRepo.GetByPath(ctx, owner.ID, "work")
		require.NoError(t, err)
		assert.Equal(t, "Renamed", stored.Name)
		assert.Equal(t, "#00FF00", stored.Color)
		assert.NotEqual(t, cal.SyncToken, stored.SyncToken, "a rename must advance the sync token")
	})
}
