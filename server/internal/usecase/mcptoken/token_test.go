package mcptoken_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	domainuser "github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"github.com/jherrma/caldav-server/internal/usecase/mcptoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type env struct {
	t        *testing.T
	tokens   domainuser.MCPTokenRepository
	userRepo domainuser.UserRepository
	create   *mcptoken.CreateUseCase
	list     *mcptoken.ListUseCase
	revoke   *mcptoken.RevokeUseCase
	auth     *mcptoken.AuthenticateUseCase
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domainuser.User{}, &domainuser.MCPToken{}))

	tokens := repository.NewMCPTokenRepository(db)
	userRepo := repository.NewUserRepository(db)
	logger := logging.NewSecurityLogger(slog.New(slog.NewJSONHandler(io.Discard, nil)))

	return &env{
		t:        t,
		tokens:   tokens,
		userRepo: userRepo,
		create:   mcptoken.NewCreateUseCase(tokens, logger),
		list:     mcptoken.NewListUseCase(tokens),
		revoke:   mcptoken.NewRevokeUseCase(tokens, logger),
		auth:     mcptoken.NewAuthenticateUseCase(tokens, userRepo),
	}
}

func (e *env) newUser(email string) *domainuser.User {
	e.t.Helper()
	u := &domainuser.User{
		UUID:         uuid.New().String(),
		Email:        email,
		Username:     email,
		PasswordHash: "x",
		IsActive:     true,
	}
	require.NoError(e.t, e.userRepo.Create(context.Background(), u))
	return u
}

func TestGenerateProducesDistinctPrefixedTokens(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		raw, hash, prefix, err := mcptoken.Generate()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(raw, mcptoken.TokenPrefix),
			"the prefix is what routes a bearer credential to the right verifier")
		assert.True(t, strings.HasPrefix(prefix, mcptoken.TokenPrefix))
		assert.Len(t, hash, 64, "the stored value is a hex sha256")
		assert.Equal(t, mcptoken.HashToken(raw), hash)
		assert.NotContains(t, hash, raw, "the raw secret must never be recoverable from what is stored")
		require.False(t, seen[raw], "generated the same token twice")
		seen[raw] = true
	}
}

func TestTokenPrefixIsOnlyARoutingHint(t *testing.T) {
	assert.True(t, mcptoken.LooksLikeToken(mcptoken.TokenPrefix+"anything"))
	assert.False(t, mcptoken.LooksLikeToken("eyJhbGciOiJIUzI1NiJ9.e30.sig"))
	assert.False(t, mcptoken.LooksLikeToken(""))
}

func TestCreateReturnsTheSecretExactlyOnce(t *testing.T) {
	e := newEnv(t)
	alice := e.newUser("alice@example.com")
	ctx := context.Background()

	out, err := e.create.Execute(ctx, alice.ID, mcptoken.CreateInput{Name: "laptop"})
	require.NoError(t, err)
	require.NotEmpty(t, out.Token)

	// Everything the list path can ever return omits the secret, because the
	// model has no field to render it from.
	listed, err := e.list.Execute(ctx, alice.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, out.TokenPrefix, listed[0].TokenPrefix)
	assert.NotEqual(t, out.Token, listed[0].TokenHash)
	assert.NotContains(t, out.Token, listed[0].TokenHash)
}

func TestCreateValidatesNameAndExpiry(t *testing.T) {
	e := newEnv(t)
	alice := e.newUser("alice@example.com")
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	tooFar := time.Now().Add(10 * 365 * 24 * time.Hour)

	for name, input := range map[string]mcptoken.CreateInput{
		"empty name":      {Name: ""},
		"whitespace name": {Name: "   "},
		"overlong name":   {Name: strings.Repeat("x", 101)},
		"expiry in past":  {Name: "ok", ExpiresAt: &past},
		"expiry too far":  {Name: "ok", ExpiresAt: &tooFar},
	} {
		_, err := e.create.Execute(ctx, alice.ID, input)
		require.Error(t, err, "%s must be rejected", name)
		assert.ErrorIs(t, err, mcptoken.ErrInvalidInput, "%s", name)
	}
}

