package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
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

// TestPropfindOutsideOwnHomeSetsIs404 is the regression test for F11: a
// PROPFIND one level below a principal that is not one of the caller's own two
// home sets — another user's calendar / address-book home, or
// /dav/{me}/{anything}/ — used to answer 207 with an empty multistatus. It must
// be a 404, like everything below another user's home, while the caller's own
// homes and the collections shared with them keep working.
func TestPropfindOutsideOwnHomeSetsIs404(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	calShareRepo := repository.NewCalendarShareRepository(db.DB())
	abShareRepo := repository.NewAddressBookShareRepository(db.DB())

	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	alice := &user.User{UUID: "alice", Email: "alice@example.com", Username: "alice", PasswordHash: string(hash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, alice))
	bob := &user.User{UUID: "bob", Email: "bob@example.com", Username: "bob", PasswordHash: string(hash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, bob))

	secretCal := &calendar.Calendar{UserID: alice.ID, UUID: uuid.New().String(), Path: "alice-secret-cal", Name: "Alice Secret Calendar", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, secretCal))
	sharedCal := &calendar.Calendar{UserID: alice.ID, UUID: uuid.New().String(), Path: "alice-shared-cal", Name: "Alice Shared Calendar", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, sharedCal))
	require.NoError(t, calShareRepo.Create(ctx, &sharing.CalendarShare{
		UUID: uuid.New().String(), CalendarID: sharedCal.ID, SharedWithID: bob.ID, Permission: "read",
	}))
	secretBook := &addressbook.AddressBook{UUID: uuid.New().String(), UserID: alice.ID, Path: "alice-secret-book", Name: "Alice Secret Book"}
	require.NoError(t, abRepo.Create(ctx, secretBook))
	sharedBook := &addressbook.AddressBook{UUID: uuid.New().String(), UserID: alice.ID, Path: "alice-shared-book", Name: "Alice Shared Book"}
	require.NoError(t, abRepo.Create(ctx, sharedBook))
	require.NoError(t, abShareRepo.Create(ctx, &sharing.AddressBookShare{
		UUID: uuid.New().String(), AddressBookID: sharedBook.ID, SharedWithID: bob.ID, Permission: "read",
	}))

	bobAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("bob@example.com:password"))
	propfind := func(t *testing.T, path, depth string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("PROPFIND", path, bytes.NewReader([]byte(
			`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:displayname/></d:prop></d:propfind>`)))
		req.Header.Set("Authorization", bobAuth)
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Depth", depth)
		resp := davDo(t, app, req)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	for _, tc := range []struct{ name, path string }{
		{"another user's calendar home", "/dav/alice/calendars/"},
		{"another user's address-book home", "/dav/alice/addressbooks/"},
		{"another user's home without trailing slash", "/dav/alice/calendars"},
		{"an unknown user's calendar home", "/dav/nobody/calendars/"},
		{"an unknown child of the caller's own principal", "/dav/bob/no-such-home/"},
		{"a foreign child of the caller's own principal", "/dav/bob/alice/"},
	} {
		for _, depth := range []string{"0", "1"} {
			t.Run(tc.name+" depth "+depth, func(t *testing.T) {
				status, body := propfind(t, tc.path, depth)
				assert.Equal(t, http.StatusNotFound, status, "body: %s", body)
				assert.NotContains(t, body, "Alice")
				assert.NotContains(t, body, "alice-")
			})
		}
	}

	t.Run("own calendar home lists own and shared calendars", func(t *testing.T) {
		status, body := propfind(t, "/dav/bob/calendars/", "1")
		require.Equal(t, http.StatusMultiStatus, status)
		assert.Contains(t, body, "/dav/bob/calendars/"+sharedCal.UUID+"/", "the sharee must still see the share")
		assert.NotContains(t, body, "alice-secret-cal")
	})
	t.Run("own address-book home lists the shared book", func(t *testing.T) {
		status, body := propfind(t, "/dav/bob/addressbooks/", "1")
		require.Equal(t, http.StatusMultiStatus, status)
		assert.Contains(t, body, sharedBook.UUID, "the sharee must still see the shared book")
		assert.NotContains(t, body, "alice-secret-book")
	})
	t.Run("shared collections stay reachable under the sharee's home", func(t *testing.T) {
		status, _ := propfind(t, "/dav/bob/calendars/"+sharedCal.UUID+"/", "1")
		assert.Equal(t, http.StatusMultiStatus, status)
		status, _ = propfind(t, "/dav/bob/addressbooks/"+sharedBook.UUID+"/", "1")
		assert.Equal(t, http.StatusMultiStatus, status)
	})
	t.Run("principal discovery is unchanged", func(t *testing.T) {
		status, _ := propfind(t, "/dav/bob/", "0")
		assert.Equal(t, http.StatusMultiStatus, status)
		status, _ = propfind(t, "/dav/", "0")
		assert.Equal(t, http.StatusMultiStatus, status)
	})
}
