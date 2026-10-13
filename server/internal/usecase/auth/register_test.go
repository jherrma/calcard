package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockUserRepo struct {
	mock.Mock
}

type mockCalendarRepo struct {
	mock.Mock
	calendar.CalendarRepository // Embed to satisfy interface
}

func (m *mockCalendarRepo) Create(ctx context.Context, c *calendar.Calendar) error {
	args := m.Called(ctx, c)
	return args.Error(0)
}

type mockAddressBookRepo struct {
	mock.Mock
	addressbook.Repository // Embed to satisfy interface
}

func (m *mockAddressBookRepo) Create(ctx context.Context, ab *addressbook.AddressBook) error {
	args := m.Called(ctx, ab)
	return args.Error(0)
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

func (m *mockUserRepo) GetByID(ctx context.Context, id uint) (*user.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) Update(ctx context.Context, u *user.User) error {
	args := m.Called(ctx, u)
	return args.Error(0)
}

func (m *mockUserRepo) InvalidateAccessTokens(ctx context.Context, userID uint, at time.Time) error {
	args := m.Called(ctx, userID, at)
	return args.Error(0)
}

func (m *mockUserRepo) CompletePasswordReset(ctx context.Context, u *user.User, at time.Time) error {
	args := m.Called(ctx, u, at)
	return args.Error(0)
}

func (m *mockUserRepo) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	args := m.Called(ctx, username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) Delete(ctx context.Context, userID uint) error {
	args := m.Called(ctx, userID)
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

func (m *mockUserRepo) GetByOAuth(ctx context.Context, provider, providerID string) (*user.User, error) {
	args := m.Called(ctx, provider, providerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) Count(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

type mockEmailService struct {
	mock.Mock
}

func (m *mockEmailService) SendActivationEmail(ctx context.Context, to, link string) error {
	args := m.Called(ctx, to, link)
	return args.Error(0)
}

func (m *mockEmailService) SendEmail(ctx context.Context, to, subject, body string) error {
	args := m.Called(ctx, to, subject, body)
	return args.Error(0)
}

func TestRegisterUseCase_Execute_NoSMTP(t *testing.T) {
	repo := new(mockUserRepo)
	calRepo := new(mockCalendarRepo)
	abRepo := new(mockAddressBookRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP: config.SMTPConfig{Host: ""},
	}
	uc := NewRegisterUseCase(repo, calRepo, abRepo, emailSvc, cfg)

	ctx := context.Background()
	email := "test@example.com"
	password := "SecurePass123!"

	repo.On("GetByEmail", ctx, email).Return(nil, nil)
	// We can't predict the username, so we mock GetByUsername to return nil (available)
	repo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	repo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Email == email && len(u.Username) == 16 && u.IsActive == true && u.EmailVerified == true
	})).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	u, token, err := uc.Execute(ctx, email, password, "Test User")

	assert.NoError(t, err)
	assert.NotNil(t, u)
	assert.Len(t, u.Username, 16)
	assert.Empty(t, token)
	repo.AssertExpectations(t)
}

func TestRegisterUseCase_Execute_WithSMTP(t *testing.T) {
	repo := new(mockUserRepo)
	calRepo := new(mockCalendarRepo)
	abRepo := new(mockAddressBookRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP:    config.SMTPConfig{Host: "smtp.example.com", From: "no-reply@example.com"},
		BaseURL: "http://localhost:8080",
	}
	uc := NewRegisterUseCase(repo, calRepo, abRepo, emailSvc, cfg)

	ctx := context.Background()
	email := "test@example.com"
	password := "SecurePass123!"

	repo.On("GetByEmail", ctx, email).Return(nil, nil)
	repo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	repo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		// Usable at once; only the address waits for the activation link.
		return u.Email == email && len(u.Username) == 16 && u.IsActive == true && u.EmailVerified == false
	})).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)
	var stored *user.EmailVerification
	repo.On("CreateVerification", ctx, mock.Anything).
		Run(func(args mock.Arguments) { stored = args.Get(1).(*user.EmailVerification) }).Return(nil)
	var link string
	emailSvc.On("SendActivationEmail", ctx, email, mock.Anything).
		Run(func(args mock.Arguments) { link = args.String(2) }).Return(nil)

	u, token, err := uc.Execute(ctx, email, password, "Test User")

	assert.NoError(t, err)
	assert.NotNil(t, u)
	assert.Len(t, u.Username, 16)
	assert.NotEmpty(t, token)
	repo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)

	// The mail links the SPA page (which calls GET /api/v1/auth/verify), on
	// cfg.BaseURL, carrying the raw token whose hash was stored.
	assert.Equal(t, "http://localhost:8080/auth/verify?token="+token, link)
	require.NotNil(t, stored)
	assert.Equal(t, hashVerificationToken(token), stored.Token)
}

func TestRegisterUseCase_Execute_CaseInsensitive(t *testing.T) {
	repo := new(mockUserRepo)
	calRepo := new(mockCalendarRepo)
	abRepo := new(mockAddressBookRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP: config.SMTPConfig{Host: ""},
	}
	uc := NewRegisterUseCase(repo, calRepo, abRepo, emailSvc, cfg)

	ctx := context.Background()
	email := "TEST@Example.Com"
	expectedEmail := "test@example.com"
	password := "SecurePass123!"

	repo.On("GetByEmail", ctx, expectedEmail).Return(nil, nil)
	// Duplicate removed
	repo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	repo.On("Create", ctx, mock.MatchedBy(func(u *user.User) bool {
		return u.Email == expectedEmail
	})).Return(nil)
	calRepo.On("Create", ctx, mock.Anything).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	u, _, err := uc.Execute(ctx, email, password, "Test User")

	assert.NoError(t, err)
	assert.Equal(t, expectedEmail, u.Email)
	repo.AssertExpectations(t)
}

