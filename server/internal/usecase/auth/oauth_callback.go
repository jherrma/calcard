package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/oauth2"
)

// ErrRegistrationDisabled is returned when an OAuth login would have to
// provision a brand-new account (no existing OAuth link and no matching local
// account) but self-service registration is disabled. Existing-user OAuth
// logins are unaffected.
var ErrRegistrationDisabled = errors.New("registration is disabled")

// OAuthCallbackUseCase handles the OAuth callback and user login/creation
type OAuthCallbackUseCase struct {
	providerManager  authadapter.OAuthProviderManager
	userRepo         user.UserRepository
	oauthRepo        user.OAuthConnectionRepository
	refreshTokenRepo user.RefreshTokenRepository
	tokenProvider    user.TokenProvider
	config           *config.Config
}

// NewOAuthCallbackUseCase creates a new OAuthCallbackUseCase
func NewOAuthCallbackUseCase(
	providerManager authadapter.OAuthProviderManager,
	userRepo user.UserRepository,
	oauthRepo user.OAuthConnectionRepository,
	refreshTokenRepo user.RefreshTokenRepository,
	tokenProvider user.TokenProvider,
	config *config.Config,
) *OAuthCallbackUseCase {
	return &OAuthCallbackUseCase{
		providerManager:  providerManager,
		userRepo:         userRepo,
		oauthRepo:        oauthRepo,
		refreshTokenRepo: refreshTokenRepo,
		tokenProvider:    tokenProvider,
		config:           config,
	}
}

// Execute processes the OAuth callback
// currentUser is optional. If provided, the flow attempts to link the provider to this user.
func (uc *OAuthCallbackUseCase) Execute(ctx context.Context, providerName, code, userAgent, ip string, currentUser *user.User) (*LoginResult, error) {
	provider, err := uc.providerManager.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	// Exchange code for token
	token, err := provider.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}

	// Get user info
	userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	existingUser, err := uc.userRepo.GetByOAuth(ctx, providerName, userInfo.Subject)
	if err != nil {
		return nil, err
	}

	// Linking Flow
	if currentUser != nil {
		if existingUser != nil {
			if existingUser.ID == currentUser.ID {
				// Already linked to this user. Just update tokens.
				conn, err := uc.oauthRepo.GetByProvider(ctx, currentUser.ID, providerName)
				if err != nil {
					return nil, err
				}
				conn.AccessToken = token.AccessToken
				conn.RefreshToken = token.RefreshToken
				conn.TokenExpiry = &token.Expiry
				if err := uc.oauthRepo.Update(ctx, conn); err != nil {
					return nil, err
				}
				return nil, nil // No login result needed, success.
			}
			return nil, fmt.Errorf("this %s account is already linked to another user", providerName)
		}

		// Not linked, link it now.
		if err := uc.linkProvider(ctx, currentUser.ID, providerName, userInfo, token.AccessToken, token.RefreshToken, token.Expiry); err != nil {
			return nil, err
		}
		return nil, nil // Success
	}

	// Login Flow
	var u *user.User

	if existingUser != nil {
		u = existingUser
	} else {
		if userInfo.Email == "" {
			return nil, fmt.Errorf("provider did not return an email address")
		}

		u, err = uc.userRepo.GetByEmail(ctx, userInfo.Email)
		if err != nil {
			return nil, err
		}

		if u != nil {
			// Auto-linking an OAuth identity onto an existing local account by
			// email is an account-takeover vector unless the provider asserts
			// the email is verified. Require it; otherwise the user must sign in
			// with their password and link the provider explicitly from settings.
			if !userInfo.EmailVerified {
				return nil, fmt.Errorf("the %s account's email address is not verified; sign in with your password and link %s from your account settings", providerName, providerName)
			}
			if err := uc.linkProvider(ctx, u.ID, providerName, userInfo, token.AccessToken, token.RefreshToken, token.Expiry); err != nil {
				return nil, err
			}
		} else {
			// Provisioning a brand-new account keyed on the provider's email is
			// an account-takeover vector unless the provider asserts the email is
			// verified: an attacker could pre-emptively create an account for a
			// victim's address (unverified), and a later genuinely-verified login
			// by the victim would auto-link into the attacker-controlled account.
			// Mirror the verified-email guard used on the link path above and
			// refuse to auto-provision from an unverified email.
			if !userInfo.EmailVerified {
				return nil, fmt.Errorf("the %s account's email address is not verified; sign in with your password and link %s from your account settings", providerName, providerName)
			}

			// First-time OAuth login would have to provision a new account.
			// Honor the same kill-switch as local registration
			// (config.Registration.Disabled / auth_handler.go) so OAuth can't be
			// used to bypass it. Existing-user logins never reach this branch.
			if uc.config.Registration.Disabled {
				return nil, ErrRegistrationDisabled
			}
			u, err = uc.createUser(ctx, userInfo)
			if err != nil {
				return nil, err
			}
			if err := uc.linkProvider(ctx, u.ID, providerName, userInfo, token.AccessToken, token.RefreshToken, token.Expiry); err != nil {
				return nil, err
			}
		}
	}

	// Deactivated accounts must not be able to obtain tokens via OAuth. New
	// users created above are always active, so this only rejects an existing
	// (resolved) user whose account has been deactivated. The handler maps this
	// error to a generic OAuth error redirect.
	if !u.IsActive {
		return nil, ErrInactiveAccount
	}

	// Generate JWTs
	accessToken, expiresAt, err := uc.tokenProvider.GenerateAccessToken(u.UUID, u.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := uc.tokenProvider.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	hash := uc.tokenProvider.HashToken(refreshToken)
	rt := &user.RefreshToken{
		UserID:    u.ID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(uc.config.JWT.RefreshExpiry),
		UserAgent: userAgent,
		IP:        ip,
	}

	if err := uc.refreshTokenRepo.Create(ctx, rt); err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return &LoginResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         u,
	}, nil
}

func (uc *OAuthCallbackUseCase) linkProvider(ctx context.Context, userID uint, providerName string, userInfo *authadapter.UserInfo, accessToken, refreshToken string, expiry time.Time) error {
	conn := &user.OAuthConnection{
		UserID:        userID,
		Provider:      providerName,
		ProviderID:    userInfo.Subject,
		ProviderEmail: userInfo.Email,
		AccessToken:   accessToken,  // Should be encrypted
		RefreshToken:  refreshToken, // Should be encrypted
		TokenExpiry:   &expiry,
	}
	return uc.oauthRepo.Create(ctx, conn)
}

func (uc *OAuthCallbackUseCase) createUser(ctx context.Context, userInfo *authadapter.UserInfo) (*user.User, error) {
	username, err := GenerateUniqueUsername(ctx, uc.userRepo)
	if err != nil {
		return nil, err
	}

	u := &user.User{
		UUID:          uuid.New().String(),
		Email:         userInfo.Email,
		Username:      username,
		DisplayName:   userInfo.Name, // or Name
		IsActive:      true,
		EmailVerified: userInfo.EmailVerified, // Trust provider? Yes.
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		// PasswordHash? It's not null in DB. We need to set dummy or allow nullable.
		// DB schema said not null. I should set a random or impossible password?
		// Or change DB schema. Changing schema is better but "PasswordHash string `gorm:"size:255;not null"`"
		// I'll set a random string that can't be bcrypt matched easily.
		PasswordHash: "*OAUTH_USER*",
	}
	if err := uc.userRepo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}
