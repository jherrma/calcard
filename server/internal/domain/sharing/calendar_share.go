package sharing

import (
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// CalendarShare represents a calendar shared with another user
type CalendarShare struct {
	ID           uint              `gorm:"primaryKey" json:"id"`
	UUID         string            `gorm:"uniqueIndex;size:36;not null" json:"uuid"`
	CalendarID   uint              `gorm:"index;not null;uniqueIndex:idx_calendar_share_user" json:"calendar_id"`
	SharedWithID uint              `gorm:"index;not null;uniqueIndex:idx_calendar_share_user" json:"shared_with_id"`
	Permission   string            `gorm:"size:20;not null" json:"permission"` // "read" or "read-write"
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	DeletedAt    gorm.DeletedAt    `gorm:"index" json:"-"`
	Calendar     calendar.Calendar `gorm:"foreignKey:CalendarID" json:"-"`
	SharedWith   user.User         `gorm:"foreignKey:SharedWithID" json:"shared_with"`
}

// TableName overrides the table name used by User to `calendar_shares`
func (CalendarShare) TableName() string {
	return "calendar_shares"
}
