package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

// ErrUserAlreadyExists reports that the address already has an account. With
// SMTP configured the HTTP layer answers it exactly like a successful
// registration (see AuthHandler.Register), so the address owner learns about
// the attempt by e-mail and the caller learns nothing (P01). Without SMTP the
// account would be usable immediately, so the outcome cannot be hidden and the
// caller gets a 409.
var ErrUserAlreadyExists = errors.New("user already exists")

type RegisterUseCase struct {
	repo            user.UserRepository
	calendarRepo    calendar.CalendarRepository
	addressBookRepo addressbook.Repository
	emailService    EmailService
	cfg             *config.Config
}

func NewRegisterUseCase(repo user.UserRepository, calendarRepo calendar.CalendarRepository, addressBookRepo addressbook.Repository, emailService EmailService, cfg *config.Config) *RegisterUseCase {
	return &RegisterUseCase{
		repo:            repo,
		calendarRepo:    calendarRepo,
		addressBookRepo: addressBookRepo,
		emailService:    emailService,
		cfg:             cfg,
	}
}

func (uc *RegisterUseCase) Execute(ctx context.Context, email, password, displayName string) (*user.User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	// 0. With a setup token configured, the first account must come from setup.
	if pending, err := setupPending(ctx, uc.repo, uc.cfg.Registration.SetupToken); err != nil {
		return nil, "", err
	} else if pending {
		return nil, "", ErrSetupRequired
	}

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
		// Spend the same bcrypt work a real registration does, so the response
		// time does not tell a taken address from a free one (P01).
		_, _ = bcrypt.GenerateFromPassword([]byte(password), user.BcryptCost)
		uc.notifyExistingAccount(ctx, existing)
		return nil, "", ErrUserAlreadyExists
	}
	// 2b. Generate unique username
	username, err := GenerateUniqueUsername(ctx, uc.repo)
	if err != nil {
		return nil, "", err
	}

	// 3. Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), user.BcryptCost)
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

	// 6. Create verification record if needed. Store only the hash; the raw
	// token goes out in the email link.
	if token != "" {
		v := &user.EmailVerification{
			UserID:    u.ID,
			Token:     hashVerificationToken(token),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		if err := uc.repo.CreateVerification(ctx, v); err != nil {
			return nil, "", err
		}

		// Send email if SMTP is configured
		if uc.cfg.SMTP.Host != "" {
			link := activationLink(uc.cfg.BaseURL, token)
			if err := uc.emailService.SendActivationEmail(ctx, u.Email, link); err != nil {
				// Log the failure without the token-bearing link; the user is
				// already created and can request a resend.
				log.Printf("failed to send activation email to user %d: %v", u.ID, err)
			}
		} else {
			// SMTP not configured: don't print the token-bearing link to logs.
			log.Printf("activation email suppressed (SMTP not configured) for user %d", u.ID)
		}
	}

	// Create default calendar
	if err := uc.createDefaultCalendar(ctx, u.ID); err != nil {
		// Log error but don't fail registration
		fmt.Printf("failed to create default calendar: %v\n", err)
	}

	// Create default address book
	if err := uc.createDefaultAddressBook(ctx, u.ID); err != nil {
		// Log error but don't fail registration
		fmt.Printf("failed to create default address book: %v\n", err)
	}

	return u, token, nil
}

// activationLink is the address an activation e-mail points at: the SPA page
// /auth/verify, which sends the token to GET /api/v1/auth/verify and shows the
// outcome — like the reset link (/auth/reset-password?token=). Linking the API
// directly left people on a raw JSON response. The token is hex, so it needs
// no escaping. The API endpoint is unchanged, so links mailed before still
// work. baseURL is cfg.BaseURL, never a request's Host (link poisoning).
func activationLink(baseURL, token string) string {
	return fmt.Sprintf("%s/auth/verify?token=%s", baseURL, token)
}

// notifyExistingAccount tells the owner of an already-registered address that
// someone tried to register it. It only runs with SMTP configured: that is the
// mode in which the HTTP response hides the duplicate, so the e-mail is the
// only place the outcome is reported — to the one person entitled to it.
//
// An account that never confirmed its address gets a fresh activation link
// instead (the usual reason to register twice is a lost activation mail). Its
// password is NOT changed: whoever submitted this form has not proven they own
// the mailbox, and replacing the password of a pending account would let them
// take it over the moment the real owner clicks the link.
//
// Failures are logged, never returned: an error here would surface as a
// different response and re-open the oracle this path exists to close.
func (uc *RegisterUseCase) notifyExistingAccount(ctx context.Context, u *user.User) {
	if uc.cfg.SMTP.Host == "" {
		return
	}

	if !u.EmailVerified {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Printf("register: failed to generate activation token for user %d: %v", u.ID, err)
			return
		}
		token := hex.EncodeToString(b)
		v := &user.EmailVerification{
			UserID:    u.ID,
			Token:     hashVerificationToken(token),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		if err := uc.repo.CreateVerification(ctx, v); err != nil {
			log.Printf("register: failed to store activation token for user %d: %v", u.ID, err)
			return
		}
		link := activationLink(uc.cfg.BaseURL, token)
		if err := uc.emailService.SendActivationEmail(ctx, u.Email, link); err != nil {
			log.Printf("failed to re-send activation email to user %d: %v", u.ID, err)
		}
		return
	}

	subject := "Someone tried to register with your e-mail address"
	body := fmt.Sprintf(`Hi %s,

Someone just tried to create a new CalDAV Server account with this e-mail
address. You already have an account, so nothing was changed.

If that was you, sign in here:

%s/auth/login

Forgot your password? Reset it here:

%s/auth/forgot-password

If it wasn't you, you can safely ignore this email.

- CalDAV Server`, u.DisplayName, uc.cfg.BaseURL, uc.cfg.BaseURL)
	if err := uc.emailService.SendEmail(ctx, u.Email, subject, body); err != nil {
		log.Printf("failed to send duplicate-registration notice to user %d: %v", u.ID, err)
	}
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

func (uc *RegisterUseCase) createDefaultAddressBook(ctx context.Context, userID uint) error {
	abUUID := uuid.New().String()

	defaultAB := &addressbook.AddressBook{
		UserID:    userID,
		UUID:      abUUID,
		Name:      "Contacts",
		Path:      abUUID, // Usually UUID
		SyncToken: addressbook.GenerateSyncToken(),
		CTag:      addressbook.GenerateCTag(),
	}

	return uc.addressBookRepo.Create(ctx, defaultAB)
}
