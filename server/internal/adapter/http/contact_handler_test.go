package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	addressbookusecase "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	contactusecase "github.com/jherrma/caldav-server/internal/usecase/contact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupContactHandlerTest(t *testing.T) (*fiber.App, database.Database, *user.User, *addressbook.AddressBook, string) {
	dataDir, err := os.MkdirTemp("", "contact-test-*")
	require.NoError(t, err)

	cfg := &config.Config{
		DataDir: dataDir,
		Database: config.DatabaseConfig{
			Driver: "sqlite",
		},
		JWT: config.JWTConfig{
			Secret:       "test-secret",
			AccessExpiry: time.Hour,
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)

	err = db.Migrate(database.Models()...)
	require.NoError(t, err)

	app := fiber.New()

	// Repos
	userRepo := repository.NewUserRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	// User
	u := &user.User{
		UUID:     "user-uuid",
		Email:    "test@example.com",
		Username: "testuser",
		IsActive: true,
	}
	err = userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	// AddressBook
	ab := &addressbook.AddressBook{
		UUID:      "ab-uuid",
		UserID:    u.ID,
		Name:      "Contacts",
		Path:      "contacts",
		SyncToken: "1",
		CTag:      "1",
	}
	err = abRepo.Create(context.Background(), ab)
	require.NoError(t, err)

	token, _, _ := jwtManager.GenerateAccessToken(u.UUID, u.Email)

	// UseCases
	abCreateContactUC := addressbookusecase.NewCreateContactUseCase(abRepo)
	contactCreateUC := contactusecase.NewCreateUseCase(abCreateContactUC)
	contactListUC := contactusecase.NewListUseCase(abRepo)
	contactGetUC := contactusecase.NewGetUseCase(abRepo)
	contactUpdateUC := contactusecase.NewUpdateUseCase(abRepo)
	contactDeleteUC := contactusecase.NewDeleteUseCase(abRepo)
	contactSearchUC := contactusecase.NewSearchUseCase(abRepo)
	contactMoveUC := contactusecase.NewMoveUseCase(abRepo)
	contactPhotoUC := contactusecase.NewPhotoUseCase(abRepo)

	handler := NewContactHandler(
		contactCreateUC,
		contactListUC,
		contactGetUC,
		contactUpdateUC,
		contactDeleteUC,
		contactSearchUC,
		contactMoveUC,
		contactPhotoUC,
	)

	// Routes
	v1 := app.Group("/api/v1")
	abGroup := v1.Group("/addressbooks", Authenticate(jwtManager, userRepo))

	abGroup.Get("/:addressbook_id/contacts", handler.List)
	abGroup.Post("/:addressbook_id/contacts", handler.Create)
	abGroup.Get("/:addressbook_id/contacts/:contact_id", handler.Get)
	abGroup.Patch("/:addressbook_id/contacts/:contact_id", handler.Update)
	abGroup.Delete("/:addressbook_id/contacts/:contact_id", handler.Delete)

	abGroup.Post("/:addressbook_id/contacts/:contact_id/move", handler.Move)
	abGroup.Put("/:addressbook_id/contacts/:contact_id/photo", handler.UploadPhoto)
	abGroup.Delete("/:addressbook_id/contacts/:contact_id/photo", handler.DeletePhoto)
	abGroup.Get("/:addressbook_id/contacts/:contact_id/photo", handler.ServePhoto)

	v1.Get("/contacts/search", Authenticate(jwtManager, userRepo), handler.Search)

	return app, db, u, ab, token
}

func TestContactHandler_CRUD(t *testing.T) {
	app, db, _, ab, token := setupContactHandlerTest(t)
	defer db.Close()

	var contactID string
	abIDStr := strconv.Itoa(int(ab.ID))

	t.Run("Create Contact", func(t *testing.T) {
		reqBody := contact.Contact{
			GivenName:  "John",
			FamilyName: "Doe",
			Emails: []contact.Email{
				{Type: "WORK", Value: "john@work.com", Primary: true},
			},
		}
		body, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("POST", "/api/v1/addressbooks/"+abIDStr+"/contacts", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

		var res contact.Contact
		json.NewDecoder(resp.Body).Decode(&res)
		assert.NotEmpty(t, res.ID)
		assert.Equal(t, "John", res.GivenName)
		assert.Equal(t, "Doe", res.FamilyName)
		contactID = res.ID
	})

	t.Run("Get Contact", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res contact.Contact
		json.NewDecoder(resp.Body).Decode(&res)
		assert.Equal(t, contactID, res.ID)
		assert.Equal(t, "John", res.GivenName)
	})

	t.Run("Update Contact", func(t *testing.T) {
		// Update Family Name
		updateInput := map[string]interface{}{
			"family_name": "Smith",
		}
		body, _ := json.Marshal(updateInput)
		req, _ := http.NewRequest("PATCH", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res contact.Contact
		json.NewDecoder(resp.Body).Decode(&res)
		assert.Equal(t, "Smith", res.FamilyName)
		assert.Equal(t, "John", res.GivenName) // Should remain unchanged
	})

	t.Run("List Contacts", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/addressbooks/"+abIDStr+"/contacts", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res struct {
			Contacts []contact.Contact `json:"contacts"`
			Total    int               `json:"total"`
		}
		json.NewDecoder(resp.Body).Decode(&res)
		assert.GreaterOrEqual(t, res.Total, 1)
		found := false
		for _, c := range res.Contacts {
			if c.ID == contactID {
				found = true
				break
			}
		}
		assert.True(t, found, "Created contact should be in list")
	})

	t.Run("Search Contacts", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/contacts/search?q=Smith", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res struct {
			Results []contact.Contact `json:"contacts"`
			Query   string            `json:"query"`
			Count   int               `json:"count"`
		}
		json.NewDecoder(resp.Body).Decode(&res)
		assert.NotEmpty(t, res.Results)
		assert.Equal(t, contactID, res.Results[0].ID)
	})

	t.Run("Delete Contact", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify gone
		reqGet, _ := http.NewRequest("GET", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID, nil)
		reqGet.Header.Set("Authorization", "Bearer "+token)
		respGet, _ := app.Test(reqGet)
		assert.Equal(t, fiber.StatusNotFound, respGet.StatusCode)
	})
}

