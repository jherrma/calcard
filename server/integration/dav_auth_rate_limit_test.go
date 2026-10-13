//go:build integration

package integration_test

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDAVBasicAuthFailedAttemptLimiter (#193) — DAV Basic auth accepts the
// account password on every request, so FAILED attempts are limited per
// identifier (the user name or e-mail address, trimmed and lower-cased) with
// the per-account login allowance. Successful attempts and the credential-less
// 401 challenge are not counted, and there is no per-IP allowance, so a
// syncing client on the same IP keeps working while a guesser is throttled.
// Every request here comes from 127.0.0.1.
func TestDAVBasicAuthFailedAttemptLimiter(t *testing.T) {
	// The per-IP auth allowance is set low: the DAV limiter must not use it.
	const ipMax, identifierMax = 4, 3
	base, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
		cfg.RateLimit.Enabled = true
		cfg.RateLimit.AuthIPRequests = ipMax
		cfg.RateLimit.AuthEmailRequests = identifierMax
	})
	t.Cleanup(shutdown)

	const (
		victim       = "dav-victim@example.test"
		victimPass   = "VictimPass123!"
		syncer       = "dav-syncer@example.test"
		syncerPass   = "SyncerPass123!"
		wrongPass    = "WrongPass123!"
		accountLimit = "Too many login attempts for this account"
	)
	registerOn(t, base, victim, victimPass, "Victim")
	registerOn(t, base, syncer, syncerPass, "Syncer")

	// propfind sends a Depth 0 PROPFIND on /dav/; with credentials == nil it
	// carries no Authorization header at all.
	propfind := func(credentials *[2]string) (int, http.Header, string) {
		t.Helper()
		req, err := http.NewRequest("PROPFIND", base+"/dav/", nil)
		require.NoError(t, err)
		req.Header.Set("Depth", "0")
		if credentials != nil {
			raw := credentials[0] + ":" + credentials[1]
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(raw)))
		}
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(body)
	}
	as := func(user, pass string) *[2]string { return &[2]string{user, pass} }

	// More successful requests than both allowances together: none is counted.
	for i := 0; i < ipMax+identifierMax+1; i++ {
		status, _, body := propfind(as(syncer, syncerPass))
		require.Equalf(t, http.StatusMultiStatus, status, "successful request %d must not be throttled: %s", i+1, body)
	}
	// Neither is the 401 challenge a client gets before it sends credentials.
	for i := 0; i < ipMax+1; i++ {
		status, _, _ := propfind(nil)
		require.Equalf(t, http.StatusUnauthorized, status, "credential-less request %d", i+1)
	}

	// Wrong passwords for the victim: identifierMax 401s, then the per-account 429.
	for i := 0; i < identifierMax; i++ {
		status, _, body := propfind(as(victim, wrongPass))
		require.Equalf(t, http.StatusUnauthorized, status, "failed attempt %d is still answered 401: %s", i+1, body)
	}
	status, header, body := propfind(as(victim, wrongPass))
	require.Equal(t, http.StatusTooManyRequests, status, body)
	assert.Contains(t, body, `"status":"error"`, "same 429 shape as the login limiters")
	assert.Contains(t, body, accountLimit)
	assert.NotEmpty(t, header.Get("Retry-After"))

	// While the identifier is spent its correct password is refused as well (a
	// different answer would confirm the guess), and so are case and
	// whitespace variants of it, which reach the same account.
	status, _, _ = propfind(as(victim, victimPass))
	assert.Equal(t, http.StatusTooManyRequests, status, "the correct password must not be told apart while the identifier is spent")
	status, _, _ = propfind(as("  DAV-Victim@Example.TEST ", wrongPass))
	assert.Equal(t, http.StatusTooManyRequests, status, "a case/whitespace variant shares the identifier's bucket")

	// A correctly authenticated client on the same IP is not throttled.
	status, _, body = propfind(as(syncer, syncerPass))
	require.Equal(t, http.StatusMultiStatus, status, "a valid client on the same IP must keep working: %s", body)

	// No per-IP allowance on DAV: spraying more fresh identifiers from this IP
	// than auth_ip_requests allows is answered 401 every time, and the valid
	// client behind the same IP still gets in.
	for i := 0; i < ipMax+1; i++ {
		status, _, body := propfind(as(fmt.Sprintf("spray-%d@example.test", i), wrongPass))
		require.Equalf(t, http.StatusUnauthorized, status, "spray attempt %d must not hit a per-IP limit: %s", i+1, body)
	}
	status, _, body = propfind(as(syncer, syncerPass))
	assert.Equal(t, http.StatusMultiStatus, status, "a valid client behind the spraying IP must keep working: %s", body)
}

// TestDAVBasicAuthConcurrentCorrectLogins (#193) — only settled failures count
// against the per-identifier allowance, so correct Basic logins in flight at
// the same time never spend it: a client opening more parallel connections
// than auth_email_requests (each a bcrypt check of ~250 ms at cost 12) is let
// in on every one. Checks in flight are capped separately at 2×
// auth_email_requests per identifier; the burst here is exactly that cap.
// Afterwards the identifier still has its whole failure budget.
func TestDAVBasicAuthConcurrentCorrectLogins(t *testing.T) {
	const identifierMax = 4
	const concurrent = 2 * identifierMax
	base, shutdown := bootServerWithConfig(t, func(cfg *config.Config) {
		cfg.RateLimit.Enabled = true
		cfg.RateLimit.AuthIPRequests = 20
		cfg.RateLimit.AuthEmailRequests = identifierMax
	})
	t.Cleanup(shutdown)

	const (
		email     = "dav-parallel@example.test"
		password  = "ParallelPass123!"
		wrongPass = "WrongPass123!"
	)
	registerOn(t, base, email, password, "Parallel")

	// propfind is safe to call from several goroutines: it reports errors
	// instead of failing the test.
	propfind := func(pass string) (int, string, error) {
		req, err := http.NewRequest("PROPFIND", base+"/dav/", nil)
		if err != nil {
			return 0, "", err
		}
		req.Header.Set("Depth", "0")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(email+":"+pass)))
		resp, err := httpClient.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), nil
	}

	type result struct {
		status int
		body   string
		err    error
	}
	results := make([]result, concurrent)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			status, body, err := propfind(password)
			results[i] = result{status, body, err}
		}(i)
	}
	close(start)
	wg.Wait()
	for i, r := range results {
		require.NoError(t, r.err)
		assert.Equalf(t, http.StatusMultiStatus, r.status, "concurrent correct login %d of %d must be let in: %s", i+1, concurrent, r.body)
	}

	// The burst counted nothing: identifierMax wrong passwords are still 401s,
	// and only the next attempt is the 429.
	for i := 0; i < identifierMax; i++ {
		status, body, err := propfind(wrongPass)
		require.NoError(t, err)
		require.Equalf(t, http.StatusUnauthorized, status, "failed attempt %d is within the budget: %s", i+1, body)
	}
	status, body, err := propfind(password)
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, status, "a spent identifier refuses the correct password too: %s", body)
}
