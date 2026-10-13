package addressbook

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

// DeleteUseCase permanently deletes an address book. REST and MCP's
// delete_address_book both run it.
type DeleteUseCase struct {
	repo addressbook.Repository
}

// NewDeleteUseCase wires the delete usecase.
//
// The share repository is no longer used: Repository.Delete removes the
// book's shares in the same transaction as everything else in it. The
// parameter stays only so existing wiring keeps compiling; drop it together
// with its callers.
func NewDeleteUseCase(repo addressbook.Repository, _ sharing.AddressBookShareRepository) *DeleteUseCase {
	return &DeleteUseCase{repo: repo}
}

func (uc *DeleteUseCase) Execute(ctx context.Context, id uint, userID uint) error {
	ab, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if ab == nil || ab.UserID != userID {
		return ErrNotFound
	}

	// Check if it's the last address book
	list, err := uc.repo.ListByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if len(list) <= 1 {
		return ErrLastAddressBook
	}

	// One transaction removes the book and everything in it: contacts, their
	// photos, change log and shares (so the book can't linger as a ghost entry
	// in the sharees' lists).
	return uc.repo.Delete(ctx, id)
}
