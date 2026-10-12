//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalDAVPutUnsupportedComponentType (F27 a): calendar collections store
// VEVENT and VTODO only (their supported-calendar-component-set), so a VJOURNAL
// PUT is refused with 403 + CALDAV:supported-calendar-component (RFC 4791
// §5.3.2.1) instead of the plain-text 400 it used to get.
func TestCalDAVPutUnsupportedComponentType(t *testing.T) {
	email := "caldav-vjournal@example.test"
	password := "caldavSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CalDAV VJOURNAL")
	_, appPassword := createAppPassword(t, token, "caldav-vjournal")

	personal, ok := listCalendarsIndex(t, token)["Personal"]
	require.True(t, ok, "default Personal calendar should exist")
	object := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/journal.ics"

	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VJOURNAL\r\nUID:journal-1\r\n" +
		"DTSTAMP:20300101T000000Z\r\nSUMMARY:notes\r\nEND:VJOURNAL\r\nEND:VCALENDAR\r\n"
	status, _, resp := davCall(t, "PUT", object, email, appPassword, body,
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	assert.Equal(t, http.StatusForbidden, status, string(resp))
	assert.Contains(t, string(resp), "supported-calendar-component", "names the failed precondition")

	status, _, _ = davCall(t, "GET", object, email, appPassword, "", nil)
	assert.Equal(t, http.StatusNotFound, status, "nothing was stored")
}

// TestContactImportRejectsInvalidCards (F27 b, c): the .vcf import used to
// store a card without VERSION (whose CardDAV GET then answered 500) and
// vCard 2.1 cards. They now count as failed with a reason; a card without FN
// is imported with an FN derived from N.
func TestContactImportRejectsInvalidCards(t *testing.T) {
	email := "contact-import-invalid@example.test"
	password := "importSecret!123"
	token, username := registerAndLoginFull(t, email, password, "Import Invalid")
	_, appPassword := createAppPassword(t, token, "import-invalid")
	abUUID, abPath := createAddressBookWithPath(t, token, "Import Validation")

	vcf := "BEGIN:VCARD\r\nUID:inv-no-version\r\nFN:No Version\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\r\nVERSION:2.1\r\nUID:inv-old\r\nFN:Old Card\r\nN:Card;Old;;;\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:inv-no-fn\r\nN:Lovelace;Ada;;;\r\nEND:VCARD\r\n"
	status, raw := rawCall(t, http.MethodPost, baseURL+"/api/v1/addressbooks/"+abUUID+"/import",
		token, []byte(vcf), map[string]string{"Content-Type": "text/vcard"})
	require.Equal(t, http.StatusOK, status, string(raw))
	var res struct {
		Total, Imported, Failed int
		Errors                  []struct {
			UID   string `json:"uid"`
			Error string `json:"error"`
		}
	}
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, 3, res.Total, string(raw))
	assert.Equal(t, 1, res.Imported, string(raw))
	assert.Equal(t, 2, res.Failed, string(raw))
	reasons := map[string]string{}
	for _, e := range res.Errors {
		reasons[e.UID] = e.Error
	}
	assert.Contains(t, reasons["inv-no-version"], "VERSION", string(raw))
	assert.Contains(t, reasons["inv-old"], "2.1", string(raw))

	assert.Equal(t, []string{"Ada Lovelace"}, contactFormattedNames(t, token, abUUID),
		"only the card without FN is stored, with FN derived from N")

	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"
	status, _, body := davCall(t, "GET", collection+"inv-no-fn.vcf", email, appPassword, "", nil)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), "FN:Ada Lovelace")
	status, _, _ = davCall(t, "GET", collection+"inv-no-version.vcf", email, appPassword, "", nil)
	assert.Equal(t, http.StatusNotFound, status, "the VERSION-less card must not have been stored")
}

// TestContactCreateDerivesFormattedName (F27 c): a REST contact without
// formatted_name used to be stored with an empty "FN:"; FN now comes from the
// name parts, and an explicit formatted_name still wins.
func TestContactCreateDerivesFormattedName(t *testing.T) {
	token, _ := registerAndLoginFull(t, "contact-derive-fn@example.test", "contactSecret!123", "Derive FN")
	abUUID, _ := createAddressBookWithPath(t, token, "Derive FN")

	var created struct {
		FormattedName string `json:"formatted_name"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/contacts", token,
		map[string]string{"given_name": "Grace", "family_name": "Hopper"}, &created)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "Grace Hopper", created.FormattedName)

	code = doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/contacts", token,
		map[string]string{"formatted_name": "Amazing Grace", "given_name": "Grace"}, &created)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "Amazing Grace", created.FormattedName)

	status, raw := rawCall(t, http.MethodGet, baseURL+"/api/v1/addressbooks/"+abUUID+"/export", token, nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), "FN:Grace Hopper")
	assert.False(t, strings.Contains(string(raw), "FN:\r\n"), "no contact may be exported with an empty FN")
}

// createAddressBookWithPath creates an address book and returns its UUID and
// DAV path segment.
func createAddressBookWithPath(t *testing.T, token, name string) (uuid, path string) {
	t.Helper()
	var ab struct {
		UUID string `json:"UUID"`
		Path string `json:"Path"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/", token, map[string]string{"name": name}, &ab)
	require.Equal(t, http.StatusCreated, code, "create addressbook %s", name)
	require.NotEmpty(t, ab.UUID)
	require.NotEmpty(t, ab.Path)
	return ab.UUID, ab.Path
}
