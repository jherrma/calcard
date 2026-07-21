package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"
)

func newTestSecurityLogger() *logging.SecurityLogger {
	return logging.NewSecurityLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func newChangePasswordUser(t *testing.T, currentPassword string) *user.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(currentPassword), bcrypt.DefaultCost)
	assert.NoError(t, err)
	return &user.User{ID: 1, UUID: "user-uuid", Email: "test@example.com", PasswordHash: string(hash)}
}

func TestChangePasswordUseCase_Execute_RejectsWeakPassword(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	jwtManager := new(mockTokenProvider)
	uc := NewChangePasswordUseCase(userRepo, refreshRepo, jwtManager, newTestSecurityLogger())

	ctx := context.Background()
	const currentPassword = "OldPass123!"
	u := newChangePasswordUser(t, currentPassword)
	originalHash := u.PasswordHash

	userRepo.On("GetByUUID", ctx, u.UUID).Return(u, nil)

	res, err := uc.Execute(ctx, ChangePasswordRequest{
		UserUUID:        u.UUID,
		CurrentPassword: currentPassword,
		NewPassword:     "a",
	})

	assert.Nil(t, res)
	assert.ErrorIs(t, err, user.ErrPasswordTooShort)
	// The account must not have been touched on the rejected path.
	assert.Equal(t, originalHash, u.PasswordHash)
	refreshRepo.AssertNotCalled(t, "DeleteByUserID", mock.Anything, mock.Anything)
	userRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	jwtManager.AssertNotCalled(t, "GenerateAccessToken", mock.Anything, mock.Anything)
}

func TestChangePasswordUseCase_Execute_CompliantPassword(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	jwtManager := new(mockTokenProvider)
	uc := NewChangePasswordUseCase(userRepo, refreshRepo, jwtManager, newTestSecurityLogger())

	ctx := context.Background()
	const currentPassword = "OldPass123!"
	const newPassword = "SecurePass123!"
	u := newChangePasswordUser(t, currentPassword)

	userRepo.On("GetByUUID", ctx, u.UUID).Return(u, nil)
	refreshRepo.On("DeleteByUserID", ctx, uint(1)).Return(nil)
	userRepo.On("Update", ctx, mock.Anything).Return(nil)
	jwtManager.On("GenerateAccessToken", u.UUID, u.Email).Return("access-token", time.Now(), nil)

	res, err := uc.Execute(ctx, ChangePasswordRequest{
		UserUUID:        u.UUID,
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
	})

	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, "access-token", res.AccessToken)
	// The stored hash must verify against the new password.
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(newPassword)))
	// And it must be hashed with the shared cost, not bcrypt.DefaultCost.
	cost, err := bcrypt.Cost([]byte(u.PasswordHash))
	assert.NoError(t, err)
	assert.Equal(t, user.BcryptCost, cost)
	userRepo.AssertExpectations(t)
	refreshRepo.AssertExpectations(t)
	jwtManager.AssertExpectations(t)
}
