package apppassword

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"golang.org/x/crypto/bcrypt"
)

// CreateCalDAVCredentialInput is the input for creating a CalDAV credential
type CreateCalDAVCredentialInput struct {
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Password   string     `json:"password"`
	Permission string     `json:"permission"`
	ExpiresAt  *time.Time `json:"expires_at"`
	IP         string     `json:"-"`
	UserAgent  string     `json:"-"`
}

// CreateCalDAVCredentialOutput is the output after creating a CalDAV credential
type CreateCalDAVCredentialOutput struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Permission string     `json:"permission"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateCalDAVCredentialUseCase handles creating CalDAV credentials
type CreateCalDAVCredentialUseCase struct {
	credRepo user.CalDAVCredentialRepository
	logger   *logging.SecurityLogger
}

// NewCreateCalDAVCredentialUseCase creates a new use case
func NewCreateCalDAVCredentialUseCase(credRepo user.CalDAVCredentialRepository, logger *logging.SecurityLogger) *CreateCalDAVCredentialUseCase {
	return &CreateCalDAVCredentialUseCase{credRepo: credRepo, logger: logger}
}

// Execute creates a new CalDAV credential
func (uc *CreateCalDAVCredentialUseCase) Execute(ctx context.Context, userID uint, input CreateCalDAVCredentialInput) (*CreateCalDAVCredentialOutput, error) {
	// Validate name
	if len(input.Name) == 0 || len(input.Name) > 100 {
		return nil, fmt.Errorf("name is required and must be at most 100 characters")
	}

	// Validate username
	if len(input.Username) < 3 || len(input.Username) > 50 {
		return nil, fmt.Errorf("username must be between 3 and 50 characters")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(input.Username) {
		return nil, fmt.Errorf("username can only contain alphanumeric characters, hyphens, and underscores")
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

	// Validate password
	if len(input.Password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	cred := &user.CalDAVCredential{
		UUID:         uuid.New().String(),
		UserID:       userID,
		Name:         input.Name,
		Username:     input.Username,
		PasswordHash: string(hash),
		Permission:   permission,
		ExpiresAt:    input.ExpiresAt,
		CreatedAt:    time.Now(),
	}

	if err := uc.credRepo.Create(ctx, cred); err != nil {
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}

	uc.logger.LogAppPasswordCreated(ctx, userID, input.Name, input.IP, input.UserAgent) // Using generic app password event for now or create specific if needed

	return &CreateCalDAVCredentialOutput{
		ID:         cred.UUID,
		Name:       cred.Name,
		Username:   cred.Username,
		Permission: cred.Permission,
		ExpiresAt:  cred.ExpiresAt,
		CreatedAt:  cred.CreatedAt,
	}, nil
}

// ListCalDAVCredentialsUseCase handles listing CalDAV credentials
type ListCalDAVCredentialsUseCase struct {
	credRepo user.CalDAVCredentialRepository
}

// NewListCalDAVCredentialsUseCase creates a new use case
func NewListCalDAVCredentialsUseCase(credRepo user.CalDAVCredentialRepository) *ListCalDAVCredentialsUseCase {
	return &ListCalDAVCredentialsUseCase{credRepo: credRepo}
}

// Execute lists all CalDAV credentials for a user
func (uc *ListCalDAVCredentialsUseCase) Execute(ctx context.Context, userID uint) ([]user.CalDAVCredential, error) {
	return uc.credRepo.ListByUserID(ctx, userID)
}

// RevokeCalDAVCredentialUseCase handles revoking CalDAV credentials
type RevokeCalDAVCredentialUseCase struct {
	credRepo user.CalDAVCredentialRepository
	logger   *logging.SecurityLogger
}

// NewRevokeCalDAVCredentialUseCase creates a new use case
func NewRevokeCalDAVCredentialUseCase(credRepo user.CalDAVCredentialRepository, logger *logging.SecurityLogger) *RevokeCalDAVCredentialUseCase {
	return &RevokeCalDAVCredentialUseCase{credRepo: credRepo, logger: logger}
}

// Execute revokes a CalDAV credential
func (uc *RevokeCalDAVCredentialUseCase) Execute(ctx context.Context, userID uint, credUUID, ip, userAgent string) error {
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
