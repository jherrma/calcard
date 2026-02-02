package sharing

import (
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// AddressBookShare represents an address book shared with another user
type AddressBookShare struct {
	ID            uint                    `gorm:"primaryKey" json:"id"`
	UUID          string                  `gorm:"uniqueIndex;size:36;not null" json:"uuid"`
	AddressBookID uint                    `gorm:"index;not null;uniqueIndex:idx_addressbook_share_user" json:"addressbook_id"`
	SharedWithID  uint                    `gorm:"index;not null;uniqueIndex:idx_addressbook_share_user" json:"shared_with_id"`
	Permission    string                  `gorm:"size:20;not null" json:"permission"` // "read" or "read-write"
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	DeletedAt     gorm.DeletedAt          `gorm:"index" json:"-"`
	AddressBook   addressbook.AddressBook `gorm:"foreignKey:AddressBookID" json:"-"`
	SharedWith    user.User               `gorm:"foreignKey:SharedWithID" json:"shared_with"`
}

// TableName overrides the table name
func (AddressBookShare) TableName() string {
	return "addressbook_shares"
}
