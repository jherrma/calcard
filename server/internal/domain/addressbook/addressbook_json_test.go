package addressbook

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// SCOPE: F01. A shared address book is loaded with its owner preloaded
// (AddressBook.User). That row must never reach an API response: the sharee gets
// the owner through a dedicated minimal block, not the whole user.
func TestAddressBookDoesNotSerialiseItsOwnerRow(t *testing.T) {
	ab := AddressBook{
		Name: "Team",
		User: user.User{Email: "owner@example.com", Username: "ownerlogin", PasswordHash: "$2a$12$hash"},
	}

	b, err := json.Marshal(ab)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.ToLower(string(b))
	for _, leaked := range []string{"owner@example.com", "ownerlogin", "$2a$", "passwordhash", `"user"`} {
		if strings.Contains(out, leaked) {
			t.Errorf("AddressBook JSON contains %q: %s", leaked, b)
		}
	}
}

// SCOPE: P03. The owner's numeric user id and the soft-delete column are
// internal bookkeeping: a sharee must not learn the owner's primary key, and
// DeletedAt is always null in a response anyway. The book's own ID and UUID
// stay — they are the API handles the web UI uses.
func TestAddressBookDoesNotSerialiseInternalIDs(t *testing.T) {
	b, err := json.Marshal(AddressBook{ID: 7, UUID: "ab-uuid", UserID: 4242, Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"UserID", "DeletedAt"} {
		if _, ok := fields[hidden]; ok {
			t.Errorf("AddressBook JSON exposes %q: %s", hidden, b)
		}
	}
	if strings.Contains(string(b), "4242") {
		t.Errorf("AddressBook JSON contains the owner's numeric id: %s", b)
	}
	for _, kept := range []string{"ID", "UUID", "Name"} {
		if _, ok := fields[kept]; !ok {
			t.Errorf("AddressBook JSON lost %q, which the web UI relies on: %s", kept, b)
		}
	}
}
