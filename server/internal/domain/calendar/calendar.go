package calendar

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// CalendarPermission represents access level
type CalendarPermission int

const (
	PermissionNone CalendarPermission = iota
	PermissionRead
	PermissionReadWrite
	PermissionOwner
)

// Calendar represents a calendar collection
type Calendar struct {
	ID                  uint           `gorm:"primaryKey" json:"id"`
	UUID                string         `gorm:"uniqueIndex;size:36;not null" json:"uuid"`
	UserID              uint           `gorm:"index;not null" json:"user_id"`
	Owner               user.User      `gorm:"foreignKey:UserID" json:"-"`
	Path                string         `gorm:"size:255;not null" json:"path"` // URL path component
	Name                string         `gorm:"size:255;not null" json:"name"`
	Description         string         `gorm:"size:1000" json:"description"`
	Color               string         `gorm:"size:7;not null" json:"color"` // #RRGGBB
	Timezone            string         `gorm:"size:50;not null" json:"timezone"`
	SupportedComponents string         `gorm:"size:100;not null" json:"supported_components"` // "VEVENT,VTODO"
	SyncToken           string         `gorm:"size:64;not null;default:''" json:"sync_token"`
	CTag                string         `gorm:"column:ctag;size:64;not null;default:''" json:"ctag"`
	PublicToken         *string        `gorm:"uniqueIndex;size:64" json:"-"`
	PublicEnabled       bool           `gorm:"default:false" json:"public_enabled"`
	PublicEnabledAt     *time.Time     `json:"public_enabled_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for Calendar
func (Calendar) TableName() string {
	return "calendars"
}

// GenerateSyncToken generates a new sync token from timestamp + random component
func GenerateSyncToken() string {
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	return fmt.Sprintf("%d-%x", timestamp, randomBytes)
}

// GenerateCTag generates a new CTag (same format as sync token)
func GenerateCTag() string {
	return GenerateSyncToken()
}

// GenerateRandomColor generates a random hex color
func GenerateRandomColor() string {
	colors := []string{
		"#3788d8", // Blue
		"#ff5733", // Red-Orange
		"#28a745", // Green
		"#ffc107", // Yellow
		"#6f42c1", // Purple
		"#fd7e14", // Orange
		"#20c997", // Teal
		"#e83e8c", // Pink
	}
	randomBytes := make([]byte, 1)
	rand.Read(randomBytes)
	return colors[int(randomBytes[0])%len(colors)]
}

// UpdateSyncTokens updates both sync token and ctag
func (c *Calendar) UpdateSyncTokens() {
	c.SyncToken = GenerateSyncToken()
	c.CTag = GenerateCTag()
}
