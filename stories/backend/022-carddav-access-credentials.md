# Story 022: CardDAV Access Credentials

## Title
Implement CardDAV Access Credentials for Contact Sync

## Description
As a user, I want to create dedicated credentials for CardDAV access so that I can sync contacts without using my main account password, especially when using OAuth/SAML authentication.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AU-2.6.1 | Users can create CardDAV access credentials |
| AU-2.6.2 | CardDAV credentials consist of a custom username and auto-generated password |
| AU-2.6.3 | Users can set a custom username for CardDAV credentials |
| AU-2.6.4 | CardDAV credentials password is displayed only once upon creation |
| AU-2.6.5 | Users can have multiple CardDAV credential sets |
| AU-2.6.6 | Users can name/label each CardDAV credential set |
| AU-2.6.7 | Users can set CardDAV credentials as read-only or read-write |
| AU-2.6.8 | CardDAV credentials work independently of user's main auth method |
| AU-2.6.9 | CardDAV credentials grant access to all user's address books |
| AU-2.6.10 | Users can view, track last-used, and revoke CardDAV credentials |

## Acceptance Criteria

### Create CardDAV Credential

- [ ] REST endpoint `POST /api/v1/carddav-credentials` (requires auth)
- [ ] Request body:
  ```json
  {
    "name": "Phone Contacts Sync",
    "username": "contacts-sync",
    "permission": "read-write",
    "expires_at": null
  }
  ```
- [ ] Name is required, max 100 characters
- [ ] Username is required, 3-50 characters, alphanumeric + hyphen/underscore
- [ ] Username must be unique across all CardDAV credentials (globally)
- [ ] Permission: `read` or `read-write` (default: `read-write`)
- [ ] Expires_at is optional (null = never expires)
- [ ] Generates secure random password (24 characters)
- [ ] Password hashed with bcrypt before storage
- [ ] Returns password in response (only time visible)
- [ ] Returns 201 Created

### List CardDAV Credentials

- [ ] REST endpoint `GET /api/v1/carddav-credentials` (requires auth)
- [ ] Returns list of credentials (without password values):
  - [ ] ID (UUID)
  - [ ] Name
  - [ ] Username
  - [ ] Permission level
  - [ ] Created date
  - [ ] Expires at (null if never)
  - [ ] Last used date (null if never used)
  - [ ] Last used IP (null if never used)

### Revoke CardDAV Credential

- [ ] REST endpoint `DELETE /api/v1/carddav-credentials/{id}` (requires auth)
- [ ] Soft-deletes the credential (sets revoked_at)
- [ ] Subsequent authentication attempts fail
- [ ] Returns 204 No Content
- [ ] Returns 404 if credential not found or belongs to another user

### HTTP Basic Auth for CardDAV

- [ ] CardDAV endpoints accept HTTP Basic Auth with CardDAV credential username + password
- [ ] Authentication flow:
  1. Parse Basic Auth header
  2. Look up CardDAV credential by username
  3. Verify password against bcrypt hash
  4. Check not revoked and not expired
  5. Set user context and permission level
- [ ] Read-only credentials:
  - [ ] Allow: GET, PROPFIND, REPORT, OPTIONS
  - [ ] Deny: PUT, DELETE, MKCOL, PROPPATCH (return 403)
- [ ] Last used timestamp and IP updated on successful auth
- [ ] Revoked/expired credentials return 401 Unauthorized

## Technical Notes

### Database Model
```go
type CardDAVCredential struct {
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

func (c *CardDAVCredential) IsValid() bool {
    if c.RevokedAt != nil {
        return false
    }
    if c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt) {
        return false
    }
    return true
}

func (c *CardDAVCredential) CanWrite() bool {
    return c.Permission == "read-write"
}
```

### Shared Credential Logic

Since CalDAV and CardDAV credentials have identical structures, consider a shared approach:

```go
// Generic DAV credential with type discriminator
type DAVCredential struct {
    ID           uint           `gorm:"primaryKey"`
    UUID         string         `gorm:"uniqueIndex;size:36;not null"`
    UserID       uint           `gorm:"index;not null"`
    Type         string         `gorm:"size:10;index;not null"` // "caldav" or "carddav"
    Name         string         `gorm:"size:100;not null"`
    Username     string         `gorm:"uniqueIndex;size:50;not null"`
    PasswordHash string         `gorm:"size:255;not null"`
    Permission   string         `gorm:"size:20;not null"`
    ExpiresAt    *time.Time     `gorm:"index"`
    LastUsedAt   *time.Time
    LastUsedIP   string         `gorm:"size:45"`
    CreatedAt    time.Time
    RevokedAt    *time.Time     `gorm:"index"`
    User         User           `gorm:"foreignKey:UserID"`
}

// Username uniqueness per type
// CREATE UNIQUE INDEX idx_dav_cred_username ON dav_credentials(type, username) WHERE revoked_at IS NULL;
```

### Code Structure
```
internal/domain/credential/
├── dav_credential.go       # Shared entity (or separate caldav/carddav)
└── repository.go           # Repository interface

internal/usecase/credential/
├── create_carddav.go       # Create CardDAV credential
├── list_carddav.go         # List CardDAV credentials
└── revoke_carddav.go       # Revoke CardDAV credential

internal/adapter/http/
└── carddav_credential_handler.go  # HTTP handlers

internal/adapter/auth/
└── carddav_basic_auth.go   # CardDAV Basic Auth middleware
```

