package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"
)

func newSetupUC(repo *mockUserRepo, cfg *config.Config) (*SetupUseCase, *mockCalendarRepo, *mockAddressBookRepo) {
	calRepo := new(mockCalendarRepo)
	abRepo := new(mockAddressBookRepo)
	register := NewRegisterUseCase(repo, calRepo, abRepo, new(mockEmailService), cfg)
	return NewSetupUseCase(repo, register, cfg.Registration.SetupToken), calRepo, abRepo
}

// SCOPE: first-run setup creates an active, verified admin only while the user
// table is empty, and then closes for good.
func TestSetupUseCase_CreatesAdminOnEmptyServer(t *testing.T) {
	repo := new(mockUserRepo)
	// SMTP configured: the admin must still be active and verified at once.
	uc, calRepo, abRepo := newSetupUC(repo, &config.Config{SMTP: config.SMTPConfig{Host: "smtp.example.com"}})
	ctx := context.Background()

	repo.On("Count", ctx).Return(int64(0), nil)
	repo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Username == "admin" && u.Email == "a@example.com" &&
			u.IsAdmin && u.IsActive && u.EmailVerified &&
			bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("SecurePass123!")) == nil
	})).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Username and email are normalised to lower case.
	u, err := uc.Execute(ctx, "  Admin ", "A@Example.com", "SecurePass123!", "Admin", "")

	assert.NoError(t, err)
	assert.True(t, u.IsAdmin)
	repo.AssertExpectations(t)
	calRepo.AssertExpectations(t)
	abRepo.AssertExpectations(t)
}

func TestSetupUseCase_RefusesOnceAnyUserExists(t *testing.T) {
	repo := new(mockUserRepo)
	uc, _, _ := newSetupUC(repo, &config.Config{})
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(1), nil)

	_, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "", "")

	assert.ErrorIs(t, err, ErrSetupCompleted)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestSetupUseCase_Validation(t *testing.T) {
	cases := []struct {
		name, username, email, password string
		want                            error
	}{
		{"short username", "ab", "a@example.com", "SecurePass123!", ErrInvalidUsername},
		{"bad characters", "ad min", "a@example.com", "SecurePass123!", ErrInvalidUsername},
		{"bad email", "admin", "not-an-email", "SecurePass123!", user.ErrInvalidEmail},
		{"weak password", "admin", "a@example.com", "short", user.ErrPasswordTooShort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(mockUserRepo)
			uc, _, _ := newSetupUC(repo, &config.Config{})
			ctx := context.Background()
			repo.On("Count", ctx).Return(int64(0), nil)

			_, err := uc.Execute(ctx, tc.username, tc.email, tc.password, "", "")

			assert.ErrorIs(t, err, tc.want)
			repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
		})
	}
}

// A repository error must fail closed: never create an account on a failed check.
func TestSetupUseCase_CountErrorFailsClosed(t *testing.T) {
	repo := new(mockUserRepo)
	uc, _, _ := newSetupUC(repo, &config.Config{})
	ctx := context.Background()
	boom := errors.New("db down")
	repo.On("Count", ctx).Return(int64(0), boom)

	_, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "", "")

	assert.ErrorIs(t, err, boom)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestSetupUseCase_CreateErrorIsReturned(t *testing.T) {
	repo := new(mockUserRepo)
	uc, calRepo, _ := newSetupUC(repo, &config.Config{})
	ctx := context.Background()
	boom := errors.New("unique constraint")
	repo.On("Count", ctx).Return(int64(0), nil)
	repo.On("Create", ctx, mock.Anything).Return(boom)

	_, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "", "")

	assert.ErrorIs(t, err, boom)
	calRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// --- setup token (CALDAV_SETUP_TOKEN) ---------------------------------------

func tokenCfg(token string) *config.Config {
	return &config.Config{Registration: config.RegistrationConfig{SetupToken: token}}
}

