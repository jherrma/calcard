# Story 007: User Authentication (Local Login)

## Title
Implement JWT-Based Local Authentication

## Description
As a registered user, I want to login with my email and password so that I can access my calendars and contacts.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AU-2.1.1 | Users can login with email and password |
| AU-2.1.2 | Failed login attempts show generic error message |
| AU-2.1.3 | Rate limiting prevents brute force attacks |
| AU-2.1.4 | Sessions expire after configured inactivity period |
| AU-2.1.5 | Users can manually logout |
| AU-2.1.6 | JWT tokens have configurable expiration |

## Acceptance Criteria

- [ ] REST endpoint `POST /api/v1/auth/login` accepts login requests
- [ ] Request body:
  ```json
  {
    "email": "user@example.com",
    "password": "SecurePass123!"
  }
  ```
- [ ] Successful login returns:
  - [ ] Access token (JWT, short-lived: 15 minutes default)
  - [ ] Refresh token (opaque, long-lived: 7 days default)
  - [ ] User profile information
- [ ] Access token contains:
  - [ ] `sub`: User UUID
  - [ ] `email`: User email
  - [ ] `username`: Username
  - [ ] `iat`: Issued at
  - [ ] `exp`: Expiration
- [ ] Invalid credentials return 401 with generic message
  - [ ] Message: "Invalid email or password" (never reveal which is wrong)
- [ ] Inactive account returns 401 with message about verification
- [ ] Rate limiting on login endpoint:
  - [ ] 5 attempts per minute per IP
  - [ ] 10 attempts per minute per email
  - [ ] Returns 429 Too Many Requests when exceeded
- [ ] `POST /api/v1/auth/refresh` exchanges refresh token for new access token
- [ ] `POST /api/v1/auth/logout` invalidates refresh token
- [ ] Refresh tokens stored in database with:
  - [ ] User ID
  - [ ] Token hash (not plaintext)
  - [ ] Expiration
  - [ ] Device info (optional, User-Agent)
- [ ] Protected endpoints require valid JWT in Authorization header
- [ ] Expired tokens return 401 Unauthorized

## Technical Notes

### New Configuration
```
CALDAV_JWT_SECRET           (required, min 32 chars)
CALDAV_JWT_ACCESS_EXPIRY    (default: "15m")
CALDAV_JWT_REFRESH_EXPIRY   (default: "168h")  # 7 days
CALDAV_RATE_LIMIT_ENABLED   (default: "true")
```

### New Database Table
```go
type RefreshToken struct {
    ID        uint           `gorm:"primaryKey"`
    UserID    uint           `gorm:"index;not null"`
    TokenHash string         `gorm:"uniqueIndex;size:64;not null"`
    ExpiresAt time.Time      `gorm:"index;not null"`
    UserAgent string         `gorm:"size:500"`
    IP        string         `gorm:"size:45"`
    CreatedAt time.Time
    RevokedAt *time.Time     `gorm:"index"`
    User      User           `gorm:"foreignKey:UserID"`
}
```

### Dependencies
```go
github.com/golang-jwt/jwt/v5      // JWT handling
github.com/gofiber/fiber/v3/middleware/limiter  // Rate limiting
```

### Code Structure
```
internal/adapter/auth/
├── jwt.go               # JWT generation and validation
├── middleware.go        # Auth middleware for Fiber
└── rate_limiter.go      # Rate limiting configuration

internal/usecase/auth/
├── login.go             # Login use case
├── refresh.go           # Token refresh use case
└── logout.go            # Logout use case

internal/adapter/repository/
└── refresh_token_repo.go  # Refresh token repository
```

## API Response Examples

### Login Success (200 OK)
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4...",
  "token_type": "Bearer",
  "expires_in": 900,
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "user@example.com",
    "username": "johndoe",
    "display_name": "John Doe"
  }
}
```

### Login Failure (401 Unauthorized)
```json
{
  "error": "authentication_failed",
  "message": "Invalid email or password"
}
```

### Rate Limited (429)
```json
{
  "error": "rate_limit_exceeded",
  "message": "Too many login attempts. Please try again later.",
  "retry_after": 60
}
```

### Token Refresh (200 OK)
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

## Auth Middleware Usage

```go
// Protected routes
api := app.Group("/api/v1")
api.Use(authMiddleware.Authenticate())

// Routes that need authentication
api.Get("/users/me", userHandler.GetMe)
api.Get("/calendars", calendarHandler.List)
```

## Definition of Done

- [ ] `POST /api/v1/auth/login` returns JWT on valid credentials
- [ ] Invalid login returns generic 401 message
- [ ] Rate limiting blocks excessive attempts
- [ ] `POST /api/v1/auth/refresh` issues new access token
- [ ] `POST /api/v1/auth/logout` invalidates refresh token
- [ ] Auth middleware protects routes, validates JWT
- [ ] Expired/invalid tokens return 401
- [ ] Unit tests for JWT generation/validation
- [ ] Integration tests for login flow
