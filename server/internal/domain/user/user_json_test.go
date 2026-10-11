package user

import (
	"encoding/json"
	"strings"
	"testing"
)

// SCOPE: a User (and an AppPassword) can end up embedded in an API response —
// a preloaded owner on a shared address book was exactly that (F01). Password
// hashes must never serialise, whatever struct happens to embed them.
func TestCredentialHashesAreNeverSerialised(t *testing.T) {
	cases := map[string]any{
		"User":            User{Email: "a@example.com", PasswordHash: "$2a$12$secrethashvalue"},
		"AppPassword":     AppPassword{Name: "dav", PasswordHash: "$2a$12$secrethashvalue"},
		"OAuthConnection": OAuthConnection{Provider: "google", AccessToken: "secrethashvalue", RefreshToken: "secrethashvalue"},
		"RefreshToken":    RefreshToken{TokenHash: "secrethashvalue", ReplacedByHash: "secrethashvalue"},
		"PasswordReset":   PasswordReset{TokenHash: "secrethashvalue"},
		"EmailVerif":      EmailVerification{Token: "secrethashvalue"},
	}
	for name, v := range cases {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if strings.Contains(strings.ToLower(string(b)), "secrethashvalue") ||
			strings.Contains(strings.ToLower(string(b)), "passwordhash") {
			t.Errorf("%s serialised its password hash: %s", name, b)
		}
	}
}
