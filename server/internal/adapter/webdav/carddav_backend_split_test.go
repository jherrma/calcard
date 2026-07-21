package webdav

import (
	"bytes"
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/require"
)

// TestMapAddressObjectSplitsCategories is the regression test for #44: every
// DAV path that serializes a contact (GET, REPORT multiget, addressbook-query,
// PROPFIND-with-address-data) goes through mapAddressObject, so splitting
// CATEGORIES here fixes the comma corruption on the multiget path phones sync
// over — not just the single-vCard GET the old post-processor covered.
func TestMapAddressObjectSplitsCategories(t *testing.T) {
	b := &CardDAVBackend{}
	// Stored with the escaped comma exactly as go-vcard's encoder would have
	// written a multi-valued CATEGORIES.
	stored := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Jane Doe\r\nCATEGORIES:Friends\\,VIP\r\nEND:VCARD\r\n"
	obj := &addressbook.AddressObject{VCardData: stored, ETag: "etag-1", Path: "jane.vcf"}

	ao, err := b.mapAddressObject("/dav/u/addressbooks/personal/jane.vcf", obj)
	require.NoError(t, err)
	require.Len(t, ao.Card["CATEGORIES"], 2, "CATEGORIES must be split into two instances")

	var buf bytes.Buffer
	require.NoError(t, vcard.NewEncoder(&buf).Encode(ao.Card))
	out := buf.String()
	require.NotContains(t, out, `\,`, "served card must not contain an escaped comma")
	require.Contains(t, out, "CATEGORIES:Friends")
	require.Contains(t, out, "CATEGORIES:VIP")
	require.Equal(t, 2, strings.Count(out, "CATEGORIES:"), "expected two CATEGORIES lines")
}
