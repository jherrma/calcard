package repository

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"gorm.io/gorm"
)

type AddressBookRepository struct {
	db *gorm.DB
}

func NewAddressBookRepository(db *gorm.DB) addressbook.Repository {
	return &AddressBookRepository{db: db}
}

func (r *AddressBookRepository) Create(ctx context.Context, ab *addressbook.AddressBook) error {
	return r.db.WithContext(ctx).Create(ab).Error
}

func (r *AddressBookRepository) GetByID(ctx context.Context, id uint) (*addressbook.AddressBook, error) {
	var ab addressbook.AddressBook
	if err := r.db.WithContext(ctx).First(&ab, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Or custom Not Found error
		}
		return nil, err
	}
	return &ab, nil
}

func (r *AddressBookRepository) GetByUUID(ctx context.Context, uuid string) (*addressbook.AddressBook, error) {
	var ab addressbook.AddressBook
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&ab).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ab, nil
}

func (r *AddressBookRepository) ListByUserID(ctx context.Context, userID uint) ([]addressbook.AddressBook, error) {
	var abs []addressbook.AddressBook
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&abs).Error; err != nil {
		return nil, err
	}
	return abs, nil
}

func (r *AddressBookRepository) Update(ctx context.Context, ab *addressbook.AddressBook) error {
	return r.db.WithContext(ctx).Save(ab).Error
}

func (r *AddressBookRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&addressbook.AddressBook{}, id).Error
}

// Address Object methods
func (r *AddressBookRepository) CreateObject(ctx context.Context, object *addressbook.AddressObject) error {
	return r.db.WithContext(ctx).Create(object).Error
}

func (r *AddressBookRepository) GetObjectByID(ctx context.Context, id uint) (*addressbook.AddressObject, error) {
	var obj addressbook.AddressObject
	if err := r.db.WithContext(ctx).First(&obj, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &obj, nil
}

func (r *AddressBookRepository) ListObjects(ctx context.Context, addressBookID uint) ([]addressbook.AddressObject, error) {
	var objs []addressbook.AddressObject
	if err := r.db.WithContext(ctx).Where("address_book_id = ?", addressBookID).Find(&objs).Error; err != nil {
		return nil, err
	}
	return objs, nil
}
