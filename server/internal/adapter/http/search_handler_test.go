package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	addressbookusecase "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	calendarusecase "github.com/jherrma/caldav-server/internal/usecase/calendar"
	searchusecase "github.com/jherrma/caldav-server/internal/usecase/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// searchFixture is the world every search test runs against: a caller who owns
// one calendar and one address book, plus a second user who shares one of each
// with them read-only and keeps one of each entirely private. Anything the
// caller must not see lives in those private collections.
type searchFixture struct {
	app     *fiber.App
	db      database.Database
	token   string
	calRepo calendar.CalendarRepository

	ownCal     *calendar.Calendar
	sharedCal  *calendar.Calendar
	privateCal *calendar.Calendar

	ownBook     *addressbook.AddressBook
	sharedBook  *addressbook.AddressBook
	privateBook *addressbook.AddressBook
}

func setupSearchHandlerTest(t *testing.T) *searchFixture {
	t.Helper()

	cfg := &config.Config{
		DataDir:  t.TempDir(),
		Database: config.DatabaseConfig{Driver: "sqlite"},
		JWT:      config.JWTConfig{Secret: "test-secret", AccessExpiry: time.Hour},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(database.Models()...))

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calendarRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	calShareRepo := repository.NewCalendarShareRepository(db.DB())
	abShareRepo := repository.NewAddressBookShareRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	caller := &user.User{UUID: "caller-uuid", Email: "caller@example.com", Username: "caller", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, caller))
	other := &user.User{UUID: "other-uuid", Email: "other@example.com", Username: "other", DisplayName: "Other Person", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, other))

	newCal := func(uuid, name string, owner *user.User) *calendar.Calendar {
		cal := &calendar.Calendar{UUID: uuid, UserID: owner.ID, Name: name, Path: uuid, Color: "#123456"}
		require.NoError(t, calendarRepo.Create(ctx, cal))
		return cal
	}
	ownCal := newCal("own-cal", "Work", caller)
	sharedCal := newCal("shared-cal", "Family Calendar", other)
	privateCal := newCal("private-cal", "Private Family Plans", other)

	require.NoError(t, calShareRepo.Create(ctx, &sharing.CalendarShare{
		UUID:         "cal-share-uuid",
		CalendarID:   sharedCal.ID,
		SharedWithID: caller.ID,
		Permission:   "read",
	}))

	newBook := func(uuid, name string, owner *user.User) *addressbook.AddressBook {
		ab := &addressbook.AddressBook{UUID: uuid, UserID: owner.ID, Name: name, Path: uuid, SyncToken: "data:1", CTag: "1"}
		require.NoError(t, abRepo.Create(ctx, ab))
		return ab
	}
	ownBook := newBook("own-book", "My Contacts", caller)
	sharedBook := newBook("shared-book", "Family Contacts", other)
	privateBook := newBook("private-book", "Private Family Contacts", other)

	require.NoError(t, abShareRepo.Create(ctx, &sharing.AddressBookShare{
		UUID:          "ab-share-uuid",
		AddressBookID: sharedBook.ID,
		SharedWithID:  caller.ID,
		Permission:    "read",
	}))

	calList := calendarusecase.NewListCalendarsUseCase(calendarRepo, calShareRepo)
	abList := addressbookusecase.NewListUseCase(abRepo, abShareRepo)
	handler := NewSearchHandler(searchusecase.NewUseCase(calendarRepo, abRepo, calList, abList))

	app := fiber.New()
	app.Get("/api/v1/search", Authenticate(jwtManager, userRepo), handler.Search)

	token, _, _ := jwtManager.GenerateAccessToken(caller.UUID, caller.Email)

	return &searchFixture{
		app: app, db: db, token: token, calRepo: calendarRepo,
		ownCal: ownCal, sharedCal: sharedCal, privateCal: privateCal,
		ownBook: ownBook, sharedBook: sharedBook, privateBook: privateBook,
	}
}

