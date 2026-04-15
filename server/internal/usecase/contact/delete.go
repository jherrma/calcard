package contact

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type DeleteUseCase struct {
	repo addressbook.Repository
}

func NewDeleteUseCase(repo addressbook.Repository) *DeleteUseCase {
	return &DeleteUseCase{repo: repo}
}

func (uc *DeleteUseCase) Execute(ctx context.Context, addressBookID uint, contactUUID string) error {
	// 1. Validate existence and ownership
	obj, err := uc.repo.GetObjectByUUID(ctx, contactUUID)
	if err != nil {
		return err
	}
	if obj == nil || obj.AddressBookID != addressBookID {
		return fmt.Errorf("contact not found") // Simple error for handler to map to 404
	}

	// 2. Delete object. DeleteObjectByUUID atomically bumps the address
	// book's sync_token / CTag and records a "deleted" SyncChangeLog entry.
	return uc.repo.DeleteObjectByUUID(ctx, contactUUID)
}
