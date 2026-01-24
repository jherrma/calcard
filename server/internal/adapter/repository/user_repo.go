package repository

import (
	"context"
	"errors"

	"strings"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type gormUserRepo struct {
	db *gorm.DB
}

// NewUserRepository creates a new GORM-based user repository
func NewUserRepository(db *gorm.DB) user.UserRepository {
	return &gormUserRepo{db: db}
}

func (r *gormUserRepo) Create(ctx context.Context, u *user.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *gormUserRepo) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u user.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepo) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	var u user.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepo) GetByUUID(ctx context.Context, uuid string) (*user.User, error) {
	var u user.User
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepo) GetByID(ctx context.Context, id uint) (*user.User, error) {
	var u user.User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepo) Update(ctx context.Context, u *user.User) error {
	return r.db.WithContext(ctx).Save(u).Error
}

func (r *gormUserRepo) Delete(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Hard delete related data
		if err := tx.Where("user_id = ?", userID).Delete(&user.RefreshToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&user.PasswordReset{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&user.EmailVerification{}).Error; err != nil {
			return err
		}

		// Soft delete user
		return tx.Delete(&user.User{}, userID).Error
	})
}

func (r *gormUserRepo) CreateVerification(ctx context.Context, v *user.EmailVerification) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *gormUserRepo) GetVerificationByToken(ctx context.Context, token string) (*user.EmailVerification, error) {
	var v user.EmailVerification
	if err := r.db.WithContext(ctx).Preload("User").Where("token = ?", token).First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

func (r *gormUserRepo) DeleteVerification(ctx context.Context, token string) error {
	return r.db.WithContext(ctx).Where("token = ?", token).Delete(&user.EmailVerification{}).Error
}
