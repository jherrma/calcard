package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"gorm.io/gorm"
)

// CalendarRepository implements calendar.CalendarRepository using GORM
type CalendarRepository struct {
	db *gorm.DB
}

// NewCalendarRepository creates a new calendar repository
func NewCalendarRepository(db *gorm.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

// Create creates a new calendar. It mints the collection's initial sync token
// and writes a matching "collection" anchor row to the change log in the same
// transaction, so the token handed out by the very first sync-collection REPORT
// corresponds to a real change-log row. Without this anchor, the next
// incremental sync would be rejected with 403 valid-sync-token, forcing an
// endless full-resync loop on fresh calendars.
func (r *CalendarRepository) Create(ctx context.Context, cal *calendar.Calendar) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		token := calendar.GenerateSyncToken()
		cal.SyncToken = token
		cal.CTag = token
		if err := tx.Create(cal).Error; err != nil {
			return err
		}
		return tx.Create(&calendar.SyncChangeLog{
			CalendarID:   cal.ID,
			ResourcePath: "",
			ChangeType:   "collection",
			SyncToken:    token,
		}).Error
	})
}

// notFoundAsDomain marks a missing calendar row as calendar.ErrNotFound, so the
// use cases can tell "no such calendar" (404) from a storage failure (500).
// gorm.ErrRecordNotFound stays in the chain for callers that test for it.
// unstorableUUID is the answer for a lookup key PostgreSQL cannot store as
// text: invalid UTF-8 or a NUL byte (a percent-encoded %00 in a URL). It is
// not a row, so it is a miss, not a storage failure (a 500 on PostgreSQL).
func unstorableUUID(uuid string) error {
	if utf8.ValidString(uuid) && !strings.ContainsRune(uuid, 0) {
		return nil
	}
	return fmt.Errorf("%w: %w", calendar.ErrNotFound, gorm.ErrRecordNotFound)
}

func notFoundAsDomain(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %w", calendar.ErrNotFound, err)
	}
	return err
}

// GetByID retrieves a calendar by its ID. A missing row is calendar.ErrNotFound.
func (r *CalendarRepository) GetByID(ctx context.Context, id uint) (*calendar.Calendar, error) {
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).First(&cal, id).Error
	if err != nil {
		return nil, notFoundAsDomain(err)
	}
	return &cal, nil
}

// GetByUUID retrieves a calendar by its UUID. A missing row is
// calendar.ErrNotFound.
func (r *CalendarRepository) GetByUUID(ctx context.Context, uuid string) (*calendar.Calendar, error) {
	if err := unstorableUUID(uuid); err != nil {
		return nil, err
	}
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&cal).Error
	if err != nil {
		return nil, notFoundAsDomain(err)
	}
	return &cal, nil
}

// ListByUserID retrieves all calendars for a user
func (r *CalendarRepository) ListByUserID(ctx context.Context, userID uint) ([]*calendar.Calendar, error) {
	var calendars []*calendar.Calendar
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Find(&calendars).Error
	return calendars, err
}

// ListReadableCalendarIDs returns the calendars userID can read (#170) in one
// SELECT: owned ones (a subscribed mirror is owned too) and those named by a
// live share row (calendar_shares.deleted_at IS NULL, the same rows
// GetUserPermission accepts), ordered by id. Soft-deleted calendars are
// skipped by the model's scope. Only the ReadableCalendar columns are read.
func (r *CalendarRepository) ListReadableCalendarIDs(ctx context.Context, userID uint) ([]calendar.ReadableCalendar, error) {
	shared := r.db.WithContext(ctx).
		Model(&sharing.CalendarShare{}).
		Select("calendar_id").
		Where("shared_with_id = ?", userID)
	readable := []calendar.ReadableCalendar{}
	err := r.db.WithContext(ctx).
		Model(&calendar.Calendar{}).
		Select("id", "uuid", "name", "color", "supported_components").
		Where("user_id = ? OR id IN (?)", userID, shared).
		Order("id").
		Find(&readable).Error
	if err != nil {
		return nil, err
	}
	return readable, nil
}

// Update updates an existing calendar
func (r *CalendarRepository) Update(ctx context.Context, cal *calendar.Calendar) error {
	return r.db.WithContext(ctx).Save(cal).Error
}

