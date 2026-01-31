package addressbook

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type CreateContactInput struct {
	AddressBookID uint
	UserID        uint
	VCardData     string
}

type CreateContactUseCase struct {
	repo addressbook.Repository
}

func NewCreateContactUseCase(repo addressbook.Repository) *CreateContactUseCase {
	return &CreateContactUseCase{repo: repo}
}

func (uc *CreateContactUseCase) Execute(ctx context.Context, input CreateContactInput) (*addressbook.AddressObject, error) {
	// 1. Check if address book exists and belongs to user
	ab, err := uc.repo.GetByID(ctx, input.AddressBookID)
	if err != nil {
		return nil, err
	}
	if ab.UserID != input.UserID {
		return nil, fmt.Errorf("address book not found or access denied")
	}

	// 2. Generate identifiers
	uid := uuid.New().String()
	objUUID := uuid.New().String()
	path := uid + ".vcf"

	// 3. Parse VCard data to extract denormalized fields
	dec := vcard.NewDecoder(strings.NewReader(input.VCardData))
	card, err := dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("invalid vcard data: %w", err)
	}

	var formattedName, givenName, familyName, email, phone, organization string

	if fn := card.PreferredValue(vcard.FieldFormattedName); fn != "" {
		formattedName = fn
	}

	if n := card.Get(vcard.FieldName); n != nil {
		givenName = n.Params.Get("GIVEN")
		familyName = n.Params.Get("FAMILY")
		// The vcard library returns values joined by semicolon for N field structure if fetched as string
		// But Address struct helpers for name components might need specific handling or just use common name
		// Let's rely on FormattedName mainly, but if we want components we can parse the Value string
		// N:Family;Given;Middle;Prefix;Suffix
		parts := strings.Split(n.Value, ";")
		if len(parts) > 0 {
			familyName = parts[0]
		}
		if len(parts) > 1 {
			givenName = parts[1]
		}
	}

	if e := card.PreferredValue(vcard.FieldEmail); e != "" {
		email = e
	}

	if p := card.PreferredValue(vcard.FieldTelephone); p != "" {
		phone = p
	}

	if org := card.PreferredValue(vcard.FieldOrganization); org != "" {
		organization = org
	}

	// 4. Create address object entity
	obj := &addressbook.AddressObject{
		UUID:          objUUID,
		AddressBookID: input.AddressBookID,
		Path:          path,
		UID:           uid,
		VCardData:     input.VCardData,
		VCardVersion:  "3.0", // Defaulting to 3.0 for broad compatibility, or extract from card
		ETag:          fmt.Sprintf("%d", time.Now().UnixNano()),
		ContentLength: len(input.VCardData),
		FormattedName: formattedName,
		GivenName:     givenName,
		FamilyName:    familyName,
		Email:         email,
		Phone:         phone,
		Organization:  organization,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := uc.repo.CreateObject(ctx, obj); err != nil {
		return nil, err
	}

	// 5. Update AddressBook CTag
	ab.UpdateSyncTokens()
	if err := uc.repo.Update(ctx, ab); err != nil {
		// Log error but don't fail, synchronization might be slightly off
		fmt.Printf("failed to update address book ctag: %v\n", err)
	}

	return obj, nil
}
