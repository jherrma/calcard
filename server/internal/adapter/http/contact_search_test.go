package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	addressbookusecase "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	contactusecase "github.com/jherrma/caldav-server/internal/usecase/contact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contactSearchFixture is the world the /contacts/search corpus tests run in: the
// caller owns one address book, a second user shares a second one with them
// read-only, and that second user keeps a third entirely to themselves. Each book
// holds one "Alice", so a single query distinguishes all three cases at once.
type contactSearchFixture struct {
	app   *fiber.App
	db    database.Database
	token string

	ownBook     *addressbook.AddressBook
	sharedBook  *addressbook.AddressBook
	privateBook *addressbook.AddressBook
}

func setupContactSearchTest(t *testing.T) *contactSearchFixture {
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
	abRepo := repository.NewAddressBookRepository(db.DB())
	abShareRepo := repository.NewAddressBookShareRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	caller := &user.User{UUID: "caller-uuid", Email: "caller@example.com", Username: "caller", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, caller))
	other := &user.User{UUID: "other-uuid", Email: "other@example.com", Username: "other", DisplayName: "Other Person", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, other))

	newBook := func(uuid, name string, owner *user.User) *addressbook.AddressBook {
		ab := &addressbook.AddressBook{UUID: uuid, UserID: owner.ID, Name: name, Path: uuid, SyncToken: "data:1", CTag: "1"}
		require.NoError(t, abRepo.Create(ctx, ab))
		return ab
	}
	ownBook := newBook("own-book", "My Contacts", caller)
	sharedBook := newBook("shared-book", "Family Contacts", other)
	privateBook := newBook("private-book", "Private Contacts", other)

	require.NoError(t, abShareRepo.Create(ctx, &sharing.AddressBookShare{
		UUID:          "ab-share-uuid",
		AddressBookID: sharedBook.ID,
		SharedWithID:  caller.ID,
		Permission:    "read",
	}))

	abListUC := addressbookusecase.NewListUseCase(abRepo, abShareRepo)
	handler := NewContactHandler(
		contactusecase.NewCreateUseCase(addressbookusecase.NewCreateContactUseCase(abRepo)),
		contactusecase.NewListUseCase(abRepo),
		contactusecase.NewGetUseCase(abRepo),
		contactusecase.NewUpdateUseCase(abRepo),
		contactusecase.NewDeleteUseCase(abRepo),
		contactusecase.NewSearchUseCase(abRepo, abListUC),
		contactusecase.NewMoveUseCase(abRepo),
		contactusecase.NewPhotoUseCase(abRepo),
		abRepo,
	)

	app := fiber.New()
	app.Get("/api/v1/contacts/search", Authenticate(jwtManager, userRepo), handler.Search)

	token, _, _ := jwtManager.GenerateAccessToken(caller.UUID, caller.Email)

	f := &contactSearchFixture{
		app: app, db: db, token: token,
		ownBook: ownBook, sharedBook: sharedBook, privateBook: privateBook,
	}
	f.addContact(t, ownBook, "Alice Owner")
	f.addContact(t, sharedBook, "Alice Shared")
	f.addContact(t, privateBook, "Alice Private")
	return f
}

