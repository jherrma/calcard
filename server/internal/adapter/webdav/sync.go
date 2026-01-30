package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
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

	// Build MultiStatus response
	ms := &SyncMultiStatus{
		XMLName:   xml.Name{Space: "DAV:", Local: "multistatus"},
		SyncToken: newToken,
	}

	for _, change := range changes {
		resp := SyncResponse{
			Href: fmt.Sprintf("/dav/%s/calendars/%s/%s", getUsername(c.Path()), getCalPath(c.Path()), change.ResourcePath),
		}

		if change.ChangeType == "deleted" {
			resp.Status = "HTTP/1.1 404 Not Found"
		} else {
			// For created/modified, we need to fetch properties if requested
			// For now, let's at least return the ETag if available
			obj, err := backend.GetCalendarObjectByPath(ctx, change.CalendarID, change.ResourcePath)
			if err == nil {
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

	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusMultiStatus)

	// Write XML header
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ms)
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
	SyncToken string         `xml:"sync-token"`
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
