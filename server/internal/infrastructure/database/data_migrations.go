package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RunDataMigrations applies idempotent data fix-ups that schema AutoMigrate
// cannot express. Safe to run on every boot — each step is a no-op once its
// rows are already fixed, and narrowCalendarComponentSets runs only once ever
// (its system_settings marker). Must run AFTER db.Migrate (it relies on the
// recurrence_end_time and task columns existing).
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
	if err := backfillTaskFields(db); err != nil {
		return fmt.Errorf("backfill task fields: %w", err)
	}
	if err := unescapeTextColumns(db); err != nil {
		return fmt.Errorf("unescape text columns: %w", err)
	}
	now := time.Now().UTC()
	if err := recordTaskKindCutoff(db, now); err != nil {
		return fmt.Errorf("record %s: %w", taskKindIntroducedAtKey, err)
	}
	if err := narrowCalendarComponentSets(db, now); err != nil {
		return fmt.Errorf("narrow calendar component sets: %w", err)
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
	if err := purgeDeletedCollections(db); err != nil {
		return fmt.Errorf("purge deleted collections: %w", err)
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

// purgeDeletedCollections removes the contents of calendars and address books
// deleted before deleting a collection removed them. Those deletes only
// soft-deleted the collection row; its events or contacts, photos, change log,
// shares and subscription stayed in the database, unreachable but retained.
// Runs the same cascade a collection delete now performs
// (repository.PurgeDeletedCollections): the soft-deleted collection rows go,
// and so does every row whose collection is not live (soft-deleted or missing).
// Live collections and everything in them are never touched.
//
// This PERMANENTLY deletes data. Its owners asked for it to be gone, but it
// cannot be undone — back the database up before upgrading if in doubt.
// Idempotent: once purged there is nothing left to match, and nothing is
// logged.
func purgeDeletedCollections(db *gorm.DB) error {
	var counts map[string]int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		counts, err = repository.PurgeDeletedCollections(tx)
		return err
	}); err != nil {
		return err
	}
	if len(counts) == 0 {
		return nil
	}
	tables := make([]string, 0, len(counts))
	for table := range counts {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	attrs := make([]any, 0, 2*len(tables))
	for _, table := range tables {
		attrs = append(attrs, table, counts[table])
	}
	slog.Warn("data migration: permanently deleted the remains of calendars and address books deleted earlier (rows per table)", attrs...)
	return nil
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

// backfillTaskFields fills the task columns (#170: status, due_at,
// completed_at, percent_complete, priority) of VTODOs stored before
// PopulateDenormFieldsFromICal derived them, so tasks synced before the upgrade
// list correctly. Soft-deleted rows are included (Unscoped), like the other
// backfills.
//
// The write-back is UpdateColumns of the derived columns only — never e_tag or
// updated_at: the object's content is unchanged, and a new ETag would make every
// DAV client re-download its tasks. is_all_day, start_time and end_time are
// rewritten too, because for a VTODO IsAllDay now follows DUE (else DTSTART).
//
// Idempotent: a VTODO always gets a non-empty status, so a filled row no longer
// matches. Undecodable rows (and rows whose stored component_type disagrees
// with their iCalendar) keep an empty status, are skipped and are harmlessly
// re-read on the next boot.
func backfillTaskFields(db *gorm.DB) error {
	var batch []calendar.CalendarObject
	return db.Unscoped().
		Where("component_type = ? AND (status IS NULL OR status = '')", calendar.ComponentToDo).
		FindInBatches(&batch, 200, func(tx *gorm.DB, _ int) error {
			for i := range batch {
				obj := batch[i]
				if err := obj.PopulateDenormFieldsFromICal(); err != nil {
					// Skip undecodable rows; don't abort the whole migration.
					continue
				}
				if obj.Status == "" {
					// The iCalendar's primary component is not a VTODO.
					continue
				}
				// FindInBatches hands the callback a fresh session: the
				// query's Unscoped does not carry over, and without it the
				// UPDATE would skip soft-deleted rows.
				if err := tx.Unscoped().Model(&calendar.CalendarObject{}).
					Where("id = ?", obj.ID).
					UpdateColumns(map[string]interface{}{
						"status":           obj.Status,
						"due_at":           obj.DueAt,
						"completed_at":     obj.CompletedAt,
						"percent_complete": obj.PercentComplete,
						"priority":         obj.Priority,
						"is_all_day":       obj.IsAllDay,
						"start_time":       obj.StartTime,
						"end_time":         obj.EndTime,
					}).Error; err != nil {
					return err
				}
			}
			return nil
		}).Error
}

// unescapeTextColumns re-derives the summary, description and location columns
// of objects stored while PopulateDenormFieldsFromICal copied the iCalendar's
// escaped TEXT value into them (`Buy milk\, eggs`, a newline as `\n`). The API
// showed those backslashes, and a client that sent the text back unchanged had
// it escaped again on every save. Soft-deleted rows are included (Unscoped),
// like the other backfills.
//
// Only rows with a backslash in one of the three columns can be affected, so
// only those are read; each is re-derived from its ICalData and written back
// with UpdateColumns of the three columns alone — never e_tag or updated_at:
// the iCalendar is unchanged, and a new ETag would make every DAV client
// re-download it. Idempotent: a re-derived value is what the column already
// holds, and such a row is not written again. A row whose plain text really
// contains a backslash stays selected and is re-read on every boot (rare and
// harmless); undecodable rows are skipped the same way.
func unescapeTextColumns(db *gorm.DB) error {
	const hasBackslash = `%\\%` // LIKE … ESCAPE '\': a literal backslash anywhere
	var batch []calendar.CalendarObject
	return db.Unscoped().
		Where(`summary LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\' OR location LIKE ? ESCAPE '\'`,
			hasBackslash, hasBackslash, hasBackslash).
		FindInBatches(&batch, 200, func(tx *gorm.DB, _ int) error {
			for i := range batch {
				stored := batch[i]
				obj := stored
				if err := obj.PopulateDenormFieldsFromICal(); err != nil {
					// Skip undecodable rows; don't abort the whole migration.
					continue
				}
				if obj.Summary == stored.Summary && obj.Description == stored.Description && obj.Location == stored.Location {
					continue
				}
				// FindInBatches hands the callback a fresh session: Unscoped
				// again, or the UPDATE skips soft-deleted rows.
				if err := tx.Unscoped().Model(&calendar.CalendarObject{}).
					Where("id = ?", obj.ID).
					UpdateColumns(map[string]interface{}{
						"summary":     obj.Summary,
						"description": obj.Description,
						"location":    obj.Location,
					}).Error; err != nil {
					return err
				}
			}
			return nil
		}).Error
}

// taskKindIntroducedAtKey is the system_settings key holding the moment the
// calendar `kind` concept (#170) reached this database: RFC 3339 with
// nanoseconds, UTC (time.RFC3339Nano). Calendars created before it predate
// `kind` and may hold a legacy "VEVENT,VTODO" component set nobody chose;
// everything created after it was a deliberate choice. The one-shot
// component-set narrowing migration only considers calendars created before
// it.
const taskKindIntroducedAtKey = "tasks.kind_introduced_at"

// recordTaskKindCutoff writes taskKindIntroducedAtKey = now if absent. It runs
// on every boot, but only the first one writes: an existing value is never
// overwritten (INSERT … ON CONFLICT DO NOTHING, so two instances booting at
// once cannot race either).
func recordTaskKindCutoff(db *gorm.DB, now time.Time) error {
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&domain.SystemSetting{
		Key:   taskKindIntroducedAtKey,
		Value: now.UTC().Format(time.RFC3339Nano),
	}).Error
}

// narrowComponentSetsMarkerKey is the system_settings key that records that
// narrowCalendarComponentSets has run (value: when, RFC 3339 UTC). Its presence
// alone makes the step a no-op.
const narrowComponentSetsMarkerKey = "migration.narrow_component_sets.v1"

// legacyComponentSet is the component set every calendar was created with
// before #170, whether its owner wanted events, tasks or both.
const legacyComponentSet = "VEVENT,VTODO"

// narrowCalendarComponentSets gives each legacy calendar the component set its
// contents show it is used for (#170), so task apps stop listing every calendar
// as a task list and calendar apps every task list as a calendar. A candidate
// is a live calendar created before the tasks.kind_introduced_at cutoff whose
// set is exactly "VEVENT,VTODO" and that is not a subscription (a feed mirror
// stays mixed: feed sync stores both components). Counting its live objects
// (soft-deleted ones do not count):
//
//   - only VEVENTs: set "VEVENT";
//   - only VTODOs: set "VTODO", even for the user's first calendar;
//   - both, or none: left "VEVENT,VTODO" (mixed, or intent unknown);
//   - any object of another component type: left alone, so nothing is ever
//     narrowed out from under an object.
//
// Legacy "" rows (also both components) are not candidates.
//
// ONE-SHOT, unlike every other step: since #170 "VEVENT,VTODO" is also a
// deliberate value (kind "both", a widening PATCH, a MKCALENDAR naming both or
// no components), so re-running it would undo the widening of a narrowed
// calendar at the next boot. The step first claims narrowComponentSetsMarkerKey
// (INSERT … ON CONFLICT DO NOTHING) and does nothing when the row already
// exists; the claim, the reads and every narrowing are one transaction, so the
// marker is never written without the narrowings and a failure leaves both
// undone (the next boot tries again). A second instance booting at the same
// time waits on the claim and then finds the marker.
//
// The cutoff must already be recorded (recordTaskKindCutoff runs first); a
// missing or unreadable one is an error, never "every calendar is legacy".
// created_at is compared with it in Go, not in SQL: on SQLite created_at is
// TEXT in the host's local offset, and a text comparison with a UTC cutoff
// would be off by that offset.
//
// Each narrowing goes through CalendarRepository.UpdateSupportedComponents (a
// savepoint inside this transaction): it advances the ctag and sync token and
// writes a "collection" change-log anchor, so DAV clients see the collection
// changed. They re-read supported-calendar-component-set only on a home-set
// PROPFIND or a collection refresh, though (DAVx5: "Refresh collection list").
func narrowCalendarComponentSets(db *gorm.DB, now time.Time) error {
	var toEvents, toTasks int
	err := db.Transaction(func(tx *gorm.DB) error {
		claim := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&domain.SystemSetting{
			Key:   narrowComponentSetsMarkerKey,
			Value: now.UTC().Format(time.RFC3339Nano),
		})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return nil // ran before
		}

		var setting domain.SystemSetting
		if err := tx.Where("key = ?", taskKindIntroducedAtKey).Take(&setting).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%s is not recorded", taskKindIntroducedAtKey)
			}
			return err
		}
		cutoff, err := time.Parse(time.RFC3339Nano, setting.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", taskKindIntroducedAtKey, err)
		}

		// A fresh chain per use: a stored *gorm.DB would accumulate conditions.
		legacy := func() *gorm.DB {
			return tx.Model(&calendar.Calendar{}).
				Where("supported_components = ? AND subscribed = ?", legacyComponentSet, false)
		}
		var cals []calendar.Calendar
		if err := legacy().Select("id", "created_at").Order("id").Find(&cals).Error; err != nil {
			return err
		}
		if len(cals) == 0 {
			return nil
		}

		// Live objects per calendar and component type, in one query.
		var rows []struct {
			CalendarID    uint
			ComponentType string
			Objects       int64
		}
		if err := tx.Model(&calendar.CalendarObject{}).
			Select("calendar_id, component_type, COUNT(*) AS objects").
			Where("calendar_id IN (?)", legacy().Select("id")).
			Group("calendar_id, component_type").
			Scan(&rows).Error; err != nil {
			return err
		}
		type contents struct{ events, tasks, other int64 }
		held := make(map[uint]*contents, len(cals))
		for _, r := range rows {
			c := held[r.CalendarID]
			if c == nil {
				c = &contents{}
				held[r.CalendarID] = c
			}
			switch r.ComponentType {
			case calendar.ComponentEvent:
				c.events += r.Objects
			case calendar.ComponentToDo:
				c.tasks += r.Objects
			default:
				c.other += r.Objects
			}
		}

		repo := repository.NewCalendarRepository(tx)
		for _, cal := range cals {
			if !cal.CreatedAt.Before(cutoff) {
				continue // created with a deliberate kind
			}
			c := held[cal.ID]
			if c == nil || c.other > 0 {
				continue // empty, or holding something it cannot be narrowed around
			}
			var set string // a one-component set is the component's name
			switch {
			case c.events > 0 && c.tasks == 0:
				set = calendar.ComponentEvent
			case c.tasks > 0 && c.events == 0:
				set = calendar.ComponentToDo
			default:
				continue // mixed
			}
			if err := repo.UpdateSupportedComponents(context.Background(), cal.ID, set); err != nil {
				return fmt.Errorf("calendar %d: %w", cal.ID, err)
			}
			if set == calendar.ComponentEvent {
				toEvents++
			} else {
				toTasks++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if toEvents+toTasks > 0 {
		slog.Info("data migration: narrowed the component set of legacy calendars to what they hold (#170)",
			"event_calendars", toEvents, "task_lists", toTasks)
	}
	return nil
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
