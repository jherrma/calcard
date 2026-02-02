package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListObjectsPagination(t *testing.T) {
	// Setup DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&addressbook.AddressBook{}, &addressbook.AddressObject{}, &addressbook.ContactPhoto{})

	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	// Seed 25 objects
	abID := uint(1)
	for i := 1; i <= 25; i++ {
		name := fmt.Sprintf("Contact %02d", i)
		obj := &addressbook.AddressObject{
			UUID:          uuid.New().String(),
			AddressBookID: abID,
			UID:           uuid.New().String(),
			VCardData:     "BEGIN:VCARD\nFN:" + name + "\nN:Test;" + fmt.Sprintf("Contact%02d", i) + ";;;\nEND:VCARD",
			FormattedName: name, // Denormalized field used for sorting
			GivenName:     fmt.Sprintf("Contact%02d", i),
			FamilyName:    "Test",
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		err = repo.CreateObject(ctx, obj)
		assert.NoError(t, err)
	}

	// Test 1: First Page (Limit 10, Offset 0)
	objs, total, err := repo.ListObjects(ctx, abID, 10, 0, "name", "asc")
	assert.NoError(t, err)
	assert.Equal(t, int64(25), total)
	assert.Len(t, objs, 10)
	assert.Equal(t, "Contact 01", objs[0].FormattedName)
	assert.Equal(t, "Contact 10", objs[9].FormattedName)

	// Test 2: Second Page (Limit 10, Offset 10)
	objs, total, err = repo.ListObjects(ctx, abID, 10, 10, "name", "asc")
	assert.NoError(t, err)
	assert.Equal(t, int64(25), total)
	assert.Len(t, objs, 10)
	assert.Equal(t, "Contact 11", objs[0].FormattedName)
	assert.Equal(t, "Contact 20", objs[9].FormattedName)

	// Test 3: Last Page (Limit 10, Offset 20)
	objs, total, err = repo.ListObjects(ctx, abID, 10, 20, "name", "asc")
	assert.NoError(t, err)
	assert.Equal(t, int64(25), total)
	assert.Len(t, objs, 5) // Remaining 5
	assert.Equal(t, "Contact 21", objs[0].FormattedName)
	assert.Equal(t, "Contact 25", objs[4].FormattedName)

	// Test 4: Sorting DESC (By First Name)
	objs, total, err = repo.ListObjects(ctx, abID, 10, 0, "name", "desc")
	assert.NoError(t, err)
	assert.Equal(t, int64(25), total)
	assert.Len(t, objs, 10)

	// Descending order: Contact25, Contact24, ..., Contact16
	assert.Equal(t, "Contact25", objs[0].GivenName)
	assert.Equal(t, "Contact16", objs[9].GivenName)

}
