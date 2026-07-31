package user

import "time"

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
)

// AllowedEventDurations lists the accepted values for PrefDefaultEventDuration
// (minutes). It is a closed set rather than a numeric range so the value always
// matches one of the options the settings UI offers.
var AllowedEventDurations = []string{"15", "30", "45", "60", "90", "120", "180", "240", "480"}

// allowedPreferenceValues maps every known key to its accepted values.
var allowedPreferenceValues = map[string][]string{
	PrefDefaultEventDuration: AllowedEventDurations,
	PrefDefaultAllDay:        {"true", "false"},
	PrefTimeFormat:           {"12h", "24h"},
}

// preferenceDefaults holds the value used when a user has not set a key.
var preferenceDefaults = map[string]string{
	PrefDefaultEventDuration: "60",
	PrefDefaultAllDay:        "false",
	PrefTimeFormat:           "24h",
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
func KnownPreferenceKey(key string) bool {
	_, ok := allowedPreferenceValues[key]
	return ok
}

// AllowedPreferenceValues returns the accepted values for key, or nil if the
// key is unknown.
func AllowedPreferenceValues(key string) []string {
	return allowedPreferenceValues[key]
}

// IsAllowedPreferenceValue reports whether value is accepted for key. An
// unknown key is never allowed.
func IsAllowedPreferenceValue(key, value string) bool {
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
