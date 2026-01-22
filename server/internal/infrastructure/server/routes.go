package server

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/infrastructure/email"
	"github.com/jherrma/caldav-server/internal/usecase/auth"
)

// SetupRoutes registers all application routes
func SetupRoutes(app *fiber.App, db database.Database, cfg *config.Config) {
	healthHandler := http.NewHealthHandler(db)

	health := app.Group("/health")
	health.Get("/", healthHandler.Liveness)
	health.Get("/ready", healthHandler.Readiness)

	// User Auth
	userRepo := repository.NewUserRepository(db.DB())
	emailService := email.NewEmailService(cfg.SMTP)
	registerUC := auth.NewRegisterUseCase(userRepo, emailService, cfg)
	verifyUC := auth.NewVerifyUseCase(userRepo)
	authHandler := http.NewAuthHandler(registerUC, verifyUC)

	api := app.Group("/api/v1")
	api.Post("/auth/register", authHandler.Register)
	api.Get("/auth/verify", authHandler.Verify)
}
