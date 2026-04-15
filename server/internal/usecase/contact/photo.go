package contact

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type PhotoUseCase struct {
	repo addressbook.Repository
}

func NewPhotoUseCase(repo addressbook.Repository) *PhotoUseCase {
	return &PhotoUseCase{repo: repo}
}

func (uc *PhotoUseCase) Upload(ctx context.Context, addressBookID uint, contactUUID string, data []byte) error {
	// 1. Get object
	obj, err := uc.repo.GetObjectByUUID(ctx, contactUUID)
	if err != nil {
		return err
	}
	if obj == nil || obj.AddressBookID != addressBookID {
		return fmt.Errorf("contact not found")
	}

	// 2. Parse
	currentContact, err := ToContact(obj.VCardData)
	if err != nil {
		return fmt.Errorf("failed to parse existing vcard: %w", err)
	}

	// 3. Update Photo (Encode to Base64)
	encoded := base64.StdEncoding.EncodeToString(data)

	// Detect Type
	mimeType := http.DetectContentType(data)
	// Map mime to vCard TYPE
	// image/jpeg -> JPEG
	// image/png -> PNG
	// image/gif -> GIF
	parts := strings.Split(mimeType, "/")
	photoType := "JPEG" // Default
	if len(parts) == 2 && parts[0] == "image" {
		photoType = strings.ToUpper(parts[1])
	}

	currentContact.Photo = encoded
	currentContact.PhotoType = photoType

	// 4. To VCard
	newVCardData, err := ToVCard(currentContact)
	if err != nil {
		return fmt.Errorf("failed to encode updated vcard: %w", err)
	}

	// 5. Update Object
	obj.VCardData = newVCardData
	obj.UpdatedAt = time.Now()
	obj.ETag = fmt.Sprintf("%d", time.Now().UnixNano())

	// UpdateObject bumps the address book's sync_token / CTag and logs the
	// change atomically; no second CTag update needed.
	_ = addressBookID // kept in signature for future caching hooks
	return uc.repo.UpdateObject(ctx, obj)
}

func (uc *PhotoUseCase) Delete(ctx context.Context, addressBookID uint, contactUUID string) error {
	obj, err := uc.repo.GetObjectByUUID(ctx, contactUUID)
	if err != nil {
		return err
	}
	if obj == nil || obj.AddressBookID != addressBookID {
		return fmt.Errorf("contact not found")
	}

	currentContact, err := ToContact(obj.VCardData)
	if err != nil {
		return fmt.Errorf("failed to parse existing vcard: %w", err)
	}

	currentContact.Photo = ""

	newVCardData, err := ToVCard(currentContact)
	if err != nil {
		return fmt.Errorf("failed to encode updated vcard: %w", err)
	}

	obj.VCardData = newVCardData
	obj.UpdatedAt = time.Now()
	obj.ETag = fmt.Sprintf("%d", time.Now().UnixNano())

	// UpdateObject bumps the address book's sync_token / CTag and logs the
	// change atomically; no second CTag update needed.
	return uc.repo.UpdateObject(ctx, obj)
}
