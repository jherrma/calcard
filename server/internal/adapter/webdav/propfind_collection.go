package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

const calendarServerNS = "http://calendarserver.org/ns/"

// The CalDAV and CardDAV namespaces, for the properties built by hand below.
const (
	calDAVNS  = "urn:ietf:params:xml:ns:caldav"
	cardDAVNS = "urn:ietf:params:xml:ns:carddav"
)

// The two DAV:current-user-privilege-set bodies this handler reports. Only the
// privileges the server actually implements are listed: advertising
// write-content on a collection that refuses PUT would be worse than silence.
const (
	readOnlyPrivileges = `<privilege xmlns="DAV:"><read/></privilege>` +
		`<privilege xmlns="DAV:"><read-current-user-privilege-set/></privilege>`
	readWritePrivileges = readOnlyPrivileges +
		`<privilege xmlns="DAV:"><write/></privilege>` +
		`<privilege xmlns="DAV:"><write-content/></privilege>` +
		`<privilege xmlns="DAV:"><bind/></privilege>` +
		`<privilege xmlns="DAV:"><unbind/></privilege>`
)

// propfindReq is the subset of a PROPFIND body we need: which named properties
// were requested (or allprop/propname).
type propfindReq struct {
	XMLName  xml.Name  `xml:"DAV: propfind"`
	Prop     *Prop     `xml:"prop"`
	AllProp  *struct{} `xml:"allprop"`
	PropName *struct{} `xml:"propname"`
}

// propfindRequest is what a PROPFIND body asks for: named properties, every
// property (allprop, also an empty or unreadable body), or only their names.
type propfindRequest struct {
	names     []xml.Name
	all       bool
	namesOnly bool
}

// parsePropfindRequest matches requested props by namespace URI + local name —
// Go's decoder resolves XML prefixes to URIs, so <A:getctag xmlns:A="...ns/">
// arrives as {Space: "http://calendarserver.org/ns/", Local: "getctag"}.
func parsePropfindRequest(body []byte) propfindRequest {
	var req propfindReq
	if err := xml.Unmarshal(body, &req); err != nil {
		// Empty body / unparseable: answer like allprop (RFC 4918 §9.1).
		return propfindRequest{all: true}
	}
	if req.Prop != nil {
		var names []xml.Name
		for _, r := range req.Prop.Raw {
			names = append(names, r.XMLName)
		}
		return propfindRequest{names: names}
	}
	if req.PropName != nil {
		return propfindRequest{all: true, namesOnly: true}
	}
	return propfindRequest{all: true}
}

func xmlEscaped(s string) []byte {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.Bytes()
}

// hrefInner is the inner XML of a property whose value is one DAV:href.
func hrefInner(target string) []byte {
	return append([]byte(`<href xmlns="DAV:">`), append(xmlEscaped(target), []byte(`</href>`)...)...)
}

