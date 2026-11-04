package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisterSeedsACalendarAndATaskList pins the defaults of an account made
// by POST /auth/register (#170) through the real register use case and
// repositories: the calendar list shows exactly "Personal", an event calendar
// (kind events, VEVENT), and "Tasks", a task list (kind tasks, VTODO).
func TestRegisterSeedsACalendarAndATaskList(t *testing.T) {
	app, _, _ := setupTestApp(t)

	call := func(method, path, bearer string, payload any) (int, []byte) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, raw
	}

	status, raw := call(http.MethodPost, "/api/v1/auth/register", "",
		map[string]string{"email": "defaults@example.com", "password": "Defaults123!", "display_name": "Defaults"})
	require.Equal(t, fiber.StatusOK, status, string(raw))
	status, raw = call(http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"email": "defaults@example.com", "password": "Defaults123!"})
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &login))
	require.NotEmpty(t, login.Data.AccessToken)

	status, raw = call(http.MethodGet, "/api/v1/calendars", login.Data.AccessToken, nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var list struct {
		Calendars []struct {
			Name                string `json:"name"`
			Kind                string `json:"kind"`
			SupportedComponents string `json:"supported_components"`
			Path                string `json:"path"`
			UUID                string `json:"uuid"`
			Subscribed          bool   `json:"subscribed"`
		} `json:"calendars"`
	}
	require.NoError(t, json.Unmarshal(raw, &list))
	type row struct{ name, kind, components string }
	got := []row{}
	for _, c := range list.Calendars {
		got = append(got, row{c.Name, c.Kind, c.SupportedComponents})
		assert.Equal(t, c.UUID+".ics", c.Path, c.Name)
		assert.False(t, c.Subscribed, c.Name)
	}
	assert.ElementsMatch(t, []row{
		{"Personal", "events", "VEVENT"},
		{"Tasks", "tasks", "VTODO"},
	}, got, string(raw))
}

// TestOAuthSignUpGetsNoDefaultCalendar pins that an account provisioned by the
// OAuth callback still gets no calendar at all (#170): like any user without a
// calendar that stores VTODO, it sees the Tasks empty state. The mock provider
// reports a verified address nobody has registered, so the callback creates a
// new account.
func TestOAuthSignUpGetsNoDefaultCalendar(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())

	h := &OAuthHandler{cfg: cfg}
	b, _ := json.Marshal(oauthContext{State: "signup-state", Action: "login"})
	payload := base64.URLEncoding.EncodeToString(b)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/oauth/google/callback?code=auth_code&state=signup-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_context", Value: payload + "." + h.cookieMAC(payload)})
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.True(t,
		resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther,
		"expected a redirect, got %d", resp.StatusCode)
	require.Contains(t, resp.Header.Get("Location"), "access_token=", "the account was provisioned and signed in")

	u, err := userRepo.GetByEmail(ctx, "oauth@example.com")
	require.NoError(t, err)
	require.NotNil(t, u, "the callback provisioned an account")
	cals, err := calRepo.ListByUserID(ctx, u.ID)
	require.NoError(t, err)
	assert.Empty(t, cals, "an OAuth-provisioned account gets no default calendar")

	var all int64
	require.NoError(t, db.DB().Model(&user.User{}).Count(&all).Error)
	assert.Equal(t, int64(1), all, "the OAuth account is the only one")
}