// addEvent inserts a VEVENT with its denormalized columns derived from the
// iCalendar body by the same function every write path uses, so the fixtures
// cannot drift from production data.
func (f *searchFixture) addEvent(t *testing.T, cal *calendar.Calendar, uid, summary string, start time.Time, extra string) *calendar.CalendarObject {
	t.Helper()

	ics := fmt.Sprintf(
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:%s\r\nSUMMARY:%s\r\nDTSTART:%s\r\nDTEND:%s\r\n%sEND:VEVENT\r\nEND:VCALENDAR\r\n",
		uid, summary,
		start.UTC().Format("20060102T150405Z"),
		start.Add(time.Hour).UTC().Format("20060102T150405Z"),
		extra,
	)

	obj := &calendar.CalendarObject{
		UUID:       uid + "-uuid",
		CalendarID: cal.ID,
		UID:        uid,
		Path:       uid + ".ics",
		ETag:       uid + "-etag",
		ICalData:   ics,
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.NoError(t, f.db.DB().Create(obj).Error)
	return obj
}

func (f *searchFixture) addContact(t *testing.T, book *addressbook.AddressBook, name string) {
	t.Helper()
	obj := &addressbook.AddressObject{
		UUID:          name + "-uuid",
		AddressBookID: book.ID,
		UID:           name + "-uid",
		Path:          name + ".vcf",
		VCardData:     "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:" + name + "\r\nEND:VCARD\r\n",
		FormattedName: name,
	}
	require.NoError(t, f.db.DB().Create(obj).Error)
}

func (f *searchFixture) search(t *testing.T, rawQuery string) (int, SearchResponse) {
	t.Helper()
	req, _ := http.NewRequest("GET", "/api/v1/search?"+rawQuery, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	resp, err := f.app.Test(req)
	require.NoError(t, err)

	var body SearchResponse
	if resp.StatusCode == fiber.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	}
	return resp.StatusCode, body
}

func eventSummaries(g SearchEventGroup) []string {
	out := make([]string, 0, len(g.Items))
	for _, item := range g.Items {
		out = append(out, item.Event.Summary)
	}
	return out
}

// The defect #156 exists to fix: with the old ±6-month client-side window an
// event two years out — or two years back — could not be found at all.
func TestSearchHasNoImplicitDateWindow(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	now := time.Now()
	f.addEvent(t, f.ownCal, "far-future", "Standup far ahead", now.AddDate(2, 0, 0), "")
	f.addEvent(t, f.ownCal, "far-past", "Standup long ago", now.AddDate(-2, 0, 0), "")
	f.addEvent(t, f.ownCal, "unrelated", "Dentist", now.AddDate(0, 0, 1), "")

	status, body := f.search(t, "q=standup")
	require.Equal(t, fiber.StatusOK, status)

	assert.ElementsMatch(t, []string{"Standup far ahead", "Standup long ago"}, eventSummaries(body.Events))
	assert.True(t, body.Events.Searched)
	assert.False(t, body.Events.HasMore)
}

// Upcoming first (soonest first), then past (most recent first) — the ranking
// the client used to apply after its fan-out.
func TestSearchRanksUpcomingBeforePast(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	now := time.Now()
	f.addEvent(t, f.ownCal, "soon", "Review soon", now.Add(48*time.Hour), "")
	f.addEvent(t, f.ownCal, "later", "Review later", now.AddDate(1, 0, 0), "")
	f.addEvent(t, f.ownCal, "recent", "Review recent", now.Add(-48*time.Hour), "")
	f.addEvent(t, f.ownCal, "ancient", "Review ancient", now.AddDate(-3, 0, 0), "")

	status, body := f.search(t, "q=review")
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t,
		[]string{"Review soon", "Review later", "Review recent", "Review ancient"},
		eventSummaries(body.Events),
	)
}

func TestSearchIncludesSharedAndHidesUnreadable(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	now := time.Now()
	f.addEvent(t, f.ownCal, "own-ev", "Picnic mine", now.Add(time.Hour), "")
	f.addEvent(t, f.sharedCal, "shared-ev", "Picnic shared", now.Add(2*time.Hour), "")
	f.addEvent(t, f.privateCal, "private-ev", "Picnic private", now.Add(3*time.Hour), "")

	f.addContact(t, f.ownBook, "Picnic Mine")
	f.addContact(t, f.sharedBook, "Picnic Shared")
	f.addContact(t, f.privateBook, "Picnic Private")

	status, body := f.search(t, "q=picnic")
	require.Equal(t, fiber.StatusOK, status)

	// A read-only share is readable: its events and contacts belong in results.
	assert.ElementsMatch(t, []string{"Picnic mine", "Picnic shared"}, eventSummaries(body.Events))

	names := make([]string, 0, len(body.Contacts.Items))
	books := make([]string, 0, len(body.Contacts.Items))
	for _, item := range body.Contacts.Items {
		names = append(names, item.Contact.FormattedName)
		books = append(books, item.AddressBookName)
	}
	assert.ElementsMatch(t, []string{"Picnic Mine", "Picnic Shared"}, names)
	assert.ElementsMatch(t, []string{"My Contacts", "Family Contacts"}, books)
}

