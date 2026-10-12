package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/emersion/go-ical"
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

const (
	mkcolDAVNS     = "DAV:"
	mkcolCalDAVNS  = "urn:ietf:params:xml:ns:caldav"
	mkcolCardDAVNS = "urn:ietf:params:xml:ns:carddav"
)

// mkcolBody is a MKCALENDAR (RFC 4791 §5.3.1) or extended MKCOL (RFC 5689)
// request body. Decoding the whole document at once, rather than re-parsing a
// property's inner XML later, keeps every child element's namespace resolved.
type mkcolBody struct {
	XMLName xml.Name
	Set     []struct {
		Prop struct {
			Props []mkcolProp `xml:",any"`
		} `xml:"DAV: prop"`
	} `xml:"DAV: set"`
}

type mkcolProp struct {
	XMLName  xml.Name
	Text     string `xml:",chardata"`
	Children []struct {
		XMLName xml.Name
		Name    string `xml:"name,attr"`
	} `xml:",any"`
}

// mkcolRefusal is a property the server cannot set, with the precondition (if
// any) that names the reason.
type mkcolRefusal struct {
	prop         xml.Name
	precondition xml.Name
}

// mkcolCalendarProps are the validated values a MKCALENDAR / extended MKCOL
// sets on the new calendar. CreateCalendar picks them up from the context,
// because go-webdav's Calendar carries neither colour nor timezone.
type mkcolCalendarProps struct {
	description, color, timezone, components *string
}

type mkcolPropsKey struct{}

func mkcolCalendarPropsFrom(ctx context.Context) (*mkcolCalendarProps, bool) {
	p, ok := ctx.Value(mkcolPropsKey{}).(*mkcolCalendarProps)
	return p, ok
}

// applyTo copies the requested values over the defaults of a new calendar.
func (p *mkcolCalendarProps) applyTo(c *calendar.Calendar) {
	if p.description != nil {
		c.Description = *p.description
	}
	if p.color != nil {
		c.Color = *p.color
	}
	if p.timezone != nil {
		c.Timezone = *p.timezone
	}
	if p.components != nil {
		c.SupportedComponents = *p.components
	}
}

// calendarComponentSet is the supported-calendar-component-set reported for a
// stored calendar. Rows created before MKCALENDAR could narrow it all hold
// "VEVENT,VTODO", so their report is unchanged.
func calendarComponentSet(stored string) []string {
	var set []string
	for _, comp := range strings.Split(stored, ",") {
		if comp = strings.TrimSpace(comp); comp != "" {
			set = append(set, comp)
		}
	}
	if len(set) == 0 {
		return []string{"VEVENT", "VTODO"}
	}
	return set
}

