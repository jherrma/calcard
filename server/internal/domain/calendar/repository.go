package calendar

import (
	"context"
	"time"
)

// EventSearchQuery describes a text search over the denormalized event columns
// of a set of calendars (#156). The caller resolves which calendars it may read
// — the repository trusts CalendarIDs and never widens it.
type EventSearchQuery struct {
	// CalendarIDs limits the search to these calendars. An empty slice matches
	// nothing (it is NOT "all calendars"), so a caller that resolved no
	// readable calendar cannot accidentally search the whole table.
	CalendarIDs []uint
	// Text is matched case-insensitively as a substring of summary, location
	// or description. LIKE wildcards in it are escaped, so it matches
	// literally.
	Text string
	// Start/End optionally bound the search to objects overlapping the range.
	// Both nil means no date bound at all — the point of #156.
	Start *time.Time
	End   *time.Time
	// Pivot ("now") splits results into live and past: objects whose last
	// occurrence is at or after Pivot come first, ordered by start time
	// ascending (so an ongoing or still-recurring series leads, then the
	// soonest upcoming one); past objects follow, most recently started first.
	Pivot time.Time
	// Limit/Offset paginate the concatenated (live, then past) ordering.
	Limit  int
	Offset int
}

// TaskQuery describes a page of tasks (VTODO objects) of a set of calendars
// (#170). Like EventSearchQuery, the caller resolves which calendars it may
// read; the repository trusts CalendarIDs and never widens it.
type TaskQuery struct {
	// CalendarIDs limits the query to these calendars. An empty slice matches
	// nothing (it is NOT "all calendars").
	CalendarIDs []uint
	// Status is TaskFilterOpen, TaskFilterCompleted or TaskFilterAll; any
	// other value is an error. "Done" is TaskDoneStatuses (COMPLETED or
	// CANCELLED); open is everything else.
	Status string
	// Text, when not empty, must occur case-insensitively in the summary or
	// the description. LIKE wildcards in it are escaped, so it matches
	// literally.
	Text string
	// DueBefore/DueAfter optionally bound DueAt to the half-open range
	// [DueAfter, DueBefore): due_at >= DueAfter and due_at < DueBefore. A task
	// without a due date never matches a bound. Normalised to UTC before
	// they are compared.
	DueBefore, DueAfter *time.Time
	// Limit/Offset page the result. Limit <= 0 returns no rows (the total is
	// still reported); a negative Offset is 0.
	Limit, Offset int
}

// TaskCount holds the counts of a set of tasks taken at one TaskDay (#170).
// Open, Overdue and DueToday count open tasks only (not done); Completed
// counts the done ones, COMPLETED and CANCELLED (TaskDoneStatuses), so it
// agrees with the "completed" list filter and a cancelled task never counts as
// open, overdue or due today. Overdue and DueToday overlap (see TaskDay).
type TaskCount struct {
	Open      int64
	Overdue   int64
	DueToday  int64
	Completed int64
}

// TaskCounts is what CountTasks returns: the counts of every queried calendar
// that holds at least one task, by calendar id (a calendar without tasks has
// no entry), and their sum.
type TaskCounts struct {
	Total      TaskCount
	ByCalendar map[uint]TaskCount
}

// ReadableCalendar is one calendar a user can read, as
// ListReadableCalendarIDs returns it (#170): the id a cross-list query runs
// over, the component set that decides whether it is a task list
// (SupportsComponent), and the UUID, name and colour that label its rows.
// Nothing that costs a join or a COUNT, and no permission: it is not a
// Calendar and must not be used to authorise a write.
type ReadableCalendar struct {
	ID                  uint
	UUID                string
	Name                string
	Color               string
	SupportedComponents string
}

// SupportsComponent reports whether the calendar stores componentType, by the
// one component-set rule (the package function SupportsComponent; a legacy
// empty set stores VEVENT and VTODO).
func (c ReadableCalendar) SupportsComponent(componentType string) bool {
	return SupportsComponent(&Calendar{SupportedComponents: c.SupportedComponents}, componentType)
}

