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

// managedVCardFields are exactly the properties applyContactToCard writes.
// PatchVCard clears these before re-writing them, so a web-UI edit refreshes
// the managed values while leaving every other property (CATEGORIES, X-*,
// IMPP, grouped props, …) untouched. VERSION and UID are deliberately NOT
// listed: VERSION is set only when absent and UID only when the contact
// carries one, so an existing card's identity is never clobbered.
var managedVCardFields = []string{
	vcard.FieldFormattedName,
	vcard.FieldName,
	vcard.FieldNickname,
	vcard.FieldOrganization,
	vcard.FieldTitle,
	vcard.FieldBirthday,
	vcard.FieldNote,
	vcard.FieldEmail,
	vcard.FieldTelephone,
	vcard.FieldAddress,
	vcard.FieldURL,
	vcard.FieldPhoto,
	vcard.FieldRevision,
}

// applyContactToCard writes the managed contact fields onto card, replacing any
// existing managed values but preserving unmanaged properties already present.
func applyContactToCard(card vcard.Card, c *contact.Contact) {
	// Clear the managed fields before re-writing them, but only the plain
	// (ungrouped) instances. Grouped entries such as item1.URL are part of an
	// Apple-style custom-label linkage (item1.URL + item1.X-ABLabel); deleting
	// them here would orphan the X-ABLabel, so they are left untouched.
	for _, f := range managedVCardFields {
		fields := card[f]
		if len(fields) == 0 {
			continue
		}
		grouped := fields[:0:0]
		for _, fld := range fields {
			if fld.Group != "" {
				grouped = append(grouped, fld)
			}
		}
		if len(grouped) > 0 {
			card[f] = grouped
		} else {
			delete(card, f)
		}
	}

	if card.Value(vcard.FieldVersion) == "" {
		card.SetValue(vcard.FieldVersion, "3.0")
	}
	if c.UID != "" {
		card.SetValue(vcard.FieldUID, c.UID)
	}
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
}

// ToVCard converts a Contact struct to a fresh vCard string (create path).
func ToVCard(c *contact.Contact) (string, error) {
	card := make(vcard.Card)
	applyContactToCard(card, c)
	return encodeCard(card)
}

// PatchVCard applies the managed fields of c onto an existing vCard blob,
// preserving every unmanaged property (CATEGORIES, X-*, IMPP, grouped props,
// custom params) that a web-UI edit would otherwise silently drop. Falls back
// to a fresh ToVCard when the existing data can't be decoded.
func PatchVCard(existingVCardData string, c *contact.Contact) (string, error) {
	card, err := vcard.NewDecoder(strings.NewReader(existingVCardData)).Decode()
	if err != nil {
		return ToVCard(c)
	}
	applyContactToCard(card, c)
	return encodeCard(card)
}

func encodeCard(card vcard.Card) (string, error) {
	var buf bytes.Buffer
	if err := vcard.NewEncoder(&buf).Encode(card); err != nil {
		return "", err
	}
	return restoreCommaLists(buf.String()), nil
}

// commaListFields are vCard properties whose value is a comma-separated list
// (RFC 6350). go-vcard's encoder escapes every comma unconditionally, so a
// preserved value such as CATEGORIES:Friends,VIP is emitted as
// CATEGORIES:Friends\,VIP — which a strict client reads as a single category
// literally named "Friends,VIP". restoreCommaLists un-escapes the list
// separators for these properties after encoding.
var commaListFields = map[string]bool{
	vcard.FieldCategories: true,
}

// restoreCommaLists rewrites the encoded vCard so comma-list properties keep
// their raw comma separators instead of the escaped form go-vcard emits. The
// encoder writes one unfolded property per line, so a line-oriented pass is
// safe here.
func restoreCommaLists(vcardStr string) string {
	lines := strings.Split(vcardStr, "\r\n")
	for i, line := range lines {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		// Property name is the token before the first ';' (params), with any
		// group prefix ("item1.") stripped.
		name := line[:colon]
		if semi := strings.IndexByte(name, ';'); semi >= 0 {
			name = name[:semi]
		}
		if dot := strings.IndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}
		if commaListFields[strings.ToUpper(name)] {
			lines[i] = line[:colon+1] + unescapeSeparatorCommas(line[colon+1:])
		}
	}
	return strings.Join(lines, "\r\n")
}

// unescapeSeparatorCommas turns escaped commas ("\,") back into raw list
// separators while leaving other escape sequences (notably "\\" and "\n")
// intact, so a literal backslash immediately before a separator survives.
func unescapeSeparatorCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == ',' {
				b.WriteByte(',')
			} else {
				b.WriteByte(s[i])
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
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

// FromAddressObject maps an AddressObject to a Contact by parsing the
// canonical vCard blob. This is the same conversion single-contact GET uses
// (see GetUseCase.Execute), so list and detail endpoints return identical
// data — no more drift between "what you see in the list" and "what you see
// on the detail page" caused by denormalized columns lagging behind the
// vCard.
//
// Parse failures fall back to what can be read from the denormalized
// columns so a broken vCard doesn't hide the row entirely from list views.
func FromAddressObject(obj *addressbook.AddressObject) *contact.Contact {
	if obj == nil {
		return nil
	}
	c, err := ToContact(obj.VCardData)
	if err != nil || c == nil {
		c = &contact.Contact{
			UID:           obj.UID,
			FormattedName: obj.FormattedName,
			GivenName:     obj.GivenName,
			FamilyName:    obj.FamilyName,
			Organization:  obj.Organization,
		}
		if obj.Email != "" {
			c.Emails = []contact.Email{{Value: obj.Email, Primary: true}}
		}
		if obj.Phone != "" {
			c.Phones = []contact.Phone{{Value: obj.Phone, Primary: true}}
		}
	}

	// Apply the object-level fields that aren't part of the vCard itself.
	c.ID = obj.UUID
	c.AddressBookID = fmt.Sprintf("%d", obj.AddressBookID)
	c.Etag = obj.ETag
	c.CreatedAt = obj.CreatedAt
	c.UpdatedAt = obj.UpdatedAt
	return c
}
