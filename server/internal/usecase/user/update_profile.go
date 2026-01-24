package user

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

var (
	ErrEmailAlreadyExists = errors.New("email address already registered")
	ErrDisplayNameTooLong = errors.New("display name must be at most 255 characters")
)

type UpdateProfileUseCase struct {
	repo user.UserRepository
}

func NewUpdateProfileUseCase(repo user.UserRepository) *UpdateProfileUseCase {
	return &UpdateProfileUseCase{repo: repo}
}

type UpdateProfileRequest struct {
	DisplayName *string
}

func (uc *UpdateProfileUseCase) Execute(ctx context.Context, userUUID string, req UpdateProfileRequest) (*user.User, error) {
	u, err := uc.repo.GetByUUID(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, errors.New("user not found")
	}

	if req.DisplayName != nil {
		if len(*req.DisplayName) > 255 {
			return nil, ErrDisplayNameTooLong
		}
		u.DisplayName = *req.DisplayName
	}

	if err := uc.repo.Update(ctx, u); err != nil {
		return nil, err
	}

	return u, nil
}
