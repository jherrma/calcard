# Story 011: OAuth2/OIDC Authentication

## Title
Implement OAuth2/OpenID Connect Authentication

## Description
As a user, I want to login using my Google, Microsoft, or custom OIDC provider so that I can access the service without creating a separate password.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AU-2.2.1 | Users can login via Google OAuth |
| AU-2.2.2 | Users can login via Microsoft/Azure AD |
| AU-2.2.3 | Users can login via custom OIDC provider |
| AU-2.2.4 | First OAuth login creates new account automatically |
| AU-2.2.5 | Subsequent OAuth logins link to existing account |
| AU-2.2.6 | Users can link multiple OAuth providers to one account |
| AU-2.2.7 | OAuth-only accounts cannot use password login |
| AU-2.2.8 | Users can disconnect OAuth providers (if other auth method exists) |

## Acceptance Criteria

### Initiate OAuth Flow

- [ ] REST endpoint `GET /api/v1/auth/oauth/{provider}` (unauthenticated)
- [ ] Supported providers: `google`, `microsoft`, custom OIDC
- [ ] Generates state parameter (CSRF protection)
- [ ] Stores state in session/cookie with 10 minute expiration
- [ ] Redirects to provider's authorization endpoint
- [ ] Includes scopes: `openid`, `email`, `profile`

### OAuth Callback

- [ ] REST endpoint `GET /api/v1/auth/oauth/{provider}/callback`
- [ ] Validates state parameter (prevents CSRF)
- [ ] Exchanges authorization code for tokens
- [ ] Retrieves user info from provider (sub, email, name)
- [ ] If user exists with this provider link:
  - [ ] Login existing user
  - [ ] Return JWT tokens
- [ ] If no user exists:
  - [ ] Create new user account
  - [ ] Generate username from email (before @)
  - [ ] Mark `email_verified: true` (provider verified)
  - [ ] Create OAuth connection record
  - [ ] Return JWT tokens
- [ ] If authenticated user linking new provider:
  - [ ] Create OAuth connection linked to current user
  - [ ] Redirect to settings page

### Link OAuth Provider (Authenticated)

- [ ] REST endpoint `POST /api/v1/auth/oauth/{provider}/link` (requires auth)
- [ ] Initiates OAuth flow with current user context
- [ ] On callback, links provider to existing account
- [ ] Returns 409 if provider account already linked to another user

### Unlink OAuth Provider

- [ ] REST endpoint `DELETE /api/v1/auth/oauth/{provider}` (requires auth)
- [ ] Requires at least one other auth method (password or other OAuth)
- [ ] Returns 400 if unlinking would leave user with no auth method
- [ ] Removes OAuth connection record
- [ ] Returns 204 No Content

### List Linked Providers

- [ ] REST endpoint `GET /api/v1/auth/oauth/providers` (requires auth)
- [ ] Returns list of linked providers with:
  - [ ] Provider name
  - [ ] Provider email (from OIDC)
  - [ ] Linked date

## Technical Notes

### New Configuration
```
CALDAV_OAUTH_GOOGLE_CLIENT_ID
CALDAV_OAUTH_GOOGLE_CLIENT_SECRET
CALDAV_OAUTH_MICROSOFT_CLIENT_ID
CALDAV_OAUTH_MICROSOFT_CLIENT_SECRET
CALDAV_OAUTH_CUSTOM_ISSUER          (e.g., "https://auth.company.com")
CALDAV_OAUTH_CUSTOM_CLIENT_ID
CALDAV_OAUTH_CUSTOM_CLIENT_SECRET
```

### Database Model
```go
type OAuthConnection struct {
    ID            uint      `gorm:"primaryKey"`
    UserID        uint      `gorm:"index;not null"`
    Provider      string    `gorm:"size:50;not null"`  // google, microsoft, custom
    ProviderID    string    `gorm:"size:255;not null"` // sub claim
    ProviderEmail string    `gorm:"size:255"`
    AccessToken   string    `gorm:"size:2000"`         // encrypted
    RefreshToken  string    `gorm:"size:2000"`         // encrypted
    TokenExpiry   *time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
    User          User      `gorm:"foreignKey:UserID"`
}

// Unique constraint on (provider, provider_id)
```

### Dependencies
```go
github.com/coreos/go-oidc/v3  // OIDC client
golang.org/x/oauth2           // OAuth2 client
```

### Provider Configuration
```go
var googleOIDC = oidc.ProviderConfig{
    Issuer:   "https://accounts.google.com",
    AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
    TokenURL: "https://oauth2.googleapis.com/token",
}

var microsoftOIDC = oidc.ProviderConfig{
    Issuer:   "https://login.microsoftonline.com/common/v2.0",
    AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
    TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
}
```

### Code Structure
```
internal/usecase/auth/
├── oauth_initiate.go      # Start OAuth flow
├── oauth_callback.go      # Handle callback
├── oauth_link.go          # Link/unlink providers
└── oauth_providers.go     # List providers

internal/adapter/auth/
├── oauth.go               # OAuth/OIDC client wrapper
└── providers/
    ├── google.go          # Google-specific config
    ├── microsoft.go       # Microsoft-specific config
    └── custom.go          # Custom OIDC provider
```

## API Response Examples

### List Providers (200 OK)
```json
{
  "providers": [
    {
      "provider": "google",
      "email": "user@gmail.com",
      "linked_at": "2024-01-15T10:00:00Z"
    },
    {
      "provider": "microsoft",
      "email": "user@outlook.com",
      "linked_at": "2024-01-20T14:30:00Z"
    }
  ],
  "has_password": true
}
```

### OAuth Callback Success - New User (201 Created)
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4...",
  "token_type": "Bearer",
  "expires_in": 900,
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "user@gmail.com",
    "username": "user",
    "display_name": "John Doe",
    "is_new": true
  }
}
```

### Unlink Provider - Would Leave No Auth (400)
```json
{
  "error": "bad_request",
  "message": "Cannot unlink provider. You must have at least one authentication method."
}
```

### Link Provider - Already Linked (409)
```json
{
  "error": "conflict",
  "message": "This Google account is already linked to another user"
}
```

## Security Considerations

1. **State Parameter**: Cryptographically random, stored server-side, expires quickly
2. **PKCE**: Use PKCE for authorization code flow (code_challenge, code_verifier)
3. **Token Storage**: Encrypt OAuth tokens at rest
4. **ID Token Validation**: Verify signature, issuer, audience, expiration
5. **Nonce**: Include nonce in authentication request, verify in ID token

## Definition of Done

- [ ] Google OAuth login creates account or logs in
- [ ] Microsoft OAuth login creates account or logs in
- [ ] Custom OIDC provider configurable and functional
- [ ] Users can link multiple providers to one account
- [ ] Users can unlink providers (if other auth exists)
- [ ] State parameter prevents CSRF attacks
- [ ] ID tokens properly validated
- [ ] Unit tests for OAuth flow logic
- [ ] Integration tests with mock OIDC provider
