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
	"golang.org/x/crypto/bcrypt"
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
		return u.Email == email && len(u.Username) == 16 && u.IsActive == false && u.EmailVerified == false
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

// P01: registering an address that already has an account must not be
// observable from the response. With SMTP the use case tells the owner by
// mail (and the handler answers as for a fresh registration); nothing is
// created and the stored account is left untouched.
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

// A pending (never confirmed) account gets a fresh activation link — the
// usual reason to register twice is a lost activation mail — but its password
// is NOT replaced: the caller has not proven they own the mailbox.
func TestRegisterUseCase_Execute_ExistingUnverified_WithSMTP_ResendsActivation(t *testing.T) {
	repo := new(mockUserRepo)
	emailSvc := new(mockEmailService)
	cfg := &config.Config{
		SMTP:    config.SMTPConfig{Host: "smtp.example.com"},
		BaseURL: "https://cal.example.com",
	}
	uc := NewRegisterUseCase(repo, new(mockCalendarRepo), new(mockAddressBookRepo), emailSvc, cfg)

	ctx := context.Background()
	existing := &user.User{ID: 9, Email: "pending@example.com", PasswordHash: "orig"}
	repo.On("GetByEmail", ctx, "pending@example.com").Return(existing, nil)
	var stored *user.EmailVerification
	repo.On("CreateVerification", ctx, mock.MatchedBy(func(v *user.EmailVerification) bool {
		return v.UserID == 9 && len(v.Token) == 64
	})).Run(func(args mock.Arguments) { stored = args.Get(1).(*user.EmailVerification) }).Return(nil)
	// The SPA page, like the first activation mail.
	const prefix = "https://cal.example.com/auth/verify?token="
	var link string
	emailSvc.On("SendActivationEmail", ctx, "pending@example.com", mock.MatchedBy(func(l string) bool {
		return strings.HasPrefix(l, prefix)
	})).Run(func(args mock.Arguments) { link = args.String(2) }).Return(nil)

	_, _, err := uc.Execute(ctx, "pending@example.com", "OtherPass123!", "Someone")

	assert.ErrorIs(t, err, ErrUserAlreadyExists)
	require.NotNil(t, stored)
	assert.Equal(t, stored.Token, hashVerificationToken(strings.TrimPrefix(link, prefix)),
		"the mailed token is the one whose hash was stored")
	assert.Equal(t, "orig", existing.PasswordHash)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
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

// The taken-address path must cost a bcrypt hash at the production work
// factor, like a real registration does; otherwise the response time alone
// tells a taken address (fast) from a free one (slow). Measured against one
// reference hash and asserted at half of it, which leaves a wide margin either
// way: without the dummy hash this path takes microseconds.
func TestRegisterUseCase_Execute_Existing_SpendsBcryptTime(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewRegisterUseCase(repo, new(mockCalendarRepo), new(mockAddressBookRepo), new(mockEmailService), &config.Config{})
	ctx := context.Background()
	repo.On("GetByEmail", ctx, "taken@example.com").Return(&user.User{ID: 1, Email: "taken@example.com", EmailVerified: true}, nil)

	start := time.Now()
	_, err := bcrypt.GenerateFromPassword([]byte("SecurePass123!"), user.BcryptCost)
	require.NoError(t, err)
	reference := time.Since(start)

	start = time.Now()
	_, _, err = uc.Execute(ctx, "taken@example.com", "SecurePass123!", "x")
	elapsed := time.Since(start)

	require.ErrorIs(t, err, ErrUserAlreadyExists)
	assert.GreaterOrEqualf(t, elapsed, reference/2,
		"taken-address path took %v, a bcrypt hash takes %v: it must hash too (P01 timing)", elapsed, reference)
}

// bcrypt refuses more than 72 bytes. The policy must refuse it first, as the
// typed sentinel the handler answers with a 400, and before the duplicate
// lookup: a taken and a free address then fail identically (P01).
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
