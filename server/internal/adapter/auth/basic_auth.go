package authadapter

import (
	"encoding/base64"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// BasicAuthMiddleware returns a Fiber middleware for HTTP Basic Authentication
func BasicAuthMiddleware(userRepo user.UserRepository, appPasswordRepo user.AppPasswordRepository) fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			c.Set("WWW-Authenticate", `Basic realm="CalCard DAV"`)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Authentication required"})
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "basic" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid auth header"})
		}

		payload, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid auth payload"})
		}

		pair := strings.SplitN(string(payload), ":", 2)
		if len(pair) != 2 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid auth format"})
		}

		username, password := pair[0], pair[1]

		// Find user by username
		u, err := userRepo.GetByUsername(c.Context(), username)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
		}
		if u == nil {
			// Also try email if username fails? Story says "not email", so I'll stick to that.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
		}

		// Verify app password
		ap, err := appPasswordRepo.FindValidForUser(c.Context(), u.ID, password)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
		}
		if ap == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
		}

		// Check scope based on path
		path := strings.ToLower(c.Path())
		requiredScope := ""
		if strings.Contains(path, "/caldav") || strings.Contains(path, "/calendar") {
			requiredScope = "caldav"
		} else if strings.Contains(path, "/carddav") || strings.Contains(path, "/addressbook") {
			requiredScope = "carddav"
		}

		if requiredScope != "" && !ap.HasScope(requiredScope) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":   "forbidden",
				"message": "App password does not have access to " + requiredScope,
			})
		}

		c.Locals("user_uuid", u.UUID)
		c.Locals("user_id", u.ID)
		c.Locals("auth_method", "app_password")

		return c.Next()
	}
}
