# Story 009: User Profile Management

## Title

Implement User Profile View and Update

## Description

As a user, I want to view and update my profile information and delete my account if needed.

## Related Acceptance Criteria

| ID       | Criterion                                                                                                                                                                       |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| UM-1.3.1 | Users can update their display name                                                                                                                                             |
| UM-1.3.2 | Users can view their account creation date                                                                                                                                      |
| UM-1.3.3 | Users can delete their account                                                                                                                                                  |
| UM-1.3.4 | Account deletion requires password confirmation                                                                                                                                 |
| UM-1.3.5 | the first user to register is an admin account and can view other account information like display name and reset their passwords and make them admin                           |
| UM-1.3.6 | the system settings endpoint provides information if an admis is already configured or not to be able to display an initial admin account creation screen                       |
| UM-1.3.7 | the server makes sure, that an admin account can only be created once without authentication for initial setup. further promotion to admins are only allowed by existing admins |

## Acceptance Criteria

### View Profile

- [ ] REST endpoint `GET /api/v1/users/me` (requires auth)
- [ ] Returns user profile information:
  ```json
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "user@example.com",
    "display_name": "John Doe",
    "is_active": true,
    "email_verified": true,
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-20T14:00:00Z",
    "auth_methods": ["local"],
    "stats": {
      "calendar_count": 3,
      "contact_count": 150,
      "app_password_count": 2
    }
  }
  ```
- [ ] Auth methods array shows linked authentication methods
- [ ] Stats show resource counts (calendars, contacts, app passwords)

### Update Profile

- [ ] REST endpoint `PATCH /api/v1/users/me` (requires auth)
- [ ] Request body (all fields optional):
  ```json
  {
    "display_name": "Jonathan Doe",
    "email": "jonathan@doe.com"
  }
  ```
- [ ] Display name:
  - [ ] Can be updated freely
  - [ ] Max 255 characters
  - [ ] Can be empty/null
- [ ] Returns updated profile on success

### Delete Account

- [ ] REST endpoint `DELETE /api/v1/users/me` (requires auth)
- [ ] Request body:
  ```json
  {
    "password": "CurrentPass123!",
    "confirmation": "DELETE"
  }
  ```
- [ ] Password required for confirmation (local auth users)
- [ ] Confirmation string must be exactly "DELETE"
- [ ] OAuth-only users: require re-authentication or alternative confirmation
- [ ] On deletion:
  - [ ] All calendars and events deleted
  - [ ] All address books and contacts deleted
  - [ ] All app passwords deleted
  - [ ] All refresh tokens deleted
  - [ ] All shares revoked
  - [ ] OAuth connections removed
  - [ ] User soft-deleted (for audit purposes)
- [ ] Returns 204 No Content on success
- [ ] Returns 401 if password incorrect
- [ ] Returns 400 if confirmation missing

## Technical Notes

### Soft Delete Implementation

```go
// User model already has DeletedAt from GORM
// Queries automatically filter soft-deleted records

// Hard delete associated data, soft delete user
func (r *UserRepository) Delete(ctx context.Context, userID uint) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        // Hard delete related data
        tx.Where("user_id = ?", userID).Delete(&Calendar{})
        tx.Where("user_id = ?", userID).Delete(&AddressBook{})
        tx.Where("user_id = ?", userID).Delete(&AppPassword{})
        tx.Where("user_id = ?", userID).Delete(&RefreshToken{})
        // ... other related tables

        // Soft delete user
        return tx.Delete(&User{}, userID).Error
    })
}
```

### Code Structure

```
internal/usecase/user/
├── get_profile.go      # Get profile use case
├── update_profile.go   # Update profile use case
└── delete_account.go   # Delete account use case

internal/adapter/http/
└── user_handler.go     # User HTTP handlers
```

## API Response Examples

### Get Profile (200 OK)

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "email": "user@example.com",
  "display_name": "John Doe",
  "is_active": true,
  "email_verified": true,
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-20T14:00:00Z",
  "auth_methods": ["local", "google"],
  "stats": {
    "calendar_count": 3,
    "contact_count": 150,
    "app_password_count": 2
  }
}
```

### Update Profile Success (200 OK)

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "email": "user@example.com",
  "display_name": "Jonathan Doe",
  "is_active": true,
  "email_verified": true,
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-21T09:15:00Z"
}
```

### Delete Account - Missing Confirmation (400)

```json
{
  "error": "validation_error",
  "message": "Please type DELETE to confirm account deletion"
}
```

### Delete Account - Wrong Password (401)

```json
{
  "error": "authentication_failed",
  "message": "Password is incorrect"
}
```

### Delete Account Success (204 No Content)

No response body.

## Definition of Done

- [ ] `GET /api/v1/users/me` returns full profile with stats
- [ ] `PATCH /api/v1/users/me` updates allowed fields
- [ ] Display name can be updated
- [ ] Username can be updated with uniqueness check
- [ ] `DELETE /api/v1/users/me` requires password + confirmation
- [ ] Account deletion removes all user data
- [ ] User record is soft-deleted for audit trail
- [ ] Unit tests for profile operations
- [ ] Integration tests for update and delete flows
