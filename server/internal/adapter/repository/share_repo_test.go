package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestCalendarShareRevokeFreesUniqueIndex is the regression test for H6: revoke
// must hard-delete the share row. The (calendar_id, shared_with_id) composite
// unique index is not partial on deleted_at, so a soft-deleted row would keep
// blocking any re-share of the same pair.
func TestCalendarShareRevokeFreesUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sharing.CalendarShare{}))

	repo := repository.NewCalendarShareRepository(db)
	ctx := context.Background()

	const calendarID, sharedWithID = uint(1), uint(2)
	first := &sharing.CalendarShare{UUID: uuid.New().String(), CalendarID: calendarID, SharedWithID: sharedWithID, Permission: "read"}
	require.NoError(t, repo.Create(ctx, first))

	require.NoError(t, repo.Revoke(ctx, first.ID))

	// Re-sharing the same (calendar, user) pair must now succeed.
	second := &sharing.CalendarShare{UUID: uuid.New().String(), CalendarID: calendarID, SharedWithID: sharedWithID, Permission: "read-write"}
	require.NoError(t, repo.Create(ctx, second), "re-share after revoke must not hit the unique index")

	// And the revoked row must be gone, not lingering soft-deleted.
	var count int64
	require.NoError(t, db.Unscoped().Model(&sharing.CalendarShare{}).Where("id = ?", first.ID).Count(&count).Error)
	require.Equal(t, int64(0), count, "revoked share must be hard-deleted")
}

// TestAddressBookShareRevokeFreesUniqueIndex is the address-book analogue of H6.
func TestAddressBookShareRevokeFreesUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sharing.AddressBookShare{}))

	repo := repository.NewAddressBookShareRepository(db)
	ctx := context.Background()

	const addressBookID, sharedWithID = uint(1), uint(2)
	first := &sharing.AddressBookShare{UUID: uuid.New().String(), AddressBookID: addressBookID, SharedWithID: sharedWithID, Permission: "read"}
	require.NoError(t, repo.Create(ctx, first))

	require.NoError(t, repo.Revoke(ctx, first.ID))

	second := &sharing.AddressBookShare{UUID: uuid.New().String(), AddressBookID: addressBookID, SharedWithID: sharedWithID, Permission: "read-write"}
	require.NoError(t, repo.Create(ctx, second), "re-share after revoke must not hit the unique index")

	var count int64
	require.NoError(t, db.Unscoped().Model(&sharing.AddressBookShare{}).Where("id = ?", first.ID).Count(&count).Error)
	require.Equal(t, int64(0), count, "revoked share must be hard-deleted")
}
