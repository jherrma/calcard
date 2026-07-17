package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"golang.org/x/oauth2"
)

// TestUserInfo_EmailVerifiedAsString verifies that a provider returning
// email_verified as the JSON string "true" (non-spec, but AD FS and others do
// this) no longer breaks the userinfo call. go-oidc's built-in UserInfo
// parsing tolerates the string form; the old code re-read it into a bool and
// failed, collapsing the whole login into a generic error.
func TestUserInfo_EmailVerifiedAsString(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

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
		// email_verified is a STRING here, not a bool — the regression trigger.
		_, _ = w.Write([]byte(`{"sub":"abc","email":"a@b.c","email_verified":"true","name":"Ada"}`))
	})

	p, err := NewOIDCProvider(context.Background(), "custom", config.OAuthProviderConfig{
		ClientID: "id", ClientSecret: "secret", Issuer: srv.URL,
	}, "https://example.com/cb")
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	info, err := p.UserInfo(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok"}))
	if err != nil {
		t.Fatalf("UserInfo must not fail on a string email_verified: %v", err)
	}
	if !info.EmailVerified {
		t.Errorf(`EmailVerified = false, want true (the string "true" must parse as verified)`)
	}
	if info.Email != "a@b.c" || info.Subject != "abc" || info.Name != "Ada" {
		t.Errorf("unexpected user info: %+v", info)
	}
}
