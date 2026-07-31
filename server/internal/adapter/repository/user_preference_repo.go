package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gormUserPreferenceRepo struct {
	db *gorm.DB
}

// NewUserPreferenceRepository creates a new GORM-based user preference repository
func NewUserPreferenceRepository(db *gorm.DB) user.UserPreferenceRepository {
	return &gormUserPreferenceRepo{db: db}
}

func (r *gormUserPreferenceRepo) GetByUserID(ctx context.Context, userID uint) ([]user.UserPreference, error) {
	var prefs []user.UserPreference
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&prefs).Error; err != nil {
		return nil, err
	}
	return prefs, nil
}

func (r *gormUserPreferenceRepo) GetByKey(ctx context.Context, userID uint, key string) (*user.UserPreference, error) {
	var pref user.UserPreference
	if err := r.db.WithContext(ctx).Where("user_id = ? AND key = ?", userID, key).First(&pref).Error; err != nil {
		// "not set" is a normal state, not an error — mirrors the other repos in
		// this package, which return (nil, nil) for a missing row.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &pref, nil
}

// Upsert relies on the idx_user_pref_key unique index over (user_id, key), which
// AutoMigrate creates from the struct tags. The conflict target must name those
// two columns in that order for both SQLite and PostgreSQL to match the index.
func (r *gormUserPreferenceRepo) Upsert(ctx context.Context, pref *user.UserPreference) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "key"}},
		// AssignmentColumns writes the excluded (incoming) row's columns, so
		// updated_at advances on every write while created_at is left alone.
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(pref).Error
}

func (r *gormUserPreferenceRepo) Delete(ctx context.Context, userID uint, key string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND key = ?", userID, key).
		Delete(&user.UserPreference{}).Error
}
