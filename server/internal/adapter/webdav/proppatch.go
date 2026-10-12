package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

const appleICalNS = "http://apple.com/ns/ical/"

type proppatchReq struct {
	XMLName xml.Name `xml:"DAV: propertyupdate"`
	Set     []struct {
		Prop Prop `xml:"prop"`
	} `xml:"set"`
	Remove []struct {
		Prop Prop `xml:"prop"`
	} `xml:"remove"`
}

// rawInnerText returns the unescaped text content of a raw property value.
// RawXMLValue.Inner is the ESCAPED inner XML (e.g. "Team &amp; X"), so wrap it
// in a dummy element and let the decoder unescape entities before storing.
func rawInnerText(v RawXMLValue) string {
	var wrap struct {
		V string `xml:",chardata"`
	}
	if err := xml.Unmarshal([]byte("<x>"+string(v.Inner)+"</x>"), &wrap); err == nil {
		return strings.TrimSpace(wrap.V)
	}
	return strings.TrimSpace(string(v.Inner))
}

// proppatchProp is one property named by a PROPPATCH and the status it is
// reported with in the multistatus.
type proppatchProp struct {
	name   xml.Name
	status int
}

// handleCollectionProppatch applies DAV:displayname (rename) and, for calendars,
// Apple's calendar-color (recolor) sent by clients as PROPPATCH on the
// collection URL. Only the owner may modify (mirrors the REST path); sharees
// get 403.
//
// PROPPATCH is all-or-nothing (RFC 4918 §9.2): any other property, any remove
// (403) or an unacceptable value (409) fails the whole request — the rest are
// reported 424 Failed Dependency and nothing is written. sabre/dav does the
// same. A request that succeeds is persisted in one transaction together with
// its sync-token bump.
func (h *Handler) handleCollectionProppatch(c fiber.Ctx, ctx context.Context, reqPath, collectionType string) error {
	var req proppatchReq
	if err := xml.Unmarshal(c.Body(), &req); err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}

	// Resolve and authorise before looking at the properties, so a missing
	// collection is a 404 and a non-owner a 403 whatever the body names.
	var commit func(newName, newColor *string) error
	if collectionType == "calendars" {
		be := h.caldavHandler.Backend.(*CalDAVBackend)
		cal, u, _, err := be.ResolvePath(ctx, reqPath)
		if err != nil {
			return c.SendStatus(http.StatusNotFound)
		}
		// Ownership, not PermissionOwner: renaming or recolouring a
		// collection is the owner's prerogative, and ResolvePath caps a
		// subscribed calendar (story 100) at read-only, which would
		// otherwise stop its owner renaming their own subscription.
		if cal.UserID != u.ID {
			return c.SendStatus(http.StatusForbidden)
		}
		commit = func(newName, newColor *string) error {
			if newName != nil {
				cal.Name = *newName
			}
			if newColor != nil {
				cal.Color = *newColor
			}
			// UpdateMetadata writes the columns and advances the sync token
			// (a "collection" change-log row, as the REST rename does) in one
			// transaction, so polling clients notice the change and a failure
			// leaves nothing half-applied.
			return be.calendarRepo.UpdateMetadata(ctx, cal)
		}
	} else {
		be := h.carddavHandler.Backend.(*CardDAVBackend)
		u, ok := UserFromContext(ctx)
		if !ok {
			return c.SendStatus(http.StatusUnauthorized)
		}
		ab, perm, err := be.resolveAddressBook(ctx, u, reqPath)
		if err != nil {
			return c.SendStatus(http.StatusNotFound)
		}
		if perm != abPermOwner {
			return c.SendStatus(http.StatusForbidden)
		}
		commit = func(newName, _ *string) error {
			if newName != nil {
				ab.Name = *newName
			}
			return be.addressBookRepo.UpdateMetadata(ctx, ab)
		}
	}

	var newName, newColor *string
	var props []proppatchProp
	for _, set := range req.Set {
		for _, p := range set.Prop.Raw {
			status := http.StatusOK
			switch {
			case p.XMLName == (xml.Name{Space: "DAV:", Local: "displayname"}):
				v := rawInnerText(p)
				// Same rule as the REST rename: non-empty, at most 255 bytes.
				if calendar.ValidateName(v) != nil {
					status = http.StatusConflict
				} else {
					newName = &v
				}
			case p.XMLName == (xml.Name{Space: appleICalNS, Local: "calendar-color"}) && collectionType == "calendars":
				v := rawInnerText(p)
				// Apple sends #RRGGBBAA; the Color column is #RRGGBB (size:7).
				if len(v) == 9 && strings.HasPrefix(v, "#") {
					v = v[:7]
				}
				// An empty colour is acknowledged and leaves the colour as is.
				if v != "" {
					if calendar.ValidateHexColor(v) != nil {
						status = http.StatusConflict
					} else {
						newColor = &v
					}
				}
			default:
				status = http.StatusForbidden
			}
			props = append(props, proppatchProp{name: p.XMLName, status: status})
		}
	}
	for _, rm := range req.Remove {
		for _, p := range rm.Prop.Raw {
			props = append(props, proppatchProp{name: p.XMLName, status: http.StatusForbidden})
		}
	}

	failed := false
	for _, p := range props {
		if p.status != http.StatusOK {
			failed = true
		}
	}
	if failed {
		for i := range props {
			if props[i].status == http.StatusOK {
				props[i].status = http.StatusFailedDependency
			}
		}
	} else if newName != nil || newColor != nil {
		if err := commit(newName, newColor); err != nil {
			return err
		}
	}

	// RFC 4918 §9.2.1: prop elements in a PROPPATCH response must be empty.
	resp := SyncResponse{Href: reqPath}
	for _, code := range []int{http.StatusOK, http.StatusForbidden, http.StatusConflict, http.StatusFailedDependency} {
		var raw []RawXMLValue
		for _, p := range props {
			if p.status == code {
				raw = append(raw, RawXMLValue{XMLName: p.name})
			}
		}
		if len(raw) > 0 {
			resp.PropStat = append(resp.PropStat, PropStat{
				Prop:   Prop{Raw: raw},
				Status: fmt.Sprintf("HTTP/1.1 %d %s", code, http.StatusText(code)),
			})
		}
	}
	ms := &SyncMultiStatus{XMLName: xml.Name{Space: "DAV:", Local: "multistatus"}, Responses: []SyncResponse{resp}}
	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusMultiStatus)
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ms)
}