func TestContactHandler_Photo(t *testing.T) {
	app, db, _, ab, token := setupContactHandlerTest(t)
	defer db.Close()
	abIDStr := strconv.Itoa(int(ab.ID))

	// Create Contact
	reqBody := contact.Contact{
		GivenName:  "Photo",
		FamilyName: "User",
	}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/addressbooks/"+abIDStr+"/contacts", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	var res contact.Contact
	json.NewDecoder(resp.Body).Decode(&res)
	contactID := res.ID

	t.Run("Upload Photo", func(t *testing.T) {
		// Small 1x1 GIF
		gifData, _ := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")
		req, _ := http.NewRequest("PUT", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID+"/photo", bytes.NewReader(gifData))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "image/gif")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
	})

	t.Run("Serve Photo", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID+"/photo", nil)
		// Photo serving doesn't strictly need auth if it's a public link, but usually it does for private contacts.
		// The route is protected in setupContactHandlerTest?
		// "abGroup := v1.Group("/addressbooks", Authenticate(jwtManager))" -> Yes, protected.
		// If real implementation uses token in URL or cookies, we rely on header here.
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, "image/gif", resp.Header.Get("Content-Type"))
	})

	t.Run("Delete Photo", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID+"/photo", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify 404 on serve
		reqServe, _ := http.NewRequest("GET", "/api/v1/addressbooks/"+abIDStr+"/contacts/"+contactID+"/photo", nil)
		reqServe.Header.Set("Authorization", "Bearer "+token)
		respServe, _ := app.Test(reqServe)
		assert.Equal(t, fiber.StatusNotFound, respServe.StatusCode)
	})
}

func TestContactHandler_Move(t *testing.T) {
	app, db, u, ab1, token := setupContactHandlerTest(t)
	defer db.Close()
	abRepo := repository.NewAddressBookRepository(db.DB())

	// Create second addressbook
	ab2 := &addressbook.AddressBook{
		UUID:   uuid.New().String(),
		UserID: u.ID,
		Name:   "Other Contacts",
		Path:   "other-contacts",
	}
	err := abRepo.Create(context.Background(), ab2)
	require.NoError(t, err)

	ab1IDStr := strconv.Itoa(int(ab1.ID))

	// Create Contact in AB1
	c := &contact.Contact{GivenName: "Mover"}
	body, _ := json.Marshal(c)
	req, _ := http.NewRequest("POST", "/api/v1/addressbooks/"+ab1IDStr+"/contacts", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	var res contact.Contact
	json.NewDecoder(resp.Body).Decode(&res)
	contactID := res.ID

	t.Run("Move Contact", func(t *testing.T) {
		targetIDStr := strconv.Itoa(int(ab2.ID))
		moveInput := map[string]string{
			"target_addressbook_id": targetIDStr,
		}
		body, _ := json.Marshal(moveInput)
		req, _ := http.NewRequest("POST", "/api/v1/addressbooks/"+ab1IDStr+"/contacts/"+contactID+"/move", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res contact.Contact
		json.NewDecoder(resp.Body).Decode(&res)
		assert.Equal(t, targetIDStr, res.AddressBookID)
	})
}
