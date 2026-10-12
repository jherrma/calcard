package calendar

import (
	"regexp"
	"time"
)

var hexColorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ValidateHexColor validates a hex color string
func ValidateHexColor(color string) error {
	if color == "" {
		return Invalidf("color cannot be empty")
	}
	if !hexColorRegex.MatchString(color) {
		return Invalidf("invalid hex color format, expected #RRGGBB")
	}
	return nil
}

// ValidateTimezone validates an IANA timezone string
func ValidateTimezone(tz string) error {
	if tz == "" {
		return Invalidf("timezone cannot be empty")
	}
	_, err := time.LoadLocation(tz)
	if err != nil {
		return Invalidf("invalid timezone: %w", err)
	}
	return nil
}

// ValidateName validates a calendar name
func ValidateName(name string) error {
	if name == "" {
		return Invalidf("name cannot be empty")
	}
	if len(name) > 255 {
		return Invalidf("name cannot exceed 255 characters")
	}
	return nil
}

// ValidateDescription validates a calendar description
func ValidateDescription(description string) error {
	if len(description) > 1000 {
		return Invalidf("description cannot exceed 1000 characters")
	}
	return nil
}
