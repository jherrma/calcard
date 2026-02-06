# Story 102: LDAP Authentication

## Title

Implement LDAP/Active Directory Authentication

## Description

As an enterprise user, I want to login using my LDAP or Active Directory credentials so that I can authenticate with my organization's existing directory service without creating a separate password.

## Related Acceptance Criteria

| ID       | Criterion                                                   |
| -------- | ----------------------------------------------------------- |
| AU-2.4.1 | Users can login via LDAP/Active Directory                   |
| AU-2.4.2 | LDAP connection supports TLS/StartTLS                       |
| AU-2.4.3 | LDAP bind authentication validates credentials              |
| AU-2.4.4 | User attributes are synced from LDAP directory              |
| AU-2.4.5 | LDAP groups can be mapped to application permissions        |
| AU-2.4.6 | LDAP user accounts are created automatically on first login |
| AU-2.4.7 | User profiles are updated on each LDAP login                |

## Acceptance Criteria

### LDAP Login

- [ ] REST endpoint `POST /api/v1/auth/ldap/login` (unauthenticated)
- [ ] Request body:
  ```json
  {
    "username": "jdoe",
    "password": "********"
  }
  ```
- [ ] Connects to configured LDAP server
- [ ] Searches for user by username or email
- [ ] Attempts bind with user DN and provided password
- [ ] If bind succeeds:
  - [ ] Retrieves user attributes from LDAP
  - [ ] If user exists locally with LDAP link: Update profile from LDAP
  - [ ] If no user exists: Create new user account
  - [ ] Mark `email_verified: true` (LDAP directory verified)
  - [ ] Create or update LDAP connection record
  - [ ] Return JWT tokens
- [ ] If bind fails:
  - [ ] Return 401 Unauthorized

### LDAP User Search

- [ ] Search base DN configurable (e.g., `ou=users,dc=example,dc=com`)
- [ ] Search filter configurable with placeholder:
  - [ ] Default: `(&(objectClass=person)(uid={username}))`
  - [ ] Active Directory: `(&(objectClass=user)(sAMAccountName={username}))`
  - [ ] Email search: `(&(objectClass=person)(mail={username}))`
- [ ] Support both `{username}` and `{email}` placeholders

### Attribute Mapping

- [ ] Configurable attribute mapping for:
  - [ ] Email: `mail`, `userPrincipalName`
  - [ ] Display Name: `displayName`, `cn`
  - [ ] First Name: `givenName`
  - [ ] Last Name: `sn`, `surname`
  - [ ] Username: `uid`, `sAMAccountName`
- [ ] Fallback to standard LDAP attribute names if not configured

### LDAP Group Mapping (Optional)

- [ ] Retrieve user's group memberships from LDAP
- [ ] Support `memberOf` attribute (Active Directory)
- [ ] Support group search queries for other LDAP servers
- [ ] Map LDAP groups to application roles:
  ```yaml
  ldap:
    group_mapping:
      cn=admins,ou=groups,dc=example,dc=com: admin
      cn=users,ou=groups,dc=example,dc=com: user
  ```
- [ ] Update user roles on each login

### Profile Synchronization

- [ ] On each LDAP login:
  - [ ] Update email if changed
  - [ ] Update display name if changed
  - [ ] Update group memberships/roles if changed
- [ ] Preserve local preferences and settings

### LDAP Connection Management

- [ ] REST endpoint `GET /api/v1/auth/methods` includes LDAP if configured
- [ ] Response includes LDAP availability:
  ```json
  {
    "methods": [
      {
        "id": "ldap",
        "type": "ldap",
        "name": "Corporate Login",
        "enabled": true
      },
      {
        "id": "local",
        "type": "local",
        "name": "Local Account",
        "enabled": true
      }
    ]
  }
  ```

## Technical Notes

### New Configuration

```bash
# LDAP Connection
CALDAV_LDAP_ENABLED=true
CALDAV_LDAP_HOST=ldap.example.com
CALDAV_LDAP_PORT=389
CALDAV_LDAP_USE_TLS=true
CALDAV_LDAP_START_TLS=false
CALDAV_LDAP_SKIP_TLS_VERIFY=false  # For dev only

# LDAP Bind
CALDAV_LDAP_BIND_DN=cn=readonly,dc=example,dc=com
CALDAV_LDAP_BIND_PASSWORD=secretpassword

# LDAP Search
CALDAV_LDAP_BASE_DN=ou=users,dc=example,dc=com
CALDAV_LDAP_SEARCH_FILTER=(uid={username})
CALDAV_LDAP_USERNAME_ATTRIBUTES=uid,sAMAccountName,mail

# Attribute Mapping
CALDAV_LDAP_ATTR_EMAIL=mail
CALDAV_LDAP_ATTR_DISPLAY_NAME=displayName
CALDAV_LDAP_ATTR_FIRST_NAME=givenName
CALDAV_LDAP_ATTR_LAST_NAME=sn

# Group Mapping (optional)
CALDAV_LDAP_GROUP_SEARCH_BASE=ou=groups,dc=example,dc=com
CALDAV_LDAP_GROUP_FILTER=(member={dn})
CALDAV_LDAP_ADMIN_GROUP=cn=admins,ou=groups,dc=example,dc=com
```

### Database Model

```go
// Reuse existing OAuthConnection table
type OAuthConnection struct {
    ID            uint      `gorm:"primaryKey"`
    UserID        uint      `gorm:"index;not null"`
    Provider      string    `gorm:"size:50;not null"`  // "ldap"
    ProviderID    string    `gorm:"size:255;not null"` // LDAP DN
    ProviderEmail string    `gorm:"size:255"`          // LDAP email
    CreatedAt     time.Time
    UpdatedAt     time.Time
    User          User      `gorm:"foreignKey:UserID"`
}

// Unique constraint on (provider='ldap', provider_id)
```

