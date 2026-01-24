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
	"github.com/jherrma/caldav-server/internal/usecase/apppassword"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
	userusecase "github.com/jherrma/caldav-server/internal/usecase/user"
)

// SetupRoutes registers all application routes
func SetupRoutes(app *fiber.App, db database.Database, cfg *config.Config) {
	// Repositories
	userRepo := repository.NewUserRepository(db.DB())
	tokenRepo := repository.NewRefreshTokenRepository(db.DB())
	systemRepo := repository.NewSystemSettingRepository(db.DB())
	resetRepo := repository.NewGORMPasswordResetRepository(db.DB())
	appPwdRepo := repository.NewAppPasswordRepository(db.DB())

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
	changePasswordUC := authusecase.NewChangePasswordUseCase(userRepo, tokenRepo, jwtManager)
	forgotPasswordUC := authusecase.NewForgotPasswordUseCase(userRepo, resetRepo, emailService, cfg.JWT.ResetExpiry)
	resetPasswordUC := authusecase.NewResetPasswordUseCase(userRepo, resetRepo, tokenRepo)

	// User Use Cases
	getProfileUC := userusecase.NewGetProfileUseCase(userRepo)
	updateProfileUC := userusecase.NewUpdateProfileUseCase(userRepo)
	deleteAccountUC := userusecase.NewDeleteAccountUseCase(userRepo)

	// App Password Use Cases
	createAppPwdUC := apppassword.NewCreateUseCase(userRepo, appPwdRepo)
	listAppPwdUC := apppassword.NewListUseCase(appPwdRepo)
	revokeAppPwdUC := apppassword.NewRevokeUseCase(appPwdRepo)

	// Handlers
	authHandler := http.NewAuthHandler(registerUC, verifyUC, loginUC, refreshUC, logoutUC, forgotPasswordUC, resetPasswordUC, cfg)
	userHandler := http.NewUserHandler(changePasswordUC, getProfileUC, updateProfileUC, deleteAccountUC)
	appPwdHandler := http.NewAppPasswordHandler(createAppPwdUC, listAppPwdUC, revokeAppPwdUC, cfg)
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
	authGroup.Post("/forgot-password", authHandler.ForgotPassword)
	authGroup.Post("/reset-password", authHandler.ResetPassword)

	// User Routes (Protected)
	userGroup := v1.Group("/users", http.Authenticate(jwtManager))
	userGroup.Get("/me", userHandler.GetProfile)
	userGroup.Patch("/me", userHandler.UpdateProfile)
	userGroup.Delete("/me", userHandler.DeleteAccount)
	userGroup.Put("/me/password", userHandler.ChangePassword)

	// App Password Routes (Protected)
	appPwdGroup := v1.Group("/app-passwords", http.Authenticate(jwtManager))
	appPwdGroup.Post("/", appPwdHandler.Create)
	appPwdGroup.Get("/", appPwdHandler.List)
	appPwdGroup.Delete("/:id", appPwdHandler.Revoke)
}
