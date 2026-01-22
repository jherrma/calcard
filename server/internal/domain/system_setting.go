package domain

import (
	"time"
)

// SystemSetting represents a persistent system-level configuration
type SystemSetting struct {
	Key       string `gorm:"primaryKey;size:255"`
	Value     string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for the SystemSetting model
func (SystemSetting) TableName() string {
	return "system_settings"
}
