package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-vcard"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"gorm.io/gorm"
)

type AddressBookRepository struct {
	db *gorm.DB
}

func NewAddressBookRepository(db *gorm.DB) addressbook.Repository {
	return &AddressBookRepository{db: db}
}

func (r *AddressBookRepository) Create(ctx context.Context, ab *addressbook.AddressBook) error {
	// Mint the initial sync token and write a matching "collection" anchor row
	// in the same transaction, so the first sync-collection REPORT hands out a
	// token that corresponds to a real change-log row (avoids the 403
	// valid-sync-token full-resync loop on fresh address books).
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		token := addressbook.GenerateSyncToken()
		ab.SyncToken = token
		ab.CTag = token
		if err := tx.Create(ab).Error; err != nil {
			return err
		}
		return tx.Create(&addressbook.SyncChangeLog{
			AddressBookID: ab.ID,
			ResourcePath:  "",
			ChangeType:    "collection",
			SyncToken:     token,
		}).Error
	})
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

// GetUserPermission resolves the user's effective permission on an address
// book: ownership first, then the share table (#53). A missing book or a user
// with no share yields PermissionNone with a nil error — callers map that to a
// 404 rather than distinguishing "gone" from "not yours". Deliberately mirrors
// CalendarRepository.GetUserPermission, including reading the share's raw
// "read"/"read-write" string.
func (r *AddressBookRepository) GetUserPermission(ctx context.Context, addressBookID, userID uint) (addressbook.AddressBookPermission, error) {
	var ab addressbook.AddressBook
	if err := r.db.WithContext(ctx).First(&ab, addressBookID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return addressbook.PermissionNone, nil
		}
		return addressbook.PermissionNone, err
	}

	if ab.UserID == userID {
		return addressbook.PermissionOwner, nil
	}

	var share sharing.AddressBookShare
	err := r.db.WithContext(ctx).
		Where("address_book_id = ? AND shared_with_id = ?", addressBookID, userID).
		First(&share).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return addressbook.PermissionNone, nil
		}
		return addressbook.PermissionNone, err
	}

	if share.Permission == "read-write" {
		return addressbook.PermissionReadWrite, nil
	}
	return addressbook.PermissionRead, nil
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

// UpdateMetadata persists a rename and mints a new sync token atomically. It
// updates only the metadata columns via Select (so a Save of the whole struct
// can't write back the stale sync_token/c_tag the caller loaded at request
// start, clobbering a token a concurrent object PUT just committed) and records
// the collection change in the same transaction (so a mid-rename failure can't
// leave the CTag un-bumped, hiding the new displayname from clients).
func (r *AddressBookRepository) UpdateMetadata(ctx context.Context, ab *addressbook.AddressBook) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&addressbook.AddressBook{}).
			Where("id = ?", ab.ID).
			Select("name", "description").
			Updates(ab).Error; err != nil {
			return err
		}
		return r.recordAddressBookChange(tx, ab.ID, "", "", "collection")
	})
}

func (r *AddressBookRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&addressbook.AddressBook{}, id).Error
}

func (r *AddressBookRepository) GetByUserAndPath(ctx context.Context, userID uint, path string) (*addressbook.AddressBook, error) {
	var ab addressbook.AddressBook
	if err := r.db.WithContext(ctx).Where("user_id = ? AND path = ?", userID, path).First(&ab).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ab, nil
}