// UpdateMetadata persists a rename and mints a new sync token atomically. It
// updates only the metadata columns via Select (so a Save of the whole struct
// can't write back the stale sync_token/ctag the caller loaded at request
// start, clobbering a token a concurrent object PUT just committed) and records
// the collection change in the same transaction (so a mid-rename failure can't
// leave the CTag un-bumped, hiding the new displayname from clients).
func (r *CalendarRepository) UpdateMetadata(ctx context.Context, cal *calendar.Calendar) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&calendar.Calendar{}).
			Where("id = ?", cal.ID).
			Select("name", "description", "color", "timezone").
			Updates(cal).Error; err != nil {
			return err
		}
		return r.recordChange(tx, cal.ID, "", "", "collection")
	})
}

// UpdateSupportedComponents stores a calendar's component set (#170) and
// records the collection change (ctag, sync token, "collection" change-log
// anchor) in the same transaction, so a client that sees the new ctag also
// finds the new supported-calendar-component-set. Like UpdateMetadata it writes
// only its own column (plus updated_at), never the sync_token/ctag of a struct
// the caller loaded earlier. The widen-only PATCH and the one-shot narrowing
// migration both write the set through it. A calendar that does not exist (or
// is soft-deleted) is calendar.ErrNotFound and nothing is written, not even a
// change-log row.
func (r *CalendarRepository) UpdateSupportedComponents(ctx context.Context, id uint, set string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&calendar.Calendar{}).
			Where("id = ?", id).
			Update("supported_components", set)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return calendar.ErrNotFound
		}
		return r.recordChange(tx, id, "", "", "collection")
	})
}

// Delete permanently removes a calendar and everything in it, in one
// transaction: its events, change log, shares, the subscription row that
// mirrors a feed into it (story 100) and its public link (purgeCalendars).
//
// The cascade lives here rather than in the use cases because a calendar is
// deleted from several places — the REST/MCP calendar use case, a DAV client's
// DELETE on the collection, the subscription endpoints — and none of them
// should have to know what hangs off a calendar. An orphaned subscription is
// not inert, for one: the worker would keep fetching the third party's feed on
// a schedule, forever, for a calendar the user believes they deleted.
func (r *CalendarRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return purgeCalendars(tx, nil, id)
	})
}

// CountByUserID counts calendars for a user
func (r *CalendarRepository) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&calendar.Calendar{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
}

// GetEventCount returns the number of events (ComponentEvent objects) in a
// calendar. Tasks (VTODO) are not counted (#170): the count is the
// `event_count` of the calendar list, /search, MCP list_calendars and the
// subscription views, and a task list must not report its tasks as events.
func (r *CalendarRepository) GetEventCount(ctx context.Context, calendarID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&calendar.CalendarObject{}).
		Where("calendar_id = ? AND component_type = ?", calendarID, calendar.ComponentEvent).
		Count(&count).Error
	return count, err
}

// GetByPath retrieves a calendar by user ID and path
func (r *CalendarRepository) GetByPath(ctx context.Context, userID uint, path string) (*calendar.Calendar, error) {
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).Where("user_id = ? AND path = ?", userID, path).First(&cal).Error
	if err != nil {
		return nil, err
	}
	return &cal, nil
}

// GetCalendarObjects retrieves all calendar objects for a calendar
func (r *CalendarRepository) GetCalendarObjects(ctx context.Context, calendarID uint) ([]*calendar.CalendarObject, error) {
	var objects []*calendar.CalendarObject
	err := r.db.WithContext(ctx).
		Where("calendar_id = ?", calendarID).
		Order("start_time ASC, created_at ASC").
		Find(&objects).Error
	return objects, err
}

// GetCalendarObjectByPath retrieves a calendar object by calendar ID and path
func (r *CalendarRepository) GetCalendarObjectByPath(ctx context.Context, calendarID uint, path string) (*calendar.CalendarObject, error) {
	var obj calendar.CalendarObject
	err := r.db.WithContext(ctx).Where("calendar_id = ? AND path = ?", calendarID, path).First(&obj).Error
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

// CreateCalendarObject creates a new calendar object
func (r *CalendarRepository) CreateCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(obj).Error; err != nil {
			return err
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "created")
	})
}

