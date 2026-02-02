package contact

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
)

// ToContact parses vCard data into a Contact struct
func ToContact(vcardData string) (*contact.Contact, error) {
	dec := vcard.NewDecoder(strings.NewReader(vcardData))
	card, err := dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("failed to parse vcard: %w", err)
	}

	c := &contact.Contact{
		UID:           card.Value(vcard.FieldUID),
		FormattedName: card.PreferredValue(vcard.FieldFormattedName),
		Organization:  card.PreferredValue(vcard.FieldOrganization),
		Title:         card.PreferredValue(vcard.FieldTitle),
		Birthday:      card.PreferredValue(vcard.FieldBirthday),
		Notes:         card.PreferredValue(vcard.FieldNote),
	}

	// Name
	if n := card.Name(); n != nil {
		c.FamilyName = n.FamilyName
		c.GivenName = n.GivenName
		c.MiddleName = n.AdditionalName
		c.Prefix = n.HonorificPrefix
		c.Suffix = n.HonorificSuffix
	}

	// Nickname
	if nn := card.PreferredValue(vcard.FieldNickname); nn != "" {
		c.Nickname = nn
	}

	// Emails
	for _, field := range card[vcard.FieldEmail] {
		c.Emails = append(c.Emails, contact.Email{
			Value:   field.Value,
			Type:    extractType(field.Params),
			Primary: isPrimary(field.Params),
		})
	}

	// Phones
	for _, field := range card[vcard.FieldTelephone] {
		c.Phones = append(c.Phones, contact.Phone{
			Value:   field.Value,
			Type:    extractType(field.Params),
			Primary: isPrimary(field.Params),
		})
	}

	// Addresses
	for _, addr := range card.Addresses() {
		// addr is *vcard.Address
		// Check for Params on the underlying field
		// vcard.Address embeds *Field
		c.Addresses = append(c.Addresses, contact.Address{
			Type:       extractType(addr.Field.Params),
			Street:     addr.StreetAddress,
			City:       addr.Locality,
			State:      addr.Region,
			PostalCode: addr.PostalCode,
			Country:    addr.Country,
		})
	}

	// URLs
	for _, field := range card[vcard.FieldURL] {
		c.URLs = append(c.URLs, contact.URL{
			Value: field.Value,
			Type:  extractType(field.Params),
		})
	}

	// Photo (base64)
	if field := card.Get(vcard.FieldPhoto); field != nil {
		c.Photo = field.Value
		c.PhotoType = extractType(field.Params)
	}

	return c, nil
}

// ToVCard converts a Contact struct to vCard string
func ToVCard(c *contact.Contact) (string, error) {
	card := make(vcard.Card)
	card.SetValue(vcard.FieldVersion, "3.0")
	card.SetValue(vcard.FieldUID, c.UID)
	card.SetValue(vcard.FieldFormattedName, c.FormattedName)

	// Name
	name := &vcard.Name{
		FamilyName:      c.FamilyName,
		GivenName:       c.GivenName,
		AdditionalName:  c.MiddleName,
		HonorificPrefix: c.Prefix,
		HonorificSuffix: c.Suffix,
	}
	card.SetName(name)

	if c.Nickname != "" {
		card.SetValue(vcard.FieldNickname, c.Nickname)
	}
	if c.Organization != "" {
		card.SetValue(vcard.FieldOrganization, c.Organization)
	}
	if c.Title != "" {
		card.SetValue(vcard.FieldTitle, c.Title)
	}
	if c.Birthday != "" {
		card.SetValue(vcard.FieldBirthday, c.Birthday)
	}
	if c.Notes != "" {
		card.SetValue(vcard.FieldNote, c.Notes)
	}

	// Emails
	for _, e := range c.Emails {
		params := make(vcard.Params)
		if e.Type != "" {
			params.Set(vcard.ParamType, e.Type)
		}
		if e.Primary {
			addTypeParam(params, "PREF")
		}
		card.Add(vcard.FieldEmail, &vcard.Field{Value: e.Value, Params: params})
	}

	// Phones
	for _, p := range c.Phones {
		params := make(vcard.Params)
		if p.Type != "" {
			params.Set(vcard.ParamType, p.Type)
		}
		if p.Primary {
			addTypeParam(params, "PREF")
		}
		card.Add(vcard.FieldTelephone, &vcard.Field{Value: p.Value, Params: params})
	}

	// Addresses
	for _, a := range c.Addresses {
		addr := &vcard.Address{
			StreetAddress: a.Street,
			Locality:      a.City,
			Region:        a.State,
			PostalCode:    a.PostalCode,
			Country:       a.Country,
		}

		// Create field and params manually to ensure we attach them correctly
		// Since card.AddAddress uses address.field() which might not have our params yet
		if addr.Field == nil {
			addr.Field = &vcard.Field{}
		}
		if addr.Field.Params == nil {
			addr.Field.Params = make(vcard.Params)
		}

		if a.Type != "" {
			addr.Field.Params.Set(vcard.ParamType, a.Type)
		}

		card.AddAddress(addr)
	}

	// URLs
	for _, u := range c.URLs {
		params := make(vcard.Params)
		if u.Type != "" {
			params.Set(vcard.ParamType, u.Type)
		}
		card.Add(vcard.FieldURL, &vcard.Field{Value: u.Value, Params: params})
	}

	// Photo
	if c.Photo != "" {
		params := make(vcard.Params)
		params.Set("ENCODING", "b")
		t := "JPEG"
		if c.PhotoType != "" {
			t = c.PhotoType
		}
		params.Set("TYPE", t)
		card.Add(vcard.FieldPhoto, &vcard.Field{Value: c.Photo, Params: params})
	}

	// Revision
	card.SetValue(vcard.FieldRevision, time.Now().Format("20060102T150405Z"))

	var buf bytes.Buffer
	enc := vcard.NewEncoder(&buf)
	if err := enc.Encode(card); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Helpers

func extractType(params vcard.Params) string {
	if params == nil {
		return ""
	}
	types := params.Types()
	if len(types) > 0 {
		return strings.ToUpper(types[0])
	}
	return ""
}

func isPrimary(params vcard.Params) bool {
	if params == nil {
		return false
	}
	return params.HasType("PREF") || params.Get("PREF") != ""
}

func addTypeParam(params vcard.Params, t string) {
	params.Add(vcard.ParamType, t)
}

// FromAddressObject maps an AddressObject to a Contact using denormalized fields.
// This is useful for list views where full vCard parsing is not required/too expensive.
func FromAddressObject(obj *addressbook.AddressObject) *contact.Contact {
	c := &contact.Contact{
		ID:            obj.UUID,
		AddressBookID: fmt.Sprintf("%d", obj.AddressBookID),
		UID:           obj.UID,
		Etag:          obj.ETag,
		FormattedName: obj.FormattedName,
		FamilyName:    obj.FamilyName,
		GivenName:     obj.GivenName,
		Organization:  obj.Organization,
		CreatedAt:     obj.CreatedAt,
		UpdatedAt:     obj.UpdatedAt,
	}

	if obj.Email != "" {
		c.Emails = []contact.Email{{Value: obj.Email, Primary: true}}
	}
	if obj.Phone != "" {
		c.Phones = []contact.Phone{{Value: obj.Phone, Primary: true}}
	}

	return c
}