// collectionProps returns every property this server reports for the calendar
// or address book collection at href, keyed by namespace + local name.
//
// It is the ONE description of a collection: the Depth: 0 handler answers from
// it, and rewriteCollectionResponses puts it in place of every collection
// emersion lists in a Depth: 1 / infinity answer (F07). Before, the two Depths
// had separate property lists, so a client saw sync-token/getctag only at
// Depth 0, calendar-description/supported-calendar-component-set only at
// Depth 1, and calendar-color (stored by PROPPATCH) at neither. A property
// added anywhere else reintroduces that split; add it here.
func (h *Handler) collectionProps(ctx context.Context, href, collectionType string) (map[xml.Name]RawXMLValue, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, errors.New("webdav: no authenticated user")
	}

	props := map[xml.Name]RawXMLValue{}
	set := func(space, local string, inner []byte) {
		n := xml.Name{Space: space, Local: local}
		props[n] = RawXMLValue{XMLName: n, Inner: inner}
	}
	var name, ctag, syncToken, resourcetypeInner, reportSetInner string
	// privileges is the DAV:current-user-privilege-set inner XML. It is what
	// tells a client a collection is read-only, which for a subscribed
	// calendar (story 100) is the difference between the client greying out
	// "new event" and the user discovering the restriction as a failed save.
	privileges := readWritePrivileges

	if collectionType == "calendars" {
		be := h.caldavHandler.Backend.(*CalDAVBackend)
		cal, _, perm, err := be.ResolvePath(ctx, href)
		if err != nil {
			return nil, err
		}
		name, ctag, syncToken = cal.Name, cal.CTag, cal.SyncToken
		if perm != calendar.PermissionOwner && perm != calendar.PermissionReadWrite {
			privileges = readOnlyPrivileges
		}
		// Describe the calendar the way the home listing (ListCalendars) does:
		// by ownership, not by the permission ResolvePath caps for a
		// subscription, or an owned subscription would be "My Calendar" at one
		// Depth and "Shared Calendar" at the other.
		mapPerm := perm
		if cal.UserID == u.ID {
			mapPerm = calendar.PermissionOwner
		}
		mapped := be.mapCalendar(u.Username, cal, mapPerm)
		set(calDAVNS, "calendar-description", xmlEscaped(mapped.Description))
		var comps bytes.Buffer
		for _, comp := range mapped.SupportedComponentSet {
			comps.WriteString(`<comp xmlns="` + calDAVNS + `" name="`)
			comps.Write(xmlEscaped(comp))
			comps.WriteString(`"/>`)
		}
		set(calDAVNS, "supported-calendar-component-set", comps.Bytes())
		set(calDAVNS, "supported-calendar-data",
			[]byte(`<calendar-data xmlns="`+calDAVNS+`" content-type="text/calendar" version="2.0"/>`))
		// Apple's calendar-color: what clients recolour a calendar with
		// (PROPPATCH, see proppatch.go) and read the colour back from. Stored
		// as #RRGGBB; a calendar without one leaves it in the 404 propstat.
		if cal.Color != "" {
			set(appleICalNS, "calendar-color", xmlEscaped(cal.Color))
		}
		if cal.Subscribed {
			// CalendarServer's "source" property: the feed a subscribed
			// collection mirrors. Note the resourcetype deliberately stays
			// <C:calendar/> rather than becoming <CS:subscribed/> — a client
			// that does not know the subscribed type would stop treating the
			// collection as a calendar and hide its events entirely, which is
			// a far worse failure than not advertising the distinction.
			if src := be.SubscriptionURL(ctx, cal.ID); src != "" {
				set(calendarServerNS, "source", hrefInner(src))
			}
		}
		resourcetypeInner = `<collection xmlns="DAV:"/><calendar xmlns="urn:ietf:params:xml:ns:caldav"/>`
		reportSetInner = `<supported-report xmlns="DAV:"><report><sync-collection/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><calendar-multiget xmlns="urn:ietf:params:xml:ns:caldav"/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><calendar-query xmlns="urn:ietf:params:xml:ns:caldav"/></report></supported-report>`
	} else {
		be := h.carddavHandler.Backend.(*CardDAVBackend)
		ab, perm, err := be.resolveAddressBook(ctx, u, href)
		if err != nil {
			return nil, err
		}
		name, ctag, syncToken = ab.Name, ab.CTag, ab.SyncToken
		if perm != abPermOwner && perm != abPermReadWrite {
			privileges = readOnlyPrivileges
		}
		mapped := be.mapAddressBook(u.Username, ab, perm != abPermOwner)
		if mapped.Description != "" {
			set(cardDAVNS, "addressbook-description", xmlEscaped(mapped.Description))
		}
		var types bytes.Buffer
		for _, t := range mapped.SupportedAddressData {
			types.WriteString(`<address-data-type xmlns="` + cardDAVNS + `" content-type="`)
			types.Write(xmlEscaped(t.ContentType))
			types.WriteString(`" version="`)
			types.Write(xmlEscaped(t.Version))
			types.WriteString(`"/>`)
		}
		set(cardDAVNS, "supported-address-data", types.Bytes())
		if mapped.MaxResourceSize > 0 {
			set(cardDAVNS, "max-resource-size", []byte(strconv.FormatInt(mapped.MaxResourceSize, 10)))
		}
		resourcetypeInner = `<collection xmlns="DAV:"/><addressbook xmlns="urn:ietf:params:xml:ns:carddav"/>`
		reportSetInner = `<supported-report xmlns="DAV:"><report><sync-collection/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><addressbook-multiget xmlns="urn:ietf:params:xml:ns:carddav"/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><addressbook-query xmlns="urn:ietf:params:xml:ns:carddav"/></report></supported-report>`
	}

	set("DAV:", "resourcetype", []byte(resourcetypeInner))
	set("DAV:", "displayname", xmlEscaped(name))
	set("DAV:", "sync-token", xmlEscaped(syncToken))
	set("DAV:", "supported-report-set", []byte(reportSetInner))
	set(calendarServerNS, "getctag", xmlEscaped(ctag))
	set("DAV:", "current-user-privilege-set", []byte(privileges))
	set("DAV:", "current-user-principal", hrefInner(fmt.Sprintf("/dav/%s/", u.Username)))
	return props, nil
}

