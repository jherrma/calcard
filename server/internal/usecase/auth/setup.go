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
// while no account has ever existed. The check-then-insert is serialised within
// one process only (mutex); a concurrent /auth/register or a second server
// process on a shared Postgres can still win the race.
type SetupUseCase struct {
	repo     user.UserRepository
	register *RegisterUseCase // reused for the default calendar / address book
	mu       sync.Mutex       // serialises the count-then-create check in this process
}

// ensureEmpty fails with ErrSetupCompleted once any account has ever existed.
// A repository error fails closed.
func (uc *SetupUseCase) ensureEmpty(ctx context.Context) error {
	count, err := uc.repo.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrSetupCompleted
	}
	return nil
}

func NewSetupUseCase(repo user.UserRepository, register *RegisterUseCase) *SetupUseCase {
	return &SetupUseCase{repo: repo, register: register}
}

func (uc *SetupUseCase) Execute(ctx context.Context, username, email, password, displayName string) (*user.User, error) {
	// Cheap pre-check first, so a configured server answers 409 without
	// validating or hashing anything (no bcrypt work for unauthenticated callers).
	if err := uc.ensureEmpty(ctx); err != nil {
		return nil, err
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

	// Hash BEFORE taking the lock and re-checking: bcrypt takes ~250 ms, and
	// holding the check-to-insert window open that long is what let concurrent
	// registrations slip in ahead of the admin.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), user.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	uc.mu.Lock()
	defer uc.mu.Unlock()
	if err := uc.ensureEmpty(ctx); err != nil {
		return nil, err
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
