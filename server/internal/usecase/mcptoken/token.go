// Package mcptoken issues and verifies the long-lived bearer tokens that
// authenticate MCP clients (story 104).
//
// Why a dedicated credential rather than the JWT the web UI uses: an access
// token lives ten minutes by default, which is fine for a browser holding a
// refresh token and useless for an MCP client that is configured once with a
// static `Authorization` header. And why not an app password: those are
// scoped to CalDAV/CardDAV, are verified with bcrypt against a username the
// client also supplies, and widening them would silently grant every existing
// DAV credential a full read/write tool surface. An MCP token is its own thing,
// listed and revocable on its own settings page, and grants exactly the MCP
// endpoint.
package mcptoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
)

// TokenPrefix marks a secret as an MCP token. It lets the transport route a
// bearer credential to the right verifier without trying (and failing) JWT
// parsing first, and it makes a leaked token greppable in logs and repos.
const TokenPrefix = "calcard_mcp_"

// secretBytes is the entropy behind each token. 32 bytes = 256 bits, which is
// what makes storing a plain SHA-256 (rather than a slow KDF) sound: there is
// no dictionary to run against it.
const secretBytes = 32

// displayPrefixLen is how much of the random part is kept in the clear so a
// token can be told apart from its siblings in the settings list.
const displayPrefixLen = 6

// Sentinel errors. Every authentication failure is reported to the client as
// the same opaque 401; the distinctions exist for the security log.
var (
	ErrTokenNotFound = errors.New("mcp token not found")
	ErrTokenRevoked  = errors.New("mcp token revoked")
	ErrTokenExpired  = errors.New("mcp token expired")
	ErrUserInactive  = errors.New("user account is not active")
	ErrInvalidInput  = errors.New("invalid input")
)

// maxTokenLifetime bounds an explicit expiry. A token that outlives the memory
// of having created it is the one that leaks; ten years is not an expiry.
const maxTokenLifetime = 5 * 365 * 24 * time.Hour

// Generate mints a new token secret and returns both the string to show the
// user exactly once and the values persisted alongside it.
func Generate() (raw, hash, displayPrefix string, err error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("failed to generate token: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	raw = TokenPrefix + secret
	return raw, HashToken(raw), TokenPrefix + secret[:displayPrefixLen], nil
}

// HashToken maps a raw token to the value stored in the database. Callers hash
// what the client sent and look that up; the raw secret is never persisted.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// LooksLikeToken reports whether a bearer credential is an MCP token rather
// than a JWT. It is a routing hint only — never an authorization decision.
func LooksLikeToken(credential string) bool {
	return strings.HasPrefix(credential, TokenPrefix)
}

// CreateInput describes a token to mint. ExpiresAt is optional; nil means the
// token is valid until revoked.
type CreateInput struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
	IP        string     `json:"-"`
	UserAgent string     `json:"-"`
}

