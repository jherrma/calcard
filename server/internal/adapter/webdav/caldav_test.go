package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func setupTestApp(t *testing.T) (*fiber.App, database.Database, *config.Config) {
	dataDir, err := os.MkdirTemp("", "caldav-test-*")
	require.NoError(t, err)

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
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	caldavBackend := NewCalDAVBackend(calendarRepo, userRepo)
	addressBookRepo := repository.NewAddressBookRepository(db.DB())
	carddavBackend := NewCardDAVBackend(addressBookRepo, userRepo)
	davHandler := NewHandler(caldavBackend, carddavBackend, userRepo, appPwdRepo, caldavCredRepo, jwtManager)

	app.Get("/.well-known/caldav", WellKnownCalDAVRedirect)
	app.Get("/.well-known/carddav", WellKnownCardDAVRedirect)
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
		resp, err := app.Test(req)
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
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusMultiStatus, resp.StatusCode)
	})

	t.Run("MKCALENDAR /dav/testuser/calendars/work/", func(t *testing.T) {
		req, _ := http.NewRequest("MKCOL", "/dav/testuser/calendars/work/", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req)
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
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get("ETag"))
	})

	t.Run("GET Event", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/dav/testuser/calendars/work/event-1.ics", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/calendar")
	})

	t.Run("DELETE Event", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/dav/testuser/calendars/work/event-1.ics", nil)
		req.Header.Set("Authorization", authHeader)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
	})
}
