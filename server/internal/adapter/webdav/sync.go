package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"gorm.io/gorm"
)

func (h *Handler) handleSyncReport(c fiber.Ctx, ctx context.Context, query *SyncCollectionQuery) error {
	backend := h.caldavHandler.Backend.(*CalDAVBackend)

	changes, newToken, err := backend.GetSyncChanges(ctx, c.Path(), query.SyncToken)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// RFC 6578: Invalid sync-token returns 403 Forbidden with valid-sync-token error
			return h.sendSyncTokenError(c)
		}
		return err
	}

	// RFC 6578 §3.8: report each resource at most once (its latest state).
	// GetChangesSinceToken returns raw change-log rows ordered by id ASC, so a
	// resource changed N times since the client's token yields N rows.
	changes = dedupeCalendarChanges(changes)

	// RFC 6578 §3.7: never return more than the client's <limit><nresults>.
	// Every change row stores the token minted with it, so a truncated response
	// hands back the token of the last INCLUDED change; the client's next
	// request resumes exactly after it. Skip on initial sync (empty token): the
	// synthesized member list shares one final token, so no valid intermediate
	// token exists — and clients essentially never send limit on initial sync.
	truncated := false
	if query.Limit != nil && query.Limit.NResults > 0 && query.SyncToken != "" &&
		len(changes) > int(query.Limit.NResults) {
		changes = changes[:query.Limit.NResults]
		newToken = changes[len(changes)-1].SyncToken
		truncated = true
	}

	// Build MultiStatus response
	ms := &SyncMultiStatus{
		XMLName:   xml.Name{Space: "DAV:", Local: "multistatus"},
		SyncToken: newToken,
	}

	for _, change := range changes {
		rawHref := fmt.Sprintf("/dav/%s/calendars/%s/%s", getUsername(c.Path()), getCalPath(c.Path()), change.ResourcePath)
		resp := SyncResponse{
			Href: (&url.URL{Path: rawHref}).EscapedPath(),
		}

		if change.ChangeType == "deleted" {
			resp.Status = "HTTP/1.1 404 Not Found"
		} else {
			// For created/modified, we need to fetch properties if requested
			// For now, let's at least return the ETag if available
			obj, err := backend.GetCalendarObjectByPath(ctx, change.CalendarID, change.ResourcePath)
			if err == nil && obj != nil {
				// Format propstat based on requested props in query.Prop
				// Simplified: return ETag and Status OK
				resp.PropStat = []PropStat{
					{
						Prop: Prop{
							Raw: []RawXMLValue{
								{
									XMLName: xml.Name{Space: "DAV:", Local: "getetag"},
									Inner:   []byte(fmt.Sprintf("\"%s\"", obj.ETag)),
								},
							},
						},
						Status: "HTTP/1.1 200 OK",
					},
				}
			} else {
				resp.Status = "HTTP/1.1 404 Not Found"
			}
		}
		ms.Responses = append(ms.Responses, resp)
	}

	// RFC 6578 §3.6: signal truncation with a 507 response for the collection
	// itself, so the client knows to issue a follow-up sync with the new token.
	if truncated {
		ms.Responses = append(ms.Responses, SyncResponse{
			Href:   (&url.URL{Path: c.Path()}).EscapedPath(),
			Status: "HTTP/1.1 507 Insufficient Storage",
		})
	}

	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusMultiStatus)

	// Write XML header
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ms)
}

// dedupeCalendarChanges keeps only the latest change-log row per resource path.
// Rows arrive ordered by id ASC; iterate from the back keeping the first (i.e.
// latest) occurrence of each path, then restore ascending order so limit
// truncation and the last-included-token math stay correct. Dedupe by
// ResourcePath (not UID) so a delete+recreate at the same path collapses to the
// latest state.
func dedupeCalendarChanges(changes []*calendar.SyncChangeLog) []*calendar.SyncChangeLog {
	seen := make(map[string]bool, len(changes))
	deduped := make([]*calendar.SyncChangeLog, 0, len(changes))
	for i := len(changes) - 1; i >= 0; i-- {
		if seen[changes[i].ResourcePath] {
			continue
		}
		seen[changes[i].ResourcePath] = true
		deduped = append(deduped, changes[i])
	}
	for i, j := 0, len(deduped)-1; i < j; i, j = i+1, j-1 {
		deduped[i], deduped[j] = deduped[j], deduped[i]
	}
	return deduped
}

func (h *Handler) sendSyncTokenError(c fiber.Ctx) error {
	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusForbidden)

	type ErrorResponse struct {
		XMLName        xml.Name `xml:"DAV: error"`
		ValidSyncToken struct{} `xml:"DAV: valid-sync-token"`
	}

	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ErrorResponse{})
}

type SyncMultiStatus struct {
	XMLName   xml.Name       `xml:"DAV: multistatus"`
	Responses []SyncResponse `xml:"response"`
	// omitempty so PROPPATCH/collection-PROPFIND responses (which carry no
	// collection-level sync-token) don't emit a bogus empty <sync-token/>. The
	// sync REPORT always sets a non-empty token, so its output is unchanged.
	SyncToken string `xml:"sync-token,omitempty"`
}

func getUsername(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func getCalPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
