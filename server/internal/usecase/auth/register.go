package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

var ErrUserAlreadyExists = errors.New("user already exists")

type RegisterUseCase struct {
	repo         user.UserRepository
	calendarRepo calendar.CalendarRepository
	emailService EmailService
	cfg          *config.Config
}

func NewRegisterUseCase(repo user.UserRepository, calendarRepo calendar.CalendarRepository, emailService EmailService, cfg *config.Config) *RegisterUseCase {
	return &RegisterUseCase{
		repo:         repo,
		calendarRepo: calendarRepo,
		emailService: emailService,
		cfg:          cfg,
	}
}

func (uc *RegisterUseCase) Execute(ctx context.Context, email, password, displayName string) (*user.User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	// 1. Validate domain rules
	if err := user.ValidateEmail(email); err != nil {
		return nil, "", err
	}
	if err := user.ValidatePassword(password); err != nil {
		return nil, "", err
	}

	// 2. Check for duplicate email
	existing, err := uc.repo.GetByEmail(ctx, email)
	if err != nil {
		return nil, "", err
	}
	if existing != nil {
		return nil, "", ErrUserAlreadyExists
	}

	if existing != nil {
		return nil, "", ErrUserAlreadyExists
	}

	// 2b. Generate unique username
	username, err := GenerateUniqueUsername(ctx, uc.repo)
	if err != nil {
		return nil, "", err
	}

	// 3. Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, "", fmt.Errorf("failed to hash password: %w", err)
	}

	// 4. Create user
	u := &user.User{
		UUID:         uuid.New().String(),
		Email:        email,
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  displayName,
	}

	// 5. Conditional activation based on SMTP host
	var token string
	if uc.cfg.SMTP.Host == "" {
		u.IsActive = true
		u.EmailVerified = true
	} else {
		u.IsActive = false
		u.EmailVerified = false

		// Generate verification token
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, "", fmt.Errorf("failed to generate token: %w", err)
		}
		token = hex.EncodeToString(b)
	}

	if err := uc.repo.Create(ctx, u); err != nil {
		return nil, "", err
	}

	// 6. Create verification record if needed
	if token != "" {
		v := &user.EmailVerification{
			UserID:    u.ID,
			Token:     token,
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		if err := uc.repo.CreateVerification(ctx, v); err != nil {
			return nil, "", err
		}

		// Send email if SMTP is configured
		if uc.cfg.SMTP.Host != "" {
			link := fmt.Sprintf("%s/api/v1/auth/verify?token=%s", uc.cfg.BaseURL, token)
			if err := uc.emailService.SendActivationEmail(ctx, u.Email, link); err != nil {
				// We log the error but don't fail registration, as the user is already created
				// and the token is in the DB. They might be able to retry or get support.
				// In a real app, you might want to handle this differently. TODO
				fmt.Printf("failed to send activation email: %v\n", err)
			}
		} else {
			// Log the link (mock email)
			fmt.Printf("[MOCK EMAIL] Verification link: %s/api/v1/auth/verify?token=%s\n", uc.cfg.BaseURL, token)
		}
	}

	// Create default calendar
	if err := uc.createDefaultCalendar(ctx, u.ID); err != nil {
		// Log error but don't fail registration
		fmt.Printf("failed to create default calendar: %v\n", err)
	}

	return u, token, nil
}

// createDefaultCalendar creates a default "Personal" calendar for a new user
func (uc *RegisterUseCase) createDefaultCalendar(ctx context.Context, userID uint) error {
	calUUID := uuid.New().String()
	path := fmt.Sprintf("%s.ics", calUUID)

	defaultCal := &calendar.Calendar{
		UUID:                calUUID,
		UserID:              userID,
		Path:                path,
		Name:                "Personal",
		Description:         "",
		Color:               "#3788d8", // Blue
		Timezone:            "UTC",
		SupportedComponents: "VEVENT,VTODO",
		SyncToken:           calendar.GenerateSyncToken(),
		CTag:                calendar.GenerateCTag(),
	}

	return uc.calendarRepo.Create(ctx, defaultCal)
}

func (uc *RegisterUseCase) generateUniqueUsername(ctx context.Context) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	length := 16
	maxRetries := 10

	for i := 0; i < maxRetries; i++ {
		b := make([]byte, length)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("failed to generate random bytes: %w", err)
		}
		for i := range b {
			b[i] = chars[b[i]%byte(len(chars))]
		}
		username := string(b)

		existing, err := uc.repo.GetByUsername(ctx, username)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return username, nil
		}
	}
	return "", errors.New("failed to generate unique username after max retries")
}
