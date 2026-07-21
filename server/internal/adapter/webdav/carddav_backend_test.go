package webdav

import (
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
)

// TestMapAddressBookAdvertisesRealMaxResourceSize is the regression test for
// #45: the advertised CARDDAV:max-resource-size must reflect what the server
// actually accepts (the 10MB global request cap), not the old 100KB fiction
// that made spec-honoring clients pre-reject any contact with a photo.
func TestMapAddressBookAdvertisesRealMaxResourceSize(t *testing.T) {
	b := &CardDAVBackend{}
	ab := &addressbook.AddressBook{Name: "Personal", Path: "personal"}

	got := b.mapAddressBook("testuser", ab)

	assert.Equal(t, int64(10*1024*1024), got.MaxResourceSize,
		"advertised max-resource-size must match the server's real 10MB request cap")
}
