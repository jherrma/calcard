package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestCanonicalDAVPath(t *testing.T) {
	for _, tc := range []struct {
		raw, want  string
		slashAdded bool
	}{
		{"/dav/", "/dav/", false},
		{"/dav", "/dav/", true},
		{"/DAV/", "/dav/", false},
		{"/dav//", "/dav/", false},
		{"/dav/alice", "/dav/alice/", true},
		{"/dav//alice/", "/dav/alice/", false},
		{"/dav/alice/calendars", "/dav/alice/calendars/", true},
		{"/dav/alice/calendars//", "/dav/alice/calendars/", false},
		{"/dav/alice//calendars/", "/dav/alice/calendars/", false},
		{"/dav/alice/%63alendars/", "/dav/alice/calendars/", false},
		{"/dav/alice/calendars%2F", "/dav/alice/calendars/", false},
		{"/dav/alice/calendars/work", "/dav/alice/calendars/work/", true},
		{"/dav/alice/calendars//work/", "/dav/alice/calendars/work/", false},
		{"/dav/alice/addressbooks/../calendars/work/", "/dav/alice/calendars/work/", false},
		// Fixed segments stay case-sensitive: only the mount is folded.
		{"/dav/alice/Calendars/", "/dav/alice/Calendars/", false},
		// Objects keep the slash state they came with.
		{"/dav/alice/calendars/work/a%20b.ics", "/dav/alice/calendars/work/a b.ics", false},
		{"/dav/alice/calendars/work/pct%2541.ics", "/dav/alice/calendars/work/pct%41.ics", false},
		{"/dav/alice/calendars/work//x.ics", "/dav/alice/calendars/work/x.ics", false},
	} {
		got, slashAdded, inDAV, ok := canonicalDAVPath(tc.raw)
		assert.True(t, ok && inDAV, tc.raw)
		assert.Equal(t, tc.want, got, tc.raw)
		assert.Equal(t, tc.slashAdded, slashAdded, tc.raw)
	}

	for _, raw := range []string{"/dav/../api/v1/users/me", "/dav/%2e%2e/api/v1/users/me", "/davx/"} {
		_, _, inDAV, ok := canonicalDAVPath(raw)
		assert.True(t, ok, raw)
		assert.False(t, inDAV, "%s must not stay under /dav", raw)
	}
	_, _, _, ok := canonicalDAVPath("/dav/alice/%zz/")
	assert.False(t, ok, "an invalid escape is not a path")
}

