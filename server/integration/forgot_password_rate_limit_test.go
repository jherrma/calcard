//go:build integration

package integration_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForgotPasswordRateLimiter is the regression guard for issue #74:
// POST /forgot-password sends a real email per request, so without a dedicated
// throttle it can be abused as a mail-flooding primitive. This test boots a
// dedicated server with RateLimit.Enabled=true (the package-level server leaves
// it off) pointed at an in-process fake SMTP server that counts every message
// handed to it, then hammers /forgot-password for a registered account past the
// limit and asserts two things:
//
//  1. The endpoint starts answering 429 within the burst (the limiter is wired).
//  2. Exactly one email is handed to the sender per allowed (200) request and
//     none for the throttled ones — i.e. the throttle actually stops the flood.
//
// It also checks the anti-enumeration property required by the issue: a
// throttled response for a non-existent account is byte-identical to one for a
// real account, and forgot-password never mails a non-existent account.
func TestForgotPasswordRateLimiter(t *testing.T) {
	smtpAddr, emailCount, stopSMTP := startCountingSMTP(t)
	t.Cleanup(stopSMTP)

	host, port, err := net.SplitHostPort(smtpAddr)
	require.NoError(t, err)

	base, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
		cfg.RateLimit.Enabled = true
		// Point the email service at the fake SMTP server so every send
		// attempt is observable as a counted TCP connection.
		cfg.SMTP = config.SMTPConfig{
			Host: host,
			Port: port,
			From: "noreply@example.test",
		}
	})
	t.Cleanup(shutdown)

	// Register the victim so forgot-password reaches the send path (the usecase
	// short-circuits for unknown emails). Registration itself sends one
	// activation email, so snapshot the counter afterwards.
	registerOn(t, base, "victim@example.test", "victimSecret!123", "Victim User")
	baseline := atomic.LoadInt64(emailCount)

	const attempts = 15
	var statuses []int
	var firstThrottledBody []byte
	for i := 0; i < attempts; i++ {
		status, body := postForgotPasswordOn(t, base, "victim@example.test")
		statuses = append(statuses, status)
		if status == http.StatusTooManyRequests && firstThrottledBody == nil {
			firstThrottledBody = body
		}
	}

	sent := atomic.LoadInt64(emailCount) - baseline

	var okCount, limited int
	for _, s := range statuses {
		switch s {
		case http.StatusOK:
			okCount++
		case http.StatusTooManyRequests:
			limited++
		}
	}

	// (1) The first attempt must get through — we're throttling abuse, not
	// blocking the feature outright.
	assert.NotEqualf(t, http.StatusTooManyRequests, statuses[0],
		"the first forgot-password attempt must not be rate-limited (got: %v)", statuses)

	// (2) The limiter must engage within the burst.
	assert.Greaterf(t, limited, 0,
		"expected at least one 429 in the burst (got: %v)", statuses)
	require.NotNil(t, firstThrottledBody, "expected to capture a 429 response body")

	// (3) Exactly one email per allowed request, zero for throttled ones. This
	// is the crux of the fix: throttled requests never reach the send path.
	assert.Equalf(t, int64(okCount), sent,
		"emails handed to sender (%d) must equal the number of allowed 200 responses (%d); statuses=%v",
		sent, okCount, statuses)

	// (4) And the throttle genuinely prevented some sends.
	assert.Lessf(t, sent, int64(attempts),
		"throttle should prevent some emails: handed %d of %d attempts to the sender", sent, attempts)

	// Anti-enumeration: a throttled request for a non-existent account must be
	// indistinguishable from one for a real account. The IP limiter for
	// 127.0.0.1 is already exhausted, so the very first ghost request is 429.
	ghostStatus, ghostThrottledBody := postForgotPasswordOn(t, base, "ghost@example.test")
	require.Equalf(t, http.StatusTooManyRequests, ghostStatus,
		"expected the non-existent-account request to be throttled as well (got %d)", ghostStatus)
	assert.Equal(t, string(firstThrottledBody), string(ghostThrottledBody),
		"429 response must be byte-identical for existing vs non-existent accounts (no enumeration)")

	// The non-existent account must never have produced an email either.
	assert.Equalf(t, int64(okCount), atomic.LoadInt64(emailCount)-baseline,
		"forgot-password for a non-existent account must not hand any email to the sender")
}

// --- local helpers ---------------------------------------------------------

// postForgotPasswordOn POSTs /auth/forgot-password and returns the status code
// and raw response body.
func postForgotPasswordOn(t *testing.T, base, email string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// startCountingSMTP spins up a minimal in-process SMTP server on a random
// localhost port. It increments the returned counter once per accepted
// connection — i.e. once per email the application's SMTP sender tries to
// deliver — and speaks just enough of the protocol to let net/smtp complete
// (or cleanly abandon) the exchange without hanging. Returns the listen
// address, a pointer to the atomic counter, and a stop function.
func startCountingSMTP(t *testing.T) (addr string, count *int64, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen fake smtp")

	var c int64
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			atomic.AddInt64(&c, 1)
			go serveFakeSMTP(conn)
		}
	}()

	return ln.Addr().String(), &c, func() { _ = ln.Close() }
}

// serveFakeSMTP handles one connection with a tiny SMTP dialogue. It never
// authenticates (it advertises no AUTH extension), which makes net/smtp abandon
// the send right after EHLO — fine for our purposes, since the connection has
// already been counted. It also tolerates a full MAIL/RCPT/DATA flow so the
// helper stays robust across net/smtp versions.
func serveFakeSMTP(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}

	writeLine("220 fake ESMTP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			writeLine("250 fake") // single line -> advertises no extensions (no AUTH)
		case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
			writeLine("250 ok")
		case strings.HasPrefix(cmd, "DATA"):
			writeLine("354 end data with <CR><LF>.<CR><LF>")
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
			}
			writeLine("250 ok queued")
		case strings.HasPrefix(cmd, "QUIT"):
			writeLine("221 bye")
			return
		default:
			writeLine("250 ok")
		}
	}
}
