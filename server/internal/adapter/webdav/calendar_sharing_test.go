package webdav

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	adapterhttp "github.com/jherrma/caldav-server/internal/adapter/http"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	sharingUC "github.com/jherrma/caldav-server/internal/usecase/sharing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func setupSharingTest(t *testing.T) (*fiber.App, database.Database, *user.User, *user.User) {
	app, db, _ := setupTestApp(t) // Reuse setup from caldav_test.go
	// Create second user
	recipient := &user.User{
		Username:     "recipient",
		Email:        "recipient@example.com",
		PasswordHash: "hash",
		UUID:         uuid.New().String(),
	}
	// Create owner
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	owner := &user.User{
		UUID:         "owner-uuid",
		Email:        "owner@example.com",
		Username:     "owner",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	db.DB().Create(owner)

	db.DB().Create(recipient)

	// Register Sharing Routes
	userRepo := repository.NewUserRepository(db.DB())
	calendarRepo := repository.NewCalendarRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&config.JWTConfig{Secret: "test-secret"})

	createShareUC := sharingUC.NewCreateCalendarShareUseCase(shareRepo, calendarRepo, userRepo)
	listShareUC := sharingUC.NewListCalendarSharesUseCase(shareRepo, calendarRepo)
	updateShareUC := sharingUC.NewUpdateCalendarShareUseCase(shareRepo, calendarRepo)
	revokeShareUC := sharingUC.NewRevokeCalendarShareUseCase(shareRepo, calendarRepo)

	shareHandler := adapterhttp.NewCalendarShareHandler(createShareUC, listShareUC, updateShareUC, revokeShareUC)

	// Suppress unused variable error if strictly checked
	_ = shareHandler

	api := app.Group("/api/v1", adapterhttp.Authenticate(jwtManager, userRepo))
	api.Post("/calendars/:id/shares", shareHandler.Create)
	api.Get("/calendars/:id/shares", shareHandler.List)
	api.Patch("/calendars/:id/shares/:share_id", shareHandler.Update)
	api.Delete("/calendars/:id/shares/:share_id", shareHandler.Revoke)

	// Setup CalDAV with Sharing
	// Setup CalDAV with Sharing
	caldavBackend := NewCalDAVBackend(calendarRepo, userRepo, shareRepo)
	_ = caldavBackend // Suppress unused
	// We need to re-register /dav handler to use the new backend with sharing support
	// But Fiber app is already set up in setupTestApp...
	// Ideally we should make a comprehensive setup function.
	// For now, let's assume we can override or just test the use cases/handler logic directly if full app setup is hard.
	// Better: Create a fresh app for this test.

	// Better: Create a fresh app for this test.

	return app, db, owner, recipient
}

func TestCalendarSharingIntegration(t *testing.T) {
	// Minimal setup since setupTestApp is in another file and might need adaptation
	// We will replicate necessary parts.
	// ... code similar to caldav_test.go ...
	// Note: Assuming setupTestApp is available in package webdav (same package)

	// Actually, let's write a standalone test function that doesn't rely on hidden setupTestApp details if possible,
	// or rely on it if it's exported or in the same package.
	// Since it's in the same package `webdav`, we can use it.

	// Since it's in the same package `webdav`, we can use it.

	app, db, _ := setupTestApp(t)
	defer db.Close()

	// Create owner
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	owner := &user.User{
		UUID:         "owner-uuid",
		Email:        "owner@example.com",
		Username:     "owner",
		PasswordHash: string(passwordHash),
		IsActive:     true,
	}
	require.NoError(t, db.DB().Create(owner).Error)

	// Repos
	shareRepo := repository.NewCalendarShareRepository(db.DB())
	calendarRepo := repository.NewCalendarRepository(db.DB())
	userRepo := repository.NewUserRepository(db.DB())

	// Create recipient
	recipient := &user.User{
		Username:     "recipient",
		Email:        "recipient@example.com",
		PasswordHash: "hash",
		UUID:         uuid.New().String(),
	}
	require.NoError(t, userRepo.Create(context.Background(), recipient))

	// Setup Handler
	createShareUC := sharingUC.NewCreateCalendarShareUseCase(shareRepo, calendarRepo, userRepo)
	shareHandler := adapterhttp.NewCalendarShareHandler(createShareUC, nil, nil, nil)

	// We need to inject the handler into the app, but the app is already built in setupTestApp.
	// We can define a new route on the existing app.
	api := app.Group("/api/test-sharing")
	// Mock Auth middleware by setting local user
	api.Use(func(c fiber.Ctx) error {
		c.Locals("user", owner)
		return c.Next()
	})
	api.Post("/:id/shares", shareHandler.Create)

	// 1. Create Calendar
	cal := &calendar.Calendar{
		Name:   "Shared Cal",
		UserID: owner.ID,
		UUID:   uuid.New().String(),
		Path:   "shared-cal",
	}
	require.NoError(t, calendarRepo.Create(context.Background(), cal))

	// 2. Share Calendar
	reqBody := `{"user_identifier": "recipient@example.com", "permission": "read-write"}`
	req := httptest.NewRequest("POST", "/api/test-sharing/"+fmt.Sprint(cal.ID)+"/shares", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// 3. Verify Share in DB
	shares, err := shareRepo.ListByCalendarID(context.Background(), cal.ID)
	require.NoError(t, err)
	require.Len(t, shares, 1)
	assert.Equal(t, recipient.ID, shares[0].SharedWithID)
	assert.Equal(t, "read-write", shares[0].Permission)

	// 4. Verify CalDAV Access for Recipient
	// We need the CalDAV backend initialized with shareRepo.
	// The setupTestApp likely initialized it without shareRepo (old version).
	// We need to replace the backend or create a new handler.

	caldavBackend := NewCalDAVBackend(calendarRepo, userRepo, shareRepo)
	// Create a specific handler for this test
	handler := NewHandler(caldavBackend, nil, userRepo, nil, nil, nil, nil)
	_ = handler // Suppress unused

	// We can test the backend methods directly instead of full HTTP stack to be easier
	ctx := context.Background()
	// Inject recipient into context (mocking middleware)
	// Inject recipient into context (mocking middleware)
	ctx = context.WithValue(ctx, userContextKey, recipient)

	// List Calendars
	cals, err := caldavBackend.ListCalendars(ctx)
	require.NoError(t, err)
	found := false
	for _, c := range cals {
		if strings.Contains(c.Path, "/dav/recipient/calendars/shared-cal/") {
			found = true
			break
		}
	}
	assert.True(t, found, "Shared calendar should be listed for recipient")
}
