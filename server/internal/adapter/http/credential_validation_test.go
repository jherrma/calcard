package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F31: credential creation used to accept blank names, unknown app-password
// scopes and DAV-credential expiries in the past.

func credentialTestUser(t *testing.T) (*fiber.App, string, *user.User) {
	t.Helper()
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)
	u := &user.User{Email: "f31@example.com", Username: "fthirtyone", PasswordHash: "h",
		IsActive: true, EmailVerified: true, UUID: "f31-uuid"}
	require.NoError(t, userRepo.Create(context.Background(), u))
	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)
	return app, token, u
}

func postJSON(t *testing.T, app *fiber.App, token, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

func TestAppPasswordHandler_CreateValidatesInput(t *testing.T) {
	app, token, _ := credentialTestUser(t)

	refused := []struct {
		name, body, msg string
	}{
		{"empty name", `{"name":"","scopes":["caldav"]}`, "name is required"},
		{"blank name", `{"name":"   ","scopes":["caldav"]}`, "name is required"},
		{"missing name", `{"scopes":["caldav"]}`, "name is required"},
		{"name over 100 characters", `{"name":"` + strings.Repeat("x", 101) + `","scopes":["caldav"]}`, "at most 100 characters"},
		{"unknown scope", `{"name":"x","scopes":["admin"]}`, "unknown scope 'admin'"},
		{"unknown scope next to a valid one", `{"name":"x","scopes":["caldav","CardDAV"]}`, "unknown scope 'CardDAV'"},
		{"empty scope list", `{"name":"x","scopes":[]}`, "at least one scope"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			code, body := postJSON(t, app, token, "/api/v1/app-passwords", tc.body)
			assert.Equal(t, fiber.StatusBadRequest, code, body)
			assert.Contains(t, body, tc.msg)
		})
	}

	type created struct {
		Data struct {
			Name   string   `json:"name"`
			Scopes []string `json:"scopes"`
		} `json:"data"`
	}
	accepted := []struct {
		name, body, wantName string
		wantScopes           []string
	}{
		{"trimmed name, both scopes", `{"name":"  Phone  ","scopes":["caldav","carddav"]}`, "Phone", []string{"caldav", "carddav"}},
		{"scopes omitted default to both", `{"name":"Laptop"}`, "Laptop", []string{"caldav", "carddav"}},
		{"scopes null default to both", `{"name":"Tablet","scopes":null}`, "Tablet", []string{"caldav", "carddav"}},
		{"duplicates collapse", `{"name":"Cal only","scopes":["caldav","caldav"]}`, "Cal only", []string{"caldav"}},
		{"100 multi-byte characters fit", `{"name":"` + strings.Repeat("ä", 100) + `","scopes":["carddav"]}`, strings.Repeat("ä", 100), []string{"carddav"}},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			code, body := postJSON(t, app, token, "/api/v1/app-passwords", tc.body)
			require.Equal(t, fiber.StatusOK, code, body)
			var res created
			require.NoError(t, json.Unmarshal([]byte(body), &res))
			assert.Equal(t, tc.wantName, res.Data.Name)
			assert.Equal(t, tc.wantScopes, res.Data.Scopes)
		})
	}
}

func TestDAVCredentialHandlers_CreateValidatesInput(t *testing.T) {
	app, token, _ := credentialTestUser(t)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	for _, kind := range []string{"caldav", "carddav"} {
		path := "/api/v1/" + kind + "-credentials"
		t.Run(kind, func(t *testing.T) {
			code, body := postJSON(t, app, token, path,
				`{"name":"x","username":"`+kind+`past","password":"LongEnough_123","expires_at":"`+past+`"}`)
			assert.Equal(t, fiber.StatusBadRequest, code, "an already expired credential must be refused: "+body)
			assert.Contains(t, body, "expires_at must be in the future")

			code, body = postJSON(t, app, token, path,
				`{"name":"   ","username":"`+kind+`blank","password":"LongEnough_123"}`)
			assert.Equal(t, fiber.StatusBadRequest, code, "a blank name must be refused: "+body)
			assert.Contains(t, body, "name is required")

			code, body = postJSON(t, app, token, path,
				`{"name":"  Phone  ","username":"`+kind+`future","password":"LongEnough_123","expires_at":"`+future+`"}`)
			require.Equal(t, fiber.StatusCreated, code, body)
			var res struct {
				Name      string     `json:"name"`
				ExpiresAt *time.Time `json:"expires_at"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &res))
			assert.Equal(t, "Phone", res.Name, "the stored name is trimmed")
			require.NotNil(t, res.ExpiresAt)

			code, body = postJSON(t, app, token, path,
				`{"name":"no expiry","username":"`+kind+`noexp","password":"LongEnough_123"}`)
			assert.Equal(t, fiber.StatusCreated, code, "expires_at stays optional: "+body)
		})
	}
}
