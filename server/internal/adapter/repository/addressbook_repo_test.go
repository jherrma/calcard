package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPhotoSeparation(t *testing.T) {
	// Setup DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&addressbook.AddressBook{}, &addressbook.AddressObject{}, &addressbook.ContactPhoto{})

	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	// data
	uid := uuid.New().String()
	objUUID := uuid.New().String()
	// vCard with Photo
	vcardWithPhoto := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:Photo Tester\r\nPHOTO;ENCODING=b;TYPE=JPEG:SGVsbG8=\r\nEND:VCARD\r\n"

	obj := &addressbook.AddressObject{
		UUID:          objUUID,
		AddressBookID: 1,
		UID:           uid,
		VCardData:     vcardWithPhoto,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// 1. Create
	err = repo.CreateObject(ctx, obj)
	assert.NoError(t, err)

	// 2. Verify Database State
	var savedObj addressbook.AddressObject
	err = db.Where("uuid = ?", objUUID).First(&savedObj).Error
	assert.NoError(t, err)

	// Expect photo stripped from vCardData in DB
	assert.NotContains(t, savedObj.VCardData, "PHOTO;ENCODING=b;TYPE=JPEG:SGVsbG8=")
	assert.NotContains(t, savedObj.VCardData, "SGVsbG8=") // Ensure data gone

	var savedPhoto addressbook.ContactPhoto
	err = db.Where("address_object_id = ?", savedObj.ID).First(&savedPhoto).Error
	assert.NoError(t, err)
	assert.Equal(t, "SGVsbG8=", savedPhoto.PhotoData)
	assert.Equal(t, "JPEG", savedPhoto.PhotoType)

	// 3. Verify Retrieval Injection
	retrievedObj, err := repo.GetObjectByUUID(ctx, objUUID)
	assert.NoError(t, err)
	assert.NotNil(t, retrievedObj)

	// Expect photo INJECTED back
	// The implementation adds PHOTO field. Order might differ but content should exist.
	assert.Contains(t, retrievedObj.VCardData, "PHOTO")
	assert.Contains(t, retrievedObj.VCardData, "SGVsbG8=")
	assert.Contains(t, retrievedObj.VCardData, "TYPE=JPEG")

	// 4. Update (Remove Photo)
	// Update with vCard WITHOUT photo
	vcardNoPhoto := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:Photo Tester Updated\r\nEND:VCARD\r\n"
	retrievedObj.VCardData = vcardNoPhoto
	err = repo.UpdateObject(ctx, retrievedObj)
	assert.NoError(t, err)

	// Verify Photo removed from DB
	err = db.Where("address_object_id = ?", savedObj.ID).First(&savedPhoto).Error
	assert.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}
