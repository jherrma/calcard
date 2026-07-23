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

	// 2. Verify ownership of the source. Loaded before the already-there check
	// so the response can carry the book's UUID: FromAddressObject builds the
	// photo URL from obj.AddressBook.UUID (#52), and GetObjectByUUID doesn't
	// preload the association (it feeds the move save path — see its comment).
	sourceAB, err := uc.repo.GetByID(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if sourceAB == nil || sourceAB.UserID != userID {
		return nil, fmt.Errorf("source address book not found or access denied")
	}

	if sourceID == targetAddressBookID {
		// Already there — nothing to move. The hydrated vCard still carries the
		// photo, so set the association the mapper needs for a valid photo URL.
		obj.AddressBook = *sourceAB
		return FromAddressObject(obj), nil
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

	// MoveObject stripped the photo out of obj.VCardData, so the mapper won't
	// build a photo URL here regardless; set the target association anyway so
	// the response is correct if that ever changes.
	obj.AddressBook = *targetAB
	return FromAddressObject(obj), nil
}
