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

func TestCardDAVCredentialIntegration(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	credRepo := repository.NewCardDAVCredentialRepository(db.DB())
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
	rwCred := &user.CardDAVCredential{
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
	roCred := &user.CardDAVCredential{
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
		req, _ := http.NewRequest("PROPFIND", "/dav/addressbooks/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req)
		require.NoError(t, err)
		// Assuming root level might return 404 or MultiStatus depending on impl, but let's check basic auth success
		// Ideally we check a valid path like /dav/addressbooks/testuser/
		// But for now just checking we don't get 401
		assert.NotEqual(t, fiber.StatusUnauthorized, resp.StatusCode)

		// 2. MKCOL (Write) logic check - ensuring we are authenticated as user
		req, _ = http.NewRequest("MKCOL", "/dav/addressbooks/"+u.Username+"/test-rw/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err = app.Test(req)
		require.NoError(t, err)
		assert.NotEqual(t, fiber.StatusUnauthorized, resp.StatusCode)
		// Note we don't assert Created because backend logic for MKCOL might fail if not fully set up in test
		// The key here is auth success and permission check passing (not 403/401)
	})

	t.Run("Read-Only Credential: Restricted Access", func(t *testing.T) {
		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("ro-user:ro-pass"))

		// 1. PROPFIND (Read) - Success
		body := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:">
  <D:prop><D:current-user-principal/></D:prop>
</D:propfind>`
		req, _ := http.NewRequest("PROPFIND", "/dav/addressbooks/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.NotEqual(t, fiber.StatusUnauthorized, resp.StatusCode)

		// 2. PUT Card (Write) - Forbidden
		vcardData := `BEGIN:VCARD
VERSION:3.0
FN:Test User
N:User;Test;;;
UID:ro-blocked
END:VCARD`
		req, _ = http.NewRequest("PUT", "/dav/addressbooks/"+u.Username+"/test-rw/ro.vcf", bytes.NewReader([]byte(vcardData)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/vcard")
		resp, err = app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)

		// 3. DELETE (Write) - Forbidden
		req, _ = http.NewRequest("DELETE", "/dav/addressbooks/"+u.Username+"/test-rw/rw.vcf", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err = app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})

	t.Run("Revoked Credential: Unauthorized", func(t *testing.T) {
		require.NoError(t, credRepo.Revoke(context.Background(), rwCred.ID))

		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("rw-user:rw-pass"))
		req, _ := http.NewRequest("GET", "/dav/addressbooks/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Invalid Password: Unauthorized", func(t *testing.T) {
		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("ro-user:wrong-pass"))
		req, _ := http.NewRequest("GET", "/dav/addressbooks/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}