func TestSetupUseCase_RequiresConfiguredToken(t *testing.T) {
	for _, given := range []string{"", "wrong", "s3cret-tokeN", "s3cret-token-extra"} {
		repo := new(mockUserRepo)
		uc, _, _ := newSetupUC(repo, tokenCfg("s3cret-token"))
		ctx := context.Background()
		repo.On("Count", ctx).Return(int64(0), nil)

		_, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "", given)

		assert.ErrorIsf(t, err, ErrInvalidSetupToken, "token %q must be refused", given)
		repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	}
}

// The token check must come BEFORE validation and bcrypt: a wrong token with
// otherwise invalid input is refused as a token error, not a validation error.
func TestSetupUseCase_TokenCheckedBeforeValidation(t *testing.T) {
	repo := new(mockUserRepo)
	uc, _, _ := newSetupUC(repo, tokenCfg("s3cret-token"))
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(0), nil)

	_, err := uc.Execute(ctx, "bad name", "not-an-email", "short", "", "wrong")

	assert.ErrorIs(t, err, ErrInvalidSetupToken)
}

func TestSetupUseCase_AcceptsConfiguredToken(t *testing.T) {
	repo := new(mockUserRepo)
	uc, calRepo, abRepo := newSetupUC(repo, tokenCfg("s3cret-token"))
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(0), nil)
	repo.On("Create", ctx, mock.Anything).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Surrounding whitespace from a copy/paste is tolerated.
	u, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "", "  s3cret-token\n")

	assert.NoError(t, err)
	assert.True(t, u.IsAdmin)
}

// With no token configured the token field is ignored: previous behaviour.
func TestSetupUseCase_EmptyTokenConfigIgnoresTokenField(t *testing.T) {
	repo := new(mockUserRepo)
	uc, calRepo, abRepo := newSetupUC(repo, tokenCfg(""))
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(0), nil)
	repo.On("Create", ctx, mock.Anything).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	_, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "", "anything")

	assert.NoError(t, err)
}

// A 409 must win over the token check, so a configured server reveals nothing
// about the token.
func TestSetupUseCase_ConfiguredServerAnswersConflictBeforeToken(t *testing.T) {
	repo := new(mockUserRepo)
	uc, _, _ := newSetupUC(repo, tokenCfg("s3cret-token"))
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(1), nil)

	_, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "", "wrong")

	assert.ErrorIs(t, err, ErrSetupCompleted)
}

func TestRegisterUseCase_RefusedUntilSetupWhenTokenConfigured(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewRegisterUseCase(repo, new(mockCalendarRepo), new(mockAddressBookRepo), new(mockEmailService), tokenCfg("s3cret-token"))
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(0), nil)

	_, _, err := uc.Execute(ctx, "user@example.com", "SecurePass123!", "User")

	assert.ErrorIs(t, err, ErrSetupRequired)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestRegisterUseCase_OpenOnceSetupDone(t *testing.T) {
	repo := new(mockUserRepo)
	calRepo := new(mockCalendarRepo)
	abRepo := new(mockAddressBookRepo)
	uc := NewRegisterUseCase(repo, calRepo, abRepo, new(mockEmailService), tokenCfg("s3cret-token"))
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(1), nil)
	repo.On("GetByEmail", ctx, "user@example.com").Return(nil, nil)
	repo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	repo.On("Create", ctx, mock.Anything).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	_, _, err := uc.Execute(ctx, "user@example.com", "SecurePass123!", "User")

	assert.NoError(t, err)
}

// Without a token configured registration never even looks at the user count.
func TestRegisterUseCase_NoTokenNeverChecksCount(t *testing.T) {
	repo := new(mockUserRepo)
	calRepo := new(mockCalendarRepo)
	abRepo := new(mockAddressBookRepo)
	uc := NewRegisterUseCase(repo, calRepo, abRepo, new(mockEmailService), tokenCfg(""))
	ctx := context.Background()
	repo.On("GetByEmail", ctx, "user@example.com").Return(nil, nil)
	repo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	repo.On("Create", ctx, mock.Anything).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	_, _, err := uc.Execute(ctx, "user@example.com", "SecurePass123!", "User")

	assert.NoError(t, err)
	repo.AssertNotCalled(t, "Count", mock.Anything)
}
