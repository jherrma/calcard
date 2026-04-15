package addressbook

import (
	"bytes"
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
	// 1. Verify the address book exists and belongs to the caller.
	ab, err := uc.repo.GetByID(ctx, input.AddressBookID)
	if err != nil {
		return nil, err
	}
	if ab.UserID != input.UserID {
		return nil, fmt.Errorf("address book not found or access denied")
	}

	// 2. Parse the vCard once. We use the parsed card to extract the UID
	//    and to derive the denormalized search fields in one pass.
	card, err := vcard.NewDecoder(strings.NewReader(input.VCardData)).Decode()
	if err != nil {
		return nil, fmt.Errorf("invalid vcard data: %w", err)
	}

	// Ensure the vCard carries a UID. If the caller didn't supply one, mint
	// it and re-encode so the persisted blob matches what CardDAV clients
	// will see on a subsequent GET.
	uid := card.PreferredValue(vcard.FieldUID)
	if uid == "" {
		uid = uuid.New().String()
		card.Set(vcard.FieldUID, &vcard.Field{Value: uid})

		var buf bytes.Buffer
		if err := vcard.NewEncoder(&buf).Encode(card); err != nil {
			return nil, fmt.Errorf("failed to encode vcard with generated UID: %w", err)
		}
		input.VCardData = buf.String()
	}

	obj := &addressbook.AddressObject{
		UUID:          uuid.New().String(), // internal DB UUID, distinct from vCard UID
		AddressBookID: input.AddressBookID,
		Path:          uid + ".vcf",
		UID:           uid,
		VCardData:     input.VCardData,
		VCardVersion:  "3.0",
		ETag:          fmt.Sprintf("%d", time.Now().UnixNano()),
		ContentLength: len(input.VCardData),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	// 3. Mirror FN / N / EMAIL / TEL / ORG into the denormalized columns
	//    using the already-parsed card — one parse for the whole create.
	addressbook.ExtractDenormFieldsFromCard(card, obj)

	if err := uc.repo.CreateObject(ctx, obj); err != nil {
		return nil, err
	}
	// CreateObject atomically bumps the address book's sync_token / CTag
	// and writes the matching SyncChangeLog entry — see
	// AddressBookRepository.recordAddressBookChange. No second update here.

	_ = ab // kept in scope for potential future cache invalidation
	return obj, nil
}