// UpdateCalendarObject updates an existing calendar object
func (r *CalendarRepository) UpdateCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(obj).Error; err != nil {
			return err
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "modified")
	})
}

// UpdateCalendarObjectIfETag writes obj (every column but the identity ones,
// UpdatedAt as the caller's clock says) over the live row with obj.ID only
// while that row still carries loadedETag, the ETag obj was loaded with (obj
// itself holds the new one), and records the "modified" change in the same
// transaction. It is one conditional UPDATE with explicit columns, never
// Save, whose fall-back to an INSERT brings a row that a concurrent DELETE
// removed back to life (#170). Nothing written is calendar.ErrNotFound when no
// live row is left (deleted meanwhile, which records nothing) and
// calendar.ErrETagMismatch when another writer changed the row first.
func (r *CalendarRepository) UpdateCalendarObjectIfETag(ctx context.Context, obj *calendar.CalendarObject, loadedETag string) error {
	obj.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&calendar.CalendarObject{}).
			Where("id = ? AND e_tag = ?", obj.ID, loadedETag).
			Select("*").
			Omit("id", "uuid", "calendar_id", "created_at", "deleted_at").
			UpdateColumns(obj)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return noRowWritten(tx, obj.ID)
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "modified")
	})
}

// noRowWritten tells a conditional write that matched nothing why: no live
// row any more (ErrNotFound) or a row with another ETag (ErrETagMismatch).
func noRowWritten(tx *gorm.DB, id uint) error {
	var live int64
	if err := tx.Model(&calendar.CalendarObject{}).Where("id = ?", id).Count(&live).Error; err != nil {
		return err
	}
	if live == 0 {
		return calendar.ErrNotFound
	}
	return calendar.ErrETagMismatch
}

// MoveCalendarObject reassigns an object to a new calendar and records both the
// target "modified" change and the source "deleted" change in a single
// transaction. obj.CalendarID must already point at the target calendar.
// Doing both sync-log writes atomically with the reassign prevents the
// permanent sync ghost that occurs if the source "deleted" change is lost
// after the reassign has already committed.
func (r *CalendarRepository) MoveCalendarObject(ctx context.Context, obj *calendar.CalendarObject, sourceCalendarID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(obj).Error; err != nil {
			return err
		}
		// Target collection sees the object arrive.
		if err := r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "modified"); err != nil {
			return err
		}
		// Source collection sees the object leave.
		return r.recordChange(tx, sourceCalendarID, obj.Path, obj.UID, "deleted")
	})
}

// DeleteCalendarObject soft-deletes a calendar object (GORM's deleted_at) and
// records the "deleted" change. The row goes for good with its calendar.
func (r *CalendarRepository) DeleteCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&calendar.CalendarObject{}, obj.ID).Error; err != nil {
			return err
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "deleted")
	})
}

// HardDeleteCalendarObject removes the object's row for good (an Unscoped
// DELETE, no tombstone) and records the "deleted" change in the same
// transaction. The change-log row keeps the path and UID, which is all a
// sync-collection REPORT needs to answer the href with a 404, so nothing else
// has to survive. Nothing deleted (the row is already gone, e.g. a concurrent
// delete won, or it only survives as a soft-deleted tombstone) is
// calendar.ErrNotFound with nothing recorded, so the loser of a race does not
// log a second removal.
func (r *CalendarRepository) HardDeleteCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deleted := tx.Unscoped().Where("deleted_at IS NULL").Delete(&calendar.CalendarObject{}, obj.ID)
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected == 0 {
			return calendar.ErrNotFound
		}
		return r.recordChange(tx, obj.CalendarID, obj.Path, obj.UID, "deleted")
	})
}

