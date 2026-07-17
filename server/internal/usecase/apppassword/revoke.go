package apppassword

import (
	"context"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
)

// ErrNotFound is returned when an app password doesn't exist or isn't owned by
// the requesting user. Handlers map it to 404 (not 403) so existence isn't
// leaked across users.
var ErrNotFound = errors.New("app password not found")

type RevokeUseCase struct {
	repo   user.AppPasswordRepository
	logger *logging.SecurityLogger
}

func NewRevokeUseCase(repo user.AppPasswordRepository, logger *logging.SecurityLogger) *RevokeUseCase {
	return &RevokeUseCase{repo: repo, logger: logger}
}

func (uc *RevokeUseCase) Execute(ctx context.Context, userID uint, appPwdUUID, ip, userAgent string) error {
	ap, err := uc.repo.GetByUUID(ctx, appPwdUUID)
	if err != nil {
		return err
	}
	if ap == nil {
		return ErrNotFound
	}
	// Ownership check: an app password may only be revoked by its owner.
	// Without this, any authenticated user could revoke another user's app
	// password by guessing/learning its UUID (IDOR).
	if ap.UserID != userID {
		return ErrNotFound
	}

	now := time.Now()
	ap.RevokedAt = &now

	if err := uc.repo.Update(ctx, ap); err != nil {
		return err
	}

	uc.logger.LogAppPasswordRevoked(ctx, ap.UserID, ap.Name, ip, userAgent)
	return nil
}
