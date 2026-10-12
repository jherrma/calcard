package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openCaseSensitiveLikeDB opens SQLite with PRAGMA case_sensitive_like=ON, which
// gives LIKE PostgreSQL's semantics: no case folding at all. SQLite's default
// LIKE folds ASCII case, which is exactly how a plain-LIKE search passed every
// SQLite test while missing "McpSmith" for "mcpsmith" on PostgreSQL (F26).
func openCaseSensitiveLikeDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:?_case_sensitive_like=1"), &gorm.Config{})
	require.NoError(t, err)
	// :memory: is per connection; keep the pool at one so the PRAGMA and the
	// schema are where the queries run.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(models...))

	var plain int
	require.NoError(t, db.Raw("SELECT 'McpSmith' LIKE '%mcpsmith%'").Scan(&plain).Error)
	require.Equal(t, 0, plain, "precondition: LIKE must be case-sensitive on this connection")
	return db
}

func TestSearchObjectsInBooksIgnoresCase(t *testing.T) {
	db := openCaseSensitiveLikeDB(t,
		&addressbook.AddressBook{}, &addressbook.AddressObject{},
		&addressbook.ContactPhoto{}, &addressbook.SyncChangeLog{})
	repo := repository.NewAddressBookRepository(db)
	ctx := context.Background()

	ab := &addressbook.AddressBook{Name: "B", UserID: 1, UUID: uuid.New().String(), Path: "/b", SyncToken: "data:1", CTag: "1"}
	require.NoError(t, repo.Create(ctx, ab))
	obj := &addressbook.AddressObject{
		UUID: uuid.New().String(), AddressBookID: ab.ID, UID: uuid.New().String(),
		VCardData:     "BEGIN:VCARD\nFN:Mia McpSmith\nEND:VCARD",
		FormattedName: "Mia McpSmith", GivenName: "Mia", FamilyName: "McpSmith",
		Email: "Mia.McpSmith@Example.COM", Organization: "ACME Corp",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, repo.CreateObject(ctx, obj))

	for _, q := range []string{"McpSmith", "mcpsmith", "MCPSMITH", "mia.mcpsmith@example.com", "acme corp"} {
		results, err := repo.SearchObjectsInBooks(ctx, []uint{ab.ID}, q, 10, 0)
		require.NoError(t, err)
		if assert.Len(t, results, 1, "query %q", q) {
			assert.Equal(t, obj.ID, results[0].ID)
		}
	}

	// Wildcards in the query still match literally.
	for _, q := range []string{"Mc_Smith", "%"} {
		results, err := repo.SearchObjectsInBooks(ctx, []uint{ab.ID}, q, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, results, "query %q", q)
	}
}

func TestSearchEventsIgnoresCase(t *testing.T) {
	db := openCaseSensitiveLikeDB(t,
		&calendar.Calendar{}, &calendar.CalendarObject{}, &calendar.SyncChangeLog{}, &calendar.CalendarSubscription{})
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()

	cal := seedCalendar(t, repo, false)
	obj := feedObject("uid-f26", "Quarterly Review")
	obj.CalendarID = cal.ID
	obj.Location = "Room Berlin"
	require.NoError(t, repo.CreateCalendarObject(ctx, obj))

	for _, q := range []string{"Quarterly", "quarterly review", "QUARTERLY", "room berlin"} {
		results, err := repo.SearchEvents(ctx, calendar.EventSearchQuery{
			CalendarIDs: []uint{cal.ID}, Text: q, Pivot: time.Now(), Limit: 10,
		})
		require.NoError(t, err)
		if assert.Len(t, results, 1, "query %q", q) {
			assert.Equal(t, obj.ID, results[0].ID)
		}
	}
}
