package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// RecurrenceIDLayout is the canonical UTC key format used to match
// RECURRENCE-ID / EXDATE values against generated occurrences.
const RecurrenceIDLayout = "20060102T150405Z"

// RecurrenceIDKeyFromProp turns a RECURRENCE-ID (or EXDATE element) property
// into a canonical UTC key. go-ical's Prop.DateTime already understands UTC-Z,
// TZID=, floating-in-loc, and VALUE=DATE forms, so all real-client variants
// normalize to the same key. loc defaults to UTC when nil.
func RecurrenceIDKeyFromProp(prop *ical.Prop, loc *time.Location) (string, error) {
	if prop == nil {
		return "", fmt.Errorf("nil recurrence-id property")
	}
	if loc == nil {
		loc = time.UTC
	}
	t, err := prop.DateTime(loc)
	if err != nil {
		return "", err
	}
	return t.UTC().Format(RecurrenceIDLayout), nil
}

// ParseRecurrenceIDString parses a RECURRENCE-ID value supplied by the REST API
// (the list endpoint hands out UTC ids) or an all-day VALUE=DATE form. It never
// returns a zero time on failure — it returns an error so callers fail loudly
// instead of writing UNTIL=0001-... and wiping a series.
func ParseRecurrenceIDString(val string) (time.Time, error) {
	val = strings.TrimSpace(val)
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		if t, err := time.ParseInLocation(layout, val, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid recurrence-id %q", val)
}

// EXDATEKeys expands an EXDATE property (which may hold a comma-separated list)
// into canonical UTC keys, carrying TZID/VALUE params onto each value. Values
// that fail to parse are skipped.
func EXDATEKeys(prop *ical.Prop, loc *time.Location) []string {
	if prop == nil {
		return nil
	}
	var keys []string
	for _, v := range strings.Split(prop.Value, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		sub := &ical.Prop{Name: prop.Name, Params: prop.Params, Value: v}
		if key, err := RecurrenceIDKeyFromProp(sub, loc); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}
