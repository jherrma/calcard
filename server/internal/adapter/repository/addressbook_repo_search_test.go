package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestSearchObjectsInBooks covers the one contact-search corpus there is: an
// explicit set of address books, resolved by the caller. It replaced the
// owner-scoped SearchObjects in #162 — searching by user_id could never reach a
// book that was merely shared with the caller, which is exactly the corpus the
// rest of the app shows them.
func TestSearchObjectsInBooks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(
		&addressbook.AddressBook{}, &addressbook.AddressObject{},
		&addressbook.ContactPhoto{}, &addressbook.SyncChangeLog{},
	))

	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	// Two books owned by DIFFERENT users: the repository must go purely by the
	// ids it is handed, so ownership plays no part here.
	mkBook := func(name, path string, userID uint) *addressbook.AddressBook {
		ab := &addressbook.AddressBook{
			Name: name, UserID: userID, UUID: uuid.New().String(),
			Path: path, SyncToken: "data:1", CTag: "1",
		}
		assert.NoError(t, repo.Create(ctx, ab))
		return ab
	}
	mkContact := func(ab *addressbook.AddressBook, fn, given, family, email string) *addressbook.AddressObject {
		obj := &addressbook.AddressObject{
			UUID: uuid.New().String(), AddressBookID: ab.ID, UID: uuid.New().String(),
			VCardData:     "BEGIN:VCARD\nFN:" + fn + "\nEND:VCARD",
			FormattedName: fn, GivenName: given, FamilyName: family, Email: email,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		assert.NoError(t, repo.CreateObject(ctx, obj))
		return obj
	}

	own := mkBook("Book1", "/book1", 1)
	other := mkBook("Book2", "/book2", 2)

	wonderland := mkContact(own, "Alice Wonderland", "Alice", "Wonderland", "alice@example.com")
	smith := mkContact(other, "Alice Smith", "Alice", "Smith", "alice.smith@example.com")
	mkContact(own, "Bob Jones", "Bob", "Jones", "bob@example.com")

	t.Run("across both books", func(t *testing.T) {
		results, err := repo.SearchObjectsInBooks(ctx, []uint{own.ID, other.ID}, "Alice", 10, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		// Ordered by formatted name, so paging is stable: Smith before Wonderland.
		if len(results) == 2 {
			assert.Equal(t, smith.ID, results[0].ID)
			assert.Equal(t, wonderland.ID, results[1].ID)
		}
	})

	t.Run("narrowed to one book", func(t *testing.T) {
		results, err := repo.SearchObjectsInBooks(ctx, []uint{own.ID}, "Alice", 10, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		if len(results) == 1 {
			assert.Equal(t, wonderland.ID, results[0].ID)
		}

		// The other book's owner never enters the query — only the id set does.
		results, err = repo.SearchObjectsInBooks(ctx, []uint{other.ID}, "Alice", 10, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		if len(results) == 1 {
			assert.Equal(t, smith.ID, results[0].ID)
		}
	})

	t.Run("no match in the searched book", func(t *testing.T) {
		results, err := repo.SearchObjectsInBooks(ctx, []uint{other.ID}, "Bob", 10, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("empty id set matches nothing", func(t *testing.T) {
		// The dangerous reading would be "no filter, so all books". A caller with
		// access to nothing must get nothing.
		results, err := repo.SearchObjectsInBooks(ctx, nil, "Alice", 10, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("limit and offset page deterministically", func(t *testing.T) {
		books := []uint{own.ID, other.ID}
		first, err := repo.SearchObjectsInBooks(ctx, books, "Alice", 1, 0)
		assert.NoError(t, err)
		assert.Len(t, first, 1)
		second, err := repo.SearchObjectsInBooks(ctx, books, "Alice", 1, 1)
		assert.NoError(t, err)
		assert.Len(t, second, 1)
		if len(first) == 1 && len(second) == 1 {
			assert.Equal(t, smith.ID, first[0].ID)
			assert.Equal(t, wonderland.ID, second[0].ID)
		}
	})

	t.Run("matches the other denormalized columns", func(t *testing.T) {
		results, err := repo.SearchObjectsInBooks(ctx, []uint{own.ID}, "bob@example", 10, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
	})
}
