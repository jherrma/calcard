package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"golang.org/x/oauth2"
)

// newAssumeVerifiedStub returns a stub OIDC issuer whose /userinfo endpoint
// omits email_verified entirely — mimicking Microsoft's userinfo endpoint.
func newAssumeVerifiedStub(t *testing.T) *httptest.Server {
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
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No email_verified claim at all.
		_, _ = w.Write([]byte(`{"sub":"1","email":"a@b.c","name":"Ada"}`))
	})
	return srv
}

func userInfoWithAssume(t *testing.T, assume bool) *UserInfo {
	t.Helper()
	srv := newAssumeVerifiedStub(t)
	t.Cleanup(srv.Close)

	p, err := NewOIDCProvider(context.Background(), "microsoft", config.OAuthProviderConfig{
		ClientID: "id", ClientSecret: "secret", Issuer: srv.URL, AssumeEmailVerified: assume,
	}, "https://example.com/cb")
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	info, err := p.UserInfo(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok"}))
	if err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	return info
}

// TestAssumeEmailVerified_Off is the safe default: a provider that omits
// email_verified must yield EmailVerified == false, keeping the auto-link /
// auto-provision guard on.
func TestAssumeEmailVerified_Off(t *testing.T) {
	if got := userInfoWithAssume(t, false); got.EmailVerified {
		t.Errorf("EmailVerified = true with the flag off, want false")
	}
}

// TestAssumeEmailVerified_On: with the admin opt-in, a returned email is
// treated as verified even though the provider never sent the claim.
func TestAssumeEmailVerified_On(t *testing.T) {
	got := userInfoWithAssume(t, true)
	if !got.EmailVerified {
		t.Errorf("EmailVerified = false with the flag on, want true")
	}
	if got.Email != "a@b.c" {
		t.Errorf("Email = %q, want a@b.c", got.Email)
	}
}
