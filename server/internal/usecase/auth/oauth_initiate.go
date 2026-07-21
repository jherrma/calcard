package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"golang.org/x/oauth2"
)

// InitiateOAuthUseCase handles starting the OAuth flow
type InitiateOAuthUseCase struct {
	providerManager authadapter.OAuthProviderManager
}

// NewInitiateOAuthUseCase creates a new InitiateOAuthUseCase
func NewInitiateOAuthUseCase(providerManager authadapter.OAuthProviderManager) *InitiateOAuthUseCase {
	return &InitiateOAuthUseCase{
		providerManager: providerManager,
	}
}

// Execute returns the authorization URL, state, and PKCE verifier for the given
// provider. The verifier is a secret that must survive the round-trip through
// the provider (stored in the signed, HttpOnly oauth_context cookie) and be
// supplied to the token exchange in the callback.
func (uc *InitiateOAuthUseCase) Execute(providerName string, redirectURL string) (string, string, string, error) {
	provider, err := uc.providerManager.GetProvider(providerName)
	if err != nil {
		return "", "", "", err
	}

	state, err := generateState()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	// PKCE (S256): send a code challenge on the authorization request and keep
	// the verifier for the token exchange. Providers that don't support PKCE
	// ignore the extra code_challenge parameter, so this is safe unconditionally.
	verifier := oauth2.GenerateVerifier()
	url := provider.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	return url, state, verifier, nil
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
