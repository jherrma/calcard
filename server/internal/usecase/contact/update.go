package contact

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
)

type UpdateUseCase struct {
	repo addressbook.Repository
}

func NewUpdateUseCase(repo addressbook.Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// UpdateInput defines fields that can be updated. Pointers indicate presence/value.
type UpdateInput struct {
	Prefix        *string            `json:"prefix"`
	GivenName     *string            `json:"given_name"`
	MiddleName    *string            `json:"middle_name"`
	FamilyName    *string            `json:"family_name"`
	Suffix        *string            `json:"suffix"`
	Nickname      *string            `json:"nickname"`
	FormattedName *string            `json:"formatted_name"`
	Organization  *string            `json:"organization"`
	Title         *string            `json:"title"`
	Emails        *[]contact.Email   `json:"emails"`
	Phones        *[]contact.Phone   `json:"phones"`
	Addresses     *[]contact.Address `json:"addresses"`
	URLs          *[]contact.URL     `json:"urls"`
	Birthday      *string            `json:"birthday"`
	Notes         *string            `json:"notes"`
}

func (uc *UpdateUseCase) Execute(ctx context.Context, addressBookID uint, contactUUID string, input UpdateInput) (*contact.Contact, error) {
	// 1. Get existing object
	obj, err := uc.repo.GetObjectByUUID(ctx, contactUUID)
	if err != nil {
		return nil, err
	}
	if obj == nil || obj.AddressBookID != addressBookID {
		return nil, nil // Not found
	}

	// 2. Parse current vCard
	currentContact, err := ToContact(obj.VCardData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse existing vcard: %w", err)
	}

	// 3. Apply updates
	if input.FormattedName != nil {
		currentContact.FormattedName = *input.FormattedName
	}
	if input.FamilyName != nil {
		currentContact.FamilyName = *input.FamilyName
	}
	if input.GivenName != nil {
		currentContact.GivenName = *input.GivenName
	}
	if input.MiddleName != nil {
		currentContact.MiddleName = *input.MiddleName
	}
	if input.Prefix != nil {
		currentContact.Prefix = *input.Prefix
	}
	if input.Suffix != nil {
		currentContact.Suffix = *input.Suffix
	}
	if input.Nickname != nil {
		currentContact.Nickname = *input.Nickname
	}
	if input.Organization != nil {
		currentContact.Organization = *input.Organization
	}
	if input.Title != nil {
		currentContact.Title = *input.Title
	}
	if input.Birthday != nil {
		currentContact.Birthday = *input.Birthday
	}
	if input.Notes != nil {
		currentContact.Notes = *input.Notes
	}
	if input.Emails != nil {
		currentContact.Emails = *input.Emails
	}
	if input.Phones != nil {
		currentContact.Phones = *input.Phones
	}
	if input.Addresses != nil {
		currentContact.Addresses = *input.Addresses
	}
	if input.URLs != nil {
		currentContact.URLs = *input.URLs
	}

	// 4. Serialize back to vCard
	newVCardData, err := ToVCard(currentContact)
	if err != nil {
		return nil, fmt.Errorf("failed to encode updated vcard: %w", err)
	}

	// 5. Update AddressObject
	obj.VCardData = newVCardData
	obj.UpdatedAt = time.Now()
	obj.ETag = fmt.Sprintf("%d", time.Now().UnixNano()) // New ETag

	// Update denormalized fields
	obj.FormattedName = currentContact.FormattedName
	obj.GivenName = currentContact.GivenName
	obj.FamilyName = currentContact.FamilyName
	obj.Organization = currentContact.Organization
	if len(currentContact.Emails) > 0 {
		obj.Email = currentContact.Emails[0].Value
	} else {
		obj.Email = ""
	}
	if len(currentContact.Phones) > 0 {
		obj.Phone = currentContact.Phones[0].Value
	} else {
		obj.Phone = ""
	}

	if err := uc.repo.UpdateObject(ctx, obj); err != nil {
		return nil, err
	}

	// 6. Update AddressBook CTag
	ab, err := uc.repo.GetByID(ctx, addressBookID)
	if err != nil {
		return nil, err
	}
	if ab != nil {
		ab.UpdateSyncTokens()
		if err := uc.repo.Update(ctx, ab); err != nil {
			// Log error but treat as non-fatal for the contact update?
			// Ideally transactional.
			fmt.Printf("failed to update address book ctag: %v\n", err)
		}
	}

	// 7. Return updated contact
	// Map back to response format
	res, _ := ToContact(newVCardData) // Reuse parsed logic
	res.ID = obj.UUID
	res.AddressBookID = fmt.Sprintf("%d", obj.AddressBookID)
	res.Etag = obj.ETag
	res.CreatedAt = obj.CreatedAt
	res.UpdatedAt = obj.UpdatedAt

	return res, nil
}
