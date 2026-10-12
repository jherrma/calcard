package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	gowebdav "github.com/emersion/go-webdav"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestDAVErrorStatus pins the reflection davErrorStatus relies on: if a
// go-webdav upgrade renames HTTPError or its Code field, this fails instead of
// every hand-rolled DAV error silently turning back into a 500.
func TestDAVErrorStatus(t *testing.T) {
	code, ok := davErrorStatus(gowebdav.NewHTTPError(http.StatusNotFound, nil))
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, code)

	code, ok = davErrorStatus(fmt.Errorf("resolve: %w", gowebdav.NewHTTPError(http.StatusForbidden, errors.New("no"))))
	assert.True(t, ok, "a wrapped HTTPError must be found")
	assert.Equal(t, http.StatusForbidden, code)

	_, ok = davErrorStatus(errors.New("database is locked"))
	assert.False(t, ok)
	_, ok = davErrorStatus(nil)
	assert.False(t, ok)
}

// TestSyncCollectionOnMissingOrForeignCollection is the regression test for
// F17: a sync-collection REPORT on a collection that does not exist, or that
// belongs to another user, answered 500 "404 Not Found" because the sync
// handlers handed go-webdav's HTTPError to Fiber as a plain error. It must be
// a 404 — the same for missing and foreign, so existence does not leak.
func TestSyncCollectionOnMissingOrForeignCollection(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	owner := &user.User{UUID: "owner", Email: "owner@example.com", Username: "owner", PasswordHash: string(hash), IsActive: true}
	other := &user.User{UUID: "other", Email: "other@example.com", Username: "other", PasswordHash: string(hash), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	require.NoError(t, userRepo.Create(ctx, other))
	require.NoError(t, calRepo.Create(ctx, &calendar.Calendar{UserID: owner.ID, UUID: "cal", Path: "work", Name: "Work", Timezone: "UTC"}))
	require.NoError(t, abRepo.Create(ctx, &addressbook.AddressBook{UUID: "ab", UserID: owner.ID, Path: "contacts", Name: "Contacts"}))

	syncBody := `<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:"><D:sync-token/><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`
	report := func(email, path string) (int, string) {
		req, _ := http.NewRequest("REPORT", path, bytes.NewReader([]byte(syncBody)))
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(email+":password")))
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Depth", "1")
		resp := davDo(t, app, req)
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	// Positive controls: the owner can sync both collections.
	status, _ := report("owner@example.com", "/dav/owner/calendars/work/")
	require.Equal(t, http.StatusMultiStatus, status, "owner's own calendar")
	status, _ = report("owner@example.com", "/dav/owner/addressbooks/contacts/")
	require.Equal(t, http.StatusMultiStatus, status, "owner's own address book")

	for _, tc := range []struct{ name, path string }{
		{"missing calendar", "/dav/other/calendars/no-such-calendar/"},
		{"another user's calendar", "/dav/owner/calendars/work/"},
		{"missing address book", "/dav/other/addressbooks/no-such-book/"},
		{"another user's address book", "/dav/owner/addressbooks/contacts/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := report("other@example.com", tc.path)
			assert.Equal(t, http.StatusNotFound, status, "body: %s", body)
			assert.NotContains(t, body, "Work")
			assert.NotContains(t, body, "Contacts")
		})
	}
}
