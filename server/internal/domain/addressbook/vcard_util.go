package addressbook

import (
	"strings"

	"github.com/emersion/go-vcard"
)

// commaListVCardFields are vCard properties whose value is a comma-separated
// list (RFC 6350 §6.3.1 CATEGORIES). go-vcard's encoder escapes every comma
// unconditionally, so a value like CATEGORIES:Friends,VIP is emitted as
// CATEGORIES:Friends\,VIP — which a strict client reads as ONE category named
// literally "Friends,VIP". Any path that round-trips a vCard through the
// go-vcard encoder (the REST web-UI edit via PatchVCard AND the CardDAV GET
// serialization) must restore the separators afterward.
var commaListVCardFields = map[string]bool{
	"CATEGORIES": true,
}

// SplitCommaListFields rewrites comma-list properties (CATEGORIES) into
// multiple single-value property instances, in place, on a decoded card.
//
// This fixes comma corruption at the source, before the card is re-encoded on
// ANY DAV path (GET, REPORT multiget, addressbook-query, PROPFIND-with-address-
// data) — unlike RestoreVCardCommaLists, which only post-processes single-vCard
// GET responses and misses the multiget path phones actually sync over.
// CATEGORIES:Friends\r\nCATEGORIES:VIP (two instances) is semantically identical
// per RFC 6350 and contains no commas for go-vcard's encoder to mangle.
//
// go-vcard's decoder already collapsed the escaped/unescaped comma distinction
// (both "\," and "," decode to ","), so a category legitimately named "A,B"
// cannot be preserved — the same accepted tradeoff RestoreVCardCommaLists makes.
func SplitCommaListFields(card vcard.Card) {
	for name := range commaListVCardFields {
		fields := card[name]
		if len(fields) == 0 {
			continue
		}
		var out []*vcard.Field
		for _, f := range fields {
			for _, part := range strings.Split(f.Value, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				nf := *f // copy so Params (TYPE=) and Group carry to each instance
				nf.Value = part
				out = append(out, &nf)
			}
		}
		if len(out) > 0 {
			card[name] = out
		}
	}
}

// RestoreVCardCommaLists rewrites an encoded vCard so comma-list properties keep
// their raw comma separators instead of the escaped form go-vcard emits. The
// encoder writes one unfolded property per line, so a line-oriented pass is
// safe. Lines are split on CRLF (the encoder's separator); a trailing bare-LF
// or unterminated input is left untouched.
func RestoreVCardCommaLists(vcardStr string) string {
	lines := strings.Split(vcardStr, "\r\n")
	for i, line := range lines {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		// Property name is the token before the first ';' (params), with any
		// group prefix ("item1.") stripped.
		name := line[:colon]
		if semi := strings.IndexByte(name, ';'); semi >= 0 {
			name = name[:semi]
		}
		if dot := strings.IndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}
		if commaListVCardFields[strings.ToUpper(name)] {
			lines[i] = line[:colon+1] + unescapeSeparatorCommas(line[colon+1:])
		}
	}
	return strings.Join(lines, "\r\n")
}

// unescapeSeparatorCommas turns escaped commas ("\,") back into raw list
// separators while leaving other escape sequences (notably "\\" and "\n")
// intact, so a literal backslash immediately before a separator survives.
func unescapeSeparatorCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == ',' {
				b.WriteByte(',')
			} else {
				b.WriteByte(s[i])
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
