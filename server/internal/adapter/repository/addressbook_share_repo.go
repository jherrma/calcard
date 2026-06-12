package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"gorm.io/gorm"
)

type gormAddressBookShareRepo struct {
	db *gorm.DB
}

// NewAddressBookShareRepository creates a new GORM-based AddressBookShare repository
func NewAddressBookShareRepository(db *gorm.DB) sharing.AddressBookShareRepository {
	return &gormAddressBookShareRepo{db: db}
}

func (r *gormAddressBookShareRepo) Create(ctx context.Context, share *sharing.AddressBookShare) error {
	return r.db.WithContext(ctx).Create(share).Error
}

func (r *gormAddressBookShareRepo) GetByUUID(ctx context.Context, uuid string) (*sharing.AddressBookShare, error) {
	var share sharing.AddressBookShare
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).Preload("SharedWith").First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}

func (r *gormAddressBookShareRepo) ListByAddressBookID(ctx context.Context, addressBookID uint) ([]sharing.AddressBookShare, error) {
	var shares []sharing.AddressBookShare
	if err := r.db.WithContext(ctx).Where("address_book_id = ?", addressBookID).Preload("SharedWith").Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

func (r *gormAddressBookShareRepo) FindAddressBooksSharedWithUser(ctx context.Context, userID uint) ([]sharing.AddressBookShare, error) {
	var shares []sharing.AddressBookShare
	if err := r.db.WithContext(ctx).Where("shared_with_id = ?", userID).Preload("AddressBook").Preload("AddressBook.User").Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

func (r *gormAddressBookShareRepo) Update(ctx context.Context, share *sharing.AddressBookShare) error {
	return r.db.WithContext(ctx).Save(share).Error
}

func (r *gormAddressBookShareRepo) Revoke(ctx context.Context, id uint) error {
	// Hard-delete: the composite unique index (address_book_id, shared_with_id)
	// is not partial on deleted_at, so a soft-deleted row would keep blocking
	// any future re-share of the same (address book, user) pair.
	return r.db.WithContext(ctx).Unscoped().Delete(&sharing.AddressBookShare{}, id).Error
}

func (r *gormAddressBookShareRepo) DeleteByAddressBookID(ctx context.Context, addressBookID uint) error {
	// Hard-delete (Unscoped) so revoked rows don't linger under the composite
	// unique index — same rationale as Revoke.
	return r.db.WithContext(ctx).Unscoped().Where("address_book_id = ?", addressBookID).Delete(&sharing.AddressBookShare{}).Error
}

func (r *gormAddressBookShareRepo) GetByAddressBookAndUser(ctx context.Context, addressBookID, userID uint) (*sharing.AddressBookShare, error) {
	var share sharing.AddressBookShare
	if err := r.db.WithContext(ctx).Where("address_book_id = ? AND shared_with_id = ?", addressBookID, userID).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}
