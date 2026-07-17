package auth

import (
	"context"
	"testing"
	"time"

	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/oauth2"
)

// Mocks

type mockOAuthProviderManager struct {
	mock.Mock
}

func (m *mockOAuthProviderManager) GetProvider(name string) (authadapter.OAuthProvider, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(authadapter.OAuthProvider), args.Error(1)
}

func (m *mockOAuthProviderManager) ListProviders() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

type mockOAuthProvider struct {
	mock.Mock
}

func (m *mockOAuthProvider) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockOAuthProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	args := m.Called(state, opts)
	return args.String(0)
}

func (m *mockOAuthProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	args := m.Called(ctx, code)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*oauth2.Token), args.Error(1)
}

func (m *mockOAuthProvider) UserInfo(ctx context.Context, tokenSource oauth2.TokenSource) (*authadapter.UserInfo, error) {
	args := m.Called(ctx, tokenSource)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*authadapter.UserInfo), args.Error(1)
}

type mockOAuthRepo struct {
	mock.Mock
}

func (m *mockOAuthRepo) Create(ctx context.Context, conn *user.OAuthConnection) error {
	args := m.Called(ctx, conn)
	return args.Error(0)
}

func (m *mockOAuthRepo) GetByProvider(ctx context.Context, userID uint, provider string) (*user.OAuthConnection, error) {
	args := m.Called(ctx, userID, provider)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.OAuthConnection), args.Error(1)
}

func (m *mockOAuthRepo) ListByUserID(ctx context.Context, userID uint) ([]user.OAuthConnection, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]user.OAuthConnection), args.Error(1)
}

func (m *mockOAuthRepo) Delete(ctx context.Context, userID uint, provider string) error {
	args := m.Called(ctx, userID, provider)
	return args.Error(0)
}

func (m *mockOAuthRepo) Update(ctx context.Context, conn *user.OAuthConnection) error {
	args := m.Called(ctx, conn)
	return args.Error(0)
}

type mockRefreshTokenRepo struct {
	mock.Mock
}

func (m *mockRefreshTokenRepo) Create(ctx context.Context, token *user.RefreshToken) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *mockRefreshTokenRepo) GetByHash(ctx context.Context, hash string) (*user.RefreshToken, error) {
	args := m.Called(ctx, hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.RefreshToken), args.Error(1)
}

func (m *mockRefreshTokenRepo) DeleteByHash(ctx context.Context, hash string) error {
	args := m.Called(ctx, hash)
	return args.Error(0)
}

func (m *mockRefreshTokenRepo) DeleteByUserID(ctx context.Context, userID uint) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

type mockTokenProvider struct {
	mock.Mock
}

func (m *mockTokenProvider) GenerateAccessToken(userID string, email string) (string, time.Time, error) {
	args := m.Called(userID, email)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *mockTokenProvider) GenerateRefreshToken() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *mockTokenProvider) HashToken(token string) string {
	args := m.Called(token)
	return args.String(0)
}

func (m *mockTokenProvider) ValidateAccessToken(tokenStr string) (string, string, error) {
	args := m.Called(tokenStr)
	return args.String(0), args.String(1), args.Error(2)
}

// Tests

func TestOAuthCallbackUseCase_Execute_LoginExistingUser(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"
	userAgent := "test-agent"
	ip := "127.0.0.1"

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	userInfo := &authadapter.UserInfo{Subject: "sub123", Email: "test@example.com"}
	existingUser := &user.User{ID: 1, UUID: "uuid1", Email: userInfo.Email, IsActive: true}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(existingUser, nil)
	tokenProvider.On("GenerateAccessToken", existingUser.UUID, existingUser.Email).Return("jwt_access", time.Now().Add(time.Hour), nil)
	tokenProvider.On("GenerateRefreshToken").Return("jwt_refresh", nil)
	tokenProvider.On("HashToken", "jwt_refresh").Return("hashed_refresh")
	refreshTokenRepo.On("Create", ctx, mock.Anything).Return(nil)

	result, err := uc.Execute(ctx, providerName, code, userAgent, ip, nil)

	assert.NoError(t, err)
	assert.Equal(t, existingUser, result.User)
	assert.Equal(t, "jwt_access", result.AccessToken)
}

