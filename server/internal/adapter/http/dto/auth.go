package dto

import "time"

// RegisterRequest represents the registration request body
type RegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// RegisterResponse represents the registration success response.
//
// With SMTP configured the new account is inert until its address is
// confirmed, and the same response must be returned whether or not the address
// was already taken (P01) — so ID, Username and CreatedAt are left out (they
// would have to be invented for an address that already has an account) and
// only the echoed request fields plus is_active/email_verified=false remain.
// Without SMTP the account is live immediately and every field is filled.
type RegisterResponse struct {
	ID            string     `json:"id,omitempty"`
	Email         string     `json:"email"`
	Username      string     `json:"username,omitempty"`
	DisplayName   string     `json:"display_name"`
	IsActive      bool       `json:"is_active"`
	EmailVerified bool       `json:"email_verified"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
}

// LoginRequest represents the user login request
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse represents the user login response
type LoginResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresAt    int64        `json:"expires_at"`
	User         UserResponse `json:"user"`
}

// UserResponse represents simplified user information in responses
type UserResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

// SetupRequest is the body of the first-run setup call.
type SetupRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	// SetupToken is required only when CALDAV_SETUP_TOKEN is configured.
	SetupToken string `json:"setup_token"`
}
