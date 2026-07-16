package repository_test

import (
	"context"
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

// TestDeleteObjectByUUIDRemovesPhoto is the regression test for the delete path
// leaking contact_photos rows. The address object is soft-deleted, so without an
// explicit cleanup the side-table row (keyed on the object's primary key) is
// orphaned forever, retaining the deleted contact's photo blob. Every other
// write path (Create/Update/Move) keeps this table in lockstep with the object;
// delete must too.
func TestDeleteObjectByUUIDRemovesPhoto(t *testing.T) {
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
	objUUID := uuid.New().String()
	vcardWithPhoto := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:Photo Tester\r\nPHOTO;ENCODING=b;TYPE=JPEG:SGVsbG8=\r\nEND:VCARD\r\n"

	obj := &addressbook.AddressObject{
		UUID:          objUUID,
		AddressBookID: ab.ID,
		UID:           uid,
		Path:          "photo.vcf",
		VCardData:     vcardWithPhoto,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	require.NoError(t, repo.CreateObject(ctx, obj))

	// The photo was extracted to the side table on create.
	var beforeCount int64
	require.NoError(t, db.Model(&addressbook.ContactPhoto{}).
		Where("address_object_id = ?", obj.ID).Count(&beforeCount).Error)
	require.Equal(t, int64(1), beforeCount, "precondition: photo row must exist before delete")

	// Delete the contact (soft delete of the address object).
	require.NoError(t, repo.DeleteObjectByUUID(ctx, objUUID))

	// The photo side-table row must not survive the delete.
	var afterCount int64
	require.NoError(t, db.Model(&addressbook.ContactPhoto{}).
		Where("address_object_id = ?", obj.ID).Count(&afterCount).Error)
	assert.Equal(t, int64(0), afterCount, "contact_photos row must be removed when the contact is deleted")
}
