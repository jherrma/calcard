package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// davTestTimeout relaxes app.Test's 1-second default. Under `go test -race` the
// bcrypt-per-request cost pushes DAV requests past that default, so app.Test
// returns (nil, err); callers that ignore the error then nil-deref the response
// and crash the whole package test binary. Referenced by identifier from the
// other webdav test files, so only this file needs the fiber/time imports.
var davTestTimeout = fiber.TestConfig{Timeout: 10 * time.Second}

// davDo runs req against the test app with the relaxed timeout and fails the
// test cleanly (instead of panicking on a nil response) if app.Test errors.
func davDo(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, davTestTimeout)
	require.NoError(t, err)
	return resp
}

func setupTestApp(t *testing.T) (*fiber.App, database.Database, *config.Config) {
	dataDir := t.TempDir() // auto-removed at test end

	cfg := &config.Config{
		DataDir: dataDir,
		Database: config.DatabaseConfig{
			Driver: "sqlite",
		},
		JWT: config.JWTConfig{
			Secret: "test-secret",
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)

	err = db.Migrate(database.Models()...)
	require.NoError(t, err)

	app := fiber.New(fiber.Config{
		RequestMethods: append(fiber.DefaultMethods,
			"PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK", "REPORT", "MKCALENDAR",
		),
	})

	userRepo := repository.NewUserRepository(db.DB())
	calendarRepo := repository.NewCalendarRepository(db.DB())
	appPwdRepo := repository.NewAppPasswordRepository(db.DB())
	caldavCredRepo := repository.NewCalDAVCredentialRepository(db.DB())
	carddavCredRepo := repository.NewCardDAVCredentialRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	shareRepo := repository.NewCalendarShareRepository(db.DB())
	abShareRepo := repository.NewAddressBookShareRepository(db.DB())
	caldavBackend := NewCalDAVBackend(calendarRepo, userRepo, shareRepo)
	addressBookRepo := repository.NewAddressBookRepository(db.DB())
	carddavBackend := NewCardDAVBackend(addressBookRepo, userRepo, abShareRepo)
	davHandler := NewHandler(caldavBackend, carddavBackend, userRepo, appPwdRepo, caldavCredRepo, carddavCredRepo, jwtManager)

	app.All("/.well-known/caldav", WellKnownCalDAVRedirect)
	app.All("/.well-known/carddav", WellKnownCardDAVRedirect)
	davGroup := app.Group("/dav", davHandler.Authenticate())

	davGroup.All("/*", davHandler.Handler())

	return app, db, cfg
}

func TestCalDAV(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{
		UUID:         "test-uuid",
		Email:        "test@example.com",
		Username:     "testuser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("test@example.com:password"))

	t.Run("OPTIONS /dav/", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", "/dav/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("DAV"), "calendar-access")
	})

	t.Run("PROPFIND /dav/testuser/", func(t *testing.T) {
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:">
  <D:prop>
    <D:current-user-principal/>
  </D:prop>
</D:propfind>`
		req, _ := http.NewRequest("PROPFIND", "/dav/testuser/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusMultiStatus, resp.StatusCode)
	})

	t.Run("MKCALENDAR /dav/testuser/calendars/work/", func(t *testing.T) {
		req, _ := http.NewRequest("MKCOL", "/dav/testuser/calendars/work/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)
	})

	t.Run("PUT Event", func(t *testing.T) {
		icalData := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//CalCard//EN
BEGIN:VEVENT
UID:event-1@example.com
DTSTAMP:20240122T090000Z
DTSTART:20240122T090000Z
DTEND:20240122T100000Z
SUMMARY:Test Event
END:VEVENT
END:VCALENDAR`
		req, _ := http.NewRequest("PUT", "/dav/testuser/calendars/work/event-1.ics", bytes.NewReader([]byte(icalData)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get("ETag"))
	})

	t.Run("GET Event", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/dav/testuser/calendars/work/event-1.ics", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/calendar")
	})

	t.Run("DELETE Event", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/dav/testuser/calendars/work/event-1.ics", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
	})
}

