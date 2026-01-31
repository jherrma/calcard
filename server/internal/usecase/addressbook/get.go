package addressbook

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type GetUseCase struct {
	repo addressbook.Repository
}

func NewGetUseCase(repo addressbook.Repository) *GetUseCase {
	return &GetUseCase{repo: repo}
}

func (uc *GetUseCase) Execute(ctx context.Context, id uint, userID uint) (*addressbook.AddressBook, error) {
	ab, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if ab == nil {
		return nil, nil // Not found
	}

	// Check ownership (and later sharing)
	if ab.UserID != userID {
		// TODO: Check if shared
		return nil, nil // Treat as not found for security
	}

	return ab, nil
}
