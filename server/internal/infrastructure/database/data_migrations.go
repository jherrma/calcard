package database

import (
	"fmt"

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
