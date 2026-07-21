package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const truncatedStatus = "HTTP/1.1 507 Insufficient Storage"

// TestBuildAddressBookHrefEscapes covers RFC 6578 defect (3): hrefs built by
// string formatting must be percent-escaped.
func TestBuildAddressBookHrefEscapes(t *testing.T) {
	got := buildAddressBookHref("/dav/u/addressbooks/book/", "a b.vcf")
	assert.Equal(t, "/dav/u/addressbooks/book/a%20b.vcf", got)
}

// TestWebDAVSyncRFC6578 covers defects (1) duplicate responses and (2) an
// ignored <limit> in the sync-collection REPORT.
func TestWebDAVSyncRFC6578(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()

	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	u := &user.User{UUID: "u", Email: "u@example.com", Username: "u", PasswordHash: string(passwordHash), IsActive: true}
	require.NoError(t, userRepo.Create(context.Background(), u))
	cal := &calendar.Calendar{UserID: u.ID, UUID: "c", Path: "cal", Name: "Cal", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(context.Background(), cal))
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("u@example.com:password"))

	put := func(name, summary string) {
		ical := fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:20240101T000000Z\r\nDTSTART:20240101T090000Z\r\nDTEND:20240101T100000Z\r\nSUMMARY:%s\r\nEND:VEVENT\r\nEND:VCALENDAR", name, summary)
		req, _ := http.NewRequest("PUT", "/dav/u/calendars/cal/"+name+".ics", bytes.NewReader([]byte(ical)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "text/calendar")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Less(t, resp.StatusCode, 300, "PUT %s", name)
	}

	sync := func(token, limitXML string) SyncMultiStatus {
		body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:"><D:sync-token>%s</D:sync-token><D:sync-level>1</D:sync-level>%s<D:prop><D:getetag/></D:prop></D:sync-collection>`, token, limitXML)
		req, _ := http.NewRequest("REPORT", "/dav/u/calendars/cal/", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/xml")
		resp, err := app.Test(req, davTestTimeout)
		require.NoError(t, err)
		require.Equal(t, 207, resp.StatusCode)
		var ms SyncMultiStatus
		require.NoError(t, xml.NewDecoder(resp.Body).Decode(&ms))
		return ms
	}

	resourceHrefs := func(ms SyncMultiStatus) []string {
		var out []string
		for _, r := range ms.Responses {
			if r.Status == truncatedStatus {
				continue
			}
			out = append(out, r.Href)
		}
		return out
	}

	// --- Defect (1): dedup ---
	put("item1", "v1")
	tokA := sync("", "").SyncToken // initial sync anchors a token after the create
	put("item1", "v2")
	put("item1", "v3")
	put("item1", "v4") // three modifications -> three change-log rows for item1

	deduped := sync(tokA, "")
	hrefs := resourceHrefs(deduped)
	require.Len(t, hrefs, 1, "a resource changed 3x must appear exactly once (RFC 6578 §3.8)")
	assert.Contains(t, hrefs[0], "item1.ics")
	tokB := deduped.SyncToken

	// --- Defect (2): limit + resumable token ---
	put("item2", "a")
	put("item3", "b") // two distinct resources changed since tokB

	limited := sync(tokB, `<D:limit><D:nresults>1</D:nresults></D:limit>`)
	limitedHrefs := resourceHrefs(limited)
	require.Len(t, limitedHrefs, 1, "limit nresults=1 must cap resource responses at 1 (RFC 6578 §3.7)")
	assert.Contains(t, limitedHrefs[0], "item2.ics", "first change should come first")

	// The truncation marker (§3.6) must be present.
	sawTruncation := false
	for _, r := range limited.Responses {
		if r.Status == truncatedStatus {
			sawTruncation = true
		}
	}
	assert.True(t, sawTruncation, "truncated response must include the 507 marker")

	// Replaying the returned token resumes exactly at the second change.
	resumed := sync(limited.SyncToken, "")
	resumedHrefs := resourceHrefs(resumed)
	require.Len(t, resumedHrefs, 1)
	assert.Contains(t, resumedHrefs[0], "item3.ics", "replaying the truncated token must yield the next change")
	for _, h := range resumedHrefs {
		assert.False(t, strings.Contains(h, "item2.ics"), "already-delivered change must not repeat")
	}
}
