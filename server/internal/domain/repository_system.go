package domain

import "context"

// SystemSettingRepository defines the interface for system settings persistence
type SystemSettingRepository interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}
