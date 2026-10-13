//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smtpOn points the server at an SMTP endpoint that refuses connections: the
// server believes mail is configured (so new addresses get an activation link)
// and every send fails fast, which the register path logs and swallows.
func smtpOn(cfg *config.Config) {
	cfg.SMTP = config.SMTPConfig{Host: "127.0.0.1", Port: "1", From: "calcard@example.test"}
}

// TestRegisterWithSMTPUsableBeforeConfirmation — over the real socket, with
// SMTP configured: the register body has the same shape as without SMTP (id,
// username; is_active true, email_verified false), the account logs in and
// reaches the authenticated API before its address is confirmed, and a taken
// address answers 409 as it does without SMTP.
func TestRegisterWithSMTPUsableBeforeConfirmation(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, smtpOn)
	t.Cleanup(shutdown)

	register := func(email, password string) (int, string) {
		t.Helper()
		status, body := rawCall(t, http.MethodPost, base+"/api/v1/auth/register", "",
			map[string]string{"email": email, "password": password, "display_name": "Probe"}, nil)
		return status, string(body)
	}

	const email = "smtp-new@example.test"
	status, body := register(email, "FirstPass123!")
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, `"id":"`)
	assert.Contains(t, body, `"username":"`)
	assert.Contains(t, body, `"is_active":true`)
	assert.Contains(t, body, `"email_verified":false`)

	loginStatus, loginBody := postLoginOn(t, base, email, "FirstPass123!")
	require.Equal(t, http.StatusOK, loginStatus, "an unconfirmed account can log in: %s", loginBody)
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(loginBody, &login))
	meStatus, meBody := rawCall(t, http.MethodGet, base+"/api/v1/users/me", login.Data.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, meStatus, string(meBody))
	assert.Contains(t, string(meBody), `"email_verified":false`)

	dupStatus, dupBody := register(email, "SecondPass123!")
	assert.Equal(t, http.StatusConflict, dupStatus, dupBody)
	// The duplicate did not touch the account's password.
	loginStatus, _ = postLoginOn(t, base, email, "SecondPass123!")
	assert.Equal(t, http.StatusUnauthorized, loginStatus)
}

// TestRegisterDuplicateWithoutSMTP — without SMTP an account is active the
// moment it is created, so a duplicate cannot be dressed up as success: the
// caller would log in right after and find out anyway. The 409 stays.
func TestRegisterDuplicateWithoutSMTP(t *testing.T) {
	const email = "dup-nosmtp@example.test"
	_ = registerAndLogin(t, email, "DupPass123!", "Dup")

	status, body := rawCall(t, http.MethodPost, baseURL+"/api/v1/auth/register", "",
		map[string]string{"email": email, "password": "OtherPass123!", "display_name": "Dup 2"}, nil)
	assert.Equal(t, http.StatusConflict, status, string(body))
}

// TestRegisterRateLimitedWithSMTP — with SMTP every registration sends a
// mail (activation link, or the "someone tried to register" notice to an
// existing owner, which answers 409), so /register carries the same per-email
// limiter as forgot-password; otherwise it is a mail-flooding primitive
// against one inbox.
func TestRegisterRateLimitedWithSMTP(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
		smtpOn(cfg)
		cfg.RateLimit.Enabled = true
		cfg.RateLimit.AuthIPRequests = 20
		cfg.RateLimit.AuthEmailRequests = 2
	})
	t.Cleanup(shutdown)

	var statuses []int
	for i := 0; i < 3; i++ {
		status, _ := rawCall(t, http.MethodPost, base+"/api/v1/auth/register", "",
			map[string]string{"email": "flood@example.test", "password": "FloodPass123!", "display_name": "x"}, nil)
		statuses = append(statuses, status)
	}
	// New address, then the same address again (a 409, still a mail to the
	// owner), then the limiter.
	assert.Equal(t, []int{http.StatusOK, http.StatusConflict, http.StatusTooManyRequests}, statuses)
}
