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
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusMultiStatus, resp.StatusCode)

		// 2. MKCOL (Write)
		req, _ = http.NewRequest("MKCOL", "/dav/testuser/calendars/test-rw/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err = app.Test(req)
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
		resp, err = app.Test(req)
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
		resp, err := app.Test(req)
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
		resp, err = app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)

		// 3. DELETE (Write) - Forbidden
		req, _ = http.NewRequest("DELETE", "/dav/testuser/calendars/test-rw/rw.ics", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err = app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})

	t.Run("Revoked Credential: Unauthorized", func(t *testing.T) {
		require.NoError(t, credRepo.Revoke(context.Background(), rwCred.ID))

		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("rw-user:rw-pass"))
		req, _ := http.NewRequest("GET", "/dav/testuser/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Invalid Password: Unauthorized", func(t *testing.T) {
		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("ro-user:wrong-pass"))
		req, _ := http.NewRequest("GET", "/dav/testuser/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}