func (r *AddressBookRepository) GetObjectByPath(ctx context.Context, addressBookID uint, path string) (*addressbook.AddressObject, error) {
	var obj addressbook.AddressObject
	if err := r.db.WithContext(ctx).Where("address_book_id = ? AND path = ?", addressBookID, path).First(&obj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if err := r.hydrateObjects(ctx, []*addressbook.AddressObject{&obj}); err != nil {
		return nil, err
	}
	return &obj, nil
}

// GetObjectByUID looks up an address object by its vCard UID within a specific
// address book (used for RFC 6352 no-uid-conflict detection on PUT). Returns
// (nil, nil) when not found.
func (r *AddressBookRepository) GetObjectByUID(ctx context.Context, addressBookID uint, uid string) (*addressbook.AddressObject, error) {
	var obj addressbook.AddressObject
	if err := r.db.WithContext(ctx).Where("address_book_id = ? AND uid = ?", addressBookID, uid).First(&obj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &obj, nil
}

// QueryObjects performs database-level filtering based on CardDAV query parameters.
// It maps vCard property names to database columns.
func (r *AddressBookRepository) QueryObjects(ctx context.Context, addressBookID uint, query *addressbook.ObjectQuery) ([]addressbook.AddressObject, error) {
	var objs []addressbook.AddressObject

	db := r.db.WithContext(ctx).Model(&addressbook.AddressObject{}).Where("address_book_id = ?", addressBookID)

	// Apply filters
	for _, filter := range query.Filters {
		db = r.applyFilter(db, filter)
	}

	// Apply limit
	if query.Limit > 0 {
		db = db.Limit(query.Limit)
	}

	if err := db.Find(&objs).Error; err != nil {
		return nil, err
	}

	if err := r.hydrateObjectSlice(ctx, objs); err != nil {
		return nil, err
	}

	return objs, nil
}

// applyFilter applies a single filter to the query.
func (r *AddressBookRepository) applyFilter(db *gorm.DB, filter addressbook.ObjectQueryFilter) *gorm.DB {
	// Map vCard property names to database columns
	column := r.propertyToColumn(filter.PropertyName)
	if column == "" {
		// Unknown property - can't filter at DB level, skip
		return db
	}

	if filter.IsNotDefined {
		if filter.NegateCondition {
			return db.Where(column + " IS NOT NULL AND " + column + " != ''")
		}
		return db.Where(column + " IS NULL OR " + column + " = ''")
	}

	searchText := strings.ToLower(filter.SearchText)

	var condition string
	var value interface{}

	switch filter.MatchType {
	case "equals":
		condition = "LOWER(" + column + ") = ?"
		value = searchText
	case "starts-with":
		condition = "LOWER(" + column + ") LIKE ? ESCAPE '\\'"
		value = escapeLike(searchText) + "%"
	case "ends-with":
		condition = "LOWER(" + column + ") LIKE ? ESCAPE '\\'"
		value = "%" + escapeLike(searchText)
	case "contains":
		fallthrough
	default:
		condition = "LOWER(" + column + ") LIKE ? ESCAPE '\\'"
		value = "%" + escapeLike(searchText) + "%"
	}

	if filter.NegateCondition {
		return db.Where("NOT ("+condition+")", value)
	}
	return db.Where(condition, value)
}

// escapeLike escapes LIKE wildcards so user input matches literally. Pair with
// `ESCAPE '\'` in the query, and apply BEFORE adding the surrounding %.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

// propertyToColumn maps vCard property names to database columns.
func (r *AddressBookRepository) propertyToColumn(property string) string {
	// Normalize property name to uppercase
	prop := strings.ToUpper(property)
	switch prop {
	case "FN":
		return "formatted_name"
	case "N":
		return "family_name" // For N, we primarily match on family_name
	case "EMAIL":
		return "email"
	case "TEL":
		return "phone"
	case "ORG":
		return "organization"
	case "GIVEN-NAME":
		return "given_name"
	case "FAMILY-NAME":
		return "family_name"
	case "UID":
		return "uid"
	default:
		return ""
	}
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

// hydrateObjects re-injects each object's stored PHOTO into its vCard body and
// recomputes ContentLength, using a single photo query for the whole batch.
// Every DAV read path must call this so served vCards include photos and the
// Content-Length header matches the body (otherwise clients hang or delete the
// photo on the next sync). Mutates the objects in place.
func (r *AddressBookRepository) hydrateObjects(ctx context.Context, objs []*addressbook.AddressObject) error {
	if len(objs) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	var photos []addressbook.ContactPhoto
	if err := r.db.WithContext(ctx).Where("address_object_id IN ?", ids).Find(&photos).Error; err != nil {
		return err
	}
	byObj := make(map[uint]addressbook.ContactPhoto, len(photos))
	for _, p := range photos {
		byObj[p.AddressObjectID] = p
	}
	for _, o := range objs {
		if p, ok := byObj[o.ID]; ok && p.PhotoData != "" {
			full, err := r.injectPhoto(o.VCardData, p.PhotoData, p.PhotoType)
			if err != nil {
				return fmt.Errorf("failed to inject photo: %w", err)
			}
			o.VCardData = full
		}
		// Always fix ContentLength to match the (possibly photo-injected) body.
		o.ContentLength = len(o.VCardData)
	}
	return nil
}

func (r *AddressBookRepository) CreateObject(ctx context.Context, object *addressbook.AddressObject) error {
	// Extract photo from vCardData
	strippedVCard, photoData, photoType, err := r.extractPhoto(object.VCardData)
	if err != nil {
		return fmt.Errorf("failed to process vcard: %w", err)
	}

	// Use stripped data for main object
	object.VCardData = strippedVCard
	object.ContentLength = len(strippedVCard)

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
		return r.recordAddressBookChange(tx, object.AddressBookID, object.Path, object.UID, "created")
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
	if err := r.hydrateObjects(ctx, []*addressbook.AddressObject{&obj}); err != nil {
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
	// Unique tiebreaker so offset paging over rows tied on the sort key is
	// deterministic (no duplicated or dropped contacts across page boundaries).
	// dbOrder is sanitized to ASC/DESC above, so this Sprintf stays injection-safe.
	query = query.Order(fmt.Sprintf("id %s", dbOrder))

	// Preload AddressBook so the DTO mapper can build the UUID-based photo URL (#52).
	if err := query.Preload("AddressBook").Limit(limit).Offset(offset).Find(&objs).Error; err != nil {
		return nil, 0, err
	}

	// An ETag-only PROPFIND poll (flagged via context) doesn't serialize vCard
	// bodies, so skip re-injecting every contact's PHOTO blob — otherwise a
	// large book pulls hundreds of MB of blob reads per poll just to serve ETags.
	if !addressbook.SkipPhotoHydration(ctx) {
		if err := r.hydrateObjectSlice(ctx, objs); err != nil {
			return nil, 0, err
		}
	}

	return objs, total, nil
}

// hydrateObjectSlice is the value-slice convenience wrapper around
// hydrateObjects (mutates the slice elements in place).
func (r *AddressBookRepository) hydrateObjectSlice(ctx context.Context, objs []addressbook.AddressObject) error {
	if len(objs) == 0 {
		return nil
	}
	ptrs := make([]*addressbook.AddressObject, len(objs))
	for i := range objs {
		ptrs[i] = &objs[i]
	}
	return r.hydrateObjects(ctx, ptrs)
}

func (r *AddressBookRepository) GetObjectByUUID(ctx context.Context, uuid string) (*addressbook.AddressObject, error) {
	var obj addressbook.AddressObject
	// NOTE: deliberately no Preload("AddressBook") here — this getter feeds the
	// contact move path, which saves the returned object back, and a populated
	// association would make GORM cascade-write it (duplicating rows). Callers
	// that expose the DTO photo URL populate the association themselves: List/
	// Search preload it, and MoveUseCase sets it from the book it already loaded.
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&obj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if err := r.hydrateObjects(ctx, []*addressbook.AddressObject{&obj}); err != nil {
		return nil, err
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
	object.ContentLength = len(strippedVCard)

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Save(object).Error; err != nil {
			return err
		}

		// Upsert (or clear) the PHOTO side-table row to match the vCard.
		if err := r.upsertPhoto(ctx, tx, object.ID, photoData, photoType); err != nil {
			return err
		}
		return r.recordAddressBookChange(tx, object.AddressBookID, object.Path, object.UID, "modified")
	})
}

// MoveObject reassigns an object to a new address book and records both the
// target "modified" change and the source "deleted" change in a single
// transaction. object.AddressBookID must already point at the target book.
//
// The caller (MoveUseCase) loads the object via GetObjectByUUID, which
// *hydrates* the stored PHOTO back into VCardData. If we saved that body
// verbatim the PHOTO would live inline in address_objects AND in the
// contact_photos side table, so every later read would inject a second copy
// (and each further move would add another). We therefore re-strip the photo
// the same way CreateObject/UpdateObject do before persisting. Doing the
// strip, the reassign, and both sync-log writes atomically also prevents the
// permanent sync ghost that occurs if the source "deleted" change is lost
// after the reassign has already committed.
func (r *AddressBookRepository) MoveObject(ctx context.Context, object *addressbook.AddressObject, sourceAddressBookID uint) error {
	strippedVCard, photoData, photoType, err := r.extractPhoto(object.VCardData)
	if err != nil {
		return fmt.Errorf("failed to process vcard: %w", err)
	}
	object.VCardData = strippedVCard
	object.ContentLength = len(strippedVCard)

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Save(object).Error; err != nil {
			return err
		}
		if err := r.upsertPhoto(ctx, tx, object.ID, photoData, photoType); err != nil {
			return err
		}
		// Target book sees the object arrive.
		if err := r.recordAddressBookChange(tx, object.AddressBookID, object.Path, object.UID, "modified"); err != nil {
			return err
		}
		// Source book sees the object leave.
		return r.recordAddressBookChange(tx, sourceAddressBookID, object.Path, object.UID, "deleted")
	})
}

// upsertPhoto persists the extracted PHOTO for an address object: it inserts or
// updates the contact_photos row when photoData is non-empty, and deletes any
// existing row when the vCard carries no photo. Callers pass the values
// returned by extractPhoto after saving the stripped object.
func (r *AddressBookRepository) upsertPhoto(ctx context.Context, tx *gorm.DB, objectID uint, photoData, photoType string) error {
	if photoData == "" {
		// vCard has no photo; ensure no stale photo record survives.
		return tx.WithContext(ctx).Where("address_object_id = ?", objectID).Delete(&addressbook.ContactPhoto{}).Error
	}
	var photo addressbook.ContactPhoto
	err := tx.WithContext(ctx).Where("address_object_id = ?", objectID).First(&photo).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		photo = addressbook.ContactPhoto{
			AddressObjectID: objectID,
			PhotoData:       photoData,
			PhotoType:       photoType,
		}
		return tx.WithContext(ctx).Create(&photo).Error
	}
	photo.PhotoData = photoData
	photo.PhotoType = photoType
	return tx.WithContext(ctx).Save(&photo).Error
}