func TestAuthenticateAcceptsAValidToken(t *testing.T) {
	e := newEnv(t)
	alice := e.newUser("alice@example.com")
	ctx := context.Background()

	out, err := e.create.Execute(ctx, alice.ID, mcptoken.CreateInput{Name: "laptop"})
	require.NoError(t, err)

	u, token, err := e.auth.Execute(ctx, out.Token, "203.0.113.7")
	require.NoError(t, err)
	assert.Equal(t, alice.ID, u.ID)
	assert.Equal(t, "laptop", token.Name)

	stored, err := e.tokens.GetByUUID(ctx, out.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.LastUsedAt)
	assert.Equal(t, "203.0.113.7", stored.LastUsedIP)
}

func TestAuthenticateRejectsEveryInvalidCase(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice := e.newUser("alice@example.com")

	valid, err := e.create.Execute(ctx, alice.ID, mcptoken.CreateInput{Name: "live"})
	require.NoError(t, err)

	revoked, err := e.create.Execute(ctx, alice.ID, mcptoken.CreateInput{Name: "revoked"})
	require.NoError(t, err)
	require.NoError(t, e.revoke.Execute(ctx, alice.ID, revoked.ID, "", ""))

	// The create use case refuses an expiry in the past, so an already-expired
	// token is written straight through the repository — which is also the
	// realistic case: a token minted last year whose expiry has since passed.
	past := time.Now().Add(-time.Minute)
	require.NoError(t, e.tokens.Create(ctx, &domainuser.MCPToken{
		UUID: uuid.New().String(), UserID: alice.ID, Name: "expired",
		TokenHash: mcptoken.HashToken(mcptoken.TokenPrefix + "expired-secret"),
		ExpiresAt: &past, CreatedAt: past,
	}))

	inactive := e.newUser("inactive@example.com")
	inactiveToken, err := e.create.Execute(ctx, inactive.ID, mcptoken.CreateInput{Name: "x"})
	require.NoError(t, err)
	inactive.IsActive = false
	require.NoError(t, e.userRepo.Update(ctx, inactive))

	for name, tc := range map[string]struct {
		token string
		want  error
	}{
		"not a token at all": {"random-string", mcptoken.ErrTokenNotFound},
		"unknown token":      {mcptoken.TokenPrefix + "nope", mcptoken.ErrTokenNotFound},
		"revoked token":      {revoked.Token, mcptoken.ErrTokenRevoked},
		"expired token":      {mcptoken.TokenPrefix + "expired-secret", mcptoken.ErrTokenExpired},
		"inactive owner":     {inactiveToken.Token, mcptoken.ErrUserInactive},
	} {
		_, _, err := e.auth.Execute(ctx, tc.token, "")
		require.Error(t, err, "%s must fail", name)
		assert.ErrorIs(t, err, tc.want, "%s", name)
	}

	// The live one still works, so the loop above is not just failing on
	// everything.
	_, _, err = e.auth.Execute(ctx, valid.Token, "")
	require.NoError(t, err)
}

func TestRevokeIsScopedToTheOwner(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	out, err := e.create.Execute(ctx, alice.ID, mcptoken.CreateInput{Name: "laptop"})
	require.NoError(t, err)

	// Reported as not found rather than forbidden, so the endpoint cannot be
	// used to probe which token ids exist.
	err = e.revoke.Execute(ctx, bob.ID, out.ID, "", "")
	require.ErrorIs(t, err, mcptoken.ErrTokenNotFound)

	_, _, err = e.auth.Execute(ctx, out.Token, "")
	require.NoError(t, err, "the token must still work after a foreign revoke attempt")

	require.NoError(t, e.revoke.Execute(ctx, alice.ID, out.ID, "", ""))
	_, _, err = e.auth.Execute(ctx, out.Token, "")
	assert.ErrorIs(t, err, mcptoken.ErrTokenRevoked)
}

func TestListOnlyReturnsTheCallersLiveTokens(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	_, err := e.create.Execute(ctx, alice.ID, mcptoken.CreateInput{Name: "alice-1"})
	require.NoError(t, err)
	gone, err := e.create.Execute(ctx, alice.ID, mcptoken.CreateInput{Name: "alice-2"})
	require.NoError(t, err)
	_, err = e.create.Execute(ctx, bob.ID, mcptoken.CreateInput{Name: "bob-1"})
	require.NoError(t, err)
	require.NoError(t, e.revoke.Execute(ctx, alice.ID, gone.ID, "", ""))

	listed, err := e.list.Execute(ctx, alice.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "alice-1", listed[0].Name)
}
