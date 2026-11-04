package calendar

import (
	"time"

	"gorm.io/gorm"
)

// Values of CalendarObject.ComponentType: the iCalendar component name of the
// object's first VEVENT/VTODO, copied verbatim by PopulateDenormFieldsFromICal.
const (
	ComponentEvent = "VEVENT"
	ComponentToDo  = "VTODO"
	// ComponentSetBoth is the SupportedComponents value of a calendar that
	// stores both (kind "both"; the set every calendar had before #170).
	ComponentSetBoth = ComponentEvent + "," + ComponentToDo
)

// ProductID is the PRODID of the iCalendar objects this server writes.
const ProductID = "-//CalCard//EN"

// UIDDomain is the domain part of the UIDs the REST routes generate
// ("<uuid>@calcard.io").
const UIDDomain = "calcard.io"

// CalendarObject represents an event or todo in a calendar.
//
// CalendarID and ComponentType form the composite index idx_calobj_cal_comp:
// the task list query always filters by both, and it also serves every
// lookup by CalendarID alone (its leading column), so there is no separate
// calendar_id index; dropRedundantCalendarIDIndex removes the one older
// databases still carry.
type CalendarObject struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	UUID          string     `gorm:"uniqueIndex;size:36;not null" json:"uuid"`
	CalendarID    uint       `gorm:"index:idx_calobj_cal_comp,priority:1;not null" json:"calendar_id"`
	Path          string     `gorm:"size:255;not null" json:"path"`
	UID           string     `gorm:"index;size:255;not null" json:"uid"` // iCalendar UID
	ETag          string     `gorm:"size:64;not null" json:"etag"`
	ComponentType string     `gorm:"size:20;not null;index:idx_calobj_cal_comp,priority:2" json:"component_type"` // VEVENT, VTODO
	ICalData      string     `gorm:"type:text;not null" json:"ical_data"`
	ContentLength int        `gorm:"not null" json:"content_length"`
	Summary       string     `gorm:"size:500" json:"summary"`      // Denormalized for search
	Description   string     `gorm:"type:text" json:"description"` // Denormalized
	Location      string     `gorm:"size:500" json:"location"`     // Denormalized
	StartTime     *time.Time `gorm:"index" json:"start_time"`      // Denormalized
	EndTime       *time.Time `json:"end_time"`
	// RecurrenceEndTime: instant after which a recurring series yields no more
	// occurrences (last occurrence's end; far-future for unbounded). NULL for
	// non-recurring objects. Maintained by PopulateDenormFieldsFromICal and used
	// by ListEvents so recurring events remain visible in windows past their
	// first occurrence. Not exposed in JSON.
	RecurrenceEndTime *time.Time `json:"-"`

	// Task columns, derived from a VTODO by PopulateDenormFieldsFromICal
	// (denorm_task.go) and never set by callers; empty/nil on VEVENT rows. Not
	// exposed in JSON: the API serves them through task DTOs.
	//
	// Status is the upper-cased STATUS, one of the TaskStatus* constants
	// (unknown values read NEEDS-ACTION; the iCalendar keeps the raw value).
	Status string `gorm:"size:20" json:"-"`
	// DueAt is DUE, else DTSTART+DURATION, in UTC; a date-only DUE is midnight
	// UTC of that date. Nil when the task has no due date.
	DueAt *time.Time `gorm:"index" json:"-"`
	// CompletedAt is COMPLETED in UTC.
	CompletedAt *time.Time `json:"-"`
	// PercentComplete is PERCENT-COMPLETE clamped to 0..100; nil when absent.
	PercentComplete *int `json:"-"`
	// Priority is PRIORITY 1..9 (RFC 5545); 0 ("undefined") is stored as nil.
	Priority *int `json:"-"`

	IsAllDay  bool           `json:"is_all_day"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for CalendarObject
func (CalendarObject) TableName() string {
	return "calendar_objects"
}

// IsEvent reports whether the object is an event (ComponentType VEVENT). The
// event routes, the MCP event tools and the event use cases serve events only
// (#170): any other object, a task (VTODO) above all, is answered exactly like
// an unknown event. A nil object is not an event.
func (o *CalendarObject) IsEvent() bool {
	return o != nil && o.ComponentType == ComponentEvent
}

// IsTask reports whether the object is a task (ComponentType VTODO), the mirror
// of IsEvent: the task routes and use cases serve tasks only (#170), so an
// event's UUID there is answered exactly like an unknown task. A nil object is
// not a task.
func (o *CalendarObject) IsTask() bool {
	return o != nil && o.ComponentType == ComponentToDo
}
