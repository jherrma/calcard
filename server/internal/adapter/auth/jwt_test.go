package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestJWTManager() *JWTManager {
	return NewJWTManager(&config.JWTConfig{Secret: "test-secret", AccessExpiry: time.Hour})
}

// SCOPE: P02 — access tokens carry their issue instant at nanosecond precision
// (iat_ns), so revocation is not blind to the second in which it happened.
func TestAccessTokenCarriesSubSecondIssuedAt(t *testing.T) {
	m := newTestJWTManager()

	before := time.Now()
	token, _, err := m.GenerateAccessToken("user-uuid", "user@example.com")
	require.NoError(t, err)
	after := time.Now()

	claims, err := m.ValidateAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-uuid", claims.UserUUID)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.False(t, claims.IssuedAt.Before(before.Truncate(0)), "IssuedAt must not predate generation")
	assert.False(t, claims.IssuedAt.After(after), "IssuedAt must not postdate generation")

	// Two tokens minted back to back inside one second are still ordered.
	second, _, err := m.GenerateAccessToken("user-uuid", "user@example.com")
	require.NoError(t, err)
	secondClaims, err := m.ValidateAccessToken(second)
	require.NoError(t, err)
	assert.True(t, secondClaims.IssuedAt.After(claims.IssuedAt) || secondClaims.IssuedAt.Equal(claims.IssuedAt))
}

// A token minted before iat_ns existed falls back to the whole-second iat,
// which rounds DOWN: it can only look older than it is, never newer, so a
// marker set after it was issued still rejects it.
func TestLegacyAccessTokenFallsBackToWholeSecondIat(t *testing.T) {
	m := newTestJWTManager()
	issued := time.Now().Truncate(time.Second).Add(-time.Minute)

	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, CustomClaims{
		UserUUID: "user-uuid",
		Email:    "user@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(issued),
		},
	})
	signed, err := legacy.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	claims, err := m.ValidateAccessToken(signed)
	require.NoError(t, err)
	assert.True(t, claims.IssuedAt.Equal(issued))
}

func TestAccessTokenWithoutAnyIatIsInfinitelyOld(t *testing.T) {
	m := newTestJWTManager()
	bare := jwt.NewWithClaims(jwt.SigningMethodHS256, CustomClaims{
		UserUUID: "user-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	signed, err := bare.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	claims, err := m.ValidateAccessToken(signed)
	require.NoError(t, err)
	assert.True(t, claims.IssuedAt.IsZero())
}
