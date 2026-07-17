package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestMoveObjectDoesNotDuplicatePhoto reproduces the regression where moving a
// contact between address books stored the photo-hydrated vCard verbatim,
// leaving the PHOTO both inline in address_objects AND in the contact_photos
// side table — so every subsequent read injected a second PHOTO. The move path
// must re-strip the photo the same way Create/Update do.
func TestMoveObjectDoesNotDuplicatePhoto(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&addressbook.AddressBook{},
		&addressbook.AddressObject{},
		&addressbook.ContactPhoto{},
		&addressbook.SyncChangeLog{},
	))

	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	// Two address books to move between.
	source := &addressbook.AddressBook{UUID: uuid.New().String(), UserID: 1, Name: "Source"}
	target := &addressbook.AddressBook{UUID: uuid.New().String(), UserID: 1, Name: "Target"}
	require.NoError(t, repo.Create(ctx, source))
	require.NoError(t, repo.Create(ctx, target))

	uid := uuid.New().String()
	objUUID := uuid.New().String()
	vcardWithPhoto := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:Photo Tester\r\nPHOTO;ENCODING=b;TYPE=JPEG:SGVsbG8=\r\nEND:VCARD\r\n"

	obj := &addressbook.AddressObject{
		UUID:          objUUID,
		AddressBookID: source.ID,
		UID:           uid,
		Path:          "photo.vcf",
		VCardData:     vcardWithPhoto,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	require.NoError(t, repo.CreateObject(ctx, obj))

	// The use case loads the object through the hydrating getter, which
	// re-inlines the PHOTO into VCardData, then hands it to MoveObject.
	hydrated, err := repo.GetObjectByUUID(ctx, objUUID)
	require.NoError(t, err)
	require.NotNil(t, hydrated)
	assert.Equal(t, 1, strings.Count(hydrated.VCardData, "SGVsbG8="), "getter should inject exactly one photo")

	hydrated.AddressBookID = target.ID
	require.NoError(t, repo.MoveObject(ctx, hydrated, source.ID))

	// The persisted row must NOT contain the inline photo any more.
	var stored addressbook.AddressObject
	require.NoError(t, db.Where("uuid = ?", objUUID).First(&stored).Error)
	assert.NotContains(t, stored.VCardData, "SGVsbG8=", "stored vCard must have PHOTO stripped after move")
	assert.Equal(t, len(stored.VCardData), stored.ContentLength, "ContentLength must match stripped body")

	// Exactly one photo row survives (keyed on the unchanged object ID).
	var photoCount int64
	require.NoError(t, db.Model(&addressbook.ContactPhoto{}).
		Where("address_object_id = ?", stored.ID).Count(&photoCount).Error)
	assert.Equal(t, int64(1), photoCount)

	// A subsequent read must inject exactly ONE photo, not two.
	retrieved, err := repo.GetObjectByUUID(ctx, objUUID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, 1, strings.Count(retrieved.VCardData, "SGVsbG8="),
		"read after move must contain exactly one PHOTO, not a duplicate")
	assert.Equal(t, target.ID, retrieved.AddressBookID)
}
