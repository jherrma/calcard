package addressbook

import (
	"time"
)

// SyncChangeLog tracks changes to address objects for WebDAV-Sync (RFC 6578)
type SyncChangeLog struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	AddressBookID uint      `gorm:"index;not null" json:"address_book_id"`
	ResourcePath  string    `gorm:"size:255;not null" json:"resource_path"`
	ResourceUID   string    `gorm:"size:255" json:"resource_uid"`
	ChangeType    string    `gorm:"size:20;not null" json:"change_type"` // created, modified, deleted
	SyncToken     string    `gorm:"index;size:64;not null" json:"sync_token"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}

// TableName specifies the table name for SyncChangeLog
func (SyncChangeLog) TableName() string {
	return "addressbook_sync_change_logs"
}
