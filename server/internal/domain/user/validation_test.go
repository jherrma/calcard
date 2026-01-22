package user

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"user@example.com", true},
		{"user.name+alias@example.co.uk", true},
		{"invalid-email", false},
		{"@example.com", false},
		{"user@", false},
		{"", false},
	}

	for _, tt := range tests {
		err := ValidateEmail(tt.email)
		if tt.valid {
			assert.NoError(t, err, "Email: %s", tt.email)
		} else {
			assert.Error(t, err, "Email: %s", tt.email)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		password string
		valid    bool
		err      error
	}{
		{"SecurePass123!", true, nil},
		{"short", false, ErrPasswordTooShort},
		{"noupper123!", false, ErrPasswordNoUpper},
		{"NOLOWER123!", false, ErrPasswordNoLower},
		{"NoDigit!", false, ErrPasswordNoDigit},
		{"NoSpecial123", false, ErrPasswordNoSpecial},
	}

	for _, tt := range tests {
		err := ValidatePassword(tt.password)
		if tt.valid {
			assert.NoError(t, err, "Password: %s", tt.password)
		} else {
			assert.ErrorIs(t, err, tt.err, "Password: %s", tt.password)
		}
	}
}
