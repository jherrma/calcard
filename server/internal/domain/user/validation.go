package user

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxCredentialNameLength is the longest display name an app password, DAV
// credential or MCP token may carry: the `size:100` of their name columns.
// PostgreSQL counts varchar(n) in characters, so this is a character count.
const MaxCredentialNameLength = 100

// NormalizeCredentialName trims a credential's display name and reports
// whether the result is usable: not empty and at most MaxCredentialNameLength
// characters. Callers store the trimmed name.
func NormalizeCredentialName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxCredentialNameLength {
		return name, false
	}
	return name, true
}

// Bounds of a CalDAV/CardDAV credential username. The SPA generates 16
// characters; people may still type their own.
const (
	MinDAVCredentialUsernameLength = 3
	MaxDAVCredentialUsernameLength = 50
)

var (
	ErrDAVCredentialUsernameLength = fmt.Errorf("username must be between %d and %d characters",
		MinDAVCredentialUsernameLength, MaxDAVCredentialUsernameLength)
	ErrDAVCredentialUsernameCharset = errors.New("username may only contain lowercase letters a-z")
)

// ValidateDAVCredentialUsername is the rule for the username of a NEW CalDAV
// or CardDAV credential: 3 to 50 lowercase ASCII letters a-z, nothing else.
// Upper case is refused, not folded: the caller keeps exactly the name it
// sent, and it cannot be typed into a DAV client in a different case than the
// one stored (CalDAV credential lookups are exact, CardDAV ones are not, F02).
// Only creation checks it; credentials stored under the older, wider rule
// ([a-zA-Z0-9_-]) keep working unchanged.
func ValidateDAVCredentialUsername(username string) error {
	if len(username) < MinDAVCredentialUsernameLength || len(username) > MaxDAVCredentialUsernameLength {
		return ErrDAVCredentialUsernameLength
	}
	for i := 0; i < len(username); i++ {
		if c := username[i]; c < 'a' || c > 'z' {
			return ErrDAVCredentialUsernameCharset
		}
	}
	return nil
}

// BcryptCost is the bcrypt work factor used whenever a user password is
// (re)hashed. Every site that hashes a password — registration, reset, and
// change — must use this constant so the cost cannot drift between flows and
// leave the newer hash of an account weaker than the original.
const BcryptCost = 12

// MaxPasswordBytes is the longest password bcrypt can hash. bcrypt reads at
// most 72 bytes of its input, and golang.org/x/crypto refuses anything longer
// (bcrypt.ErrPasswordTooLong) instead of truncating it, so every site that
// hashes a password chosen by a person must refuse it first — as a 400 with
// ErrPasswordTooLong, not as a failed hash. Bytes, not characters: "ä" is two.
// Only a NEW password is checked; login compares and never applies it.
const MaxPasswordBytes = 72

var (
	ErrInvalidEmail      = errors.New("invalid email format")
	ErrPasswordTooShort  = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong   = fmt.Errorf("password must be at most %d bytes", MaxPasswordBytes)
	ErrPasswordNoUpper   = errors.New("password must contain at least one uppercase letter")
	ErrPasswordNoLower   = errors.New("password must contain at least one lowercase letter")
	ErrPasswordNoDigit   = errors.New("password must contain at least one digit")
	ErrPasswordNoSpecial = errors.New("password must contain at least one special character")
)

// ValidateEmail checks if the email format is valid
func ValidateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return ErrInvalidEmail
	}
	return nil
}

// ValidatePassword checks a NEW password against the policy: 8 to
// MaxPasswordBytes bytes, with an upper-case letter, a lower-case letter, a
// digit and a punctuation or symbol character (Unicode classes, so "Ü" is
// upper case and a space is not special). The SPA mirrors it in
// webinterface/utils/passwordPolicy.ts; keep both in step.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if !hasUpper {
		return ErrPasswordNoUpper
	}
	if !hasLower {
		return ErrPasswordNoLower
	}
	if !hasDigit {
		return ErrPasswordNoDigit
	}
	if !hasSpecial {
		return ErrPasswordNoSpecial
	}

	return nil
}
