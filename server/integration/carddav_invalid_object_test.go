//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A vCard must carry a VERSION (RFC 6350 §6.7.9). A PUT without one parses,
// but used to fail only when the server re-encoded it, as a 500. It is the
// client's mistake: answer with the valid-address-data precondition (RFC 6352
// §6.3.2.1) instead.
func TestCardDAVPutRejectsInvalidAddressObject(t *testing.T) {
	email := "carddav-invalid@example.test"
	password := "carddavSecret!123"
	token, username := registerAndLoginFull(t, email, password, "CardDAV Invalid")
	_, appPassword := createAppPassword(t, token, "carddav-invalid")

	var wrap struct {
		AddressBooks []struct {
			Name string `json:"Name"`
			Path string `json:"Path"`
		} `json:"addressbooks"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap))
	var abPath string
	for _, ab := range wrap.AddressBooks {
		if ab.Name == "Contacts" {
			abPath = ab.Path
		}
	}
	require.NotEmpty(t, abPath, "default Contacts address book should exist")
	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"

	put := func(name, body string) (int, []byte) {
		status, _, resp := davCall(t, "PUT", collection+name+".vcf", email, appPassword, body,
			map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
		return status, resp
	}

	cases := map[string]string{
		"no VERSION":  "BEGIN:VCARD\r\nUID:no-version\r\nFN:No Version\r\nEND:VCARD\r\n",
		"empty vCard": "BEGIN:VCARD\r\nEND:VCARD\r\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			status, resp := put("invalid", body)
			assert.Equal(t, http.StatusConflict, status, "must be a precondition error: %s", string(resp))
			assert.True(t, strings.Contains(string(resp), "valid-address-data"),
				"body should name the failed precondition: %s", string(resp))
		})
	}

	t.Run("nothing was stored", func(t *testing.T) {
		status, _, _ := davCall(t, "GET", collection+"invalid.vcf", email, appPassword, "", nil)
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a valid vCard is still accepted", func(t *testing.T) {
		status, resp := put("ok", buildMinimalVCard("carddav-invalid-ok", "Valid Card", "Valid", "Card"))
		assert.Contains(t, []int{http.StatusCreated, http.StatusNoContent}, status, string(resp))
	})
}
