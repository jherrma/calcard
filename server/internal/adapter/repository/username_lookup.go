package repository

import (
	"strings"

	"gorm.io/gorm"
)

// findByUsername looks a row up by its username column, case-insensitively.
//
// Usernames are stored as issued: generated account usernames are mixed-case
// and are also the /dav/{username}/ URL segment (compared exactly), so they
// cannot be rewritten. Clients may send any case, though (the old lookups
// lower-cased the input, so a lower-cased stored name matched every spelling).
//
// The exact spelling is tried first (index-backed, the normal case). Only if
// that misses is the comparison case-folded, and a folded hit counts only when
// it is the single row matching: the unique indexes are case-sensitive, so two
// names that differ only in case can exist, and guessing between them would
// authenticate or share with the wrong row. Ambiguous or unknown → (nil, nil).
func findByUsername[T any](db *gorm.DB, username string, scopes ...func(*gorm.DB) *gorm.DB) (*T, error) {
	username = strings.TrimSpace(username)

	// Find, not First: a miss is expected here and must not log "record not found".
	var exact []T
	if err := db.Scopes(scopes...).Where("username = ?", username).Limit(1).Find(&exact).Error; err != nil {
		return nil, err
	}
	if len(exact) == 1 {
		return &exact[0], nil
	}

	var folded []T
	if err := db.Scopes(scopes...).Where("LOWER(username) = LOWER(?)", username).
		Limit(2).Find(&folded).Error; err != nil {
		return nil, err
	}
	if len(folded) != 1 {
		return nil, nil
	}
	return &folded[0], nil
}
