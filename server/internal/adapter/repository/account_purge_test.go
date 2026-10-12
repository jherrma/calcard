package repository_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// accountModels is every table that holds per-account data, in the order
// AutoMigrate needs them.
var accountModels = []any{
	&user.User{}, &user.EmailVerification{}, &user.RefreshToken{}, &user.PasswordReset{},
	&user.AppPassword{}, &user.OAuthConnection{}, &user.UserPreference{},
	&calendar.Calendar{}, &calendar.CalendarObject{}, &calendar.SyncChangeLog{}, &calendar.CalendarSubscription{},
	&addressbook.AddressBook{}, &addressbook.AddressObject{}, &addressbook.ContactPhoto{}, &addressbook.SyncChangeLog{},
	&user.CalDAVCredential{}, &user.CardDAVCredential{}, &user.MCPToken{},
	&sharing.CalendarShare{}, &sharing.AddressBookShare{},
}

// openAccountDB opens a file-backed SQLite DB with foreign keys enforced, as
// production does (an in-memory DB would give each pooled connection its own
// database, and FK enforcement is what makes the delete order matter).
func openAccountDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "purge.db") + "?_foreign_keys=on"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(accountModels...))
	return db
}

type seededAccount struct {
	user *user.User
	cal  *calendar.Calendar
	book *addressbook.AddressBook
}

// seedAccount gives a user one of everything an account can own: a calendar
// with an event, a change-log row, a public link and a subscription; an address
// book with a contact, a photo and a change-log row; credentials, tokens, a
// preference and an OAuth link.
func seedAccount(t *testing.T, db *gorm.DB, name string) seededAccount {
	t.Helper()
	u := &user.User{UUID: "u-" + name, Email: name + "@example.test", Username: name, PasswordHash: "x", IsActive: true}
	require.NoError(t, db.Create(u).Error)

	token := "pub-" + name
	cal := &calendar.Calendar{
		UUID: "cal-" + name, UserID: u.ID, Path: "cal-" + name, Name: "Cal " + name,
		Color: "#123456", Timezone: "UTC", SupportedComponents: "VEVENT",
		PublicToken: &token, PublicEnabled: true,
	}
	require.NoError(t, db.Create(cal).Error)
	require.NoError(t, db.Create(&calendar.CalendarObject{
		UUID: "obj-" + name, CalendarID: cal.ID, Path: "e.ics", UID: "e@" + name,
		ETag: "1", ComponentType: "VEVENT", ICalData: "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
	}).Error)
	require.NoError(t, db.Create(&calendar.SyncChangeLog{CalendarID: cal.ID, ResourcePath: "e.ics", ChangeType: "created", SyncToken: "t-" + name}).Error)
	require.NoError(t, db.Create(&calendar.CalendarSubscription{
		UUID: "sub-" + name, CalendarID: cal.ID, UserID: u.ID, URL: "https://feed.example/" + name,
		RefreshInterval: time.Hour, NextSyncAt: time.Now(), Enabled: true,
	}).Error)

	book := &addressbook.AddressBook{UUID: "ab-" + name, UserID: u.ID, Path: "ab-" + name, Name: "AB " + name, SyncToken: "s", CTag: "c"}
	require.NoError(t, db.Create(book).Error)
	contact := &addressbook.AddressObject{
		UUID: "con-" + name, AddressBookID: book.ID, Path: "c.vcf", UID: "c@" + name,
		VCardData: "BEGIN:VCARD\r\nFN:X\r\nEND:VCARD\r\n", FormattedName: "X",
	}
	require.NoError(t, db.Create(contact).Error)
	require.NoError(t, db.Create(&addressbook.ContactPhoto{AddressObjectID: contact.ID, PhotoData: "AA==", PhotoType: "JPEG"}).Error)
	require.NoError(t, db.Create(&addressbook.SyncChangeLog{AddressBookID: book.ID, ResourcePath: "c.vcf", ChangeType: "created", SyncToken: "t-" + name}).Error)

	require.NoError(t, db.Create(&user.AppPassword{UUID: "ap-" + name, UserID: u.ID, Name: "ap", PasswordHash: "x"}).Error)
	require.NoError(t, db.Create(&user.CalDAVCredential{UUID: "cd-" + name, UserID: u.ID, Name: "cd", Username: "cd-" + name, PasswordHash: "x"}).Error)
	require.NoError(t, db.Create(&user.CardDAVCredential{UUID: "cr-" + name, UserID: u.ID, Name: "cr", Username: "cr-" + name, PasswordHash: "x"}).Error)
	require.NoError(t, db.Create(&user.MCPToken{UUID: "mcp-" + name, UserID: u.ID, Name: "mcp", TokenHash: "h-" + name, TokenPrefix: "p"}).Error)
	require.NoError(t, db.Create(&user.OAuthConnection{UserID: u.ID, Provider: "google", ProviderID: "g-" + name}).Error)
	require.NoError(t, db.Create(&user.UserPreference{UserID: u.ID, Key: "theme", Value: "dark"}).Error)
	return seededAccount{user: u, cal: cal, book: book}
}