// Collections themselves are searched, and an unshared one must not surface —
// a calendar name alone reveals its existence.
func TestSearchCollectionsRespectAccess(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	status, body := f.search(t, "q=family")
	require.Equal(t, fiber.StatusOK, status)

	calNames := make([]string, 0, len(body.Calendars.Items))
	for _, cal := range body.Calendars.Items {
		calNames = append(calNames, cal.Name)
		assert.Equal(t, "read", cal.Permission, "the shared calendar must report the share's level")
		assert.True(t, cal.Shared)
		require.NotNil(t, cal.Owner)
		assert.Equal(t, "Other Person", cal.Owner.DisplayName)
	}
	assert.Equal(t, []string{"Family Calendar"}, calNames, "the unshared 'Private Family Plans' must not appear")

	bookNames := make([]string, 0, len(body.AddressBooks.Items))
	for _, ab := range body.AddressBooks.Items {
		bookNames = append(bookNames, ab.Name)
	}
	assert.Equal(t, []string{"Family Contacts"}, bookNames, "the unshared 'Private Family Contacts' must not appear")
}

// A recurring series is one hit, represented by the occurrence a user would
// expect to be shown — the next one — carrying its own recurrence id so the
// client can open the occurrence instead of the series master.
func TestSearchResolvesUpcomingOccurrenceOfLiveSeries(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	// Weekly since two years ago, unbounded: nothing about its stored start
	// time is worth showing, and the old window-based search would have found
	// it only because some occurrence happened to fall inside the window.
	start := time.Now().AddDate(-2, 0, 0).Truncate(time.Hour)
	f.addEvent(t, f.ownCal, "series", "Weekly Standup", start, "RRULE:FREQ=WEEKLY\r\n")

	status, body := f.search(t, "q=standup")
	require.Equal(t, fiber.StatusOK, status)
	require.Len(t, body.Events.Items, 1, "a series must be returned once, not once per occurrence")

	hit := body.Events.Items[0]
	assert.Equal(t, "Weekly Standup", hit.Event.Summary)
	assert.True(t, hit.Event.IsRecurring)
	require.NotNil(t, hit.Event.RecurrenceID, "the occurrence must carry a recurrence id")
	assert.NotEmpty(t, *hit.Event.RecurrenceID)
	assert.False(t, hit.Event.Start.Before(time.Now().Add(-time.Hour)),
		"expected the next occurrence, got %s (the series master start was %s)", hit.Event.Start, start)
	assert.True(t, hit.Event.Start.Before(time.Now().AddDate(0, 0, 8)), "the next weekly beat is within a week")
	assert.Equal(t, f.ownCal.UUID, hit.CalendarUUID)
	assert.Equal(t, "Work", hit.CalendarName)
	assert.Equal(t, "#123456", hit.CalendarColor)
}

// A series that has finished is represented by its LAST occurrence, not its
// first — "when did that stop happening" is the useful answer.
func TestSearchResolvesLastOccurrenceOfPastSeries(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	start := time.Now().AddDate(-3, 0, 0).Truncate(time.Hour)
	f.addEvent(t, f.ownCal, "old-series", "Retired Ritual", start, "RRULE:FREQ=WEEKLY;COUNT=5\r\n")

	status, body := f.search(t, "q=ritual")
	require.Equal(t, fiber.StatusOK, status)
	require.Len(t, body.Events.Items, 1)

	hit := body.Events.Items[0]
	require.NotNil(t, hit.Event.RecurrenceID)
	// COUNT=5 weekly: the fifth beat is four weeks after the first.
	assert.WithinDuration(t, start.AddDate(0, 0, 28), hit.Event.Start, time.Hour)
}

func TestSearchTypesFilter(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	f.addEvent(t, f.ownCal, "ev", "Alice sync", time.Now().Add(time.Hour), "")
	f.addContact(t, f.ownBook, "Alice Adams")

	status, body := f.search(t, "q=alice&types=contacts")
	require.Equal(t, fiber.StatusOK, status)

	assert.Len(t, body.Contacts.Items, 1)
	assert.True(t, body.Contacts.Searched)
	// The distinction that matters: events were not searched, which is NOT the
	// same as "no events matched".
	assert.False(t, body.Events.Searched)
	assert.Empty(t, body.Events.Items)
	assert.Equal(t, []string{"contacts"}, body.Types)

	status, _ = f.search(t, "q=alice&types=events,teapots")
	assert.Equal(t, fiber.StatusBadRequest, status, "an unknown type must be rejected, not silently dropped")
}

