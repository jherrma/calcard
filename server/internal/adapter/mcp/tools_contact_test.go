package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListAddressBooksReturnsOwnedAndShared(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")

	own := e.newAddressBook(alice.ID, "Personal")
	hidden := e.newAddressBook(bob.ID, "Bob private")
	shared := e.newAddressBook(bob.ID, "Bob shared")
	e.shareAddressBook(shared, alice.ID, "read")

	payload, isErr := e.call(alice.ID, "list_address_books", nil)
	require.False(t, isErr)

	byID := map[string]map[string]interface{}{}
	for _, raw := range payload["address_books"].([]interface{}) {
		b := raw.(map[string]interface{})
		byID[b["id"].(string)] = b
	}

	require.Contains(t, byID, own.UUID)
	assert.Equal(t, "owner", byID[own.UUID]["permission"])
	require.Contains(t, byID, shared.UUID)
	assert.Equal(t, "read", byID[shared.UUID]["permission"])
	assert.NotContains(t, byID, hidden.UUID, "another user's unshared book must not be listed")
}

func TestCreateContactThenList(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	ab := e.newAddressBook(alice.ID, "Personal")

	created, isErr := e.call(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": ab.UUID,
		"first_name":      "Jane",
		"last_name":       "Smith",
		"email":           "jane@example.com",
		"phone":           "+1-555-0123",
		"organization":    "Acme",
	})
	require.False(t, isErr, "create_contact failed: %v", created["error"])

	contact := created["contact"].(map[string]interface{})
	assert.Equal(t, "Jane Smith", contact["name"])
	assert.Equal(t, ab.UUID, contact["address_book_id"],
		"a contact must report its book's UUID, not the numeric id the REST DTO carries")
	require.NotEmpty(t, contact["id"])

	listed, isErr := e.call(alice.ID, "get_contacts", map[string]interface{}{"address_book_id": ab.UUID})
	require.False(t, isErr)
	contacts := listed["contacts"].([]interface{})
	require.Len(t, contacts, 1)
	assert.Equal(t, []interface{}{"jane@example.com"}, contacts[0].(map[string]interface{})["emails"])
}

func TestCreateContactRequiresAName(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	ab := e.newAddressBook(alice.ID, "Personal")

	result := e.callRaw(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": ab.UUID,
		"notes":           "no name at all",
	})
	require.True(t, result.IsError, "a nameless contact is a row nobody can find again")
	assert.Contains(t, result.Content[0].Text, "first_name")
}

func TestCreateContactRejectedOnReadOnlyShare(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	ab := e.newAddressBook(bob.ID, "Bob")
	e.shareAddressBook(ab, alice.ID, "read")

	result := e.callRaw(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": ab.UUID,
		"first_name":      "Sneaky",
	})
	require.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "read-only")
}

func TestUpdateContactRefreshesDisplayName(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	ab := e.newAddressBook(alice.ID, "Personal")

	created, _ := e.call(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": ab.UUID,
		"first_name":      "Jane",
		"last_name":       "Smith",
	})
	contactID := created["contact"].(map[string]interface{})["id"].(string)

	updated, isErr := e.call(alice.ID, "update_contact", map[string]interface{}{
		"contact_id": contactID,
		"last_name":  "Doe",
	})
	require.False(t, isErr, "update_contact failed: %v", updated["error"])

	contact := updated["contact"].(map[string]interface{})
	assert.Equal(t, "Doe", contact["last_name"])
	// The regression this guards: leaving the formatted name stale would make
	// the contact still read as "Jane Smith" in every DAV client.
	assert.Equal(t, "Jane Doe", contact["name"])
}

func TestUpdateContactEmailReplacesTheList(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	ab := e.newAddressBook(alice.ID, "Personal")

	created, _ := e.call(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": ab.UUID,
		"first_name":      "Jane",
		"email":           "old@example.com",
	})
	contactID := created["contact"].(map[string]interface{})["id"].(string)

	updated, isErr := e.call(alice.ID, "update_contact", map[string]interface{}{
		"contact_id": contactID,
		"email":      "new@example.com",
	})
	require.False(t, isErr)
	assert.Equal(t, []interface{}{"new@example.com"},
		updated["contact"].(map[string]interface{})["emails"],
		"the documented behaviour is replacement, not append")
}

func TestDeleteContact(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	ab := e.newAddressBook(alice.ID, "Personal")

	created, _ := e.call(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": ab.UUID,
		"first_name":      "Temp",
	})
	contactID := created["contact"].(map[string]interface{})["id"].(string)

	deleted, isErr := e.call(alice.ID, "delete_contact", map[string]interface{}{"contact_id": contactID})
	require.False(t, isErr, "delete_contact failed: %v", deleted["error"])

	listed, _ := e.call(alice.ID, "get_contacts", map[string]interface{}{"address_book_id": ab.UUID})
	assert.Empty(t, listed["contacts"])
}

func TestContactToolsRefuseForeignContact(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	ab := e.newAddressBook(bob.ID, "Bob private")

	created, _ := e.call(bob.ID, "create_contact", map[string]interface{}{
		"address_book_id": ab.UUID,
		"first_name":      "Secret",
	})
	contactID := created["contact"].(map[string]interface{})["id"].(string)

	for _, tool := range []string{"update_contact", "delete_contact"} {
		result := e.callRaw(alice.ID, tool, map[string]interface{}{
			"contact_id": contactID,
			"first_name": "Hijacked",
		})
		assert.True(t, result.IsError, "%s must refuse a contact in an unshared book", tool)
	}

	// And it still says "Secret".
	listed, _ := e.call(bob.ID, "get_contacts", map[string]interface{}{"address_book_id": ab.UUID})
	contacts := listed["contacts"].([]interface{})
	require.Len(t, contacts, 1)
	assert.Equal(t, "Secret", contacts[0].(map[string]interface{})["name"])
}

func TestSearchContactsSpansSharedBooks(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	own := e.newAddressBook(alice.ID, "Personal")
	shared := e.newAddressBook(bob.ID, "Bob shared")
	e.shareAddressBook(shared, alice.ID, "read")
	hidden := e.newAddressBook(bob.ID, "Bob private")

	mustCreateContact(t, e, alice.ID, own.UUID, "Anna", "Schmidt")
	mustCreateContact(t, e, bob.ID, shared.UUID, "Bernd", "Schmidt")
	mustCreateContact(t, e, bob.ID, hidden.UUID, "Carla", "Schmidt")

	payload, isErr := e.call(alice.ID, "search_contacts", map[string]interface{}{"query": "schmidt"})
	require.False(t, isErr, "search_contacts failed: %v", payload["error"])

	names := map[string]bool{}
	for _, raw := range payload["matches"].([]interface{}) {
		m := raw.(map[string]interface{})
		names[m["contact"].(map[string]interface{})["name"].(string)] = true
	}

	assert.True(t, names["Anna Schmidt"])
	// The #162 property, restated for MCP: a shared book is searchable.
	assert.True(t, names["Bernd Schmidt"], "a shared address book must be searched")
	assert.False(t, names["Carla Schmidt"], "an unshared book must not be searched")
}

func mustCreateContact(t *testing.T, e *testEnv, userID uint, bookUUID, first, last string) string {
	t.Helper()
	created, isErr := e.call(userID, "create_contact", map[string]interface{}{
		"address_book_id": bookUUID,
		"first_name":      first,
		"last_name":       last,
	})
	require.False(t, isErr, "create_contact(%s %s) failed: %v", first, last, created["error"])
	return created["contact"].(map[string]interface{})["id"].(string)
}
