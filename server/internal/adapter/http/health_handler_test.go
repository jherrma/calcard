package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthHandler_Liveness(t *testing.T) {
	app, _, _ := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var respData struct {
		Status string `json:"status"`
	}
	err = json.NewDecoder(resp.Body).Decode(&respData)
	require.NoError(t, err)
	assert.Equal(t, "ok", respData.Status)
}

func TestHealthHandler_Readiness(t *testing.T) {
	app, _, _ := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var respData struct {
		Status string `json:"status"`
		Data   struct {
			Checks map[string]string `json:"checks"`
		} `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&respData)
	require.NoError(t, err)
	assert.Equal(t, "ok", respData.Status)
	assert.Equal(t, "ok", respData.Data.Checks["database"])
}
