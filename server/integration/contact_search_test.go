//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContactSearchCoversSharedBooks is the end-to-end regression test for #162.
// GET /contacts/search used to search only the caller's OWN address books, so a
// contact in a book shared with them disappeared the moment they typed its name
// into the contacts page — while the very same contact stayed listed, readable
// and (with a read-write share) editable everywhere else.
//
// It runs against the real wired route on purpose: the handler tests build their
// own Fiber app, so a mistake in routes.go — where the readable-book corpus is
// now injected — would not show up there.
func TestContactSearchCoversSharedBooks(t *testing.T) {
	const password = "contactSearch!123"
	ownerToken := registerAndLogin(t, "csearch-owner@example.test", password, "CSearch Owner")
	shareeEmail := "csearch-sharee@example.test"
	shareeToken := registerAndLogin(t, shareeEmail, password, "CSearch Sharee")

	// The owner shares one book with the sharee and keeps another to themselves;
	// the sharee also owns one. One "Wanda" per book, so a single query
	// distinguishes all three corpora.
	_, sharedUUID := createAddressBook(t, ownerToken, "Shared Search Book")
	_, privateUUID := createAddressBook(t, ownerToken, "Private Search Book")
	_, ownUUID := createAddressBook(t, shareeToken, "Sharee Own Book")

	shareAddressBook(t, ownerToken, sharedUUID, shareeEmail, "read")

	createContactIn(t, ownerToken, sharedUUID, "Wanda Shared")
	createContactIn(t, ownerToken, privateUUID, "Wanda Private")
	createContactIn(t, shareeToken, ownUUID, "Wanda Own")

	// search returns the status and the matched formatted names. The response
	// shape is unchanged by #162: raw { contacts, query, count }, no envelope.
	search := func(t *testing.T, token, rawQuery string) (int, []string) {
		t.Helper()
		status, raw := restCall(t, http.MethodGet, "/contacts/search?"+rawQuery, token, nil)
		if status != http.StatusOK {
			return status, nil
		}
		var body struct {
			Contacts []struct {
				FormattedName string `json:"formatted_name"`
			} `json:"contacts"`
			Count int `json:"count"`
		}
		require.NoErrorf(t, json.Unmarshal(raw, &body), "decode search response: %s", raw)
		require.Equal(t, len(body.Contacts), body.Count, "count must match the items returned")
		names := make([]string, 0, len(body.Contacts))
		for _, c := range body.Contacts {
			names = append(names, c.FormattedName)
		}
		return status, names
	}

	t.Run("searches own and shared books, never an unshared one", func(t *testing.T) {
		status, names := search(t, shareeToken, "q=Wanda")
		require.Equal(t, http.StatusOK, status)
		assert.ElementsMatch(t, []string{"Wanda Own", "Wanda Shared"}, names)
	})

	t.Run("addressbook_id may name a shared book", func(t *testing.T) {
		status, names := search(t, shareeToken, "q=Wanda&addressbook_id="+sharedUUID)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, []string{"Wanda Shared"}, names)
	})

	t.Run("addressbook_id naming an unreadable book is a 404", func(t *testing.T) {
		// Not an empty 200: that would assert the book holds no match. The same
		// 404 answers an unknown UUID, so existence isn't leaked.
		status, _ := search(t, shareeToken, "q=Wanda&addressbook_id="+privateUUID)
		assert.Equal(t, http.StatusNotFound, status)

		status, _ = search(t, shareeToken, "q=Wanda&addressbook_id=00000000-0000-0000-0000-000000000000")
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("the owner still sees only their own books", func(t *testing.T) {
		// Sharing is one-directional: nothing about #162 gives the owner sight of
		// the sharee's book.
		status, names := search(t, ownerToken, "q=Wanda")
		require.Equal(t, http.StatusOK, status)
		assert.ElementsMatch(t, []string{"Wanda Shared", "Wanda Private"}, names)
	})
}
