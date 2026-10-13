package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"
)

type mockPasswordResetRepo struct {
	mock.Mock
}

func (m *mockPasswordResetRepo) Create(ctx context.Context, reset *user.PasswordReset) error {
	args := m.Called(ctx, reset)
	return args.Error(0)
}

func (m *mockPasswordResetRepo) GetByHash(ctx context.Context, hash string) (*user.PasswordReset, error) {
	args := m.Called(ctx, hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.PasswordReset), args.Error(1)
}

func (m *mockPasswordResetRepo) DeleteByUserID(ctx context.Context, userID uint) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func TestResetPasswordUseCase_Execute_RejectsWeakPassword(t *testing.T) {
	cases := []struct {
		name, newPassword string
		want              error
	}{
		{"too short", "a", user.ErrPasswordTooShort},
		// Over bcrypt's limit: refused by the policy, not by a failed hash
		// (which the handler would turn into a 500).
		{"over 72 bytes", "Aa1!" + strings.Repeat("x", 69), user.ErrPasswordTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userRepo := new(mockUserRepo)
			resetRepo := new(mockPasswordResetRepo)
			uc := NewResetPasswordUseCase(userRepo, resetRepo)

			ctx := context.Background()
			u := &user.User{ID: 1, UUID: "user-uuid", Email: "test@example.com", PasswordHash: "original-hash"}
			reset := &user.PasswordReset{UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}

			resetRepo.On("GetByHash", ctx, mock.Anything).Return(reset, nil)
			userRepo.On("GetByID", ctx, uint(1)).Return(u, nil)

			err := uc.Execute(ctx, ResetPasswordRequest{Token: "raw-token", NewPassword: tc.newPassword})

			assert.ErrorIs(t, err, tc.want)
			// The account must not have been touched on the rejected path.
			assert.Equal(t, "original-hash", u.PasswordHash)
			userRepo.AssertNotCalled(t, "CompletePasswordReset", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestResetPasswordUseCase_Execute_CompliantPassword(t *testing.T) {
	userRepo := new(mockUserRepo)
	resetRepo := new(mockPasswordResetRepo)
	uc := NewResetPasswordUseCase(userRepo, resetRepo)

	ctx := context.Background()
	const newPassword = "SecurePass123!"
	u := &user.User{ID: 1, UUID: "user-uuid", Email: "test@example.com", PasswordHash: "original-hash"}
	reset := &user.PasswordReset{UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}

	resetRepo.On("GetByHash", ctx, mock.Anything).Return(reset, nil)
	userRepo.On("GetByID", ctx, uint(1)).Return(u, nil)
	// Saves the password and revokes tokens and credentials in one call (P02).
	userRepo.On("CompletePasswordReset", ctx, mock.Anything, mock.AnythingOfType("time.Time")).Return(nil)

	err := uc.Execute(ctx, ResetPasswordRequest{Token: "raw-token", NewPassword: newPassword})

	assert.NoError(t, err)
	// The stored hash must verify against the new password.
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(newPassword)))
	// And it must be hashed with the shared cost, not bcrypt.DefaultCost.
	cost, err := bcrypt.Cost([]byte(u.PasswordHash))
	assert.NoError(t, err)
	assert.Equal(t, user.BcryptCost, cost)
	userRepo.AssertExpectations(t)
	resetRepo.AssertExpectations(t)
}
