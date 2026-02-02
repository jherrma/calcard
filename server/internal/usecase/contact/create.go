package contact

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/contact"
	"github.com/jherrma/caldav-server/internal/usecase/addressbook"
)

type CreateUseCase struct {
	addrBookUC *addressbook.CreateContactUseCase
}

func NewCreateUseCase(addrBookUC *addressbook.CreateContactUseCase) *CreateUseCase {
	return &CreateUseCase{addrBookUC: addrBookUC}
}

func (uc *CreateUseCase) Execute(ctx context.Context, userID uint, addressBookID uint, input *contact.Contact) (*contact.Contact, error) {
	// 1. Convert to vCard
	// Ensure we generate a UID if not present so ToVCard includes it.
	if input.ID == "" {
		input.ID = uuid.New().String()
	}

	vcardData, err := ToVCard(input)
	if err != nil {
		return nil, fmt.Errorf("failed to encode vcard: %w", err)
	}

	ucInput := addressbook.CreateContactInput{
		UserID:        userID,
		AddressBookID: addressBookID,
		VCardData:     vcardData,
	}

	obj, err := uc.addrBookUC.Execute(ctx, ucInput)
	if err != nil {
		return nil, err
	}

	// Map backend object back to Contact domain for response
	res, err := ToContact(obj.VCardData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stored vcard: %w", err)
	}

	// Populate metadata
	res.ID = obj.UUID // Use the internal DB UUID
	res.UID = obj.UID
	res.AddressBookID = fmt.Sprintf("%d", obj.AddressBookID)

	res.Etag = obj.ETag
	res.CreatedAt = obj.CreatedAt
	res.UpdatedAt = obj.UpdatedAt

	return res, nil
}
