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

	// Update CTag and SyncToken
	ab.UpdateSyncTokens()

	if err := uc.repo.Update(ctx, ab); err != nil {
		return nil, err
	}

	return ab, nil
}
