package addressbook

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type UpdateUseCase struct {
	repo addressbook.Repository
}

func NewUpdateUseCase(repo addressbook.Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

type UpdateInput struct {
	ID          uint
	UserID      uint
	Name        *string
	Description *string
}

func (uc *UpdateUseCase) Execute(ctx context.Context, input UpdateInput) (*addressbook.AddressBook, error) {
	ab, err := uc.repo.GetByID(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	if ab == nil || ab.UserID != input.UserID {
		return nil, fmt.Errorf("address book not found")
	}

	if input.Name != nil {
		ab.Name = *input.Name
	}
	if input.Description != nil {
		ab.Description = *input.Description
	}

	// Persist the rename and advance the sync token (backed by a change-log
	// anchor row) atomically in one transaction — see UpdateMetadata. Doing the
	// Save and RecordChange separately let a concurrent object PUT's fresh token
	// be overwritten by the stale one loaded above, and a RecordChange failure
	// after the Save committed left the CTag un-bumped.
	if err := uc.repo.UpdateMetadata(ctx, ab); err != nil {
		return nil, err
	}

	return ab, nil
}
