//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDerivedBaseURL covers a deployment that never set CALDAV_BASE_URL. The
// URLs the API hands back (app-password server_url, public calendar feed URL)
// used to be the config default "http://localhost:8080" — wrong for every
// client not running on the server itself. They now come from the request the
// client actually made, and the feed URL handed out really serves the feed.
// Forwarded headers are ignored unless explicitly trusted.
func TestDerivedBaseURL(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, func(c *config.Config) { c.BaseURL = "" })
	t.Cleanup(shutdown)
	host := strings.TrimPrefix(base, "http://")

	email, password := "baseurl@example.test", "BaseUrlSecret!123"
	registerOn(t, base, email, password, "Base URL")
	token := loginTokenOn(t, base, email, password)

	t.Run("app password server_url follows the Host header", func(t *testing.T) {
		var resp struct {
			Data struct {
				Credentials struct {
					ServerURL string `json:"server_url"`
				} `json:"credentials"`
			} `json:"data"`
		}
		code := callOn(t, base, http.MethodPost, "/api/v1/app-passwords/", token, map[string]any{
			"name": "phone", "scopes": []string{"caldav", "carddav"},
		}, map[string]string{"Host": "nas.example:8751"}, &resp)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "http://nas.example:8751", resp.Data.Credentials.ServerURL)
	})

	t.Run("forwarded headers are ignored by default", func(t *testing.T) {
		var resp struct {
			Data struct {
				Credentials struct {
					ServerURL string `json:"server_url"`
				} `json:"credentials"`
			} `json:"data"`
		}
		code := callOn(t, base, http.MethodPost, "/api/v1/app-passwords/", token, map[string]any{
			"name": "spoof", "scopes": []string{"caldav"},
		}, map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "attacker.example"}, &resp)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, base, resp.Data.Credentials.ServerURL)
	})

	t.Run("public_url is reachable as handed out", func(t *testing.T) {
		var cal struct {
			UUID string `json:"uuid"`
		}
		code := callOn(t, base, http.MethodPost, "/api/v1/calendars/", token,
			map[string]string{"name": "Shared", "color": "#123456"}, nil, &cal)
		require.Equal(t, http.StatusCreated, code)
		require.NotEmpty(t, cal.UUID)

		var pub struct {
			PublicURL *string `json:"public_url"`
		}
		code = callOn(t, base, http.MethodPost, "/api/v1/calendars/"+cal.UUID+"/public", token,
			map[string]bool{"enabled": true}, nil, &pub)
		require.Equal(t, http.StatusOK, code)
		require.NotNil(t, pub.PublicURL)
		assert.True(t, strings.HasPrefix(*pub.PublicURL, base+"/public/calendar/"), "got %s", *pub.PublicURL)

		status, _, body := rawGet(t, *pub.PublicURL)
		require.Equal(t, http.StatusOK, status)
		assert.Contains(t, string(body), "BEGIN:VCALENDAR")

		// Status and regenerate derive the same way.
		code = callOn(t, base, http.MethodGet, "/api/v1/calendars/"+cal.UUID+"/public", token, nil,
			map[string]string{"Host": "nas.example:8751"}, &pub)
		require.Equal(t, http.StatusOK, code)
		require.NotNil(t, pub.PublicURL)
		assert.True(t, strings.HasPrefix(*pub.PublicURL, "http://nas.example:8751/public/calendar/"), "got %s", *pub.PublicURL)

		code = callOn(t, base, http.MethodPost, "/api/v1/calendars/"+cal.UUID+"/public/regenerate", token, nil, nil, &pub)
		require.Equal(t, http.StatusOK, code)
		require.NotNil(t, pub.PublicURL)
		assert.True(t, strings.HasPrefix(*pub.PublicURL, "http://"+host+"/public/calendar/"), "got %s", *pub.PublicURL)
	})
}

// TestDerivedBaseURLTrustedProxy: behind a reverse proxy that terminates TLS,
// the operator opts in to X-Forwarded-Proto / X-Forwarded-Host.
func TestDerivedBaseURLTrustedProxy(t *testing.T) {
	base, shutdown := bootServerWithConfig(t, func(c *config.Config) {
		c.BaseURL = ""
		c.TrustForwardedHeaders = true
	})
	t.Cleanup(shutdown)

	email, password := "baseurl-proxy@example.test", "BaseUrlSecret!123"
	registerOn(t, base, email, password, "Base URL Proxy")
	token := loginTokenOn(t, base, email, password)

	var resp struct {
		Data struct {
			Credentials struct {
				ServerURL string `json:"server_url"`
			} `json:"credentials"`
		} `json:"data"`
	}
	code := callOn(t, base, http.MethodPost, "/api/v1/app-passwords/", token, map[string]any{
		"name": "laptop", "scopes": []string{"caldav"},
	}, map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "cal.example.com"}, &resp)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "https://cal.example.com", resp.Data.Credentials.ServerURL)
}

// loginTokenOn logs in against the given server and returns the access token.
func loginTokenOn(t *testing.T, base, email, password string) string {
	t.Helper()
	status, body := postLoginOn(t, base, email, password)
	require.Equalf(t, http.StatusOK, status, "login: %s", body)
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &login))
	require.NotEmpty(t, login.Data.AccessToken)
	return login.Data.AccessToken
}

// callOn sends a JSON request to an alternate server; a "Host" entry in
// headers overrides the Host header the client sends.
func callOn(t *testing.T, base, method, path, token string, body any, headers map[string]string, out any) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if out != nil && len(raw) > 0 {
		require.NoErrorf(t, json.Unmarshal(raw, out), "decode %s %s: %s", method, path, raw)
	}
	return resp.StatusCode
}
