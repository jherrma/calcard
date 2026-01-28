package auth

import (
	"context"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock SAML Session Repository
type mockSAMLSessionRepo struct {
	mock.Mock
}

func (m *mockSAMLSessionRepo) Create(ctx context.Context, session *user.SAMLSession) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *mockSAMLSessionRepo) GetBySessionID(ctx context.Context, sessionID string) (*user.SAMLSession, error) {
	args := m.Called(ctx, sessionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.SAMLSession), args.Error(1)
}

func (m *mockSAMLSessionRepo) DeleteBySessionID(ctx context.Context, sessionID string) error {
	args := m.Called(ctx, sessionID)
	return args.Error(0)
}

func (m *mockSAMLSessionRepo) DeleteByUserID(ctx context.Context, userID uint) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func TestSAMLLoginUseCase_HandleACS_NewUser(t *testing.T) {
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	samlRepo := new(mockSAMLSessionRepo)
	tokenProvider := new(mockTokenProvider)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	// We pass nil for SP as we are testing logic that doesn't use it directly in HandleACS
	uc := NewSAMLLoginUseCase(nil, userRepo, oauthRepo, samlRepo, tokenProvider, refreshTokenRepo, cfg)

	ctx := context.Background()
	nameID := "user@example.com"
	attributes := map[string][]string{
		"email":     {"user@example.com"},
		"givenName": {"John"},
		"surname":   {"Doe"},
	}

	userRepo.On("GetByOAuth", ctx, "saml", nameID).Return(nil, nil)
	userRepo.On("GetByEmail", ctx, "user@example.com").Return(nil, nil)
	userRepo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	userRepo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Email == "user@example.com" && u.DisplayName == "John Doe" && u.IsActive && u.EmailVerified
	})).Return(nil)
	oauthRepo.On("GetByProvider", ctx, uint(0), "saml").Return(nil, nil) // Check before create
	oauthRepo.On("Create", ctx, mock.MatchedBy(func(c *user.OAuthConnection) bool {
		return c.Provider == "saml" && c.ProviderID == nameID
	})).Return(nil)

	tokenProvider.On("GenerateAccessToken", mock.Anything, "user@example.com").Return("access", time.Now().Add(time.Hour), nil)
	tokenProvider.On("GenerateRefreshToken").Return("refresh", nil)
	tokenProvider.On("HashToken", "refresh").Return("hashed")
	refreshTokenRepo.On("Create", ctx, mock.Anything).Return(nil)

	res, err := uc.HandleACS(ctx, nameID, attributes, "agent", "1.1.1.1")

	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, "user@example.com", res.User.Email)
}

func TestSAMLLoginUseCase_HandleACS_ExistingUser(t *testing.T) {
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	samlRepo := new(mockSAMLSessionRepo)
	tokenProvider := new(mockTokenProvider)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewSAMLLoginUseCase(nil, userRepo, oauthRepo, samlRepo, tokenProvider, refreshTokenRepo, cfg)

	ctx := context.Background()
	nameID := "user@example.com"
	attributes := map[string][]string{}
	existingUser := &user.User{ID: 123, UUID: "uuid", Email: "user@example.com"}

	userRepo.On("GetByOAuth", ctx, "saml", nameID).Return(existingUser, nil)

	tokenProvider.On("GenerateAccessToken", existingUser.UUID, existingUser.Email).Return("access", time.Now().Add(time.Hour), nil)
	tokenProvider.On("GenerateRefreshToken").Return("refresh", nil)
	tokenProvider.On("HashToken", "refresh").Return("hashed")
	refreshTokenRepo.On("Create", ctx, mock.Anything).Return(nil)

	res, err := uc.HandleACS(ctx, nameID, attributes, "agent", "1.1.1.1")

	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, uint(123), res.User.ID)
}
