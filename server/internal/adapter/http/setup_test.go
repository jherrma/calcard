package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/infrastructure/logging"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/infrastructure/email"
	addressbookusecase "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	"github.com/jherrma/caldav-server/internal/usecase/apppassword"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
	userusecase "github.com/jherrma/caldav-server/internal/usecase/user"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func setupTestApp(t *testing.T) (*fiber.App, database.Database, *config.Config) {
	dataDir, err := os.MkdirTemp("", "calcard-test-*")
	require.NoError(t, err)

	cfg := &config.Config{
		DataDir: dataDir,
		Database: config.DatabaseConfig{
			Driver: "sqlite",
		},
		JWT: config.JWTConfig{
			Secret:        "test-secret",
			AccessExpiry:  time.Hour,
			RefreshExpiry: 24 * time.Hour,
			ResetExpiry:   15 * time.Minute,
		},
		SMTP: config.SMTPConfig{}, // Empty config to skip sending emails
	}

	db, err := database.New(cfg)
	require.NoError(t, err)

	err = db.Migrate(database.Models()...)
	require.NoError(t, err)

	app := fiber.New()

	// Repositories
	userRepo := repository.NewUserRepository(db.DB())
	tokenRepo := repository.NewRefreshTokenRepository(db.DB())
	resetRepo := repository.NewGORMPasswordResetRepository(db.DB())
	calendarRepo := repository.NewCalendarRepository(db.DB())
	addressBookRepo := repository.NewAddressBookRepository(db.DB())
	appPwdRepo := repository.NewAppPasswordRepository(db.DB())

	// Services
	emailService := email.NewEmailService(cfg.SMTP)
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	mockProviderManager := &mockOAuthProviderManager{
		providers: map[string]authadapter.OAuthProvider{
			"google": &mockOAuthProvider{name: "google"},
		},
	}

	// Logger
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	securityLogger := logging.NewSecurityLogger(logger)

	// Auth Use Cases
	registerUC := authusecase.NewRegisterUseCase(userRepo, calendarRepo, addressBookRepo, emailService, cfg)
	verifyUC := authusecase.NewVerifyUseCase(userRepo)
	loginUC := authusecase.NewLoginUseCase(userRepo, tokenRepo, jwtManager, cfg, securityLogger)
	refreshUC := authusecase.NewRefreshUseCase(tokenRepo, jwtManager)
	logoutUC := authusecase.NewLogoutUseCase(tokenRepo, jwtManager)
	forgotUC := authusecase.NewForgotPasswordUseCase(userRepo, resetRepo, emailService, cfg.JWT.ResetExpiry)
	resetUC := authusecase.NewResetPasswordUseCase(userRepo, resetRepo, tokenRepo)
	changePasswordUC := authusecase.NewChangePasswordUseCase(userRepo, tokenRepo, jwtManager, securityLogger)

	// OAuth Use Cases
	oauthInitiateUC := authusecase.NewInitiateOAuthUseCase(mockProviderManager)
	oauthCallbackUC := authusecase.NewOAuthCallbackUseCase(mockProviderManager, userRepo, repository.NewOAuthConnectionRepository(db.DB()), tokenRepo, jwtManager, cfg)
	oauthUnlinkUC := authusecase.NewUnlinkProviderUseCase(repository.NewOAuthConnectionRepository(db.DB()), userRepo)
	oauthListUC := authusecase.NewListLinkedProvidersUseCase(repository.NewOAuthConnectionRepository(db.DB()), userRepo)

	// User Use Cases
	getProfileUC := userusecase.NewGetProfileUseCase(userRepo)
	updateProfileUC := userusecase.NewUpdateProfileUseCase(userRepo)
	deleteAccountUC := userusecase.NewDeleteAccountUseCase(userRepo)

	// App Password Use Cases
	createAppPwdUC := apppassword.NewCreateUseCase(userRepo, appPwdRepo, securityLogger)
	listAppPwdUC := apppassword.NewListUseCase(appPwdRepo)
	revokeAppPwdUC := apppassword.NewRevokeUseCase(appPwdRepo, securityLogger)

	// Calendar Use Cases
	calendarCreateUC := calendarusecase.NewCreateCalendarUseCase(calendarRepo)
	calendarListUC := calendarusecase.NewListCalendarsUseCase(calendarRepo)
	calendarGetUC := calendarusecase.NewGetCalendarUseCase(calendarRepo)
	calendarUpdateUC := calendarusecase.NewUpdateCalendarUseCase(calendarRepo)
	calendarDeleteUC := calendarusecase.NewDeleteCalendarUseCase(calendarRepo)
	calendarExportUC := calendarusecase.NewExportCalendarUseCase(calendarRepo)

	// Address Book Use Cases
	abCreateUC := addressbookusecase.NewCreateUseCase(addressBookRepo)
	abListUC := addressbookusecase.NewListUseCase(addressBookRepo)
	abGetUC := addressbookusecase.NewGetUseCase(addressBookRepo)
	abUpdateUC := addressbookusecase.NewUpdateUseCase(addressBookRepo)
	abDeleteUC := addressbookusecase.NewDeleteUseCase(addressBookRepo)
	abExportUC := addressbookusecase.NewExportUseCase(addressBookRepo)
	abCreateContactUC := addressbookusecase.NewCreateContactUseCase(addressBookRepo)

	// Handlers
	authHandler := NewAuthHandler(
		registerUC,
		verifyUC,
		loginUC,
		refreshUC,
		logoutUC,
		forgotUC,
		resetUC,
		cfg,
	)

	userHandler := NewUserHandler(
		changePasswordUC,
		getProfileUC,
		updateProfileUC,
		deleteAccountUC,
	)

	calendarHandler := NewCalendarHandler(
		calendarCreateUC,
		calendarListUC,
		calendarGetUC,
		calendarUpdateUC,
		calendarDeleteUC,
		calendarExportUC,
	)

	abHandler := NewAddressBookHandler(
		abCreateUC,
		abListUC,
		abGetUC,
		abUpdateUC,
		abDeleteUC,
		abExportUC,
		abCreateContactUC,
	)

	appPwdHandler := NewAppPasswordHandler(
		createAppPwdUC,
		listAppPwdUC,
		revokeAppPwdUC,
		cfg,
	)

	oauthHandler := NewOAuthHandler(
		oauthInitiateUC,
		oauthCallbackUC,
		oauthUnlinkUC,
		oauthListUC,
	)

	healthHandler := NewHealthHandler(db)

	// Routes
	api := app.Group("/api/v1")

	// Auth Routes
	authGroup := api.Group("/auth")
	authGroup.Post("/register", authHandler.Register)
	authGroup.Get("/verify", authHandler.Verify)
	authGroup.Post("/login", authHandler.Login)
	authGroup.Post("/refresh", authHandler.Refresh)
	authGroup.Post("/logout", authHandler.Logout)
	authGroup.Post("/forgot-password", authHandler.ForgotPassword)
	authGroup.Post("/reset-password", authHandler.ResetPassword)

	// User Routes
	userGroup := api.Group("/users", Authenticate(jwtManager, userRepo))
	userGroup.Get("/me", userHandler.GetProfile)
	userGroup.Patch("/me", userHandler.UpdateProfile)
	userGroup.Delete("/me", userHandler.DeleteAccount)
	userGroup.Put("/me/password", userHandler.ChangePassword)

	// Calendar Routes
	calendarGroup := api.Group("/calendars", Authenticate(jwtManager, userRepo))
	calendarGroup.Post("/", calendarHandler.Create)
	calendarGroup.Get("/", calendarHandler.List)
	calendarGroup.Get("/:id", calendarHandler.Get)
	calendarGroup.Patch("/:id", calendarHandler.Update)
	calendarGroup.Delete("/:id", calendarHandler.Delete)
	calendarGroup.Get("/:id/export", calendarHandler.Export)

	// Address Book Routes
	abGroup := api.Group("/addressbooks", Authenticate(jwtManager, userRepo))
	abGroup.Post("/", abHandler.Create)
	abGroup.Get("/", abHandler.List)
	abGroup.Get("/:id", abHandler.Get)
	abGroup.Patch("/:id", abHandler.Update)
	abGroup.Delete("/:id", abHandler.Delete)
	abGroup.Get("/:id/export", abHandler.Export)

	// App Password Routes
	appPwdGroup := api.Group("/app-passwords", Authenticate(jwtManager, userRepo))
	appPwdGroup.Get("/", appPwdHandler.List)
	appPwdGroup.Delete("/:id", appPwdHandler.Revoke)

	// OAuth Routes
	oauthGroup := api.Group("/auth/oauth")
	oauthGroup.Get("/:provider/initiate", oauthHandler.Initiate)
	oauthGroup.Get("/:provider/callback", oauthHandler.Callback)
	oauthGroup.Post("/:provider/link", Authenticate(jwtManager, userRepo), oauthHandler.Link)
	oauthGroup.Delete("/:provider", Authenticate(jwtManager, userRepo), oauthHandler.Unlink)
	oauthGroup.Get("/providers", Authenticate(jwtManager, userRepo), oauthHandler.List)

	// Health Routes
	app.Get("/health", healthHandler.Liveness)
	app.Get("/health/ready", healthHandler.Readiness)

	// Cleanup
	t.Cleanup(func() {
		os.RemoveAll(dataDir)
		sqlDB, err := db.DB().DB()
		if err == nil {
			sqlDB.Close()
		}
	})

	return app, db, cfg
}

type mockOAuthProvider struct {
	name string
}

func (p *mockOAuthProvider) Name() string { return p.name }
func (p *mockOAuthProvider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return "https://example.com/auth?state=" + state
}
func (p *mockOAuthProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "fake-token"}, nil
}
func (p *mockOAuthProvider) UserInfo(ctx context.Context, tokenSource oauth2.TokenSource) (*authadapter.UserInfo, error) {
	return &authadapter.UserInfo{
		Subject:       "fake-sub",
		Email:         "oauth@example.com",
		EmailVerified: true,
		Name:          "OAuth User",
	}, nil
}

type mockOAuthProviderManager struct {
	providers map[string]authadapter.OAuthProvider
}

func (m *mockOAuthProviderManager) GetProvider(name string) (authadapter.OAuthProvider, error) {
	if p, ok := m.providers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockOAuthProviderManager) ListProviders() []string {
	return []string{"google"}
}