// TestDAVPathFormsAreCanonical is the regression test for F29. Every spelling
// of a DAV path — without the collection's trailing slash, with doubled
// slashes, percent-encoded, with an upper-case mount — must be answered like
// the canonical path. It used to be decided three ways: Fiber's raw c.Path()
// for scope and routing, emersion's decoded r.URL.Path for the response, and
// an exact string compare for the home set, so a home set without its slash
// answered an empty 207 and an encoded "calendars" segment slipped past the
// protocol scope of a CardDAV-only credential.
func TestDAVPathFormsAreCanonical(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	appPwdRepo := repository.NewAppPasswordRepository(db.DB())
	cardCredRepo := repository.NewCardDAVCredentialRepository(db.DB())

	hash := func(pw string) string {
		h, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
		return string(h)
	}
	alice := &user.User{UUID: "alice", Email: "alice@example.com", Username: "alice", PasswordHash: hash("password"), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, alice))
	require.NoError(t, calRepo.Create(ctx, &calendar.Calendar{UserID: alice.ID, UUID: uuid.New().String(), Path: "work", Name: "Work", Timezone: "UTC"}))
	require.NoError(t, abRepo.Create(ctx, &addressbook.AddressBook{UUID: uuid.New().String(), UserID: alice.ID, Path: "contacts", Name: "Contacts"}))
	require.NoError(t, appPwdRepo.Create(ctx, &user.AppPassword{UUID: uuid.New().String(), UserID: alice.ID, Name: "cards", PasswordHash: hash("card-only"), Scopes: `["carddav"]`, CreatedAt: time.Now()}))
	require.NoError(t, appPwdRepo.Create(ctx, &user.AppPassword{UUID: uuid.New().String(), UserID: alice.ID, Name: "cals", PasswordHash: hash("cal-only"), Scopes: `["caldav"]`, CreatedAt: time.Now()}))
	require.NoError(t, cardCredRepo.Create(ctx, &user.CardDAVCredential{UUID: uuid.New().String(), UserID: alice.ID, Name: "card cred", Username: "card-cred", PasswordHash: hash("card-cred-pass"), Permission: "read-write", CreatedAt: time.Now()}))

	basic := func(id, pw string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+pw))
	}
	owner := basic("alice@example.com", "password")
	do := func(t *testing.T, method, path, auth, depth, body string, hdr ...string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, path, bytes.NewReader([]byte(body)))
		require.NoError(t, err)
		req.Header.Set("Authorization", auth)
		if depth != "" {
			req.Header.Set("Depth", depth)
		}
		if method == "PUT" {
			req.Header.Set("Content-Type", "text/calendar")
		} else if body != "" {
			req.Header.Set("Content-Type", "application/xml")
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		return davDo(t, app, req)
	}
	const propfind = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:displayname/></d:prop></d:propfind>`
	read := func(resp *http.Response) string {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	for _, home := range []struct{ seg, encoded string }{
		{"calendars", "%63alendars"},
		{"addressbooks", "%61ddressbooks"},
	} {
		canonical := "/dav/alice/" + home.seg + "/"
		for _, depth := range []string{"0", "1"} {
			resp := do(t, "PROPFIND", canonical, owner, depth, propfind)
			require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
			assert.Empty(t, resp.Header.Get("Content-Location"), "the canonical form needs no Content-Location")
			want := read(resp)
			require.Contains(t, want, "<href>"+canonical+"</href>")

			t.Run(home.seg+" without slash depth "+depth, func(t *testing.T) {
				resp := do(t, "PROPFIND", "/dav/alice/"+home.seg, owner, depth, propfind)
				require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
				assert.Equal(t, canonical, resp.Header.Get("Content-Location"), "RFC 4918 §5.2")
				assert.Equal(t, want, read(resp), "the home without its slash must answer like the home")
			})

			for _, variant := range []string{
				"/dav/alice/" + home.seg + "//",
				"/dav//alice/" + home.seg + "/",
				"/dav/alice//" + home.seg + "/",
				"/DAV/alice/" + home.seg + "/",
				"/dav/alice/" + home.encoded + "/",
			} {
				t.Run(variant+" depth "+depth, func(t *testing.T) {
					resp := do(t, "PROPFIND", variant, owner, depth, propfind)
					require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
					assert.Equal(t, want, read(resp))
				})
			}
		}
	}

	t.Run("a collection without its slash is described under the slash form", func(t *testing.T) {
		for _, depth := range []string{"0", "1"} {
			resp := do(t, "PROPFIND", "/dav/alice/calendars/work", owner, depth, propfind)
			require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
			assert.Equal(t, "/dav/alice/calendars/work/", resp.Header.Get("Content-Location"))
			body := read(resp)
			assert.Contains(t, body, "<href>/dav/alice/calendars/work/</href>", "depth %s", depth)
			assert.NotContains(t, body, "<href>/dav/alice/calendars/work</href>", "depth %s", depth)
		}
	})

	t.Run("fixed segments stay case-sensitive", func(t *testing.T) {
		resp := do(t, "PROPFIND", "/dav/alice/Calendars/", owner, "1", propfind)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("dot segments cannot leave /dav", func(t *testing.T) {
		resp := do(t, "GET", "/dav/%2e%2e/api/v1/users/me", owner, "", "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	// The scope of a single-protocol credential is decided on the same path
	// emersion serves, however the client spells it.
	cardOnly := map[string]string{
		"carddav-only app password": basic("alice@example.com", "card-only"),
		"CardDAV credential":        basic("card-cred", "card-cred-pass"),
	}
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//t//EN\r\nBEGIN:VEVENT\r\nUID:scope-%s\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nSUMMARY:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	for name, auth := range cardOnly {
		t.Run(name+" cannot reach calendars through another spelling", func(t *testing.T) {
			for _, p := range []string{"/dav/alice/%63alendars/work/", "/dav/alice//calendars/work/", "/dav/alice/addressbooks/../calendars/work/"} {
				resp := do(t, "PROPFIND", p, auth, "1", propfind)
				assert.Equal(t, http.StatusForbidden, resp.StatusCode, "PROPFIND %s", p)
			}
			resp := do(t, "PUT", "/dav/alice/%63alendars/work/sneaky.ics", auth, "", ics)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "PUT")
			resp = do(t, "PROPFIND", "/dav/alice/addressbooks/contacts/", auth, "1", propfind)
			assert.Equal(t, http.StatusMultiStatus, resp.StatusCode, "its own protocol keeps working")
		})
	}
	t.Run("caldav-only app password cannot reach address books through another spelling", func(t *testing.T) {
		resp := do(t, "PROPFIND", "/dav/alice/%61ddressbooks/contacts/", basic("alice@example.com", "cal-only"), "1", propfind)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("If-Match on DELETE holds for a percent-encoded object name", func(t *testing.T) {
		resp := do(t, "PUT", "/dav/alice/calendars/work/a%20b.ics", owner, "", ics)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		resp = do(t, "DELETE", "/dav/alice/calendars/work/a%20b.ics", owner, "", "", "If-Match", `"stale"`)
		assert.Equal(t, http.StatusPreconditionFailed, resp.StatusCode)
		resp = do(t, "GET", "/dav/alice/calendars/work/a%20b.ics", owner, "", "")
		assert.Equal(t, http.StatusOK, resp.StatusCode, "a stale If-Match must not delete the object")
	})
}
