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

	if err := purgeAccountCredentials(tx, users); err != nil {
		return err
	}
	return hardDelete(tx, nil, &user.UserPreference{}, "user_id IN (?)", users)
}

// accountCredentials is every way into an account besides its password and
// its sessions: app passwords, CalDAV/CardDAV credentials, MCP tokens and OAuth
// links. Account deletion removes them with the rest of the data; a password
// reset removes exactly these (gormUserRepo.CompletePasswordReset), so whoever
// held the account before the reset keeps no way back in. A new kind of
// credential belongs here.
var accountCredentials = []any{
	&user.AppPassword{},
	&user.CalDAVCredential{},
	&user.CardDAVCredential{},
	&user.MCPToken{},
	&user.OAuthConnection{},
}

// purgeAccountCredentials hard-deletes accountCredentials of users (an id, a
// slice of ids or a subquery, as for PurgeAccountData).
func purgeAccountCredentials(tx *gorm.DB, users any) error {
	for _, model := range accountCredentials {
		if err := hardDelete(tx, nil, model, "user_id IN (?)", users); err != nil {
			return err
		}
	}
	return nil
}
