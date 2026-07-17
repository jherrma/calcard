package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
)

// stubOIDCServer returns an httptest.Server that serves a minimal OIDC
// discovery document whose issuer matches the server's own URL, so that
// go-oidc's oidc.NewProvider succeeds without a real identity provider.
func stubOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "` + srv.URL + `",
			"authorization_endpoint": "` + srv.URL + `/auth",
			"token_endpoint": "` + srv.URL + `/token",
			"jwks_uri": "` + srv.URL + `/keys",
			"userinfo_endpoint": "` + srv.URL + `/userinfo",
			"id_token_signing_alg_values_supported": ["RS256"]
		}`))
	})
	return srv
}

// TestNewOAuthProviderManager_RedirectURIIsAbsolute verifies that the redirect
// URI sent to the provider is the absolute {base_url}/api/v1/auth/oauth/{name}/
// callback URL, not a relative path (regression test for the OIDC login bug).
func TestNewOAuthProviderManager_RedirectURIIsAbsolute(t *testing.T) {
	srv := stubOIDCServer(t)
	defer srv.Close()

	cfg := &config.OAuthConfig{
		Custom: config.OAuthProviderConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			Issuer:       srv.URL,
		},
	}

	mgr, err := NewOAuthProviderManager(cfg, "https://example.com")
	if err != nil {
		t.Fatalf("NewOAuthProviderManager returned error: %v", err)
	}

	p, err := mgr.GetProvider("custom")
	if err != nil {
		t.Fatalf("GetProvider(custom) failed — provider was not initialized: %v", err)
	}

	got := p.AuthCodeURL("state-123")
	const wantRedirect = "redirect_uri=https%3A%2F%2Fexample.com%2Fapi%2Fv1%2Fauth%2Foauth%2Fcustom%2Fcallback"
	if !strings.Contains(got, wantRedirect) {
		t.Errorf("AuthCodeURL redirect_uri is not the expected absolute URL.\n got: %s\n want substring: %s", got, wantRedirect)
	}
}

// TestNewOAuthProviderManager_TrailingSlashBaseURL ensures a base_url with a
// trailing slash does not produce a doubled slash in the redirect URI, which
// providers reject as a mismatch against the registered URI.
func TestNewOAuthProviderManager_TrailingSlashBaseURL(t *testing.T) {
	srv := stubOIDCServer(t)
	defer srv.Close()

	cfg := &config.OAuthConfig{
		Custom: config.OAuthProviderConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			Issuer:       srv.URL,
		},
	}

	mgr, err := NewOAuthProviderManager(cfg, "https://example.com/")
	if err != nil {
		t.Fatalf("NewOAuthProviderManager returned error: %v", err)
	}
	p, err := mgr.GetProvider("custom")
	if err != nil {
		t.Fatalf("GetProvider(custom) failed: %v", err)
	}
	got := p.AuthCodeURL("state-123")
	if strings.Contains(got, "example.com%2F%2Fapi") {
		t.Errorf("trailing slash in base_url produced a doubled slash in redirect_uri: %s", got)
	}
}
