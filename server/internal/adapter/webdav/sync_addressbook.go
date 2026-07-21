package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"gorm.io/gorm"
)

// handleAddressBookSyncReport handles REPORT sync-collection for address books.
func (h *Handler) handleAddressBookSyncReport(c fiber.Ctx, ctx context.Context, query *SyncCollectionQuery) error {
	backend := h.carddavHandler.Backend.(*CardDAVBackend)

	changes, newToken, err := backend.GetSyncChanges(ctx, c.Path(), query.SyncToken)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// RFC 6578: Invalid sync-token returns 403 Forbidden with valid-sync-token error
			return h.sendSyncTokenError(c)
		}
		return err
	}

	// RFC 6578 §3.8: report each resource at most once (its latest state).
	changes = dedupeAddressBookChanges(changes)

	// RFC 6578 §3.7: honor <limit><nresults>, resuming from the last included
	// change's token. Skipped on initial sync (empty token). See the calendar
	// handler for the full rationale — these two paths are kept symmetric.
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
		resp := SyncResponse{
			Href: buildAddressBookHref(c.Path(), change.ResourcePath),
		}

		if change.ChangeType == "deleted" {
			resp.Status = "HTTP/1.1 404 Not Found"
		} else {
			// For created/modified, we need to fetch properties if requested
			obj, err := backend.GetAddressObjectByPath(ctx, change.AddressBookID, change.ResourcePath)
			if err == nil && obj != nil {
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

	// RFC 6578 §3.6: signal truncation with a 507 for the collection itself.
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

// dedupeAddressBookChanges keeps only the latest change-log row per resource
// path (mirror of dedupeCalendarChanges; see it for the full rationale).
func dedupeAddressBookChanges(changes []*addressbook.SyncChangeLog) []*addressbook.SyncChangeLog {
	seen := make(map[string]bool, len(changes))
	deduped := make([]*addressbook.SyncChangeLog, 0, len(changes))
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

// buildAddressBookHref constructs the full, percent-escaped href for an address
// object. ResourcePath may contain spaces or reserved characters; an unescaped
// href is invalid XML/URI (RFC 6578).
func buildAddressBookHref(path, resourcePath string) string {
	// Path is like /dav/username/addressbooks/abname/, ResourcePath like contact.vcf
	path = strings.TrimSuffix(path, "/")
	return (&url.URL{Path: path + "/" + resourcePath}).EscapedPath()
}

// getAddressBookPath extracts the address book path from a full path.
func getAddressBookPath(p string) string {
	// Path: /dav/username/addressbooks/abname/...
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// resolveAddressBookFromPath resolves an address book from the request path.
func (b *CardDAVBackend) resolveAddressBookFromPath(ctx context.Context, path string) (*addressbook.AddressBook, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("unauthorized")
	}
	ab, _, err := b.resolveAddressBook(ctx, u, path)
	return ab, err
}
