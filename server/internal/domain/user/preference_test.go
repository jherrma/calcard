package user

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// SCOPE: the preference registry itself. Story 046 added a SECOND way for a key
// to be valid — a pattern, for values that cannot be enumerated — next to the
// original closed sets. These tests pin the invariants that keep the two styles
// from disagreeing about which keys exist.

func TestPreferenceRegistryIsConsistent(t *testing.T) {
	// preferenceDefaults is the registry KnownPreferenceKey answers from, so a
	// key validated somewhere but missing a default would be silently rejected
	// on write while still being a "real" key everywhere else.
	for key := range allowedPreferenceValues {
		assert.Contains(t, preferenceDefaults, key, "closed-set key %q has no default", key)
	}
	for key := range patternPreferences {
		assert.Contains(t, preferenceDefaults, key, "pattern key %q has no default", key)
	}

	for key := range preferenceDefaults {
		_, closed := allowedPreferenceValues[key]
		_, pattern := patternPreferences[key]
		assert.True(t, closed != pattern,
			"key %q must be validated by exactly one of the two mechanisms (closed=%v, pattern=%v)",
			key, closed, pattern)
	}

	// Every default has to survive its own validator, or a user who never
	// touched a setting would be handed a value the server would refuse to
	// store back.
	for key, def := range preferenceDefaults {
		assert.True(t, IsAllowedPreferenceValue(key, def),
			"default %q for key %q is not itself a valid value", def, key)
	}
}

func TestKnownPreferenceKey(t *testing.T) {
	for _, key := range []string{PrefDefaultEventDuration, PrefDefaultAllDay, PrefTimeFormat, PrefAccentColor} {
		assert.True(t, KnownPreferenceKey(key), "%q should be known", key)
	}
	for _, key := range []string{"is_admin", "", "accent_colour", "ACCENT_COLOR"} {
		assert.False(t, KnownPreferenceKey(key), "%q should not be known", key)
	}
}

func TestDefaultPreferencesReturnsACopy(t *testing.T) {
	first := DefaultPreferences()
	first[PrefTimeFormat] = "12h"
	assert.Equal(t, "24h", DefaultPreferences()[PrefTimeFormat],
		"callers merge into this map in place; it must not be the package's own")
}

func TestAccentColorValidation(t *testing.T) {
	valid := []string{"#3b82f6", "#000000", "#ffffff", "#8b5cf6"}
	for _, v := range valid {
		assert.True(t, IsAllowedPreferenceValue(PrefAccentColor, v), "%q should be accepted", v)
	}

	invalid := []string{
		"3b82f6",   // no hash
		"#3b82f",   // five digits
		"#3b82f6a", // seven digits
		"#3b82fg",  // g is not hex
		"#abc",     // shorthand is not accepted — the frontend expands it first
		"rebeccapurple",
		"",
		"red",
		"rgb(59,130,246)",
		"#3b82f6; background: url(x)", // the value is interpolated into CSS downstream
	}
	for _, v := range invalid {
		assert.False(t, IsAllowedPreferenceValue(PrefAccentColor, v), "%q should be rejected", v)
	}
}

func TestNormalizePreferenceValue(t *testing.T) {
	// The pattern is deliberately lowercase-only, so normalization is what makes
	// an uppercase hex acceptable rather than the validator being lenient. That
	// keeps exactly one spelling of a colour in the database.
	assert.Equal(t, "#8b5cf6", NormalizePreferenceValue(PrefAccentColor, "#8B5CF6"))
	assert.Equal(t, "#8b5cf6", NormalizePreferenceValue(PrefAccentColor, "  #8B5CF6\t"))
	assert.True(t, IsAllowedPreferenceValue(PrefAccentColor, NormalizePreferenceValue(PrefAccentColor, "#8B5CF6")))

	// Closed-set keys have no normalizer and must come back byte-identical:
	// silently trimming or lowercasing them would let a value that is NOT in the
	// allowed set slip through as one that is.
	assert.Equal(t, " 12H ", NormalizePreferenceValue(PrefTimeFormat, " 12H "))
	assert.Equal(t, "60", NormalizePreferenceValue(PrefDefaultEventDuration, "60"))
	assert.Equal(t, "anything", NormalizePreferenceValue("unknown_key", "anything"))
}

func TestPreferenceValueHint(t *testing.T) {
	// Closed sets enumerate; patterns describe. Both complete "<key> must be …".
	assert.Equal(t, "one of 12h, 24h", PreferenceValueHint(PrefTimeFormat))
	assert.Contains(t, PreferenceValueHint(PrefAccentColor), "hex")
	assert.NotContains(t, PreferenceValueHint(PrefAccentColor), "one of",
		"a pattern key must not claim to have an enumerable set")
	assert.Empty(t, PreferenceValueHint("is_admin"))

	// AllowedPreferenceValues cannot answer for a pattern key — which is exactly
	// why the hint exists and why callers must not build messages from it.
	assert.Nil(t, AllowedPreferenceValues(PrefAccentColor))
	assert.Equal(t, []string{"12h", "24h"}, AllowedPreferenceValues(PrefTimeFormat))
}
