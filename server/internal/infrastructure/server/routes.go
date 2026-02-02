package server

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http"
	samlhandler "github.com/jherrma/caldav-server/internal/adapter/http/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/adapter/webdav"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/infrastructure/email"
	addressbookusecase "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	"github.com/jherrma/caldav-server/internal/usecase/apppassword"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
	contactusecase "github.com/jherrma/caldav-server/internal/usecase/contact"
	eventusecase "github.com/jherrma/caldav-server/internal/usecase/event"
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

	calendarRepo := repository.NewCalendarRepository(db.DB())
	addressBookRepo := repository.NewAddressBookRepository(db.DB())
	caldavCredRepo := repository.NewCalDAVCredentialRepository(db.DB())

	// Services
	emailService := email.NewEmailService(cfg.SMTP)
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// Ensure JWT Secret
	if err := jwtManager.EnsureSecret(context.Background(), systemRepo); err != nil {
		fmt.Printf("failed to ensure JWT secret: %v\n", err)
	}

	// Use Cases
	registerUC := authusecase.NewRegisterUseCase(userRepo, calendarRepo, addressBookRepo, emailService, cfg)
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

	// CalDAV Credential Use Cases
	createCaldavCredUC := apppassword.NewCreateCalDAVCredentialUseCase(caldavCredRepo)
	listCaldavCredUC := apppassword.NewListCalDAVCredentialsUseCase(caldavCredRepo)
	revokeCaldavCredUC := apppassword.NewRevokeCalDAVCredentialUseCase(caldavCredRepo)

	// Handlers
	authHandler := http.NewAuthHandler(registerUC, verifyUC, loginUC, refreshUC, logoutUC, forgotPasswordUC, resetPasswordUC, cfg)
	userHandler := http.NewUserHandler(changePasswordUC, getProfileUC, updateProfileUC, deleteAccountUC)
	appPwdHandler := http.NewAppPasswordHandler(createAppPwdUC, listAppPwdUC, revokeAppPwdUC, cfg)
	caldavCredHandler := http.NewCalDAVCredentialHandler(createCaldavCredUC, listCaldavCredUC, revokeCaldavCredUC)
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
	userGroup := v1.Group("/users", http.Authenticate(jwtManager, userRepo))
	userGroup.Get("/me", userHandler.GetProfile)
	userGroup.Patch("/me", userHandler.UpdateProfile)
	userGroup.Delete("/me", userHandler.DeleteAccount)
	userGroup.Put("/me/password", userHandler.ChangePassword)

	// App Password Routes (Protected)
	appPwdGroup := v1.Group("/app-passwords", http.Authenticate(jwtManager, userRepo))
	appPwdGroup.Get("/", appPwdHandler.List)
	appPwdGroup.Delete("/:id", appPwdHandler.Revoke)

	// CalDAV Credential Routes (Protected)
	caldavCredGroup := v1.Group("/caldav-credentials", http.Authenticate(jwtManager, userRepo))
	caldavCredGroup.Post("/", caldavCredHandler.Create)
	caldavCredGroup.Get("/", caldavCredHandler.List)
	caldavCredGroup.Delete("/:id", caldavCredHandler.Revoke)

	// OAuth Routes
	oauthRepo := repository.NewOAuthConnectionRepository(db.DB())
	oauthManager, err := authadapter.NewOAuthProviderManager(&cfg.OAuth)
	if err != nil {
		fmt.Printf("Failed to initialize OAuth provider manager: %v\n", err)
	}

	initiateOAuthUC := authusecase.NewInitiateOAuthUseCase(oauthManager)
	oauthCallbackUC := authusecase.NewOAuthCallbackUseCase(oauthManager, userRepo, oauthRepo, tokenRepo, jwtManager, cfg)
	unlinkUC := authusecase.NewUnlinkProviderUseCase(oauthRepo, userRepo)
	listLinkedUC := authusecase.NewListLinkedProvidersUseCase(oauthRepo, userRepo)

	oauthHandler := http.NewOAuthHandler(initiateOAuthUC, oauthCallbackUC, unlinkUC, listLinkedUC)

	oauthGroup := v1.Group("/auth/oauth")
	oauthGroup.Get("/providers", http.Authenticate(jwtManager, userRepo), oauthHandler.List) // List linked providers (auth required)
	oauthGroup.Get("/:provider", oauthHandler.Initiate)
	oauthGroup.Get("/:provider/callback", oauthHandler.Callback)
	oauthGroup.Post("/:provider/link", http.Authenticate(jwtManager, userRepo), oauthHandler.Link)
	oauthGroup.Delete("/:provider", http.Authenticate(jwtManager, userRepo), oauthHandler.Unlink)

	// SAML Routes - Conditionally enabled based on config
	samlSessionRepo := repository.NewSAMLSessionRepository(db.DB())
	samlProvider, samlErr := authadapter.NewSAMLServiceProvider(&cfg.SAML, cfg.BaseURL)
	if samlErr != nil {
		fmt.Printf("Failed to initialize SAML provider: %v\n", samlErr)
	} else if samlProvider != nil {
		samlLoginUC := authusecase.NewSAMLLoginUseCase(samlProvider, userRepo, oauthRepo, samlSessionRepo, jwtManager, tokenRepo, cfg)
		samlMetadataUC := authusecase.NewSAMLMetadataUseCase(samlProvider)

		samlHandler := samlhandler.NewSAMLHandler(samlProvider, samlLoginUC, samlMetadataUC)

		samlGroup := v1.Group("/auth/saml")
		samlGroup.Get("/metadata", samlHandler.Metadata)
		samlGroup.Get("/login", samlHandler.Login)
		samlGroup.Post("/acs", samlHandler.ACS)
	}

	// Calendar Routes (Protected)
	calendarCreateUC := calendarusecase.NewCreateCalendarUseCase(calendarRepo)
	calendarListUC := calendarusecase.NewListCalendarsUseCase(calendarRepo)
	calendarGetUC := calendarusecase.NewGetCalendarUseCase(calendarRepo)
	calendarUpdateUC := calendarusecase.NewUpdateCalendarUseCase(calendarRepo)
	calendarDeleteUC := calendarusecase.NewDeleteCalendarUseCase(calendarRepo)
	calendarExportUC := calendarusecase.NewExportCalendarUseCase(calendarRepo)

	calendarHandler := http.NewCalendarHandler(
		calendarCreateUC,
		calendarListUC,
		calendarGetUC,
		calendarUpdateUC,
		calendarDeleteUC,
		calendarExportUC,
	)

	calendarGroup := v1.Group("/calendars", http.Authenticate(jwtManager, userRepo))
	calendarGroup.Post("/", calendarHandler.Create)
	calendarGroup.Get("/", calendarHandler.List)
	calendarGroup.Get("/:id", calendarHandler.Get)
	calendarGroup.Patch("/:id", calendarHandler.Update)

	calendarGroup.Delete("/:id", calendarHandler.Delete)
	calendarGroup.Get("/:id/export", calendarHandler.Export)

	// Address Book Routes (Protected)
	abCreateUC := addressbookusecase.NewCreateUseCase(addressBookRepo)
	abListUC := addressbookusecase.NewListUseCase(addressBookRepo)
	abGetUC := addressbookusecase.NewGetUseCase(addressBookRepo)
	abUpdateUC := addressbookusecase.NewUpdateUseCase(addressBookRepo)
	abDeleteUC := addressbookusecase.NewDeleteUseCase(addressBookRepo)
	abExportUC := addressbookusecase.NewExportUseCase(addressBookRepo)
	abCreateContactUC := addressbookusecase.NewCreateContactUseCase(addressBookRepo)

	abHandler := http.NewAddressBookHandler(
		abCreateUC,
		abListUC,
		abGetUC,
		abUpdateUC,
		abDeleteUC,
		abExportUC,
		abCreateContactUC,
	)

	abGroup := v1.Group("/addressbooks", http.Authenticate(jwtManager, userRepo))
	abGroup.Post("/", abHandler.Create)
	abGroup.Get("/", abHandler.List)
	abGroup.Get("/:id", abHandler.Get)
	abGroup.Patch("/:id", abHandler.Update)
	abGroup.Delete("/:id", abHandler.Delete)
	abGroup.Get("/:id/export", abHandler.Export)

	// Contact Use Cases
	contactCreateUC := contactusecase.NewCreateUseCase(abCreateContactUC)
	contactGetUC := contactusecase.NewGetUseCase(addressBookRepo)
	contactListUC := contactusecase.NewListUseCase(addressBookRepo)
	contactUpdateUC := contactusecase.NewUpdateUseCase(addressBookRepo)
	contactDeleteUC := contactusecase.NewDeleteUseCase(addressBookRepo)
	contactSearchUC := contactusecase.NewSearchUseCase(addressBookRepo)
	contactMoveUC := contactusecase.NewMoveUseCase(addressBookRepo)
	contactPhotoUC := contactusecase.NewPhotoUseCase(addressBookRepo)

	contactHandler := http.NewContactHandler(
		contactCreateUC,
		contactListUC,
		contactGetUC,
		contactUpdateUC,
		contactDeleteUC,
		contactSearchUC,
		contactMoveUC,
		contactPhotoUC,
	)

	// Contact Routes
	// Using :addressbook_id to match handler expectation
	abGroup.Get("/:addressbook_id/contacts", contactHandler.List)
	abGroup.Post("/:addressbook_id/contacts", contactHandler.Create)
	abGroup.Get("/:addressbook_id/contacts/:contact_id", contactHandler.Get)
	abGroup.Patch("/:addressbook_id/contacts/:contact_id", contactHandler.Update)
	abGroup.Delete("/:addressbook_id/contacts/:contact_id", contactHandler.Delete)

	abGroup.Post("/:addressbook_id/contacts/:contact_id/move", contactHandler.Move)
	abGroup.Put("/:addressbook_id/contacts/:contact_id/photo", contactHandler.UploadPhoto)
	abGroup.Delete("/:addressbook_id/contacts/:contact_id/photo", contactHandler.DeletePhoto)
	abGroup.Get("/:addressbook_id/contacts/:contact_id/photo", contactHandler.ServePhoto)

	// Global Contact Search
	v1.Get("/contacts/search", http.Authenticate(jwtManager, userRepo), contactHandler.Search)

	// CalDAV/CardDAV Routes
	caldavBackend := webdav.NewCalDAVBackend(calendarRepo, userRepo)
	carddavBackend := webdav.NewCardDAVBackend(addressBookRepo, userRepo)
	davHandler := webdav.NewHandler(caldavBackend, carddavBackend, userRepo, appPwdRepo, caldavCredRepo, jwtManager)

	app.Get("/.well-known/caldav", webdav.WellKnownCalDAVRedirect)
	app.Get("/.well-known/carddav", webdav.WellKnownCardDAVRedirect)

	davGroup := app.Group("/dav", davHandler.Authenticate())

	davGroup.Use("/*", davHandler.Handler())

	// Event Routes (Protected)
	eventListUC := eventusecase.NewListEventsUseCase(calendarRepo)
	eventGetUC := eventusecase.NewGetEventUseCase(calendarRepo)
	eventCreateUC := eventusecase.NewCreateEventUseCase(calendarRepo)
	eventUpdateUC := eventusecase.NewUpdateEventUseCase(calendarRepo)
	eventDeleteUC := eventusecase.NewDeleteEventUseCase(calendarRepo)
	eventMoveUC := eventusecase.NewMoveEventUseCase(calendarRepo)

	eventHandler := http.NewEventHandler(
		eventListUC,
		eventGetUC,
		eventCreateUC,
		eventUpdateUC,
		eventDeleteUC,
		eventMoveUC,
	)

	eventGroup := calendarGroup.Group("/:calendar_id/events")
	eventGroup.Get("/", eventHandler.List)
	eventGroup.Post("/", eventHandler.Create)
	eventGroup.Get("/:event_id", eventHandler.Get)
	eventGroup.Patch("/:event_id", eventHandler.Update)
	eventGroup.Delete("/:event_id", eventHandler.Delete)
	eventGroup.Post("/:event_id/move", eventHandler.Move)
}
