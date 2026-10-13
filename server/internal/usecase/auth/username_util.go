package auth

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// GenerateUniqueUsername generates a random 16-character username and ensures it's unique
// by checking against the provided user repository. It will retry up to maxRetries times
// if a collision occurs.
//
// The username is lowercase letters only, like the set-up admin's and every new
// DAV credential's, so it is stored exactly as a client that lower-cases it
// would send it. Accounts registered before this kept their mixed-case names
// (they are the /dav/{username}/ URL segment and are never rewritten).
func GenerateUniqueUsername(ctx context.Context, userRepo user.UserRepository) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyz"
	const length = 16
	const maxRetries = 10

	for i := 0; i < maxRetries; i++ {
		b := make([]byte, length)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("failed to generate random bytes: %w", err)
		}
		for i := range b {
			b[i] = chars[b[i]%byte(len(chars))]
		}
		username := string(b)

		existing, err := userRepo.GetByUsername(ctx, username)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return username, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique username after max retries")
}
