package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	adapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// SAMLLoginUseCase handles SAML login initiation and ACS callback
type SAMLLoginUseCase struct {
	sp               *adapter.SAMLServiceProvider
	userRepo         user.UserRepository
	oauthRepo        user.OAuthConnectionRepository
	samlSessionRepo  user.SAMLSessionRepository
	tokenProvider    user.TokenProvider
	refreshTokenRepo user.RefreshTokenRepository
	config           *config.Config
}

// NewSAMLLoginUseCase creates a new SAML login use case
func NewSAMLLoginUseCase(
	sp *adapter.SAMLServiceProvider,
	userRepo user.UserRepository,
	oauthRepo user.OAuthConnectionRepository,
	samlSessionRepo user.SAMLSessionRepository,
	tokenProvider user.TokenProvider,
	refreshTokenRepo user.RefreshTokenRepository,
	cfg *config.Config,
) *SAMLLoginUseCase {
	return &SAMLLoginUseCase{
		sp:               sp,
		userRepo:         userRepo,
		oauthRepo:        oauthRepo,
		samlSessionRepo:  samlSessionRepo,
		tokenProvider:    tokenProvider,
		refreshTokenRepo: refreshTokenRepo,
		config:           cfg,
	}
}

// InitiateLogin returns the URL to redirect the user to for SAML login
func (uc *SAMLLoginUseCase) InitiateLogin() (string, error) {
	return uc.sp.LoginURL()
}

// HandleACS processes the SAML response (ACS) and logs in/creates the user
func (uc *SAMLLoginUseCase) HandleACS(ctx context.Context, nameID string, attributes map[string][]string, userAgent, ip string) (*LoginResult, error) {
	// nameID is the unique identifier from IDP
	// attributes contains other claims

	if nameID == "" {
		return nil, fmt.Errorf("SAML response missing NameID")
	}

	// 1. Check if user exists with this SAML link
	linkedUser, err := uc.userRepo.GetByOAuth(ctx, "saml", nameID)
	if err != nil {
		return nil, err
	}

	var u *user.User

	if linkedUser != nil {
		u = linkedUser
	} else {
		// New user or linking existing email
		email := getAttributeFromMap(attributes, "email", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress", "mail")
		if email == "" {
			// Fallback: use NameID if it looks like an email
			email = nameID
		}

		if email == "" {
			return nil, fmt.Errorf("could not determine email address from SAML assertion")
		}

		existingUser, err := uc.userRepo.GetByEmail(ctx, email)
		if err != nil {
			return nil, err
		}

		if existingUser != nil {
			u = existingUser
			// Link this SAML ID to the existing user
			if err := uc.linkSAML(ctx, u.ID, nameID, email); err != nil {
				return nil, err
			}
		} else {
			// Create new user
			firstName := getAttributeFromMap(attributes, "givenName", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname")
			lastName := getAttributeFromMap(attributes, "surname", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname")
			displayName := fmt.Sprintf("%s %s", firstName, lastName)
			if firstName == "" && lastName == "" {
				displayName = getAttributeFromMap(attributes, "displayName", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name")
			}

			u, err = uc.createUser(ctx, email, displayName)
			if err != nil {
				return nil, err
			}
			if err := uc.linkSAML(ctx, u.ID, nameID, email); err != nil {
				return nil, err
			}
		}
	}

	// 2. Generate JWTs
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

func (uc *SAMLLoginUseCase) linkSAML(ctx context.Context, userID uint, nameID, providerEmail string) error {
	conn := &user.OAuthConnection{
		UserID:        userID,
		Provider:      "saml",
		ProviderID:    nameID,
		ProviderEmail: providerEmail,
		// No tokens to store for SAML
	}
	// Check if already linked to avoid error
	existing, err := uc.oauthRepo.GetByProvider(ctx, userID, "saml")
	if err == nil && existing != nil {
		return nil // Already linked
	}
	return uc.oauthRepo.Create(ctx, conn)
}

func (uc *SAMLLoginUseCase) createUser(ctx context.Context, email, displayName string) (*user.User, error) {
	// Generate random username
	username, err := GenerateUniqueUsername(ctx, uc.userRepo)
	if err != nil {
		return nil, err
	}

	u := &user.User{
		UUID:          uuid.New().String(),
		Email:         email,
		Username:      username,
		DisplayName:   displayName,
		IsActive:      true, // Auto-activate SAML users
		EmailVerified: true, // IDP verified
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		PasswordHash:  "*SAML_USER*",
	}
	if err := uc.userRepo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func getAttributeFromMap(attributes map[string][]string, keys ...string) string {
	for _, key := range keys {
		if vals, ok := attributes[key]; ok && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}