func TestOAuthCallbackUseCase_Execute_LinkNewUser(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"
	userAgent := "test-agent"
	ip := "127.0.0.1"

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	userInfo := &authadapter.UserInfo{Subject: "sub123", Email: "new@example.com", Name: "New User", EmailVerified: true}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(nil, nil)
	userRepo.On("GetByEmail", ctx, userInfo.Email).Return(nil, nil)
	userRepo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	userRepo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Email == userInfo.Email && u.DisplayName == userInfo.Name && len(u.Username) == 16
	})).Return(nil)
	oauthRepo.On("Create", ctx, mock.MatchedBy(func(c *user.OAuthConnection) bool {
		return c.Provider == providerName && c.ProviderID == userInfo.Subject
	})).Return(nil)

	tokenProvider.On("GenerateAccessToken", mock.Anything, userInfo.Email).Return("jwt_access", time.Now().Add(time.Hour), nil)
	tokenProvider.On("GenerateRefreshToken").Return("jwt_refresh", nil)
	tokenProvider.On("HashToken", "jwt_refresh").Return("hashed_refresh")
	refreshTokenRepo.On("Create", ctx, mock.Anything).Return(nil)

	result, err := uc.Execute(ctx, providerName, code, userAgent, ip, nil)

	assert.NoError(t, err)
	assert.NotNil(t, result.User)
	assert.Equal(t, userInfo.Email, result.User.Email)
}

