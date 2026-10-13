package user

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
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

// bcrypt hashes at most 72 bytes and refuses longer input, so the policy has a
// maximum, counted in bytes like the minimum.
func TestValidatePassword_MaxBytes(t *testing.T) {
	const prefix = "Aa1!" // meets every class rule in 4 bytes
	tests := []struct {
		name     string
		password string
		err      error
	}{
		{"exactly 72 bytes", prefix + strings.Repeat("x", 68), nil},
		{"73 bytes", prefix + strings.Repeat("x", 69), ErrPasswordTooLong},
		// 4 + 34×2 = 72 bytes in 38 characters: allowed.
		{"72 bytes of mostly two-byte characters", prefix + strings.Repeat("ä", 34), nil},
		// 4 + 35×2 = 74 bytes in only 39 characters: refused.
		{"39 characters but 74 bytes", prefix + strings.Repeat("ä", 35), ErrPasswordTooLong},
		// 4 + 23×3 = 73 bytes in 27 characters.
		{"27 characters but 73 bytes", prefix + strings.Repeat("€", 23), ErrPasswordTooLong},
		{"far too long", prefix + strings.Repeat("x", 500), ErrPasswordTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if tt.err == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.err)
			}
		})
	}
	assert.Equal(t, "password must be at most 72 bytes", ErrPasswordTooLong.Error())
}

// The limit is bcrypt's, not a choice: whatever ValidatePassword accepts must
// hash, and the first byte it refuses must be one bcrypt refuses too.
func TestMaxPasswordBytesMatchesBcrypt(t *testing.T) {
	_, err := bcrypt.GenerateFromPassword([]byte(strings.Repeat("x", MaxPasswordBytes)), bcrypt.MinCost)
	assert.NoError(t, err)
	_, err = bcrypt.GenerateFromPassword([]byte(strings.Repeat("x", MaxPasswordBytes+1)), bcrypt.MinCost)
	assert.ErrorIs(t, err, bcrypt.ErrPasswordTooLong)
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
