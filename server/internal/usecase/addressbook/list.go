package addressbook

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type ListUseCase struct {
	repo addressbook.Repository
}

func NewListUseCase(repo addressbook.Repository) *ListUseCase {
	return &ListUseCase{repo: repo}
}

func (uc *ListUseCase) Execute(ctx context.Context, userID uint) ([]addressbook.AddressBook, error) {
	// TODO: Include shared address books
	return uc.repo.ListByUserID(ctx, userID)
}
