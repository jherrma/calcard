package user

import (
	"encoding/json"
	"time"
)

// AppPassword represents an app-specific password for DAV clients
type AppPassword struct {
	ID           uint   `gorm:"primaryKey"`
	UUID         string `gorm:"uniqueIndex;size:36;not null"`
	UserID       uint   `gorm:"index;not null"`
	Name         string `gorm:"size:100;not null"`
	PasswordHash string `gorm:"size:255;not null"`
	Scopes       string `gorm:"size:255;not null"` // JSON array of strings: ["caldav", "carddav"]
	LastUsedAt   *time.Time
	LastUsedIP   string `gorm:"size:45"`
	CreatedAt    time.Time
	RevokedAt    *time.Time `gorm:"index"`
	User         User       `gorm:"foreignKey:UserID"`
}

// TableName returns the table name for the AppPassword model
func (AppPassword) TableName() string {
	return "app_passwords"
}

// HasScope checks if the app password has the given scope
func (a *AppPassword) HasScope(scope string) bool {
	var scopes []string
	if err := json.Unmarshal([]byte(a.Scopes), &scopes); err != nil {
		return false
	}
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// GetScopes returns the scopes as a string slice
func (a *AppPassword) GetScopes() []string {
	var scopes []string
	if err := json.Unmarshal([]byte(a.Scopes), &scopes); err != nil {
		return []string{}
	}
	return scopes
}

// IsRevoked checks if the app password is revoked
func (a *AppPassword) IsRevoked() bool {
	return a.RevokedAt != nil
}
