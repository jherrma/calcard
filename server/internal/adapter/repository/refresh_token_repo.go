package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type gormRefreshTokenRepo struct {
	db *gorm.DB
}

// NewRefreshTokenRepository creates a new GORM-based refresh token repository
func NewRefreshTokenRepository(db *gorm.DB) user.RefreshTokenRepository {
	return &gormRefreshTokenRepo{db: db}
}

func (r *gormRefreshTokenRepo) Create(ctx context.Context, t *user.RefreshToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *gormRefreshTokenRepo) GetByHash(ctx context.Context, hash string) (*user.RefreshToken, error) {
	// Automatically delete expired tokens
	if err := r.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&user.RefreshToken{}).Error; err != nil {
		// Log error but don't fail the request
		fmt.Printf("failed to cleanup expired refresh tokens: %v\n", err)
	}

	var t user.RefreshToken
	if err := r.db.WithContext(ctx).Preload("User").Where("token_hash = ?", hash).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (r *gormRefreshTokenRepo) DeleteByHash(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).Where("token_hash = ?", hash).Delete(&user.RefreshToken{}).Error
}

func (r *gormRefreshTokenRepo) DeleteByUserID(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&user.RefreshToken{}).Error
}

// Rotate atomically supersedes oldHash with newToken: it marks the old row
// revoked and points its ReplacedByHash at the successor's hash, then creates
// the successor — both in one transaction, so a crash can never leave both
// tokens live (reuse window) or both dead (lockout).
//
// The revoke is CONDITIONAL on the old row still being live (revoked_at IS
// NULL) and runs FIRST: if a concurrent request already rotated the same token,
// RowsAffected is 0 and we return user.ErrRefreshTokenRotated, rolling the
// transaction back before any successor is inserted. Without this, two
// simultaneous refreshes of the same token would both mint live successors and
// slip past reuse detection.
func (r *gormRefreshTokenRepo) Rotate(ctx context.Context, oldHash string, newToken *user.RefreshToken) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Claim the old token FIRST, and only if it is still live. A map update
		// (not a struct) so the zero-value guard doesn't drop the revoked_at /
		// replaced_by_hash assignments.
		res := tx.Model(&user.RefreshToken{}).
			Where("token_hash = ? AND revoked_at IS NULL", oldHash).
			Updates(map[string]interface{}{
				"revoked_at":       time.Now(),
				"replaced_by_hash": newToken.TokenHash,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Already revoked/rotated by a concurrent request: abort without
			// creating a second live successor.
			return user.ErrRefreshTokenRotated
		}
		return tx.Create(newToken).Error
	})
}

// RevokeFamily revokes every still-live token whose family_id matches. It
// refuses an empty familyID outright: legacy pre-rotation rows carry
// family_id=” and a blanket revoke of that value would nuke unrelated users'
// sessions (see #75). The caller treats a legacy token as a single-token reject.
func (r *gormRefreshTokenRepo) RevokeFamily(ctx context.Context, familyID string) error {
	if familyID == "" {
		return errors.New("refresh token: refusing to revoke an empty token family")
	}
	return r.db.WithContext(ctx).Model(&user.RefreshToken{}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Update("revoked_at", time.Now()).Error
}
