package http

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
	"github.com/jherrma/caldav-server/internal/usecase/importexport"
	"github.com/jherrma/caldav-server/internal/usecase/sharing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarErrorMapping guards F28. Calendar routes (CRUD, export, import,
// shares, public link) used to answer storage failures as 400 with the
// database's error text, or as 404 "not found" (export, and any failed
// lookup), and checked the request body before resolving the calendar. Now:
// unknown and foreign calendars are the same 404 whatever the body says,
// rejected requests stay 400 with their message, and a storage failure is a
// 500 that does not describe it (the cause goes to the log).
//
// Storage failures are injected into the test's SQLite database: a BEFORE
// UPDATE trigger that aborts, or a table renamed away and back.
func TestCalendarErrorMapping(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	gdb := db.DB()
	userRepo := repository.NewUserRepository(gdb)
	calRepo := repository.NewCalendarRepository(gdb)
	shareRepo := repository.NewCalendarShareRepository(gdb)
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// The routes setupTestApp does not register.
	importHandler := NewImportHandler(importexport.NewCalendarImportUseCase(calRepo), nil, nil)
	shareHandler := NewCalendarShareHandler(
		sharing.NewCreateCalendarShareUseCase(shareRepo, calRepo, userRepo),
		sharing.NewListCalendarSharesUseCase(shareRepo, calRepo),
		sharing.NewUpdateCalendarShareUseCase(shareRepo, calRepo),
		sharing.NewRevokeCalendarShareUseCase(shareRepo, calRepo),
		calRepo,
	)
	publicHandler := NewCalendarPublicHandler(
		calendarusecase.NewEnablePublicUseCase(calRepo),
		calendarusecase.NewGetPublicStatusUseCase(calRepo),
		calendarusecase.NewRegenerateTokenUseCase(calRepo),
		calRepo, cfg,
	)
	extra := app.Group("/api/v1/calendars", Authenticate(jwtManager, userRepo))
	extra.Post("/:id/import", importHandler.ImportCalendar)
	extra.Get("/:id/shares", shareHandler.List)
	extra.Post("/:id/shares", shareHandler.Create)
	extra.Get("/:id/public", publicHandler.GetPublicStatus)
	extra.Post("/:id/public", publicHandler.EnablePublic)

	mkUser := func(name string) (*user.User, string) {
		u := &user.User{
			Email: name + "@example.com", Username: name, PasswordHash: "hash",
			IsActive: true, EmailVerified: true, UUID: name + "-uuid",
		}
		require.NoError(t, userRepo.Create(context.Background(), u))
		token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
		require.NoError(t, err)
		return u, token
	}
	owner, ownerToken := mkUser("f28owner")
	_, strangerToken := mkUser("f28stranger")
	// Several calendars, so the last-calendar guard never decides a DELETE.
	for _, uid := range []string{"f28-cal-a", "f28-cal-b", "f28-cal-c", "f28-cal-d"} {
		require.NoError(t, calRepo.Create(context.Background(), &calendar.Calendar{
			UUID: uid, UserID: owner.ID, Name: uid, Path: uid + ".ics", Timezone: "UTC", Color: "#112233",
		}))
	}
	const unknownUUID = "00000000-0000-0000-0000-000000000000"

	call := func(method, path, token, ctype, body string) (int, string) {
		req := httptest.NewRequest(method, "/api/v1/calendars/"+path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}
	exec := func(sql string) {
		t.Helper()
		require.NoError(t, gdb.Exec(sql).Error)
	}
	const jsonType = "application/json"

	t.Run("unknown and foreign are 404 whatever the body", func(t *testing.T) {
		for _, tc := range []struct {
			name, method, suffix, ctype, body string
		}{
			{"DELETE with a wrong confirmation", http.MethodDelete, "", jsonType, `{"confirmation":"yes"}`},
			{"DELETE without a body", http.MethodDelete, "", "", ""},
			{"PATCH with malformed JSON", http.MethodPatch, "", jsonType, `{"name":`},
			{"PATCH with an invalid colour", http.MethodPatch, "", jsonType, `{"color":"blue"}`},
			{"import without data", http.MethodPost, "/import", jsonType, `{}`},
			{"import of garbage", http.MethodPost, "/import", "text/calendar", "not an icalendar file"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				unknownStatus, unknownBody := call(tc.method, unknownUUID+tc.suffix, ownerToken, tc.ctype, tc.body)
				foreignStatus, foreignBody := call(tc.method, "f28-cal-a"+tc.suffix, strangerToken, tc.ctype, tc.body)
				assert.Equal(t, fiber.StatusNotFound, unknownStatus, "unknown calendar: %s", unknownBody)
				assert.Equal(t, fiber.StatusNotFound, foreignStatus, "another user's calendar: %s", foreignBody)
				assert.Equal(t, unknownBody, foreignBody, "unknown and foreign must be indistinguishable")
			})
		}
	})

	t.Run("rejected requests on an owned calendar stay 400 with their message", func(t *testing.T) {
		status, body := call(http.MethodPatch, "f28-cal-a", ownerToken, jsonType, `{"color":"blue"}`)
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "invalid hex color format")
		status, body = call(http.MethodDelete, "f28-cal-a", ownerToken, jsonType, `{"confirmation":"yes"}`)
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "please type DELETE")
		status, body = call(http.MethodPost, "f28-cal-a/import", ownerToken, "text/calendar", "not an icalendar file")
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "invalid iCalendar format")
		status, body = call(http.MethodPost, "", ownerToken, jsonType, `{"name":""}`)
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "name cannot be empty")
		status, body = call(http.MethodPost, "f28-cal-a/shares", ownerToken, jsonType,
			`{"user_identifier":"nobody@example.com","permission":"read"}`)
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "user 'nobody@example.com' not found")
	})

	// assert500 checks a storage failure's response: 500, the generic body of
	// the route's error shape, and none of the database's own words.
	assert500 := func(t *testing.T, status int, body, generic string) {
		t.Helper()
		assert.Equal(t, fiber.StatusInternalServerError, status, "body: %s", body)
		assert.Contains(t, body, generic)
		assert.NotContains(t, body, "f28 injected")
		assert.NotContains(t, body, "no such table")
	}
	const restGeneric, subGeneric = `"message":"Internal server error"`, `"error":"internal_error"`

	t.Run("an aborted calendar update is a 500", func(t *testing.T) {
		exec("CREATE TRIGGER f28_block BEFORE UPDATE ON calendars BEGIN SELECT RAISE(ABORT, 'f28 injected failure'); END")
		defer exec("DROP TRIGGER f28_block")

		// The cause reaches the server log, not the client.
		var logBuf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
		defer slog.SetDefault(prev)

		status, body := call(http.MethodPatch, "f28-cal-a", ownerToken, jsonType, `{"name":"renamed"}`)
		assert500(t, status, body, restGeneric)
		assert.Contains(t, logBuf.String(), "f28 injected failure")

		status, body = call(http.MethodPost, "f28-cal-a/public", ownerToken, jsonType, `{"enabled":true}`)
		assert500(t, status, body, subGeneric)
	})

	t.Run("a failing object table is a 500 for export and import", func(t *testing.T) {
		exec("ALTER TABLE calendar_objects RENAME TO calendar_objects_f28")
		defer exec("ALTER TABLE calendar_objects_f28 RENAME TO calendar_objects")

		status, body := call(http.MethodGet, "f28-cal-a/export", ownerToken, "", "")
		assert500(t, status, body, restGeneric)
		status, body = call(http.MethodPost, "f28-cal-a/import", ownerToken, "text/calendar",
			"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//f28//EN\r\nEND:VCALENDAR\r\n")
		assert500(t, status, body, restGeneric)
	})

	t.Run("a failing share table is a 500", func(t *testing.T) {
		exec("ALTER TABLE calendar_shares RENAME TO calendar_shares_f28")
		defer exec("ALTER TABLE calendar_shares_f28 RENAME TO calendar_shares")

		status, body := call(http.MethodGet, "f28-cal-b/shares", ownerToken, "", "")
		assert500(t, status, body, subGeneric)
		status, body = call(http.MethodDelete, "f28-cal-d", ownerToken, jsonType, `{"confirmation":"DELETE"}`)
		assert500(t, status, body, restGeneric)
	})

	t.Run("a failing calendar lookup is a 500, not a 404", func(t *testing.T) {
		exec("ALTER TABLE calendars RENAME TO calendars_f28")
		defer exec("ALTER TABLE calendars_f28 RENAME TO calendars")

		status, body := call(http.MethodGet, "f28-cal-c", ownerToken, "", "")
		assert500(t, status, body, restGeneric)
		status, body = call(http.MethodGet, "f28-cal-c/public", ownerToken, "", "")
		assert500(t, status, body, subGeneric)
	})

	// Nothing above may have left the database broken for the owner.
	status, body := call(http.MethodGet, "f28-cal-c", ownerToken, "", "")
	assert.Equal(t, fiber.StatusOK, status, body)
	assert.True(t, strings.Contains(body, `"name":"f28-cal-c"`), body)
}