// CalendarRepository defines the interface for calendar persistence
type CalendarRepository interface {
	// Create creates a new calendar
	Create(ctx context.Context, calendar *Calendar) error

	// GetByID retrieves a calendar by its ID
	GetByID(ctx context.Context, id uint) (*Calendar, error)

	// GetByUUID retrieves a calendar by its UUID
	GetByUUID(ctx context.Context, uuid string) (*Calendar, error)

	// ListByUserID retrieves all calendars for a user
	ListByUserID(ctx context.Context, userID uint) ([]*Calendar, error)

	// ListReadableCalendarIDs returns every calendar the user can read, in
	// one query and ordered by id: the calendars they own (subscribed mirrors
	// included) and the calendars shared with them, read-only or read-write
	// (#170). Unlike the calendar list use case (ListByUserID, the share list
	// and one GetEventCount per calendar) it loads no owner and counts
	// nothing; the cross-list task routes resolve their calendars with it.
	ListReadableCalendarIDs(ctx context.Context, userID uint) ([]ReadableCalendar, error)

	// Update updates an existing calendar
	Update(ctx context.Context, calendar *Calendar) error

	// UpdateMetadata persists a collection rename (name/description/color/
	// timezone only) and advances the sync token via a change-log anchor row,
	// atomically in a single transaction. It must NOT write sync_token/ctag from
	// the passed struct (which may be stale relative to a concurrent object PUT);
	// it updates only the named columns and mints a fresh token in the same tx.
	UpdateMetadata(ctx context.Context, calendar *Calendar) error

	// UpdateSupportedComponents stores a calendar's component set (the
	// SupportedComponents comma list, #170) and advances the sync token via a
	// change-log anchor row, atomically in a single transaction. Besides the set
	// it writes only the fresh sync_token/ctag (and updated_at), never a stale
	// struct's columns. Used by the widen-only PATCH and the
	// narrowing data migration; UpdateMetadata cannot write this column. A
	// calendar that does not exist is ErrNotFound and nothing is written.
	UpdateSupportedComponents(ctx context.Context, id uint, set string) error

	// Delete permanently removes a calendar by ID together with everything in
	// it — events, change log, shares, subscription, public link — in one
	// transaction. Callers need not (and should not) clean up after it.
	Delete(ctx context.Context, id uint) error

	// CountByUserID counts calendars for a user
	CountByUserID(ctx context.Context, userID uint) (int64, error)

	// GetEventCount returns the number of events (ComponentEvent objects) in a
	// calendar; tasks (VTODO) are not counted.
	GetEventCount(ctx context.Context, calendarID uint) (int64, error)

	// GetCalendarObjects retrieves all calendar objects (events/todos) for a calendar
	GetCalendarObjects(ctx context.Context, calendarID uint) ([]*CalendarObject, error)

	// GetByPath retrieves a calendar by user ID and path
	GetByPath(ctx context.Context, userID uint, path string) (*Calendar, error)

	// GetCalendarObjectByPath retrieves a calendar object by calendar ID and path
	GetCalendarObjectByPath(ctx context.Context, calendarID uint, path string) (*CalendarObject, error)

	// CreateCalendarObject creates a new calendar object
	CreateCalendarObject(ctx context.Context, obj *CalendarObject) error

	// UpdateCalendarObject updates an existing calendar object
	UpdateCalendarObject(ctx context.Context, obj *CalendarObject) error

	// UpdateCalendarObjectIfETag is UpdateCalendarObject made conditional: obj
	// (holding the new ETag) is written only while the live row still has
	// loadedETag, and a row deleted meanwhile is never brought back. No live
	// row is ErrNotFound, a row with another ETag ErrETagMismatch; neither
	// records a change. Used by the task edits (#170).
	UpdateCalendarObjectIfETag(ctx context.Context, obj *CalendarObject, loadedETag string) error

	// MoveCalendarObject reassigns an object to a new calendar (obj.CalendarID
	// must already be set to the target) and, atomically in a single
	// transaction, records a "modified" change on the target calendar and a
	// "deleted" change on the source calendar so both collections' sync
	// clients converge. Used by the cross-calendar move use case.
	MoveCalendarObject(ctx context.Context, obj *CalendarObject, sourceCalendarID uint) error

	// DeleteCalendarObject deletes a calendar object (a soft delete: the row
	// keeps a deleted_at tombstone until its calendar is deleted) and records
	// the "deleted" change. Used by event deletes (REST, MCP, DAV) and import's
	// replace.
	DeleteCalendarObject(ctx context.Context, obj *CalendarObject) error

	// HardDeleteCalendarObject removes the object's row for good — no
	// soft-delete tombstone — and, in the same transaction, records the
	// "deleted" change (new sync token and ctag), which is all WebDAV sync
	// needs to report the removal. A row that is already gone is ErrNotFound
	// and records nothing. Used by every task DELETE (#170): the REST route and
	// a DAV client's DELETE of a VTODO object.
	HardDeleteCalendarObject(ctx context.Context, obj *CalendarObject) error

	// GetChangesSinceToken retrieves all changes to a calendar since a given sync token
	GetChangesSinceToken(ctx context.Context, calendarID uint, token string) ([]*SyncChangeLog, error)

	// RecordChange advances the calendar sync token and writes a matching
	// change-log row atomically (for mutations outside the object CRUD methods).
	RecordChange(ctx context.Context, calendarID uint, path, uid, changeType string) error

	// ListEvents retrieves the events (ComponentEvent objects) of a calendar
	// within a time range; tasks (VTODO) are excluded.
	ListEvents(ctx context.Context, calendarID uint, start, end time.Time) ([]*CalendarObject, error)

	// SearchEvents finds events (ComponentEvent objects; tasks are excluded)
	// across several calendars whose denormalized summary/location/description
	// match q.Text, ordered as described on EventSearchQuery. It returns series
	// masters, one row per stored object — expanding them into occurrences is
	// the caller's job.
	SearchEvents(ctx context.Context, q EventSearchQuery) ([]*CalendarObject, error)

	// ListTasks returns one page of the tasks (ComponentToDo objects; events
	// are excluded) matching q, and the total number of matching tasks across
	// all pages. A recurring task is one row, its stored object. Order: due
	// date ascending with undated tasks last, then priority ascending with
	// unprioritised tasks last, then created_at, then id (a stable order, so
	// pages never overlap).
	ListTasks(ctx context.Context, q TaskQuery) ([]*CalendarObject, int64, error)

	// CountTasks counts the tasks (ComponentToDo objects; events and deleted
	// objects are excluded) of calendarIDs at TaskDayOf(asOf): open, overdue,
	// due today and completed, per calendar and in total, in one query over
	// the denormalized task columns, whatever the number of tasks. Like
	// TaskQuery it trusts calendarIDs and never widens it: an empty slice
	// counts nothing (zero counts, an empty ByCalendar).
	CountTasks(ctx context.Context, calendarIDs []uint, asOf time.Time) (TaskCounts, error)

	// GetCalendarObjectByUUID retrieves a calendar object by UUID
	GetCalendarObjectByUUID(ctx context.Context, uuid string) (*CalendarObject, error)

	// GetCalendarObjectByUID retrieves a calendar object by calendar ID and
	// iCalendar UID (used for RFC 4791 no-uid-conflict detection on PUT).
	// Returns (nil, nil) when no matching object exists.
	GetCalendarObjectByUID(ctx context.Context, calendarID uint, uid string) (*CalendarObject, error)

	// GetUserPermission determines a user's permission for a calendar
	GetUserPermission(ctx context.Context, calendarID, userID uint) (CalendarPermission, error)

	// FindByPublicToken retrieves a calendar by its public token
	FindByPublicToken(ctx context.Context, token string) (*Calendar, error)

	// ReplaceFeedObjects makes the calendar's contents match objects exactly:
	// it creates objects whose UID is new, rewrites those whose iCalendar data
	// changed, and deletes stored objects the feed no longer contains — all in
	// a single transaction, so a feed sync is never half-applied.
	//
	// It exists as its own method rather than as a loop over the object CRUD
	// methods for two reasons. A loop would mint one sync token per object and
	// re-Update the calendar row that many times; and it would have to run
	// outside any transaction, so a failure partway through would leave the
	// mirror showing a state the feed never published. Objects whose iCalendar
	// data is byte-identical to what is stored are left completely untouched,
	// which is what keeps a feed that republishes unchanged content from
	// bumping the CTag and waking every connected DAV client.
	//
	// It is used only by the subscription sync path (story 100), which is also
	// the only writer permitted on a Subscribed calendar.
	ReplaceFeedObjects(ctx context.Context, calendarID uint, objects []*CalendarObject) (FeedSyncStats, error)
}

