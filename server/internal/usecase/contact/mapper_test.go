package contact

import (
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
	"github.com/stretchr/testify/assert"
)

func TestToContact(t *testing.T) {
	vcardData := `BEGIN:VCARD
VERSION:3.0
UID:uuid-1234
FN:John Doe
N:Doe;John;;;
EMAIL;TYPE=WORK:john@work.com
EMAIL;TYPE=HOME:john@home.com
TEL;TYPE=CELL:123-456-7890
ADR;TYPE=WORK:;;123 Main St;City;State;12345;Country
ORG:ACME Corp
TITLE:Engineer
NOTE:Some notes
END:VCARD`

	c, err := ToContact(vcardData)
	assert.NoError(t, err)
	assert.NotNil(t, c)

	assert.Equal(t, "uuid-1234", c.UID)
	assert.Equal(t, "John Doe", c.FormattedName)
	assert.Equal(t, "Doe", c.FamilyName)
	assert.Equal(t, "John", c.GivenName)
	assert.Equal(t, "ACME Corp", c.Organization)
	assert.Equal(t, "Engineer", c.Title)
	assert.Equal(t, "Some notes", c.Notes)

	assert.Len(t, c.Emails, 2)
	assert.Equal(t, "john@work.com", c.Emails[0].Value)
	assert.Equal(t, "WORK", strings.ToUpper(c.Emails[0].Type))

	assert.Len(t, c.Phones, 1)
	assert.Equal(t, "123-456-7890", c.Phones[0].Value)
	assert.Equal(t, "CELL", strings.ToUpper(c.Phones[0].Type))

	assert.Len(t, c.Addresses, 1)
	assert.Equal(t, "123 Main St", c.Addresses[0].Street)
	assert.Equal(t, "City", c.Addresses[0].City)
}

func TestToVCard(t *testing.T) {
	c := &contact.Contact{
		UID:           "uuid-5678",
		FormattedName: "Jane Smith",
		FamilyName:    "Smith",
		GivenName:     "Jane",
		Organization:  "Tech Inc",
		Emails: []contact.Email{
			{Value: "jane@tech.com", Type: "WORK", Primary: true},
		},
		Phones: []contact.Phone{
			{Value: "555-555-5555", Type: "HOME"},
		},
	}

	vcardStr, err := ToVCard(c)
	assert.NoError(t, err)
	assert.Contains(t, vcardStr, "BEGIN:VCARD")
	assert.Contains(t, vcardStr, "FN:Jane Smith")
	assert.Contains(t, vcardStr, "N:Smith;Jane")
	assert.Contains(t, vcardStr, "ORG:Tech Inc")
	assert.Contains(t, vcardStr, "EMAIL;TYPE=WORK;TYPE=PREF:jane@tech.com")
	assert.Contains(t, vcardStr, "TEL;TYPE=HOME:555-555-5555") // Could verify param order but contains is simpler
	assert.Contains(t, vcardStr, "END:VCARD")
}

// TestPatchVCard is the regression test for M6: editing a contact through the
// web UI must preserve vCard properties the UI doesn't manage (CATEGORIES,
// X-*, IMPP, grouped labels) instead of dropping them, while still refreshing
// the managed fields exactly once.
func TestPatchVCard(t *testing.T) {
	existing := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:patch-uid\r\n" +
		"FN:Old Name\r\nN:Name;Old;;;\r\n" +
		"CATEGORIES:Friends,VIP\r\n" +
		"X-CUSTOM:keep-me\r\n" +
		"IMPP:xmpp:old@chat.example\r\n" +
		"item1.URL:https://example.com\r\n" +
		"item1.X-ABLabel:homepage\r\n" +
		"END:VCARD\r\n"

	// Parse into a contact, change only the name, patch back.
	c, err := ToContact(existing)
	assert.NoError(t, err)
	c.FormattedName = "New Name"
	c.FamilyName = "Name"
	c.GivenName = "New"

	result, err := PatchVCard(existing, c)
	assert.NoError(t, err)

	// Managed field refreshed, exactly once, old value gone.
	assert.Contains(t, result, "FN:New Name")
	assert.NotContains(t, result, "Old Name")
	fnCount := 0
	for _, l := range strings.Split(result, "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(l)), "FN:") {
			fnCount++
		}
	}
	assert.Equal(t, 1, fnCount, "exactly one FN line expected")

	assert.Contains(t, result, "X-CUSTOM:keep-me", "X-CUSTOM must survive the edit")
	assert.Contains(t, result, "chat.example", "IMPP must survive the edit")

	// Bug (1): the comma-separated CATEGORIES list must survive as separate
	// categories, not collapse into one literal "Friends\,VIP" value. Assert on
	// the wire form directly — go-vcard's decoder un-escapes "\," to "," and its
	// Categories() splits on every comma, so a decode-based check cannot tell the
	// corrupted form apart from the correct one.
	assert.Contains(t, result, "CATEGORIES:Friends,VIP", "CATEGORIES must keep raw list separators")
	assert.NotContains(t, result, `Friends\,VIP`, "CATEGORIES must not escape the list separator")

	// Bug (2): the grouped item1.URL must keep its group so the paired
	// item1.X-ABLabel is not orphaned. Decode and assert the real structure.
	card, err := vcard.NewDecoder(strings.NewReader(result)).Decode()
	assert.NoError(t, err)

	var groupedURL *vcard.Field
	for _, f := range card[vcard.FieldURL] {
		if f.Group == "item1" {
			groupedURL = f
			break
		}
	}
	if assert.NotNil(t, groupedURL, "item1.URL must keep its group") {
		assert.Equal(t, "https://example.com", groupedURL.Value)
	}

	label := card.Get("X-ABLABEL")
	if assert.NotNil(t, label, "item1.X-ABLabel must survive the edit") {
		assert.Equal(t, "item1", label.Group, "X-ABLabel must keep its group")
		assert.Equal(t, "homepage", label.Value)
	}
}

// TestFromAddressObjectPhotoURL is the regression test for #10: list/search
// mappings must expose a photo via a relative URL (loaded separately by the
// client) whenever the hydrated vCard carries a PHOTO, and must not inline the
// base64 blob. Contacts without a photo carry no photo_url.
func TestFromAddressObjectPhotoURL(t *testing.T) {
	withPhoto := &addressbook.AddressObject{
		UUID:          "contact-uuid-1",
		AddressBookID: 7,
		VCardData: "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:with-photo\r\n" +
			"FN:Ada Lovelace\r\nN:Lovelace;Ada;;;\r\n" +
			"PHOTO;ENCODING=b;TYPE=JPEG:SGVsbG8=\r\nEND:VCARD\r\n",
	}
	c := FromAddressObject(withPhoto)
	assert.NotNil(t, c)
	assert.Equal(t, "/api/v1/addressbooks/7/contacts/contact-uuid-1/photo", c.PhotoURL)
	assert.Empty(t, c.Photo, "base64 blob must not be inlined in the mapped contact")

	noPhoto := &addressbook.AddressObject{
		UUID:          "contact-uuid-2",
		AddressBookID: 7,
		VCardData: "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:no-photo\r\n" +
			"FN:Grace Hopper\r\nN:Hopper;Grace;;;\r\nEND:VCARD\r\n",
	}
	c = FromAddressObject(noPhoto)
	assert.NotNil(t, c)
	assert.Empty(t, c.PhotoURL, "contacts without a photo must not carry a photo_url")
}
