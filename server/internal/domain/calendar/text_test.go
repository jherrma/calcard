package calendar

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnescapeText: every RFC 5545 §3.3.11 escape is undone, in one string
// (no split at a comma), and anything that is not an escape is kept as written.
func TestUnescapeText(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"plain", "Buy milk", "Buy milk"},
		{"empty", "", ""},
		{"comma", `Buy milk\, eggs`, "Buy milk, eggs"},
		{"semicolon", `Milk\; eggs`, "Milk; eggs"},
		{"backslash", `C:\\Users\\me`, `C:\Users\me`},
		{"newline", `Line one\nLine two`, "Line one\nLine two"},
		{"upper-case N", `Line one\NLine two`, "Line one\nLine two"},
		{"all at once", `a\, b\; c\\d\ne`, "a, b; c\\d\ne"},
		{"escaped backslash before n is not a newline", `C:\\new`, `C:\new`},
		{"escaped backslash before comma", `a\\\,b`, `a\,b`},
		{"unescaped comma is kept, not split", "Buy milk, eggs", "Buy milk, eggs"},
		{"unescaped semicolon is kept", "Milk; eggs", "Milk; eggs"},
		{"unescaped comma next to an escape", `Buy milk, eggs\; bread`, "Buy milk, eggs; bread"},
		{"unknown escape is kept", `a\tb\:c`, `a\tb\:c`},
		{"trailing backslash is kept", `ends with \`, `ends with \`},
		{"lone backslash", `\`, `\`},
		{"non-ASCII", `Ärztin\, Zahnärztin\; 東京\nñ`, "Ärztin, Zahnärztin; 東京\nñ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, UnescapeText(tc.value))
		})
	}
}

// TestEscapeText: backslash, semicolon and comma are escaped, every line break
// is \n, nothing else changes.
func TestEscapeText(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"plain", "Buy milk", "Buy milk"},
		{"empty", "", ""},
		{"comma", "Buy milk, eggs", `Buy milk\, eggs`},
		{"semicolon", "Milk; eggs", `Milk\; eggs`},
		{"backslash", `C:\Users`, `C:\\Users`},
		{"LF", "a\nb", `a\nb`},
		{"CRLF", "a\r\nb", `a\nb`},
		{"lone CR", "a\rb", `a\nb`},
		{"blank line", "a\r\n\r\nb", `a\n\nb`},
		{"already escaped text is text", `a\, b`, `a\\\, b`},
		{"non-ASCII", "Ärztin, 東京", `Ärztin\, 東京`},
		{"colon and quote stay", `Note: "x"`, `Note: "x"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EscapeText(tc.text))
		})
	}
}

// textSamples are API texts with every character TEXT escaping touches.
var textSamples = []string{
	"Buy milk, eggs; bread",
	`C:\Users\me\new`,
	"Line one\nLine two\n\nLine four",
	`a\, b`, // a text that looks escaped is still just text
	`\\`,
	"trailing backslash \\",
	",;\\\n",
	"Ärztin, Zahnärztin; 東京",
	"",
}

