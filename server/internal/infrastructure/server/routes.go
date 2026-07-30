package server

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/adapter/webdav"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/infrastructure/email"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	aboutusecase "github.com/jherrma/caldav-server/internal/usecase/about"
	addressbookusecase "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	"github.com/jherrma/caldav-server/internal/usecase/apppassword"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
	contactusecase "github.com/jherrma/caldav-server/internal/usecase/contact"
	eventusecase "github.com/jherrma/caldav-server/internal/usecase/event"
	"github.com/jherrma/caldav-server/internal/usecase/importexport"
	"github.com/jherrma/caldav-server/internal/usecase/sharing"
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
	carddavCredRepo := repository.NewCardDAVCredentialRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())
	abShareRepo := repository.NewAddressBookShareRepository(db.DB())

	// Services
	emailService := email.NewEmailService(cfg.SMTP)
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// Ensure JWT Secret. This loads the persisted secret from system_settings
	// (or generates and persists one on first boot). It must succeed — running
	// with an empty secret would sign every token with "" and accept forgeries.
	if err := jwtManager.EnsureSecret(context.Background(), systemRepo); err != nil {
		log.Fatalf("failed to ensure JWT secret: %v", err)
	}

	// Logging
	securityLogger := logging.NewSecurityLogger(slog.Default())

	// Use Cases
	registerUC := authusecase.NewRegisterUseCase(userRepo, calendarRepo, addressBookRepo, emailService, cfg)
	verifyUC := authusecase.NewVerifyUseCase(userRepo)
	loginUC := authusecase.NewLoginUseCase(userRepo, tokenRepo, jwtManager, cfg, securityLogger)
	refreshUC := authusecase.NewRefreshUseCase(tokenRepo, jwtManager, cfg.JWT.RefreshExpiry, securityLogger)
	logoutUC := authusecase.NewLogoutUseCase(tokenRepo, jwtManager)
	changePasswordUC := authusecase.NewChangePasswordUseCase(userRepo, tokenRepo, jwtManager, securityLogger)
	forgotPasswordUC := authusecase.NewForgotPasswordUseCase(userRepo, resetRepo, emailService, cfg.JWT.ResetExpiry)
	resetPasswordUC := authusecase.NewResetPasswordUseCase(userRepo, resetRepo, tokenRepo)

	// User Use Cases
	getProfileUC := userusecase.NewGetProfileUseCase(userRepo)
	updateProfileUC := userusecase.NewUpdateProfileUseCase(userRepo)
	deleteAccountUC := userusecase.NewDeleteAccountUseCase(userRepo)

	// App Password Use Cases
	createAppPwdUC := apppassword.NewCreateUseCase(userRepo, appPwdRepo, securityLogger)
	listAppPwdUC := apppassword.NewListUseCase(appPwdRepo)
	revokeAppPwdUC := apppassword.NewRevokeUseCase(appPwdRepo, securityLogger)

	// CalDAV Credential Use Cases
	createCaldavCredUC := apppassword.NewCreateCalDAVCredentialUseCase(caldavCredRepo, securityLogger)
	listCaldavCredUC := apppassword.NewListCalDAVCredentialsUseCase(caldavCredRepo)
	revokeCaldavCredUC := apppassword.NewRevokeCalDAVCredentialUseCase(caldavCredRepo, securityLogger)

	// CardDAV Credential Use Cases
	createCarddavCredUC := apppassword.NewCreateCardDAVCredentialUseCase(carddavCredRepo, securityLogger)
	listCarddavCredUC := apppassword.NewListCardDAVCredentialsUseCase(carddavCredRepo)
	revokeCarddavCredUC := apppassword.NewRevokeCardDAVCredentialUseCase(carddavCredRepo, securityLogger)

	// Sharing Use Cases
	createShareUC := sharing.NewCreateCalendarShareUseCase(shareRepo, calendarRepo, userRepo)
	listShareUC := sharing.NewListCalendarSharesUseCase(shareRepo, calendarRepo)
	updateShareUC := sharing.NewUpdateCalendarShareUseCase(shareRepo, calendarRepo)
	revokeShareUC := sharing.NewRevokeCalendarShareUseCase(shareRepo, calendarRepo)

	// Address Book Sharing Use Cases
	createABShareUC := sharing.NewCreateAddressBookShareUseCase(abShareRepo, addressBookRepo, userRepo)
	listABShareUC := sharing.NewListAddressBookSharesUseCase(abShareRepo, addressBookRepo)
	updateABShareUC := sharing.NewUpdateAddressBookShareUseCase(abShareRepo, addressBookRepo)
	revokeABShareUC := sharing.NewRevokeAddressBookShareUseCase(abShareRepo, addressBookRepo)

	// OAuth Manager (initialized early for system handler)
	oauthRepo := repository.NewOAuthConnectionRepository(db.DB())
	oauthManager, err := authadapter.NewOAuthProviderManager(&cfg.OAuth, cfg.BaseURL)
	if err != nil {
		fmt.Printf("Failed to initialize OAuth provider manager: %v\n", err)
	}

	// Handlers
	authHandler := http.NewAuthHandler(registerUC, verifyUC, loginUC, refreshUC, logoutUC, forgotPasswordUC, resetPasswordUC, cfg)
	systemHandler := http.NewSystemHandler(cfg, userRepo, oauthManager)
	userHandler := http.NewUserHandler(changePasswordUC, getProfileUC, updateProfileUC, deleteAccountUC, calendarRepo, addressBookRepo, appPwdRepo)
	appPwdHandler := http.NewAppPasswordHandler(createAppPwdUC, listAppPwdUC, revokeAppPwdUC, cfg)
	caldavCredHandler := http.NewCalDAVCredentialHandler(createCaldavCredUC, listCaldavCredUC, revokeCaldavCredUC)
	carddavCredHandler := http.NewCardDAVCredentialHandler(createCarddavCredUC, listCarddavCredUC, revokeCarddavCredUC)
	shareHandler := http.NewCalendarShareHandler(createShareUC, listShareUC, updateShareUC, revokeShareUC, calendarRepo)
	abShareHandler := http.NewAddressBookShareHandler(createABShareUC, listABShareUC, updateABShareUC, revokeABShareUC, addressBookRepo)
	healthHandler := http.NewHealthHandler(db)
	aboutHandler := http.NewAboutHandler(aboutusecase.NewListOpenSourceUseCase())

	// Public Calendar Use Cases
	enablePublicUC := calendarusecase.NewEnablePublicUseCase(calendarRepo, cfg.BaseURL)
	getPublicStatusUC := calendarusecase.NewGetPublicStatusUseCase(calendarRepo, cfg.BaseURL)
	regenerateTokenUC := calendarusecase.NewRegenerateTokenUseCase(calendarRepo, cfg.BaseURL)
	calendarPublicHandler := http.NewCalendarPublicHandler(enablePublicUC, getPublicStatusUC, regenerateTokenUC, calendarRepo)
	publicCalendarHandler := http.NewPublicCalendarHandler(calendarRepo)

	// Public Routes
	app.Get("/health", healthHandler.Liveness)
	app.Get("/ready", healthHandler.Readiness)
	app.Get("/public/calendar/:token", publicCalendarHandler.GetICalFeed)

	// API Documentation Routes
	http.SetupDocsRoutes(app, "./docs")

	// API Group
	v1 := app.Group("/api/v1")

	// System Routes (public - needed by frontend before auth)
	systemGroup := v1.Group("/system")
	systemGroup.Get("/settings", systemHandler.Settings)

	// About Routes (Protected) — open-source attribution (#101). The story asks
	// for authenticated access, which also keeps the exact versions of every
	// linked Go library out of anonymous reach. Note this is NOT a secrecy
	// guarantee for the project's dependencies as a whole: the npm half of the
	// list is a static SPA asset (public/open-source.json) and is therefore
	// served unauthenticated like every other frontend asset.
	aboutGroup := v1.Group("/about", http.Authenticate(jwtManager, userRepo))
	aboutGroup.Get("/open-source", aboutHandler.OpenSource)

	// Auth Routes
	authGroup := v1.Group("/auth")
	authGroup.Get("/methods", systemHandler.AuthMethods)
	authGroup.Post("/register", authHandler.Register)
	authGroup.Get("/verify", authHandler.Verify)

	// Auth limiter thresholds. The per-IP allowance is kept ABOVE the per-email
	// allowance on purpose: with IP <= email the IP limiter always trips first
	// from a single source address, so the tighter per-account (per-email)
	// control is never reached — masked in production behind a NAT/reverse
	// proxy (where every client collapses to one c.IP()) and untestable from a
	// single connection. Defaults come from config (20 IP / 10 email); tests
	// override them to pin exact thresholds.
	authIPMax := cfg.RateLimit.AuthIPRequests
	authEmailMax := cfg.RateLimit.AuthEmailRequests
	authWindow := cfg.RateLimit.Window
	if authWindow <= 0 {
		authWindow = time.Minute
	}

	// Login rate limiting — gated by the same RateLimit.Enabled flag that
	// controls the global limiter, so integration tests (which set it false)
	// don't trip IP-level limits when they log in repeatedly from 127.0.0.1.
	if cfg.RateLimit.Enabled {
		loginIPLimiter := http.NewIPRateLimiter(authIPMax, authWindow)
		loginEmailLimiter := http.NewEmailRateLimiter(authEmailMax, authWindow)
		authGroup.Post("/login", http.ExtractEmailMiddleware(), loginIPLimiter, loginEmailLimiter, authHandler.Login)
	} else {
		authGroup.Post("/login", authHandler.Login)
	}

	authGroup.Post("/refresh", authHandler.Refresh)
	authGroup.Post("/logout", authHandler.Logout)

	// Password-reset rate limiting — gated by the same RateLimit.Enabled flag
	// as login so tests (which disable it) aren't clamped. forgot-password
	// sends a real email per request, so without a dedicated throttle it can be
	// abused as a mail-flooding primitive against a victim inbox. It reuses the
	// login pattern (IP + email limiter): the per-EMAIL limiter is the primary
	// control, since behind a reverse proxy c.IP() collapses every client to a
	// single address. reset-password sends no mail (tokens are high-entropy and
	// hashed), so an IP limiter is enough there.
	if cfg.RateLimit.Enabled {
		forgotIPLimiter := http.NewIPRateLimiter(authIPMax, authWindow)
		forgotEmailLimiter := http.NewEmailRateLimiter(authEmailMax, authWindow)
		authGroup.Post("/forgot-password", http.ExtractEmailMiddleware(), forgotIPLimiter, forgotEmailLimiter, authHandler.ForgotPassword)

		// reset-password has no email in its body ({token, new_password}), so
		// only the per-IP allowance applies here.
		resetIPLimiter := http.NewIPRateLimiter(authIPMax, authWindow)
		authGroup.Post("/reset-password", resetIPLimiter, authHandler.ResetPassword)
	} else {
		authGroup.Post("/forgot-password", authHandler.ForgotPassword)
		authGroup.Post("/reset-password", authHandler.ResetPassword)
	}

	// User Routes (Protected)
	userGroup := v1.Group("/users", http.Authenticate(jwtManager, userRepo))
	userGroup.Get("/me", userHandler.GetProfile)
	userGroup.Patch("/me", userHandler.UpdateProfile)
	userGroup.Delete("/me", userHandler.DeleteAccount)
	userGroup.Put("/me/password", userHandler.ChangePassword)

	// Import/Export Use Cases
	calendarImportUC := importexport.NewCalendarImportUseCase(calendarRepo)
	contactImportUC := importexport.NewContactImportUseCase(addressBookRepo)
	backupExportUC := importexport.NewBackupExportUseCase(calendarRepo, addressBookRepo)

	importHandler := http.NewImportHandler(calendarImportUC, contactImportUC, addressBookRepo)
	backupHandler := http.NewBackupHandler(backupExportUC)

	// Backup Export Route
	userGroup.Get("/me/export", backupHandler.Export)

	// App Password Routes (Protected)
	appPwdGroup := v1.Group("/app-passwords", http.Authenticate(jwtManager, userRepo))
	appPwdGroup.Get("/", appPwdHandler.List)
	appPwdGroup.Post("/", appPwdHandler.Create)
	appPwdGroup.Delete("/:id", appPwdHandler.Revoke)

	// CalDAV Credential Routes (Protected)
	caldavCredGroup := v1.Group("/caldav-credentials", http.Authenticate(jwtManager, userRepo))
	caldavCredGroup.Post("/", caldavCredHandler.Create)
	caldavCredGroup.Get("/", caldavCredHandler.List)
	caldavCredGroup.Delete("/:id", caldavCredHandler.Revoke)

	// CardDAV Credential Routes (Protected)
	carddavCredGroup := v1.Group("/carddav-credentials", http.Authenticate(jwtManager, userRepo))
	carddavCredGroup.Post("/", carddavCredHandler.Create)
	carddavCredGroup.Get("/", carddavCredHandler.List)
	carddavCredGroup.Delete("/:id", carddavCredHandler.Revoke)

	// OAuth Routes
	initiateOAuthUC := authusecase.NewInitiateOAuthUseCase(oauthManager)
	oauthCallbackUC := authusecase.NewOAuthCallbackUseCase(oauthManager, userRepo, oauthRepo, tokenRepo, jwtManager, cfg)
	unlinkUC := authusecase.NewUnlinkProviderUseCase(oauthRepo, userRepo)
	listLinkedUC := authusecase.NewListLinkedProvidersUseCase(oauthRepo, userRepo)

	oauthHandler := http.NewOAuthHandler(initiateOAuthUC, oauthCallbackUC, unlinkUC, listLinkedUC, cfg)

	oauthGroup := v1.Group("/auth/oauth")
	oauthGroup.Get("/providers", http.Authenticate(jwtManager, userRepo), oauthHandler.List) // List linked providers (auth required)
	oauthGroup.Get("/:provider", oauthHandler.Initiate)
	oauthGroup.Get("/:provider/callback", oauthHandler.Callback)
	oauthGroup.Post("/:provider/link", http.Authenticate(jwtManager, userRepo), oauthHandler.Link)
	oauthGroup.Delete("/:provider", http.Authenticate(jwtManager, userRepo), oauthHandler.Unlink)

	// Calendar Routes (Protected)
	calendarCreateUC := calendarusecase.NewCreateCalendarUseCase(calendarRepo)
	calendarListUC := calendarusecase.NewListCalendarsUseCase(calendarRepo, shareRepo)
	calendarGetUC := calendarusecase.NewGetCalendarUseCase(calendarRepo)
	calendarUpdateUC := calendarusecase.NewUpdateCalendarUseCase(calendarRepo)
	calendarDeleteUC := calendarusecase.NewDeleteCalendarUseCase(calendarRepo, shareRepo)
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
	calendarGroup.Post("/:id/import", importHandler.ImportCalendar)

	// Calendar Share Routes
	calendarGroup.Post("/:id/shares", shareHandler.Create)
	calendarGroup.Get("/:id/shares", shareHandler.List)
	calendarGroup.Patch("/:id/shares/:share_id", shareHandler.Update)
	calendarGroup.Delete("/:id/shares/:share_id", shareHandler.Revoke)

	// Calendar Public Access Routes
	calendarGroup.Post("/:id/public", calendarPublicHandler.EnablePublic)
	calendarGroup.Get("/:id/public", calendarPublicHandler.GetPublicStatus)
	calendarGroup.Post("/:id/public/regenerate", calendarPublicHandler.RegenerateToken)

	// Address Book Routes (Protected)
	abCreateUC := addressbookusecase.NewCreateUseCase(addressBookRepo)
	abListUC := addressbookusecase.NewListUseCase(addressBookRepo, abShareRepo)
	abGetUC := addressbookusecase.NewGetUseCase(addressBookRepo)
	abUpdateUC := addressbookusecase.NewUpdateUseCase(addressBookRepo)
	abDeleteUC := addressbookusecase.NewDeleteUseCase(addressBookRepo, abShareRepo)
	abExportUC := addressbookusecase.NewExportUseCase(addressBookRepo)
	// NOTE: addressbookusecase.CreateContactUseCase is still alive — it backs
	// ContactHandler.Create through contactusecase.CreateUseCase (see below).
	abCreateContactUC := addressbookusecase.NewCreateContactUseCase(addressBookRepo)

	abHandler := http.NewAddressBookHandler(
		abCreateUC,
		abListUC,
		abGetUC,
		abUpdateUC,
		abDeleteUC,
		abExportUC,
		addressBookRepo,
	)

	abGroup := v1.Group("/addressbooks", http.Authenticate(jwtManager, userRepo))
	abGroup.Post("/", abHandler.Create)
	abGroup.Get("/", abHandler.List)
	abGroup.Get("/:id", abHandler.Get)
	abGroup.Patch("/:id", abHandler.Update)
	abGroup.Delete("/:id", abHandler.Delete)
	abGroup.Get("/:id/export", abHandler.Export)
	abGroup.Post("/:id/import", importHandler.ImportContact)

	// Address Book Share Routes
	abGroup.Post("/:id/shares", abShareHandler.Create)
	abGroup.Get("/:id/shares", abShareHandler.List)
	abGroup.Patch("/:id/shares/:share_id", abShareHandler.Update)
	abGroup.Delete("/:id/shares/:share_id", abShareHandler.Revoke)

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
		addressBookRepo,
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
	caldavBackend := webdav.NewCalDAVBackend(calendarRepo, userRepo, shareRepo)
	carddavBackend := webdav.NewCardDAVBackend(addressBookRepo, userRepo, abShareRepo)
	davHandler := webdav.NewHandler(caldavBackend, carddavBackend, userRepo, appPwdRepo, caldavCredRepo, carddavCredRepo, jwtManager)

	// RFC 6764 §5: redirect the well-known context path regardless of method.
	// Apple's iOS/macOS account setup autodiscovers via PROPFIND (not GET), so a
	// GET-only route 404s those clients. The redirect handlers are method-agnostic.
	app.All("/.well-known/caldav", webdav.WellKnownCalDAVRedirect)
	app.All("/.well-known/carddav", webdav.WellKnownCardDAVRedirect)

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
		calendarRepo,
	)

	eventGroup := calendarGroup.Group("/:calendar_id/events")
	eventGroup.Get("/", eventHandler.List)
	eventGroup.Post("/", eventHandler.Create)
	eventGroup.Get("/:event_id", eventHandler.Get)
	eventGroup.Patch("/:event_id", eventHandler.Update)
	eventGroup.Delete("/:event_id", eventHandler.Delete)
	eventGroup.Post("/:event_id/move", eventHandler.Move)

	// Serve the built SPA (single-container deployment). Registered LAST so every
	// API/DAV/well-known/health/docs route above wins first.
	registerWebUI(app, "./public")
}

// registerWebUI serves the built Nuxt SPA from dir when dir/index.html exists
// (the Docker image copies the static output there; in dev / unit tests the
// directory is absent, so this is a no-op and behavior is unchanged). Unknown
// non-API GET paths fall back to index.html so the SPA's client-side router can
// handle deep links; API-ish paths keep 404-ing (never HTML). Returns whether
// static serving was registered. Must be called AFTER all API/DAV routes.
func registerWebUI(app *fiber.App, dir string) bool {
	if _, err := os.Stat(dir + "/index.html"); err != nil {
		return false
	}
	app.Get("/*", static.New(dir, static.Config{
		Compress: true,
		NotFoundHandler: func(c fiber.Ctx) error {
			p := c.Path()
			if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/dav/") ||
				strings.HasPrefix(p, "/.well-known/") {
				return c.SendStatus(fiber.StatusNotFound)
			}
			// The static handler set 404 before delegating here; a client-side
			// route is a successful SPA load, so reset to 200 for the fallback.
			c.Status(fiber.StatusOK)
			return c.SendFile(dir + "/index.html")
		},
	}))
	return true
}
