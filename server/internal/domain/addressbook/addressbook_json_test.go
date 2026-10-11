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
