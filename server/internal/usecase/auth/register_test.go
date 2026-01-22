package auth

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockUserRepo struct {
	mock.Mock
}

func (m *mockUserRepo) Create(ctx context.Context, u *user.User) error {
	args := m.Called(ctx, u)
	return args.Error(0)
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) GetByUUID(ctx context.Context, uuid string) (*user.User, error) {
	args := m.Called(ctx, uuid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) Update(ctx context.Context, u *user.User) error {
	args := m.Called(ctx, u)
	return args.Error(0)
}

func (m *mockUserRepo) CreateVerification(ctx context.Context, v *user.EmailVerification) error {
	args := m.Called(ctx, v)
	return args.Error(0)
}

func (m *mockUserRepo) GetVerificationByToken(ctx context.Context, token string) (*user.EmailVerification, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.EmailVerification), args.Error(1)
}

func (m *mockUserRepo) DeleteVerification(ctx context.Context, token string) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

type mockEmailService struct {
	mock.Mock
}

func (m *mockEmailService) SendActivationEmail(ctx context.Context, to, link string) error {
	args := m.Called(ctx, to, link)
	return args.Error(0)
}

func TestRegisterUseCase_Execute_NoSMTP(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP: config.SMTPConfig{Host: ""},
	}
	uc := NewRegisterUseCase(repo, emailSvc, cfg)

	ctx := context.Background()
	email := "test@example.com"
	password := "SecurePass123!"

	repo.On("GetByEmail", ctx, email).Return(nil, nil)
	repo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Email == email && u.IsActive == true && u.EmailVerified == true
	})).Return(nil)

	u, token, err := uc.Execute(ctx, email, password, "Test User")

	assert.NoError(t, err)
	assert.NotNil(t, u)
	assert.Empty(t, token)
	repo.AssertExpectations(t)
}

func TestRegisterUseCase_Execute_WithSMTP(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP:    config.SMTPConfig{Host: "smtp.example.com", From: "no-reply@example.com"},
		BaseURL: "http://localhost:8080",
	}
	uc := NewRegisterUseCase(repo, emailSvc, cfg)

	ctx := context.Background()
	email := "test@example.com"
	password := "SecurePass123!"

	repo.On("GetByEmail", ctx, email).Return(nil, nil)
	repo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Email == email && u.IsActive == false && u.EmailVerified == false
	})).Return(nil)
	repo.On("CreateVerification", ctx, mock.Anything).Return(nil)
	emailSvc.On("SendActivationEmail", ctx, email, mock.Anything).Return(nil)

	u, token, err := uc.Execute(ctx, email, password, "Test User")

	assert.NoError(t, err)
	assert.NotNil(t, u)
	assert.NotEmpty(t, token)
	repo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

func TestRegisterUseCase_Execute_CaseInsensitive(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP: config.SMTPConfig{Host: ""},
	}
	uc := NewRegisterUseCase(repo, emailSvc, cfg)

	ctx := context.Background()
	email := "TEST@Example.Com"
	expectedEmail := "test@example.com"
	password := "SecurePass123!"

	repo.On("GetByEmail", ctx, expectedEmail).Return(nil, nil)
	repo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Email == expectedEmail
	})).Return(nil)

	u, _, err := uc.Execute(ctx, email, password, "Test User")

	assert.NoError(t, err)
	assert.Equal(t, expectedEmail, u.Email)
	repo.AssertExpectations(t)
}