// prepareCollectionMkcol validates the properties of a MKCALENDAR, or of an
// extended MKCOL on a calendar or address book path, before anything is
// created. go-webdav reads only displayname (and addressbook-description) from
// such a body and silently ignores everything else, answering 201 as if it had
// been applied. RFC 4791 §5.3.1 and RFC 5689 §3 require all-or-nothing: either
// every property is set, or the collection is not created at all.
//
// On success it returns the context the backend must see (carrying the values
// go-webdav cannot pass on) and handled=false; the request then continues to
// go-webdav, with a MKCALENDAR rewritten into the extended-MKCOL shape it
// parses. Otherwise the response has been written and handled is true.
func (h *Handler) prepareCollectionMkcol(c fiber.Ctx, ctx context.Context, reqPath string) (context.Context, bool, error) {
	isMkcalendar := c.Method() == "MKCALENDAR"
	collectionType := davCollectionType(reqPath)
	if isMkcalendar {
		// A MKCALENDAR always asks for a calendar; where it may be created is
		// go-webdav's decision, as before.
		collectionType = "calendars"
	}
	body := c.Body()
	empty := len(bytes.TrimSpace(body)) == 0
	if !isMkcalendar && (empty || collectionType == "") {
		return ctx, false, nil
	}

	var req mkcolBody
	var name *string
	var calProps mkcolCalendarProps
	var all []xml.Name
	var refused []mkcolRefusal
	if !empty {
		wantRoot := "mkcol"
		if isMkcalendar {
			wantRoot = "mkcalendar"
		}
		if err := xml.Unmarshal(body, &req); err != nil || req.XMLName.Local != wantRoot {
			return ctx, true, c.SendStatus(http.StatusBadRequest)
		}
		// RFC 5689 §3: instructions are processed in document order, so a
		// property set twice keeps its last value.
		for _, set := range req.Set {
			for _, p := range set.Prop.Props {
				all = append(all, p.XMLName)
				var ok bool
				var precondition xml.Name
				if collectionType == "calendars" {
					ok, precondition = mkcolCalendarProp(p, &name, &calProps)
				} else {
					ok, precondition = mkcolAddressBookProp(p, &name)
				}
				if !ok {
					refused = append(refused, mkcolRefusal{prop: p.XMLName, precondition: precondition})
				}
			}
		}
	}

	if len(refused) > 0 {
		return ctx, true, writeMkcolFailure(c, reqPath, isMkcalendar, all, refused)
	}

	if isMkcalendar {
		// go-webdav only dispatches MKCOL; the calendar backend treats both
		// identically (CreateCalendar). Hand it the extended-MKCOL body it
		// parses: resourcetype (it rejects a body without collection+calendar)
		// plus the displayname. The other values travel in the context.
		var buf bytes.Buffer
		buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
		buf.WriteString(`<mkcol xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><set><prop>`)
		buf.WriteString(`<resourcetype><collection/><C:calendar/></resourcetype>`)
		if name != nil && *name != "" {
			buf.WriteString(`<displayname>`)
			_ = xml.EscapeText(&buf, []byte(*name))
			buf.WriteString(`</displayname>`)
		}
		buf.WriteString(`</prop></set></mkcol>`)

		c.Request().Header.SetMethod("MKCOL")
		c.Request().SetBody(buf.Bytes())
		c.Request().Header.SetContentLength(buf.Len())
		c.Request().Header.SetContentType("application/xml; charset=utf-8")
	}
	if collectionType == "calendars" {
		ctx = context.WithValue(ctx, mkcolPropsKey{}, &calProps)
	}
	return ctx, false, nil
}

// mkcolCalendarProp records one calendar property, or reports why it cannot
// be set.
func mkcolCalendarProp(p mkcolProp, name **string, props *mkcolCalendarProps) (bool, xml.Name) {
	text := strings.TrimSpace(p.Text)
	switch p.XMLName {
	case xml.Name{Space: mkcolDAVNS, Local: "resourcetype"}:
		if !mkcolHasChildren(p, xml.Name{Space: mkcolDAVNS, Local: "collection"}, xml.Name{Space: mkcolCalDAVNS, Local: "calendar"}) {
			return false, xml.Name{Space: mkcolDAVNS, Local: "valid-resourcetype"}
		}
	case xml.Name{Space: mkcolDAVNS, Local: "displayname"}:
		if len(text) > 255 {
			return false, xml.Name{}
		}
		*name = &text
	case xml.Name{Space: mkcolCalDAVNS, Local: "calendar-description"}:
		if calendar.ValidateDescription(text) != nil {
			return false, xml.Name{}
		}
		props.description = &text
	case xml.Name{Space: appleICalNS, Local: "calendar-color"}:
		// Apple and DAVx5 send #RRGGBBAA; the Color column is #RRGGBB.
		if len(text) == 9 && strings.HasPrefix(text, "#") {
			text = text[:7]
		}
		if calendar.ValidateHexColor(text) != nil {
			return false, xml.Name{}
		}
		props.color = &text
	case xml.Name{Space: mkcolCalDAVNS, Local: "calendar-timezone"}:
		tzid, valid := mkcolVTimezoneID(text)
		if !valid {
			return false, xml.Name{Space: mkcolCalDAVNS, Local: "valid-calendar-data"}
		}
		tz, ok := mkcolResolveTimezone(tzid)
		if !ok {
			return false, xml.Name{}
		}
		props.timezone = &tz
	case xml.Name{Space: mkcolCalDAVNS, Local: "calendar-timezone-id"}:
		// RFC 7809 §5.2: the IANA name alone, without a VTIMEZONE.
		tz, ok := mkcolResolveTimezone(text)
		if !ok {
			return false, xml.Name{}
		}
		props.timezone = &tz
	case xml.Name{Space: mkcolCalDAVNS, Local: "supported-calendar-component-set"}:
		comps, ok := mkcolComponentSet(p)
		if !ok {
			return false, xml.Name{Space: mkcolCalDAVNS, Local: "supported-calendar-component"}
		}
		props.components = &comps
	case xml.Name{Space: appleICalNS, Local: "calendar-order"}:
		// Accepted, not stored: Apple's client-local sort position, which
		// iOS and macOS put in every MKCALENDAR. There is nothing to store it
		// in, and refusing it would stop Apple Calendar creating calendars.
	case xml.Name{Space: mkcolCalDAVNS, Local: "schedule-calendar-transp"}:
		// RFC 6638 §9.1: opaque is the default and the only behaviour a
		// server without scheduling has, so only opaque can be honoured.
		if !mkcolHasChildren(p, xml.Name{Space: mkcolCalDAVNS, Local: "opaque"}) {
			return false, xml.Name{}
		}
	default:
		return false, xml.Name{}
	}
	return true, xml.Name{}
}

