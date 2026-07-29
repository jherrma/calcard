package addressbook

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// AddressBookPermission represents a user's effective access level on an
// address book. Mirrors calendar.CalendarPermission so both collection types
// are authorized the same way (#53): the REST layer used to gate contacts on
// ownership alone, which locked read-write sharees out of the web UI even
// though CardDAV honored their share.
type AddressBookPermission int

const (
	// PermissionNone means the book doesn't exist, or exists but is neither
	// owned by nor shared with the user. Callers treat it as "not found" so
	// existence isn't leaked across users.
	PermissionNone AddressBookPermission = iota
	PermissionRead
	PermissionReadWrite
	PermissionOwner
)

// CanRead reports whether the permission grants any access at all.
func (p AddressBookPermission) CanRead() bool {
	return p != PermissionNone
}

// CanWrite reports whether the permission grants write access — the owner, or
// a sharee whose share is "read-write".
func (p AddressBookPermission) CanWrite() bool {
	return p == PermissionOwner || p == PermissionReadWrite
}

type AddressBook struct {
	ID          uint   `gorm:"primaryKey"`
	UUID        string `gorm:"uniqueIndex;size:36;not null"`
	UserID      uint   `gorm:"index;not null"`
	Path        string `gorm:"size:255;not null"`
	Name        string `gorm:"size:255;not null"`
	Description string `gorm:"size:1000"`
	SyncToken   string `gorm:"size:64;not null"`
	CTag        string `gorm:"size:64;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt  `gorm:"index"`
	User        user.User       `gorm:"foreignKey:UserID"`
	Contacts    []AddressObject `gorm:"foreignKey:AddressBookID"`
}

// GenerateSyncToken generates a new sync token. Includes a crypto-random suffix
// so two changes minted in the same nanosecond tick still get distinct tokens
// (mirrors calendar.GenerateSyncToken).
func GenerateSyncToken() string {
	randomBytes := make([]byte, 8)
	_, _ = rand.Read(randomBytes)
	return fmt.Sprintf("data:,%d-%x", time.Now().UnixNano(), randomBytes)
}

// GenerateCTag generates a new CTag
func GenerateCTag() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// NewETag generates a new ETag value. The value is stored UNQUOTED; the
// transport layer (go-webdav, and the hand-rolled sync REPORT) adds the
// surrounding quotes when serializing. Never store a quoted ETag.
//
// Deliberately NOT the sync-token format: ETags travel in If-Match /
// If-None-Match headers, where the sync token's "data:," prefix (note the
// comma) invites naive header-list parsers to split the value — which is
// exactly how contact ETags ended up reading "data:,1751...-ab".
func NewETag() string {
	randomBytes := make([]byte, 8)
	_, _ = rand.Read(randomBytes)
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), randomBytes)
}

// UpdateSyncTokens updates both sync token and ctag
func (ab *AddressBook) UpdateSyncTokens() {
	ab.SyncToken = GenerateSyncToken()
	ab.CTag = GenerateCTag()
}
