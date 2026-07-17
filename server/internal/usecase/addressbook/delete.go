package addressbook

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

type DeleteUseCase struct {
	repo      addressbook.Repository
	shareRepo sharing.AddressBookShareRepository
}

// NewDeleteUseCase wires the delete usecase. shareRepo may be nil in unit
// tests that don't exercise sharing.
func NewDeleteUseCase(repo addressbook.Repository, shareRepo sharing.AddressBookShareRepository) *DeleteUseCase {
	return &DeleteUseCase{repo: repo, shareRepo: shareRepo}
}

func (uc *DeleteUseCase) Execute(ctx context.Context, id uint, userID uint) error {
	ab, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if ab == nil || ab.UserID != userID {
		return fmt.Errorf("address book not found")
	}

	// Check if it's the last address book
	list, err := uc.repo.ListByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if len(list) <= 1 {
		return fmt.Errorf("cannot delete your last address book")
	}

	if err := uc.repo.Delete(ctx, id); err != nil {
		return err
	}

	// Revoke every share of this address book so it doesn't linger as a ghost
	// entry in the sharees' lists (and so a future book can't inherit a stale
	// share).
	if uc.shareRepo != nil {
		if err := uc.shareRepo.DeleteByAddressBookID(ctx, id); err != nil {
			return fmt.Errorf("failed to revoke address book shares: %w", err)
		}
	}

	return nil
}
