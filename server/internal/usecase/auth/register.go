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

// ErrUserAlreadyExists reports that the address already has an account. The
// HTTP layer answers it with a 409, SMTP or not: a new account is usable at
// once, so a caller who registered an address and then logs in with the
// password they chose would learn the outcome anyway. With SMTP the address
// owner is also told about the attempt by e-mail (notifyExistingAccount).
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

	// 4. Create user. The account is active (usable) at once, SMTP or not.
	// With SMTP its address still has to be confirmed: EmailVerified stays
	// false until the activation link is opened, and login does not wait for
	// it. Without SMTP nothing can be confirmed, so it is verified right away.
	u := &user.User{
		UUID:          uuid.New().String(),
		Email:         email,
		Username:      username,
		PasswordHash:  string(hash),
		DisplayName:   displayName,
		IsActive:      true,
		EmailVerified: uc.cfg.SMTP.Host == "",
	}

	// 5. With SMTP, an activation token for the e-mail confirmation
	var token string
	if uc.cfg.SMTP.Host != "" {
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
				// already created and can use the account. A password reset
				// confirms the address too; registering again mails no new
				// link (notifyExistingAccount).
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
// someone tried to register it. It only runs with SMTP configured; the caller
// gets the 409 either way. The account is never changed.
//
// It never mails an activation link, not even for an account whose address was
// never confirmed (#193). Such an account carries the password of whoever
// registered first, who need not own the mailbox: a fresh link handed to the
// real owner, who has just tried to register and expects one, would have them
// confirm the address of an account someone else holds the password of — and a
// confirmed address lets the owner's OAuth sign-in be linked into it. The
// notice sends them to the password reset instead, which proves the mailbox,
// sets their password, confirms the address and removes every session and
// credential of the first registrant.
//
// Failures are logged, never returned: the caller's answer is the 409 whether
// or not the mail went out.
func (uc *RegisterUseCase) notifyExistingAccount(ctx context.Context, u *user.User) {
	if uc.cfg.SMTP.Host == "" {
		return
	}

	subject := "Someone tried to register with your e-mail address"
	if !u.EmailVerified {
		// No display name: on an unconfirmed account it is text the first
		// registrant chose.
		body := fmt.Sprintf(`Hello,

Someone just tried to create a new CalDAV Server account with this e-mail
address. An account with this address already exists, but the address was
never confirmed. Nothing was changed.

If you created that account, sign in here:

%s/auth/login

If you did not create it, or forgot its password, reset the password here.
That confirms the address as yours and signs out everyone else who used the
account, including whoever created it:

%s/auth/forgot-password

Unless you created the account yourself, do not open an activation link you
may have received for it earlier: it would confirm an account whose password
someone else chose.

- CalDAV Server`, uc.cfg.BaseURL, uc.cfg.BaseURL)
		if err := uc.emailService.SendEmail(ctx, u.Email, subject, body); err != nil {
			log.Printf("failed to send duplicate-registration notice to user %d: %v", u.ID, err)
		}
		return
	}

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
