package auth

import (
	"context"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// LinkedProvider represents a linked OAuth provider
type LinkedProvider struct {
	Provider string    `json:"provider"`
	Email    string    `json:"email"`
	LinkedAt time.Time `json:"linked_at"`
}

// ListLinkedProvidersUseCase handles listing linked OAuth providers
type ListLinkedProvidersUseCase struct {
	oauthRepo user.OAuthConnectionRepository
	userRepo  user.UserRepository
}

// NewListLinkedProvidersUseCase creates a new ListLinkedProvidersUseCase
func NewListLinkedProvidersUseCase(
	oauthRepo user.OAuthConnectionRepository,
	userRepo user.UserRepository,
) *ListLinkedProvidersUseCase {
	return &ListLinkedProvidersUseCase{
		oauthRepo: oauthRepo,
		userRepo:  userRepo,
	}
}

// Execute lists linked providers for the user and checks if password auth is available
func (uc *ListLinkedProvidersUseCase) Execute(ctx context.Context, userID uint) ([]LinkedProvider, bool, error) {
	conns, err := uc.oauthRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, false, err
	}

	u, err := uc.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, false, err
	}

	hasPassword := false
	if u != nil {
		hasPassword = u.PasswordHash != "" && u.PasswordHash != "*OAUTH_USER*"
	}

	var result []LinkedProvider
	for _, c := range conns {
		result = append(result, LinkedProvider{
			Provider: c.Provider,
			Email:    c.ProviderEmail,
			LinkedAt: c.CreatedAt,
		})
	}

	return result, hasPassword, nil
}
