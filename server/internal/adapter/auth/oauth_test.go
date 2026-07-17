package auth

import (
	"bytes"
	"log"
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

// TestNewOAuthProviderManager_LogsAndSkipsFailedProvider verifies that when a
// configured provider fails OIDC discovery, the manager (a) still constructs
// without error, (b) omits the failed provider, and (c) logs a loud ERROR line
// naming the provider — instead of silently swallowing the failure.
func TestNewOAuthProviderManager_LogsAndSkipsFailedProvider(t *testing.T) {
	// A discovery endpoint that returns 500 makes oidc.NewProvider fail fast
	// (no network hang), simulating a misconfigured/unreachable IdP at boot.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	}()

	cfg := &config.OAuthConfig{
		Custom: config.OAuthProviderConfig{
			ClientID:     "id",
			ClientSecret: "super-secret-value",
			Issuer:       srv.URL,
		},
	}

	mgr, err := NewOAuthProviderManager(cfg, "https://example.com")
	if err != nil {
		t.Fatalf("manager construction must not fail when a provider fails to init: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager must be non-nil so the remaining providers stay available")
	}
	if _, err := mgr.GetProvider("custom"); err == nil {
		t.Error("expected the failed provider to be absent from the manager")
	}
	if got := mgr.ListProviders(); len(got) != 0 {
		t.Errorf("expected no providers, got %v", got)
	}

	logged := buf.String()
	if !strings.Contains(logged, "custom") || !strings.Contains(strings.ToUpper(logged), "ERROR") {
		t.Errorf("expected an ERROR log line naming the failed provider, got: %q", logged)
	}
	if strings.Contains(logged, "super-secret-value") {
		t.Errorf("log must not leak the client secret, got: %q", logged)
	}
}

// TestIsAzureMultiTenantIssuer covers the predicate that decides when go-oidc's
// strict issuer-equality check must be relaxed. Azure AD's multi-tenant
// endpoints return a templated issuer, so the check has to be skipped for them
// — but only for them; every other issuer keeps the strict guard.
func TestIsAzureMultiTenantIssuer(t *testing.T) {
	cases := []struct {
		issuer string
		want   bool
	}{
		{"https://login.microsoftonline.com/common/v2.0", true},
		{"https://login.microsoftonline.com/organizations/v2.0", true},
		{"https://login.microsoftonline.com/consumers/v2.0", true},
		// Single-tenant (a real tenant GUID) reports a concrete issuer that
		// matches, so the strict check must stay on.
		{"https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/v2.0", false},
		// Non-Microsoft providers must never be relaxed.
		{"https://accounts.google.com", false},
		{"https://keycloak.example.com/realms/myrealm", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isAzureMultiTenantIssuer(c.issuer); got != c.want {
			t.Errorf("isAzureMultiTenantIssuer(%q) = %v, want %v", c.issuer, got, c.want)
		}
	}
}