func TestOAuthCallbackUseCase_Execute_CreateRequiresVerifiedEmail(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	// No existing OAuth link and no matching local account, so this would
	// auto-provision. The provider asserts an email but does NOT mark it
	// verified — provisioning must be refused (account-takeover vector).
	userInfo := &authadapter.UserInfo{Subject: "attacker-sub", Email: "victim@example.com", Name: "Victim", EmailVerified: false}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(nil, nil)
	userRepo.On("GetByEmail", ctx, userInfo.Email).Return(nil, nil)

	_, err := uc.Execute(ctx, providerName, code, "ua", "127.0.0.1", nil)

	assert.Error(t, err, "unverified-email auto-provisioning must be rejected")
	assert.ErrorIs(t, err, ErrEmailNotVerified)
	// No account may have been created and no provider connection linked.
	userRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	oauthRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestOAuthCallbackUseCase_Execute_LinkByEmailRequiresVerified(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	// Provider asserts the email but does NOT mark it verified.
	userInfo := &authadapter.UserInfo{Subject: "attacker-sub", Email: "victim@example.com", EmailVerified: false}
	existingUser := &user.User{ID: 7, UUID: "victim-uuid", Email: userInfo.Email}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(nil, nil)
	userRepo.On("GetByEmail", ctx, userInfo.Email).Return(existingUser, nil)

	_, err := uc.Execute(ctx, providerName, code, "ua", "127.0.0.1", nil)

	assert.Error(t, err, "unverified-email auto-link must be rejected")
	assert.ErrorIs(t, err, ErrEmailNotVerified)
	// No OAuth connection must have been created.
	oauthRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestOAuthCallbackUseCase_Execute_LinkByEmailVerifiedSucceeds(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	userInfo := &authadapter.UserInfo{Subject: "sub123", Email: "owner@example.com", EmailVerified: true}
	existingUser := &user.User{ID: 7, UUID: "owner-uuid", Email: userInfo.Email, IsActive: true}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(nil, nil)
	userRepo.On("GetByEmail", ctx, userInfo.Email).Return(existingUser, nil)
	oauthRepo.On("Create", ctx, mock.Anything).Return(nil)
	tokenProvider.On("GenerateAccessToken", mock.Anything, userInfo.Email).Return("jwt_access", time.Now().Add(time.Hour), nil)
	tokenProvider.On("GenerateRefreshToken").Return("jwt_refresh", nil)
	tokenProvider.On("HashToken", "jwt_refresh").Return("hashed_refresh")
	refreshTokenRepo.On("Create", ctx, mock.Anything).Return(nil)

	result, err := uc.Execute(ctx, providerName, code, "ua", "127.0.0.1", nil)

	assert.NoError(t, err)
	assert.NotNil(t, result.User)
	assert.Equal(t, userInfo.Email, result.User.Email)
}

func TestOAuthCallbackUseCase_Execute_LinkLoggedInUser(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"
	userAgent := "test-agent"
	ip := "127.0.0.1"
	currentUser := &user.User{ID: 99, UUID: "currUUID", Email: "curr@example.com"}

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	userInfo := &authadapter.UserInfo{Subject: "sub123", Email: "new@example.com"}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(nil, nil)
	// No account of this provider linked to the user yet — the link proceeds.
	oauthRepo.On("GetByProvider", ctx, currentUser.ID, providerName).Return(nil, nil)

	oauthRepo.On("Create", ctx, mock.MatchedBy(func(c *user.OAuthConnection) bool {
		return c.UserID == currentUser.ID && c.Provider == providerName && c.ProviderID == userInfo.Subject
	})).Return(nil)

	// Since we return (nil, nil) for successful linking, no token generation
	result, err := uc.Execute(ctx, providerName, code, userAgent, ip, currentUser)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestOAuthCallbackUseCase_Execute_LinkAlreadyLinkedError(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"
	currentUser := &user.User{ID: 99}

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	userInfo := &authadapter.UserInfo{Subject: "sub123", Email: "test@example.com"}
	otherUser := &user.User{ID: 100}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(otherUser, nil)

	_, err := uc.Execute(ctx, providerName, code, "", "", currentUser)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderAlreadyLinked)
}

func TestOAuthCallbackUseCase_Execute_LinkAlreadyLinkedSuccess(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"
	currentUser := &user.User{ID: 99}

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "new_access_token", RefreshToken: "new_refresh_token", Expiry: time.Now().Add(time.Hour)}
	userInfo := &authadapter.UserInfo{Subject: "sub123", Email: "test@example.com"}
	existingUser := &user.User{ID: 99} // Same ID as currentUser
	existingConn := &user.OAuthConnection{ID: 1, UserID: 99, Provider: providerName, ProviderID: userInfo.Subject}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(existingUser, nil)
	oauthRepo.On("GetByProvider", ctx, currentUser.ID, providerName).Return(existingConn, nil)
	oauthRepo.On("Update", ctx, mock.MatchedBy(func(c *user.OAuthConnection) bool {
		return c.AccessToken == token.AccessToken && c.RefreshToken == token.RefreshToken
	})).Return(nil)

	result, err := uc.Execute(ctx, providerName, code, "", "", currentUser)

	assert.NoError(t, err)
	assert.Nil(t, result) // Success returns nil result for linking
	oauthRepo.AssertExpectations(t)
}

// TestOAuthCallbackUseCase_Execute_LinkSecondAccountRejected covers the new
// (user_id, provider) uniqueness: a logged-in user who already has one account
// linked for a provider tries to link a *different* account of the same
// provider. It must be rejected with an actionable message and must not create
// a second connection row.
func TestOAuthCallbackUseCase_Execute_LinkSecondAccountRejected(t *testing.T) {
	providerManager := new(mockOAuthProviderManager)
	userRepo := new(mockUserRepo)
	oauthRepo := new(mockOAuthRepo)
	refreshTokenRepo := new(mockRefreshTokenRepo)
	tokenProvider := new(mockTokenProvider)
	cfg := &config.Config{JWT: config.JWTConfig{RefreshExpiry: time.Hour}}

	uc := NewOAuthCallbackUseCase(providerManager, userRepo, oauthRepo, refreshTokenRepo, tokenProvider, cfg)

	ctx := context.Background()
	providerName := "google"
	code := "auth_code"
	currentUser := &user.User{ID: 99}

	provider := new(mockOAuthProvider)
	token := &oauth2.Token{AccessToken: "access_token"}
	// A brand-new provider identity (subject) not linked to anyone yet.
	userInfo := &authadapter.UserInfo{Subject: "second-sub", Email: "second@example.com"}
	// ...but the user already has a *different* google account linked.
	existingConn := &user.OAuthConnection{ID: 1, UserID: 99, Provider: providerName, ProviderID: "first-sub"}

	providerManager.On("GetProvider", providerName).Return(provider, nil)
	provider.On("Exchange", ctx, code).Return(token, nil)
	provider.On("UserInfo", ctx, mock.Anything).Return(userInfo, nil)
	userRepo.On("GetByOAuth", ctx, providerName, userInfo.Subject).Return(nil, nil)
	oauthRepo.On("GetByProvider", ctx, currentUser.ID, providerName).Return(existingConn, nil)

	_, err := uc.Execute(ctx, providerName, code, "", "", currentUser)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already linked")
	oauthRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}
