package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// MCPTokenRepository implements user.MCPTokenRepository
type MCPTokenRepository struct {
	db *gorm.DB
}

// NewMCPTokenRepository creates a new MCPTokenRepository
func NewMCPTokenRepository(db *gorm.DB) *MCPTokenRepository {
	return &MCPTokenRepository{db: db}
}

// Create stores a new MCP token.
func (r *MCPTokenRepository) Create(ctx context.Context, token *user.MCPToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

// GetByUUID retrieves a non-revoked MCP token by its public UUID. A missing or
// revoked row yields (nil, nil) so callers can answer "not found" without
// distinguishing an unknown id from a revoked one.
func (r *MCPTokenRepository) GetByUUID(ctx context.Context, uuid string) (*user.MCPToken, error) {
	var token user.MCPToken
	err := r.db.WithContext(ctx).Where("uuid = ? AND revoked_at IS NULL", uuid).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// GetByHash retrieves an MCP token by the SHA-256 of the presented secret.
//
// Unlike GetByUUID this deliberately does NOT filter on revoked_at: the caller
// validates revocation and expiry itself so it can log a revoked-token
// presentation, which is a materially different signal from a token that never
// existed. A miss is (nil, nil), not an error.
func (r *MCPTokenRepository) GetByHash(ctx context.Context, hash string) (*user.MCPToken, error) {
	var token user.MCPToken
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// ListByUserID lists a user's non-revoked MCP tokens, newest first.
func (r *MCPTokenRepository) ListByUserID(ctx context.Context, userID uint) ([]user.MCPToken, error) {
	var tokens []user.MCPToken
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Order("created_at DESC").
		Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}

// Revoke soft-deletes a token by stamping revoked_at. The row is kept so the
// hash stays reserved and a replay of the old secret is recognizable.
func (r *MCPTokenRepository) Revoke(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&user.MCPToken{}).
		Where("id = ?", id).
		Update("revoked_at", time.Now()).Error
}

// UpdateLastUsed records the most recent successful use. Failures here must not
// fail the request that triggered them — callers log and continue.
func (r *MCPTokenRepository) UpdateLastUsed(ctx context.Context, id uint, ip string) error {
	return r.db.WithContext(ctx).
		Model(&user.MCPToken{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_used_at": time.Now(),
			"last_used_ip": ip,
		}).Error
}
