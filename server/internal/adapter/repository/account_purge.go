package repository

import (
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// PurgeAccountData hard-deletes everything an account owns or was granted:
// its calendars (objects, change logs, subscription, public link), its address
// books (contacts, photos, change logs), every share of those collections AND
// every share granted TO the account, and its app passwords, DAV credentials,
// MCP tokens, OAuth links and preferences (F18).
//
// users is a single user id, a slice of ids, or a subquery selecting ids. Run
// it inside the transaction that removes the user row, and before it: the rows
// reference users.id through foreign keys.
//
// Soft-deleting the user row alone left all of it reachable — sharees kept
// reading the calendars and /public/calendar/<token> kept serving — because no
// read path looks at the owner's deleted_at. Removing the data closes every
// such path at once instead of teaching each one about deleted owners.
//
// Everything is deleted Unscoped, so collections and objects the account had
// already soft-deleted go too. Children are deleted before their parents
// because both SQLite (_foreign_keys=on) and PostgreSQL enforce the GORM
// foreign keys.
func PurgeAccountData(tx *gorm.DB, users any) error {
	calendars := tx.Unscoped().Model(&calendar.Calendar{}).Select("id").Where("user_id IN (?)", users)
	books := tx.Unscoped().Model(&addressbook.AddressBook{}).Select("id").Where("user_id IN (?)", users)
	contacts := tx.Unscoped().Model(&addressbook.AddressObject{}).Select("id").Where("address_book_id IN (?)", books)

	steps := []struct {
		model any
		query string
		args  []any
	}{
		{&sharing.CalendarShare{}, "calendar_id IN (?) OR shared_with_id IN (?)", []any{calendars, users}},
		{&sharing.AddressBookShare{}, "address_book_id IN (?) OR shared_with_id IN (?)", []any{books, users}},
		{&calendar.CalendarSubscription{}, "user_id IN (?) OR calendar_id IN (?)", []any{users, calendars}},
		{&calendar.CalendarObject{}, "calendar_id IN (?)", []any{calendars}},
		{&calendar.SyncChangeLog{}, "calendar_id IN (?)", []any{calendars}},
		{&calendar.Calendar{}, "user_id IN (?)", []any{users}},
		{&addressbook.ContactPhoto{}, "address_object_id IN (?)", []any{contacts}},
		{&addressbook.AddressObject{}, "address_book_id IN (?)", []any{books}},
		{&addressbook.SyncChangeLog{}, "address_book_id IN (?)", []any{books}},
		{&addressbook.AddressBook{}, "user_id IN (?)", []any{users}},
		{&user.AppPassword{}, "user_id IN (?)", []any{users}},
		{&user.CalDAVCredential{}, "user_id IN (?)", []any{users}},
		{&user.CardDAVCredential{}, "user_id IN (?)", []any{users}},
		{&user.MCPToken{}, "user_id IN (?)", []any{users}},
		{&user.OAuthConnection{}, "user_id IN (?)", []any{users}},
		{&user.UserPreference{}, "user_id IN (?)", []any{users}},
	}
	for _, s := range steps {
		if err := tx.Unscoped().Where(s.query, s.args...).Delete(s.model).Error; err != nil {
			return err
		}
	}
	return nil
}
