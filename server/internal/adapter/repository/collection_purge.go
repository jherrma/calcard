package repository

import (
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"gorm.io/gorm"
)

// The collection cascade: what "delete a calendar / address book" removes.
//
// Deleting a collection used to soft-delete only its own row. Its events or
// contacts, photos, change log, shares and subscription stayed in the database
// with deleted_at NULL — unreachable through every API, but retained until the
// whole account was deleted. A collection delete now removes all of it, for
// good, in the caller's transaction.
//
// Every way of deleting a collection ends here: CalendarRepository.Delete and
// AddressBookRepository.Delete (REST, MCP, a DAV client's DELETE on the
// collection, the subscription endpoints), PurgeAccountData (account deletion,
// F18) and PurgeDeletedCollections (the data migration for collections deleted
// before). A table that gains a calendar_id / address_book_id — or hangs off
// one that has it — must be added to the lists below, or deleted collections
// leak its rows.
//
// Order matters: children before their parents, because SQLite
// (_foreign_keys=on) and PostgreSQL enforce the GORM foreign keys
// (calendar_shares and calendar_subscriptions reference calendars;
// addressbook_shares and address_objects reference address_books). Every
// delete is Unscoped, so rows the collection had already soft-deleted (a
// deleted event, say) go too.
//
// The collection row itself is hard-deleted as well, last (lockCollections
// marks it first; see there). Nothing reads a soft-deleted collection — every
// lookup (DAV path, UUID, id, public token, share preload) uses the default
// scope — so the tombstone protected nothing, while it kept the public token's
// unique index slot taken. Ids are not reused: SQLite tables are AUTOINCREMENT,
// PostgreSQL uses sequences.

// collectionMatch selects collections by id for one cascade: a column is
// compared with op ("IN" or "NOT IN") against ids — a single id, a slice of
// ids or a subquery selecting them.
type collectionMatch struct {
	op  string
	ids any
}

// where renders the predicate for column.
func (m collectionMatch) where(column string) string {
	return column + " " + m.op + " (?)"
}

// purgeCounts collects, per table, how many rows a cascade removed. A nil
// purgeCounts counts nothing.
type purgeCounts map[string]int64

// hardDelete removes the rows of model matching query, Unscoped, and records
// how many went.
func hardDelete(tx *gorm.DB, counts purgeCounts, model any, query string, args ...any) error {
	res := tx.Unscoped().Where(query, args...).Delete(model)
	if res.Error != nil {
		return res.Error
	}
	if counts != nil && res.RowsAffected > 0 {
		counts[res.Statement.Table] += res.RowsAffected
	}
	return nil
}

// calendarContents is every table holding rows that belong to a calendar,
// keyed on calendar_id, children before parents. The public link has no table
// of its own: its token lives on the calendar row.
var calendarContents = []any{
	&sharing.CalendarShare{},
	&calendar.CalendarSubscription{},
	&calendar.CalendarObject{},
	&calendar.SyncChangeLog{},
}

// deleteCalendarContents hard-deletes everything that belongs to the
// calendars selected by m — but not the calendar rows themselves.
func deleteCalendarContents(tx *gorm.DB, counts purgeCounts, m collectionMatch) error {
	for _, model := range calendarContents {
		if err := hardDelete(tx, counts, model, m.where("calendar_id"), m.ids); err != nil {
			return err
		}
	}
	return nil
}

// purgeCalendars hard-deletes the calendars whose id is in ids (a single id, a
// slice or a subquery) together with everything in them. Run it inside a
// transaction: half a cascade is worse than none.
func purgeCalendars(tx *gorm.DB, counts purgeCounts, ids any) error {
	if err := lockCollections(tx, &calendar.Calendar{}, ids); err != nil {
		return err
	}
	if err := deleteCalendarContents(tx, counts, collectionMatch{op: "IN", ids: ids}); err != nil {
		return err
	}
	return hardDelete(tx, counts, &calendar.Calendar{}, "id IN (?)", ids)
}