// TestTextRoundTrip: EscapeText and UnescapeText are inverse, repeating the
// round trip adds nothing, EscapeText writes exactly what go-ical's SetText
// writes (CR aside), and a value survives go-ical's encoder and decoder.
func TestTextRoundTrip(t *testing.T) {
	for _, text := range textSamples {
		escaped := EscapeText(text)
		assert.Equal(t, text, UnescapeText(escaped), "unescape(escape(%q))", text)
		assert.Equal(t, escaped, EscapeText(UnescapeText(escaped)), "a second round trip of %q adds nothing", text)

		viaGoICal := ical.NewProp(ical.PropSummary)
		viaGoICal.SetText(text)
		assert.Equal(t, viaGoICal.Value, escaped, "same bytes as go-ical's SetText for %q", text)

		// Through the real encoder and decoder, as a stored object would go.
		cal := ical.NewCalendar()
		cal.Props.SetText(ical.PropVersion, "2.0")
		cal.Props.SetText(ical.PropProductID, "-//test//EN")
		event := ical.NewEvent()
		event.Props.SetText(ical.PropUID, "u")
		event.Props.SetDateTime(ical.PropDateTimeStamp, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		event.Props.Set(NewTextProp(ical.PropSummary, text))
		cal.Children = append(cal.Children, event.Component)
		var buf bytes.Buffer
		require.NoError(t, ical.NewEncoder(&buf).Encode(cal), "%q", text)
		decoded, err := ical.NewDecoder(strings.NewReader(buf.String())).Decode()
		require.NoError(t, err)
		assert.Equal(t, text, TextPropValue(decoded.Children[0], ical.PropSummary), "encode/decode of %q", text)
	}
}

// TestNewTextProp: a CR, which go-ical would copy into the value and then
// refuse to encode, becomes \n; the property has no parameters.
func TestNewTextProp(t *testing.T) {
	p := NewTextProp(ical.PropDescription, "Windows\r\nline, break")
	assert.Equal(t, ical.PropDescription, p.Name)
	assert.Equal(t, `Windows\nline\, break`, p.Value)
	assert.Empty(t, p.Params)

	goICal := ical.NewProp(ical.PropDescription)
	goICal.SetText("Windows\r\nline")
	assert.Contains(t, goICal.Value, "\r", "go-ical keeps the CR (what NewTextProp exists to avoid)")
}

// TestTextPropValue: the first property's plain text, "" without one.
func TestTextPropValue(t *testing.T) {
	comp := ical.NewComponent(ical.CompToDo)
	assert.Equal(t, "", TextPropValue(comp, ical.PropSummary))
	comp.Props.Set(&ical.Prop{Name: ical.PropSummary, Params: ical.Params{"LANGUAGE": {"de"}}, Value: `Milch\, Eier`})
	assert.Equal(t, "Milch, Eier", TextPropValue(comp, ical.PropSummary))
}

// TestPopulateDenormFields_TextIsUnescaped: the summary, description and
// location columns hold plain text, read from the master; the UID stays raw.
func TestPopulateDenormFields_TextIsUnescaped(t *testing.T) {
	for _, component := range []string{"VEVENT", "VTODO"} {
		t.Run(component, func(t *testing.T) {
			o := &CalendarObject{ICalData: wrap(strings.Join([]string{
				"BEGIN:" + component,
				`UID:odd\,uid`,
				"DTSTAMP:20260101T000000Z",
				"DTSTART:20260101T100000Z",
				`SUMMARY;LANGUAGE=en:Buy milk\, eggs\; bread`,
				`DESCRIPTION:Line one\nLine two\, with C:\\path`,
				`LOCATION:Market\, Hall 2`,
				"END:" + component,
			}, "\r\n"))}
			require.NoError(t, o.PopulateDenormFieldsFromICal())
			assert.Equal(t, "Buy milk, eggs; bread", o.Summary)
			assert.Equal(t, "Line one\nLine two, with C:\\path", o.Description)
			assert.Equal(t, "Market, Hall 2", o.Location)
			assert.Equal(t, `odd\,uid`, o.UID, "the UID is an identifier and stays as stored")
		})
	}

	t.Run("a sloppy unescaped comma keeps the whole text", func(t *testing.T) {
		o := &CalendarObject{ICalData: wrap("BEGIN:VTODO\r\nUID:a\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:Buy milk, eggs\r\nEND:VTODO")}
		require.NoError(t, o.PopulateDenormFieldsFromICal())
		assert.Equal(t, "Buy milk, eggs", o.Summary)
	})

	t.Run("the master's text, not an override's in front of it", func(t *testing.T) {
		o := &CalendarObject{ICalData: wrap(strings.Join([]string{
			"BEGIN:VEVENT", "UID:s", "DTSTAMP:20260101T000000Z", "RECURRENCE-ID:20260108T100000Z",
			"DTSTART:20260108T110000Z", `SUMMARY:Moved\, once`, "END:VEVENT",
			"BEGIN:VEVENT", "UID:s", "DTSTAMP:20260101T000000Z", "DTSTART:20260101T100000Z",
			"RRULE:FREQ=WEEKLY", `SUMMARY:Standup\; daily`, "END:VEVENT",
		}, "\r\n"))}
		require.NoError(t, o.PopulateDenormFieldsFromICal())
		assert.Equal(t, "Standup; daily", o.Summary)
	})
}

// TestExpandRecurringEvent_TextIsUnescaped: an occurrence's summary,
// description and location are plain text, for the series and for an
// override that carries its own.
func TestExpandRecurringEvent_TextIsUnescaped(t *testing.T) {
	obj := &CalendarObject{UUID: "u", ICalData: wrap(strings.Join([]string{
		"BEGIN:VEVENT", "UID:s", "DTSTAMP:20260101T000000Z",
		"DTSTART:20260105T090000Z", "DTEND:20260105T100000Z", "RRULE:FREQ=DAILY;COUNT=3",
		`SUMMARY:Standup\, team A`, `DESCRIPTION:Agenda\nnotes`, `LOCATION:Room 1\; floor 2`, "END:VEVENT",
		"BEGIN:VEVENT", "UID:s", "DTSTAMP:20260101T000000Z", "RECURRENCE-ID:20260106T090000Z",
		"DTSTART:20260106T110000Z", "DTEND:20260106T120000Z",
		`SUMMARY:Standup\, moved`, `DESCRIPTION:C:\\temp`, `LOCATION:Room 2\, hall`, "END:VEVENT",
	}, "\r\n"))}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())

	instances, err := ExpandRecurringEvent(obj, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, instances, 3)
	byID := map[string]EventInstance{}
	for _, inst := range instances {
		byID[inst.RecurrenceID] = inst
	}
	series := byID["20260105T090000Z"]
	assert.Equal(t, "Standup, team A", series.Summary)
	assert.Equal(t, "Agenda\nnotes", series.Description)
	assert.Equal(t, "Room 1; floor 2", series.Location)
	moved := byID["20260106T090000Z"]
	assert.True(t, moved.IsException)
	assert.Equal(t, "Standup, moved", moved.Summary)
	assert.Equal(t, `C:\temp`, moved.Description)
	assert.Equal(t, "Room 2, hall", moved.Location)
}
