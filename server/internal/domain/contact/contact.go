package contact

import (
	"time"
)

// Contact represents a contact entity suitable for JSON serialization/deserialization
// and mapping to/from vCard data.
type Contact struct {
	ID            string `json:"id"`             // UUID of the AddressObject
	AddressBookID string `json:"addressbook_id"` // UUID of the AddressBook
	UID           string `json:"uid"`            // vCard UID
	Etag          string `json:"etag,omitempty"`

	// Name components
	Prefix        string `json:"prefix,omitempty"`
	GivenName     string `json:"given_name,omitempty"`
	MiddleName    string `json:"middle_name,omitempty"`
	FamilyName    string `json:"family_name,omitempty"`
	Suffix        string `json:"suffix,omitempty"`
	Nickname      string `json:"nickname,omitempty"`
	FormattedName string `json:"formatted_name,omitempty"`

	// Organization
	Organization string `json:"organization,omitempty"`
	Title        string `json:"title,omitempty"`

	// Lists
	Emails    []Email   `json:"emails,omitempty"`
	Phones    []Phone   `json:"phones,omitempty"`
	Addresses []Address `json:"addresses,omitempty"`
	URLs      []URL     `json:"urls,omitempty"`

	// Other
	Birthday  string `json:"birthday,omitempty"` // YYYY-MM-DD
	Notes     string `json:"notes,omitempty"`
	Photo     string `json:"-"`                   // Internal storage for base64 photo data. Not exported in JSON list/get usually (served via URL)
	PhotoType string `json:"-"`                   // e.g. "JPEG", "PNG"
	PhotoURL  string `json:"photo_url,omitempty"` // Constructed URL for the photo

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Email struct {
	Type    string `json:"type"`
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type Phone struct {
	Type    string `json:"type"`
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type Address struct {
	Type       string `json:"type"`
	Street     string `json:"street,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	Country    string `json:"country,omitempty"`
}

type URL struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
