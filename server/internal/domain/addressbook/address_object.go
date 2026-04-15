package addressbook

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-vcard"
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

// PopulateDenormFieldsFromVCard parses VCardData and mirrors a small set of
// properties (FN, N, EMAIL, TEL, ORG) into the denormalized columns used for
// list views and search indexes. Every write path that mutates VCardData
// should call this (or ExtractDenormFieldsFromCard, if a parsed Card is
// already on hand) so the columns never drift from the canonical vCard blob.
func (o *AddressObject) PopulateDenormFieldsFromVCard() error {
	if o.VCardData == "" {
		return nil
	}
	card, err := vcard.NewDecoder(strings.NewReader(o.VCardData)).Decode()
	if err != nil {
		return fmt.Errorf("parse vCard: %w", err)
	}
	ExtractDenormFieldsFromCard(card, o)
	return nil
}

// ExtractDenormFieldsFromCard copies the denormalized properties from an
// already-parsed vCard.Card onto the given object. Intended for callers
// that already decoded the vCard for other reasons (UID extraction, etc.)
// and don't want to pay for a second parse.
func ExtractDenormFieldsFromCard(card vcard.Card, o *AddressObject) {
	o.FormattedName = card.PreferredValue(vcard.FieldFormattedName)

	// vCard N value is "Family;Given;Middle;Prefix;Suffix". Reset both fields
	// so a write that removes the N property also clears the columns.
	o.GivenName = ""
	o.FamilyName = ""
	if n := card.Get(vcard.FieldName); n != nil {
		parts := strings.Split(n.Value, ";")
		if len(parts) > 0 {
			o.FamilyName = parts[0]
		}
		if len(parts) > 1 {
			o.GivenName = parts[1]
		}
	}

	o.Email = card.PreferredValue(vcard.FieldEmail)
	o.Phone = card.PreferredValue(vcard.FieldTelephone)
	o.Organization = card.PreferredValue(vcard.FieldOrganization)
}
