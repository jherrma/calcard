package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

const calendarServerNS = "http://calendarserver.org/ns/"

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

func xmlEscaped(s string) []byte {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.Bytes()
}

// handleCollectionPropfind answers a Depth: 0 PROPFIND on a calendar or address
// book collection, serving the sync-discovery properties emersion/go-webdav
// never emits (sync-token, calendarserver getctag, supported-report-set) plus
// displayname/resourcetype. Without these, mainstream clients never discover
// the sync-collection REPORT and fall back to full-collection PROPFIND polling.
func (h *Handler) handleCollectionPropfind(c fiber.Ctx, ctx context.Context, reqPath, collectionType string) error {
	var name, ctag, syncToken string
	var resourcetypeInner, reportSetInner string
	// extra carries properties only one collection type has (currently the
	// subscription source), merged into the known set below.
	extra := map[xml.Name]RawXMLValue{}
	// privileges is the DAV:current-user-privilege-set inner XML. It is what
	// tells a client a collection is read-only, which for a subscribed
	// calendar (story 100) is the difference between the client greying out
	// "new event" and the user discovering the restriction as a failed save.
	privileges := readWritePrivileges

	if collectionType == "calendars" {
		be := h.caldavHandler.Backend.(*CalDAVBackend)
		cal, _, perm, err := be.ResolvePath(ctx, reqPath)
		if err != nil {
			return c.SendStatus(http.StatusNotFound)
		}
		name, ctag, syncToken = cal.Name, cal.CTag, cal.SyncToken
		if perm != calendar.PermissionOwner && perm != calendar.PermissionReadWrite {
			privileges = readOnlyPrivileges
		}
		if cal.Subscribed {
			// CalendarServer's "source" property: the feed a subscribed
			// collection mirrors. Note the resourcetype deliberately stays
			// <C:calendar/> rather than becoming <CS:subscribed/> — a client
			// that does not know the subscribed type would stop treating the
			// collection as a calendar and hide its events entirely, which is
			// a far worse failure than not advertising the distinction.
			if src := be.SubscriptionURL(ctx, cal.ID); src != "" {
				extra[xml.Name{Space: calendarServerNS, Local: "source"}] = RawXMLValue{
					XMLName: xml.Name{Space: calendarServerNS, Local: "source"},
					Inner:   append([]byte(`<href xmlns="DAV:">`), append(xmlEscaped(src), []byte(`</href>`)...)...),
				}
			}
		}
		resourcetypeInner = `<collection xmlns="DAV:"/><calendar xmlns="urn:ietf:params:xml:ns:caldav"/>`
		reportSetInner = `<supported-report xmlns="DAV:"><report><sync-collection/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><calendar-multiget xmlns="urn:ietf:params:xml:ns:caldav"/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><calendar-query xmlns="urn:ietf:params:xml:ns:caldav"/></report></supported-report>`
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
		name, ctag, syncToken = ab.Name, ab.CTag, ab.SyncToken
		if perm != abPermOwner && perm != abPermReadWrite {
			privileges = readOnlyPrivileges
		}
		resourcetypeInner = `<collection xmlns="DAV:"/><addressbook xmlns="urn:ietf:params:xml:ns:carddav"/>`
		reportSetInner = `<supported-report xmlns="DAV:"><report><sync-collection/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><addressbook-multiget xmlns="urn:ietf:params:xml:ns:carddav"/></report></supported-report>` +
			`<supported-report xmlns="DAV:"><report><addressbook-query xmlns="urn:ietf:params:xml:ns:carddav"/></report></supported-report>`
	}

	// The properties this handler can answer, keyed by namespace+local name.
	known := map[xml.Name]RawXMLValue{
		{Space: "DAV:", Local: "resourcetype"}:         {XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Inner: []byte(resourcetypeInner)},
		{Space: "DAV:", Local: "displayname"}:          {XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Inner: xmlEscaped(name)},
		{Space: "DAV:", Local: "sync-token"}:           {XMLName: xml.Name{Space: "DAV:", Local: "sync-token"}, Inner: xmlEscaped(syncToken)},
		{Space: "DAV:", Local: "supported-report-set"}: {XMLName: xml.Name{Space: "DAV:", Local: "supported-report-set"}, Inner: []byte(reportSetInner)},
		{Space: calendarServerNS, Local: "getctag"}:    {XMLName: xml.Name{Space: calendarServerNS, Local: "getctag"}, Inner: xmlEscaped(ctag)},
		{Space: "DAV:", Local: "current-user-privilege-set"}: {
			XMLName: xml.Name{Space: "DAV:", Local: "current-user-privilege-set"},
			Inner:   []byte(privileges),
		},
	}
	for n, v := range extra {
		known[n] = v
	}

	// Which props were requested? Match by namespace URI + local name — Go's
	// decoder resolves XML prefixes to URIs, so <A:getctag xmlns:A="...ns/">
	// arrives as {Space: "http://calendarserver.org/ns/", Local: "getctag"}.
	var req propfindReq
	var requested []xml.Name
	if err := xml.Unmarshal(c.Body(), &req); err == nil && req.Prop != nil {
		for _, r := range req.Prop.Raw {
			requested = append(requested, r.XMLName)
		}
	} else {
		// Empty body / allprop / propname: answer with all live props (allprop
		// clients tolerate extras; propname is rare enough to treat the same).
		for n := range known {
			requested = append(requested, n)
		}
	}

	var found, missing []RawXMLValue
	for _, n := range requested {
		if v, ok := known[n]; ok {
			found = append(found, v)
		} else {
			missing = append(missing, RawXMLValue{XMLName: n})
		}
	}

	resp := SyncResponse{Href: reqPath}
	if len(found) > 0 {
		resp.PropStat = append(resp.PropStat, PropStat{Prop: Prop{Raw: found}, Status: "HTTP/1.1 200 OK"})
	}
	if len(missing) > 0 {
		resp.PropStat = append(resp.PropStat, PropStat{Prop: Prop{Raw: missing}, Status: "HTTP/1.1 404 Not Found"})
	}

	ms := &SyncMultiStatus{
		XMLName:   xml.Name{Space: "DAV:", Local: "multistatus"},
		Responses: []SyncResponse{resp},
	}
	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusMultiStatus)
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(ms)
}
