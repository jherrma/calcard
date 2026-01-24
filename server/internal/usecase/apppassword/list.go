package apppassword

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

type ListUseCase struct {
	repo user.AppPasswordRepository
}

func NewListUseCase(repo user.AppPasswordRepository) *ListUseCase {
	return &ListUseCase{repo: repo}
}

func (uc *ListUseCase) Execute(ctx context.Context, userID uint) ([]user.AppPassword, error) {
	return uc.repo.ListByUserID(ctx, userID)
}
