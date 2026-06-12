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

	// 3. Move object. Capture the path/UID before reassigning so we can record
	// a "deleted" entry on the source book.
	srcPath, srcUID := obj.Path, obj.UID
	obj.AddressBookID = targetAddressBookID
	obj.UpdatedAt = time.Now()
	obj.ETag = addressbook.NewETag()

	// UpdateObject records a "modified" change on the TARGET book (the object's
	// AddressBookID now points there) and advances the target token.
	if err := uc.repo.UpdateObject(ctx, obj); err != nil {
		return nil, fmt.Errorf("failed to move contact: %w", err)
	}

	// The source book must see the contact leave: record a "deleted" change
	// (which also advances the source token via RecordChange), otherwise a
	// syncing client keeps the stale copy forever.
	if err := uc.repo.RecordChange(ctx, sourceID, srcPath, srcUID, "deleted"); err != nil {
		return nil, fmt.Errorf("failed to record source change: %w", err)
	}

	return FromAddressObject(obj), nil
}