// CalendarSubscriptionRepository defines persistence for remote calendar
// subscriptions (story 100).
type CalendarSubscriptionRepository interface {
	// Create stores a new subscription.
	Create(ctx context.Context, sub *CalendarSubscription) error

	// GetByUUID retrieves a subscription by its external id, or (nil, nil)
	// when none exists.
	GetByUUID(ctx context.Context, uuid string) (*CalendarSubscription, error)

	// GetByCalendarID retrieves the subscription backing a calendar, or
	// (nil, nil) when the calendar is not a subscription.
	GetByCalendarID(ctx context.Context, calendarID uint) (*CalendarSubscription, error)

	// ListByUserID retrieves a user's subscriptions, newest first, with the
	// backing Calendar preloaded (the API reports its name, colour and event
	// count alongside the feed's own state).
	ListByUserID(ctx context.Context, userID uint) ([]*CalendarSubscription, error)

	// CountByUserID counts a user's subscriptions, for the per-user cap.
	CountByUserID(ctx context.Context, userID uint) (int64, error)

	// Update persists a subscription's mutable fields.
	Update(ctx context.Context, sub *CalendarSubscription) error

	// Delete removes a subscription row by id. The backing calendar is deleted
	// separately by the use case.
	Delete(ctx context.Context, id uint) error

	// FindDue returns up to limit enabled subscriptions whose NextSyncAt has
	// passed, oldest due first so a backlog drains fairly.
	FindDue(ctx context.Context, now time.Time, limit int) ([]*CalendarSubscription, error)
}