// TestConditionalDeleteIfMatch verifies that a DELETE carrying an If-Match
// header is enforced by the dispatcher: a stale ETag is rejected with 412 and
// the object survives, while the current ETag deletes successfully. emersion's
// caldav dispatch does not forward If-Match to DELETE, so this must be enforced
// in Handler() before delegation.
func TestConditionalDeleteIfMatch(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{
		UUID:         "test-uuid-ifmatch",
		Email:        "ifmatch@example.com",
		Username:     "ifmatchuser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("ifmatch@example.com:password"))

	// Create the calendar collection.
	{
		req, _ := http.NewRequest("MKCOL", "/dav/ifmatchuser/calendars/work/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	}

	// PUT an event and capture its current ETag.
	const objURL = "/dav/ifmatchuser/calendars/work/event-ifmatch.ics"
	icalData := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//CalCard//EN
BEGIN:VEVENT
UID:event-ifmatch@example.com
DTSTAMP:20240122T090000Z
DTSTART:20240122T090000Z
DTEND:20240122T100000Z
SUMMARY:If-Match Event
END:VEVENT
END:VCALENDAR`
	var currentETag string
	{
		req, _ := http.NewRequest("PUT", objURL, bytes.NewReader([]byte(icalData)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)
		currentETag = resp.Header.Get("ETag")
		require.NotEmpty(t, currentETag)
	}

	t.Run("stale If-Match is rejected with 412 and object survives", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", objURL, nil)
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("If-Match", `"stale-etag-value"`)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusPreconditionFailed, resp.StatusCode)

		// The object must still be there.
		getReq, _ := http.NewRequest("GET", objURL, nil)
		getReq.Header.Set("Authorization", authHeader)
		getResp, err := app.Test(getReq, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, getResp.StatusCode)
	})

	t.Run("matching If-Match deletes successfully", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", objURL, nil)
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("If-Match", currentETag)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// The object must now be gone.
		getReq, _ := http.NewRequest("GET", objURL, nil)
		getReq.Header.Set("Authorization", authHeader)
		getResp, err := app.Test(getReq, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, getResp.StatusCode)
	})
}

// TestCardDAVDeleteAddressBookRevokesShares verifies that deleting an address
// book through the CardDAV backend also hard-deletes every share of that book,
// mirroring the CalDAV collection-delete and REST delete paths. Without the
// revoke, the share row lingers: it surfaces as a ghost entry in the sharee's
// address book list and blocks re-sharing the same (book, user) pair under the
// composite unique index.
func TestCardDAVDeleteAddressBookRevokesShares(t *testing.T) {
	_, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	addressBookRepo := repository.NewAddressBookRepository(db.DB())
	abShareRepo := repository.NewAddressBookShareRepository(db.DB())
	backend := NewCardDAVBackend(addressBookRepo, userRepo, abShareRepo)

	owner := &user.User{
		UUID:     "owner-uuid",
		Email:    "owner@example.com",
		Username: "owner",
		IsActive: true,
	}
	require.NoError(t, userRepo.Create(ctx, owner))

	sharee := &user.User{
		UUID:     "sharee-uuid",
		Email:    "sharee@example.com",
		Username: "sharee",
		IsActive: true,
	}
	require.NoError(t, userRepo.Create(ctx, sharee))

	ab := &addressbook.AddressBook{
		UUID:   "ab-uuid",
		UserID: owner.ID,
		Path:   "contacts",
		Name:   "Contacts",
	}
	require.NoError(t, addressBookRepo.Create(ctx, ab))

	share := &sharing.AddressBookShare{
		UUID:          "share-uuid",
		AddressBookID: ab.ID,
		SharedWithID:  sharee.ID,
		Permission:    "read",
	}
	require.NoError(t, abShareRepo.Create(ctx, share))

	// Precondition: the share exists before the delete.
	before, err := abShareRepo.ListByAddressBookID(ctx, ab.ID)
	require.NoError(t, err)
	require.Len(t, before, 1)

	// Delete the address book as its owner.
	err = backend.DeleteAddressBook(WithUser(ctx, owner), "/dav/owner/addressbooks/contacts/")
	require.NoError(t, err)

	// The share must be gone (hard-deleted, so a default-scoped list sees none).
	after, err := abShareRepo.ListByAddressBookID(ctx, ab.ID)
	require.NoError(t, err)
	assert.Empty(t, after, "deleting the address book must revoke all of its shares")
}

// TestCalDAVPutRejectsUIDChange verifies that a PUT to an existing calendar
// object whose body carries a DIFFERENT UID is rejected with the no-uid-conflict
// precondition (409 Conflict, matching the existing sibling check) rather than
// silently accepted (which would leave the stored uid column and the
// no-uid-conflict lookup stale). A same-UID update must still succeed.
func TestCalDAVPutRejectsUIDChange(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{
		UUID:         "uid-change-cal",
		Email:        "uidcal@example.com",
		Username:     "uidcaluser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	require.NoError(t, userRepo.Create(context.Background(), u))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("uidcal@example.com:password"))

	// Create the calendar collection.
	{
		req, _ := http.NewRequest("MKCOL", "/dav/uidcaluser/calendars/work/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	}

	const objURL = "/dav/uidcaluser/calendars/work/event-uid.ics"
	putEvent := func(uid string) *http.Response {
		body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard//EN\r\n" +
			"BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20240122T090000Z\r\n" +
			"DTSTART:20240122T090000Z\r\nDTEND:20240122T100000Z\r\n" +
			"SUMMARY:UID Change Event\r\nEND:VEVENT\r\nEND:VCALENDAR"
		req, _ := http.NewRequest("PUT", objURL, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		return resp
	}

	// Initial create with UID A.
	resp := putEvent("uid-A@example.com")
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	// A PUT to the same path with a DIFFERENT UID must be rejected via the
	// no-uid-conflict precondition (409 Conflict).
	resp = putEvent("uid-B@example.com")
	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

	// The stored object must still carry the original UID A.
	{
		getReq, _ := http.NewRequest("GET", objURL, nil)
		getReq.Header.Set("Authorization", authHeader)
		getResp, err := app.Test(getReq, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, getResp.StatusCode)
		data, _ := io.ReadAll(getResp.Body)
		assert.Contains(t, string(data), "UID:uid-A@example.com")
		assert.NotContains(t, string(data), "uid-B@example.com")
	}

	// A PUT carrying the SAME UID (an ordinary update) must still succeed.
	resp = putEvent("uid-A@example.com")
	assert.NotEqual(t, fiber.StatusConflict, resp.StatusCode)
	assert.GreaterOrEqual(t, resp.StatusCode, 200)
	assert.Less(t, resp.StatusCode, 300)
}

// TestCardDAVPutRejectsUIDChange is the CardDAV counterpart to
// TestCalDAVPutRejectsUIDChange: a PUT to an existing contact whose body
// carries a different UID must be rejected via the no-uid-conflict precondition
// (409 Conflict), while a same-UID update must still succeed.
func TestCardDAVPutRejectsUIDChange(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{
		UUID:         "uid-change-card",
		Email:        "uidcard@example.com",
		Username:     "uidcarduser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	require.NoError(t, userRepo.Create(context.Background(), u))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("uidcard@example.com:password"))

	// Create the address book collection.
	{
		req, _ := http.NewRequest("MKCOL", "/dav/uidcarduser/addressbooks/contacts/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	}

	const objURL = "/dav/uidcarduser/addressbooks/contacts/contact-uid.vcf"
	putCard := func(uidLine string) *http.Response {
		body := "BEGIN:VCARD\r\nVERSION:3.0\r\n" + uidLine +
			"FN:UID Change Person\r\nN:Person;UID;;;\r\nEND:VCARD"
		req, _ := http.NewRequest("PUT", objURL, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/vcard")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		return resp
	}

	// Initial create with UID A.
	resp := putCard("UID:card-A@example.com\r\n")
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	// A PUT to the same path with a DIFFERENT UID must be rejected via the
	// no-uid-conflict precondition (409 Conflict).
	resp = putCard("UID:card-B@example.com\r\n")
	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

	// The stored object must still carry the original UID A.
	{
		getReq, _ := http.NewRequest("GET", objURL, nil)
		getReq.Header.Set("Authorization", authHeader)
		getResp, err := app.Test(getReq, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, getResp.StatusCode)
		data, _ := io.ReadAll(getResp.Body)
		assert.Contains(t, string(data), "card-A@example.com")
		assert.NotContains(t, string(data), "card-B@example.com")
	}

	// A PUT carrying the SAME UID (an ordinary update) must still succeed.
	resp = putCard("UID:card-A@example.com\r\n")
	assert.NotEqual(t, fiber.StatusConflict, resp.StatusCode)
	assert.GreaterOrEqual(t, resp.StatusCode, 200)
	assert.Less(t, resp.StatusCode, 300)
}

// TestWellKnownRedirect is the regression test for #66: RFC 6764 autodiscovery
// must redirect the well-known context path for ANY method, not just GET. Apple
// clients probe with PROPFIND, which previously 404'd because only GET was routed.
func TestWellKnownRedirect(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	for _, path := range []string{"/.well-known/caldav", "/.well-known/carddav"} {
		for _, method := range []string{"GET", "PROPFIND"} {
			req, _ := http.NewRequest(method, path, nil)
			resp, err := app.Test(req, davTestTimeout)
			require.NoError(t, err)
			assert.Equalf(t, fiber.StatusMovedPermanently, resp.StatusCode,
				"%s %s should 301-redirect (RFC 6764), got %d", method, path, resp.StatusCode)
			assert.Equalf(t, "/dav/", resp.Header.Get("Location"),
				"%s %s should redirect to /dav/", method, path)
		}
	}
}
