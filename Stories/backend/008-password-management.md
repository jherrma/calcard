# Story 008: Password Management

## Title
Implement Password Change and Reset Functionality

## Description
As a user, I want to change my password and recover access if I forget it, so that I can maintain secure access to my account.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| UM-1.2.1 | Users can change their password when logged in |
| UM-1.2.2 | Password change requires current password confirmation |
| UM-1.2.3 | Users can request password reset via email |
| UM-1.2.4 | Password reset links expire after configured time (e.g., 1 hour) |
| UM-1.2.5 | Password reset invalidates previous reset links |
| UM-1.2.6 | All sessions are invalidated after password change |

## Acceptance Criteria

### Password Change (Authenticated)

- [ ] REST endpoint `PUT /api/v1/users/me/password` (requires auth)
- [ ] Request body:
  ```json
  {
    "current_password": "OldPass123!",
    "new_password": "NewPass456!"
  }
  ```
- [ ] Current password verified before change
- [ ] Incorrect current password returns 401
- [ ] New password must meet same strength requirements as registration
- [ ] New password cannot be same as current password
- [ ] Password updated with new bcrypt hash
- [ ] All refresh tokens for user are revoked (logout everywhere)
- [ ] Current session remains valid (new access token issued)
- [ ] Returns 200 OK with success message

### Password Reset (Unauthenticated)

- [ ] REST endpoint `POST /api/v1/auth/forgot-password`
- [ ] Request body:
  ```json
  {
    "email": "user@example.com"
  }
  ```
- [ ] Always returns 200 OK (prevent email enumeration)
- [ ] If email exists:
  - [ ] Previous reset tokens for user are invalidated
  - [ ] New reset token generated (32 bytes, hex encoded)
  - [ ] Token stored with 1 hour expiration
  - [ ] Email sent with reset link
- [ ] REST endpoint `POST /api/v1/auth/reset-password`
- [ ] Request body:
  ```json
  {
    "token": "abc123...",
    "new_password": "NewPass456!"
  }
  ```
- [ ] Invalid/expired token returns 400 Bad Request
- [ ] Valid token:
  - [ ] Password updated
  - [ ] Token deleted (single-use)
  - [ ] All refresh tokens revoked
- [ ] Returns 200 OK on success

## Technical Notes

### New Configuration
```
CALDAV_PASSWORD_RESET_EXPIRY   (default: "1h")
```

### New Database Table
```go
type PasswordReset struct {
    ID        uint      `gorm:"primaryKey"`
    UserID    uint      `gorm:"index;not null"`
    TokenHash string    `gorm:"uniqueIndex;size:64;not null"`
    ExpiresAt time.Time `gorm:"index;not null"`
    CreatedAt time.Time
    UsedAt    *time.Time
    User      User      `gorm:"foreignKey:UserID"`
}
```

### Code Structure
```
internal/usecase/auth/
├── change_password.go    # Password change use case
├── forgot_password.go    # Forgot password use case
└── reset_password.go     # Reset password use case

internal/adapter/repository/
└── password_reset_repo.go  # Password reset token repository

internal/infrastructure/email/
├── email.go              # Email service interface
├── smtp.go               # SMTP implementation
└── noop.go               # No-op implementation (dev mode)
```

## API Response Examples

### Password Change Success (200 OK)
```json
{
  "message": "Password changed successfully",
  "access_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

### Password Change - Wrong Current Password (401)
```json
{
  "error": "authentication_failed",
  "message": "Current password is incorrect"
}
```

### Forgot Password (200 OK - Always)
```json
{
  "message": "If an account with that email exists, a password reset link has been sent."
}
```

### Reset Password Success (200 OK)
```json
{
  "message": "Password has been reset successfully. Please login with your new password."
}
```

### Reset Password - Invalid Token (400)
```json
{
  "error": "invalid_token",
  "message": "Password reset link is invalid or has expired"
}
```

## Email Templates

### Password Reset Email
```
Subject: Reset your CalDAV Server password

Hi {display_name},

You requested to reset your password. Click the link below to set a new password:

{base_url}/auth/reset-password?token={token}

This link expires in 1 hour.

If you didn't request this, you can safely ignore this email.

- CalDAV Server
```

## Security Considerations

1. **Token Storage**: Store hash of reset token, not plaintext
2. **Timing Attacks**: Use constant-time comparison for tokens
3. **Email Enumeration**: Always return same response regardless of email existence
4. **Rate Limiting**: Apply rate limits to forgot-password endpoint (e.g., 3/hour/email, 10/hour/IP)
5. **Token Invalidation**: Delete old tokens when new one is requested

## Definition of Done

- [ ] `PUT /api/v1/users/me/password` changes password for authenticated user
- [ ] Password change requires correct current password
- [ ] Password change revokes all other sessions
- [ ] `POST /api/v1/auth/forgot-password` sends reset email (if configured)
- [ ] Reset tokens expire after configured time
- [ ] New reset request invalidates old tokens
- [ ] `POST /api/v1/auth/reset-password` validates token and updates password
- [ ] Password reset revokes all sessions
- [ ] No email enumeration via response differences
- [ ] Unit tests for password validation
- [ ] Integration tests for change/reset flows