// accountRows counts, per table, the rows that belong to (or were granted to)
// the account — soft-deleted rows included.
func accountRows(t *testing.T, db *gorm.DB, a seededAccount) map[string]int64 {
	t.Helper()
	count := func(model any, where string, args ...any) int64 {
		var n int64
		require.NoError(t, db.Unscoped().Model(model).Where(where, args...).Count(&n).Error)
		return n
	}
	uid, cid, bid := a.user.ID, a.cal.ID, a.book.ID
	return map[string]int64{
		"calendars":              count(&calendar.Calendar{}, "user_id = ?", uid),
		"calendar_objects":       count(&calendar.CalendarObject{}, "calendar_id = ?", cid),
		"sync_change_logs":       count(&calendar.SyncChangeLog{}, "calendar_id = ?", cid),
		"calendar_subscriptions": count(&calendar.CalendarSubscription{}, "user_id = ?", uid),
		"calendar_shares":        count(&sharing.CalendarShare{}, "calendar_id = ? OR shared_with_id = ?", cid, uid),
		"address_books":          count(&addressbook.AddressBook{}, "user_id = ?", uid),
		"address_objects":        count(&addressbook.AddressObject{}, "address_book_id = ?", bid),
		"contact_photos":         count(&addressbook.ContactPhoto{}, "address_object_id IN (SELECT id FROM address_objects WHERE address_book_id = ?)", bid),
		"addressbook_changelogs": count(&addressbook.SyncChangeLog{}, "address_book_id = ?", bid),
		"addressbook_shares":     count(&sharing.AddressBookShare{}, "address_book_id = ? OR shared_with_id = ?", bid, uid),
		"app_passwords":          count(&user.AppPassword{}, "user_id = ?", uid),
		"caldav_credentials":     count(&user.CalDAVCredential{}, "user_id = ?", uid),
		"carddav_credentials":    count(&user.CardDAVCredential{}, "user_id = ?", uid),
		"mcp_tokens":             count(&user.MCPToken{}, "user_id = ?", uid),
		"oauth_connections":      count(&user.OAuthConnection{}, "user_id = ?", uid),
		"user_preferences":       count(&user.UserPreference{}, "user_id = ?", uid),
	}
}

// shareBothWays makes each account share its calendar and address book with
// the other, so the deleted account is both an owner and a sharee.
func shareBothWays(t *testing.T, db *gorm.DB, a, b seededAccount) {
	t.Helper()
	for i, pair := range [][2]seededAccount{{a, b}, {b, a}} {
		owner, sharee := pair[0], pair[1]
		require.NoError(t, db.Create(&sharing.CalendarShare{UUID: fmt.Sprintf("cs-%d", i), CalendarID: owner.cal.ID, SharedWithID: sharee.user.ID, Permission: "read"}).Error)
		require.NoError(t, db.Create(&sharing.AddressBookShare{UUID: fmt.Sprintf("as-%d", i), AddressBookID: owner.book.ID, SharedWithID: sharee.user.ID, Permission: "read"}).Error)
	}
}

// F18: deleting an account must remove everything it owns and every share
// granted to it, so nothing stays reachable through a sharee, a public link
// or a credential — and must leave every other account untouched.
func TestUserRepoDelete_PurgesAccountData(t *testing.T) {
	db := openAccountDB(t)
	ctx := context.Background()

	gone := seedAccount(t, db, "gone")
	kept := seedAccount(t, db, "kept")
	shareBothWays(t, db, gone, kept)

	// An already soft-deleted calendar of the account must go as well.
	stale := &calendar.Calendar{UUID: "cal-stale", UserID: gone.user.ID, Path: "stale", Name: "Stale", Color: "#000000", Timezone: "UTC", SupportedComponents: "VEVENT"}
	require.NoError(t, db.Create(stale).Error)
	require.NoError(t, db.Delete(stale).Error)

	before := accountRows(t, db, gone)
	for table, n := range before {
		require.NotZerof(t, n, "seed: %s must hold rows of the account before deletion", table)
	}

	require.NoError(t, repository.NewUserRepository(db).Delete(ctx, gone.user.ID))

	for table, n := range accountRows(t, db, gone) {
		assert.Zerof(t, n, "%s: the deleted account's rows must be hard-deleted", table)
	}
	var cal calendar.Calendar
	assert.ErrorIs(t, db.Where("public_token = ?", "pub-gone").First(&cal).Error, gorm.ErrRecordNotFound,
		"the public link must die with the calendar")

	// The user row itself stays soft-deleted (the setup gate counts it).
	var n int64
	require.NoError(t, db.Unscoped().Model(&user.User{}).Where("id = ? AND deleted_at IS NOT NULL", gone.user.ID).Count(&n).Error)
	assert.EqualValues(t, 1, n)

	// The other account keeps everything except the shares that involved the
	// deleted one: one share of each kind went in each direction.
	after := accountRows(t, db, kept)
	for table, n := range after {
		want := int64(1)
		if table == "calendar_shares" || table == "addressbook_shares" {
			want = 0
		}
		assert.Equalf(t, want, n, "%s of the remaining account", table)
	}
}
