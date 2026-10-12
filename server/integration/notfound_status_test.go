//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// zeroUUID is a well-formed UUID no resource ever has.
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// TestAddressBookAndContactNotFoundStatus is the regression test for F05: a
// contact or address book that is missing — or belongs to someone else — must
// be a 404 on every verb, exactly like GET. It used to be a 500 for DELETE and
// the photo routes of an unknown contact and for PATCH of a foreign book, and a
// 400 for DELETE and export of a foreign book.
//
// The foreign-book requests are compared byte-for-byte against the same request
// on an unknown UUID: the status alone is not enough, because a different body
// (or a body-validation 400 that only a foreign book reaches) would still tell
// the caller the book exists.
func TestAddressBookAndContactNotFoundStatus(t *testing.T) {
	const password = "notFound!12345"
	ownerToken := registerAndLogin(t, "f05-owner@example.test", password, "F05 Owner")
	strangerToken := registerAndLogin(t, "f05-stranger@example.test", password, "F05 Stranger")

	_, abUUID := createAddressBook(t, ownerToken, "F05 Book")
	createContactIn(t, ownerToken, abUUID, "F05 Contact")

	pngHeader := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

	t.Run("unknown contact is a 404 on every verb", func(t *testing.T) {
		base := "/addressbooks/" + abUUID + "/contacts/" + zeroUUID
		cases := []struct {
			name, method, path string
			body               any
		}{
			{"GET", http.MethodGet, base, nil},
			{"PATCH", http.MethodPatch, base, map[string]string{"formatted_name": "x"}},
			{"DELETE", http.MethodDelete, base, nil},
			{"photo upload", http.MethodPut, base + "/photo", pngHeader},
			{"photo delete", http.MethodDelete, base + "/photo", nil},
		}
		for _, tc := range cases {
			status, raw := restCall(t, tc.method, tc.path, ownerToken, tc.body)
			assert.Equalf(t, http.StatusNotFound, status, "%s of an unknown contact (body: %s)", tc.name, raw)
		}
	})

	t.Run("foreign address book looks exactly like an unknown one", func(t *testing.T) {
		confirm := map[string]string{"confirmation": "DELETE"}
		cases := []struct {
			name, method, suffix string
			body                 any
		}{
			{"GET", http.MethodGet, "", nil},
			{"PATCH", http.MethodPatch, "", map[string]string{"name": "hijacked"}},
			{"PATCH with a malformed body", http.MethodPatch, "", "{not json"},
			{"DELETE", http.MethodDelete, "", confirm},
			{"DELETE without confirmation", http.MethodDelete, "", map[string]string{}},
			{"export", http.MethodGet, "/export", nil},
		}
		for _, tc := range cases {
			foreignStatus, foreignBody := restCall(t, tc.method, "/addressbooks/"+abUUID+tc.suffix, strangerToken, tc.body)
			unknownStatus, unknownBody := restCall(t, tc.method, "/addressbooks/"+zeroUUID+tc.suffix, strangerToken, tc.body)
			assert.Equalf(t, http.StatusNotFound, foreignStatus, "%s of another user's book (body: %s)", tc.name, foreignBody)
			assert.Equalf(t, http.StatusNotFound, unknownStatus, "%s of an unknown book (body: %s)", tc.name, unknownBody)
			assert.Equalf(t, string(unknownBody), string(foreignBody),
				"%s: a foreign book must answer exactly like an unknown one", tc.name)
		}

		// The owner's book survived every attempt, unrenamed.
		var ab struct {
			Name string `json:"Name"`
		}
		status := doJSONRaw(t, http.MethodGet, "/addressbooks/"+abUUID, ownerToken, nil, &ab)
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "F05 Book", ab.Name)
	})
}
