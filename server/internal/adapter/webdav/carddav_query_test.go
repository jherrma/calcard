package webdav

import (
	"context"
	"testing"

	"github.com/emersion/go-webdav/carddav"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCanFilterInDB pins the routing decision: only a single filter on a
// DB-mapped property with at most one text-match and no params may take the
// SQL fast path; everything else must fall back to the spec-compliant matcher.
func TestCanFilterInDB(t *testing.T) {
	tm := []carddav.TextMatch{{Text: "x", MatchType: carddav.MatchContains}}

	assert.True(t, canFilterInDB(&carddav.AddressBookQuery{}), "no filters is a plain list")
	assert.True(t, canFilterInDB(&carddav.AddressBookQuery{
		PropFilters: []carddav.PropFilter{{Name: "FN", TextMatches: tm}},
	}), "single mapped filter is DB-safe")

	assert.False(t, canFilterInDB(&carddav.AddressBookQuery{
		PropFilters: []carddav.PropFilter{{Name: "FN", TextMatches: tm}, {Name: "EMAIL", TextMatches: tm}},
	}), "multiple filters must fall back (default combinator is anyof)")
	assert.False(t, canFilterInDB(&carddav.AddressBookQuery{
		PropFilters: []carddav.PropFilter{{Name: "NICKNAME", TextMatches: tm}},
	}), "unmapped property must fall back (else silently dropped)")
	assert.False(t, canFilterInDB(&carddav.AddressBookQuery{
		PropFilters: []carddav.PropFilter{{Name: "FN", TextMatches: append(tm, carddav.TextMatch{Text: "y"})}},
	}), "multiple text-matches must fall back")
}

// TestQueryAddressObjectsAnyOfAndUnmapped is the regression test for #49: a
// multi-filter query must be evaluated as anyof (OR), and a filter on an
// unmapped property must return zero rather than everyone.
func TestQueryAddressObjectsAnyOfAndUnmapped(t *testing.T) {
	_, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	backend := NewCardDAVBackend(abRepo, userRepo, nil)

	owner := &user.User{UUID: "o", Email: "o@example.com", Username: "owner", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	ab := &addressbook.AddressBook{UUID: "ab", UserID: owner.ID, Path: "contacts", Name: "Contacts"}
	require.NoError(t, abRepo.Create(ctx, ab))

	// A matches on FN, B matches on EMAIL — neither matches both. Casing is
	// aligned with the search text ("alice") so this exercises the anyof/allof
	// combinator, not go-webdav's (case-sensitive) text collation.
	require.NoError(t, abRepo.CreateObject(ctx, &addressbook.AddressObject{
		UUID: "a", AddressBookID: ab.ID, UID: "a", Path: "a.vcf",
		VCardData: "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:a\r\nFN:alice\r\nEND:VCARD\r\n",
	}))
	require.NoError(t, abRepo.CreateObject(ctx, &addressbook.AddressObject{
		UUID: "b", AddressBookID: ab.ID, UID: "b", Path: "b.vcf",
		VCardData: "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:b\r\nFN:Bob\r\nEMAIL:alice@x.y\r\nEND:VCARD\r\n",
	}))

	authCtx := WithUser(ctx, owner)
	tm := []carddav.TextMatch{{Text: "alice", MatchType: carddav.MatchContains}}

	// anyof (the parser's default for a filter with no test attribute).
	anyof := &carddav.AddressBookQuery{
		FilterTest: carddav.FilterAnyOf,
		PropFilters: []carddav.PropFilter{
			{Name: "FN", TextMatches: tm},
			{Name: "EMAIL", TextMatches: tm},
		},
	}
	got, err := backend.QueryAddressObjects(authCtx, "/dav/owner/addressbooks/contacts/", anyof)
	require.NoError(t, err)
	assert.Len(t, got, 2, "FN-contains-alice OR EMAIL-contains-alice must match both contacts")

	// Unmapped property (NICKNAME) — nobody has one, so zero results.
	unmapped := &carddav.AddressBookQuery{
		PropFilters: []carddav.PropFilter{
			{Name: "NICKNAME", TextMatches: []carddav.TextMatch{{Text: "zzz", MatchType: carddav.MatchContains}}},
		},
	}
	got, err = backend.QueryAddressObjects(authCtx, "/dav/owner/addressbooks/contacts/", unmapped)
	require.NoError(t, err)
	assert.Empty(t, got, "a filter on an unmapped property must match no one, not everyone")
}
