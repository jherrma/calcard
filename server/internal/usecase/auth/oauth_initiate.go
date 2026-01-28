package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
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

// Execute returns the authorization URL and state for the given provider
func (uc *InitiateOAuthUseCase) Execute(providerName string, redirectURL string) (string, string, error) {
	provider, err := uc.providerManager.GetProvider(providerName)
	if err != nil {
		return "", "", err
	}

	state, err := generateState()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	// We might want to pass options like AccessTypeOffline here
	url := provider.AuthCodeURL(state)

	return url, state, nil
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
