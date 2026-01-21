# Story 003: Database Connection Layer

## Title
Implement GORM Database Connection with SQLite/PostgreSQL Support

## Description
As a developer, I want a database abstraction layer using GORM that supports both SQLite and PostgreSQL, so that users can choose their preferred database without code changes.

## Acceptance Criteria

- [ ] GORM initialized based on configuration from Story 002
- [ ] SQLite connection:
  - [ ] Creates data directory if it doesn't exist
  - [ ] Creates SQLite file at configured path
  - [ ] Enables WAL mode for better concurrency
  - [ ] Sets appropriate pragmas (foreign_keys, busy_timeout)
- [ ] PostgreSQL connection:
  - [ ] Connects using DSN from config
  - [ ] Connection pool configured (max open, max idle, lifetime)
  - [ ] SSL mode configurable
- [ ] Database interface defined for dependency injection:
  ```go
  type Database interface {
      DB() *gorm.DB
      Close() error
      Ping() error
      Migrate(models ...interface{}) error
  }
  ```
- [ ] Graceful shutdown closes database connections
- [ ] Connection health check function available
- [ ] Automatic retry on initial connection failure (3 attempts, exponential backoff)

## Technical Notes

Dependencies:
```go
gorm.io/gorm v1.25+
gorm.io/driver/sqlite
gorm.io/driver/postgres
```

SQLite pragmas to set:
```sql
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
PRAGMA synchronous=NORMAL;
```

PostgreSQL pool settings:
- MaxOpenConns: 25
- MaxIdleConns: 5
- ConnMaxLifetime: 5 minutes

## Code Structure

```
internal/infrastructure/database/
├── database.go      # Interface and factory
├── sqlite.go        # SQLite implementation
├── postgres.go      # PostgreSQL implementation
└── database_test.go # Integration tests
```

## Definition of Done

- [ ] `database.New(config)` returns appropriate implementation
- [ ] SQLite: Creates file and directory, applies pragmas
- [ ] PostgreSQL: Connects with pool settings
- [ ] `db.Ping()` returns nil on healthy connection
- [ ] `db.Close()` cleanly shuts down connections
- [ ] Integration tests pass for both SQLite and PostgreSQL
