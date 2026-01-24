package auth

import (
	"context"

	"golang.org/x/oauth2"
)

// OAuthProvider defines the interface for interacting with an OAuth/OIDC provider
type OAuthProvider interface {
	Name() string
	AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	UserInfo(ctx context.Context, tokenSource oauth2.TokenSource) (*UserInfo, error)
}

// UserInfo represents the user information retrieved from the provider
type UserInfo struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// OAuthProviderManager defines instructions for managing OAuth providers
type OAuthProviderManager interface {
	GetProvider(name string) (OAuthProvider, error)
	ListProviders() []string
}