func (r *AddressBookRepository) DeleteObjectByUUID(ctx context.Context, uuid string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Look up the object first so we still have its AddressBookID /
		// Path / UID after the soft-delete — we need them for the change
		// log entry below.
		var obj addressbook.AddressObject
		if err := tx.WithContext(ctx).Where("uuid = ?", uuid).First(&obj).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // nothing to delete, nothing to log
			}
			return err
		}
		if err := tx.WithContext(ctx).Delete(&obj).Error; err != nil {
			return err
		}
		// Drop the PHOTO side-table row too. The address object is soft-deleted
		// (AddressObject has a DeletedAt), so without this the contact_photos row
		// — keyed on the object's primary key — is orphaned forever, retaining
		// the deleted contact's (often large, base64) photo blob. Every other
		// write path (Create/Update/Move) keeps this side table in lockstep with
		// the object inside its transaction; delete must do the same.
		if err := r.upsertPhoto(ctx, tx, obj.ID, "", ""); err != nil {
			return err
		}
		return r.recordAddressBookChange(tx, obj.AddressBookID, obj.Path, obj.UID, "deleted")
	})
}

// recordAddressBookChange advances the address book's sync token and writes
// a matching entry to the sync change log, so subsequent sync-collection
// REPORTs can compute the delta. Without this, the DAV sync-token returned
// by the initial sync won't correspond to any SyncChangeLog row, and every
// incremental sync is rejected with "valid-sync-token" 403 — forcing real
// clients to re-download the entire collection on every refresh.
func (r *AddressBookRepository) recordAddressBookChange(tx *gorm.DB, addressBookID uint, path, uid, changeType string) error {
	newToken := addressbook.GenerateSyncToken()
	if err := tx.Model(&addressbook.AddressBook{}).
		Where("id = ?", addressBookID).
		Updates(map[string]interface{}{
			"sync_token": newToken,
			"c_tag":      newToken,
		}).Error; err != nil {
		return err
	}
	return tx.Create(&addressbook.SyncChangeLog{
		AddressBookID: addressBookID,
		ResourcePath:  path,
		ResourceUID:   uid,
		ChangeType:    changeType,
		SyncToken:     newToken,
	}).Error
}

