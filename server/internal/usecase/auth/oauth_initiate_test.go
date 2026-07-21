package auth

import (
	"context"
	"testing"

	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"
)

// pkceRecordingProvider is a minimal OAuthProvider that renders a real
// authorization URL from an oauth2.Config, so the emitted PKCE parameters can
// be asserted end-to-end.
type pkceRecordingProvider struct {
	lastURL string
}

func (p *pkceRecordingProvider) Name() string { return "fake" }

func (p *pkceRecordingProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	conf := &oauth2.Config{
		ClientID:    "cid",
		RedirectURL: "https://app.example/callback",
		Endpoint:    oauth2.Endpoint{AuthURL: "https://provider.example/authorize"},
	}
	p.lastURL = conf.AuthCodeURL(state, opts...)
	return p.lastURL
}

func (p *pkceRecordingProvider) Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	return nil, nil
}

func (p *pkceRecordingProvider) UserInfo(ctx context.Context, tokenSource oauth2.TokenSource) (*authadapter.UserInfo, error) {
	return nil, nil
}

// TestInitiateOAuthUseCase_Execute_AddsPKCEChallenge asserts the authorization
// URL carries an S256 code challenge and that a non-empty verifier is returned
// for the callback to use.
func TestInitiateOAuthUseCase_Execute_AddsPKCEChallenge(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	provider := &pkceRecordingProvider{}
	providerManager.On("GetProvider", "google").Return(provider, nil)

	uc := NewInitiateOAuthUseCase(providerManager)

	url, state, verifier, err := uc.Execute("google", "")

	assert.NoError(t, err)
	assert.NotEmpty(t, state)
	assert.NotEmpty(t, verifier, "a PKCE verifier must be returned for the callback")
	assert.Contains(t, url, "code_challenge=")
	assert.Contains(t, url, "code_challenge_method=S256")
}
