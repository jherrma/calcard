package database

import (
	"fmt"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// RunDataMigrations applies idempotent data fix-ups that schema AutoMigrate
// cannot express. Safe to run on every boot — each step is a no-op once its
// rows are already fixed. Must run AFTER db.Migrate (it relies on the
// recurrence_end_time column existing).
func RunDataMigrations(db *gorm.DB) error {
	if err := stripETagQuotes(db); err != nil {
		return fmt.Errorf("strip etag quotes: %w", err)
	}
	if err := wrapBareICalObjects(db); err != nil {
		return fmt.Errorf("wrap bare ical objects: %w", err)
	}
	if err := backfillRecurrenceEnd(db); err != nil {
		return fmt.Errorf("backfill recurrence_end_time: %w", err)
	}
	if err := backfillCollectionAnchors(db); err != nil {
		return fmt.Errorf("backfill collection anchors: %w", err)
	}
	if err := purgeSoftDeletedShares(db); err != nil {
		return fmt.Errorf("purge soft-deleted shares: %w", err)
	}
	if err := purgeDeletedAccounts(db); err != nil {
		return fmt.Errorf("purge deleted accounts: %w", err)
	}
	if err := releaseDeletedUserIdentities(db); err != nil {
		return fmt.Errorf("release deleted user identities: %w", err)
	}
	if err := releaseRevokedCredentialUsernames(db); err != nil {
		return fmt.Errorf("release revoked credential usernames: %w", err)
	}
	return nil
}

// purgeDeletedAccounts removes the data of accounts deleted before account
// deletion cascaded to it (F18). Those accounts' users rows were soft-deleted
// but their calendars, address books, shares, public links and credentials
// stayed — still readable by sharees and through /public/calendar/<token>.
// Runs the same cascade DELETE /users/me now performs, for every soft-deleted
// user. Idempotent: once purged there is nothing left to match.
func purgeDeletedAccounts(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		deleted := tx.Unscoped().Model(&user.User{}).Select("id").Where("deleted_at IS NOT NULL")
		return repository.PurgeAccountData(tx, deleted)
	})
}

// releaseDeletedUserIdentities applies what UserRepository.Delete now does to
// accounts deleted by older code (F19): those soft-deleted rows still hold
// their e-mail and username in the non-partial unique indexes, so the address
// could never register again, and their OAuth connections still claim the
// provider identity. The e-mail and username become "deleted:<uuid>" and the
// connections are removed. Idempotent: tombstoned rows are skipped, and the
// connections are already gone.
func releaseDeletedUserIdentities(db *gorm.DB) error {
	expr := gorm.Expr("? || uuid", user.DeletedIdentityPrefix)
	if err := db.Unscoped().Model(&user.User{}).
		Where("deleted_at IS NOT NULL AND email NOT LIKE ?", user.DeletedIdentityPrefix+"%").
		UpdateColumns(map[string]interface{}{"email": expr, "username": expr}).Error; err != nil {
		return err
	}
	return db.Exec(`DELETE FROM oauth_connections WHERE user_id IN (SELECT id FROM users WHERE deleted_at IS NOT NULL)`).Error
}

// purgeSoftDeletedShares hard-deletes share rows left behind by the old
// soft-delete revoke path. Revoke/DeleteBy* now issue Unscoped() hard deletes,
// but rows soft-deleted by the previous code still occupy their
// (calendar_id, shared_with_id) / (addressbook_id, shared_with_id) slot in the
// non-partial unique index while being invisible to default-scoped lookups —
// so re-sharing that same pair hits a UNIQUE constraint and surfaces as an
// opaque 500. Removing the tombstones frees the slot. Idempotent: once the
// tombstones are gone the DELETEs match nothing.
func purgeSoftDeletedShares(db *gorm.DB) error {
	if err := db.Exec(`DELETE FROM calendar_shares WHERE deleted_at IS NOT NULL`).Error; err != nil {
		return err
	}
	return db.Exec(`DELETE FROM addressbook_shares WHERE deleted_at IS NOT NULL`).Error
}

