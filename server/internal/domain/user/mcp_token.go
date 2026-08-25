package user

import (
	"time"
)

// MCPToken is a long-lived bearer credential for the MCP endpoint (story 104).
//
// It is deliberately NOT an app password or a DAV credential. Those authorize
// the DAV protocol surface and are verified with bcrypt against a username the
// client also sends. An MCP client sends one opaque bearer string and nothing
// else, so the token must be findable from that string alone — which bcrypt,
// being salted per row, cannot do without scanning every row. MCPToken instead
// stores the SHA-256 of a 256-bit random token: unique-indexed, one lookup, and
// safe without a slow KDF precisely because the secret is full-entropy random
// rather than a user-chosen password.
//
// TokenPrefix is the only part of the secret kept in the clear. It exists so a
// token that is shown exactly once at creation can still be recognized in the
// settings list ("which of these three is the one in my laptop's config?").
type MCPToken struct {
	ID     uint   `gorm:"primaryKey" json:"-"`
	UUID   string `gorm:"uniqueIndex;size:36;not null" json:"id"`
	UserID uint   `gorm:"index;not null" json:"-"`
	Name   string `gorm:"size:100;not null" json:"name"`
	// TokenHash is hex(sha256(raw token)). Unique so a lookup by hash is a
	// single indexed read and a (astronomically unlikely) collision is a
	// database error rather than an ambiguous match.
	TokenHash   string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	TokenPrefix string     `gorm:"size:32;not null" json:"token_prefix"`
	ExpiresAt   *time.Time `gorm:"index" json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	LastUsedIP  string     `gorm:"size:45" json:"last_used_ip"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `gorm:"index" json:"-"`
	User        User       `gorm:"foreignKey:UserID" json:"-"`
}

// TableName returns the table name for the MCPToken model
func (MCPToken) TableName() string {
	return "mcp_tokens"
}

// IsRevoked reports whether the token has been revoked.
func (t *MCPToken) IsRevoked() bool {
	return t.RevokedAt != nil
}

// IsExpired reports whether the token's expiry has passed. A nil ExpiresAt
// means the token never expires.
func (t *MCPToken) IsExpired() bool {
	if t.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.ExpiresAt)
}

// IsValid reports whether the token may still be used to authenticate.
func (t *MCPToken) IsValid() bool {
	return !t.IsRevoked() && !t.IsExpired()
}
