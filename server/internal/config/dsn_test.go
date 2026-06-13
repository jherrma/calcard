package config

import "testing"

func TestQuoteDSNValue(t *testing.T) {
	got := quoteDSNValue(`p ss'w\rd`)
	want := `'p ss\'w\\rd'`
	if got != want {
		t.Fatalf("quoteDSNValue = %q, want %q", got, want)
	}
}
