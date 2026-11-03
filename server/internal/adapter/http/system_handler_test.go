package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The About page reads the server version from the public settings endpoint, so
// the field must be present and carry whatever the build set on version.Version.
func TestSystemHandler_Settings_ReportsVersion(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	handler := NewSystemHandler(cfg, repository.NewUserRepository(db.DB()), nil)
	app.Get("/api/v1/system/settings", handler.Settings)

	original := version.Version
	t.Cleanup(func() { version.Version = original })
	version.Version = "1.2.3-test"

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/system/settings", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	got, ok := body.Data["version"]
	require.True(t, ok, "settings must include a version field")
	assert.Equal(t, "1.2.3-test", got)
}
