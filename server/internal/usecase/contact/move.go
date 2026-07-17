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

	// 3. Move object. MoveObject atomically records a "modified" change on the
	// TARGET book and a "deleted" change on the SOURCE book in one transaction,
	// so a partial failure can't leave a permanent sync ghost on the source.
	obj.AddressBookID = targetAddressBookID
	obj.UpdatedAt = time.Now()
	obj.ETag = addressbook.NewETag()

	if err := uc.repo.MoveObject(ctx, obj, sourceID); err != nil {
		return nil, fmt.Errorf("failed to move contact: %w", err)
	}

	return FromAddressObject(obj), nil
}