func TestSearchLimitCapAndTruncation(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	now := time.Now()
	for i := 1; i <= 3; i++ {
		f.addEvent(t, f.ownCal, fmt.Sprintf("ev-%d", i), fmt.Sprintf("Sprint %d", i), now.Add(time.Duration(i)*time.Hour), "")
	}

	status, body := f.search(t, "q=sprint&limit=2")
	require.Equal(t, fiber.StatusOK, status)
	assert.Len(t, body.Events.Items, 2)
	assert.Equal(t, 2, body.Events.Count)
	assert.True(t, body.Events.HasMore, "truncation must be reported, not silent")

	// Page two picks up where page one stopped.
	status, page2 := f.search(t, "q=sprint&limit=2&offset=2")
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, []string{"Sprint 3"}, eventSummaries(page2.Events))
	assert.False(t, page2.Events.HasMore)

	// A limit above the cap is clamped, and the cap is visible in the response.
	status, capped := f.search(t, "q=sprint&limit=5000")
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, searchusecase.MaxLimit, capped.Limit)
	assert.Equal(t, searchusecase.MaxLimit, capped.MaxLimit)
}

func TestSearchExplicitDateBounds(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	now := time.Now()
	f.addEvent(t, f.ownCal, "inside", "Bound inside", now.Add(24*time.Hour), "")
	f.addEvent(t, f.ownCal, "outside", "Bound outside", now.AddDate(1, 0, 0), "")

	q := url.Values{}
	q.Set("q", "bound")
	q.Set("start", now.Format(time.RFC3339))
	q.Set("end", now.Add(72*time.Hour).Format(time.RFC3339))

	status, body := f.search(t, q.Encode())
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, []string{"Bound inside"}, eventSummaries(body.Events))
}

func TestSearchRejectsBadInput(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	cases := map[string]string{
		"missing query":     "",
		"one character":     "q=a",
		"whitespace only":   "q=%20%20",
		"bad limit":         "q=alice&limit=lots",
		"negative offset":   "q=alice&offset=-1",
		"bad start":         "q=alice&start=yesterday",
		"bad end":           "q=alice&end=tomorrow",
		"end before start":  "q=alice&start=2026-06-02T00:00:00Z&end=2026-06-01T00:00:00Z",
		"empty types value": "q=alice&types=",
	}
	for name, rawQuery := range cases {
		t.Run(name, func(t *testing.T) {
			status, _ := f.search(t, rawQuery)
			if name == "empty types value" {
				// An empty types parameter means "all types", not an error.
				assert.Equal(t, fiber.StatusOK, status)
				return
			}
			assert.Equal(t, fiber.StatusBadRequest, status)
		})
	}
}

func TestSearchRequiresAuthentication(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	req, _ := http.NewRequest("GET", "/api/v1/search?q=alice", nil)
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
}

// LIKE wildcards in the query text must match literally, so a user typing "%"
// does not get every event in every calendar back.
func TestSearchTreatsWildcardsLiterally(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	now := time.Now()
	f.addEvent(t, f.ownCal, "pct", "Sale 50% off", now.Add(time.Hour), "")
	f.addEvent(t, f.ownCal, "plain", "Ordinary meeting", now.Add(2*time.Hour), "")

	status, body := f.search(t, "q=%25%25")
	require.Equal(t, fiber.StatusOK, status)
	assert.Empty(t, body.Events.Items, "'%%%%' must match literally, not act as a wildcard")

	status, body = f.search(t, "q=50%25")
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, []string{"Sale 50% off"}, eventSummaries(body.Events))
}

// Location and description are searchable too, matching what the client used to
// filter on.
func TestSearchMatchesLocationAndDescription(t *testing.T) {
	f := setupSearchHandlerTest(t)
	defer f.db.Close()

	now := time.Now()
	f.addEvent(t, f.ownCal, "loc", "Lunch", now.Add(time.Hour), "LOCATION:Standup Cafe\r\n")
	f.addEvent(t, f.ownCal, "desc", "Retro", now.Add(2*time.Hour), "DESCRIPTION:replaces the standup\r\n")

	status, body := f.search(t, "q=standup")
	require.Equal(t, fiber.StatusOK, status)
	assert.ElementsMatch(t, []string{"Lunch", "Retro"}, eventSummaries(body.Events))
}
