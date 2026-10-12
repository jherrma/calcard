package webdav

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestPrincipalPropfindEmptyBodyIsAllprop is the regression test for F06: a
// PROPFIND without a body on the discovery root or the principal must be
// treated as allprop (RFC 4918 §9.1) instead of the 400 "webdav: expected
// application/xml request" go-webdav's ServePrincipal answers on its own.
func TestPrincipalPropfindEmptyBodyIsAllprop(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	require.NoError(t, repository.NewUserRepository(db.DB()).Create(context.Background(), &user.User{
		UUID:         "principal-uuid",
		Email:        "principal@example.com",
		Username:     "principaluser",
		PasswordHash: string(hash),
		IsActive:     true,
	}))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("principal@example.com:password"))

	for _, target := range []string{"/dav/", "/dav/principaluser/"} {
		// With and without a Content-Type: an empty body is allprop either way.
		for _, ctype := range []string{"", "application/xml"} {
			t.Run(target+" content-type="+ctype, func(t *testing.T) {
				req, _ := http.NewRequest("PROPFIND", target, nil)
				req.Header.Set("Authorization", authHeader)
				req.Header.Set("Depth", "0")
				if ctype != "" {
					req.Header.Set("Content-Type", ctype)
				}
				resp := davDo(t, app, req)
				defer resp.Body.Close()
				raw, _ := io.ReadAll(resp.Body)
				body := string(raw)

				require.Equalf(t, http.StatusMultiStatus, resp.StatusCode, "body: %s", body)
				assert.Contains(t, body, "current-user-principal")
				assert.Contains(t, body, "/dav/principaluser/calendars/", "allprop must include the calendar home set")
				assert.Contains(t, body, "/dav/principaluser/addressbooks/", "allprop must include the addressbook home set")
				assert.Contains(t, body, "<href>"+target+"</href>", "the response must describe the requested URL")
			})
		}
	}
}