// GetChangesSinceToken retrieves all changes to a calendar since a given sync token
func (r *CalendarRepository) GetChangesSinceToken(ctx context.Context, calendarID uint, token string) ([]*calendar.SyncChangeLog, error) {
	var changes []*calendar.SyncChangeLog

	if token == "" {
		// Initial sync (RFC 6578 §3.3): report the current members once each,
		// synthesized from live objects — NOT a replay of the raw change log
		// (which would duplicate hrefs and emit 404s for long-gone resources).
		var objs []*calendar.CalendarObject
		if err := r.db.WithContext(ctx).Where("calendar_id = ?", calendarID).Find(&objs).Error; err != nil {
			return nil, err
		}
		var cal calendar.Calendar
		if err := r.db.WithContext(ctx).First(&cal, calendarID).Error; err != nil {
			return nil, err
		}
		for _, obj := range objs {
			changes = append(changes, &calendar.SyncChangeLog{
				CalendarID:   calendarID,
				ResourcePath: obj.Path,
				ResourceUID:  obj.UID,
				ChangeType:   "created",
				SyncToken:    cal.SyncToken,
			})
		}
		return changes, nil
	}

	// "collection" anchor rows exist only to give freshly minted tokens a valid
	// change-log entry; they never represent a resource change, so exclude them
	// from the delta itself.
	query := r.db.WithContext(ctx).Where("calendar_id = ?", calendarID).Where("change_type <> ?", "collection")
	{
		// Find the ID of the sync change log entry with the given token
		var lastChange calendar.SyncChangeLog
		err := r.db.WithContext(ctx).Where("calendar_id = ? AND sync_token = ?", calendarID, token).First(&lastChange).Error
		if err == nil {
			query = query.Where("id > ?", lastChange.ID)
		} else if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		// If token not found, RFC 6578 says we SHOULD return 403 Forbidden with valid-sync-token error.
		// For now, if we can't find the token, we'll return an error that the caller can handle.
		if err == gorm.ErrRecordNotFound {
			return nil, gorm.ErrRecordNotFound
		}
	}

	err := query.Order("id ASC").Find(&changes).Error
	return changes, err
}

// GetCalendarObjectByUUID retrieves a calendar object by UUID
func (r *CalendarRepository) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	if err := unstorableUUID(uuid); err != nil {
		return nil, err
	}
	var obj calendar.CalendarObject
	err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&obj).Error
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

