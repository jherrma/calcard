package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/jherrma/caldav-server/internal/config"
)

// CORSMiddleware creates a new CORS middleware handler
func CORSMiddleware(cfg config.CORSConfig) fiber.Handler {
	if !cfg.Enabled {
		return func(c fiber.Ctx) error {
			return c.Next()
		}
	}

	return cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "PROPFIND", "PROPPATCH", "REPORT", "MKCOL", "MOVE", "COPY", "LOCK", "UNLOCK"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "Brief", "Depth", "If-Match", "If-None-Match", "If-Schedule-Tag-Match"},
		ExposeHeaders:    cfg.ExposeHeaders,
		AllowCredentials: cfg.AllowCredentials,
		MaxAge:           cfg.MaxAge,
	})
}
