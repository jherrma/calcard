package addressbook

import (
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

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

// GenerateSyncToken generates a new sync token
func GenerateSyncToken() string {
	return fmt.Sprintf("data:,%d", time.Now().UnixNano())
}

// GenerateCTag generates a new CTag
func GenerateCTag() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// UpdateSyncTokens updates both sync token and ctag
func (ab *AddressBook) UpdateSyncTokens() {
	ab.SyncToken = GenerateSyncToken()
	ab.CTag = GenerateCTag()
}
