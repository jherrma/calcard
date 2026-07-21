package auth

import (
	"context"
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
	userRepo := new(mockUserRepo)
	resetRepo := new(mockPasswordResetRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	uc := NewResetPasswordUseCase(userRepo, resetRepo, refreshRepo)

	ctx := context.Background()
	u := &user.User{ID: 1, UUID: "user-uuid", Email: "test@example.com", PasswordHash: "original-hash"}
	reset := &user.PasswordReset{UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}

	resetRepo.On("GetByHash", ctx, mock.Anything).Return(reset, nil)
	userRepo.On("GetByID", ctx, uint(1)).Return(u, nil)

	err := uc.Execute(ctx, ResetPasswordRequest{Token: "raw-token", NewPassword: "a"})

	assert.ErrorIs(t, err, user.ErrPasswordTooShort)
	// The account must not have been touched on the rejected path.
	assert.Equal(t, "original-hash", u.PasswordHash)
	userRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	resetRepo.AssertNotCalled(t, "DeleteByUserID", mock.Anything, mock.Anything)
	refreshRepo.AssertNotCalled(t, "DeleteByUserID", mock.Anything, mock.Anything)
}

func TestResetPasswordUseCase_Execute_CompliantPassword(t *testing.T) {
	userRepo := new(mockUserRepo)
	resetRepo := new(mockPasswordResetRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	uc := NewResetPasswordUseCase(userRepo, resetRepo, refreshRepo)

	ctx := context.Background()
	const newPassword = "SecurePass123!"
	u := &user.User{ID: 1, UUID: "user-uuid", Email: "test@example.com", PasswordHash: "original-hash"}
	reset := &user.PasswordReset{UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}

	resetRepo.On("GetByHash", ctx, mock.Anything).Return(reset, nil)
	userRepo.On("GetByID", ctx, uint(1)).Return(u, nil)
	userRepo.On("Update", ctx, mock.Anything).Return(nil)
	resetRepo.On("DeleteByUserID", ctx, uint(1)).Return(nil)
	refreshRepo.On("DeleteByUserID", ctx, uint(1)).Return(nil)

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
	refreshRepo.AssertExpectations(t)
}
