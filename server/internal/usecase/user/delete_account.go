package user

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrIncorrectPassword    = errors.New("password is incorrect")
	ErrConfirmationRequired = errors.New("please type DELETE to confirm account deletion")
)

type DeleteAccountUseCase struct {
	repo user.UserRepository
}

func NewDeleteAccountUseCase(repo user.UserRepository) *DeleteAccountUseCase {
	return &DeleteAccountUseCase{repo: repo}
}

func (uc *DeleteAccountUseCase) Execute(ctx context.Context, userUUID string, password, confirmation string) error {
	if confirmation != "DELETE" {
		return ErrConfirmationRequired
	}

	u, err := uc.repo.GetByUUID(ctx, userUUID)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("user not found")
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return ErrIncorrectPassword
	}

	return uc.repo.Delete(ctx, u.ID)
}
