package dto

import "time"

// UserProfileResponse represents the user profile information
type UserProfileResponse struct {
	ID            string           `json:"id"`
	Email         string           `json:"email"`
	DisplayName   string           `json:"display_name"`
	IsActive      bool             `json:"is_active"`
	EmailVerified bool             `json:"email_verified"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	AuthMethods   []string         `json:"auth_methods"`
	Stats         UserProfileStats `json:"stats"`
}

// UserProfileStats represents resource counts for a user
type UserProfileStats struct {
	CalendarCount    int `json:"calendar_count"`
	ContactCount     int `json:"contact_count"`
	AppPasswordCount int `json:"app_password_count"`
}

// UpdateProfileRequest represents the request body for updating profile
type UpdateProfileRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
}

// DeleteAccountRequest represents the request body for account deletion
type DeleteAccountRequest struct {
	Password     string `json:"password"`
	Confirmation string `json:"confirmation"`
}