func (r *AddressBookRepository) SearchObjects(ctx context.Context, userID uint, query string, addressBookID *uint, limit int) ([]addressbook.AddressObject, error) {
	var objs []addressbook.AddressObject
	q := "%" + escapeLike(query) + "%"

	// Join with AddressBooks to filter by UserID
	// And filter by query on denormalized fields
	db := r.db.WithContext(ctx).
		// GORM auto-applies the soft-delete scope to the primary model
		// (address_objects) but NOT to a raw-joined table, so without this
		// `deleted_at IS NULL` the contacts of a soft-deleted address book would
		// still surface here (M7). Soft delete does not cascade to the objects.
		Joins("JOIN address_books ON address_books.id = address_objects.address_book_id AND address_books.deleted_at IS NULL").
		Where("address_books.user_id = ?", userID)

	if addressBookID != nil {
		db = db.Where("address_objects.address_book_id = ?", *addressBookID)
	}

	err := db.Where("address_objects.formatted_name LIKE ? ESCAPE '\\' OR address_objects.email LIKE ? ESCAPE '\\' OR address_objects.phone LIKE ? ESCAPE '\\' OR address_objects.organization LIKE ? ESCAPE '\\' OR address_objects.given_name LIKE ? ESCAPE '\\' OR address_objects.family_name LIKE ? ESCAPE '\\'", q, q, q, q, q, q).
		// Preload AddressBook so the DTO mapper can build the UUID-based photo URL (#52).
		Preload("AddressBook").
		Limit(limit).
		Find(&objs).Error

	if err != nil {
		return nil, err
	}
	if err := r.hydrateObjectSlice(ctx, objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// GetChangesSinceToken returns all changes since the given sync token.
// If token is empty, returns all current objects as "created".
func (r *AddressBookRepository) GetChangesSinceToken(ctx context.Context, addressBookID uint, token string) ([]*addressbook.SyncChangeLog, error) {
	var changes []*addressbook.SyncChangeLog

	if token == "" {
		// Initial sync: return all objects as "created"
		var objs []addressbook.AddressObject
		if err := r.db.WithContext(ctx).Where("address_book_id = ?", addressBookID).Find(&objs).Error; err != nil {
			return nil, err
		}

		// Get current sync token from address book
		var ab addressbook.AddressBook
		if err := r.db.WithContext(ctx).First(&ab, addressBookID).Error; err != nil {
			return nil, err
		}

		for _, obj := range objs {
			changes = append(changes, &addressbook.SyncChangeLog{
				AddressBookID: addressBookID,
				ResourcePath:  obj.Path,
				ResourceUID:   obj.UID,
				ChangeType:    "created",
				SyncToken:     ab.SyncToken,
			})
		}
		return changes, nil
	}

	// Incremental sync: validate token exists
	var lastChange addressbook.SyncChangeLog
	if err := r.db.WithContext(ctx).
		Where("address_book_id = ? AND sync_token = ?", addressBookID, token).
		First(&lastChange).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound // Invalid token
		}
		return nil, err
	}

	// Get all changes after the token's row, ordered by ID (not created_at):
	// two rows written in the same timestamp tick would otherwise be skipped.
	// "collection" anchor rows are excluded — they exist only to validate
	// freshly minted tokens.
	if err := r.db.WithContext(ctx).
		Where("address_book_id = ? AND id > ? AND change_type <> ?", addressBookID, lastChange.ID, "collection").
		Order("id ASC").
		Find(&changes).Error; err != nil {
		return nil, err
	}

	return changes, nil
}

// CountContactsByUserID counts all contacts across all address books for a user.
func (r *AddressBookRepository) CountContactsByUserID(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&addressbook.AddressObject{}).
		// GORM auto-applies the soft-delete scope to the primary model
		// (address_objects) but NOT to a raw-joined table, so without this
		// `deleted_at IS NULL` the contacts of a soft-deleted address book would
		// still surface here (M7). Soft delete does not cascade to the objects.
		Joins("JOIN address_books ON address_books.id = address_objects.address_book_id AND address_books.deleted_at IS NULL").
		Where("address_books.user_id = ?", userID).
		Count(&count).Error
	return count, err
}

// RecordChange advances the address book sync token and writes a matching
// change-log row atomically (for mutations outside the object CRUD methods):
// collection rename (change type "collection") and the source side of a
// cross-book contact move (change type "deleted").
func (r *AddressBookRepository) RecordChange(ctx context.Context, addressBookID uint, path, uid, changeType string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.recordAddressBookChange(tx, addressBookID, path, uid, changeType)
	})
}
