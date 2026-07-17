package database

import (
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
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
	return nil
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
