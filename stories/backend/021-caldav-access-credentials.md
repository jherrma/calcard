# Story 021: CalDAV Access Credentials

## Title
Implement CalDAV Access Credentials for Calendar Sync

## Description
As a user, I want to create dedicated credentials for CalDAV access so that I can share calendar URLs or sync calendars without using my main account password, especially when using OAuth/SAML authentication.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AU-2.5.1 | Users can create CalDAV access credentials |
| AU-2.5.2 | CalDAV credentials consist of a custom username and auto-generated password |
| AU-2.5.3 | Users can set a custom username for CalDAV credentials |
| AU-2.5.4 | CalDAV credentials password is displayed only once upon creation |
| AU-2.5.5 | Users can have multiple CalDAV credential sets |
| AU-2.5.6 | Users can name/label each credential set |
| AU-2.5.7 | Users can set credentials as read-only or read-write |
| AU-2.5.8 | CalDAV credentials work independently of user's main auth method |
| AU-2.5.9 | CalDAV credentials grant access to all user's calendars |
| AU-2.5.10 | Users can view list of CalDAV credentials with creation dates |
| AU-2.5.11 | Users can see last-used date for each CalDAV credential |
| AU-2.5.12 | Users can revoke individual CalDAV credentials |
| AU-2.5.13 | CalDAV credentials can have optional expiration date |

## Acceptance Criteria

### Create CalDAV Credential

- [ ] REST endpoint `POST /api/v1/caldav-credentials` (requires auth)
- [ ] Request body:
  ```json
  {
    "name": "Google Calendar Import",
    "username": "calendar-sync",
    "permission": "read",
    "expires_at": "2024-12-31T23:59:59Z"
  }
  ```
- [ ] Name is required, max 100 characters
- [ ] Username is required, 3-50 characters, alphanumeric + hyphen/underscore
- [ ] Username must be unique across all CalDAV credentials (globally)
- [ ] Permission: `read` or `read-write` (default: `read-write`)
- [ ] Expires_at is optional (null = never expires)
- [ ] Generates secure random password (24 characters)
- [ ] Password hashed with bcrypt before storage
- [ ] Returns password in response (only time visible)
- [ ] Returns 201 Created

### List CalDAV Credentials

- [ ] REST endpoint `GET /api/v1/caldav-credentials` (requires auth)
- [ ] Returns list of credentials (without password values):
  - [ ] ID (UUID)
  - [ ] Name
  - [ ] Username
  - [ ] Permission level
  - [ ] Created date
  - [ ] Expires at (null if never)
  - [ ] Last used date (null if never used)
  - [ ] Last used IP (null if never used)

### Revoke CalDAV Credential

- [ ] REST endpoint `DELETE /api/v1/caldav-credentials/{id}` (requires auth)
- [ ] Soft-deletes the credential (sets revoked_at)
- [ ] Subsequent authentication attempts fail
- [ ] Returns 204 No Content
- [ ] Returns 404 if credential not found or belongs to another user

### HTTP Basic Auth for CalDAV

- [ ] CalDAV endpoints accept HTTP Basic Auth with CalDAV credential username + password
- [ ] Authentication flow:
  1. Parse Basic Auth header
  2. Look up CalDAV credential by username
  3. Verify password against bcrypt hash
  4. Check not revoked and not expired
  5. Set user context and permission level
- [ ] Read-only credentials:
  - [ ] Allow: GET, PROPFIND, REPORT, OPTIONS
  - [ ] Deny: PUT, DELETE, MKCALENDAR, PROPPATCH (return 403)
- [ ] Last used timestamp and IP updated on successful auth
- [ ] Revoked/expired credentials return 401 Unauthorized

## Technical Notes

### Database Model
```go
type CalDAVCredential struct {
    ID           uint           `gorm:"primaryKey"`
    UUID         string         `gorm:"uniqueIndex;size:36;not null"`
    UserID       uint           `gorm:"index;not null"`
    Name         string         `gorm:"size:100;not null"`
    Username     string         `gorm:"uniqueIndex;size:50;not null"`
    PasswordHash string         `gorm:"size:255;not null"`
    Permission   string         `gorm:"size:20;not null"` // "read" or "read-write"
    ExpiresAt    *time.Time     `gorm:"index"`
    LastUsedAt   *time.Time
    LastUsedIP   string         `gorm:"size:45"`
    CreatedAt    time.Time
    RevokedAt    *time.Time     `gorm:"index"`
    User         User           `gorm:"foreignKey:UserID"`
}

func (c *CalDAVCredential) IsValid() bool {
    if c.RevokedAt != nil {
        return false
    }
    if c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt) {
        return false
    }
    return true
}

func (c *CalDAVCredential) CanWrite() bool {
    return c.Permission == "read-write"
}
```