// GetCalendarObjectByUID looks up a calendar object by its iCalendar UID within
// a specific calendar (used for RFC 4791 no-uid-conflict detection on PUT).
// Returns (nil, nil) when not found.
func (r *CalendarRepository) GetCalendarObjectByUID(ctx context.Context, calendarID uint, uid string) (*calendar.CalendarObject, error) {
	var obj calendar.CalendarObject
	if err := r.db.WithContext(ctx).Where("calendar_id = ? AND uid = ?", calendarID, uid).First(&obj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &obj, nil
}

// ListEvents retrieves the events (VEVENT objects) of a calendar within a time
// range. Tasks (VTODO) stored in the same collection are excluded (#170): this
// feeds the REST event list and the MCP event tools, which would otherwise show
// a task with a DTSTART as an event. DAV reads go through GetCalendarObjects
// and still see every object.
func (r *CalendarRepository) ListEvents(ctx context.Context, calendarID uint, start, end time.Time) ([]*calendar.CalendarObject, error) {
	var objects []*calendar.CalendarObject
	// For recurring masters, end_time holds only the FIRST occurrence's end;
	// recurrence_end_time holds the last occurrence's end (far-future for
	// unbounded series). COALESCE keeps recurring events visible in windows
	// past their first occurrence while leaving non-recurring rows unchanged.
	// UTC-normalised for the same reason as SearchEvents: under SQLite these are
	// TEXT columns compared lexicographically, so a bound carrying a non-UTC
	// offset shifts the window by that offset. Callers currently pass RFC 3339
	// with a Z, which is UTC by luck rather than by contract.
	err := r.db.WithContext(ctx).
		Where("calendar_id = ?", calendarID).
		Where("component_type = ?", calendar.ComponentEvent).
		Where("start_time < ? AND COALESCE(recurrence_end_time, end_time) > ?", end.UTC(), start.UTC()).
		Order("start_time ASC, created_at ASC").
		Find(&objects).Error
	return objects, err
}

// SearchEvents implements the cross-calendar event text search behind
// GET /api/v1/search (#156).
//
// It matches the denormalized summary/location/description columns — the same
// three fields the old client-side scan filtered on — and NOT ical_data, so an
// occurrence renamed through an exception override is found via its series
// master's text rather than its own. Matching ical_data would also match UIDs,
// timezone identifiers and attendee addresses, which is worse than the gap.
//
// Ordering is two index-friendly queries rather than one CASE expression,
// because GORM's Order() cannot bind parameters: "live" objects (last
// occurrence at or after the pivot) ascending by start time, then "past" ones
// descending. Concatenating them and slicing gives a stable total order, with
// id as the tie-breaker so equal timestamps cannot shuffle between pages.
func (r *CalendarRepository) SearchEvents(ctx context.Context, q calendar.EventSearchQuery) ([]*calendar.CalendarObject, error) {
	if len(q.CalendarIDs) == 0 || strings.TrimSpace(q.Text) == "" {
		return nil, nil
	}
	if q.Limit <= 0 {
		return nil, nil
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	// Enough rows that slicing at [offset:offset+limit] is correct even when the
	// whole page comes from one of the two groups.
	need := q.Offset + q.Limit

	// EVERY time bound must be normalised to UTC before it becomes a bound
	// parameter. Under SQLite these columns are TEXT ("2026-08-04 06:38:33+00:00")
	// and comparison is LEXICOGRAPHIC, so the trailing offset is not honoured: a
	// parameter carrying a +02:00 wall clock silently compares two hours off, and
	// events land in the wrong half of the pivot. The other endpoints get away
	// without this only because their bounds are parsed from RFC 3339 with a Z.
	pivot := q.Pivot.UTC()
	var start, end *time.Time
	if q.Start != nil {
		utc := q.Start.UTC()
		start = &utc
	}
	if q.End != nil {
		utc := q.End.UTC()
		end = &utc
	}

	textWhere, textArgs := foldedLike(q.Text, "summary", "location", "description")
	base := func() *gorm.DB {
		db := r.db.WithContext(ctx).
			Model(&calendar.CalendarObject{}).
			Where("calendar_id IN ?", q.CalendarIDs).
			// Events only, like ListEvents: a task (VTODO) is not an event and
			// must not come back as one (#170).
			Where("component_type = ?", calendar.ComponentEvent).
			// An object without a start time has no place on a timeline and is
			// invisible to ListEvents too; keep both endpoints consistent.
			Where("start_time IS NOT NULL").
			Where(textWhere, textArgs...)
		// Same overlap predicate as ListEvents: COALESCE keeps a recurring
		// series visible in windows past its first occurrence.
		if end != nil {
			db = db.Where("start_time < ?", *end)
		}
		if start != nil {
			db = db.Where("COALESCE(recurrence_end_time, end_time) > ?", *start)
		}
		return db
	}

	var live []*calendar.CalendarObject
	if err := base().
		Where("COALESCE(recurrence_end_time, end_time) >= ?", pivot).
		Order("start_time ASC, id ASC").
		Limit(need).
		Find(&live).Error; err != nil {
		return nil, err
	}

	results := live
	// Only reach for past objects when the live ones don't already fill the page.
	if len(results) < need {
		var past []*calendar.CalendarObject
		if err := base().
			Where("COALESCE(recurrence_end_time, end_time) < ?", pivot).
			Order("start_time DESC, id ASC").
			Limit(need - len(results)).
			Find(&past).Error; err != nil {
			return nil, err
		}
		results = append(results, past...)
	}

	if q.Offset >= len(results) {
		return nil, nil
	}
	return results[q.Offset:], nil
}

// taskOrder is the stable server order of every task list (#170): due date
// ascending with undated tasks last, then priority ascending with
// unprioritised tasks last, then created_at, then id as the final tie-breaker
// so equal rows cannot shuffle between pages. NULLS LAST is spelled out as
// "(col IS NULL), col" because SQLite and PostgreSQL disagree on where NULLs
// sort by default (SQLite first, PostgreSQL last).
const taskOrder = "(due_at IS NULL), due_at, (priority IS NULL), priority, created_at, id"

// ListTasks implements the task list behind GET /api/v1/calendars/:id/tasks
// (and the cross-list GET /api/v1/tasks): the VTODO objects of q.CalendarIDs,
// filtered by status, text and due date in SQL on the denormalized task
// columns, one page plus the total.
//
// The total is the page length whenever the page itself proves it (a page
// shorter than the limit that is not empty, or the first page coming back
// empty), so the usual small list costs one query; otherwise it is a COUNT.
func (r *CalendarRepository) ListTasks(ctx context.Context, q calendar.TaskQuery) ([]*calendar.CalendarObject, int64, error) {
	statusWhere, statusArgs, err := taskStatusCondition(q.Status)
	if err != nil {
		return nil, 0, err
	}
	if len(q.CalendarIDs) == 0 {
		return nil, 0, nil
	}
	offset := max(q.Offset, 0)

	var tasks []*calendar.CalendarObject
	if q.Limit > 0 {
		page := r.matchingTasks(ctx, q, statusWhere, statusArgs).Order(taskOrder).Limit(q.Limit).Offset(offset)
		if err := page.Find(&tasks).Error; err != nil {
			return nil, 0, err
		}
	}

	// A short page ends the result, so it tells the total, unless it is empty
	// because the offset lies past the end (then only a COUNT knows).
	if q.Limit > 0 && len(tasks) < q.Limit && (len(tasks) > 0 || offset == 0) {
		return tasks, int64(offset + len(tasks)), nil
	}
	var total int64
	if err := r.matchingTasks(ctx, q, statusWhere, statusArgs).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

// taskStatusCondition is the SQL condition of a task status filter (empty for
// TaskFilterAll); an unknown filter is an error.
func taskStatusCondition(filter string) (string, []interface{}, error) {
	switch filter {
	case calendar.TaskFilterOpen:
		// Not done. NULL/"" can only be a row the backfill could not decode;
		// the API reports it as needs-action, so it is open here too.
		return "(status IS NULL OR status NOT IN ?)", []interface{}{calendar.TaskDoneStatuses()}, nil
	case calendar.TaskFilterCompleted:
		return "status IN ?", []interface{}{calendar.TaskDoneStatuses()}, nil
	case calendar.TaskFilterAll:
		return "", nil, nil
	}
	return "", nil, fmt.Errorf("unknown task status filter %q", filter)
}

// matchingTasks starts a fresh query of the VTODO objects q selects: its
// calendars, the status condition, the text and the due bounds, without
// paging or order. Fresh per call: a reused *gorm.DB would accumulate
// conditions.
func (r *CalendarRepository) matchingTasks(ctx context.Context, q calendar.TaskQuery, statusWhere string, statusArgs []interface{}) *gorm.DB {
	db := r.db.WithContext(ctx).
		Model(&calendar.CalendarObject{}).
		Where("calendar_id IN ?", q.CalendarIDs).
		Where("component_type = ?", calendar.ComponentToDo)
	if statusWhere != "" {
		db = db.Where(statusWhere, statusArgs...)
	}
	if q.Text != "" {
		textWhere, textArgs := foldedLike(q.Text, "summary", "description")
		db = db.Where(textWhere, textArgs...)
	}
	// UTC-normalised like SearchEvents: under SQLite these are TEXT
	// columns compared lexicographically, so a bound with a non-UTC
	// offset would shift the range by that offset.
	if q.DueAfter != nil {
		db = db.Where("due_at >= ?", q.DueAfter.UTC())
	}
	if q.DueBefore != nil {
		db = db.Where("due_at < ?", q.DueBefore.UTC())
	}
	return db
}

// taskCountsSelect is the conditional aggregation behind CountTasks, one row
// per calendar. "Open" is the ListTasks open predicate (not done, a NULL or ""
// status included), "completed" its completed one, so the counts agree with
// the list filters. An all-day task (is_all_day true) compares its due date,
// stored as midnight UTC, with as_of's date; any other task (false, or NULL in
// a row no write path produced) is timed and compares instants. A NULL due_at
// makes every due comparison NULL, so an undated task is never overdue or due
// today. Placeholders, in order: done statuses (completed), done (open), done,
// true, Date, false, Now (overdue), done, true, Date, NextDate, false, Start,
// End (due today).
const taskCountsSelect = `calendar_id,
	SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END) AS completed_count,
	SUM(CASE WHEN status IS NULL OR status NOT IN ? THEN 1 ELSE 0 END) AS open_count,
	SUM(CASE WHEN (status IS NULL OR status NOT IN ?) AND (
		(is_all_day = ? AND due_at < ?) OR
		((is_all_day IS NULL OR is_all_day = ?) AND due_at < ?)
	) THEN 1 ELSE 0 END) AS overdue_count,
	SUM(CASE WHEN (status IS NULL OR status NOT IN ?) AND (
		(is_all_day = ? AND due_at >= ? AND due_at < ?) OR
		((is_all_day IS NULL OR is_all_day = ?) AND due_at >= ? AND due_at < ?)
	) THEN 1 ELSE 0 END) AS due_today_count`

// CountTasks implements the counts behind GET /api/v1/tasks/summary (#170):
// one grouped query over the denormalized task columns of calendarIDs, so the
// numbers do not depend on how many tasks a list page returns. "Today" is
// calendar.TaskDayOf(asOf); every bound is UTC, like ListTasks (under SQLite
// these are TEXT columns compared lexicographically).
func (r *CalendarRepository) CountTasks(ctx context.Context, calendarIDs []uint, asOf time.Time) (calendar.TaskCounts, error) {
	counts := calendar.TaskCounts{ByCalendar: map[uint]calendar.TaskCount{}}
	if len(calendarIDs) == 0 {
		return counts, nil
	}
	day := calendar.TaskDayOf(asOf)
	done := calendar.TaskDoneStatuses()

	var rows []struct {
		CalendarID     uint
		OpenCount      int64
		OverdueCount   int64
		DueTodayCount  int64
		CompletedCount int64
	}
	err := r.db.WithContext(ctx).
		Model(&calendar.CalendarObject{}).
		Select(taskCountsSelect,
			done,
			done,
			done, true, day.Date, false, day.Now,
			done, true, day.Date, day.NextDate, false, day.Start, day.End,
		).
		Where("calendar_id IN ?", calendarIDs).
		Where("component_type = ?", calendar.ComponentToDo).
		Group("calendar_id").
		Scan(&rows).Error
	if err != nil {
		return calendar.TaskCounts{}, err
	}
	for _, row := range rows {
		count := calendar.TaskCount{
			Open:      row.OpenCount,
			Overdue:   row.OverdueCount,
			DueToday:  row.DueTodayCount,
			Completed: row.CompletedCount,
		}
		counts.ByCalendar[row.CalendarID] = count
		counts.Total.Open += count.Open
		counts.Total.Overdue += count.Overdue
		counts.Total.DueToday += count.DueToday
		counts.Total.Completed += count.Completed
	}
	return counts, nil
}

// RecordChange advances the calendar's sync token and writes a matching
// change-log row, atomically. Used by mutations that don't go through
// Create/Update/DeleteCalendarObject — collection rename (change type
// "collection") and the source side of a cross-calendar move (change type
// "deleted").
func (r *CalendarRepository) RecordChange(ctx context.Context, calendarID uint, path, uid, changeType string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.recordChange(tx, calendarID, path, uid, changeType)
	})
}

// recordChange advances the collection's sync token and appends a changelog row
// inside tx.
//
// Known limitation (M20): under PostgreSQL, concurrent transactions can commit
// changelog rows out of monotonic ID order. A client running a sync REPORT in
// the window between two such commits can permanently miss the lower-ID change,
// because its next sync starts from the higher token it already observed. This
// is not mitigated here. A full fix would serialize changelog appends per
// collection (e.g. a Postgres advisory lock keyed on calendarID, or
// SELECT ... FOR UPDATE on the calendar row before inserting). SQLite is
// unaffected because writes are serialized.
func (r *CalendarRepository) recordChange(tx *gorm.DB, calendarID uint, path, uid, changeType string) error {
	newToken := calendar.GenerateSyncToken()

	// Update calendar sync token and ctag
	if err := tx.Model(&calendar.Calendar{}).Where("id = ?", calendarID).Updates(map[string]interface{}{
		"sync_token": newToken,
		"ctag":       newToken,
	}).Error; err != nil {
		return err
	}

	// Record change
	return tx.Create(&calendar.SyncChangeLog{
		CalendarID:   calendarID,
		ResourcePath: path,
		ResourceUID:  uid,
		ChangeType:   changeType,
		SyncToken:    newToken,
	}).Error
}

// GetUserPermission determines a user's permission for a calendar
func (r *CalendarRepository) GetUserPermission(ctx context.Context, calendarID, userID uint) (calendar.CalendarPermission, error) {
	// Check ownership
	var cal calendar.Calendar
	if err := r.db.WithContext(ctx).First(&cal, calendarID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return calendar.PermissionNone, nil
		}
		return calendar.PermissionNone, err
	}

	if cal.UserID == userID {
		return calendar.PermissionOwner, nil
	}

	// Check share
	var share sharing.CalendarShare
	err := r.db.WithContext(ctx).Where("calendar_id = ? AND shared_with_id = ?", calendarID, userID).First(&share).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return calendar.PermissionNone, nil
		}
		return calendar.PermissionNone, err
	}

	if share.Permission == "read-write" {
		return calendar.PermissionReadWrite, nil
	}
	return calendar.PermissionRead, nil
}

