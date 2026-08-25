package subscription

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// Non-standard properties publishers use to name a feed. They are not in RFC
// 5545, but every major publisher (Google, Apple, Outlook, and every PHP
// exporter in between) emits X-WR-CALNAME, and it is the only sensible default
// name for a subscription the user did not name themselves.
const (
	propCalendarName = "X-WR-CALNAME"
	propCalendarTZ   = "X-WR-TIMEZONE"
	propCalendarDesc = "X-WR-CALDESC"
)

// ParsedFeed is a feed decomposed into the objects a calendar stores.
type ParsedFeed struct {
	// Name/Description/Timezone come from the feed's own X-WR-* properties and
	// are used only as defaults when creating a subscription.
	Name        string
	Description string
	Timezone    string
	// Objects is one CalendarObject per UID, each a complete VCALENDAR.
	Objects []*calendar.CalendarObject
	// Skipped counts components that could not be turned into an object. It is
	// reported rather than treated as failure: one malformed VEVENT in a feed
	// of hundreds must not cost the user the other 99%.
	Skipped int
}

// ParseFeed turns raw feed bytes into storable calendar objects.
//
// Components are grouped by UID, because that is the unit CalDAV addresses: a
// recurring event and its RECURRENCE-ID overrides share one UID and must live
// in ONE resource (RFC 4791 §4.1), while a feed lists them as sibling VEVENTs.
// Splitting them would publish overrides as independent events — the same
// meeting appearing twice, once at its original time and once at its moved one.
func ParseFeed(data []byte) (*ParsedFeed, error) {
	cal, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotCalendar, err)
	}

	feed := &ParsedFeed{
		Name:        strings.TrimSpace(propText(cal.Component, propCalendarName)),
		Description: strings.TrimSpace(propText(cal.Component, propCalendarDesc)),
		Timezone:    strings.TrimSpace(propText(cal.Component, propCalendarTZ)),
	}

	// VTIMEZONE definitions are shared across the whole feed; each object gets
	// only the ones its own components reference, so a feed declaring twenty
	// zones does not attach all twenty to every event.
	timezones := map[string]*ical.Component{}
	for _, child := range cal.Children {
		if child.Name != ical.CompTimezone {
			continue
		}
		if tzid := propText(child, ical.PropTimezoneID); tzid != "" {
			timezones[tzid] = child
		}
	}

	// Preserve feed order within a UID group so the master (which publishers
	// list first) stays first — PopulateDenormFieldsFromICal reads the primary
	// component for summary/location.
	var order []string
	groups := map[string][]*ical.Component{}
	for _, child := range cal.Children {
		if child.Name != ical.CompEvent && child.Name != ical.CompToDo {
			continue
		}
		uid := strings.TrimSpace(propText(child, ical.PropUID))
		if uid == "" {
			// Without a UID there is no stable identity, so the object could
			// never be matched on the next sync: it would be deleted and
			// recreated every refresh, churning the sync log forever.
			feed.Skipped++
			continue
		}
		if _, seen := groups[uid]; !seen {
			order = append(order, uid)
		}
		groups[uid] = append(groups[uid], child)
	}

	for _, uid := range order {
		obj, err := buildObject(uid, groups[uid], timezones)
		if err != nil {
			feed.Skipped++
			continue
		}
		feed.Objects = append(feed.Objects, obj)
	}

	if len(feed.Objects) == 0 && feed.Skipped == 0 {
		// A syntactically valid VCALENDAR with no events at all is a legitimate
		// (if dull) feed — an empty holiday calendar for a quiet year. It is
		// only a problem on creation, where the create use case decides.
		return feed, nil
	}
	return feed, nil
}

// buildObject wraps one UID's components into a self-contained VCALENDAR.
func buildObject(uid string, comps []*ical.Component, timezones map[string]*ical.Component) (*calendar.CalendarObject, error) {
	wrapper := ical.NewCalendar()
	wrapper.Props.SetText(ical.PropProductID, "-//CalCard//Calendar Subscription//EN")
	wrapper.Props.SetText(ical.PropVersion, "2.0")

	attached := map[string]bool{}
	for _, comp := range comps {
		for _, tzid := range referencedTimezones(comp) {
			if attached[tzid] {
				continue
			}
			if tz, ok := timezones[tzid]; ok {
				wrapper.Children = append(wrapper.Children, tz)
				attached[tzid] = true
			}
		}
	}

	for _, comp := range comps {
		// DTSTAMP is mandatory (RFC 5545 §3.6.1) and the encoder enforces it,
		// but feeds omit it often enough that dropping those events would lose
		// real data. Stamping the component with a FIXED instant rather than
		// time.Now() matters: a per-sync timestamp would make the object's
		// bytes differ on every refresh, so every sync would report every
		// event as modified and bump the CTag, waking every DAV client hourly.
		if comp.Props.Get(ical.PropDateTimeStamp) == nil {
			comp.Props.SetDateTime(ical.PropDateTimeStamp, syntheticDTStamp)
		}
		wrapper.Children = append(wrapper.Children, comp)
	}

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(wrapper); err != nil {
		return nil, err
	}

	objUUID := uuid.New().String()
	obj := &calendar.CalendarObject{
		UUID: objUUID,
		UID:  uid,
		// The DAV path is derived from our own UUID, not the feed's UID: a UID
		// is an opaque string that may contain slashes, spaces or non-ASCII,
		// none of which survive being a URL path segment, and two distinct UIDs
		// must never sanitize down to the same href. On a refresh the stored
		// object keeps its path, so client-visible URLs stay stable.
		Path:     objUUID + ".ics",
		ETag:     calendar.NewETag(),
		ICalData: buf.String(),
	}
	if err := obj.PopulateDenormFieldsFromICal(); err != nil {
		return nil, err
	}
	return obj, nil
}

// syntheticDTStamp is the DTSTAMP given to components that lack one. Any fixed
// instant works; the Unix epoch is unambiguous about being synthetic.
var syntheticDTStamp = time.Unix(0, 0).UTC()

// referencedTimezones lists the TZIDs a component's date-time properties name,
// sorted.
//
// The sort is not cosmetic: Props is a map, so iterating it yields a different
// order on every run, and that order decides the order VTIMEZONE children are
// attached in — which would make the same event encode to different bytes on
// every sync, so every refresh would report every event as modified.
func referencedTimezones(comp *ical.Component) []string {
	var ids []string
	seen := map[string]bool{}
	for _, props := range comp.Props {
		for _, prop := range props {
			tzid := prop.Params.Get(ical.ParamTimezoneID)
			if tzid == "" || seen[tzid] {
				continue
			}
			seen[tzid] = true
			ids = append(ids, tzid)
		}
	}
	sort.Strings(ids)
	return ids
}

func propText(comp *ical.Component, name string) string {
	if p := comp.Props.Get(name); p != nil {
		return p.Value
	}
	return ""
}
