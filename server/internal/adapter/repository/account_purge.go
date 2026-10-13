package repository

import (
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// PurgeAccountData hard-deletes everything an account owns or was granted:
// its calendars and address books with everything in them (the collection
// cascade of collection_purge.go: objects, photos, change logs, shares,
// subscription, public link), every share granted TO the account, and its app
// passwords, DAV credentials, MCP tokens, OAuth links and preferences (F18).
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

	// What the account holds in OTHER accounts' collections: the shares granted
	// to it. A subscription always lives in the subscriber's own calendar, so
	// the calendar cascade removes it anyway; matching it by user_id as well
	// costs nothing.
	for _, s := range []struct {
		model any
		query string
	}{
		{&sharing.CalendarShare{}, "shared_with_id IN (?)"},
		{&sharing.AddressBookShare{}, "shared_with_id IN (?)"},
		{&calendar.CalendarSubscription{}, "user_id IN (?)"},
	} {
		if err := hardDelete(tx, nil, s.model, s.query, users); err != nil {
			return err
		}
	}

	if err := purgeCalendars(tx, nil, calendars); err != nil {
		return err
	}
	if err := purgeAddressBooks(tx, nil, books); err != nil {
		return err
	}

	for _, model := range []any{
		&user.AppPassword{},
		&user.CalDAVCredential{},
		&user.CardDAVCredential{},
		&user.MCPToken{},
		&user.OAuthConnection{},
		&user.UserPreference{},
	} {
		if err := hardDelete(tx, nil, model, "user_id IN (?)", users); err != nil {
			return err
		}
	}
	return nil
}