### CardDAV Auth Middleware
```go
func CardDAVBasicAuthMiddleware(credentialRepo credential.Repository, userRepo user.Repository) fiber.Handler {
    return func(c fiber.Ctx) error {
        username, password, ok := parseBasicAuth(c.Get("Authorization"))
        if !ok {
            return c.Status(401).JSON(fiber.Map{
                "error": "Authentication required",
            })
        }

        // Find credential by username
        cred, err := credentialRepo.FindCardDAVByUsername(ctx, username)
        if err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
        }

        // Verify password
        if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(password)); err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
        }

        // Check valid
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
        c.Locals("auth_method", "carddav_credential")
        c.Locals("carddav_credential", cred)
        c.Locals("can_write", cred.CanWrite())

        return c.Next()
    }
}
```

## API Response Examples

### Create CardDAV Credential (201 Created)
```json
{
  "id": "990e8400-e29b-41d4-a716-446655440001",
  "name": "Phone Contacts Sync",
  "username": "contacts-sync",
  "permission": "read-write",
  "expires_at": null,
  "created_at": "2024-01-21T10:00:00Z",
  "password": "mP3nQ8rS5tU2vX4yZ6aB9cD1",
  "carddav_url": "https://caldav.example.com/dav/addressbooks/",
  "usage_instructions": "Use this username and password for CardDAV Basic Authentication"
}
```

### List CardDAV Credentials (200 OK)
```json
{
  "credentials": [
    {
      "id": "990e8400-e29b-41d4-a716-446655440001",
      "name": "Phone Contacts Sync",
      "username": "contacts-sync",
      "permission": "read-write",
      "expires_at": null,
      "created_at": "2024-01-21T10:00:00Z",
      "last_used_at": "2024-01-22T15:30:00Z",
      "last_used_ip": "192.168.1.100"
    },
    {
      "id": "990e8400-e29b-41d4-a716-446655440002",
      "name": "Backup Export",
      "username": "contacts-backup",
      "permission": "read",
      "expires_at": "2024-06-30T23:59:59Z",
      "created_at": "2024-01-20T09:00:00Z",
      "last_used_at": null,
      "last_used_ip": null
    }
  ]
}
```

### Read-Only Write Attempt (403)
```json
{
  "error": "forbidden",
  "message": "This credential has read-only access to contacts"
}
```

## Authentication Priority

When a Basic Auth request comes to DAV endpoints, the server should check credentials in order:

1. **App Password** (from Story 010) - username is user's main username
2. **CalDAV Credential** (for /dav/calendars/) - if path is CalDAV
3. **CardDAV Credential** (for /dav/addressbooks/) - if path is CardDAV

```go
func DAVAuthMiddleware(/* repos */) fiber.Handler {
    return func(c fiber.Ctx) error {
        username, password, ok := parseBasicAuth(c.Get("Authorization"))
        if !ok {
            return c.Status(401).JSON(fiber.Map{"error": "Authentication required"})
        }

        path := c.Path()

        // Try app password first (uses main username)
        if user, appPwd, err := tryAppPassword(ctx, username, password); err == nil {
            // Check scope
            if strings.HasPrefix(path, "/dav/calendars/") && !appPwd.HasScope("caldav") {
                return c.Status(403).JSON(fiber.Map{"error": "No CalDAV access"})
            }
            if strings.HasPrefix(path, "/dav/addressbooks/") && !appPwd.HasScope("carddav") {
                return c.Status(403).JSON(fiber.Map{"error": "No CardDAV access"})
            }
            setUserContext(c, user, "app_password", true)
            return c.Next()
        }

        // Try CalDAV credential
        if strings.HasPrefix(path, "/dav/calendars/") {
            if user, cred, err := tryCalDAVCredential(ctx, username, password); err == nil {
                setUserContext(c, user, "caldav_credential", cred.CanWrite())
                return c.Next()
            }
        }

        // Try CardDAV credential
        if strings.HasPrefix(path, "/dav/addressbooks/") {
            if user, cred, err := tryCardDAVCredential(ctx, username, password); err == nil {
                setUserContext(c, user, "carddav_credential", cred.CanWrite())
                return c.Next()
            }
        }

        return c.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
    }
}
```

## Definition of Done

- [ ] `POST /api/v1/carddav-credentials` creates new credential
- [ ] Password displayed only once on creation
- [ ] `GET /api/v1/carddav-credentials` lists credentials without passwords
- [ ] `DELETE /api/v1/carddav-credentials/{id}` revokes credential
- [ ] CardDAV endpoints accept Basic Auth with credential username/password
- [ ] Read-only credentials cannot modify contacts
- [ ] Expired credentials are rejected
- [ ] Revoked credentials are rejected
- [ ] Last used timestamp and IP tracked
- [ ] Works independently of OAuth/SAML auth
- [ ] Auth priority: App Password > CalDAV/CardDAV Credential
- [ ] Unit tests for credential validation
- [ ] Integration tests for CardDAV auth flow
