package importexport

import (
	"context"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// importStubRepo implements the slice of addressbook.Repository the contact
// import uses, keeping created objects in memory.
type importStubRepo struct {
	addressbook.Repository
	created []*addressbook.AddressObject
}

func (s *importStubRepo) GetUserPermission(ctx context.Context, addressBookID, userID uint) (addressbook.AddressBookPermission, error) {
	return addressbook.PermissionOwner, nil
}

func (s *importStubRepo) GetByID(ctx context.Context, id uint) (*addressbook.AddressBook, error) {
	return &addressbook.AddressBook{ID: id}, nil
}

func (s *importStubRepo) GetObjectByUID(ctx context.Context, addressBookID uint, uid string) (*addressbook.AddressObject, error) {
	for _, o := range s.created {
		if o.UID == uid {
			return o, nil
		}
	}
	return nil, nil
}

func (s *importStubRepo) CreateObject(ctx context.Context, object *addressbook.AddressObject) error {
	s.created = append(s.created, object)
	return nil
}

// TestContactImportValidatesCards is the regression test for F27 (b) and (c):
// the .vcf import used to store every card verbatim, so a card without
// VERSION was stored (its CardDAV GET then failed with a 500), a vCard 2.1
// card was served although address books only advertise 3.0 and 4.0, and a
// card without FN stayed nameless. Invalid cards now fail with a reason; a
// missing FN is derived.
func TestContactImportValidatesCards(t *testing.T) {
	card := func(lines ...string) string {
		return "BEGIN:VCARD\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCARD\r\n"
	}
	vcf := card("VERSION:3.0", "UID:ok-1", "FN:Valid Person", "N:Person;Valid;;;") +
		card("UID:no-version", "FN:No Version", "N:Version;No;;;") +
		card("VERSION:2.1", "UID:old-1", "FN:Old Card", "N:Card;Old;;;") +
		card("VERSION:3.0", "UID:no-fn", "N:Lovelace;Ada;;;", "ORG:Analytical Engines") +
		card("VERSION:4.0", "UID:org-only", "ORG:Acme Inc.;Sales") +
		card("VERSION:3.0", "UID:nameless", "NOTE:nothing to call it") +
		"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:no-end\r\nFN:No End\r\n"

	repo := &importStubRepo{}
	res, err := NewContactImportUseCase(repo).Execute(context.Background(), 1, 1, []byte(vcf), ImportOptions{})
	require.NoError(t, err)

	assert.Equal(t, 7, res.Total)
	assert.Equal(t, 3, res.Imported, "the valid card and the two with a derivable FN: %+v", res)
	assert.Equal(t, 4, res.Failed, "%+v", res)

	reasons := map[string]string{}
	for _, e := range res.Errors {
		reasons[e.UID] = e.Error
	}
	assert.Contains(t, reasons["no-version"], "missing VERSION")
	assert.Contains(t, reasons["old-1"], `unsupported vCard version "2.1"`)
	assert.Contains(t, reasons["nameless"], "no FN")
	assert.Contains(t, reasons["no-end"], "invalid vCard")

	stored := map[string]*addressbook.AddressObject{}
	for _, o := range repo.created {
		stored[o.UID] = o
	}
	require.Len(t, stored, 3)
	for uid, o := range stored {
		assert.Contains(t, o.VCardData, "VERSION:", uid)
		assert.Equal(t, len(o.VCardData), o.ContentLength, uid)
	}

	if o := stored["no-fn"]; assert.NotNil(t, o) {
		assert.Contains(t, o.VCardData, "FN:Ada Lovelace\r\n", "FN derived from N")
		assert.Equal(t, "Ada Lovelace", o.FormattedName)
	}
	if o := stored["org-only"]; assert.NotNil(t, o) {
		assert.Contains(t, o.VCardData, "FN:Acme Inc.\r\n", "FN derived from ORG")
		assert.Equal(t, "4.0", o.VCardVersion, "the stored version is the card's")
	}
	if o := stored["ok-1"]; assert.NotNil(t, o) {
		assert.Contains(t, o.VCardData, "FN:Valid Person\r\n")
		assert.Equal(t, "3.0", o.VCardVersion)
	}
}

// A card without UID gets one, and duplicate_handling=duplicate re-keys the
// parsed card rather than a raw "UID:" line.
func TestContactImportMintsAndReplacesUIDs(t *testing.T) {
	repo := &importStubRepo{}
	uc := NewContactImportUseCase(repo)

	res, err := uc.Execute(context.Background(), 1, 1,
		[]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:No Uid\r\nEND:VCARD\r\n"), ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, res.Imported, "%+v", res)
	minted := repo.created[0].UID
	require.NotEmpty(t, minted)
	assert.Contains(t, repo.created[0].VCardData, "UID:"+minted+"\r\n")

	again := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID;VALUE=text:" + minted + "\r\nFN:No Uid\r\nEND:VCARD\r\n"
	res, err = uc.Execute(context.Background(), 1, 1, []byte(again), ImportOptions{DuplicateHandling: "duplicate"})
	require.NoError(t, err)
	require.Equal(t, 1, res.Imported, "%+v", res)
	copied := repo.created[1]
	assert.NotEqual(t, minted, copied.UID)
	assert.Contains(t, copied.VCardData, "UID:"+copied.UID)
	assert.NotContains(t, copied.VCardData, minted, "the copy must not keep the original UID anywhere")
}

func (s *importStubRepo) GetObjectByPath(ctx context.Context, addressBookID uint, path string) (*addressbook.AddressObject, error) {
	for _, o := range s.created {
		if o.Path == path {
			return o, nil
		}
	}
	return nil, nil
}
