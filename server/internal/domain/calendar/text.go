package calendar

import (
	"strings"

	"github.com/emersion/go-ical"
)

// RFC 5545 TEXT values (§3.3.11): SUMMARY, DESCRIPTION and LOCATION are stored
// escaped in the iCalendar (`\\`, `\;`, `\,`, `\n`), but everything the API
// shows or takes is plain text. Every read of such a property for the API (the
// denormalized Summary/Description/Location columns, an occurrence's override
// text) goes through UnescapeText, and every write through NewTextProp, so a
// value the API returned can be sent back unchanged without gaining a
// backslash per round trip.

// UnescapeText decodes an RFC 5545 TEXT value as ONE string: `\\` → `\`,
// `\;` → `;`, `\,` → `,`, `\n` and `\N` → a newline.
//
// Unlike go-ical's Prop.Text it never splits at an unescaped comma (Text
// returns TextList()[0], so a sloppy client's "SUMMARY:Buy milk, eggs" would
// read "Buy milk") and it never fails: a backslash before any other character,
// or at the very end, is kept as written.
func UnescapeText(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var sb strings.Builder
	sb.Grow(len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c != '\\' || i+1 == len(value) {
			sb.WriteByte(c)
			continue
		}
		switch next := value[i+1]; next {
		case '\\', ';', ',':
			sb.WriteByte(next)
			i++
		case 'n', 'N':
			sb.WriteByte('\n')
			i++
		default:
			// Not an escape RFC 5545 defines: keep the backslash; the next
			// character is copied by the following iteration.
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// EscapeText encodes text as an RFC 5545 TEXT value: backslash, semicolon and
// comma are escaped, and every line break — CRLF, a lone CR or LF — becomes
// `\n`. That is go-ical's SetText, plus the CR: go-ical copies a CR into the
// value, and its encoder then refuses the whole object ("contains a CR or
// LF"). UnescapeText(EscapeText(s)) is s with its line breaks turned into LF.
func EscapeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var sb strings.Builder
	sb.Grow(len(text))
	for _, r := range text {
		switch r {
		case '\\', ';', ',':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case '\n':
			sb.WriteString(`\n`)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// NewTextProp returns the TEXT property name (SUMMARY, DESCRIPTION, LOCATION)
// holding text, escaped by EscapeText, without parameters. Set it with
// Props.Set, which replaces every earlier property of that name.
func NewTextProp(name, text string) *ical.Prop {
	p := ical.NewProp(name)
	p.Value = EscapeText(text)
	return p
}

// TextPropValue returns the plain text (UnescapeText) of comp's first property
// name, or "" when comp has none.
func TextPropValue(comp *ical.Component, name string) string {
	if p := comp.Props.Get(name); p != nil {
		return UnescapeText(p.Value)
	}
	return ""
}
