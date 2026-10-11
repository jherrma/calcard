//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smtpOn points the server at an SMTP endpoint that refuses connections: the
// server believes mail is configured (so registrations need confirmation)
// and every send fails fast, which the register path logs and swallows.
func smtpOn(cfg *config.Config) {
	cfg.SMTP = config.SMTPConfig{Host: "127.0.0.1", Port: "1", From: "calcard@example.test"}
}

// TestRegisterDoesNotEnumerateWithSMTP — P01 over the real socket. With SMTP
// configured, registering a taken address answers byte-for-byte like
// registering a free one, and the follow-up login (with the password the
// caller just chose) answers identically for "you created an unconfirmed
// account" and "that address belonged to someone else".
func TestRegisterDoesNotEnumerateWithSMTP(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, smtpOn)
	t.Cleanup(shutdown)

	register := func(email, password string) (int, string) {
		t.Helper()
		status, body := rawCall(t, http.MethodPost, base+"/api/v1/auth/register", "",
			map[string]string{"email": email, "password": password, "display_name": "Probe"}, nil)
		return status, string(body)
	}

	const email = "enum-target@example.test"
	firstStatus, firstBody := register(email, "FirstPass123!")
	require.Equal(t, http.StatusOK, firstStatus, firstBody)
	assert.NotContains(t, firstBody, `"id"`, "a pending registration exposes no stored-row fields")
	assert.NotContains(t, firstBody, `"username"`)
	assert.Contains(t, firstBody, `"is_active":false`)

	// The address is now taken. A second registration — different password,
	// same display name — must be indistinguishable from the first.
	secondStatus, secondBody := register(email, "SecondPass123!")
	assert.Equal(t, firstStatus, secondStatus)
	assert.Equal(t, firstBody, secondBody, "a taken address must answer exactly like a free one")

	// Login with the first password: the account exists but is unconfirmed.
	// Login with the second: wrong password. Same answer for both.
	unconfirmedStatus, unconfirmedBody := postLoginOn(t, base, email, "FirstPass123!")
	wrongStatus, wrongBody := postLoginOn(t, base, email, "SecondPass123!")
	assert.Equal(t, http.StatusUnauthorized, unconfirmedStatus)
	assert.Equal(t, unconfirmedStatus, wrongStatus)
	assert.Equal(t, string(unconfirmedBody), string(wrongBody))
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
// existing owner), so /register carries the same per-email limiter as
// forgot-password; otherwise it is a mail-flooding primitive against one inbox.
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
	assert.Equal(t, []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}, statuses)
}
