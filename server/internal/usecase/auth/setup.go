package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrSetupCompleted is returned once any user exists: first-run setup is a
	// one-shot that must not become a way to mint admins later.
	ErrSetupCompleted = errors.New("setup already completed")
	// ErrInvalidUsername rejects usernames outside the allowed shape.
	ErrInvalidUsername = errors.New("username must be 3-64 characters: lowercase letters, digits, '.', '_' or '-', starting with a letter or digit")
)

// usernamePattern is lowercase-only on purpose: user lookup lower-cases the
// name before matching, so a mixed-case username could never log in.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

// SetupUseCase creates the first account, as an administrator. It is open only
// while the user table is empty.
type SetupUseCase struct {
	repo     user.UserRepository
	register *RegisterUseCase // reused for the default calendar / address book
	mu       sync.Mutex       // serialises the count-then-create check in this process
}

func NewSetupUseCase(repo user.UserRepository, register *RegisterUseCase) *SetupUseCase {
	return &SetupUseCase{repo: repo, register: register}
}

func (uc *SetupUseCase) Execute(ctx context.Context, username, email, password, displayName string) (*user.User, error) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	count, err := uc.repo.Count(ctx)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrSetupCompleted
	}

	username = strings.ToLower(strings.TrimSpace(username))
	email = strings.ToLower(strings.TrimSpace(email))
	if !usernamePattern.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	if err := user.ValidateEmail(email); err != nil {
		return nil, err
	}
	if err := user.ValidatePassword(password); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), user.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// The administrator is active and verified immediately, SMTP or not: there
	// is nobody else who could approve the account.
	u := &user.User{
		UUID:          uuid.New().String(),
		Email:         email,
		Username:      username,
		PasswordHash:  string(hash),
		DisplayName:   displayName,
		IsActive:      true,
		EmailVerified: true,
		IsAdmin:       true,
	}
	if err := uc.repo.Create(ctx, u); err != nil {
		return nil, err
	}

	if err := uc.register.createDefaultCalendar(ctx, u.ID); err != nil {
		fmt.Printf("failed to create default calendar: %v\n", err)
	}
	if err := uc.register.createDefaultAddressBook(ctx, u.ID); err != nil {
		fmt.Printf("failed to create default address book: %v\n", err)
	}
	return u, nil
}
