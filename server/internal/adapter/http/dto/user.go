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

// CreateAppPasswordRequest represents the request to create an app password
type CreateAppPasswordRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// CreateAppPasswordResponse represents the response after creating an app password
type CreateAppPasswordResponse struct {
	ID          string                         `json:"id"`
	Name        string                         `json:"name"`
	Scopes      []string                       `json:"scopes"`
	CreatedAt   string                         `json:"created_at"`
	Password    string                         `json:"password"`
	Credentials AppPasswordCredentialsResponse `json:"credentials"`
}

// AppPasswordCredentialsResponse contains formatted credentials for the user
type AppPasswordCredentialsResponse struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	ServerURL string `json:"server_url"`
}

// AppPasswordResponse represents an app password in a list
type AppPasswordResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at"`
	LastUsedIP *string  `json:"last_used_ip"`
}

// ListAppPasswordsResponse represents the list response
type ListAppPasswordsResponse struct {
	AppPasswords []AppPasswordResponse `json:"app_passwords"`
}

// ChangePasswordRequest represents variable for change password
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
