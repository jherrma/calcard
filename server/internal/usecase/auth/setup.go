package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
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
	// ErrInvalidSetupToken is returned when CALDAV_SETUP_TOKEN is configured and
	// the request does not carry it.
	ErrInvalidSetupToken = errors.New("invalid setup token")
	// ErrSetupRequired is returned to registration attempts while a setup token
	// is configured and the first account does not exist yet.
	ErrSetupRequired = errors.New("server setup is not complete")
	// ErrInvalidUsername rejects usernames outside the allowed shape.
	ErrInvalidUsername = errors.New("username must be 3-64 characters: lowercase letters, digits, '.', '_' or '-', starting with a letter or digit")
)

// usernamePattern is lowercase-only on purpose: every new username is stored in
// lower case (registration generates lowercase letters too), so the stored
// name is what a client that lower-cases it sends.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

// SetupUseCase creates the first account, as an administrator. It is open only
// while no account has ever existed. The check-then-insert is serialised within
// one process only (mutex); a concurrent /auth/register or a second server
// process on a shared Postgres can still win the race.
type SetupUseCase struct {
	repo     user.UserRepository
	register *RegisterUseCase // reused for the default calendar / address book
	token    string           // CALDAV_SETUP_TOKEN; empty = no token required
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

// NewSetupUseCase builds the use case. token is CALDAV_SETUP_TOKEN; empty means
// setup needs no token.
func NewSetupUseCase(repo user.UserRepository, register *RegisterUseCase, token string) *SetupUseCase {
	return &SetupUseCase{repo: repo, register: register, token: token}
}

func (uc *SetupUseCase) Execute(ctx context.Context, username, email, password, displayName, setupToken string) (*user.User, error) {
	// Cheap pre-check first, so a configured server answers 409 without
	// validating or hashing anything (no bcrypt work for unauthenticated callers).
	if err := uc.ensureEmpty(ctx); err != nil {
		return nil, err
	}

	// Only a caller who knows the token may proceed. This sits before validation
	// and bcrypt so a wrong token costs nothing.
	if uc.token != "" && !tokenMatches(setupToken, uc.token) {
		return nil, ErrInvalidSetupToken
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

// setupPending reports whether registration must be refused because a setup
// token is configured and no account has ever existed. With no token it is
// always false (the previous, open behaviour).
func setupPending(ctx context.Context, repo user.UserRepository, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	count, err := repo.Count(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// tokenMatches compares the SHA-256 digests in constant time, so neither the
// content nor the length of the configured token shows in the response time.
func tokenMatches(given, want string) bool {
	g := sha256.Sum256([]byte(strings.TrimSpace(given)))
	w := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}