// collectionResponse answers req for one collection from its known
// properties: the found ones in a 200 propstat, the rest in a 404 propstat.
func collectionResponse(href string, known map[xml.Name]RawXMLValue, req propfindRequest) SyncResponse {
	requested := req.names
	if req.all {
		// allprop (also an empty body) answers with every known property:
		// clients tolerate extras. Sorted so the answer is stable.
		requested = make([]xml.Name, 0, len(known))
		for n := range known {
			requested = append(requested, n)
		}
		sort.Slice(requested, func(i, j int) bool {
			if requested[i].Space != requested[j].Space {
				return requested[i].Space < requested[j].Space
			}
			return requested[i].Local < requested[j].Local
		})
	}

	var found, missing []RawXMLValue
	for _, n := range requested {
		v, ok := known[n]
		switch {
		case !ok:
			missing = append(missing, RawXMLValue{XMLName: n})
		case req.namesOnly:
			// propname: the names without their values (RFC 4918 §9.1).
			found = append(found, RawXMLValue{XMLName: n})
		default:
			found = append(found, v)
		}
	}

	resp := SyncResponse{Href: href}
	if len(found) > 0 {
		resp.PropStat = append(resp.PropStat, PropStat{Prop: Prop{Raw: found}, Status: "HTTP/1.1 200 OK"})
	}
	if len(missing) > 0 {
		resp.PropStat = append(resp.PropStat, PropStat{Prop: Prop{Raw: missing}, Status: "HTTP/1.1 404 Not Found"})
	}
	return resp
}

// handleCollectionPropfind answers a Depth: 0 PROPFIND on a calendar or address
// book collection from collectionProps, which serves the sync-discovery
// properties emersion/go-webdav never emits (sync-token, calendarserver
// getctag, supported-report-set) next to the CalDAV/CardDAV ones it does.
// Without these, mainstream clients never discover the sync-collection REPORT
// and fall back to full-collection PROPFIND polling.
func (h *Handler) handleCollectionPropfind(c fiber.Ctx, ctx context.Context, reqPath, collectionType string) error {
	if _, ok := UserFromContext(ctx); !ok {
		return c.SendStatus(http.StatusUnauthorized)
	}
	known, err := h.collectionProps(ctx, reqPath, collectionType)
	if err != nil {
		return c.SendStatus(http.StatusNotFound)
	}

	ms := &SyncMultiStatus{
		XMLName:   xml.Name{Space: "DAV:", Local: "multistatus"},
		Responses: []SyncResponse{collectionResponse(reqPath, known, parsePropfindRequest(c.Body()))},
	}
	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusMultiStatus)
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ms)
}

// rewriteCollectionResponses replaces, in the multistatus emersion produced for
// a Depth: 1 / infinity PROPFIND, every <response> describing a calendar or
// address book collection with the one built from collectionProps, so a
// collection reports the same properties whatever the Depth (F07).
//
// It splices bytes rather than re-encoding the document: every other response
// (the home, the objects with their calendar-data) stays byte for byte what
// emersion wrote. A response whose href does not resolve to a collection is
// kept as is. Reports whether anything was replaced; on a parse error the body
// comes back unchanged.
func (h *Handler) rewriteCollectionResponses(ctx context.Context, body []byte, collectionType string, req propfindRequest) ([]byte, bool) {
	davResponse := xml.Name{Space: "DAV:", Local: "response"}
	dec := xml.NewDecoder(bytes.NewReader(body))
	var out bytes.Buffer
	last, changed := 0, false
	for {
		start := int(dec.InputOffset())
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return body, false
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name != davResponse {
			continue
		}
		var r struct {
			Hrefs []string `xml:"DAV: href"`
		}
		if err := dec.DecodeElement(&r, &se); err != nil {
			return body, false
		}
		end := int(dec.InputOffset())
		if len(r.Hrefs) != 1 {
			continue
		}
		href := strings.TrimSpace(r.Hrefs[0])
		u, err := url.Parse(href)
		if err != nil {
			continue
		}
		if len(strings.Split(strings.Trim(u.Path, "/"), "/")) != 4 || davCollectionType(u.Path) != collectionType {
			continue
		}
		known, err := h.collectionProps(ctx, u.Path, collectionType)
		if err != nil {
			continue
		}
		repl, err := xml.Marshal(collectionResponse(href, known, req))
		if err != nil {
			return body, false
		}
		out.Write(body[last:start])
		out.Write(repl)
		last, changed = end, true
	}
	if !changed {
		return body, false
	}
	out.Write(body[last:])
	return out.Bytes(), true
}
