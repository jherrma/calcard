package importexport

import (
	"sort"

	ical "github.com/emersion/go-ical"
)

// vtimezonesByID indexes the top-level VTIMEZONE components of an imported
// file by their TZID. A file declares each zone once for all its events, while
// every stored object must carry its own copy (see referencedVTimezones).
func vtimezonesByID(cal *ical.Calendar) map[string]*ical.Component {
	timezones := map[string]*ical.Component{}
	for _, child := range cal.Children {
		if child.Name != ical.CompTimezone {
			continue
		}
		if p := child.Props.Get(ical.PropTimezoneID); p != nil && p.Value != "" {
			timezones[p.Value] = child
		}
	}
	return timezones
}

// referencedVTimezones returns the VTIMEZONEs the given components name in a
// TZID parameter, so an object re-wrapped on its own stays self-contained: RFC
// 5545 §3.2.19 requires every TZID to resolve to a VTIMEZONE in the same
// iCalendar object, and a DAV client GETting the resource sees nothing else.
// Zones the file declares but these components do not use are left out; a TZID
// the file never defines has nothing to copy and is left as it was.
//
// The result is sorted by TZID because Props is a map: iterating it unsorted
// would attach the zones in a different order on every run.
func referencedVTimezones(timezones map[string]*ical.Component, comps ...*ical.Component) []*ical.Component {
	seen := map[string]bool{}
	var collect func(*ical.Component)
	collect = func(comp *ical.Component) {
		for _, props := range comp.Props {
			for _, prop := range props {
				if tzid := prop.Params.Get(ical.ParamTimezoneID); tzid != "" {
					seen[tzid] = true
				}
			}
		}
		// Sub-components (VALARM) carry date-time properties too.
		for _, child := range comp.Children {
			collect(child)
		}
	}
	for _, comp := range comps {
		collect(comp)
	}

	ids := make([]string, 0, len(seen))
	for tzid := range seen {
		if _, ok := timezones[tzid]; ok {
			ids = append(ids, tzid)
		}
	}
	sort.Strings(ids)

	out := make([]*ical.Component, 0, len(ids))
	for _, tzid := range ids {
		out = append(out, timezones[tzid])
	}
	return out
}
