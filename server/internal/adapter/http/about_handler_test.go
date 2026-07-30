package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	aboutuc "github.com/jherrma/caldav-server/internal/usecase/about"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openSourceResponse mirrors the wrapped SuccessResponse envelope of
// GET /api/v1/about/open-source.
type openSourceResponse struct {
	Status string `json:"status"`
	Data   struct {
		Generator string                      `json:"generator"`
		Note      string                      `json:"note"`
		Count     int                         `json:"count"`
		Packages  []aboutuc.OpenSourcePackage `json:"packages"`
	} `json:"data"`
}

// setupAboutRoutes registers the authenticated /api/v1/about group on the shared
// test app (routes.go wires the same group in production) and returns a valid
// access token for a freshly created active user.
func setupAboutRoutes(t *testing.T) (*fiber.App, string) {
	t.Helper()

	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "about@example.com",
		Username:      "aboutuser",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		DisplayName:   "About User",
		UUID:          "about-uuid",
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	handler := NewAboutHandler(aboutuc.NewListOpenSourceUseCase())
	aboutGroup := app.Group("/api/v1/about", Authenticate(jwtManager, userRepo))
	aboutGroup.Get("/open-source", handler.OpenSource)

	return app, token
}

func TestAboutHandler_OpenSource_RequiresAuth(t *testing.T) {
	app, _ := setupAboutRoutes(t)

	t.Run("no token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/about/open-source", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("garbage token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/about/open-source", nil)
		req.Header.Set("Authorization", "Bearer not-a-jwt")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})
}

func TestAboutHandler_OpenSource_Shape(t *testing.T) {
	app, token := setupAboutRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/about/open-source", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var body openSourceResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.Equal(t, "ok", body.Status)
	assert.NotEmpty(t, body.Data.Generator)
	assert.Contains(t, body.Data.Note, "best-effort")
	assert.NotEmpty(t, body.Data.Packages)
	assert.Equal(t, len(body.Data.Packages), body.Data.Count)

	// Every entry must be complete: the frontend renders name/version/license and
	// links the URL, so an empty field would surface as a broken row.
	for _, p := range body.Data.Packages {
		assert.NotEmpty(t, p.Name)
		assert.NotEmpty(t, p.Version)
		assert.NotEmpty(t, p.License)
		assert.True(t, strings.HasPrefix(p.URL, "https://"), "expected https URL for %q, got %q", p.Name, p.URL)
	}

	// Sanity check that the manifest really describes THIS project's deps.
	names := make([]string, 0, len(body.Data.Packages))
	for _, p := range body.Data.Packages {
		names = append(names, p.Name)
	}
	assert.Contains(t, names, "github.com/gofiber/fiber/v3")
}
