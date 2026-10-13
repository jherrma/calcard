package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddressBookHandler_CRUD(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "ab@example.com",
		Username:      "abuser",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "ab-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	var createdABID uint
	var createdABUUID string

	t.Run("Create AddressBook", func(t *testing.T) {
		payload := map[string]string{
			"name":        "Test AddressBook",
			"description": "A test address book",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/addressbooks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

		var ab addressbook.AddressBook
		err = json.NewDecoder(resp.Body).Decode(&ab)
		require.NoError(t, err)
		assert.Equal(t, "Test AddressBook", ab.Name)
		assert.NotEmpty(t, ab.ID)
		require.NotEmpty(t, ab.UUID)
		createdABID = ab.ID
		createdABUUID = ab.UUID
	})

	t.Run("Get AddressBook", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/addressbooks/"+createdABUUID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var ab addressbook.AddressBook
		err = json.NewDecoder(resp.Body).Decode(&ab)
		require.NoError(t, err)
		assert.Equal(t, createdABID, ab.ID)
		assert.Equal(t, "Test AddressBook", ab.Name)
	})

	t.Run("List AddressBooks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/addressbooks", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var respData struct {
			AddressBooks []addressbook.AddressBook `json:"addressbooks"`
		}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(respData.AddressBooks), 1)
	})

	t.Run("Update AddressBook", func(t *testing.T) {
		payload := map[string]string{
			"name": "Updated AB Name",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/addressbooks/"+createdABUUID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var ab addressbook.AddressBook
		err = json.NewDecoder(resp.Body).Decode(&ab)
		require.NoError(t, err)
		assert.Equal(t, "Updated AB Name", ab.Name)
	})

	t.Run("Delete AddressBook", func(t *testing.T) {
		// Create another AB to ensure we are not deleting the last one
		dummyAB := &addressbook.AddressBook{
			UserID:      u.ID,
			Name:        "Dummy AddressBook",
			Description: "Dummy",
		}
		require.NoError(t, abRepo.Create(context.Background(), dummyAB))

		// Put a contact with a photo and a share in the book to be deleted: the
		// delete must take them along, not leave them behind in the database.
		contact := &addressbook.AddressObject{
			UUID: "doomed-contact", AddressBookID: createdABID, Path: "c.vcf", UID: "c@doomed",
			VCardData: "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c@doomed\r\nFN:Pic\r\nPHOTO;ENCODING=b;TYPE=JPEG:AAAA\r\nEND:VCARD\r\n",
		}
		require.NoError(t, abRepo.CreateObject(context.Background(), contact))
		var photos int64
		require.NoError(t, db.DB().Model(&addressbook.ContactPhoto{}).Where("address_object_id = ?", contact.ID).Count(&photos).Error)
		require.EqualValues(t, 1, photos, "seed: the PHOTO is stored in contact_photos")
		sharee := &user.User{Email: "ab-sharee@example.com", Username: "absharee", PasswordHash: "hash", IsActive: true, UUID: "ab-sharee-uuid"}
		require.NoError(t, userRepo.Create(context.Background(), sharee))
		require.NoError(t, repository.NewAddressBookShareRepository(db.DB()).Create(context.Background(), &sharing.AddressBookShare{
			UUID: "doomed-ab-share", AddressBookID: createdABID, SharedWithID: sharee.ID, Permission: "read",
		}))

		payload := map[string]string{
			"confirmation": "DELETE",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/addressbooks/"+createdABUUID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		ab, err := abRepo.GetByID(context.Background(), createdABID)
		require.NoError(t, err)
		assert.Nil(t, ab)

		// Gone from the database, not just hidden: soft-deleted rows count too.
		for model, where := range map[any][2]any{
			&addressbook.AddressBook{}:   {"id = ?", createdABID},
			&addressbook.AddressObject{}: {"address_book_id = ?", createdABID},
			&addressbook.ContactPhoto{}:  {"address_object_id = ?", contact.ID},
			&addressbook.SyncChangeLog{}: {"address_book_id = ?", createdABID},
			&sharing.AddressBookShare{}:  {"address_book_id = ?", createdABID},
		} {
			var n int64
			require.NoError(t, db.DB().Unscoped().Model(model).Where(where[0], where[1]).Count(&n).Error)
			assert.Zerof(t, n, "%T rows of the deleted address book must be hard-deleted", model)
		}
	})
}

func TestAddressBookHandler_Export(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	userRepo := repository.NewUserRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		Email:         "export_ab@example.com",
		Username:      "exportab",
		PasswordHash:  "hash",
		IsActive:      true,
		EmailVerified: true,
		UUID:          "export-ab-uuid",
	}
	err := userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
	require.NoError(t, err)

	ab := &addressbook.AddressBook{
		UUID:        "export-ab-book-uuid",
		UserID:      u.ID,
		Name:        "Export AddressBook",
		Description: "Export Test",
	}
	err = abRepo.Create(context.Background(), ab)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/addressbooks/"+ab.UUID+"/export", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/vcard")
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "attachment")
	})
}

func typeToString(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

// I will rewrite typeToString properly below using strconv
