package calendar

import "strings"

// StripVCalendarWrapper removes any BEGIN:VCALENDAR / END:VCALENDAR and its
// header properties (VERSION, PRODID, CALSCALE, X-WR-*) from the given iCal
// payload, returning just the contained VEVENT/VTODO/VJOURNAL/VALARM blocks.
// If the input is already a bare component (no VCALENDAR wrapper), it is
// returned unchanged except for whitespace trimming. The result always ends
// with a single CRLF.
func StripVCalendarWrapper(data string) string {
	// Normalize to \n so splitting is predictable.
	data = strings.ReplaceAll(data, "\r\n", "\n")
	lines := strings.Split(data, "\n")

	var out []string
	depth := 0          // how deep we are inside nested VCALENDARs
	componentDepth := 0 // how deep we are inside VEVENT/VTODO/etc.
	for _, line := range lines {
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case upper == "BEGIN:VCALENDAR":
			depth++
			continue
		case upper == "END:VCALENDAR":
			if depth > 0 {
				depth--
			}
			continue
		case depth > 0 && componentDepth == 0 && isVCalendarHeader(upper):
			// Drop calendar-level header props that belong on the outer wrapper.
			continue
		}
		if strings.HasPrefix(upper, "BEGIN:") && depth > 0 {
			componentDepth++
		}
		if strings.HasPrefix(upper, "END:") && componentDepth > 0 {
			componentDepth--
		}
		out = append(out, line)
	}
	result := strings.Join(out, "\r\n")
	return strings.TrimSpace(result) + "\r\n"
}

func isVCalendarHeader(upperLine string) bool {
	switch {
	case strings.HasPrefix(upperLine, "VERSION:"),
		strings.HasPrefix(upperLine, "PRODID:"),
		strings.HasPrefix(upperLine, "CALSCALE:"),
		strings.HasPrefix(upperLine, "METHOD:"),
		strings.HasPrefix(upperLine, "X-WR-"):
		return true
	}
	return false
}

// EnsureVCalendarWrapper guarantees the payload is a complete VCALENDAR object.
// If data already begins with BEGIN:VCALENDAR it is returned unchanged;
// otherwise the (bare component) body is wrapped with a minimal VCALENDAR
// header/footer. This is the inverse of StripVCalendarWrapper and is used to
// repair bare-component rows written by older import code.
func EnsureVCalendarWrapper(data string) string {
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(data)), "BEGIN:VCALENDAR") {
		return data
	}
	body := strings.TrimSpace(data)
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard//EN\r\n" + body + "\r\nEND:VCALENDAR\r\n"
}
