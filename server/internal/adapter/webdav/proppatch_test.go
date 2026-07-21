package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestCollectionProppatchRenameRecolor is the regression test for #43:
// PROPPATCH on a collection (which emersion 501s) must apply displayname and
// Apple calendar-color for the owner, 403 unsupported props, and 403 a sharee.
func TestCollectionProppatchRenameRecolor(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())

	pw, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	owner := &user.User{UUID: "owner", Email: "owner@example.com", Username: "owner", PasswordHash: string(pw), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, owner))
	sharee := &user.User{UUID: "sharee", Email: "sharee@example.com", Username: "sharee", PasswordHash: string(pw), IsActive: true}
	require.NoError(t, userRepo.Create(ctx, sharee))

	cal := &calendar.Calendar{UserID: owner.ID, UUID: uuid.New().String(), Path: "work", Name: "Work", Color: "#111111", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, cal))
	require.NoError(t, shareRepo.Create(ctx, &sharing.CalendarShare{
		UUID: uuid.New().String(), CalendarID: cal.ID, SharedWithID: sharee.ID, Permission: "read",
	}))

	proppatch := func(user, pass, path, body string) (*http.Response, SyncMultiStatus) {
		req, _ := http.NewRequest("PROPPATCH", path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		var ms SyncMultiStatus
		if resp.StatusCode == http.StatusMultiStatus {
			require.NoError(t, xml.NewDecoder(resp.Body).Decode(&ms))
		}
		return resp, ms
	}

	locals := func(ms SyncMultiStatus, code string) map[string]bool {
		out := map[string]bool{}
		if len(ms.Responses) == 0 {
			return out
		}
		for _, ps := range ms.Responses[0].PropStat {
			if strings.Contains(ps.Status, code) {
				for _, raw := range ps.Prop.Raw {
					out[raw.XMLName.Local] = true
				}
			}
		}
		return out
	}

	body := `<D:propertyupdate xmlns:D="DAV:" xmlns:A="http://apple.com/ns/ical/">
  <D:set><D:prop>
    <D:displayname>Renamed Work</D:displayname>
    <A:calendar-color>#FF0000FF</A:calendar-color>
    <D:unsupported-thing>x</D:unsupported-thing>
  </D:prop></D:set>
</D:propertyupdate>`

	t.Run("owner rename + recolor", func(t *testing.T) {
		resp, ms := proppatch("owner@example.com", "password", "/dav/owner/calendars/work/", body)
		require.Equal(t, http.StatusMultiStatus, resp.StatusCode)

		ok := locals(ms, "200")
		assert.True(t, ok["displayname"], "displayname must be applied")
		assert.True(t, ok["calendar-color"], "calendar-color must be applied")

		bad := locals(ms, "403")
		assert.True(t, bad["unsupported-thing"], "unsupported prop must be 403'd")

		stored, err := calRepo.GetByPath(ctx, owner.ID, "work")
		require.NoError(t, err)
		assert.Equal(t, "Renamed Work", stored.Name)
		assert.Equal(t, "#FF0000", stored.Color, "Apple #RRGGBBAA must be truncated to #RRGGBB")
	})

	t.Run("sharee is forbidden", func(t *testing.T) {
		resp, _ := proppatch("sharee@example.com", "password", "/dav/sharee/calendars/work/", body)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}