// Registering an address that already has an account: with SMTP the use case
// tells the owner by mail (the handler answers 409, as without SMTP); nothing
// is created and the stored account is left untouched.
func TestRegisterUseCase_Execute_ExistingVerified_WithSMTP_NotifiesOwner(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP:    config.SMTPConfig{Host: "smtp.example.com"},
		BaseURL: "https://cal.example.com",
	}
	uc := NewRegisterUseCase(repo, new(mockCalendarRepo), new(mockAddressBookRepo), emailSvc, cfg)

	ctx := context.Background()
	existing := &user.User{ID: 7, Email: "taken@example.com", DisplayName: "Owner", IsActive: true, EmailVerified: true, PasswordHash: "orig"}
	repo.On("GetByEmail", ctx, "taken@example.com").Return(existing, nil)
	emailSvc.On("SendEmail", ctx, "taken@example.com", mock.Anything, mock.MatchedBy(func(body string) bool {
		return strings.Contains(body, "https://cal.example.com/auth/forgot-password") &&
			strings.Contains(body, "https://cal.example.com/auth/login")
	})).Return(nil)

	u, token, err := uc.Execute(ctx, "Taken@Example.com", "SecurePass123!", "Intruder")

	assert.ErrorIs(t, err, ErrUserAlreadyExists)
	assert.Nil(t, u)
	assert.Empty(t, token)
	assert.Equal(t, "orig", existing.PasswordHash, "the stored password must never change")
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	emailSvc.AssertExpectations(t)
}

// Attacker first (#193): the account of a never-confirmed address carries the
// first registrant's password. When the real owner registers the address, the
// use case must neither touch that password nor mail an activation link —
// following it would confirm the address of an account someone else holds the
// password of. The owner gets the notice, which points at the password reset,
// warns against an activation link received earlier, and does not repeat the
// display name the first registrant chose.
func TestRegisterUseCase_Execute_ExistingUnverified_WithSMTP_NoActivationLink(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP:    config.SMTPConfig{Host: "smtp.example.com"},
		BaseURL: "https://cal.example.com",
	}
	uc := NewRegisterUseCase(repo, new(mockCalendarRepo), new(mockAddressBookRepo), emailSvc, cfg)

	ctx := context.Background()
	existing := &user.User{ID: 9, Email: "pending@example.com", DisplayName: "Visit evil.example", IsActive: true, PasswordHash: "attacker"}
	repo.On("GetByEmail", ctx, "pending@example.com").Return(existing, nil)
	emailSvc.On("SendEmail", ctx, "pending@example.com", mock.Anything, mock.MatchedBy(func(body string) bool {
		return strings.Contains(body, "https://cal.example.com/auth/forgot-password") &&
			strings.Contains(body, "do not open an activation link") &&
			!strings.Contains(body, "/auth/verify") &&
			!strings.Contains(body, existing.DisplayName)
	})).Return(nil)

	_, _, err := uc.Execute(ctx, "pending@example.com", "OwnerPass123!", "Owner")

	assert.ErrorIs(t, err, ErrUserAlreadyExists)
	assert.Equal(t, "attacker", existing.PasswordHash, "the stored password must never change")
	assert.False(t, existing.EmailVerified)
	repo.AssertNotCalled(t, "CreateVerification", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	emailSvc.AssertNotCalled(t, "SendActivationEmail", mock.Anything, mock.Anything, mock.Anything)
	emailSvc.AssertExpectations(t)
}

// Without SMTP the duplicate is reported (the handler answers 409) and no
// mail is attempted.
func TestRegisterUseCase_Execute_Existing_NoSMTP(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	uc := NewRegisterUseCase(repo, new(mockCalendarRepo), new(mockAddressBookRepo), emailSvc, &config.Config{})

	ctx := context.Background()
	repo.On("GetByEmail", ctx, "taken@example.com").Return(&user.User{ID: 1, Email: "taken@example.com", EmailVerified: true}, nil)

	_, _, err := uc.Execute(ctx, "taken@example.com", "SecurePass123!", "x")

	assert.ErrorIs(t, err, ErrUserAlreadyExists)
	emailSvc.AssertNotCalled(t, "SendEmail", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	emailSvc.AssertNotCalled(t, "SendActivationEmail", mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// bcrypt refuses more than 72 bytes. The policy must refuse it first, as the
// typed sentinel the handler answers with a 400, and before the duplicate
// lookup: a taken and a free address then fail identically.
func TestRegisterUseCase_Execute_RejectsPasswordOverBcryptLimit(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{SMTP: config.SMTPConfig{Host: "smtp.example.com"}, BaseURL: "https://cal.example.com"}
	uc := NewRegisterUseCase(repo, new(mockCalendarRepo), new(mockAddressBookRepo), emailSvc, cfg)

	_, _, err := uc.Execute(context.Background(), "long@example.com", "Aa1!"+strings.Repeat("x", 69), "Long")

	assert.ErrorIs(t, err, user.ErrPasswordTooLong)
	repo.AssertNotCalled(t, "GetByEmail", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	emailSvc.AssertNotCalled(t, "SendActivationEmail", mock.Anything, mock.Anything, mock.Anything)
}