// FindByPublicToken retrieves a calendar by its public token
func (r *CalendarRepository) FindByPublicToken(ctx context.Context, token string) (*calendar.Calendar, error) {
	var cal calendar.Calendar
	err := r.db.WithContext(ctx).Where("public_token = ? AND public_enabled = ?", token, true).First(&cal).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cal, nil
}

// ReplaceFeedObjects makes a subscribed calendar's contents match the feed
// exactly, in one transaction. See the interface doc for why this is not a loop
// over the object CRUD methods.
//
// Change-log bookkeeping deliberately mirrors recordChange rather than calling
// it: each changed resource gets its OWN sync token (the change log is queried
// by "the id of the row carrying this token", so two rows sharing a token would
// make a resuming client replay that whole batch on every subsequent sync,
// forever), but the calendar row is updated exactly once, to the last token.
// A sync that changes nothing touches neither, so an unchanged feed does not
// bump the CTag and wake every connected DAV client.
func (r *CalendarRepository) ReplaceFeedObjects(ctx context.Context, calendarID uint, objects []*calendar.CalendarObject) (calendar.FeedSyncStats, error) {
	var stats calendar.FeedSyncStats

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []*calendar.CalendarObject
		if err := tx.Where("calendar_id = ?", calendarID).Find(&existing).Error; err != nil {
			return err
		}
		byUID := make(map[string]*calendar.CalendarObject, len(existing))
		for _, obj := range existing {
			byUID[obj.UID] = obj
		}

		lastToken := ""
		record := func(path, uid, changeType string) error {
			token := calendar.GenerateSyncToken()
			lastToken = token
			return tx.Create(&calendar.SyncChangeLog{
				CalendarID:   calendarID,
				ResourcePath: path,
				ResourceUID:  uid,
				ChangeType:   changeType,
				SyncToken:    token,
			}).Error
		}

		seen := make(map[string]bool, len(objects))
		for _, obj := range objects {
			if seen[obj.UID] {
				// Two components in one feed claiming the same UID: the first
				// wins. Letting the second through would create a duplicate
				// resource that the next sync would then delete and recreate
				// forever.
				continue
			}
			seen[obj.UID] = true

			prev, ok := byUID[obj.UID]
			if !ok {
				obj.CalendarID = calendarID
				if err := tx.Create(obj).Error; err != nil {
					return err
				}
				if err := record(obj.Path, obj.UID, "created"); err != nil {
					return err
				}
				stats.Created++
				continue
			}

			if prev.ICalData == obj.ICalData {
				stats.Unchanged++
				continue
			}

			// Keep the stored resource's identity (UUID, path, id) so DAV
			// clients see an update to a resource they already know rather
			// than a delete plus an unrelated create.
			prev.ICalData = obj.ICalData
			prev.ETag = calendar.NewETag()
			if err := prev.PopulateDenormFieldsFromICal(); err != nil {
				return err
			}
			if err := tx.Save(prev).Error; err != nil {
				return err
			}
			if err := record(prev.Path, prev.UID, "modified"); err != nil {
				return err
			}
			stats.Updated++
		}

		for uid, obj := range byUID {
			if seen[uid] {
				continue
			}
			if err := tx.Delete(&calendar.CalendarObject{}, obj.ID).Error; err != nil {
				return err
			}
			if err := record(obj.Path, obj.UID, "deleted"); err != nil {
				return err
			}
			stats.Deleted++
		}

		if lastToken == "" {
			return nil
		}
		return tx.Model(&calendar.Calendar{}).Where("id = ?", calendarID).Updates(map[string]interface{}{
			"sync_token": lastToken,
			"ctag":       lastToken,
		}).Error
	})
	if err != nil {
		return calendar.FeedSyncStats{}, err
	}
	return stats, nil
}
