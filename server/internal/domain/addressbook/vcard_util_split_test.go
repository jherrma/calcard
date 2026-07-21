package addressbook

import (
	"bytes"
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
)

// TestSplitCommaListFields is part of the regression coverage for #44: a
// multi-valued CATEGORIES must become repeated single-value instances so
// go-vcard's encoder can't collapse it into one "Friends\,VIP" category.
func TestSplitCommaListFields(t *testing.T) {
	card := vcard.Card{}
	card.SetValue(vcard.FieldVersion, "3.0")
	card.SetValue("FN", "Jane Doe")
	card.SetValue("CATEGORIES", "Friends,VIP")

	SplitCommaListFields(card)

	got := card["CATEGORIES"]
	if len(got) != 2 {
		t.Fatalf("expected 2 CATEGORIES instances, got %d: %+v", len(got), got)
	}
	if got[0].Value != "Friends" || got[1].Value != "VIP" {
		t.Fatalf("expected [Friends VIP], got [%q %q]", got[0].Value, got[1].Value)
	}

	// Re-encoding must produce two CATEGORIES lines and NO escaped comma.
	var buf bytes.Buffer
	if err := vcard.NewEncoder(&buf).Encode(card); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `\,`) {
		t.Fatalf("encoded card must not contain an escaped comma:\n%s", out)
	}
	if strings.Count(out, "CATEGORIES:") != 2 {
		t.Fatalf("expected two CATEGORIES: lines, got:\n%s", out)
	}
}

// TestSplitCommaListFields_PreservesParamsAndSingle checks params carry over and
// a single-value CATEGORIES is untouched.
func TestSplitCommaListFields_PreservesParamsAndSingle(t *testing.T) {
	card := vcard.Card{}
	card.Set("CATEGORIES", &vcard.Field{
		Value:  "Work,Client",
		Params: vcard.Params{"TYPE": []string{"x"}},
		Group:  "item1",
	})

	SplitCommaListFields(card)

	got := card["CATEGORIES"]
	if len(got) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(got))
	}
	for _, f := range got {
		if f.Group != "item1" || len(f.Params["TYPE"]) != 1 || f.Params["TYPE"][0] != "x" {
			t.Fatalf("params/group must carry to each split instance, got %+v", f)
		}
	}

	single := vcard.Card{}
	single.SetValue("CATEGORIES", "Solo")
	SplitCommaListFields(single)
	if len(single["CATEGORIES"]) != 1 || single["CATEGORIES"][0].Value != "Solo" {
		t.Fatalf("single-value CATEGORIES must be unchanged, got %+v", single["CATEGORIES"])
	}
}
