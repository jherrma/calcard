package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestListObjectsPagingTiebreaker verifies that offset paging is stable even
// when every contact ties on the sort key. Without a unique tiebreaker on the
// ORDER BY, tied rows can be returned in different orders per query, so a
// contact may appear on two consecutive pages (duplicate) or on neither
// (dropped). Regression test for issue #119.
func TestListObjectsPagingTiebreaker(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	err = db.AutoMigrate(&addressbook.AddressBook{}, &addressbook.AddressObject{}, &addressbook.ContactPhoto{}, &addressbook.SyncChangeLog{})
	assert.NoError(t, err)

	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	// Seed 10 contacts that all tie on every supported sort field
	// (given_name, family_name, email, updated_at). Only the unique id
	// distinguishes them, so paging can only be stable via the tiebreaker.
	abID := uint(1)
	const seedCount = 10
	sharedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	seeded := make(map[string]struct{}, seedCount)
	for i := 0; i < seedCount; i++ {
		obj := &addressbook.AddressObject{
			UUID:          uuid.New().String(),
			AddressBookID: abID,
			UID:           uuid.New().String(),
			VCardData:     "BEGIN:VCARD\nFN:Same Person\nN:Person;Same;;;\nEMAIL:same@example.com\nEND:VCARD",
			FormattedName: "Same Person",
			GivenName:     "Same",
			FamilyName:    "Person",
			Email:         "same@example.com",
			CreatedAt:     sharedTime,
			UpdatedAt:     sharedTime,
		}
		err = repo.CreateObject(ctx, obj)
		assert.NoError(t, err)
		seeded[obj.UUID] = struct{}{}
	}

	// pageAll walks every page with the given limit and returns the objects in
	// the exact order they were paged, concatenated across page boundaries.
	pageAll := func(t *testing.T, sortField, order string, limit int) []addressbook.AddressObject {
		t.Helper()
		var seen []addressbook.AddressObject
		for offset := 0; ; offset += limit {
			objs, total, err := repo.ListObjects(ctx, abID, limit, offset, sortField, order)
			assert.NoError(t, err)
			assert.Equal(t, int64(seedCount), total)
			seen = append(seen, objs...)
			if offset+limit >= int(total) {
				break
			}
		}
		return seen
	}

	for _, sortField := range []string{"name", "email", "updated_at"} {
		for _, order := range []string{"asc", "desc"} {
			t.Run(fmt.Sprintf("%s_%s", sortField, order), func(t *testing.T) {
				seen := pageAll(t, sortField, order, 3)

				// Exactly the seeded set, no duplicates, none missing.
				assert.Len(t, seen, seedCount, "page union must have exactly the seeded count (no dup/drop)")

				unionUnique := make(map[string]struct{}, len(seen))
				for _, o := range seen {
					_, dup := unionUnique[o.UUID]
					assert.False(t, dup, "contact %s appeared on more than one page", o.UUID)
					unionUnique[o.UUID] = struct{}{}
					_, ok := seeded[o.UUID]
					assert.True(t, ok, "paged contact %s was not in the seeded set", o.UUID)
				}
				assert.Equal(t, len(seeded), len(unionUnique), "every seeded contact must appear exactly once across pages")

				// Because every row ties on the sort key, the unique id tiebreaker
				// is the only thing that can determine a total order. Assert the
				// paged sequence is strictly monotonic in id, following the sort
				// direction. Without the tiebreaker the order is unspecified (and
				// on SQLite it stays ascending rowid order even for DESC, so this
				// assertion fails), which is exactly the instability issue #119
				// fixes.
				for i := 1; i < len(seen); i++ {
					prev, cur := seen[i-1].ID, seen[i].ID
					if order == "desc" {
						assert.Greater(t, prev, cur, "DESC paging must be strictly descending by id (tiebreaker)")
					} else {
						assert.Less(t, prev, cur, "ASC paging must be strictly ascending by id (tiebreaker)")
					}
				}
			})
		}
	}
}
