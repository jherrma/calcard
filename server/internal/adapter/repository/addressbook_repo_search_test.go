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

func TestSearchObjects(t *testing.T) {
	// Setup DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&addressbook.AddressBook{}, &addressbook.AddressObject{}, &addressbook.ContactPhoto{})

	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	// Create AddressBooks
	// Create AddressBooks
	ab1 := &addressbook.AddressBook{
		Name:      "Book1",
		UserID:    1,
		UUID:      uuid.New().String(),
		Path:      "/book1",
		SyncToken: "data:1",
		CTag:      "123",
	}
	err = repo.Create(ctx, ab1)
	assert.NoError(t, err)

	ab2 := &addressbook.AddressBook{
		Name:      "Book2",
		UserID:    1,
		UUID:      uuid.New().String(),
		Path:      "/book2",
		SyncToken: "data:2",
		CTag:      "456",
	}
	err = repo.Create(ctx, ab2)
	assert.NoError(t, err)

	// Create Contacts
	// Contact in Book1
	c1 := &addressbook.AddressObject{
		UUID:          uuid.New().String(),
		AddressBookID: ab1.ID,
		UID:           uuid.New().String(),
		VCardData:     "BEGIN:VCARD\nFN:Alice Wonderland\nEND:VCARD",
		FormattedName: "Alice Wonderland",
		GivenName:     "Alice",
		FamilyName:    "Wonderland",
		Email:         "alice@example.com",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	err = repo.CreateObject(ctx, c1)
	assert.NoError(t, err)

	// Contact in Book2
	c2 := &addressbook.AddressObject{
		UUID:          uuid.New().String(),
		AddressBookID: ab2.ID,
		UID:           uuid.New().String(),
		VCardData:     "BEGIN:VCARD\nFN:Alice Smith\nEND:VCARD",
		FormattedName: "Alice Smith",
		GivenName:     "Alice",
		FamilyName:    "Smith",
		Email:         "alice.smith@example.com",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	err = repo.CreateObject(ctx, c2)
	assert.NoError(t, err)

	// Contact in Book1 (No match)
	c3 := &addressbook.AddressObject{
		UUID:          uuid.New().String(),
		AddressBookID: ab1.ID,
		UID:           uuid.New().String(),
		VCardData:     "BEGIN:VCARD\nFN:Bob Jones\nEND:VCARD",
		FormattedName: "Bob Jones",
		GivenName:     "Bob",
		FamilyName:    "Jones",
		Email:         "bob@example.com",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	err = repo.CreateObject(ctx, c3)
	assert.NoError(t, err)

	// Test 1: Global Search for "Alice" (Should return 2)
	results, err := repo.SearchObjects(ctx, 1, "Alice", nil, 10)
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	// Test 2: Search for "Alice" in Book1 (Should return 1)
	results, err = repo.SearchObjects(ctx, 1, "Alice", &ab1.ID, 10)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	if len(results) > 0 {
		assert.Equal(t, c1.ID, results[0].ID)
	}

	// Test 3: Search for "Alice" in Book2 (Should return 1)
	results, err = repo.SearchObjects(ctx, 1, "Alice", &ab2.ID, 10)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	if len(results) > 0 {
		assert.Equal(t, c2.ID, results[0].ID)
	}

	// Test 4: Search for "Bob" in Book2 (Should return 0)
	results, err = repo.SearchObjects(ctx, 1, "Bob", &ab2.ID, 10)
	assert.NoError(t, err)
	assert.Len(t, results, 0)
}
