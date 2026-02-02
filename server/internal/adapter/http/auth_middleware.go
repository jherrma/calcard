package http

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// Authenticate returns a Fiber middleware that validates JWT tokens
func Authenticate(jwtManager user.TokenProvider, userRepo user.UserRepository) fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return UnauthorizedResponse(c, "missing authentication token")
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return UnauthorizedResponse(c, "invalid authentication header format")
		}

		userUUID, email, err := jwtManager.ValidateAccessToken(parts[1])
		if err != nil {
			if err == authadapter.ErrExpiredToken {
				return UnauthorizedResponse(c, "token expired")
			}
			return UnauthorizedResponse(c, "invalid or expired token")
		}

		// Look up user to get uint ID
		u, err := userRepo.GetByUUID(c.Context(), userUUID)
		if err != nil {
			return UnauthorizedResponse(c, "user not found")
		}

		// Store user info in context
		c.Locals("user_uuid", userUUID)
		c.Locals("user_email", email)
		c.Locals("user_id", u.ID)
		c.Locals("user", u)

		return c.Next()
	}
}

// GetUserIDFromContext retrieves the user ID from the fiber context
func GetUserIDFromContext(c fiber.Ctx) (uint, error) {
	userID, ok := c.Locals("user_id").(uint)
	if !ok {
		return 0, fmt.Errorf("user id not found in context")
	}
	return userID, nil
}
