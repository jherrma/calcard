package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
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

	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusMultiStatus)

	// Write XML header
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ms)
}

// buildAddressBookHref constructs the full href for an address object.
func buildAddressBookHref(path, resourcePath string) string {
	// Path is like /dav/username/addressbooks/abname/
	// ResourcePath is like contact.vcf
	path = strings.TrimSuffix(path, "/")
	return path + "/" + resourcePath
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
	return b.resolveAddressBook(ctx, u, path)
}
