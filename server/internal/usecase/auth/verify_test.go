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
	u := &user.User{
		Email:         "test@example.com",
		IsActive:      false,
		EmailVerified: false,
	}
	v := &user.EmailVerification{
		UserID:    1,
		Token:     token,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		User:      *u,
	}

	repo.On("GetVerificationByToken", ctx, token).Return(v, nil)
	repo.On("Update", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.IsActive == true && u.EmailVerified == true
	})).Return(nil)
	repo.On("DeleteVerification", ctx, token).Return(nil)

	err := uc.Execute(ctx, token)

	assert.NoError(t, err)
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

	repo.On("GetVerificationByToken", ctx, token).Return(v, nil)
	repo.On("DeleteVerification", ctx, token).Return(nil)

	err := uc.Execute(ctx, token)

	assert.ErrorIs(t, err, ErrInvalidToken)
	repo.AssertExpectations(t)
}
