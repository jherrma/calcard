package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestCalDAVCredentialIntegration(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	credRepo := repository.NewCalDAVCredentialRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("primary-pass"), bcrypt.DefaultCost)

	u := &user.User{
		UUID:         "user-uuid",
		Email:        "user@example.com",
		Username:     "testuser",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	// Create a read-write credential
	rwPassHash, _ := bcrypt.GenerateFromPassword([]byte("rw-pass"), bcrypt.DefaultCost)
	rwCred := &user.CalDAVCredential{
		UUID:         "rw-uuid",
		UserID:       u.ID,
		Name:         "RW Cred",
		Username:     "rw-user",
		PasswordHash: string(rwPassHash),
		Permission:   "read-write",
		CreatedAt:    time.Now(),
	}
	require.NoError(t, credRepo.Create(context.Background(), rwCred))

	// Create a read-only credential
	roPassHash, _ := bcrypt.GenerateFromPassword([]byte("ro-pass"), bcrypt.DefaultCost)
	roCred := &user.CalDAVCredential{
		UUID:         "ro-uuid",
		UserID:       u.ID,
		Name:         "RO Cred",
		Username:     "ro-user",
		PasswordHash: string(roPassHash),
		Permission:   "read",
		CreatedAt:    time.Now(),
	}
	require.NoError(t, credRepo.Create(context.Background(), roCred))

	t.Run("Read-Write Credential: Full Access", func(t *testing.T) {
		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("rw-user:rw-pass"))

		// 1. PROPFIND (Read)
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:">
  <D:prop><D:current-user-principal/></D:prop>
</D:propfind>`
		req, _ := http.NewRequest("PROPFIND", "/dav/testuser/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusMultiStatus, resp.StatusCode)

		// 2. MKCOL (Write)
		req, _ = http.NewRequest("MKCOL", "/dav/testuser/calendars/test-rw/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err = app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

		// 3. PUT Event (Write)
		icalData := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:rw-event@example.com
SUMMARY:RW Event
END:VEVENT
END:VCALENDAR`
		req, _ = http.NewRequest("PUT", "/dav/testuser/calendars/test-rw/rw.ics", bytes.NewReader([]byte(icalData)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		resp, err = app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)
	})

	t.Run("Read-Only Credential: Restricted Access", func(t *testing.T) {
		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("ro-user:ro-pass"))

		// 1. PROPFIND (Read) - Success
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:">
  <D:prop><D:current-user-principal/></D:prop>
</D:propfind>`
		req, _ := http.NewRequest("PROPFIND", "/dav/testuser/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusMultiStatus, resp.StatusCode)

		// 2. PUT Event (Write) - Forbidden
		icalData := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:ro-blocked@example.com
SUMMARY:RO Blocked
END:VEVENT
END:VCALENDAR`
		req, _ = http.NewRequest("PUT", "/dav/testuser/calendars/test-rw/ro.ics", bytes.NewReader([]byte(icalData)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		resp, err = app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)

		// 3. DELETE (Write) - Forbidden
		req, _ = http.NewRequest("DELETE", "/dav/testuser/calendars/test-rw/rw.ics", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err = app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})

	t.Run("Revoked Credential: Unauthorized", func(t *testing.T) {
		require.NoError(t, credRepo.Revoke(context.Background(), rwCred.ID))

		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("rw-user:rw-pass"))
		req, _ := http.NewRequest("GET", "/dav/testuser/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Invalid Password: Unauthorized", func(t *testing.T) {
		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("ro-user:wrong-pass"))
		req, _ := http.NewRequest("GET", "/dav/testuser/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}

// A CalDAV credential's username authenticates in any case, as a CardDAV
// credential's already did (F02). Credential usernames are lowercase letters
// only, so the lookup folds the input to lower case.
func TestCalDAVCredentialUsernameAnyCase(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()

	u := &user.User{UUID: "case-uuid", Email: "case@example.com", Username: "caseowner", PasswordHash: "x", IsActive: true}
	require.NoError(t, repository.NewUserRepository(db.DB()).Create(ctx, u))

	credRepo := repository.NewCalDAVCredentialRepository(db.DB())
	addCred := func(uuid, username, password string) {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		require.NoError(t, err)
		require.NoError(t, credRepo.Create(ctx, &user.CalDAVCredential{
			UUID: uuid, UserID: u.ID, Name: username, Username: username,
			PasswordHash: string(hash), Permission: "read-write", CreatedAt: time.Now(),
		}))
	}
	addCred("cal-uuid", "calsync", "cal-sync-pass")
	addCred("laptop-uuid", "laptopsync", "laptop-pass")

	propfind := func(identifier, password string) int {
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:"><D:prop><D:displayname/></D:prop></D:propfind>`
		req, _ := http.NewRequest("PROPFIND", "/dav/caseowner/calendars/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(identifier+":"+password)))
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Depth", "1")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		return resp.StatusCode
	}

	for _, name := range []string{"calsync", "CALSYNC", "CalSync"} {
		t.Run("username "+name+" authenticates", func(t *testing.T) {
			assert.Equal(t, fiber.StatusMultiStatus, propfind(name, "cal-sync-pass"))
		})
	}
	t.Run("username in another case with a wrong password is rejected", func(t *testing.T) {
		assert.Equal(t, fiber.StatusUnauthorized, propfind("CALSYNC", "wrong-pass"))
	})
	t.Run("username in another case with another credential's password is rejected", func(t *testing.T) {
		assert.Equal(t, fiber.StatusUnauthorized, propfind("CALSYNC", "laptop-pass"))
	})
	t.Run("mixed-case input finds the other lowercase-stored credential", func(t *testing.T) {
		assert.Equal(t, fiber.StatusMultiStatus, propfind("LaptopSync", "laptop-pass"))
	})
}
