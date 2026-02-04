package dto

// CreateCardDAVCredentialRequest is the request for creating a CardDAV credential
type CreateCardDAVCredentialRequest struct {
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	Password   string  `json:"password"`
	Permission string  `json:"permission"`
	ExpiresAt  *string `json:"expires_at"`
}

// CardDAVCredentialResponse represents the credential details
type CardDAVCredentialResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	Permission string  `json:"permission"`
	ExpiresAt  *string `json:"expires_at"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	LastUsedIP *string `json:"last_used_ip"`
}

// CardDAVCredentialListResponse wraps the list of credentials
type CardDAVCredentialListResponse struct {
	Credentials []CardDAVCredentialResponse `json:"credentials"`
}
