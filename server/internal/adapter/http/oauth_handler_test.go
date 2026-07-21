package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthHandler_CookieMACTamperDetection(t *testing.T) {
	_, _, cfg := setupTestApp(t)
	cfg.JWT.Secret = "test-secret-for-mac"
	h := &OAuthHandler{cfg: cfg}

	payload := "eyJzdGF0ZSI6ImFiYyIsImFjdGlvbiI6ImxpbmsiLCJ1c2VyX2lkIjo0Mn0"
	mac := h.cookieMAC(payload)

	// Same payload reproduces the same MAC.
	assert.Equal(t, mac, h.cookieMAC(payload), "MAC must be deterministic")

	// A tampered payload (e.g. attacker swaps in a different user_id) yields a
	// different MAC, so hmac.Equal in getContextCookie would reject it.
	tampered := "eyJzdGF0ZSI6ImFiYyIsImFjdGlvbiI6ImxpbmsiLCJ1c2VyX2lkIjo5OX0"
	assert.NotEqual(t, mac, h.cookieMAC(tampered), "tampered payload must not validate")
}

// TestOAuthHandler_GetContextCookieValidatesMAC exercises the actual security
// boundary (getContextCookie's hmac.Equal check, the M14 fix) rather than only
// the cookieMAC helper: a correctly-signed cookie is accepted and its trusted
// fields decoded, while a cookie whose payload was mutated after signing (the
// MAC no longer matches) is rejected.
func TestOAuthHandler_GetContextCookieValidatesMAC(t *testing.T) {
	_, _, cfg := setupTestApp(t)
	cfg.JWT.Secret = "test-secret-for-mac"
	h := &OAuthHandler{cfg: cfg}

	// Route that surfaces getContextCookie's outcome so we drive the real
	// request path (c.Cookies + hmac.Equal), not the helper in isolation.
	app := fiber.New()
	app.Get("/probe", func(c fiber.Ctx) error {
		data, err := h.getContextCookie(c)
		if err != nil {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		return c.JSON(fiber.Map{"user_id": data.UserID, "action": data.Action})
	})

	sign := func(ctx oauthContext) (payload, value string) {
		b, _ := json.Marshal(ctx)
		payload = base64.URLEncoding.EncodeToString(b)
		return payload, payload + "." + h.cookieMAC(payload)
	}

	// A well-formed, correctly-signed cookie is ACCEPTED and its fields decoded.
	validPayload, validValue := sign(oauthContext{State: "abc", Action: "link", UserID: 42})
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_context", Value: validValue})
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode, "valid MAC must be accepted")
	var decoded struct {
		UserID uint   `json:"user_id"`
		Action string `json:"action"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&decoded))
	assert.Equal(t, uint(42), decoded.UserID)
	assert.Equal(t, "link", decoded.Action)

	// Mutate the payload (attacker swaps user_id 42 → 99) but keep the MAC of the
	// ORIGINAL payload: hmac.Equal fails, so the cookie is rejected.
	mutated, _ := json.Marshal(oauthContext{State: "abc", Action: "link", UserID: 99})
	tamperedValue := base64.URLEncoding.EncodeToString(mutated) + "." + h.cookieMAC(validPayload)
	req2 := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req2.AddCookie(&http.Cookie{Name: "oauth_context", Value: tamperedValue})
	resp2, err := app.Test(req2)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp2.StatusCode, "tampered payload must be rejected")
}

func TestOAuthHandler_Lifecycle(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)
	connRepo := repository.NewOAuthConnectionRepository(db.DB())

	u := &user.User{
		Email:         "oauth_test@example.com",
		Username:      "oauthtest",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "oauth-test-uuid",
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	t.Run("Initiate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/google/initiate", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		// Fiber might return 302 or 303
		assert.True(t, resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther, "Expected 302 or 303 status")
		assert.Contains(t, resp.Header.Get("Location"), "https://example.com/auth")
		assert.NotEmpty(t, resp.Cookies())
	})

	t.Run("Link", func(t *testing.T) {
		// The /link route is POST + JWT and is called via an authenticated XHR, so
		// it must return the provider URL as JSON (not a redirect) and set the
		// signed oauth_context cookie the callback reads back.
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/google/link", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Status string `json:"status"`
			Data   struct {
				URL string `json:"url"`
			} `json:"data"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.Equal(t, "ok", respData.Status)
		assert.Contains(t, respData.Data.URL, "https://example.com/auth")

		// The oauth_context cookie must be set so the callback can validate state
		// and know this is a "link" action for the current user.
		var found bool
		for _, ck := range resp.Cookies() {
			if ck.Name == "oauth_context" {
				found = true
			}
		}
		assert.True(t, found, "oauth_context cookie must be set")
	})

	t.Run("List Providers", func(t *testing.T) {
		// Mock a connection first so it's not empty
		conn := &user.OAuthConnection{
			UserID:     u.ID,
			Provider:   "google",
			ProviderID: "fake-sub",
		}
		require.NoError(t, connRepo.Create(context.Background(), conn))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/providers", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			Providers []struct {
				Provider string `json:"provider"`
				Email    string `json:"email"`
			} `json:"providers"`
			HasPassword bool `json:"has_password"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.Equal(t, "google", respData.Providers[0].Provider)
		assert.True(t, respData.HasPassword)
	})

	t.Run("Unlink", func(t *testing.T) {
		// Connection already exists from "List Providers" test
		// But let's create a second authentication method (password) so we can unlink.
		// Unlink UC checks if the user has other providers or a password.
		// In setupTestApp, u has a password "hash".

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/oauth/google", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify deleted
		c, err := connRepo.GetByProvider(context.Background(), u.ID, "google")
		assert.NoError(t, err)
		assert.Nil(t, c)
	})
}
