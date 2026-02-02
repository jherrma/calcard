package contact

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
)

type GetUseCase struct {
	repo addressbook.Repository
}

func NewGetUseCase(repo addressbook.Repository) *GetUseCase {
	return &GetUseCase{repo: repo}
}

func (uc *GetUseCase) Execute(ctx context.Context, addressBookID uint, contactUUID string) (*contact.Contact, error) {
	obj, err := uc.repo.GetObjectByUUID(ctx, contactUUID)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, nil // Not found
	}

	// Verify object belongs to the requested address book
	if obj.AddressBookID != addressBookID {
		// Just treat as not found to avoid leaking existence
		return nil, nil
	}

	res, err := ToContact(obj.VCardData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse vcard for contact %s: %w", contactUUID, err)
	}

	res.ID = obj.UUID
	res.AddressBookID = strconv.FormatUint(uint64(obj.AddressBookID), 10)
	res.Etag = obj.ETag
	res.CreatedAt = obj.CreatedAt
	res.UpdatedAt = obj.UpdatedAt

	return res, nil
}
