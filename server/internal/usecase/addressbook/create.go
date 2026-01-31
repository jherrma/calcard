package addressbook

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type CreateUseCase struct {
	repo addressbook.Repository
}

func NewCreateUseCase(repo addressbook.Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo}
}

type CreateInput struct {
	UserID      uint
	Name        string
	Description string
}

func (uc *CreateUseCase) Execute(ctx context.Context, input CreateInput) (*addressbook.AddressBook, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	ab := &addressbook.AddressBook{
		UserID:      input.UserID,
		Name:        input.Name,
		Description: input.Description,
		UUID:        uuid.New().String(),
		Path:        uuid.New().String(), // Usually UUID is used as path
		SyncToken:   addressbook.GenerateSyncToken(),
		CTag:        addressbook.GenerateCTag(),
	}

	if err := uc.repo.Create(ctx, ab); err != nil {
		return nil, err
	}

	return ab, nil
}
