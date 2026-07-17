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

// TestSoftDeletedAddressBookHidesContacts is the regression test for M7: a
// soft-deleted address book's contacts must not leak through global search or
// the per-user contact count. Soft delete does not cascade to the objects, so
// the join must filter on address_books.deleted_at.
func TestSoftDeletedAddressBookHidesContacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(
		&addressbook.AddressBook{}, &addressbook.AddressObject{},
		&addressbook.ContactPhoto{}, &addressbook.SyncChangeLog{},
	))

	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	mkBook := func(name, path string) *addressbook.AddressBook {
		ab := &addressbook.AddressBook{
			Name: name, UserID: 1, UUID: uuid.New().String(),
			Path: path, SyncToken: "data:1", CTag: "1",
		}
		assert.NoError(t, repo.Create(ctx, ab))
		return ab
	}
	mkContact := func(ab *addressbook.AddressBook, fn string) *addressbook.AddressObject {
		obj := &addressbook.AddressObject{
			UUID: uuid.New().String(), AddressBookID: ab.ID, UID: uuid.New().String(),
			VCardData: "BEGIN:VCARD\nFN:" + fn + "\nEND:VCARD", FormattedName: fn,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		assert.NoError(t, repo.CreateObject(ctx, obj))
		return obj
	}

	keep := mkBook("Keep", "/keep")
	doomed := mkBook("Doomed", "/doomed")
	keepContact := mkContact(keep, "Zoe Keeper")
	mkContact(doomed, "Zoe Doomed")

	// Baseline: both contacts are visible.
	results, err := repo.SearchObjects(ctx, 1, "Zoe", nil, 10)
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	count, err := repo.CountContactsByUserID(ctx, 1)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Soft-delete one address book. Its AddressObject rows remain (no cascade).
	assert.NoError(t, repo.Delete(ctx, doomed.ID))

	// Search must now exclude the deleted book's contact.
	results, err = repo.SearchObjects(ctx, 1, "Zoe", nil, 10)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	if len(results) == 1 {
		assert.Equal(t, keepContact.ID, results[0].ID)
	}

	// Count must drop to only the surviving book's contact.
	count, err = repo.CountContactsByUserID(ctx, 1)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
}