// CreateOutput carries the one and only delivery of the secret. Token is
// absent from every other response shape by construction: the model has no
// field to render it from.
type CreateOutput struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Token       string     `json:"token"`
	TokenPrefix string     `json:"token_prefix"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreateUseCase mints MCP tokens.
type CreateUseCase struct {
	repo   user.MCPTokenRepository
	logger *logging.SecurityLogger
}

func NewCreateUseCase(repo user.MCPTokenRepository, logger *logging.SecurityLogger) *CreateUseCase {
	return &CreateUseCase{repo: repo, logger: logger}
}

// Execute mints a token for userID and returns the secret exactly once.
func (uc *CreateUseCase) Execute(ctx context.Context, userID uint, input CreateInput) (*CreateOutput, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: name is required and must be at most 100 characters", ErrInvalidInput)
	}
	if input.ExpiresAt != nil {
		if !input.ExpiresAt.After(time.Now()) {
			return nil, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
		}
		if input.ExpiresAt.After(time.Now().Add(maxTokenLifetime)) {
			return nil, fmt.Errorf("%w: expires_at must be at most 5 years from now", ErrInvalidInput)
		}
	}

	raw, hash, displayPrefix, err := Generate()
	if err != nil {
		return nil, err
	}

	token := &user.MCPToken{
		UUID:        uuid.New().String(),
		UserID:      userID,
		Name:        name,
		TokenHash:   hash,
		TokenPrefix: displayPrefix,
		ExpiresAt:   input.ExpiresAt,
		CreatedAt:   time.Now(),
	}
	if err := uc.repo.Create(ctx, token); err != nil {
		return nil, fmt.Errorf("failed to create mcp token: %w", err)
	}

	uc.logger.LogMCPTokenCreated(ctx, userID, name, input.IP, input.UserAgent)

	return &CreateOutput{
		ID:          token.UUID,
		Name:        token.Name,
		Token:       raw,
		TokenPrefix: token.TokenPrefix,
		ExpiresAt:   token.ExpiresAt,
		CreatedAt:   token.CreatedAt,
	}, nil
}

// ListUseCase lists a user's live MCP tokens.
type ListUseCase struct {
	repo user.MCPTokenRepository
}

func NewListUseCase(repo user.MCPTokenRepository) *ListUseCase {
	return &ListUseCase{repo: repo}
}

func (uc *ListUseCase) Execute(ctx context.Context, userID uint) ([]user.MCPToken, error) {
	return uc.repo.ListByUserID(ctx, userID)
}

// RevokeUseCase revokes an MCP token.
type RevokeUseCase struct {
	repo   user.MCPTokenRepository
	logger *logging.SecurityLogger
}

func NewRevokeUseCase(repo user.MCPTokenRepository, logger *logging.SecurityLogger) *RevokeUseCase {
	return &RevokeUseCase{repo: repo, logger: logger}
}

// Execute revokes tokenUUID if it belongs to userID. A token owned by someone
// else is reported as not found, so the endpoint cannot be used to probe which
// token ids exist.
func (uc *RevokeUseCase) Execute(ctx context.Context, userID uint, tokenUUID, ip, userAgent string) error {
	token, err := uc.repo.GetByUUID(ctx, tokenUUID)
	if err != nil {
		return fmt.Errorf("failed to look up mcp token: %w", err)
	}
	if token == nil || token.UserID != userID {
		return ErrTokenNotFound
	}
	if err := uc.repo.Revoke(ctx, token.ID); err != nil {
		return fmt.Errorf("failed to revoke mcp token: %w", err)
	}
	uc.logger.LogMCPTokenRevoked(ctx, userID, token.Name, ip, userAgent)
	return nil
}

// AuthenticateUseCase resolves a presented bearer token to its owner.
type AuthenticateUseCase struct {
	repo     user.MCPTokenRepository
	userRepo user.UserRepository
}

func NewAuthenticateUseCase(repo user.MCPTokenRepository, userRepo user.UserRepository) *AuthenticateUseCase {
	return &AuthenticateUseCase{repo: repo, userRepo: userRepo}
}

// Execute verifies a raw token and returns its owner.
//
// The stored hash is compared in constant time. That is belt-and-braces given
// the lookup is already by hash — an attacker who can produce the hash has the
// token — but it costs nothing and keeps the comparison honest if the lookup
// ever changes.
//
// ip is recorded as the token's last use. A failure to record it is deliberately
// ignored: the request has already authenticated, and refusing it because an
// audit column could not be written would turn a bookkeeping problem into an
// outage.
func (uc *AuthenticateUseCase) Execute(ctx context.Context, raw, ip string) (*user.User, *user.MCPToken, error) {
	if !LooksLikeToken(raw) {
		return nil, nil, ErrTokenNotFound
	}
	hash := HashToken(raw)
	token, err := uc.repo.GetByHash(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to look up mcp token: %w", err)
	}
	if token == nil {
		return nil, nil, ErrTokenNotFound
	}
	if subtle.ConstantTimeCompare([]byte(token.TokenHash), []byte(hash)) != 1 {
		return nil, nil, ErrTokenNotFound
	}
	if token.IsRevoked() {
		return nil, nil, ErrTokenRevoked
	}
	if token.IsExpired() {
		return nil, nil, ErrTokenExpired
	}

	u, err := uc.userRepo.GetByID(ctx, token.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load token owner: %w", err)
	}
	if u == nil {
		// The owner was deleted while the token lived on. Treat it as an
		// unknown token rather than falling through with a nil user.
		return nil, nil, ErrTokenNotFound
	}
	if !u.IsActive {
		return nil, nil, ErrUserInactive
	}

	_ = uc.repo.UpdateLastUsed(ctx, token.ID, ip)

	return u, token, nil
}
