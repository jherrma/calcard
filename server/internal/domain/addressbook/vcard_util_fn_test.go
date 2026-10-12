package addressbook

import (
	"testing"

	"github.com/emersion/go-vcard"
)

// TestEnsureFormattedName covers F27 (c): a card without a usable FN gets one
// derived from N, ORG, NICKNAME, EMAIL or TEL — in that order — and an
// existing FN is never touched.
func TestEnsureFormattedName(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]string
		wantFN string
		wantOK bool
	}{
		{"existing FN kept", map[string]string{"FN": "Set By Client", "N": "Doe;Jane;;;"}, "Set By Client", true},
		{"blank FN replaced", map[string]string{"FN": "  ", "N": "Doe;Jane;;;"}, "Jane Doe", true},
		{"from N with all parts", map[string]string{"N": "Lovelace;Ada;Augusta;Countess;FRS"}, "Countess Ada Augusta Lovelace FRS", true},
		{"from ORG, first component", map[string]string{"N": ";;;;", "ORG": "Acme Inc.;Sales"}, "Acme Inc.", true},
		{"from NICKNAME", map[string]string{"NICKNAME": "Ace"}, "Ace", true},
		{"from EMAIL", map[string]string{"EMAIL": "who@example.test"}, "who@example.test", true},
		{"from TEL", map[string]string{"TEL": "+49 30 1234567"}, "+49 30 1234567", true},
		{"nothing to derive from", map[string]string{"NOTE": "anonymous"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := vcard.Card{}
			card.SetValue(vcard.FieldVersion, "3.0")
			for k, v := range tc.fields {
				card.SetValue(k, v)
			}
			ok := EnsureFormattedName(card)
			if ok != tc.wantOK {
				t.Fatalf("EnsureFormattedName = %v, want %v", ok, tc.wantOK)
			}
			if got := card.Value(vcard.FieldFormattedName); got != tc.wantFN {
				t.Fatalf("FN = %q, want %q", got, tc.wantFN)
			}
		})
	}
}
