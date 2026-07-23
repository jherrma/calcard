package user

import (
	"time"
)

// RefreshToken represents a long-lived token used to obtain new access tokens.
//
// Rotation & reuse detection (OWASP): every login starts a token "family"
// (FamilyID). Each refresh rotates the presented token — the old row is revoked
// and its ReplacedByHash is set to the successor's hash. Presenting a token that
// is already revoked means either a benign near-simultaneous double-refresh
// (tolerated within a short grace window) or a replayed/stolen token, in which
// case the whole family is revoked.
type RefreshToken struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	UserAgent string    `gorm:"size:500"`
	IP        string    `gorm:"size:45"`
	CreatedAt time.Time
	RevokedAt *time.Time `gorm:"index"`
	// FamilyID groups every token descended from a single login so a detected
	// reuse can revoke the entire lineage. Empty on legacy pre-migration rows.
	FamilyID string `gorm:"index"`
	// ReplacedByHash is set on a token when it is rotated, to its successor's
	// hash. A non-empty value marks a token that was legitimately superseded,
	// which lets the refresh use case distinguish a benign multi-tab race from a
	// genuine token replay. Empty by default.
	ReplacedByHash string
	User           User `gorm:"foreignKey:UserID"`
}

// TableName returns the table name for the RefreshToken model
func (RefreshToken) TableName() string {
	return "refresh_tokens"
}
