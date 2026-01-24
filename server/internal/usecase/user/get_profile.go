package user

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

type GetProfileUseCase struct {
	repo user.UserRepository
}

func NewGetProfileUseCase(repo user.UserRepository) *GetProfileUseCase {
	return &GetProfileUseCase{repo: repo}
}

func (uc *GetProfileUseCase) Execute(ctx context.Context, userUUID string) (*user.User, error) {
	return uc.repo.GetByUUID(ctx, userUUID)
}
