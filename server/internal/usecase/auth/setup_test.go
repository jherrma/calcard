package auth

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newSetupUC(repo *mockUserRepo, cfg *config.Config) (*SetupUseCase, *mockCalendarRepo, *mockAddressBookRepo) {
	calRepo := new(mockCalendarRepo)
	abRepo := new(mockAddressBookRepo)
	register := NewRegisterUseCase(repo, calRepo, abRepo, new(mockEmailService), cfg)
	return NewSetupUseCase(repo, register), calRepo, abRepo
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
			u.IsAdmin && u.IsActive && u.EmailVerified && u.PasswordHash != "SecurePass123!"
	})).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Username and email are normalised to lower case.
	u, err := uc.Execute(ctx, "  Admin ", "A@Example.com", "SecurePass123!", "Admin")

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

	_, err := uc.Execute(ctx, "admin", "a@example.com", "SecurePass123!", "")

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

			_, err := uc.Execute(ctx, tc.username, tc.email, tc.password, "")

			assert.ErrorIs(t, err, tc.want)
			repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
		})
	}
}
