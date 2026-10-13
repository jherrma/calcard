package auth

import (
	"context"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestVerifyUseCase_Execute_Success(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewVerifyUseCase(repo)

	ctx := context.Background()
	token := "valid-token"
	// Registration creates every account active; only the address waits.
	u := &user.User{
		Email:         "test@example.com",
		IsActive:      true,
		EmailVerified: false,
	}
	v := &user.EmailVerification{
		UserID:    1,
		Token:     token,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		User:      *u,
	}

	// The use case hashes the token before lookup/delete.
	tokenHash := hashVerificationToken(token)
	repo.On("GetVerificationByToken", ctx, tokenHash).Return(v, nil)
	repo.On("Update", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.IsActive == true && u.EmailVerified == true
	})).Return(nil)
	repo.On("DeleteVerification", ctx, tokenHash).Return(nil)

	err := uc.Execute(ctx, token)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}

// Confirming the address sets email_verified only. is_active=false means a
// deactivated account, and opening an activation link must not reactivate it.
func TestVerifyUseCase_Execute_DeactivatedStaysDeactivated(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewVerifyUseCase(repo)

	ctx := context.Background()
	token := "deactivated-token"
	v := &user.EmailVerification{
		UserID:    1,
		Token:     token,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		User:      user.User{Email: "off@example.com", IsActive: false, EmailVerified: false},
	}

	tokenHash := hashVerificationToken(token)
	repo.On("GetVerificationByToken", ctx, tokenHash).Return(v, nil)
	repo.On("Update", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.IsActive == false && u.EmailVerified == true
	})).Return(nil)
	repo.On("DeleteVerification", ctx, tokenHash).Return(nil)

	assert.NoError(t, uc.Execute(ctx, token))
	repo.AssertExpectations(t)
}

func TestVerifyUseCase_Execute_Expired(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewVerifyUseCase(repo)

	ctx := context.Background()
	token := "expired-token"
	v := &user.EmailVerification{
		Token:     token,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}

	tokenHash := hashVerificationToken(token)
	repo.On("GetVerificationByToken", ctx, tokenHash).Return(v, nil)
	repo.On("DeleteVerification", ctx, tokenHash).Return(nil)

	err := uc.Execute(ctx, token)

	assert.ErrorIs(t, err, ErrInvalidToken)
	repo.AssertExpectations(t)
}
