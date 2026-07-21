package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// discoveryBody requests displayname, getctag, sync-token, supported-report-set
// and a bogus <foo/> (which must land in the 404 propstat).
const discoveryBody = `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:CS="http://calendarserver.org/ns/">
  <D:prop>
    <D:displayname/>
    <CS:getctag/>
    <D:sync-token/>
    <D:supported-report-set/>
    <D:foo/>
  </D:prop>
</D:propfind>`

// propstatByStatus returns the local-name -> inner-text map for the propstat
// whose Status contains the given code (e.g. "200", "404").
func propstatByStatus(t *testing.T, ms SyncMultiStatus, code string) map[string]string {
	t.Helper()
	require.Len(t, ms.Responses, 1)
	for _, ps := range ms.Responses[0].PropStat {
		if strings.Contains(ps.Status, code) {
			out := map[string]string{}
			for _, raw := range ps.Prop.Raw {
				out[raw.XMLName.Local] = string(raw.Inner)
			}
			return out
		}
	}
	return map[string]string{}
}

// TestCollectionPropfindServesSyncDiscovery is the regression test for #41: a
// Depth:0 PROPFIND on a collection must serve sync-token / getctag /
// supported-report-set / displayname so clients discover the sync REPORT.
func TestCollectionPropfindServesSyncDiscovery(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	abRepo := repository.NewAddressBookRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{UUID: "u", Email: "u@example.com", Username: "u", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(context.Background(), u))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("u@example.com:password"))

	cal := &calendar.Calendar{UserID: u.ID, UUID: "c", Path: "cal", Name: "My Calendar", Timezone: "UTC", CTag: "ctag-cal", SyncToken: "data:,synctoken-cal"}
	require.NoError(t, calRepo.Create(context.Background(), cal))
	ab := &addressbook.AddressBook{UUID: "ab", UserID: u.ID, Path: "book", Name: "My Book", CTag: "ctag-ab", SyncToken: "data:,synctoken-ab"}
	require.NoError(t, abRepo.Create(context.Background(), ab))

	do := func(path string) SyncMultiStatus {
		req, _ := http.NewRequest("PROPFIND", path, bytes.NewReader([]byte(discoveryBody)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		req.Header.Set("Depth", "0")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)
		var ms SyncMultiStatus
		require.NoError(t, xml.NewDecoder(resp.Body).Decode(&ms))
		return ms
	}

	t.Run("calendar", func(t *testing.T) {
		// Read back the authoritative stored values (Create may re-mint).
		stored, err := calRepo.GetByPath(context.Background(), u.ID, "cal")
		require.NoError(t, err)

		ms := do("/dav/u/calendars/cal/")
		ok := propstatByStatus(t, ms, "200")
		assert.Equal(t, "My Calendar", ok["displayname"])
		assert.Equal(t, stored.CTag, ok["getctag"], "getctag must equal the calendar's CTag")
		assert.Equal(t, stored.SyncToken, ok["sync-token"])
		assert.Contains(t, ok, "supported-report-set")
		assert.Contains(t, ok["supported-report-set"], "sync-collection")

		nf := propstatByStatus(t, ms, "404")
		assert.Contains(t, nf, "foo", "an unknown prop must sit in the 404 propstat")
	})

	t.Run("addressbook", func(t *testing.T) {
		ms := do("/dav/u/addressbooks/book/")
		ok := propstatByStatus(t, ms, "200")
		assert.Equal(t, "My Book", ok["displayname"])
		assert.NotEmpty(t, ok["getctag"])
		assert.Contains(t, ok["supported-report-set"], "addressbook-multiget")

		nf := propstatByStatus(t, ms, "404")
		assert.Contains(t, nf, "foo")
	})
}
