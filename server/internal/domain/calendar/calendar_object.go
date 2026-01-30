package calendar

import (
	"time"

	"gorm.io/gorm"
)

// CalendarObject represents an event or todo in a calendar
type CalendarObject struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	UUID          string         `gorm:"uniqueIndex;size:36;not null" json:"uuid"`
	CalendarID    uint           `gorm:"index;not null" json:"calendar_id"`
	Path          string         `gorm:"size:255;not null" json:"path"`
	UID           string         `gorm:"index;size:255;not null" json:"uid"` // iCalendar UID
	ETag          string         `gorm:"size:64;not null" json:"etag"`
	ComponentType string         `gorm:"size:20;not null" json:"component_type"` // VEVENT, VTODO
	ICalData      string         `gorm:"type:text;not null" json:"ical_data"`
	ContentLength int            `gorm:"not null" json:"content_length"`
	Summary       string         `gorm:"size:500" json:"summary"`      // Denormalized for search
	Description   string         `gorm:"type:text" json:"description"` // Denormalized
	Location      string         `gorm:"size:500" json:"location"`     // Denormalized
	StartTime     *time.Time     `gorm:"index" json:"start_time"`      // Denormalized
	EndTime       *time.Time     `json:"end_time"`
	IsAllDay      bool           `json:"is_all_day"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for CalendarObject
func (CalendarObject) TableName() string {
	return "calendar_objects"
}
