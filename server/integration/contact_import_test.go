//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContactImportDuplicateDetection is the regression test for H11: contact
// import must detect duplicates by the vCard UID within the target address
// book. The old code looked the UID up against the internal DB UUID, so it
// never matched and every re-import silently doubled the contacts.
func TestContactImportDuplicateDetection(t *testing.T) {
	token := registerAndLogin(t, "contact-import-dup@example.test", "importSecret!123", "Import Dup User")
	_, abUUID := createAddressBook(t, token, "Import Target")

	vcf := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:import-dup-1\r\nFN:Alice Example\r\nN:Example;Alice;;;\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:import-dup-2\r\nFN:Bob Example\r\nN:Example;Bob;;;\r\nEND:VCARD\r\n"

	// First import: both contacts are new.
	res := importContacts(t, token, abUUID, "skip", []byte(vcf))
	assert.Equal(t, 2, res.Imported, "first import should add both contacts")
	require.Equal(t, 2, contactCount(t, token, abUUID))

	// Re-import with skip: both detected as duplicates, nothing added.
	res = importContacts(t, token, abUUID, "skip", []byte(vcf))
	assert.Equalf(t, 0, res.Imported, "skip re-import must import nothing: %+v", res)
	assert.Equalf(t, 2, res.Skipped, "skip re-import must skip both: %+v", res)
	assert.Equal(t, 2, contactCount(t, token, abUUID), "skip re-import must not duplicate")

	// Re-import with replace, MUTATING a field. Replace must both dedup by UID
	// (count stays 2) AND actually overwrite — a replace path that silently
	// no-ops (skips) would pass a count-only check identically to a correct
	// overwrite, so assert the new FN is what the subsequent GET returns.
	mutated := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:import-dup-1\r\nFN:Alice Renamed\r\nN:Example;Alice;;;\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:import-dup-2\r\nFN:Bob Example\r\nN:Example;Bob;;;\r\nEND:VCARD\r\n"
	res = importContacts(t, token, abUUID, "replace", []byte(mutated))
	assert.Equal(t, 2, contactCount(t, token, abUUID), "replace re-import must not duplicate")

	names := contactFormattedNames(t, token, abUUID)
	assert.Contains(t, names, "Alice Renamed", "replace must overwrite the existing contact's FN")
	assert.NotContains(t, names, "Alice Example", "the pre-replace FN must be gone")
}

// contactFormattedNames returns the formatted_name of every contact in a book.
func contactFormattedNames(t *testing.T, token string, abUUID string) []string {
	t.Helper()
	var resp struct {
		Contacts []struct {
			FormattedName string `json:"formatted_name"`
		} `json:"Contacts"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet,
		"/addressbooks/"+abUUID+"/contacts", token, nil, &resp))
	names := make([]string, 0, len(resp.Contacts))
	for _, c := range resp.Contacts {
		names = append(names, c.FormattedName)
	}
	return names
}

type contactImportResult struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

func importContacts(t *testing.T, token string, abUUID string, duplicateHandling string, vcf []byte) contactImportResult {
	t.Helper()
	status, raw := rawCall(t, http.MethodPost,
		baseURL+"/api/v1/addressbooks/"+abUUID+"/import?duplicate_handling="+duplicateHandling,
		token, vcf, map[string]string{"Content-Type": "text/vcard"})
	require.Equalf(t, http.StatusOK, status, "import contacts: %s", string(raw))
	var res contactImportResult
	require.NoError(t, json.Unmarshal(raw, &res))
	require.Equalf(t, 0, res.Failed, "import had failures: %s", string(raw))
	return res
}

func contactCount(t *testing.T, token string, abUUID string) int {
	t.Helper()
	var resp struct {
		Total int `json:"Total"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/"+abUUID+"/contacts", token, nil, &resp)
	require.Equal(t, http.StatusOK, code)
	return resp.Total
}