// lockCollections is the first write of a collection cascade: it marks the
// collection rows deleted (GORM's soft delete, an UPDATE of deleted_at) before
// anything in them is touched. The mark itself is never seen — the rows are
// hard-deleted at the end of the same transaction — but the UPDATE takes their
// row locks up front. On PostgreSQL an object write that has already bumped the
// collection's sync token therefore commits before the cascade reads the
// contents, instead of slipping its object in behind the cascade as an orphan
// (calendar_objects has no foreign key to stop it). On SQLite the UPDATE makes
// the transaction the writer at once. It is also where a storage failure
// surfaces first: the MCP tests inject one with a BEFORE UPDATE trigger.
func lockCollections(tx *gorm.DB, model any, ids any) error {
	return tx.Where("id IN (?)", ids).Delete(model).Error
}

// deleteAddressBookContents hard-deletes everything that belongs to the
// address books selected by m — but not the address book rows themselves.
// Photos hang off the contact, not the book, so they are matched through it
// and go before the contacts.
func deleteAddressBookContents(tx *gorm.DB, counts purgeCounts, m collectionMatch) error {
	contacts := tx.Unscoped().Model(&addressbook.AddressObject{}).Select("id").Where(m.where("address_book_id"), m.ids)
	steps := []struct {
		model any
		query string
		arg   any
	}{
		{&sharing.AddressBookShare{}, m.where("address_book_id"), m.ids},
		{&addressbook.ContactPhoto{}, "address_object_id IN (?)", contacts},
		{&addressbook.AddressObject{}, m.where("address_book_id"), m.ids},
		{&addressbook.SyncChangeLog{}, m.where("address_book_id"), m.ids},
	}
	for _, s := range steps {
		if err := hardDelete(tx, counts, s.model, s.query, s.arg); err != nil {
			return err
		}
	}
	return nil
}

// purgeAddressBooks hard-deletes the address books whose id is in ids (a single
// id, a slice or a subquery) together with everything in them. Run it inside a
// transaction.
func purgeAddressBooks(tx *gorm.DB, counts purgeCounts, ids any) error {
	if err := lockCollections(tx, &addressbook.AddressBook{}, ids); err != nil {
		return err
	}
	if err := deleteAddressBookContents(tx, counts, collectionMatch{op: "IN", ids: ids}); err != nil {
		return err
	}
	return hardDelete(tx, counts, &addressbook.AddressBook{}, "id IN (?)", ids)
}

// PurgeDeletedCollections removes what deleting a collection left behind
// before the cascade above existed: every calendar and address book row that
// is soft-deleted, and every row hanging off a collection that is not live —
// soft-deleted or missing altogether — plus photos whose contact row is gone.
// It returns how many rows went, per table.
//
// It never touches a live collection (deleted_at NULL) or anything in one: the
// contents are matched as "collection id NOT IN the live ids", the collection
// rows by deleted_at. A soft-deleted event or contact inside a live collection
// is left alone too. Idempotent: once purged there is nothing left to match.
//
// This permanently deletes data of collections their owners already deleted.
// That is the point, but it cannot be undone.
func PurgeDeletedCollections(tx *gorm.DB) (map[string]int64, error) {
	counts := purgeCounts{}

	liveCalendars := tx.Unscoped().Model(&calendar.Calendar{}).Select("id").Where("deleted_at IS NULL")
	if err := deleteCalendarContents(tx, counts, collectionMatch{op: "NOT IN", ids: liveCalendars}); err != nil {
		return nil, err
	}
	if err := hardDelete(tx, counts, &calendar.Calendar{}, "deleted_at IS NOT NULL"); err != nil {
		return nil, err
	}

	liveBooks := tx.Unscoped().Model(&addressbook.AddressBook{}).Select("id").Where("deleted_at IS NULL")
	if err := deleteAddressBookContents(tx, counts, collectionMatch{op: "NOT IN", ids: liveBooks}); err != nil {
		return nil, err
	}
	allContacts := tx.Unscoped().Model(&addressbook.AddressObject{}).Select("id")
	if err := hardDelete(tx, counts, &addressbook.ContactPhoto{}, "address_object_id NOT IN (?)", allContacts); err != nil {
		return nil, err
	}
	if err := hardDelete(tx, counts, &addressbook.AddressBook{}, "deleted_at IS NOT NULL"); err != nil {
		return nil, err
	}
	return counts, nil
}
