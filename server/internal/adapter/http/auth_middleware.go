package http

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// Authenticate returns a Fiber middleware that validates JWT tokens
func Authenticate(jwtManager user.TokenProvider) fiber.Handler {
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

		// Store user info in context
		c.Locals("user_uuid", userUUID)
		c.Locals("user_email", email)

		return c.Next()
	}
}
