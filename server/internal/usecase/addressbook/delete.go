package addressbook

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

	return uc.repo.Delete(ctx, id)
}
