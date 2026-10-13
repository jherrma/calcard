package user

import (
	"strings"
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

func TestValidateDAVCredentialUsername(t *testing.T) {
	tests := []struct {
		username string
		err      error
	}{
		{"abc", nil},
		{"qzmwtbrhxkfepvan", nil}, // what the SPA generates: 16 letters
		{strings.Repeat("z", MaxDAVCredentialUsernameLength), nil},
		{"", ErrDAVCredentialUsernameLength},
		{"ab", ErrDAVCredentialUsernameLength},
		{strings.Repeat("a", MaxDAVCredentialUsernameLength+1), ErrDAVCredentialUsernameLength},
		// Upper case is refused, never folded into lower case.
		{"Thunderbird", ErrDAVCredentialUsernameCharset},
		{"ABC", ErrDAVCredentialUsernameCharset},
		{"phone2", ErrDAVCredentialUsernameCharset},
		{"my-phone", ErrDAVCredentialUsernameCharset},
		{"my_phone", ErrDAVCredentialUsernameCharset},
		{"my phone", ErrDAVCredentialUsernameCharset},
		{"phone.", ErrDAVCredentialUsernameCharset},
		{"jürgen", ErrDAVCredentialUsernameCharset},
		{"ab\x00", ErrDAVCredentialUsernameCharset},
	}
	for _, tt := range tests {
		err := ValidateDAVCredentialUsername(tt.username)
		if tt.err == nil {
			assert.NoError(t, err, "username %q", tt.username)
		} else {
			assert.ErrorIs(t, err, tt.err, "username %q", tt.username)
		}
	}
}

func TestNormalizeCredentialName(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Phone", "Phone", true},
		{"  Phone \t", "Phone", true},
		{"", "", false},
		{"   ", "", false},
		{strings.Repeat("x", MaxCredentialNameLength), strings.Repeat("x", MaxCredentialNameLength), true},
		{strings.Repeat("x", MaxCredentialNameLength+1), "", false},
		// The limit counts characters (like PostgreSQL varchar), not bytes.
		{strings.Repeat("ä", MaxCredentialNameLength), strings.Repeat("ä", MaxCredentialNameLength), true},
	}
	for _, tt := range tests {
		got, ok := NormalizeCredentialName(tt.in)
		assert.Equal(t, tt.ok, ok, "input %q", tt.in)
		if tt.ok {
			assert.Equal(t, tt.want, got)
		}
	}
}
