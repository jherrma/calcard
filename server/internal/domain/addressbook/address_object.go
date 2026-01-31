package addressbook

import (
	"time"

	"gorm.io/gorm"
)

type AddressObject struct {
	ID            uint   `gorm:"primaryKey"`
	UUID          string `gorm:"uniqueIndex;size:36;not null"`
	AddressBookID uint   `gorm:"index;not null"`
	Path          string `gorm:"size:255;not null"`
	UID           string `gorm:"index;size:255;not null"` // vCard UID
	ETag          string `gorm:"size:64;not null"`
	VCardData     string `gorm:"type:text;not null"`
	VCardVersion  string `gorm:"size:5;not null"` // "3.0" or "4.0"
	ContentLength int    `gorm:"not null"`
	// Denormalized fields for search
	FormattedName string `gorm:"size:500;index"`
	GivenName     string `gorm:"size:255"`
	FamilyName    string `gorm:"size:255"`
	Email         string `gorm:"size:255;index"` // Primary email
	Phone         string `gorm:"size:50"`        // Primary phone
	Organization  string `gorm:"size:255"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
	AddressBook   AddressBook    `gorm:"foreignKey:AddressBookID"`
}
