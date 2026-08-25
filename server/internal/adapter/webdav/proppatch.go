package webdav

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
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

// handleCollectionProppatch applies DAV:displayname (rename) and, for calendars,
// Apple's calendar-color (recolor) sent by clients as PROPPATCH on the
// collection URL. RFC 4918 says PROPPATCH is all-or-nothing; like sabre/dav we
// apply supported props and 403 the rest, which is what real clients tolerate.
// Only the owner may modify (mirrors the REST path); sharees get 403.
func (h *Handler) handleCollectionProppatch(c fiber.Ctx, ctx context.Context, reqPath, collectionType string) error {
	var req proppatchReq
	if err := xml.Unmarshal(c.Body(), &req); err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}

	var newName, newColor *string
	var applied, rejected []RawXMLValue
	for _, set := range req.Set {
		for _, p := range set.Prop.Raw {
			switch {
			case p.XMLName == (xml.Name{Space: "DAV:", Local: "displayname"}):
				v := rawInnerText(p)
				newName = &v
				applied = append(applied, RawXMLValue{XMLName: p.XMLName})
			case p.XMLName == (xml.Name{Space: appleICalNS, Local: "calendar-color"}) && collectionType == "calendars":
				v := rawInnerText(p)
				// Apple sends #RRGGBBAA; the Color column is #RRGGBB (size:7).
				if len(v) == 9 && strings.HasPrefix(v, "#") {
					v = v[:7]
				}
				newColor = &v
				applied = append(applied, RawXMLValue{XMLName: p.XMLName})
			default:
				rejected = append(rejected, RawXMLValue{XMLName: p.XMLName})
			}
		}
	}
	for _, rm := range req.Remove {
		for _, p := range rm.Prop.Raw {
			rejected = append(rejected, RawXMLValue{XMLName: p.XMLName})
		}
	}

	if newName != nil || newColor != nil {
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
			if newName != nil {
				cal.Name = *newName
			}
			if newColor != nil && *newColor != "" {
				cal.Color = *newColor
			}
			if err := be.calendarRepo.Update(ctx, cal); err != nil {
				return err
			}
			// Advance the sync token / write a "collection" change-log row (as the
			// REST rename does) so polling clients notice the change.
			if err := be.calendarRepo.RecordChange(ctx, cal.ID, cal.Path, "", "collection"); err != nil {
				return err
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
			if newName != nil {
				ab.Name = *newName
			}
			if err := be.addressBookRepo.Update(ctx, ab); err != nil {
				return err
			}
			if err := be.addressBookRepo.RecordChange(ctx, ab.ID, ab.Path, "", "collection"); err != nil {
				return err
			}
		}
	}

	// RFC 4918 §9.2.1: prop elements in a PROPPATCH response must be empty.
	resp := SyncResponse{Href: reqPath}
	if len(applied) > 0 {
		resp.PropStat = append(resp.PropStat, PropStat{Prop: Prop{Raw: applied}, Status: "HTTP/1.1 200 OK"})
	}
	if len(rejected) > 0 {
		resp.PropStat = append(resp.PropStat, PropStat{Prop: Prop{Raw: rejected}, Status: "HTTP/1.1 403 Forbidden"})
	}
	ms := &SyncMultiStatus{XMLName: xml.Name{Space: "DAV:", Local: "multistatus"}, Responses: []SyncResponse{resp}}
	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusMultiStatus)
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ms)
}
