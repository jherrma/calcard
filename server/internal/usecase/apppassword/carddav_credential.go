package apppassword

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"golang.org/x/crypto/bcrypt"
)

// CreateCardDAVCredentialInput is the input for creating a CardDAV credential
type CreateCardDAVCredentialInput struct {
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Password   string     `json:"password"`
	Permission string     `json:"permission"`
	ExpiresAt  *time.Time `json:"expires_at"`
	IP         string     `json:"-"`
	UserAgent  string     `json:"-"`
}

// CreateCardDAVCredentialOutput is the output after creating a CardDAV credential
type CreateCardDAVCredentialOutput struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Permission string     `json:"permission"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateCardDAVCredentialUseCase handles creating CardDAV credentials
type CreateCardDAVCredentialUseCase struct {
	credRepo user.CardDAVCredentialRepository
	logger   *logging.SecurityLogger
}

// NewCreateCardDAVCredentialUseCase creates a new use case
func NewCreateCardDAVCredentialUseCase(credRepo user.CardDAVCredentialRepository, logger *logging.SecurityLogger) *CreateCardDAVCredentialUseCase {
	return &CreateCardDAVCredentialUseCase{credRepo: credRepo, logger: logger}
}

// Execute creates a new CardDAV credential
func (uc *CreateCardDAVCredentialUseCase) Execute(ctx context.Context, userID uint, input CreateCardDAVCredentialInput) (*CreateCardDAVCredentialOutput, error) {
	// Validate name
	name, ok := user.NormalizeCredentialName(input.Name)
	if !ok {
		return nil, fmt.Errorf("name is required and must be at most %d characters", user.MaxCredentialNameLength)
	}

	// An expiry must lie ahead: a credential that is already expired could
	// never authenticate.
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("expires_at must be in the future")
	}

	// Validate username: lowercase letters a-z only, for new credentials.
	if err := user.ValidateDAVCredentialUsername(input.Username); err != nil {
		return nil, err
	}

	// Validate password
	if len(input.Password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Check for existing username
	existing, _ := uc.credRepo.GetByUsername(ctx, input.Username)
	if existing != nil {
		return nil, fmt.Errorf("username '%s' is already in use", input.Username)
	}

	// Default permission
	permission := input.Permission
	if permission == "" {
		permission = "read-write"
	}
	if permission != "read" && permission != "read-write" {
		return nil, fmt.Errorf("permission must be 'read' or 'read-write'")
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	cred := &user.CardDAVCredential{
		UUID:         uuid.New().String(),
		UserID:       userID,
		Name:         name,
		Username:     input.Username,
		PasswordHash: string(hash),
		Permission:   permission,
		ExpiresAt:    input.ExpiresAt,
		CreatedAt:    time.Now(),
	}

	if err := uc.credRepo.Create(ctx, cred); err != nil {
		// Taken despite the check above (a concurrent create): same answer.
		if errors.Is(err, user.ErrCredentialUsernameTaken) {
			return nil, fmt.Errorf("username '%s' is already in use", input.Username)
		}
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}

	uc.logger.LogAppPasswordCreated(ctx, userID, name, input.IP, input.UserAgent)

	return &CreateCardDAVCredentialOutput{
		ID:         cred.UUID,
		Name:       cred.Name,
		Username:   cred.Username,
		Permission: cred.Permission,
		ExpiresAt:  cred.ExpiresAt,
		CreatedAt:  cred.CreatedAt,
	}, nil
}

// ListCardDAVCredentialsUseCase handles listing CardDAV credentials
type ListCardDAVCredentialsUseCase struct {
	credRepo user.CardDAVCredentialRepository
}

// NewListCardDAVCredentialsUseCase creates a new use case
func NewListCardDAVCredentialsUseCase(credRepo user.CardDAVCredentialRepository) *ListCardDAVCredentialsUseCase {
	return &ListCardDAVCredentialsUseCase{credRepo: credRepo}
}

// Execute lists all CardDAV credentials for a user
func (uc *ListCardDAVCredentialsUseCase) Execute(ctx context.Context, userID uint) ([]user.CardDAVCredential, error) {
	return uc.credRepo.ListByUserID(ctx, userID)
}

// RevokeCardDAVCredentialUseCase handles revoking CardDAV credentials
type RevokeCardDAVCredentialUseCase struct {
	credRepo user.CardDAVCredentialRepository
	logger   *logging.SecurityLogger
}

// NewRevokeCardDAVCredentialUseCase creates a new use case
func NewRevokeCardDAVCredentialUseCase(credRepo user.CardDAVCredentialRepository, logger *logging.SecurityLogger) *RevokeCardDAVCredentialUseCase {
	return &RevokeCardDAVCredentialUseCase{credRepo: credRepo, logger: logger}
}

// Execute revokes a CardDAV credential
func (uc *RevokeCardDAVCredentialUseCase) Execute(ctx context.Context, userID uint, credUUID, ip, userAgent string) error {
	cred, err := uc.credRepo.GetByUUID(ctx, credUUID)
	if err != nil {
		return fmt.Errorf("credential not found")
	}

	if cred.UserID != userID {
		return fmt.Errorf("credential not found")
	}

	if err := uc.credRepo.Revoke(ctx, cred.ID); err != nil {
		return err
	}

	uc.logger.LogAppPasswordRevoked(ctx, cred.UserID, cred.Name, ip, userAgent)
	return nil
}
