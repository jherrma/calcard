package apppassword

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

type RevokeUseCase struct {
	repo user.AppPasswordRepository
}

func NewRevokeUseCase(repo user.AppPasswordRepository) *RevokeUseCase {
	return &RevokeUseCase{repo: repo}
}

func (uc *RevokeUseCase) Execute(ctx context.Context, userUUID, appPwdUUID string) error {
	ap, err := uc.repo.GetByUUID(ctx, appPwdUUID)
	if err != nil {
		return err
	}
	if ap == nil {
		return fmt.Errorf("app password not found")
	}

	now := time.Now()
	ap.RevokedAt = &now

	return uc.repo.Update(ctx, ap)
}
