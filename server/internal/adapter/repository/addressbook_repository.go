package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-vcard"
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
// Helpers for Photo Management
func (r *AddressBookRepository) extractPhoto(vcardData string) (string, string, string, error) {
	dec := vcard.NewDecoder(strings.NewReader(vcardData))
	card, err := dec.Decode()
	if err != nil {
		return "", "", "", err
	}

	photoField := card.Get(vcard.FieldPhoto)
	if photoField == nil {
		return vcardData, "", "", nil // No photo, return original
	}

	// Extract photo data
	photoData := photoField.Value

	// Extract type
	photoType := "JPEG" // Default
	if photoField.Params != nil {
		types := photoField.Params.Types()
		if len(types) > 0 {
			photoType = strings.ToUpper(types[0])
		} else {
			// Try "TYPE" param explicitly if Types() helper doesn't catch it
			t := photoField.Params.Get("TYPE")
			if t != "" {
				photoType = strings.ToUpper(t)
			}
		}
	}

	// Remove photo from card
	delete(card, vcard.FieldPhoto)

	// Re-encode card without photo
	var buf bytes.Buffer
	enc := vcard.NewEncoder(&buf)
	if err := enc.Encode(card); err != nil {
		return "", "", "", err
	}

	return buf.String(), photoData, photoType, nil
}

func (r *AddressBookRepository) injectPhoto(vcardData string, photoData string, photoType string) (string, error) {
	if photoData == "" {
		return vcardData, nil
	}

	dec := vcard.NewDecoder(strings.NewReader(vcardData))
	card, err := dec.Decode()
	if err != nil {
		return "", err
	}

	// Add photo back
	params := make(vcard.Params)
	params.Set("ENCODING", "b")
	if photoType == "" {
		photoType = "JPEG"
	}
	params.Set("TYPE", photoType)

	card.Add(vcard.FieldPhoto, &vcard.Field{Value: photoData, Params: params})

	var buf bytes.Buffer
	enc := vcard.NewEncoder(&buf)
	if err := enc.Encode(card); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (r *AddressBookRepository) CreateObject(ctx context.Context, object *addressbook.AddressObject) error {
	// Extract photo from vCardData
	strippedVCard, photoData, photoType, err := r.extractPhoto(object.VCardData)
	if err != nil {
		return fmt.Errorf("failed to process vcard: %w", err)
	}

	// Use stripped data for main object
	object.VCardData = strippedVCard

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(object).Error; err != nil {
			return err
		}

		if photoData != "" {
			photo := &addressbook.ContactPhoto{
				AddressObjectID: object.ID,
				PhotoData:       photoData,
				PhotoType:       photoType,
			}
			if err := tx.WithContext(ctx).Create(photo).Error; err != nil {
				return err
			}
		}
		return nil
	})
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

func (r *AddressBookRepository) ListObjects(ctx context.Context, addressBookID uint, limit, offset int, sortField, order string) ([]addressbook.AddressObject, int64, error) {
	var objs []addressbook.AddressObject
	var total int64

	db := r.db.WithContext(ctx).Model(&addressbook.AddressObject{}).Where("address_book_id = ?", addressBookID)

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Sorting
	// Map allowed sort fields to DB columns
	// "name" -> "given_name", "family_name"
	// "email" -> "email"
	// "updated_at" -> "updated_at"

	dbOrder := "ASC"
	if strings.ToUpper(order) == "DESC" {
		dbOrder = "DESC"
	}

	query := db
	switch sortField {
	case "email":
		query = query.Order(fmt.Sprintf("email %s", dbOrder))
	case "updated_at":
		query = query.Order(fmt.Sprintf("updated_at %s", dbOrder))
	case "name":
		fallthrough
	default:
		// Sort by First Name then Last Name
		query = query.Order(fmt.Sprintf("given_name %s", dbOrder)).Order(fmt.Sprintf("family_name %s", dbOrder))
	}

	if err := query.Limit(limit).Offset(offset).Find(&objs).Error; err != nil {
		return nil, 0, err
	}

	return objs, total, nil
}

func (r *AddressBookRepository) GetObjectByUUID(ctx context.Context, uuid string) (*addressbook.AddressObject, error) {
	var obj addressbook.AddressObject
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&obj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	// Fetch Photo
	var photo addressbook.ContactPhoto
	if err := r.db.WithContext(ctx).Where("address_object_id = ?", obj.ID).First(&photo).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		// No photo found, proceed
	}

	// Inject Photo if exists
	if photo.PhotoData != "" {
		fullVCard, err := r.injectPhoto(obj.VCardData, photo.PhotoData, photo.PhotoType)
		if err != nil {
			return nil, fmt.Errorf("failed to inject photo: %w", err)
		}
		obj.VCardData = fullVCard
	}

	return &obj, nil
}

func (r *AddressBookRepository) UpdateObject(ctx context.Context, object *addressbook.AddressObject) error {
	// Extract photo from vCardData
	strippedVCard, photoData, photoType, err := r.extractPhoto(object.VCardData)
	if err != nil {
		return fmt.Errorf("failed to process vcard: %w", err)
	}

	// Use stripped data for main object
	object.VCardData = strippedVCard

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Save(object).Error; err != nil {
			return err
		}

		// Handle Photo
		if photoData != "" {
			// Upsert photo
			var photo addressbook.ContactPhoto
			// Check if exists
			err := tx.WithContext(ctx).Where("address_object_id = ?", object.ID).First(&photo).Error
			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				// Create new
				photo = addressbook.ContactPhoto{
					AddressObjectID: object.ID,
					PhotoData:       photoData,
					PhotoType:       photoType,
				}
				if err := tx.WithContext(ctx).Create(&photo).Error; err != nil {
					return err
				}
			} else {
				// Update existing
				photo.PhotoData = photoData
				photo.PhotoType = photoType
				if err := tx.WithContext(ctx).Save(&photo).Error; err != nil {
					return err
				}
			}
		} else {
			// If vCard has no photo, ensure no photo record exists.
			if err := tx.WithContext(ctx).Where("address_object_id = ?", object.ID).Delete(&addressbook.ContactPhoto{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *AddressBookRepository) DeleteObjectByUUID(ctx context.Context, uuid string) error {
	return r.db.WithContext(ctx).Where("uuid = ?", uuid).Delete(&addressbook.AddressObject{}).Error
}

func (r *AddressBookRepository) SearchObjects(ctx context.Context, userID uint, query string, addressBookID *uint, limit int) ([]addressbook.AddressObject, error) {
	var objs []addressbook.AddressObject
	q := "%" + query + "%"

	// Join with AddressBooks to filter by UserID
	// And filter by query on denormalized fields
	db := r.db.WithContext(ctx).
		Joins("JOIN address_books ON address_books.id = address_objects.address_book_id").
		Where("address_books.user_id = ?", userID)

	if addressBookID != nil {
		db = db.Where("address_objects.address_book_id = ?", *addressBookID)
	}

	err := db.Where("address_objects.formatted_name LIKE ? OR address_objects.email LIKE ? OR address_objects.phone LIKE ? OR address_objects.organization LIKE ? OR address_objects.given_name LIKE ? OR address_objects.family_name LIKE ?", q, q, q, q, q, q).
		Limit(limit).
		Find(&objs).Error

	if err != nil {
		return nil, err
	}
	return objs, nil
}
