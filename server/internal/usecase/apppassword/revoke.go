package apppassword

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
)

type RevokeUseCase struct {
	repo   user.AppPasswordRepository
	logger *logging.SecurityLogger
}

func NewRevokeUseCase(repo user.AppPasswordRepository, logger *logging.SecurityLogger) *RevokeUseCase {
	return &RevokeUseCase{repo: repo, logger: logger}
}

func (uc *RevokeUseCase) Execute(ctx context.Context, userUUID, appPwdUUID, ip, userAgent string) error {
	ap, err := uc.repo.GetByUUID(ctx, appPwdUUID)
	if err != nil {
		return err
	}
	if ap == nil {
		return fmt.Errorf("app password not found")
	}

	now := time.Now()
	ap.RevokedAt = &now

	if err := uc.repo.Update(ctx, ap); err != nil {
		return err
	}

	uc.logger.LogAppPasswordRevoked(ctx, ap.UserID, ap.Name, ip, userAgent)
	return nil
}