### Code Structure
```
internal/domain/credential/
├── caldav_credential.go    # Entity
└── repository.go           # Repository interface

internal/usecase/credential/
├── create_caldav.go        # Create credential
├── list_caldav.go          # List credentials
└── revoke_caldav.go        # Revoke credential

internal/adapter/http/
└── caldav_credential_handler.go  # HTTP handlers

internal/adapter/auth/
└── caldav_basic_auth.go    # CalDAV Basic Auth middleware
```

### CalDAV Auth Middleware
```go
func CalDAVBasicAuthMiddleware(credentialRepo credential.Repository, userRepo user.Repository) fiber.Handler {
    return func(c fiber.Ctx) error {
        username, password, ok := parseBasicAuth(c.Get("Authorization"))
        if !ok {
            return c.Status(401).JSON(fiber.Map{
                "error": "Authentication required",
            })
        }

        // Find credential by username
        cred, err := credentialRepo.FindCalDAVByUsername(ctx, username)
        if err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
        }

        // Verify password
        if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(password)); err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
        }

        // Check valid (not revoked, not expired)
        if !cred.IsValid() {
            return c.Status(401).JSON(fiber.Map{"error": "Credentials expired or revoked"})
        }

        // Get user
        user, err := userRepo.FindByID(ctx, cred.UserID)
        if err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
        }

        // Update last used (async)
        go credentialRepo.UpdateLastUsed(ctx, cred.ID, c.IP())

        // Set context
        c.Locals("user", user)
        c.Locals("auth_method", "caldav_credential")
        c.Locals("caldav_credential", cred)
        c.Locals("can_write", cred.CanWrite())

        return c.Next()
    }
}
```

### Write Permission Check Middleware
```go
func RequireWritePermission() fiber.Handler {
    return func(c fiber.Ctx) error {
        canWrite, ok := c.Locals("can_write").(bool)
        if !ok || !canWrite {
            return c.Status(403).JSON(fiber.Map{
                "error": "forbidden",
                "message": "This credential has read-only access",
            })
        }
        return c.Next()
    }
}

// Usage in routes
caldav.Put("/:calendar/:event", RequireWritePermission(), handler.PutEvent)
caldav.Delete("/:calendar/:event", RequireWritePermission(), handler.DeleteEvent)
```

## API Response Examples

### Create CalDAV Credential (201 Created)
```json
{
  "id": "880e8400-e29b-41d4-a716-446655440001",
  "name": "Google Calendar Import",
  "username": "calendar-sync",
  "permission": "read",
  "expires_at": "2024-12-31T23:59:59Z",
  "created_at": "2024-01-21T10:00:00Z",
  "password": "xK9mN2pL4qR7sT1vW3yZ5bD8",
  "caldav_url": "https://caldav.example.com/dav/calendars/",
  "usage_instructions": "Use this username and password for CalDAV Basic Authentication"
}
```

### List CalDAV Credentials (200 OK)
```json
{
  "credentials": [
    {
      "id": "880e8400-e29b-41d4-a716-446655440001",
      "name": "Google Calendar Import",
      "username": "calendar-sync",
      "permission": "read",
      "expires_at": "2024-12-31T23:59:59Z",
      "created_at": "2024-01-21T10:00:00Z",
      "last_used_at": "2024-01-22T15:30:00Z",
      "last_used_ip": "192.168.1.100"
    },
    {
      "id": "880e8400-e29b-41d4-a716-446655440002",
      "name": "Thunderbird Sync",
      "username": "tb-calendars",
      "permission": "read-write",
      "expires_at": null,
      "created_at": "2024-01-20T09:00:00Z",
      "last_used_at": null,
      "last_used_ip": null
    }
  ]
}
```

### Username Conflict (409)
```json
{
  "error": "conflict",
  "message": "Username 'calendar-sync' is already in use"
}
```

### Read-Only Write Attempt (403)
```json
{
  "error": "forbidden",
  "message": "This credential has read-only access"
}
```

## Definition of Done

- [ ] `POST /api/v1/caldav-credentials` creates new credential
- [ ] Password displayed only once on creation
- [ ] `GET /api/v1/caldav-credentials` lists credentials without passwords
- [ ] `DELETE /api/v1/caldav-credentials/{id}` revokes credential
- [ ] CalDAV endpoints accept Basic Auth with credential username/password
- [ ] Read-only credentials cannot modify calendars
- [ ] Expired credentials are rejected
- [ ] Revoked credentials are rejected
- [ ] Last used timestamp and IP tracked
- [ ] Works independently of OAuth/SAML auth
- [ ] Unit tests for credential validation
- [ ] Integration tests for CalDAV auth flow
