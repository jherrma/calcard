# Story 005: Database Migration System

## Title
Implement Database Migration System with Initial Schema

## Description
As a developer, I want a database migration system that manages schema changes, so that database updates are version-controlled, repeatable, and work across both SQLite and PostgreSQL.

## Acceptance Criteria

- [ ] Migration system uses GORM AutoMigrate for development simplicity
- [ ] Migration runs automatically on server startup (configurable)
- [ ] CLI command available: `./server migrate`
- [ ] Initial User model and table created:
  ```go
  type User struct {
      ID            uint           `gorm:"primaryKey"`
      UUID          string         `gorm:"uniqueIndex;size:36;not null"`
      Email         string         `gorm:"uniqueIndex;size:255;not null"`
      Username      string         `gorm:"uniqueIndex;size:100;not null"`
      PasswordHash  string         `gorm:"size:255;not null"`
      DisplayName   string         `gorm:"size:255"`
      IsActive      bool           `gorm:"default:true"`
      EmailVerified bool           `gorm:"default:false"`
      CreatedAt     time.Time
      UpdatedAt     time.Time
      DeletedAt     gorm.DeletedAt `gorm:"index"`
  }
  ```
- [ ] Indexes created:
  - [ ] Unique index on `uuid`
  - [ ] Unique index on `email`
  - [ ] Unique index on `username`
  - [ ] Index on `deleted_at` for soft deletes
- [ ] Migration logs which tables/columns are being modified
- [ ] Configuration option to disable auto-migration in production:
  ```
  CALDAV_DB_AUTO_MIGRATE=true (default: true)
  ```
- [ ] Migration is idempotent (safe to run multiple times)

## Technical Notes

GORM AutoMigrate behavior:
- Creates tables if they don't exist
- Adds missing columns
- Creates missing indexes
- Does NOT delete columns (safe)
- Does NOT change column types automatically

For production environments, consider using a dedicated migration tool later:
- `golang-migrate/migrate` for versioned migrations
- `pressly/goose` as alternative

Initial approach uses GORM AutoMigrate for simplicity during development.

## Domain Models Location

```
internal/domain/user/
├── user.go          # User entity
└── repository.go    # Repository interface

internal/adapter/repository/
└── user_repo.go     # GORM implementation
```

## User Entity

```go
package user

import (
    "time"
    "gorm.io/gorm"
)

type User struct {
    ID            uint           `gorm:"primaryKey"`
    UUID          string         `gorm:"uniqueIndex;size:36;not null"`
    Email         string         `gorm:"uniqueIndex;size:255;not null"`
    Username      string         `gorm:"uniqueIndex;size:100;not null"`
    PasswordHash  string         `gorm:"size:255;not null"`
    DisplayName   string         `gorm:"size:255"`
    IsActive      bool           `gorm:"default:true"`
    EmailVerified bool           `gorm:"default:false"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (User) TableName() string {
    return "users"
}
```

## CLI Integration

```go
// cmd/server/main.go
func main() {
    if len(os.Args) > 1 && os.Args[1] == "migrate" {
        runMigrations()
        return
    }
    startServer()
}
```

## Definition of Done

- [ ] `./server migrate` runs migrations and exits
- [ ] Server startup runs migrations when `CALDAV_DB_AUTO_MIGRATE=true`
- [ ] `users` table created with all columns and indexes
- [ ] Running migrations twice doesn't error or duplicate data
- [ ] Works on both SQLite and PostgreSQL
- [ ] Migration output logs table/column changes
