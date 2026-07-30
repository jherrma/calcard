//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharedAddressBookContactsREST is the regression test for #53: the REST
// contact API used to gate every route on OWNERSHIP alone, so a user granted
// read-write access to an address book could manage its contacts over CardDAV
// (a phone, Thunderbird) but not in the web UI. Calendars/events already
// honored shares; address books didn't.
//
// The matrix below is the contract, from the sharee's point of view:
//
//	read-write share  → read AND write succeed
//	read-only share   → reads succeed, writes are 403 ("you have read-only…")
//	no share at all   → everything is 404, never 403 (don't leak existence)
//
// The 404-vs-403 split matters: 403 admits the book exists, so it's only ever
// returned for a book the caller can genuinely see.
func TestSharedAddressBookContactsREST(t *testing.T) {
	const password = "abShareRest!123"
	ownerToken := registerAndLogin(t, "abshare-rest-owner@example.test", password, "Share Owner")
	shareeEmail := "abshare-rest-sharee@example.test"
	shareeToken := registerAndLogin(t, shareeEmail, password, "Share Sharee")

	// Owner's three books: one shared read-write, one read-only, one private.
	_, rwUUID := createAddressBook(t, ownerToken, "RW Shared Book")
	_, roUUID := createAddressBook(t, ownerToken, "RO Shared Book")
	_, privUUID := createAddressBook(t, ownerToken, "Private Book")

	shareAddressBook(t, ownerToken, rwUUID, shareeEmail, "read-write")
	shareAddressBook(t, ownerToken, roUUID, shareeEmail, "read")

	// Seed one owner-authored contact in each shared book so the sharee has
	// something pre-existing to read and (where permitted) mutate.
	rwSeed := createContactIn(t, ownerToken, rwUUID, "Rita Writable")
	roSeed := createContactIn(t, ownerToken, roUUID, "Ronald Readonly")
	privSeed := createContactIn(t, ownerToken, privUUID, "Percy Private")

	// --- read-write share: full contact management -----------------------

	t.Run("read-write sharee can read", func(t *testing.T) {
		status, raw := restCall(t, http.MethodGet, "/addressbooks/"+rwUUID+"/contacts", shareeToken, nil)
		require.Equalf(t, http.StatusOK, status, "list contacts in read-write book: %s", errorMessage(raw))
		assert.Contains(t, string(raw), "Rita Writable")

		status, raw = restCall(t, http.MethodGet, "/addressbooks/"+rwUUID+"/contacts/"+rwSeed, shareeToken, nil)
		assert.Equalf(t, http.StatusOK, status, "get contact in read-write book: %s", errorMessage(raw))
	})

	t.Run("read-write sharee can create, update and delete", func(t *testing.T) {
		var created struct {
			ID string `json:"id"`
		}
		code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+rwUUID+"/contacts", shareeToken,
			map[string]any{
				"formatted_name": "Sam Sharee",
				"given_name":     "Sam",
				"family_name":    "Sharee",
			}, &created)
		require.Equal(t, http.StatusCreated, code, "sharee with read-write must be able to create a contact")
		require.NotEmpty(t, created.ID)

		status, raw := restCall(t, http.MethodPatch, "/addressbooks/"+rwUUID+"/contacts/"+created.ID, shareeToken,
			map[string]any{"formatted_name": "Sam Renamed", "given_name": "Sam", "family_name": "Renamed"})
		require.Equalf(t, http.StatusOK, status, "sharee update: %s", errorMessage(raw))

		// The owner must observe the sharee's write — proves it actually landed
		// in the owner's book rather than in some copy.
		status, raw = restCall(t, http.MethodGet, "/addressbooks/"+rwUUID+"/contacts", ownerToken, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Contains(t, string(raw), "Sam Renamed", "owner must see the contact the sharee wrote")

		status, raw = restCall(t, http.MethodDelete, "/addressbooks/"+rwUUID+"/contacts/"+created.ID, shareeToken, nil)
		require.Equalf(t, http.StatusNoContent, status, "sharee delete: %s", errorMessage(raw))
	})

	t.Run("read-write sharee can manage photos", func(t *testing.T) {
		icon := readAsset(t, "user-icon.jpg")
		photoPath := baseURL + "/api/v1/addressbooks/" + rwUUID + "/contacts/" + rwSeed + "/photo"

		status, raw := rawCall(t, http.MethodPut, photoPath, shareeToken, icon,
			map[string]string{"Content-Type": "image/jpeg"})
		require.Equalf(t, http.StatusNoContent, status, "sharee photo upload: %s", string(raw))

		status, _ = restCall(t, http.MethodGet, "/addressbooks/"+rwUUID+"/contacts/"+rwSeed+"/photo", shareeToken, nil)
		assert.Equal(t, http.StatusOK, status, "sharee must be able to read the photo back")

		status, raw = restCall(t, http.MethodDelete, "/addressbooks/"+rwUUID+"/contacts/"+rwSeed+"/photo", shareeToken, nil)
		assert.Equalf(t, http.StatusNoContent, status, "sharee photo delete: %s", errorMessage(raw))
	})

	t.Run("read-write sharee can import", func(t *testing.T) {
		vcf := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:share-import-1\r\nFN:Imogen Import\r\nN:Import;Imogen;;;\r\nEND:VCARD\r\n"
		status, raw := rawCall(t, http.MethodPost,
			baseURL+"/api/v1/addressbooks/"+rwUUID+"/import?duplicate_handling=skip",
			shareeToken, []byte(vcf), map[string]string{"Content-Type": "text/vcard"})
		require.Equalf(t, http.StatusOK, status, "sharee import into read-write book: %s", string(raw))
		var res contactImportResult
		require.NoError(t, json.Unmarshal(raw, &res))
		assert.Equal(t, 1, res.Imported, "import into a read-write shared book must add the contact")
	})

	// --- read-only share: reads yes, writes 403 --------------------------

	t.Run("read-only sharee can read", func(t *testing.T) {
		status, raw := restCall(t, http.MethodGet, "/addressbooks/"+roUUID+"/contacts", shareeToken, nil)
		require.Equalf(t, http.StatusOK, status, "list contacts in read-only book: %s", errorMessage(raw))
		assert.Contains(t, string(raw), "Ronald Readonly")

		status, _ = restCall(t, http.MethodGet, "/addressbooks/"+roUUID+"/contacts/"+roSeed, shareeToken, nil)
		assert.Equal(t, http.StatusOK, status, "get contact in read-only book must succeed")
	})

	t.Run("read-only sharee is refused every write with 403", func(t *testing.T) {
		status, _ := restCall(t, http.MethodPost, "/addressbooks/"+roUUID+"/contacts", shareeToken,
			map[string]any{"formatted_name": "Nope Nope", "given_name": "Nope"})
		assert.Equal(t, http.StatusForbidden, status, "create in a read-only book must be 403")

		status, _ = restCall(t, http.MethodPatch, "/addressbooks/"+roUUID+"/contacts/"+roSeed, shareeToken,
			map[string]any{"formatted_name": "Hijacked"})
		assert.Equal(t, http.StatusForbidden, status, "update in a read-only book must be 403")

		status, _ = restCall(t, http.MethodDelete, "/addressbooks/"+roUUID+"/contacts/"+roSeed, shareeToken, nil)
		assert.Equal(t, http.StatusForbidden, status, "delete in a read-only book must be 403")

		icon := readAsset(t, "user-icon.jpg")
		status, _ = rawCall(t, http.MethodPut,
			baseURL+"/api/v1/addressbooks/"+roUUID+"/contacts/"+roSeed+"/photo",
			shareeToken, icon, map[string]string{"Content-Type": "image/jpeg"})
		assert.Equal(t, http.StatusForbidden, status, "photo upload in a read-only book must be 403")

		status, _ = restCall(t, http.MethodDelete, "/addressbooks/"+roUUID+"/contacts/"+roSeed+"/photo", shareeToken, nil)
		assert.Equal(t, http.StatusForbidden, status, "photo delete in a read-only book must be 403")

		vcf := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:ro-import\r\nFN:Nope Import\r\nEND:VCARD\r\n"
		status, _ = rawCall(t, http.MethodPost,
			baseURL+"/api/v1/addressbooks/"+roUUID+"/import?duplicate_handling=skip",
			shareeToken, []byte(vcf), map[string]string{"Content-Type": "text/vcard"})
		assert.Equal(t, http.StatusForbidden, status, "import into a read-only book must be 403")

		// The refusals must have been real, not merely reported.
		status, raw := restCall(t, http.MethodGet, "/addressbooks/"+roUUID+"/contacts", ownerToken, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Contains(t, string(raw), "Ronald Readonly", "the read-only contact must still exist")
		assert.NotContains(t, string(raw), "Hijacked", "the refused update must not have landed")
		assert.NotContains(t, string(raw), "Nope", "no refused write may have landed")
	})

	// --- no share: 404 everywhere ---------------------------------------

	t.Run("non-sharee gets 404, never 403", func(t *testing.T) {
		for _, tc := range []struct {
			name, method, path string
			body               any
		}{
			{"list", http.MethodGet, "/addressbooks/" + privUUID + "/contacts", nil},
			{"get", http.MethodGet, "/addressbooks/" + privUUID + "/contacts/" + privSeed, nil},
			{"create", http.MethodPost, "/addressbooks/" + privUUID + "/contacts", map[string]any{"formatted_name": "X"}},
			{"update", http.MethodPatch, "/addressbooks/" + privUUID + "/contacts/" + privSeed, map[string]any{"formatted_name": "X"}},
			{"delete", http.MethodDelete, "/addressbooks/" + privUUID + "/contacts/" + privSeed, nil},
			{"photo", http.MethodGet, "/addressbooks/" + privUUID + "/contacts/" + privSeed + "/photo", nil},
		} {
			status, _ := restCall(t, tc.method, tc.path, shareeToken, tc.body)
			assert.Equalf(t, http.StatusNotFound, status,
				"%s on an unshared book must be 404 (403 would confirm it exists)", tc.name)
		}
	})

	// --- move: both sides need write ------------------------------------

	t.Run("move requires write on both source and target", func(t *testing.T) {
		_, ownUUID := createAddressBook(t, shareeToken, "Sharee Own Book")

		// The move route is nested under the source book:
		// POST /addressbooks/:addressbook_id/contacts/:contact_id/move.
		movePath := func(bookUUID, contactUUID string) string {
			return "/addressbooks/" + bookUUID + "/contacts/" + contactUUID + "/move"
		}

		// read-write source → own book: allowed.
		movable := createContactIn(t, ownerToken, rwUUID, "Mover One")
		status, raw := restCall(t, http.MethodPost, movePath(rwUUID, movable), shareeToken,
			map[string]string{"target_addressbook_id": ownUUID})
		require.Equalf(t, http.StatusOK, status, "move out of a read-write share into own book: %s", errorMessage(raw))

		// read-write source → read-only target: refused on the target.
		movable2 := createContactIn(t, ownerToken, rwUUID, "Mover Two")
		status, _ = restCall(t, http.MethodPost, movePath(rwUUID, movable2), shareeToken,
			map[string]string{"target_addressbook_id": roUUID})
		assert.Equal(t, http.StatusForbidden, status, "moving INTO a read-only book must be 403")

		// read-only source → own book: refused on the source.
		status, _ = restCall(t, http.MethodPost, movePath(roUUID, roSeed), shareeToken,
			map[string]string{"target_addressbook_id": ownUUID})
		assert.Equal(t, http.StatusForbidden, status, "moving OUT of a read-only book must be 403")

		// unshared source: 404, and unshared target: 404.
		status, _ = restCall(t, http.MethodPost, movePath(privUUID, privSeed), shareeToken,
			map[string]string{"target_addressbook_id": ownUUID})
		assert.Equal(t, http.StatusNotFound, status, "moving a contact from an unshared book must be 404")

		movable3 := createContactIn(t, ownerToken, rwUUID, "Mover Three")
		status, _ = restCall(t, http.MethodPost, movePath(rwUUID, movable3), shareeToken,
			map[string]string{"target_addressbook_id": privUUID})
		assert.Equal(t, http.StatusNotFound, status, "moving into an unshared book must be 404")

		// The permitted move must have actually landed in the sharee's own book.
		status, raw = restCall(t, http.MethodGet, "/addressbooks/"+ownUUID+"/contacts", shareeToken, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Contains(t, string(raw), "Mover One", "the permitted move must have landed in the target book")
	})

	// --- the list endpoint must tell the UI which books are writable -----

	t.Run("address book list reports the effective permission", func(t *testing.T) {
		var list struct {
			AddressBooks []struct {
				UUID       string `json:"UUID"`
				Name       string `json:"Name"`
				Shared     bool   `json:"shared"`
				Permission string `json:"permission"`
			} `json:"addressbooks"`
		}
		code := doJSONRaw(t, http.MethodGet, "/addressbooks/", shareeToken, nil, &list)
		require.Equal(t, http.StatusOK, code)

		perms := map[string]string{}
		for _, ab := range list.AddressBooks {
			perms[ab.UUID] = ab.Permission
		}
		assert.Equal(t, "read-write", perms[rwUUID], "the read-write share must be advertised as writable")
		assert.Equal(t, "read", perms[roUUID], "the read-only share must be advertised as read-only")
		assert.NotContains(t, perms, privUUID, "an unshared book must not appear at all")

		// The sharee's own book is "owner" — the UI keys write controls off this
		// single field, so owned books must not come back with an empty string.
		var ownedFound bool
		for _, ab := range list.AddressBooks {
			if !ab.Shared {
				assert.Equalf(t, "owner", ab.Permission, "owned book %q must report permission=owner", ab.Name)
				ownedFound = true
			}
		}
		assert.True(t, ownedFound, "the sharee should own at least one book by now")
	})
}

// shareAddressBook has the owner share a book with another user at the given
// permission ("read" or "read-write").
func shareAddressBook(t *testing.T, ownerToken, abUUID, shareeEmail, permission string) string {
	t.Helper()
	var resp struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/shares", ownerToken,
		map[string]string{
			"user_identifier": shareeEmail,
			"permission":      permission,
		}, &resp)
	require.Equalf(t, http.StatusCreated, code, "share %s with %s as %s", abUUID, shareeEmail, permission)
	require.NotEmpty(t, resp.ID)
	return resp.ID
}

// createContactIn creates a contact in a book and returns its internal UUID.
func createContactIn(t *testing.T, token, abUUID, formattedName string) string {
	t.Helper()
	var created struct {
		ID string `json:"id"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+abUUID+"/contacts", token,
		map[string]any{"formatted_name": formattedName}, &created)
	require.Equalf(t, http.StatusCreated, code, "create contact %q", formattedName)
	require.NotEmpty(t, created.ID)
	return created.ID
}
