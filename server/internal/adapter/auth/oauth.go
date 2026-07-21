package auth

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jherrma/caldav-server/internal/config"
	"golang.org/x/oauth2"
)

// OAuthProvider defines the interface for interacting with an OAuth/OIDC provider
type OAuthProvider interface {
	Name() string
	AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string
	Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error)
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
	name                string
	provider            *oidc.Provider
	config              *oauth2.Config
	assumeEmailVerified bool
}

// NewOIDCProvider creates a new OIDC-based OAuth provider
func NewOIDCProvider(ctx context.Context, name string, conf config.OAuthProviderConfig, redirectURL string) (OAuthProvider, error) {
	// Azure AD's multi-tenant endpoints ("common", "organizations",
	// "consumers") return a templated issuer ({tenantid}) in their discovery
	// document, which go-oidc's strict issuer-equality check always rejects.
	// Skip that check for these endpoints only. This is safe here because
	// identity comes from the /userinfo endpoint over TLS (see UserInfo below),
	// not from ID-token validation against the issuer string.
	if isAzureMultiTenantIssuer(conf.Issuer) {
		ctx = oidc.InsecureIssuerURLContext(ctx, conf.Issuer)
	}
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
		name:                name,
		provider:            provider,
		config:              oauthConfig,
		assumeEmailVerified: conf.AssumeEmailVerified,
	}, nil
}

// isAzureMultiTenantIssuer reports whether issuer is one of Azure AD's
// multi-tenant endpoints, whose discovery document returns a templated issuer
// ({tenantid}) that go-oidc's strict issuer-equality check would otherwise
// reject. The strict check is kept for all other providers (Google, custom) as
// a useful misconfiguration guard.
func isAzureMultiTenantIssuer(issuer string) bool {
	if !strings.Contains(issuer, "login.microsoftonline.com") {
		return false
	}
	return strings.Contains(issuer, "/common/") ||
		strings.Contains(issuer, "/organizations/") ||
		strings.Contains(issuer, "/consumers/")
}

func (p *oidcProvider) Name() string {
	return p.name
}

func (p *oidcProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return p.config.AuthCodeURL(state, opts...)
}

func (p *oidcProvider) Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code, opts...)
}

func (p *oidcProvider) UserInfo(ctx context.Context, tokenSource oauth2.TokenSource) (*UserInfo, error) {
	userInfo, err := p.provider.UserInfo(ctx, tokenSource)
	if err != nil {
		return nil, err
	}

	// go-oidc already parses sub/email/email_verified and tolerates providers
	// that (non-spec, but common: AD FS and others) send email_verified as the
	// string "true" instead of a boolean. Re-reading those into a local bool
	// struct broke on the string form and failed the whole login. Only "name"
	// needs a raw-claims read (go-oidc doesn't expose it); treat it as optional.
	var claims struct {
		Name string `json:"name"`
	}
	if err := userInfo.Claims(&claims); err != nil {
		claims.Name = ""
	}

	// Some providers (Microsoft's userinfo endpoint) never return an
	// email_verified claim, so it unmarshals to false and blocks new-user
	// sign-in and auto-linking. When the admin has explicitly opted in for this
	// provider, treat a returned email as verified.
	emailVerified := userInfo.EmailVerified
	if p.assumeEmailVerified && userInfo.Email != "" {
		emailVerified = true
	}

	return &UserInfo{
		Subject:       userInfo.Subject,
		Email:         userInfo.Email,
		EmailVerified: emailVerified,
		Name:          claims.Name,
	}, nil
}

// OAuthProviderManager defines the interface for managing OAuth providers
type OAuthProviderManager interface {
	GetProvider(name string) (OAuthProvider, error)
	ListProviders() []string
}

// oauthProviderManager implements OAuthProviderManager
type oauthProviderManager struct {
	providers map[string]OAuthProvider
}

// NewOAuthProviderManager creates a new OAuth provider manager. baseURL is the
// public origin of the deployment (cfg.BaseURL); OAuth redirect URIs are
// derived from it, so it must be the externally reachable URL that the admin
// registered at each provider.
func NewOAuthProviderManager(cfg *config.OAuthConfig, baseURL string) (OAuthProviderManager, error) {
	providers := make(map[string]OAuthProvider)

	// Bound OIDC discovery so an unreachable issuer can't stall startup.
	// go-oidc copies only the HTTP client out of this context for later use
	// (cloneContext), so cancelling it after discovery is safe.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Trailing-slash trim matters: base_url "http://host:8080/" would otherwise
	// yield "//api/..." and providers reject the mismatched redirect URI.
	base := strings.TrimRight(baseURL, "/")
	redirectFor := func(name string) string {
		return base + "/api/v1/auth/oauth/" + name + "/callback"
	}

	// Initialize Google
	if cfg.Google.ClientID != "" && cfg.Google.ClientSecret != "" {
		p, err := NewOIDCProvider(ctx, "google", cfg.Google, redirectFor("google"))
		if err != nil {
			log.Printf("ERROR: OAuth provider %q failed to initialize and will be UNAVAILABLE: %v", "google", err)
		} else {
			providers["google"] = p
		}
	}

	// Initialize Microsoft
	if cfg.Microsoft.ClientID != "" && cfg.Microsoft.ClientSecret != "" {
		p, err := NewOIDCProvider(ctx, "microsoft", cfg.Microsoft, redirectFor("microsoft"))
		if err != nil {
			log.Printf("ERROR: OAuth provider %q failed to initialize and will be UNAVAILABLE: %v", "microsoft", err)
		} else {
			providers["microsoft"] = p
		}
	}

	// Initialize Custom
	if cfg.Custom.ClientID != "" && cfg.Custom.ClientSecret != "" {
		p, err := NewOIDCProvider(ctx, "custom", cfg.Custom, redirectFor("custom"))
		if err != nil {
			log.Printf("ERROR: OAuth provider %q failed to initialize and will be UNAVAILABLE: %v", "custom", err)
		} else {
			providers["custom"] = p
		}
	}

	return &oauthProviderManager{providers: providers}, nil
}

func (m *oauthProviderManager) GetProvider(name string) (OAuthProvider, error) {
	p, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found or not configured", name)
	}
	return p, nil
}

func (m *oauthProviderManager) ListProviders() []string {
	var names []string
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}
