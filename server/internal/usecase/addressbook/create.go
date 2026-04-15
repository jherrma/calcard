package addressbook

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

// ErrNameRequired is returned by Create when the supplied name is empty.
// Exported so handlers can distinguish user-input validation failures (400)
// from genuine repository errors (500).
var ErrNameRequired = errors.New("name is required")

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
		return nil, ErrNameRequired
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
