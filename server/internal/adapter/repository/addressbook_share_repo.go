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
	if err := r.db.WithContext(ctx).Where("addressbook_id = ?", addressBookID).Preload("SharedWith").Find(&shares).Error; err != nil {
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
	return r.db.WithContext(ctx).Delete(&sharing.AddressBookShare{}, id).Error
}

func (r *gormAddressBookShareRepo) GetByAddressBookAndUser(ctx context.Context, addressBookID, userID uint) (*sharing.AddressBookShare, error) {
	var share sharing.AddressBookShare
	if err := r.db.WithContext(ctx).Where("addressbook_id = ? AND shared_with_id = ?", addressBookID, userID).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}
