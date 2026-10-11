package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain"
	domainuser "github.com/jherrma/caldav-server/internal/domain/user"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// JWTManager handles JWT generation and validation
type JWTManager struct {
	cfg *config.JWTConfig
}

// CustomClaims represents the JWT claims
type CustomClaims struct {
	UserUUID string `json:"sub"`
	Email    string `json:"email"`
	// IssuedAtNano repeats iat at nanosecond precision. RFC 7519's iat is
	// whole seconds, which would leave a one-second window in which a token
	// minted just BEFORE a logout/password change survives it, or one minted
	// just AFTER (the replacement token change-password hands out) dies. See
	// user.User.AcceptsAccessTokenIssuedAt.
	IssuedAtNano int64 `json:"iat_ns,omitempty"`
	jwt.RegisteredClaims
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(cfg *config.JWTConfig) *JWTManager {
	return &JWTManager{cfg: cfg}
}

// GenerateAccessToken generates a short-lived access token
func (m *JWTManager) GenerateAccessToken(userID string, email string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(m.cfg.AccessExpiry)
	claims := CustomClaims{
		UserUUID:     userID,
		Email:        email,
		IssuedAtNano: now.UnixNano(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(m.cfg.Secret))
	return signed, exp, err
}

// ValidateAccessToken validates an access token's signature and expiry and
// returns its claims. It cannot know whether the token has since been revoked:
// callers must load the user and check AcceptsAccessTokenIssuedAt.
func (m *JWTManager) ValidateAccessToken(tokenStr string) (*domainuser.AccessTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(m.cfg.Secret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return &domainuser.AccessTokenClaims{
		UserUUID: claims.UserUUID,
		Email:    claims.Email,
		IssuedAt: issuedAt(claims),
	}, nil
}

// issuedAt prefers the nanosecond iat_ns claim. Tokens minted before it
// existed fall back to the whole-second iat, which rounds DOWN — so such a
// token compares as older than any marker set after it was issued, never
// newer. A token with neither claim is treated as infinitely old.
func issuedAt(c *CustomClaims) time.Time {
	if c.IssuedAtNano != 0 {
		return time.Unix(0, c.IssuedAtNano)
	}
	if c.IssuedAt != nil {
		return c.IssuedAt.Time
	}
	return time.Time{}
}

// GenerateRefreshToken generates an opaque long-lived refresh token
func (m *JWTManager) GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns a SHA256 hash of a token
func (m *JWTManager) HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// EnsureSecret ensures that a JWT secret exists, fetching it from the DB or generating it if needed
func (m *JWTManager) EnsureSecret(ctx context.Context, repo domain.SystemSettingRepository) error {
	if m.cfg.Secret != "" {
		return nil // Secret already set (e.g. from env)
	}

	key := "jwt_secret"
	secret, err := repo.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to get jwt secret from db: %w", err)
	}

	if secret != "" {
		m.cfg.Secret = secret
		return nil
	}

	// Generate new secret
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("failed to generate random secret: %w", err)
	}
	secret = hex.EncodeToString(b)

	if err := repo.Set(ctx, key, secret); err != nil {
		return fmt.Errorf("failed to save jwt secret to db: %w", err)
	}

	m.cfg.Secret = secret
	return nil
}
