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

// TestListObjectsSkipsPhotoHydration is the regression test for #51: an
// ETag-only PROPFIND poll (flagged via WithSkipPhotoHydration) must not hydrate
// PHOTO blobs, while ETag/ContentLength still reflect the stored row. The
// default path keeps hydrating photos.
func TestListObjectsSkipsPhotoHydration(t *testing.T) {
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

	ab := &addressbook.AddressBook{UUID: uuid.New().String(), UserID: 1, Name: "Book"}
	require.NoError(t, repo.Create(ctx, ab))

	uid := uuid.New().String()
	vcardWithPhoto := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid +
		"\r\nFN:Photo Tester\r\nPHOTO;ENCODING=b;TYPE=JPEG:SGVsbG8=\r\nEND:VCARD\r\n"
	obj := &addressbook.AddressObject{
		UUID:          uuid.New().String(),
		AddressBookID: ab.ID,
		UID:           uid,
		Path:          "photo.vcf",
		ETag:          "etag-photo-1",
		VCardData:     vcardWithPhoto,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	require.NoError(t, repo.CreateObject(ctx, obj))

	// Default path: PHOTO is hydrated back into the served body.
	hydrated, _, err := repo.ListObjects(ctx, ab.ID, -1, 0, "", "")
	require.NoError(t, err)
	require.Len(t, hydrated, 1)
	assert.Contains(t, hydrated[0].VCardData, "PHOTO", "default listing must hydrate the photo")

	// Skip-hydration path: no PHOTO, but ETag and ContentLength still come from
	// the stored row (ContentLength == the stripped body length).
	skipCtx := addressbook.WithSkipPhotoHydration(ctx)
	skipped, _, err := repo.ListObjects(skipCtx, ab.ID, -1, 0, "", "")
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	assert.NotContains(t, skipped[0].VCardData, "PHOTO", "skip path must not hydrate the photo")
	assert.Equal(t, "etag-photo-1", skipped[0].ETag, "ETag must survive the skip path")
	assert.Equal(t, len(skipped[0].VCardData), skipped[0].ContentLength,
		"ContentLength must match the served (stripped) body")
	assert.Less(t, skipped[0].ContentLength, hydrated[0].ContentLength,
		"stripped body must be smaller than the hydrated one")
	// Sanity: the stripped body is still a valid-looking card.
	assert.True(t, strings.Contains(skipped[0].VCardData, "FN:Photo Tester"))
}
