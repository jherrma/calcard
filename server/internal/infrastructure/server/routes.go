package server

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/infrastructure/email"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

// SetupRoutes registers all application routes
func SetupRoutes(app *fiber.App, db database.Database, cfg *config.Config) {
	// Repositories
	userRepo := repository.NewUserRepository(db.DB())
	tokenRepo := repository.NewRefreshTokenRepository(db.DB())
	systemRepo := repository.NewSystemSettingRepository(db.DB())

	// Services
	emailService := email.NewEmailService(cfg.SMTP)
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// Ensure JWT Secret
	if err := jwtManager.EnsureSecret(context.Background(), systemRepo); err != nil {
		fmt.Printf("failed to ensure JWT secret: %v\n", err)
	}

	// Use Cases
	registerUC := authusecase.NewRegisterUseCase(userRepo, emailService, cfg)
	verifyUC := authusecase.NewVerifyUseCase(userRepo)
	loginUC := authusecase.NewLoginUseCase(userRepo, tokenRepo, jwtManager, cfg)
	refreshUC := authusecase.NewRefreshUseCase(tokenRepo, jwtManager)
	logoutUC := authusecase.NewLogoutUseCase(tokenRepo, jwtManager)

	// Handlers
	authHandler := http.NewAuthHandler(registerUC, verifyUC, loginUC, refreshUC, logoutUC)
	healthHandler := http.NewHealthHandler(db)

	// Public Routes
	app.Get("/health", healthHandler.Liveness)
	app.Get("/ready", healthHandler.Readiness)

	// API Group
	v1 := app.Group("/api/v1")

	// Auth Routes
	authGroup := v1.Group("/auth")
	authGroup.Post("/register", authHandler.Register)
	authGroup.Get("/verify", authHandler.Verify)

	// Login with rate limiting
	loginIPLimiter := http.NewIPRateLimiter(5, time.Minute)
	loginEmailLimiter := http.NewEmailRateLimiter(10, time.Minute)
	authGroup.Post("/login", http.ExtractEmailMiddleware(), loginIPLimiter, loginEmailLimiter, authHandler.Login)

	authGroup.Post("/refresh", authHandler.Refresh)
	authGroup.Post("/logout", authHandler.Logout)
}
