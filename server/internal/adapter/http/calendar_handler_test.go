package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalendarHandler_CRUD(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "cal@example.com",
		Username:      "caluser",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		DisplayName:   "Cal User",
		UUID:          "cal-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	var createdCalID string

	t.Run("Create Calendar", func(t *testing.T) {
		payload := map[string]string{
			"name":        "Test Calendar",
			"description": "A test calendar",
			"color":       "#ff0000",
			"timezone":    "UTC",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/calendars", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

		var cal calendar.Calendar
		err = json.NewDecoder(resp.Body).Decode(&cal)
		require.NoError(t, err)
		assert.Equal(t, "Test Calendar", cal.Name)
		assert.NotEmpty(t, cal.UUID)
		createdCalID = cal.UUID
	})

	t.Run("Get Calendar", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/calendars/"+createdCalID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var cal calendar.Calendar
		err = json.NewDecoder(resp.Body).Decode(&cal)
		require.NoError(t, err)
		assert.Equal(t, createdCalID, cal.UUID)
		assert.Equal(t, "Test Calendar", cal.Name)
	})

	t.Run("List Calendars", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/calendars", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Calendars []calendar.Calendar `json:"calendars"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(respData.Calendars), 1)
	})

	t.Run("Update Calendar", func(t *testing.T) {
		payload := map[string]string{
			"name":  "Updated Name",
			"color": "#00ff00",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/calendars/"+createdCalID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		// Verify update via response or DB check
		// The handler returns the updated calendar
		var cal calendar.Calendar
		err = json.NewDecoder(resp.Body).Decode(&cal)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", cal.Name)
		assert.Equal(t, "#00ff00", cal.Color)
	})

	t.Run("Delete Calendar", func(t *testing.T) {
		// Create another calendar so we can delete the first one (cannot delete last calendar)
		dummyCal := &calendar.Calendar{
			UUID:     "dummy-cal-uuid",
			UserID:   u.ID,
			Name:     "Dummy Calendar",
			Path:     "dummy-cal.ics",
			Timezone: "UTC",
		}
		require.NoError(t, calRepo.Create(context.Background(), dummyCal))

		payload := map[string]string{
			"confirmation": "DELETE",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/calendars/"+createdCalID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify deleted
		cal, err := calRepo.GetByUUID(context.Background(), createdCalID)
		if err != nil {
			switch err.Error() {
			case "record not found":
				// This is expected if GetByUUID returns error for not found
			default:
				// If it returns nil, that's also fine
				if cal != nil {
					assert.Fail(t, "expected calendar to be deleted, but found it")
				}
			}
		} else {
			// If no error, cal might be nil or returned.
			// GORM GetByUUID might return error "record not found" OR return nil.
			// Given previous error output showed it found the record, and failure was 400.
			// Ideally we want it NOT found.
			assert.Nil(t, cal)
		}
	})
}

func TestCalendarHandler_Export(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "export@example.com",
		Username:      "export",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "export-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	// Create calendar and event to export
	cal := &calendar.Calendar{
		UUID:     "export-cal-uuid",
		UserID:   u.ID,
		Name:     "Export Calendar",
		Path:     "export-cal.ics",
		Timezone: "UTC",
	}
	err = calRepo.Create(context.Background(), cal)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/calendars/"+cal.UUID+"/export", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/calendar")
		// Content-Disposition should be attachment
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "attachment")
	})
}

// TestCalendarHandler_NotFoundIsIdenticalForUnknownAndForeign guards F12:
// PATCH and DELETE on an unknown calendar, and on another user's calendar,
// must answer the same 404 as GET (status AND body, so the two cases cannot be
// told apart), while a genuine validation error on an owned calendar stays 400.
func TestCalendarHandler_NotFoundIsIdenticalForUnknownAndForeign(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	mkUser := func(name string) (*user.User, string) {
		u := &user.User{
			Email:         name + "@example.com",
			Username:      name,
			PasswordHash:  "hash",
			IsActive:      true,
			EmailVerified: true,
			UUID:          name + "-uuid",
		}
		require.NoError(t, userRepo.Create(context.Background(), u))
		token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
		require.NoError(t, err)
		return u, token
	}
	owner, ownerToken := mkUser("f12owner")
	_, strangerToken := mkUser("f12stranger")

	// Two calendars, so the last-calendar guard cannot be what rejects a DELETE.
	for _, uid := range []string{"f12-cal-a", "f12-cal-b"} {
		require.NoError(t, calRepo.Create(context.Background(), &calendar.Calendar{
			UUID: uid, UserID: owner.ID, Name: uid, Path: uid + ".ics", Timezone: "UTC", Color: "#112233",
		}))
	}
	const unknownUUID = "00000000-0000-0000-0000-000000000000"

	call := func(method, uuid, token, body string) (int, string) {
		req := httptest.NewRequest(method, "/api/v1/calendars/"+uuid, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}

	for _, tc := range []struct {
		method, body string
	}{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"name":"hijacked"}`},
		{http.MethodDelete, `{"confirmation":"DELETE"}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			unknownStatus, unknownBody := call(tc.method, unknownUUID, ownerToken, tc.body)
			foreignStatus, foreignBody := call(tc.method, "f12-cal-a", strangerToken, tc.body)
			assert.Equal(t, fiber.StatusNotFound, unknownStatus, "unknown calendar: %s", unknownBody)
			assert.Equal(t, fiber.StatusNotFound, foreignStatus, "another user's calendar: %s", foreignBody)
			assert.Equal(t, unknownBody, foreignBody, "unknown and foreign must be indistinguishable")
		})
	}

	// The owner's calendar is untouched by the stranger's attempts.
	cal, err := calRepo.GetByUUID(context.Background(), "f12-cal-a")
	require.NoError(t, err)
	assert.Equal(t, "f12-cal-a", cal.Name)

	// Real request errors on an owned calendar keep their 400.
	status, body := call(http.MethodPatch, "f12-cal-a", ownerToken, `{"color":"blue"}`)
	assert.Equal(t, fiber.StatusBadRequest, status, "invalid colour: %s", body)
	status, body = call(http.MethodDelete, "f12-cal-a", ownerToken, `{"confirmation":"yes"}`)
	assert.Equal(t, fiber.StatusBadRequest, status, "wrong confirmation: %s", body)
}
