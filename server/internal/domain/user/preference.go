package user

import (
	"regexp"
	"strings"
	"time"
)

// UserPreference is one key/value setting owned by a single user (story 103).
//
// Values are stored as strings so adding a preference key needs no schema
// change. The trade-off is that nothing at the storage layer constrains a
// value, so the ONLY place a value is trusted is after it passed
// IsAllowedPreferenceValue — never hand a raw row value to a client without
// filtering the key through KnownPreferenceKey first (a key that was removed
// from the code can still be sitting in the table).
type UserPreference struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    uint   `gorm:"uniqueIndex:idx_user_pref_key;not null"`
	Key       string `gorm:"uniqueIndex:idx_user_pref_key;size:100;not null"`
	Value     string `gorm:"size:500;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for the UserPreference model
func (UserPreference) TableName() string {
	return "user_preferences"
}

// Preference keys. Only keys listed here are accepted on write or returned on
// read — see KnownPreferenceKey.
const (
	// PrefDefaultEventDuration is the length in minutes a newly created event
	// gets by default.
	PrefDefaultEventDuration = "default_event_duration"
	// PrefDefaultAllDay decides whether new events start out as all-day
	// ("true" / "false").
	PrefDefaultAllDay = "default_all_day"
	// PrefTimeFormat selects 12-hour or 24-hour time display ("12h" / "24h").
	PrefTimeFormat = "time_format"
	// PrefAccentColor is the UI accent colour as a 6-digit hex string
	// ("#3b82f6"). Unlike the keys above it is validated by PATTERN, not by a
	// closed set — see patternPreferences (story 046).
	PrefAccentColor = "accent_color"
)

// AllowedEventDurations lists the accepted values for PrefDefaultEventDuration
// (minutes). It is a closed set rather than a numeric range so the value always
// matches one of the options the settings UI offers.
var AllowedEventDurations = []string{"15", "30", "45", "60", "90", "120", "180", "240", "480"}

// allowedPreferenceValues maps every CLOSED-SET key to its accepted values.
// Keys whose value space is too large to enumerate live in patternPreferences
// instead; a key belongs to exactly one of the two.
var allowedPreferenceValues = map[string][]string{
	PrefDefaultEventDuration: AllowedEventDurations,
	PrefDefaultAllDay:        {"true", "false"},
	PrefTimeFormat:           {"12h", "24h"},
}

// Lowercase-only on purpose: normalize lowercases before this ever runs, so a
// value that reaches storage is always in one canonical form and "#3B82F6" can
// never sit alongside "#3b82f6" meaning the same thing.
var accentColorPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// patternPreference validates a key whose accepted values cannot be listed.
type patternPreference struct {
	// normalize canonicalises a value before validation and before storage.
	normalize func(string) string
	valid     func(string) bool
	// hint completes the sentence "<key> must be …" in a 400.
	hint string
}

var patternPreferences = map[string]patternPreference{
	PrefAccentColor: {
		normalize: func(v string) string { return strings.ToLower(strings.TrimSpace(v)) },
		valid:     accentColorPattern.MatchString,
		hint:      "a 6-digit hex colour such as #3b82f6",
	},
}

// preferenceDefaults holds the value used when a user has not set a key.
//
// It doubles as the registry of known keys (see KnownPreferenceKey): every key
// this server manages has a default, whichever way it is validated.
var preferenceDefaults = map[string]string{
	PrefDefaultEventDuration: "60",
	PrefDefaultAllDay:        "false",
	PrefTimeFormat:           "24h",
	// Tailwind blue-500, matching the built-in preset in nuxt.config.ts.
	PrefAccentColor: "#3b82f6",
}

// DefaultPreferences returns a fresh copy of the default preference map. It is
// a copy on purpose: callers merge stored values into it in place.
func DefaultPreferences() map[string]string {
	defaults := make(map[string]string, len(preferenceDefaults))
	for k, v := range preferenceDefaults {
		defaults[k] = v
	}
	return defaults
}

// KnownPreferenceKey reports whether key is a preference this server manages.
// Membership is decided by preferenceDefaults, the one table both validation
// styles are obliged to appear in.
func KnownPreferenceKey(key string) bool {
	_, ok := preferenceDefaults[key]
	return ok
}

// AllowedPreferenceValues returns the accepted values for key, or nil if the key
// is unknown OR is pattern-validated (its values cannot be enumerated). Use
// PreferenceValueHint when you need something to show a user.
func AllowedPreferenceValues(key string) []string {
	return allowedPreferenceValues[key]
}

// PreferenceValueHint describes what key accepts, phrased to complete the
// sentence "<key> must be …". Empty for an unknown key.
func PreferenceValueHint(key string) string {
	if p, ok := patternPreferences[key]; ok {
		return p.hint
	}
	if allowed, ok := allowedPreferenceValues[key]; ok {
		return "one of " + strings.Join(allowed, ", ")
	}
	return ""
}

// NormalizePreferenceValue canonicalises a value for key: lowercasing a hex
// colour, for instance. Values for keys with no normalizer come back untouched.
//
// Call this BEFORE IsAllowedPreferenceValue — pattern validators are written
// against the canonical form, so "#3B82F6" only passes once it has been
// lowercased.
func NormalizePreferenceValue(key, value string) string {
	if p, ok := patternPreferences[key]; ok && p.normalize != nil {
		return p.normalize(value)
	}
	return value
}

// IsAllowedPreferenceValue reports whether value is accepted for key. An
// unknown key is never allowed.
func IsAllowedPreferenceValue(key, value string) bool {
	if p, ok := patternPreferences[key]; ok {
		return p.valid(value)
	}
	allowed, ok := allowedPreferenceValues[key]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == value {
			return true
		}
	}
	return false
}