// mkcolAddressBookProp records one address book property. go-webdav stores
// displayname and addressbook-description itself; only validation is needed.
func mkcolAddressBookProp(p mkcolProp, name **string) (bool, xml.Name) {
	text := strings.TrimSpace(p.Text)
	switch p.XMLName {
	case xml.Name{Space: mkcolDAVNS, Local: "resourcetype"}:
		if !mkcolHasChildren(p, xml.Name{Space: mkcolDAVNS, Local: "collection"}, xml.Name{Space: mkcolCardDAVNS, Local: "addressbook"}) {
			return false, xml.Name{Space: mkcolDAVNS, Local: "valid-resourcetype"}
		}
	case xml.Name{Space: mkcolDAVNS, Local: "displayname"}:
		if len(text) > 255 {
			return false, xml.Name{}
		}
		*name = &text
	case xml.Name{Space: mkcolCardDAVNS, Local: "addressbook-description"}:
		if len(text) > 1000 {
			return false, xml.Name{}
		}
	default:
		return false, xml.Name{}
	}
	return true, xml.Name{}
}

func mkcolHasChildren(p mkcolProp, want ...xml.Name) bool {
	for _, w := range want {
		found := false
		for _, child := range p.Children {
			if child.XMLName == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// mkcolComponentSet accepts a supported-calendar-component-set that is a
// non-empty subset of what calendar objects on this server may contain, and
// returns it in the stored "VEVENT,VTODO" form.
func mkcolComponentSet(p mkcolProp) (string, bool) {
	var hasEvent, hasTodo bool
	for _, child := range p.Children {
		if child.XMLName != (xml.Name{Space: mkcolCalDAVNS, Local: "comp"}) {
			return "", false
		}
		switch strings.ToUpper(child.Name) {
		case "VEVENT":
			hasEvent = true
		case "VTODO":
			hasTodo = true
		default:
			return "", false
		}
	}
	switch {
	case hasEvent && hasTodo:
		return "VEVENT,VTODO", true
	case hasEvent:
		return "VEVENT", true
	case hasTodo:
		return "VTODO", true
	}
	return "", false
}

// mkcolVTimezoneID returns the TZID of a calendar-timezone value, which RFC
// 4791 §5.3.1 requires to be an iCalendar object with exactly one VTIMEZONE.
func mkcolVTimezoneID(text string) (string, bool) {
	cal, err := ical.NewDecoder(strings.NewReader(text)).Decode()
	if err != nil || cal == nil {
		return "", false
	}
	var tzid string
	count := 0
	for _, comp := range cal.Children {
		if comp.Name == ical.CompTimezone {
			count++
			tzid, _ = comp.Props.Text(ical.PropTimezoneID)
		}
	}
	if count != 1 || tzid == "" {
		return "", false
	}
	return tzid, true
}

// mkcolResolveTimezone maps a TZID onto the IANA name the Timezone column
// holds. libical-based clients prefix their zones
// ("/freeassociation.sourceforge.net/Europe/Berlin"), so leading segments are
// dropped until a known zone remains.
func mkcolResolveTimezone(tzid string) (string, bool) {
	s := strings.TrimSpace(tzid)
	for s != "" {
		if s != "Local" && len(s) <= 50 && calendar.ValidateTimezone(s) == nil {
			return s, true
		}
		i := strings.Index(s, "/")
		if i < 0 {
			break
		}
		s = s[i+1:]
	}
	return "", false
}

type mkcolPropStat struct {
	XMLName xml.Name    `xml:"DAV: propstat"`
	Prop    Prop        `xml:"DAV: prop"`
	Status  string      `xml:"DAV: status"`
	Error   *mkcolError `xml:"DAV: error,omitempty"`
}

type mkcolError struct {
	Raw []RawXMLValue `xml:",any"`
}

// mkcolResponse is RFC 5689's DAV:mkcol-response.
type mkcolResponse struct {
	XMLName  xml.Name        `xml:"DAV: mkcol-response"`
	PropStat []mkcolPropStat `xml:"DAV: propstat"`
}

type mkcolMultiStatus struct {
	XMLName  xml.Name            `xml:"DAV: multistatus"`
	Response mkcolStatusResponse `xml:"DAV: response"`
}

type mkcolStatusResponse struct {
	Href     string          `xml:"DAV: href"`
	PropStat []mkcolPropStat `xml:"DAV: propstat"`
}

// writeMkcolFailure reports a refused MKCALENDAR / extended MKCOL. Each
// refused property gets a 403 propstat (with its precondition, if any) and
// the rest a 424 Failed Dependency: nothing was set because nothing was
// created. RFC 4791 §5.3.1.1 answers a MKCALENDAR with 207 Multi-Status; RFC
// 5689 §3 (example 3.5) answers an extended MKCOL with 403 and a
// DAV:mkcol-response.
func writeMkcolFailure(c fiber.Ctx, reqPath string, isMkcalendar bool, all []xml.Name, refused []mkcolRefusal) error {
	var propstats []mkcolPropStat
	isRefused := map[xml.Name]bool{}
	for _, r := range refused {
		isRefused[r.prop] = true
		ps := mkcolPropStat{Prop: Prop{Raw: []RawXMLValue{{XMLName: r.prop}}}, Status: "HTTP/1.1 403 Forbidden"}
		if r.precondition != (xml.Name{}) {
			ps.Error = &mkcolError{Raw: []RawXMLValue{{XMLName: r.precondition}}}
		}
		propstats = append(propstats, ps)
	}
	var dependent []RawXMLValue
	seen := map[xml.Name]bool{}
	for _, n := range all {
		if !isRefused[n] && !seen[n] {
			seen[n] = true
			dependent = append(dependent, RawXMLValue{XMLName: n})
		}
	}
	if len(dependent) > 0 {
		propstats = append(propstats, mkcolPropStat{Prop: Prop{Raw: dependent}, Status: "HTTP/1.1 424 Failed Dependency"})
	}

	var doc interface{} = &mkcolResponse{PropStat: propstats}
	status := http.StatusForbidden
	if isMkcalendar {
		status = http.StatusMultiStatus
		doc = &mkcolMultiStatus{Response: mkcolStatusResponse{Href: reqPath, PropStat: propstats}}
	}

	c.Set("Content-Type", "application/xml; charset=utf-8")
	c.Set("Cache-Control", "no-cache")
	c.Status(status)
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(doc)
}
