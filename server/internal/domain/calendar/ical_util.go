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
	physical := strings.Split(data, "\n")

	// Unfold first (RFC 5545 §3.1): a physical line beginning with a space or
	// tab continues the previous logical line. Classifying line-by-line before
	// unfolding would drop a folded calendar header's first line (e.g. a long
	// "X-WR-CALNAME:") while keeping its leading-space continuation, leaving an
	// orphan content line inside the output → invalid ICS.
	var lines []string
	for _, line := range physical {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}

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

// VTimezoneBlock is a single VTIMEZONE component paired with the TZID it
// defines.
type VTimezoneBlock struct {
	TZID  string
	Block string // complete BEGIN:VTIMEZONE…END:VTIMEZONE, with a trailing CRLF
}

// ExtractVTimezones separates VTIMEZONE components from an already-stripped iCal
// body (see StripVCalendarWrapper). It returns the body with those components
// removed and the extracted blocks in document order. A VTIMEZONE with no
// resolvable TZID is left inline in the body so nothing is lost.
func ExtractVTimezones(body string) (string, []VTimezoneBlock) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	var blocks []VTimezoneBlock
	var out []string
	for i := 0; i < len(lines); i++ {
		if strings.ToUpper(strings.TrimSpace(lines[i])) != "BEGIN:VTIMEZONE" {
			out = append(out, lines[i])
			continue
		}
		// Collect the whole VTIMEZONE, honoring nested STANDARD/DAYLIGHT
		// sub-components via depth tracking.
		var block []string
		depth := 0
		tzid := ""
		for ; i < len(lines); i++ {
			l := lines[i]
			u := strings.ToUpper(strings.TrimSpace(l))
			if strings.HasPrefix(u, "BEGIN:") {
				depth++
			} else if strings.HasPrefix(u, "END:") {
				depth--
			} else if tzid == "" && strings.HasPrefix(u, "TZID:") {
				if parts := strings.SplitN(strings.TrimSpace(l), ":", 2); len(parts) == 2 {
					tzid = strings.TrimSpace(parts[1])
				}
			}
			block = append(block, l)
			if depth == 0 {
				break
			}
		}
		if tzid == "" {
			// Can't dedup safely; keep it inline in the body.
			out = append(out, block...)
			continue
		}
		blocks = append(blocks, VTimezoneBlock{TZID: tzid, Block: strings.Join(block, "\r\n") + "\r\n"})
	}

	bodyOut := strings.TrimRight(strings.Join(out, "\r\n"), "\r\n")
	if bodyOut != "" {
		bodyOut += "\r\n"
	}
	return bodyOut, blocks
}

// ConcatObjectsDedupVTimezones strips the VCALENDAR wrapper from each object,
// hoists a single copy of each distinct-TZID VTIMEZONE to the front (RFC 5545
// §3.6.5 requires one VTIMEZONE per distinct TZID), and returns the combined
// content to place between a VCALENDAR header and END:VCALENDAR.
func ConcatObjectsDedupVTimezones(objects []*CalendarObject) string {
	seen := make(map[string]bool)
	var tzOut, bodyOut strings.Builder
	for _, obj := range objects {
		body, tzs := ExtractVTimezones(StripVCalendarWrapper(obj.ICalData))
		for _, tz := range tzs {
			if seen[tz.TZID] {
				continue
			}
			seen[tz.TZID] = true
			tzOut.WriteString(tz.Block)
		}
		bodyOut.WriteString(body)
	}
	return tzOut.String() + bodyOut.String()
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