### LDAP Service

```go
type LDAPConfig struct {
    Host             string
    Port             int
    UseTLS           bool
    StartTLS         bool
    SkipTLSVerify    bool
    BindDN           string
    BindPassword     string
    BaseDN           string
    SearchFilter     string
    Attributes       AttributeMapping
    GroupSearchBase  string
    GroupFilter      string
    AdminGroup       string
}

type AttributeMapping struct {
    Email       string
    DisplayName string
    FirstName   string
    LastName    string
    Username    string
}

type LDAPService struct {
    config LDAPConfig
}

func (s *LDAPService) Authenticate(username, password string) (*LDAPUser, error) {
    // 1. Connect to LDAP server
    // 2. Bind with service account
    // 3. Search for user
    // 4. Attempt bind with user DN and password
    // 5. If successful, retrieve attributes
    // 6. Retrieve group memberships
    // 7. Return user data
}

type LDAPUser struct {
    DN          string
    Username    string
    Email       string
    DisplayName string
    FirstName   string
    LastName    string
    Groups      []string
}
```

### Dependencies

```go
github.com/go-ldap/ldap/v3  // LDAP client library
```

### Code Structure

```
internal/domain/auth/
└── ldap_user.go            # LDAP user entity

internal/usecase/auth/
├── ldap_login.go           # LDAP login use case
└── ldap_sync.go            # LDAP profile sync

internal/adapter/auth/
├── ldap_service.go         # LDAP connection and operations
└── ldap_config.go          # LDAP configuration loading

internal/adapter/http/
└── ldap_handler.go         # LDAP login endpoint
```

## API Response Examples

### LDAP Login Success - New User (201 Created)

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4...",
  "token_type": "Bearer",
  "expires_in": 900,
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "jdoe@example.com",
    "username": "jdoe",
    "display_name": "John Doe",
    "auth_method": "ldap",
    "is_new": true
  }
}
```

### LDAP Login Success - Existing User (200 OK)

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4...",
  "token_type": "Bearer",
  "expires_in": 900,
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "jdoe@example.com",
    "username": "jdoe",
    "display_name": "John Doe",
    "auth_method": "ldap"
  }
}
```

### LDAP Authentication Failed (401)

```json
{
  "error": "unauthorized",
  "message": "Invalid LDAP credentials"
}
```

### LDAP Server Unreachable (503)

```json
{
  "error": "service_unavailable",
  "message": "LDAP server is unreachable. Please try again later."
}
```

### LDAP Not Configured (501)

```json
{
  "error": "not_implemented",
  "message": "LDAP authentication is not configured"
}
```

## LDAP Server Configuration Examples

### OpenLDAP

```bash
CALDAV_LDAP_HOST=ldap.example.com
CALDAV_LDAP_PORT=389
CALDAV_LDAP_START_TLS=true
CALDAV_LDAP_BASE_DN=ou=users,dc=example,dc=com
CALDAV_LDAP_SEARCH_FILTER=(uid={username})
CALDAV_LDAP_ATTR_EMAIL=mail
CALDAV_LDAP_ATTR_DISPLAY_NAME=cn
```

### Active Directory

```bash
CALDAV_LDAP_HOST=dc.example.com
CALDAV_LDAP_PORT=389
CALDAV_LDAP_USE_TLS=true
CALDAV_LDAP_BASE_DN=DC=example,DC=com
CALDAV_LDAP_SEARCH_FILTER=(&(objectClass=user)(sAMAccountName={username}))
CALDAV_LDAP_ATTR_EMAIL=userPrincipalName
CALDAV_LDAP_ATTR_DISPLAY_NAME=displayName
CALDAV_LDAP_ATTR_FIRST_NAME=givenName
CALDAV_LDAP_ATTR_LAST_NAME=sn
```

### FreeIPA

```bash
CALDAV_LDAP_HOST=ipa.example.com
CALDAV_LDAP_PORT=389
CALDAV_LDAP_START_TLS=true
CALDAV_LDAP_BASE_DN=cn=users,cn=accounts,dc=example,dc=com
CALDAV_LDAP_SEARCH_FILTER=(uid={username})
CALDAV_LDAP_ATTR_EMAIL=mail
CALDAV_LDAP_ATTR_DISPLAY_NAME=displayName
```

## Security Considerations

1. **TLS/StartTLS**: Always use encrypted connections in production
2. **Bind Credentials**: Store LDAP bind password securely (encrypted environment variable)
3. **Connection Pooling**: Reuse LDAP connections efficiently
4. **Timeout Configuration**: Set appropriate connection and search timeouts
5. **Certificate Validation**: Validate TLS certificates (disable only for dev)
6. **Rate Limiting**: Protect against brute force attacks on LDAP login
7. **Password Handling**: Never log or store LDAP passwords
8. **DN Injection**: Sanitize username input to prevent LDAP injection attacks

## Definition of Done

- [ ] `POST /api/v1/auth/ldap/login` authenticates users via LDAP
- [ ] LDAP connection supports TLS and StartTLS
- [ ] User attributes are synced from LDAP directory on login
- [ ] New users are created automatically on first LDAP login
- [ ] Existing LDAP users can login and profile is updated
- [ ] LDAP configuration supports OpenLDAP, Active Directory, FreeIPA
- [ ] LDAP groups can be mapped to application roles (optional)
- [ ] Connection errors return appropriate HTTP status codes
- [ ] LDAP authentication is only enabled when properly configured
- [ ] Unit tests for LDAP service and authentication logic
- [ ] Integration tests with mock LDAP server
- [ ] Documentation for common LDAP server configurations