func (f *contactSearchFixture) addContact(t *testing.T, book *addressbook.AddressBook, name string) {
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

// search issues the request and returns the status plus the decoded body. The
// response shape is unchanged by #162: { contacts, query, count }, raw JSON.
func (f *contactSearchFixture) search(t *testing.T, rawQuery string) (int, contactusecase.SearchOutput) {
	t.Helper()
	req, _ := http.NewRequest("GET", "/api/v1/contacts/search?"+rawQuery, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	resp, err := f.app.Test(req)
	require.NoError(t, err)

	var body contactusecase.SearchOutput
	if resp.StatusCode == fiber.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	}
	return resp.StatusCode, body
}

func contactNames(out contactusecase.SearchOutput) []string {
	names := make([]string, 0, len(out.Contacts))
	for _, c := range out.Contacts {
		names = append(names, c.FormattedName)
	}
	return names
}

// TestContactSearchIncludesSharedBooks is the #162 regression test: the corpus is
// every book the caller can READ, so a contact in a shared book is findable — it
// used to vanish the moment you searched for it, because the query filtered on
// address_books.user_id. A book that was never shared still contributes nothing.
func TestContactSearchIncludesSharedBooks(t *testing.T) {
	f := setupContactSearchTest(t)
	defer f.db.Close()

	status, out := f.search(t, "q=Alice")
	require.Equal(t, fiber.StatusOK, status)

	assert.ElementsMatch(t, []string{"Alice Owner", "Alice Shared"}, contactNames(out))
	assert.Equal(t, 2, out.Count)
	assert.NotContains(t, contactNames(out), "Alice Private")
}

// TestContactSearchHitsCarryTheirOwnBook guards the grouping the contacts page
// does client-side: it matches a hit to a sidebar entry by the numeric
// addressbook_id, so a shared hit must report the SHARED book's id and not the
// caller's own. Getting this wrong would file the contact under the wrong book
// instead of hiding it — a subtler version of the same bug.
func TestContactSearchHitsCarryTheirOwnBook(t *testing.T) {
	f := setupContactSearchTest(t)
	defer f.db.Close()

	_, out := f.search(t, "q=Alice+Shared")
	require.Len(t, out.Contacts, 1)
	assert.Equal(t, fmt.Sprintf("%d", f.sharedBook.ID), out.Contacts[0].AddressBookID)
}

// TestContactSearchFilterAcceptsSharedBook covers the second half of #162: the
// addressbook_id filter narrows WITHIN the readable set, so pointing it at a
// shared book returns that book's matches. Before the fix the handler accepted
// the UUID and then ANDed it with user_id = caller, which can never match — an
// authoritative-looking empty list.
func TestContactSearchFilterAcceptsSharedBook(t *testing.T) {
	f := setupContactSearchTest(t)
	defer f.db.Close()

	status, out := f.search(t, "q=Alice&addressbook_id="+f.sharedBook.UUID)
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, []string{"Alice Shared"}, contactNames(out))
}

func TestContactSearchFilterAcceptsOwnBook(t *testing.T) {
	f := setupContactSearchTest(t)
	defer f.db.Close()

	status, out := f.search(t, "q=Alice&addressbook_id="+f.ownBook.UUID)
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, []string{"Alice Owner"}, contactNames(out))
}

// TestContactSearchFilterRejectsUnreadableBook pins the deliberate choice of
// #162: a filter naming a book the caller cannot read is a 404, not a silently
// dropped filter (which would answer about other books) and not an empty 200
// (which would assert the book has no matches). An unknown UUID gets the same
// answer, so the status can't be used to probe which books exist.
func TestContactSearchFilterRejectsUnreadableBook(t *testing.T) {
	f := setupContactSearchTest(t)
	defer f.db.Close()

	for _, tc := range []struct {
		name string
		uuid string
	}{
		{"book shared with nobody", f.privateBook.UUID},
		{"book that does not exist", "no-such-book"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := f.search(t, "q=Alice&addressbook_id="+tc.uuid)
			assert.Equal(t, fiber.StatusNotFound, status)
		})
	}
}

func TestContactSearchRequiresQuery(t *testing.T) {
	f := setupContactSearchTest(t)
	defer f.db.Close()

	status, _ := f.search(t, "q=")
	assert.Equal(t, fiber.StatusBadRequest, status)
}

// TestContactSearchEmptyResultIsAnArray: no matches must serialise as [] rather
// than null, so a client can't read the absence of a value as an error. Asserted
// on the raw body, because unmarshalling would paper over the difference.
func TestContactSearchEmptyResultIsAnArray(t *testing.T) {
	f := setupContactSearchTest(t)
	defer f.db.Close()

	req, _ := http.NewRequest("GET", "/api/v1/contacts/search?q=Nobody", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"contacts":[]`)
	assert.Contains(t, string(body), `"count":0`)
}