// backfillCollectionAnchors repairs collections created before the "collection"
// anchor row was introduced. Calendar/AddressBook Create() now mints the
// initial sync token together with a change_type="collection" changelog row so
// the first incremental sync-collection REPORT can resolve that token to a real
// row. Collections that predate this change carry a stored sync_token with no
// matching changelog row: GetChangesSinceToken then returns ErrRecordNotFound
// → 403 valid-sync-token → the client full-resyncs → the SAME unanchored token
// is handed back → 403 again, forever, until an unrelated write happens to mint
// an anchored token. For every calendar and address book with a non-empty
// sync_token that has no changelog row referencing it, we insert the missing
// anchor. The EXISTS guard makes this a no-op on re-run and skips collections
// that already have any (anchor or change) row for their current token.
func backfillCollectionAnchors(db *gorm.DB) error {
	var cals []calendar.Calendar
	if err := db.Where("sync_token <> ''").Find(&cals).Error; err != nil {
		return err
	}
	for i := range cals {
		var count int64
		if err := db.Model(&calendar.SyncChangeLog{}).
			Where("calendar_id = ? AND sync_token = ?", cals[i].ID, cals[i].SyncToken).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&calendar.SyncChangeLog{
			CalendarID:   cals[i].ID,
			ResourcePath: "",
			ChangeType:   "collection",
			SyncToken:    cals[i].SyncToken,
		}).Error; err != nil {
			return err
		}
	}

	var books []addressbook.AddressBook
	if err := db.Where("sync_token <> ''").Find(&books).Error; err != nil {
		return err
	}
	for i := range books {
		var count int64
		if err := db.Model(&addressbook.SyncChangeLog{}).
			Where("address_book_id = ? AND sync_token = ?", books[i].ID, books[i].SyncToken).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&addressbook.SyncChangeLog{
			AddressBookID: books[i].ID,
			ResourcePath:  "",
			ChangeType:    "collection",
			SyncToken:     books[i].SyncToken,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// stripETagQuotes removes embedded double quotes from stored ETags. ETags are
// now stored unquoted (the transport layer adds quotes); older rows were stored
// pre-quoted, which produced double-quoted entity-tags on the wire.
func stripETagQuotes(db *gorm.DB) error {
	// GORM maps the ETag field to column "e_tag".
	if err := db.Exec(`UPDATE calendar_objects SET e_tag = REPLACE(e_tag, '"', '') WHERE e_tag LIKE '%"%'`).Error; err != nil {
		return err
	}
	return db.Exec(`UPDATE address_objects SET e_tag = REPLACE(e_tag, '"', '') WHERE e_tag LIKE '%"%'`).Error
}

// wrapBareICalObjects repairs calendar objects whose ICalData was stored as a
// bare VEVENT/VTODO block (older import code) rather than a full VCALENDAR.
// Every consumer decodes with a strict VCALENDAR decoder, so bare rows are
// otherwise unreadable.
func wrapBareICalObjects(db *gorm.DB) error {
	var batch []calendar.CalendarObject
	return db.Unscoped().
		Where("i_cal_data NOT LIKE 'BEGIN:VCALENDAR%'").
		FindInBatches(&batch, 200, func(tx *gorm.DB, _ int) error {
			for i := range batch {
				wrapped := calendar.EnsureVCalendarWrapper(batch[i].ICalData)
				if err := tx.Model(&calendar.CalendarObject{}).
					Where("id = ?", batch[i].ID).
					UpdateColumns(map[string]interface{}{
						"i_cal_data":     wrapped,
						"content_length": len(wrapped),
					}).Error; err != nil {
					return err
				}
			}
			return nil
		}).Error
}

// backfillRecurrenceEnd populates recurrence_end_time for existing recurring
// objects so they remain visible in listing windows past their first
// occurrence. Rows that merely contain the substring "RRULE" but don't parse
// stay NULL and are harmlessly re-scanned on the next boot.
func backfillRecurrenceEnd(db *gorm.DB) error {
	var batch []calendar.CalendarObject
	return db.Unscoped().
		Where("recurrence_end_time IS NULL AND i_cal_data LIKE '%RRULE%'").
		FindInBatches(&batch, 200, func(tx *gorm.DB, _ int) error {
			for i := range batch {
				obj := batch[i]
				if err := obj.PopulateDenormFieldsFromICal(); err != nil {
					// Skip undecodable rows; don't abort the whole migration.
					continue
				}
				if obj.RecurrenceEndTime == nil {
					continue
				}
				if err := tx.Model(&calendar.CalendarObject{}).
					Where("id = ?", obj.ID).
					UpdateColumns(map[string]interface{}{
						"recurrence_end_time": obj.RecurrenceEndTime,
					}).Error; err != nil {
					return err
				}
			}
			return nil
		}).Error
}

// releaseRevokedCredentialUsernames frees the usernames of CalDAV/CardDAV
// credentials revoked before Revoke started doing it itself (F24). Revoke only
// stamped revoked_at, so the row kept its name in the non-partial unique
// index while the duplicate check (which skips revoked rows) let a new
// credential with that name through to an INSERT that always failed. The name
// is rewritten to the same "revoked:<uuid>" tombstone Revoke now writes.
// Idempotent: tombstoned rows no longer match the NOT LIKE guard.
func releaseRevokedCredentialUsernames(db *gorm.DB) error {
	prefix := user.RevokedCredentialUsernamePrefix
	for _, table := range []string{"caldav_credentials", "card_dav_credentials"} {
		if err := db.Exec(`UPDATE `+table+` SET username = '`+prefix+`' || uuid WHERE revoked_at IS NOT NULL AND username NOT LIKE ?`,
			prefix+"%").Error; err != nil {
			return err
		}
	}
	return nil
}
