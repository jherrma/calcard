# Story 010: App Passwords for DAV Clients

## Title
Implement App-Specific Passwords for CalDAV/CardDAV Client Authentication

## Description
As a user, I want to create app-specific passwords so that I can authenticate DAV clients (like DAVx5) without using my main account password, especially when using OAuth/SAML authentication.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AU-2.4.1 | Users can create app-specific passwords |
| AU-2.4.2 | App password is displayed only once upon creation |
| AU-2.4.3 | Users can name app passwords (e.g., "DAVx5 Phone") |
| AU-2.4.4 | Users can view list of app passwords with creation dates |
| AU-2.4.5 | Users can see last-used date for each app password |
| AU-2.4.6 | Users can revoke individual app passwords |
| AU-2.4.7 | App passwords can be scoped (CalDAV-only, CardDAV-only, or both) |
| AU-2.4.8 | App passwords work with HTTP Basic Auth for DAV endpoints |

## Acceptance Criteria

### Create App Password

- [ ] REST endpoint `POST /api/v1/app-passwords` (requires auth)
- [ ] Request body:
  ```json
  {
    "name": "DAVx5 Phone",
    "scopes": ["caldav", "carddav"]
  }
  ```
- [ ] Name is required, max 100 characters
- [ ] Scopes must be non-empty array of: `caldav`, `carddav`
- [ ] Generates secure random password (24 characters, alphanumeric)
- [ ] Password hashed with bcrypt before storage
- [ ] Returns password in response (only time it's visible)
- [ ] Response includes formatted credentials for easy copying

### List App Passwords

- [ ] REST endpoint `GET /api/v1/app-passwords` (requires auth)
- [ ] Returns list of app passwords (without password values):
  - [ ] ID (UUID)
  - [ ] Name
  - [ ] Scopes
  - [ ] Created date
  - [ ] Last used date (null if never used)
  - [ ] Last used IP (null if never used)

### Revoke App Password

- [ ] REST endpoint `DELETE /api/v1/app-passwords/{id}` (requires auth)
- [ ] Soft-deletes the app password (sets revoked_at)
- [ ] Subsequent authentication attempts fail
- [ ] Returns 204 No Content
- [ ] Returns 404 if password not found or belongs to another user

### HTTP Basic Auth for DAV

- [ ] DAV endpoints accept HTTP Basic Auth with username + app password
- [ ] Username is the user's username (not email)
- [ ] Password is verified against app password hashes
- [ ] Scope is checked:
  - [ ] CalDAV endpoints require `caldav` scope
  - [ ] CardDAV endpoints require `carddav` scope
  - [ ] Wrong scope returns 403 Forbidden
- [ ] Last used timestamp and IP updated on successful auth
- [ ] Revoked passwords return 401 Unauthorized

## Technical Notes

### Database Model
```go
type AppPassword struct {
    ID           uint           `gorm:"primaryKey"`
    UUID         string         `gorm:"uniqueIndex;size:36;not null"`
    UserID       uint           `gorm:"index;not null"`
    Name         string         `gorm:"size:100;not null"`
    PasswordHash string         `gorm:"size:255;not null"`
    Scopes       string         `gorm:"size:50;not null"` // JSON array: ["caldav","carddav"]
    LastUsedAt   *time.Time
    LastUsedIP   string         `gorm:"size:45"`
    CreatedAt    time.Time
    RevokedAt    *time.Time     `gorm:"index"`
    User         User           `gorm:"foreignKey:UserID"`
}

func (a *AppPassword) HasScope(scope string) bool
func (a *AppPassword) IsRevoked() bool
```

### Password Generation
```go
// Generate 24-character password (144 bits of entropy)
func generateAppPassword() string {
    const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, 24)
    for i := range b {
        b[i] = charset[secureRandomInt(len(charset))]
    }
    return string(b)
}
```

### Code Structure
```
internal/domain/apppassword/
├── app_password.go       # Entity
└── repository.go         # Repository interface

internal/usecase/apppassword/
├── create.go             # Create app password
├── list.go               # List app passwords
└── revoke.go             # Revoke app password

internal/adapter/http/
└── app_password_handler.go  # HTTP handlers

internal/adapter/auth/
└── basic_auth.go         # HTTP Basic Auth middleware for DAV
```

## API Response Examples

### Create App Password (201 Created)
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "name": "DAVx5 Phone",
  "scopes": ["caldav", "carddav"],
  "created_at": "2024-01-21T10:00:00Z",
  "password": "xK9mN2pL4qR7sT1vW3yZ5bD8",
  "credentials": {
    "username": "johndoe",
    "password": "xK9mN2pL4qR7sT1vW3yZ5bD8",
    "server_url": "https://caldav.example.com"
  }
}
```

### List App Passwords (200 OK)
```json
{
  "app_passwords": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440001",
      "name": "DAVx5 Phone",
      "scopes": ["caldav", "carddav"],
      "created_at": "2024-01-21T10:00:00Z",
      "last_used_at": "2024-01-22T15:30:00Z",
      "last_used_ip": "192.168.1.100"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440002",
      "name": "Thunderbird Work",
      "scopes": ["caldav"],
      "created_at": "2024-01-20T09:00:00Z",
      "last_used_at": null,
      "last_used_ip": null
    }
  ]
}
```

### Revoke (204 No Content)
No response body.

### Basic Auth - Wrong Scope (403 Forbidden)
```json
{
  "error": "forbidden",
  "message": "App password does not have access to CardDAV"
}
```

## DAV Basic Auth Middleware

```go
func BasicAuthMiddleware(appPasswordRepo apppassword.Repository) fiber.Handler {
    return func(c fiber.Ctx) error {
        username, password, ok := parseBasicAuth(c.Get("Authorization"))
        if !ok {
            return c.Status(401).JSON(fiber.Map{
                "error": "Authentication required",
            })
        }

        // Find user by username
        user, err := userRepo.FindByUsername(ctx, username)
        if err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
        }

        // Find valid app password
        appPwd, err := appPasswordRepo.FindValidForUser(ctx, user.ID, password)
        if err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
        }

        // Check scope based on request path
        requiredScope := determineRequiredScope(c.Path())
        if !appPwd.HasScope(requiredScope) {
            return c.Status(403).JSON(fiber.Map{
                "error": "App password does not have required scope",
            })
        }

        // Update last used (async)
        go appPasswordRepo.UpdateLastUsed(ctx, appPwd.ID, c.IP())

        // Set user context
        c.Locals("user", user)
        c.Locals("auth_method", "app_password")
        return c.Next()
    }
}
```

## Definition of Done

- [ ] `POST /api/v1/app-passwords` creates new app password
- [ ] Password displayed only once on creation
- [ ] `GET /api/v1/app-passwords` lists passwords without values
- [ ] Last used timestamp tracked and displayed
- [ ] `DELETE /api/v1/app-passwords/{id}` revokes password
- [ ] DAV endpoints accept Basic Auth with app passwords
- [ ] Scope enforcement works (caldav/carddav)
- [ ] Revoked passwords are rejected
- [ ] Unit tests for password generation and validation
- [ ] Integration tests for create/list/revoke flow
- [ ] Integration test for DAV Basic Auth
