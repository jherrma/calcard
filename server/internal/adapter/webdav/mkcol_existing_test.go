package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/emersion/go-webdav/caldav"
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

// TestMkcolOnExistingCollection is the regression test for F03: MKCOL and
// MKCALENDAR on a URL that already holds a collection answered 201 and
// inserted a second calendar / address book at the same path. RFC 4918
// §9.3.1 requires 405 (MKCOL only on an unmapped URL).
func TestMkcolOnExistingCollection(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{
		UUID:         "u-uuid",
		Email:        "test@example.com",
		Username:     "testuser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	require.NoError(t, userRepo.Create(context.Background(), u))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("test@example.com:password"))

	dav := func(method, path, body string) int {
		req, _ := http.NewRequest(method, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		if body != "" {
			req.Header.Set("Content-Type", "application/xml")
		}
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		return resp.StatusCode
	}
	countCalendars := func(path string) int64 {
		var n int64
		require.NoError(t, db.DB().Model(&calendar.Calendar{}).Where("user_id = ? AND path = ?", u.ID, path).Count(&n).Error)
		return n
	}
	countBooks := func(path string) int64 {
		var n int64
		require.NoError(t, db.DB().Model(&addressbook.AddressBook{}).Where("user_id = ? AND path = ?", u.ID, path).Count(&n).Error)
		return n
	}

	mkcalendar := `<C:mkcalendar xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:D="DAV:">` +
		`<D:set><D:prop><D:displayname>Work</D:displayname></D:prop></D:set></C:mkcalendar>`
	mkcolBook := `<D:mkcol xmlns:D="DAV:" xmlns:A="urn:ietf:params:xml:ns:carddav"><D:set><D:prop>` +
		`<D:resourcetype><D:collection/><A:addressbook/></D:resourcetype><D:displayname>Friends</D:displayname>` +
		`</D:prop></D:set></D:mkcol>`

	t.Run("calendar: MKCALENDAR and MKCOL on an existing calendar are 405", func(t *testing.T) {
		const p = "/dav/testuser/calendars/work/"
		require.Equal(t, http.StatusCreated, dav("MKCALENDAR", p, mkcalendar))
		assert.Equal(t, http.StatusMethodNotAllowed, dav("MKCALENDAR", p, mkcalendar))
		assert.Equal(t, http.StatusMethodNotAllowed, dav("MKCOL", p, ""))
		assert.Equal(t, int64(1), countCalendars("work"), "a refused request must not insert a duplicate")
	})

	t.Run("calendar: a deleted calendar's path can be created again", func(t *testing.T) {
		const p = "/dav/testuser/calendars/again/"
		require.Equal(t, http.StatusCreated, dav("MKCALENDAR", p, mkcalendar))
		require.Equal(t, http.StatusNoContent, dav("DELETE", p, ""))
		assert.Equal(t, http.StatusCreated, dav("MKCALENDAR", p, mkcalendar))
		assert.Equal(t, int64(1), countCalendars("again"))
	})

	t.Run("address book: MKCOL on an existing address book is 405", func(t *testing.T) {
		const p = "/dav/testuser/addressbooks/friends/"
		require.Equal(t, http.StatusCreated, dav("MKCOL", p, mkcolBook))
		assert.Equal(t, http.StatusMethodNotAllowed, dav("MKCOL", p, mkcolBook))
		assert.Equal(t, http.StatusMethodNotAllowed, dav("MKCOL", p, ""))
		assert.Equal(t, int64(1), countBooks("friends"), "a refused request must not insert a duplicate")
	})

	t.Run("address book: a deleted address book's path can be created again", func(t *testing.T) {
		const p = "/dav/testuser/addressbooks/again/"
		require.Equal(t, http.StatusCreated, dav("MKCOL", p, mkcolBook))
		require.Equal(t, http.StatusNoContent, dav("DELETE", p, ""))
		assert.Equal(t, http.StatusCreated, dav("MKCOL", p, mkcolBook))
		assert.Equal(t, int64(1), countBooks("again"))
	})
}

// TestMkcolOnSharedCalendarURL: a share is reachable for the sharee under the
// calendar's UUID, so that URL is mapped and MKCOL there must be refused
// rather than creating an owned calendar that would shadow the share.
func TestMkcolOnSharedCalendarURL(t *testing.T) {
	_, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())
	owner := &user.User{UUID: uuid.NewString(), Email: "owner@example.com", Username: "owner", PasswordHash: "x", IsActive: true}
	sharee := &user.User{UUID: uuid.NewString(), Email: "sharee@example.com", Username: "sharee", PasswordHash: "x", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	require.NoError(t, userRepo.Create(ctx, sharee))
	cal := &calendar.Calendar{UUID: uuid.NewString(), UserID: owner.ID, Path: "team", Name: "Team",
		Color: "#3788d8", Timezone: "UTC", SupportedComponents: "VEVENT,VTODO"}
	require.NoError(t, calRepo.Create(ctx, cal))
	require.NoError(t, db.DB().Create(&sharing.CalendarShare{UUID: uuid.NewString(), CalendarID: cal.ID,
		SharedWithID: sharee.ID, Permission: "read"}).Error)

	backend := NewCalDAVBackend(calRepo, userRepo, shareRepo)
	err := backend.CreateCalendar(WithUser(ctx, sharee), &caldav.Calendar{Path: "/dav/sharee/calendars/" + cal.UUID + "/"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "405 Method Not Allowed")

	var n int64
	require.NoError(t, db.DB().Model(&calendar.Calendar{}).Where("user_id = ?", sharee.ID).Count(&n).Error)
	assert.Zero(t, n, "no calendar may be created for the sharee at the share's URL")
}
