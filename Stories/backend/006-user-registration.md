# Story 006: User Registration

## Title

Implement User Registration with Email Verification

## Description

As a new user, I want to create an account with email and password so that I can access the CalDAV/CardDAV server.

## Related Acceptance Criteria

| ID       | Criterion                                           |
| -------- | --------------------------------------------------- |
| UM-1.1.1 | Users can create an account with email and password |
| UM-1.1.2 | Email verification is sent upon registration        |
| UM-1.1.3 | Account is not active until email is verified       |
| UM-1.1.4 | Duplicate email addresses are rejected              |
| UM-1.1.5 | Password strength requirements are enforced         |
| UM-1.1.6 | Username uniqueness is enforced                     |

## Acceptance Criteria

- [ ] REST endpoint `POST /api/v1/auth/register` accepts registration requests
- [ ] Request body accepts:
  ```json
  {
    "email": "user@example.com",
    "username": "johndoe",
    "password": "SecurePass123!",
    "display_name": "John Doe"
  }
  ```
- [ ] Email validation:
  - [ ] Valid email format required
  - [ ] Duplicate email returns 409 Conflict
- [ ] Username is generated and 16 letters long
- [ ] Password validation:
  - [ ] Minimum 8 characters
  - [ ] At least one uppercase letter
  - [ ] At least one lowercase letter
  - [ ] At least one digit
  - [ ] At least one special character
  - [ ] Validation errors return specific messages
- [ ] Password hashed with bcrypt (cost factor 12)
- [ ] User created with `is_active: false`, `email_verified: false`
- [ ] UUID generated for user (v4)
- [ ] Verification token generated (32 bytes, hex encoded)
- [ ] Verification token stored with expiration (24 hours)
- [ ] Email sent with verification link (placeholder implementation)
- [ ] Successful registration returns 201 with user info (no password)
- [ ] `GET /api/v1/auth/verify?token={token}` activates account
- [ ] Valid token sets `is_active: true`, `email_verified: true`
- [ ] Expired or invalid token returns 400 Bad Request
- [ ] Token is single-use (deleted after verification)

## Technical Notes

### New Configuration

```
CALDAV_SMTP_HOST         (optional, disables email if not set)
CALDAV_SMTP_PORT         (default: "587")
CALDAV_SMTP_USER
CALDAV_SMTP_PASSWORD
CALDAV_SMTP_FROM         (e.g., "noreply@caldav.example.com")
CALDAV_BASE_URL          (e.g., "https://caldav.example.com")
```

### New Database Table

```go
type EmailVerification struct {
    ID        uint      `gorm:"primaryKey"`
    UserID    uint      `gorm:"index;not null"`
    Token     string    `gorm:"uniqueIndex;size:64;not null"`
    ExpiresAt time.Time `gorm:"index;not null"`
    CreatedAt time.Time
    User      User      `gorm:"foreignKey:UserID"`
}
```

### Dependencies

```go
golang.org/x/crypto/bcrypt  // Password hashing
github.com/google/uuid      // UUID generation
```

### Code Structure

```
internal/domain/user/
├── user.go              # User entity
├── repository.go        # Repository interface
└── validation.go        # Validation rules

internal/usecase/auth/
├── register.go          # Registration use case
└── verify.go            # Email verification use case

internal/adapter/http/
├── auth_handler.go      # HTTP handlers
└── dto/
    └── auth.go          # Request/Response DTOs

internal/adapter/repository/
└── user_repo.go         # GORM user repository
```

## API Response Examples

### Success (201 Created)

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "email": "user@example.com",
  "username": "johndoe",
  "display_name": "John Doe",
  "is_active": false,
  "email_verified": false,
  "created_at": "2024-01-15T10:30:00Z"
}
```

### Validation Error (400 Bad Request)

```json
{
  "error": "validation_error",
  "message": "Validation failed",
  "details": [
    {
      "field": "password",
      "message": "must contain at least one uppercase letter"
    },
    { "field": "email", "message": "invalid email format" }
  ]
}
```

### Conflict (409)

```json
{
  "error": "conflict",
  "message": "Email address already registered"
}
```

## Definition of Done

- [ ] `POST /api/v1/auth/register` creates inactive user
- [ ] All validation rules enforced with clear error messages
- [ ] Password stored as bcrypt hash
- [ ] Email verification token created
- [ ] `GET /api/v1/auth/verify?token=...` activates account
- [ ] Duplicate email/username properly rejected
- [ ] Unit tests for validation logic
- [ ] Integration tests for registration flow
