package auth

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// UnlinkProviderUseCase handles unlinking an OAuth provider from a user account
type UnlinkProviderUseCase struct {
	oauthRepo user.OAuthConnectionRepository
	userRepo  user.UserRepository
}

// NewUnlinkProviderUseCase creates a new UnlinkProviderUseCase
func NewUnlinkProviderUseCase(
	oauthRepo user.OAuthConnectionRepository,
	userRepo user.UserRepository,
) *UnlinkProviderUseCase {
	return &UnlinkProviderUseCase{
		oauthRepo: oauthRepo,
		userRepo:  userRepo,
	}
}

// Execute unlinks the specified provider for the user
func (uc *UnlinkProviderUseCase) Execute(ctx context.Context, userID uint, provider string) error {
	u, err := uc.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if u == nil {
		return fmt.Errorf("user not found")
	}

	conns, err := uc.oauthRepo.ListByUserID(ctx, userID)
	if err != nil {
		return err
	}

	hasPassword := u.PasswordHash != "" && u.PasswordHash != "*OAUTH_USER*" // Simple check
	otherProviders := 0
	foundTarget := false

	for _, c := range conns {
		if c.Provider == provider {
			foundTarget = true
		} else {
			otherProviders++
		}
	}

	if !foundTarget {
		return fmt.Errorf("provider %s is not linked to this account", provider)
	}

	if !hasPassword && otherProviders == 0 {
		return fmt.Errorf("cannot unlink provider: you must have at least one authentication method")
	}

	// 3. Delete the connection
	return uc.oauthRepo.Delete(ctx, userID, provider)
}
