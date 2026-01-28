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
	jwt.RegisteredClaims
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(cfg *config.JWTConfig) *JWTManager {
	return &JWTManager{cfg: cfg}
}

// GenerateAccessToken generates a short-lived access token
func (m *JWTManager) GenerateAccessToken(userID string, email string) (string, time.Time, error) {
	exp := time.Now().Add(m.cfg.AccessExpiry)
	claims := CustomClaims{
		UserUUID: userID,
		Email:    email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(m.cfg.Secret))
	return signed, exp, err
}

// ValidateAccessToken validates an access token and returns user details
func (m *JWTManager) ValidateAccessToken(tokenStr string) (string, string, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(m.cfg.Secret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", "", ErrExpiredToken
		}
		return "", "", ErrInvalidToken
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok || !token.Valid {
		return "", "", ErrInvalidToken
	}

	return claims.UserUUID, claims.Email, nil
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
