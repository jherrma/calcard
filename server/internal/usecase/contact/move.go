package contact

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
)

type MoveUseCase struct {
	repo addressbook.Repository
}

func NewMoveUseCase(repo addressbook.Repository) *MoveUseCase {
	return &MoveUseCase{repo: repo}
}

func (uc *MoveUseCase) Execute(ctx context.Context, userID uint, contactUUID string, targetAddressBookID uint) (*contact.Contact, error) {
	// 1. Get object
	obj, err := uc.repo.GetObjectByUUID(ctx, contactUUID)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("contact not found")
	}

	sourceID := obj.AddressBookID
	if sourceID == targetAddressBookID {
		// Already there
		return FromAddressObject(obj), nil
	}

	// 2. Verify ownership of source and target
	sourceAB, err := uc.repo.GetByID(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if sourceAB == nil || sourceAB.UserID != userID {
		return nil, fmt.Errorf("source address book not found or access denied")
	}

	targetAB, err := uc.repo.GetByID(ctx, targetAddressBookID)
	if err != nil {
		return nil, err
	}
	if targetAB == nil || targetAB.UserID != userID {
		return nil, fmt.Errorf("target address book not found or access denied")
	}

	// 3. Move object
	obj.AddressBookID = targetAddressBookID
	obj.UpdatedAt = time.Now()
	obj.ETag = fmt.Sprintf("%d", time.Now().UnixNano())

	if err := uc.repo.UpdateObject(ctx, obj); err != nil {
		return nil, fmt.Errorf("failed to move contact: %w", err)
	}

	// 4. Update Sync Tokens for both
	sourceAB.UpdateSyncTokens()
	if err := uc.repo.Update(ctx, sourceAB); err != nil {
		fmt.Printf("failed to update source address book ctag: %v\n", err)
	}

	targetAB.UpdateSyncTokens()
	if err := uc.repo.Update(ctx, targetAB); err != nil {
		fmt.Printf("failed to update target address book ctag: %v\n", err)
	}

	return FromAddressObject(obj), nil
}
