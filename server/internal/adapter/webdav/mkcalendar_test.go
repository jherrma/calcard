package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestMKCALENDARPreservesDisplayName is the regression test for #42: the
// MKCALENDAR body was dropped, so client-created calendars lost their name
// forever (emersion's PROPPATCH is a 501, so it couldn't be set afterward).
// The body is now transformed into the extended-MKCOL shape emersion parses.
func TestMKCALENDARPreservesDisplayName(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
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

	mkcalendar := func(path, body string) int {
		req, _ := http.NewRequest("MKCALENDAR", path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		if body != "" {
			req.Header.Set("Content-Type", "application/xml")
		}
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		return resp.StatusCode
	}

	t.Run("named calendar keeps its name (with XML entity)", func(t *testing.T) {
		body := `<C:mkcalendar xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:D="DAV:">` +
			`<D:set><D:prop><D:displayname>Team &amp; Family</D:displayname></D:prop></D:set></C:mkcalendar>`
		require.Equal(t, fiber.StatusCreated, mkcalendar("/dav/testuser/calendars/team/", body))

		cal, err := calRepo.GetByPath(context.Background(), u.ID, "team")
		require.NoError(t, err)
		require.NotNil(t, cal)
		assert.Equal(t, "Team & Family", cal.Name, "MKCALENDAR displayname must survive creation")
	})

	t.Run("empty MKCALENDAR body still creates the calendar", func(t *testing.T) {
		require.Equal(t, fiber.StatusCreated, mkcalendar("/dav/testuser/calendars/nameless/", ""))
		cal, err := calRepo.GetByPath(context.Background(), u.ID, "nameless")
		require.NoError(t, err)
		require.NotNil(t, cal)
		assert.Empty(t, cal.Name, "empty MKCALENDAR body yields an empty name (unchanged behavior)")
	})
}
