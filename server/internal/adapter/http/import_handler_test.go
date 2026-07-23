package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/usecase/importexport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validICS / validVCard are small, well-formed payloads used for the
// under-limit happy path.
const validICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:happy-1@example.com\r\nDTSTAMP:20340101T000000Z\r\n" +
	"SUMMARY:Happy Event\r\nDTSTART:20350601T100000Z\r\nDTEND:20350601T110000Z\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

const validVCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:happy-contact-1\r\n" +
	"FN:Happy Person\r\nN:Person;Happy;;;\r\nEND:VCARD\r\n"

// importTestEnv wires the ImportHandler against real use cases and SQLite repos.
type importTestEnv struct {
	app             *fiber.App
	calendarRepo    calendar.CalendarRepository
	addressBookRepo addressbook.Repository
	cal             *calendar.Calendar
	ab              *addressbook.AddressBook
}

// setupImportTestApp builds a Fiber app with a DELIBERATELY RAISED BodyLimit so
// that oversize JSON/raw bodies actually reach the handler. This isolates the
// import handler's own size check from the global Fiber BodyLimit (which today
// happens to cap these paths at 10MB) — exactly the scenario issue #72 guards
// against: the day BodyLimit is raised, the import limit must still hold.
func setupImportTestApp(t *testing.T) *importTestEnv {
	t.Helper()

	cfg := &config.Config{
		DataDir:  t.TempDir(),
		Database: config.DatabaseConfig{Driver: "sqlite"},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(database.Models()...))
	t.Cleanup(func() {
		if sqlDB, err := db.DB().DB(); err == nil {
			sqlDB.Close()
		}
	})

	userRepo := repository.NewUserRepository(db.DB())
	calendarRepo := repository.NewCalendarRepository(db.DB())
	addressBookRepo := repository.NewAddressBookRepository(db.DB())

	u := &user.User{
		Email:         "import@example.com",
		Username:      "importuser",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "import-user-uuid",
	}
	require.NoError(t, userRepo.Create(context.Background(), u))

	cal := &calendar.Calendar{
		UUID:     "import-cal-uuid",
		UserID:   u.ID,
		Name:     "Import Calendar",
		Path:     "import-cal.ics",
		Timezone: "UTC",
	}
	require.NoError(t, calendarRepo.Create(context.Background(), cal))

	ab := &addressbook.AddressBook{
		UUID:      "import-ab-uuid",
		UserID:    u.ID,
		Name:      "Import Address Book",
		Path:      "import-ab.vcf",
		SyncToken: addressbook.GenerateSyncToken(),
		CTag:      addressbook.GenerateCTag(),
	}
	require.NoError(t, addressBookRepo.Create(context.Background(), ab))

	calImportUC := importexport.NewCalendarImportUseCase(calendarRepo)
	contactImportUC := importexport.NewContactImportUseCase(addressBookRepo)
	h := NewImportHandler(calImportUC, contactImportUC, addressBookRepo)

	app := fiber.New(fiber.Config{
		BodyLimit: 50 * 1024 * 1024, // 50MB: bigger than the 10MB import limit
	})
	// Inject the authenticated user the way the real auth middleware does.
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", u.ID)
		return c.Next()
	})
	api := app.Group("/api/v1")
	api.Post("/calendars/:id/import", h.ImportCalendar)
	api.Post("/addressbooks/:id/import", h.ImportContact)

	return &importTestEnv{
		app:             app,
		calendarRepo:    calendarRepo,
		addressBookRepo: addressBookRepo,
		cal:             cal,
		ab:              ab,
	}
}

func oversizePayload() []byte {
	return bytes.Repeat([]byte("A"), maxImportFileSize+1)
}

func (e *importTestEnv) calObjectCount(t *testing.T) int {
	t.Helper()
	objs, err := e.calendarRepo.GetCalendarObjects(context.Background(), e.cal.ID)
	require.NoError(t, err)
	return len(objs)
}

func (e *importTestEnv) contactCount(t *testing.T) int64 {
	t.Helper()
	_, total, err := e.addressBookRepo.ListObjects(context.Background(), e.ab.ID, 1000, 0, "", "")
	require.NoError(t, err)
	return total
}

func TestImportCalendar_SizeLimit(t *testing.T) {
	t.Run("JSON data field oversize is rejected and not imported", func(t *testing.T) {
		env := setupImportTestApp(t)

		body, err := json.Marshal(map[string]string{"data": string(oversizePayload())})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/calendars/"+env.cal.UUID+"/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := env.app.Test(req, fiber.TestConfig{Timeout: 0})
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusRequestEntityTooLarge, resp.StatusCode)
		assert.Equal(t, 0, env.calObjectCount(t), "oversize input must not create any calendar objects")
	})

	t.Run("raw text/calendar body oversize is rejected and not imported", func(t *testing.T) {
		env := setupImportTestApp(t)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/calendars/"+env.cal.UUID+"/import", bytes.NewReader(oversizePayload()))
		req.Header.Set("Content-Type", "text/calendar")

		resp, err := env.app.Test(req, fiber.TestConfig{Timeout: 0})
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusRequestEntityTooLarge, resp.StatusCode)
		assert.Equal(t, 0, env.calObjectCount(t), "oversize input must not create any calendar objects")
	})

	t.Run("under-limit import succeeds", func(t *testing.T) {
		env := setupImportTestApp(t)

		body, err := json.Marshal(map[string]string{"data": validICS})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/calendars/"+env.cal.UUID+"/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := env.app.Test(req, fiber.TestConfig{Timeout: 0})
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var result importexport.ImportResult
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, 1, result.Imported)
		assert.Equal(t, 1, env.calObjectCount(t))
	})
}

func TestImportContact_SizeLimit(t *testing.T) {
	t.Run("JSON data field oversize is rejected and not imported", func(t *testing.T) {
		env := setupImportTestApp(t)

		body, err := json.Marshal(map[string]string{"data": string(oversizePayload())})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/addressbooks/"+env.ab.UUID+"/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := env.app.Test(req, fiber.TestConfig{Timeout: 0})
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusRequestEntityTooLarge, resp.StatusCode)
		assert.Equal(t, int64(0), env.contactCount(t), "oversize input must not create any contacts")
	})

	t.Run("raw text/vcard body oversize is rejected and not imported", func(t *testing.T) {
		env := setupImportTestApp(t)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/addressbooks/"+env.ab.UUID+"/import", bytes.NewReader(oversizePayload()))
		req.Header.Set("Content-Type", "text/vcard")

		resp, err := env.app.Test(req, fiber.TestConfig{Timeout: 0})
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusRequestEntityTooLarge, resp.StatusCode)
		assert.Equal(t, int64(0), env.contactCount(t), "oversize input must not create any contacts")
	})

	t.Run("under-limit import succeeds", func(t *testing.T) {
		env := setupImportTestApp(t)

		body, err := json.Marshal(map[string]string{"data": validVCard})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/addressbooks/"+env.ab.UUID+"/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := env.app.Test(req, fiber.TestConfig{Timeout: 0})
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var result importexport.ImportResult
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		assert.Equal(t, 1, result.Imported)
		assert.Equal(t, int64(1), env.contactCount(t))
	})
}
