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
	ID                  uint       `gorm:"primaryKey" json:"id"`
	UUID                string     `gorm:"uniqueIndex;size:36;not null" json:"uuid"`
	UserID              uint       `gorm:"index;not null" json:"user_id"`
	Owner               user.User  `gorm:"foreignKey:UserID" json:"-"`
	Path                string     `gorm:"size:255;not null" json:"path"` // URL path component
	Name                string     `gorm:"size:255;not null" json:"name"`
	Description         string     `gorm:"size:1000" json:"description"`
	Color               string     `gorm:"size:7;not null" json:"color"` // #RRGGBB
	Timezone            string     `gorm:"size:50;not null" json:"timezone"`
	SupportedComponents string     `gorm:"size:100;not null" json:"supported_components"` // "VEVENT,VTODO"
	SyncToken           string     `gorm:"size:64;not null;default:''" json:"sync_token"`
	CTag                string     `gorm:"column:ctag;size:64;not null;default:''" json:"ctag"`
	PublicToken         *string    `gorm:"uniqueIndex;size:64" json:"-"`
	PublicEnabled       bool       `gorm:"default:false" json:"public_enabled"`
	PublicEnabledAt     *time.Time `json:"public_enabled_at,omitempty"`
	// Subscribed marks a calendar as the local mirror of a remote iCalendar
	// feed (story 100). Its details live in the CalendarSubscription sidecar.
	//
	// The flag makes the collection read-only to EVERY write path — REST,
	// CalDAV and MCP alike, and to the owner as much as to a sharee: the next
	// refresh replaces the collection's contents wholesale, so a write here
	// would be silently discarded, which is worse than being refused. The
	// owner keeps full control of the collection itself (rename, recolor,
	// share, delete) because those are ownership decisions, not object writes.
	Subscribed bool           `gorm:"not null;default:false" json:"subscribed"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for Calendar
func (Calendar) TableName() string {
	return "calendars"
}

// GenerateSyncToken generates a new sync token from timestamp + random
// component. RFC 6578 §6.4 requires the sync token to be a URI, so it carries
// the same "data:," scheme the addressbook domain already uses. Tokens are
// compared by exact string equality against stored change-log rows, so tokens
// clients already hold keep resolving; only newly minted ones gain the prefix.
func GenerateSyncToken() string {
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	return fmt.Sprintf("data:,%d-%x", timestamp, randomBytes)
}

// GenerateCTag generates a new CTag (same format as sync token). getctag is an
// opaque per-collection string to clients, so the "data:," prefix it inherits
// from GenerateSyncToken is harmless.
func GenerateCTag() string {
	return GenerateSyncToken()
}

// NewETag generates a new ETag value. The value is stored UNQUOTED; the
// transport layer (go-webdav, and the hand-rolled sync REPORT) adds the
// surrounding quotes when serializing. Never store a quoted ETag.
//
// Deliberately NOT the sync-token format: ETags travel in If-Match /
// If-None-Match headers, where a "data:," prefix (note the comma) invites naive
// header-list parsers to split the value.
func NewETag() string {
	randomBytes := make([]byte, 8)
	_, _ = rand.Read(randomBytes)
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), randomBytes)
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

// EffectivePermission caps a resolved permission at read-only for a subscribed
// calendar (story 100).
//
// It is the single place the "a subscription is read-only" rule is expressed,
// and every adapter that resolves a permission — REST, CalDAV, MCP — funnels
// through it, so the rule cannot hold on one protocol and not another. Capping
// the PERMISSION rather than adding a separate check at each write site is
// what makes that work: a write path that already refuses PermissionRead needs
// no new code and cannot be forgotten.
//
// It caps only object-level access. Ownership of the collection is unaffected,
// so the owner can still rename, recolour, share and delete a subscribed
// calendar; those sites check cal.UserID directly.
func EffectivePermission(cal *Calendar, perm CalendarPermission) CalendarPermission {
	if cal != nil && cal.Subscribed && perm > PermissionRead {
		return PermissionRead
	}
	return perm
}
