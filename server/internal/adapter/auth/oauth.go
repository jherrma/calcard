package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jherrma/caldav-server/internal/config"
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

type oidcProvider struct {
	name     string
	provider *oidc.Provider
	config   *oauth2.Config
}

// NewOIDCProvider creates a new OIDC-based OAuth provider
func NewOIDCProvider(ctx context.Context, name string, conf config.OAuthProviderConfig, redirectURL string) (OAuthProvider, error) {
	provider, err := oidc.NewProvider(ctx, conf.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider for %s: %w", name, err)
	}

	oauthConfig := &oauth2.Config{
		ClientID:     conf.ClientID,
		ClientSecret: conf.ClientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &oidcProvider{
		name:     name,
		provider: provider,
		config:   oauthConfig,
	}, nil
}

func (p *oidcProvider) Name() string {
	return p.name
}

func (p *oidcProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return p.config.AuthCodeURL(state, opts...)
}

func (p *oidcProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *oidcProvider) UserInfo(ctx context.Context, tokenSource oauth2.TokenSource) (*UserInfo, error) {
	userInfo, err := p.provider.UserInfo(ctx, tokenSource)
	if err != nil {
		return nil, err
	}

	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}

	if err := userInfo.Claims(&claims); err != nil {
		return nil, err
	}

	return &UserInfo{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Name:          claims.Name,
	}, nil
}

// OAuthProviderManager manages multiple OAuth providers
type OAuthProviderManager struct {
	providers map[string]OAuthProvider
}

// NewOAuthProviderManager creates a new OAuthProviderManager
func NewOAuthProviderManager(cfg *config.OAuthConfig) (*OAuthProviderManager, error) {
	providers := make(map[string]OAuthProvider)
	ctx := context.Background() // TODO: potentially pass context

	// Initialize Google
	if cfg.Google.ClientID != "" && cfg.Google.ClientSecret != "" {
		p, err := NewOIDCProvider(ctx, "google", cfg.Google, "/api/v1/auth/oauth/google/callback") // Redirect URL needs to be constructed properly
		if err == nil {
			providers["google"] = p
		}
	}

	// Initialize Microsoft
	if cfg.Microsoft.ClientID != "" && cfg.Microsoft.ClientSecret != "" {
		p, err := NewOIDCProvider(ctx, "microsoft", cfg.Microsoft, "/api/v1/auth/oauth/microsoft/callback")
		if err == nil {
			providers["microsoft"] = p
		}
	}

	// Initialize Custom
	if cfg.Custom.ClientID != "" && cfg.Custom.ClientSecret != "" {
		p, err := NewOIDCProvider(ctx, "custom", cfg.Custom, "/api/v1/auth/oauth/custom/callback")
		if err == nil {
			providers["custom"] = p
		}
	}

	return &OAuthProviderManager{providers: providers}, nil
}

func (m *OAuthProviderManager) GetProvider(name string) (OAuthProvider, error) {
	p, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found or not configured", name)
	}
	return p, nil
}

func (m *OAuthProviderManager) ListProviders() []string {
	var names []string
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}
