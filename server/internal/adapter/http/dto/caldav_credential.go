package dto

// CreateCalDAVCredentialRequest is the request for creating a CalDAV credential
type CreateCalDAVCredentialRequest struct {
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	Password   string  `json:"password"`
	Permission string  `json:"permission"`
	ExpiresAt  *string `json:"expires_at"`
}

// CalDAVCredentialResponse represents the credential details
type CalDAVCredentialResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	Permission string  `json:"permission"`
	ExpiresAt  *string `json:"expires_at"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	LastUsedIP *string `json:"last_used_ip"`
}

// CalDAVCredentialListResponse wraps the list of credentials
type CalDAVCredentialListResponse struct {
	Credentials []CalDAVCredentialResponse `json:"credentials"`
}
