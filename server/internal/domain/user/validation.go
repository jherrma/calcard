package user

import (
	"errors"
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

// BcryptCost is the bcrypt work factor used whenever a user password is
// (re)hashed. Every site that hashes a password — registration, reset, and
// change — must use this constant so the cost cannot drift between flows and
// leave the newer hash of an account weaker than the original.
const BcryptCost = 12

var (
	ErrInvalidEmail      = errors.New("invalid email format")
	ErrPasswordTooShort  = errors.New("password must be at least 8 characters")
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

// ValidatePassword checks if the password meets complexity requirements
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
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
