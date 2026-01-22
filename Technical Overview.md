# Technical Overview

Note: this is AI generated.

This document provides a comprehensive technical specification for implementing a self-hostable CalDAV/CardDAV server in Go with a Vue.js web frontend.

---

## Table of Contents

1. [Technology Stack](#1-technology-stack)
2. [Protocol Specifications](#2-protocol-specifications)
3. [Backend Architecture](#3-backend-architecture)
4. [Database Design](#4-database-design)
5. [Authentication System](#5-authentication-system)
6. [CalDAV Implementation](#6-caldav-implementation)
7. [CardDAV Implementation](#7-carddav-implementation)
8. [REST API Design](#8-rest-api-design)
9. [Frontend Architecture](#9-frontend-architecture)
10. [Deployment Architecture](#10-deployment-architecture)
11. [Security Considerations](#11-security-considerations)

---

## 1. Technology Stack

### 1.1 Backend

| Component        | Technology                 | Version | Purpose                           |
| ---------------- | -------------------------- | ------- | --------------------------------- |
| Language         | Go                         | 1.22+   | Core server implementation        |
| HTTP Framework   | Fiber                      | v1.9.x  | REST API routing and middleware   |
| WebDAV Library   | emersion/go-webdav         | v0.7.0  | CalDAV/CardDAV protocol handling  |
| iCalendar Parser | emersion/go-ical           | latest  | iCalendar data parsing/generation |
| vCard Parser     | emersion/go-vcard          | latest  | vCard data parsing/generation     |
| ORM              | GORM                       | v1.25.x | Database abstraction              |
| JWT Library      | golang-jwt/jwt             | v5.x    | JWT token handling                |
| OAuth2/OIDC      | coreos/go-oidc             | v3.x    | OpenID Connect client             |
| SAML             | crewjam/saml               | v0.4.x  | SAML Service Provider             |
| Password Hashing | golang.org/x/crypto/bcrypt | latest  | Secure password storage           |
| Configuration    | spf13/viper                | v1.18.x | Configuration management          |
| Logging          | uber-go/zap                | v1.27.x | Structured logging                |
| UUID             | google/uuid                | v1.6.x  | UUID generation                   |
| Validation       | go-playground/validator    | v10.x   | Request validation                |

### 1.2 Frontend

| Component         | Technology   | Version | Purpose                     |
| ----------------- | ------------ | ------- | --------------------------- |
| Framework         | Vue.js       | 3.4.x   | UI framework                |
| Meta-Framework    | Nuxt         | 3.10.x  | SSR, routing, conventions   |
| State Management  | Pinia        | 2.x     | Centralized state           |
| Calendar UI       | FullCalendar | 6.x     | Calendar visualization      |
| UI Components     | PrimeVue     | 3.x     | UI component library        |
| HTTP Client       | ofetch       | latest  | API requests (Nuxt default) |
| Composables       | VueUse       | 10.x    | Utility composables         |
| Schema Validation | Zod          | 3.x     | Runtime type validation     |
| CSS               | Tailwind CSS | 3.x     | Utility-first styling       |

### 1.3 Database

| Database      | Use Case                | Notes                                  |
| ------------- | ----------------------- | -------------------------------------- |
| PostgreSQL 16 | Production              | Recommended for multi-user deployments |
| SQLite 3      | Development/Single-user | Embedded, zero-config                  |
| MySQL 8       | Alternative             | Supported via GORM                     |

### 1.4 Infrastructure

| Component     | Technology     | Purpose                         |
| ------------- | -------------- | ------------------------------- |
| Container     | Docker         | Containerization                |
| Orchestration | Docker Compose | Multi-container deployment      |
| Reverse Proxy | Nginx/Caddy    | TLS termination, load balancing |
| CI/CD         | GitHub Actions | Build, test, release            |

---

## 2. Protocol Specifications

### 2.1 Required RFCs

| RFC      | Name                       | Purpose                               |
| -------- | -------------------------- | ------------------------------------- |
| RFC 4918 | HTTP Extensions for WebDAV | Base WebDAV protocol                  |
| RFC 4791 | CalDAV                     | Calendar access extensions            |
| RFC 6352 | CardDAV                    | Contact/address book extensions       |
| RFC 6578 | WebDAV Sync                | Efficient collection synchronization  |
| RFC 6764 | CalDAV/CardDAV Discovery   | Service discovery (.well-known)       |
| RFC 3744 | WebDAV ACL                 | Access control (basic implementation) |
| RFC 5545 | iCalendar                  | Calendar data format                  |
| RFC 6350 | vCard 4.0                  | Contact data format                   |
| RFC 2426 | vCard 3.0                  | Legacy contact format (compatibility) |

### 2.2 HTTP Methods Required

```
WebDAV Core:    OPTIONS, GET, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL
CalDAV:         MKCALENDAR, REPORT (calendar-query, calendar-multiget, sync-collection)
CardDAV:        REPORT (addressbook-query, addressbook-multiget, sync-collection)
```

### 2.3 DAV Headers

Server must advertise capabilities in OPTIONS response:

```http
DAV: 1, 2, 3, calendar-access, addressbook
Allow: OPTIONS, GET, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL, MKCALENDAR, REPORT
```

---

## 3. Backend Architecture

### 3.1 Clean Architecture Layers

```
┌─────────────────────────────────────────────────────────────┐
│                    Presentation Layer                        │
│  ┌─────────────────┐  ┌─────────────────┐                   │
│  │  WebDAV Handlers │  │   REST Handlers  │                   │
│  │  (CalDAV/CardDAV)│  │   (Web UI API)   │                   │
│  └────────┬────────┘  └────────┬────────┘                   │
└───────────┼────────────────────┼────────────────────────────┘
            │                    │
┌───────────▼────────────────────▼────────────────────────────┐
│                    Application Layer                         │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                    Use Cases                         │    │
│  │  AuthUseCase, CalendarUseCase, ContactUseCase, etc. │    │
│  └────────────────────────┬────────────────────────────┘    │
└───────────────────────────┼─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│                     Domain Layer                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   Entities   │  │  Interfaces  │  │Domain Services│       │
│  │ User,Calendar│  │ Repositories │  │   Validation  │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│                  Infrastructure Layer                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ GORM Repos   │  │ Auth Adapters │  │   Config     │       │
│  │(Postgres/SQL)│  │(OAuth/SAML)  │  │   Server     │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 Project Structure

```
caldav-server/
├── cmd/
│   └── server/
│       └── main.go                      # Application entry point
│
├── internal/
│   ├── domain/                          # Domain layer
│   │   ├── user/
│   │   │   ├── entity.go                # User, AppPassword entities
│   │   │   └── repository.go            # Repository interface
│   │   ├── calendar/
│   │   │   ├── entity.go                # Calendar, CalendarObject entities
│   │   │   ├── repository.go            # Repository interface
│   │   │   └── ical.go                  # iCalendar domain helpers
│   │   ├── addressbook/
│   │   │   ├── entity.go                # AddressBook, Contact entities
│   │   │   ├── repository.go            # Repository interface
│   │   │   └── vcard.go                 # vCard domain helpers
│   │   └── sharing/
│   │       └── entity.go                # Share entities
│   │
│   ├── usecase/                         # Application layer
│   │   ├── auth/
│   │   │   ├── login.go                 # Login use case
│   │   │   ├── register.go              # Registration use case
│   │   │   ├── oauth.go                 # OAuth flow use case
│   │   │   ├── saml.go                  # SAML flow use case
│   │   │   └── app_password.go          # App password management
│   │   ├── calendar/
│   │   │   ├── calendar.go              # Calendar CRUD use cases
│   │   │   ├── event.go                 # Event CRUD use cases
│   │   │   └── share.go                 # Calendar sharing use cases
│   │   ├── contact/
│   │   │   ├── addressbook.go           # AddressBook CRUD use cases
│   │   │   └── contact.go               # Contact CRUD use cases
│   │   └── user/
│   │       └── profile.go               # User profile use cases
│   │
│   ├── adapter/                         # Interface adapters
│   │   ├── repository/                  # GORM implementations
│   │   │   ├── user_repo.go
│   │   │   ├── calendar_repo.go
│   │   │   ├── addressbook_repo.go
│   │   │   └── sync_repo.go             # Sync token tracking
│   │   │
│   │   ├── webdav/                      # WebDAV/CalDAV/CardDAV
│   │   │   ├── handler.go               # HTTP handler setup
│   │   │   ├── caldav_backend.go        # caldav.Backend implementation
│   │   │   ├── carddav_backend.go       # carddav.Backend implementation
│   │   │   ├── principal.go             # User principal backend
│   │   │   └── sync.go                  # WebDAV-Sync implementation
│   │   │
│   │   ├── http/                        # REST API handlers
│   │   │   ├── router.go                # Route definitions
│   │   │   ├── auth.go                  # Auth endpoints
│   │   │   ├── calendar.go              # Calendar endpoints
│   │   │   ├── contact.go               # Contact endpoints
│   │   │   ├── user.go                  # User endpoints
│   │   │   └── dto/                     # Request/response DTOs
│   │   │       ├── auth.go
│   │   │       ├── calendar.go
│   │   │       └── contact.go
│   │   │
│   │   └── auth/                        # Auth adapters
│   │       ├── jwt.go                   # JWT service
│   │       ├── oauth.go                 # OAuth2/OIDC client
│   │       ├── saml.go                  # SAML SP
│   │       └── basic.go                 # HTTP Basic Auth for DAV
│   │
│   └── infrastructure/                  # Infrastructure
│       ├── config/
│       │   ├── config.go                # Configuration structs
│       │   └── loader.go                # Viper configuration loading
│       ├── database/
│       │   ├── gorm.go                  # GORM setup
│       │   └── migrations.go            # Auto-migration
│       ├── middleware/
│       │   ├── auth.go                  # JWT auth middleware
│       │   ├── cors.go                  # CORS middleware
│       │   ├── logging.go               # Request logging
│       │   └── ratelimit.go             # Rate limiting
│       └── server/
│           └── server.go                # HTTP server setup
│
├── pkg/                                 # Public packages (if needed)
│   └── ical/
│       └── helpers.go                   # iCalendar utilities
│
├── frontend/                            # Vue.js frontend (separate build)
│   └── ...
│
├── migrations/                          # SQL migrations (if not using GORM auto-migrate)
├── configs/
│   ├── config.yaml.example
│   └── docker/
├── scripts/
├── docs/
├── go.mod
├── go.sum
├── Dockerfile
└── docker-compose.yaml
```

### 3.3 Dependency Injection

```go
// cmd/server/main.go
func main() {
    // Load configuration
    cfg := config.Load()

    // Initialize database
    db := database.NewGormDB(cfg.Database)

    // Initialize repositories
    userRepo := repository.NewUserRepository(db)
    calendarRepo := repository.NewCalendarRepository(db)
    addressbookRepo := repository.NewAddressBookRepository(db)
    syncRepo := repository.NewSyncRepository(db)

    // Initialize services
    jwtService := auth.NewJWTService(cfg.JWT)
    oauthService := auth.NewOAuthService(cfg.OAuth)

    // Initialize use cases
    authUseCase := usecase.NewAuthUseCase(userRepo, jwtService, oauthService)
    calendarUseCase := usecase.NewCalendarUseCase(calendarRepo, syncRepo)
    contactUseCase := usecase.NewContactUseCase(addressbookRepo, syncRepo)

    // Initialize WebDAV backends
    caldavBackend := webdav.NewCalDAVBackend(calendarRepo, syncRepo)
    carddavBackend := webdav.NewCardDAVBackend(addressbookRepo, syncRepo)

    // Initialize HTTP handlers
    authHandler := http.NewAuthHandler(authUseCase)
    calendarHandler := http.NewCalendarHandler(calendarUseCase)
    contactHandler := http.NewContactHandler(contactUseCase)

    // Setup router
    router := server.SetupRouter(
        cfg,
        authHandler, calendarHandler, contactHandler,
        caldavBackend, carddavBackend,
    )

    // Start server
    server.Run(router, cfg.Server)
}
```

---

## 4. Database Design

### 4.1 Entity-Relationship Diagram

```
┌─────────────┐     ┌──────────────────┐     ┌───────────────────┐
│    users    │────<│    calendars     │────<│  calendar_objects │
├─────────────┤     ├──────────────────┤     ├───────────────────┤
│ id          │     │ id               │     │ id                │
│ uuid        │     │ uuid             │     │ uuid              │
│ email       │     │ user_id (FK)     │     │ calendar_id (FK)  │
│ username    │     │ path             │     │ path              │
│ password_h  │     │ name             │     │ uid               │
│ display_name│     │ description      │     │ etag              │
│ is_active   │     │ color            │     │ component_type    │
│ verified    │     │ timezone         │     │ ical_data         │
│ created_at  │     │ sync_token       │     │ summary           │
│ updated_at  │     │ ctag             │     │ start_time        │
│ deleted_at  │     │ created_at       │     │ end_time          │
└─────────────┘     │ updated_at       │     │ is_all_day        │
       │            │ deleted_at       │     │ created_at        │
       │            └──────────────────┘     │ updated_at        │
       │                    │                │ deleted_at        │
       │                    │                └───────────────────┘
       │                    │
       │            ┌───────▼────────┐
       │            │ calendar_shares │
       │            ├────────────────┤
       │            │ id             │
       ├───────────>│ calendar_id    │
       │            │ shared_with_id │
       │            │ permission     │
       │            │ created_at     │
       │            └────────────────┘
       │
       │     ┌──────────────────┐     ┌───────────────────┐
       │────<│  addressbooks    │────<│  address_objects  │
       │     ├──────────────────┤     ├───────────────────┤
       │     │ id               │     │ id                │
       │     │ uuid             │     │ uuid              │
       │     │ user_id (FK)     │     │ addressbook_id    │
       │     │ path             │     │ path              │
       │     │ name             │     │ uid               │
       │     │ description      │     │ etag              │
       │     │ sync_token       │     │ vcard_data        │
       │     │ ctag             │     │ vcard_version     │
       │     │ created_at       │     │ formatted_name    │
       │     │ updated_at       │     │ given_name        │
       │     │ deleted_at       │     │ family_name       │
       │     └──────────────────┘     │ email             │
       │                              │ phone             │
       │                              │ organization      │
       │     ┌──────────────────┐     │ created_at        │
       │────<│  app_passwords   │     │ updated_at        │
       │     ├──────────────────┤     │ deleted_at        │
       │     │ id               │     └───────────────────┘
       │     │ uuid             │
       │     │ user_id (FK)     │
       │     │ name             │
       │     │ password_hash    │
       │     │ scopes           │
       │     │ last_used_at     │
       │     │ expires_at       │
       │     │ revoked_at       │
       │     │ created_at       │
       │     └──────────────────┘
       │
       │     ┌──────────────────┐
       └────<│oauth_connections │
             ├──────────────────┤
             │ id               │
             │ user_id (FK)     │
             │ provider         │
             │ provider_id      │
             │ access_token     │
             │ refresh_token    │
             │ token_expiry     │
             │ created_at       │
             │ updated_at       │
             └──────────────────┘

┌────────────────────┐
│  sync_change_log   │  (For WebDAV-Sync RFC 6578)
├────────────────────┤
│ id                 │
│ collection_id      │
│ collection_type    │
│ resource_path      │
│ change_type        │
│ sync_token         │
│ created_at         │
└────────────────────┘

┌─────────────────────────┐
│  calendar_credentials   │  (Per-Calendar Basic Auth)
├─────────────────────────┤
│ id                      │
│ uuid                    │
│ calendar_id (FK)        │
│ username                │  (custom username for this credential)
│ password_hash           │
│ name                    │  (label like "Google Calendar Import")
│ permission              │  (read, read-write)
│ last_used_at            │
│ last_used_ip            │
│ expires_at              │
│ revoked_at              │
│ created_at              │
└─────────────────────────┘

┌─────────────────────────┐
│ addressbook_credentials │  (Per-AddressBook Basic Auth)
├─────────────────────────┤
│ id                      │
│ uuid                    │
│ addressbook_id (FK)     │
│ username                │
│ password_hash           │
│ name                    │
│ permission              │
│ last_used_at            │
│ last_used_ip            │
│ expires_at              │
│ revoked_at              │
│ created_at              │
└─────────────────────────┘
```

### 4.2 GORM Model Definitions

```go
// internal/domain/user/entity.go
type User struct {
    ID            uint           `gorm:"primaryKey"`
    UUID          string         `gorm:"uniqueIndex;size:36;not null"`
    Email         string         `gorm:"uniqueIndex;size:255;not null"`
    Username      string         `gorm:"uniqueIndex;size:100;not null"`
    PasswordHash  string         `gorm:"size:255"`
    DisplayName   string         `gorm:"size:255"`
    IsActive      bool           `gorm:"default:true"`
    EmailVerified bool           `gorm:"default:false"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt `gorm:"index"`
}

// internal/domain/calendar/entity.go
type Calendar struct {
    ID                  uint           `gorm:"primaryKey"`
    UUID                string         `gorm:"uniqueIndex;size:36;not null"`
    UserID              uint           `gorm:"index;not null"`
    Path                string         `gorm:"uniqueIndex;size:512;not null"`
    Name                string         `gorm:"size:255;not null"`
    Description         string         `gorm:"type:text"`
    Color               string         `gorm:"size:7"`
    Timezone            string         `gorm:"size:100"`
    SupportedComponents string         `gorm:"size:100;default:'VEVENT,VTODO'"`
    MaxResourceSize     int64          `gorm:"default:10485760"`
    SyncToken           string         `gorm:"size:255"`
    CTag                string         `gorm:"size:255"`
    IsDefault           bool           `gorm:"default:false"`
    CreatedAt           time.Time
    UpdatedAt           time.Time
    DeletedAt           gorm.DeletedAt `gorm:"index"`

    User    User              `gorm:"foreignKey:UserID"`
    Objects []CalendarObject  `gorm:"foreignKey:CalendarID"`
    Shares  []CalendarShare   `gorm:"foreignKey:CalendarID"`
}

type CalendarObject struct {
    ID            uint           `gorm:"primaryKey"`
    UUID          string         `gorm:"uniqueIndex;size:36;not null"`
    CalendarID    uint           `gorm:"index;not null"`
    Path          string         `gorm:"uniqueIndex;size:512;not null"`
    UID           string         `gorm:"index;size:255;not null"`
    ComponentType string         `gorm:"size:20;not null"` // VEVENT, VTODO
    ETag          string         `gorm:"size:64;not null"`
    ICalData      string         `gorm:"type:mediumtext;not null"`
    ContentLength int64
    Summary       string         `gorm:"size:500"`
    StartTime     *time.Time     `gorm:"index"`
    EndTime       *time.Time     `gorm:"index"`
    IsAllDay      bool
    RecurrenceID  string         `gorm:"size:255"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt `gorm:"index"`

    Calendar Calendar `gorm:"foreignKey:CalendarID"`
}

type CalendarShare struct {
    ID           uint      `gorm:"primaryKey"`
    CalendarID   uint      `gorm:"index;not null"`
    SharedWithID uint      `gorm:"index;not null"`
    Permission   string    `gorm:"size:20;not null"` // read, read-write
    CreatedAt    time.Time
    UpdatedAt    time.Time

    Calendar   Calendar `gorm:"foreignKey:CalendarID"`
    SharedWith User     `gorm:"foreignKey:SharedWithID"`
}

// internal/domain/addressbook/entity.go
type AddressBook struct {
    ID              uint           `gorm:"primaryKey"`
    UUID            string         `gorm:"uniqueIndex;size:36;not null"`
    UserID          uint           `gorm:"index;not null"`
    Path            string         `gorm:"uniqueIndex;size:512;not null"`
    Name            string         `gorm:"size:255;not null"`
    Description     string         `gorm:"type:text"`
    MaxResourceSize int64          `gorm:"default:10485760"`
    SyncToken       string         `gorm:"size:255"`
    CTag            string         `gorm:"size:255"`
    IsDefault       bool           `gorm:"default:false"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
    DeletedAt       gorm.DeletedAt `gorm:"index"`

    User    User            `gorm:"foreignKey:UserID"`
    Objects []AddressObject `gorm:"foreignKey:AddressBookID"`
}

type AddressObject struct {
    ID            uint           `gorm:"primaryKey"`
    UUID          string         `gorm:"uniqueIndex;size:36;not null"`
    AddressBookID uint           `gorm:"index;not null"`
    Path          string         `gorm:"uniqueIndex;size:512;not null"`
    UID           string         `gorm:"index;size:255;not null"`
    ETag          string         `gorm:"size:64;not null"`
    VCardData     string         `gorm:"type:mediumtext;not null"`
    VCardVersion  string         `gorm:"size:10;default:'4.0'"`
    ContentLength int64
    FormattedName string         `gorm:"size:500;index"`
    GivenName     string         `gorm:"size:255"`
    FamilyName    string         `gorm:"size:255"`
    Email         string         `gorm:"size:255;index"`
    Phone         string         `gorm:"size:50"`
    Organization  string         `gorm:"size:255"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt `gorm:"index"`

    AddressBook AddressBook `gorm:"foreignKey:AddressBookID"`
}

// internal/domain/user/entity.go (App Passwords)
type AppPassword struct {
    ID           uint       `gorm:"primaryKey"`
    UUID         string     `gorm:"uniqueIndex;size:36;not null"`
    UserID       uint       `gorm:"index;not null"`
    Name         string     `gorm:"size:255;not null"`
    PasswordHash string     `gorm:"size:255;not null"`
    Scopes       string     `gorm:"size:255;default:'caldav,carddav'"`
    LastUsedAt   *time.Time
    LastUsedIP   string     `gorm:"size:45"`
    ExpiresAt    *time.Time `gorm:"index"`
    RevokedAt    *time.Time
    CreatedAt    time.Time

    User User `gorm:"foreignKey:UserID"`
}

// internal/domain/calendar/entity.go (Per-Calendar Credentials)
// CalendarCredential allows per-calendar basic auth access independent of user's main auth
type CalendarCredential struct {
    ID           uint       `gorm:"primaryKey"`
    UUID         string     `gorm:"uniqueIndex;size:36;not null"`
    CalendarID   uint       `gorm:"index;not null"`
    Username     string     `gorm:"size:100;not null"`          // Custom username for this credential
    PasswordHash string     `gorm:"size:255;not null"`
    Name         string     `gorm:"size:255;not null"`          // Label like "Google Calendar Import"
    Permission   string     `gorm:"size:20;default:'read'"`     // read, read-write
    LastUsedAt   *time.Time
    LastUsedIP   string     `gorm:"size:45"`
    ExpiresAt    *time.Time `gorm:"index"`
    RevokedAt    *time.Time
    CreatedAt    time.Time

    Calendar Calendar `gorm:"foreignKey:CalendarID"`
}

// internal/domain/addressbook/entity.go (Per-AddressBook Credentials)
// AddressBookCredential allows per-addressbook basic auth access
type AddressBookCredential struct {
    ID            uint       `gorm:"primaryKey"`
    UUID          string     `gorm:"uniqueIndex;size:36;not null"`
    AddressBookID uint       `gorm:"index;not null"`
    Username      string     `gorm:"size:100;not null"`
    PasswordHash  string     `gorm:"size:255;not null"`
    Name          string     `gorm:"size:255;not null"`
    Permission    string     `gorm:"size:20;default:'read'"`
    LastUsedAt    *time.Time
    LastUsedIP    string     `gorm:"size:45"`
    ExpiresAt     *time.Time `gorm:"index"`
    RevokedAt     *time.Time
    CreatedAt     time.Time

    AddressBook AddressBook `gorm:"foreignKey:AddressBookID"`
}
```

### 4.3 Performance Indexes

```go
// Additional indexes for query performance
func (db *gorm.DB) CreateIndexes() {
    // Time-range queries for events
    db.Exec(`CREATE INDEX IF NOT EXISTS idx_calendar_objects_time_range
             ON calendar_objects(calendar_id, start_time, end_time)
             WHERE deleted_at IS NULL`)

    // Sync token queries
    db.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_change_log_lookup
             ON sync_change_log(collection_type, collection_id, sync_token)`)

    // Active app passwords
    db.Exec(`CREATE INDEX IF NOT EXISTS idx_app_passwords_active
             ON app_passwords(user_id)
             WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())`)

    // Contact search
    db.Exec(`CREATE INDEX IF NOT EXISTS idx_address_objects_search
             ON address_objects USING gin(to_tsvector('english', formatted_name || ' ' || COALESCE(email, '')))`)
}
```

---

## 5. Authentication System

### 5.1 Authentication Flow Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Authentication Routes                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Web UI Authentication                    DAV Client Authentication │
│  ─────────────────────                    ─────────────────────────│
│                                                                     │
│  ┌─────────────────┐                      ┌─────────────────┐      │
│  │ POST /api/login │                      │ DAV endpoints   │      │
│  │ Email + Password│                      │ /dav/*          │      │
│  └────────┬────────┘                      └────────┬────────┘      │
│           │                                        │                │
│  ┌────────▼────────┐                      ┌────────▼────────┐      │
│  │ GET /api/oauth/ │                      │ HTTP Basic Auth │      │
│  │ {provider}      │                      │ username +      │      │
│  └────────┬────────┘                      │ app-password    │      │
│           │                               └────────┬────────┘      │
│  ┌────────▼────────┐                               │               │
│  │ SAML ACS        │                      ┌────────▼────────┐      │
│  │ /api/saml/acs   │                      │ Validate app    │      │
│  └────────┬────────┘                      │ password hash   │      │
│           │                               └────────┬────────┘      │
│           │                                        │               │
│  ┌────────▼───────────────────────────────────────▼────────┐      │
│  │                    User Context                          │      │
│  │              (UserID, Username, Scopes)                  │      │
│  └──────────────────────────┬───────────────────────────────┘      │
│                             │                                       │
│                    ┌────────▼────────┐                             │
│                    │  JWT Session    │  (Web UI only)              │
│                    │  Cookie/Header  │                             │
│                    └─────────────────┘                             │
└─────────────────────────────────────────────────────────────────────┘
```

### 5.2 JWT Token Structure

```go
// internal/adapter/auth/jwt.go
type Claims struct {
    UserID   uint   `json:"uid"`
    Username string `json:"username"`
    Email    string `json:"email"`
    jwt.RegisteredClaims
}

type TokenPair struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    ExpiresAt    time.Time `json:"expires_at"`
    TokenType    string    `json:"token_type"` // "Bearer"
}

func (s *JWTService) GenerateTokenPair(user *User) (*TokenPair, error) {
    // Access token (short-lived: 15 minutes)
    accessClaims := Claims{
        UserID:   user.ID,
        Username: user.Username,
        Email:    user.Email,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
            Subject:   user.UUID,
        },
    }
    accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
    accessSigned, _ := accessToken.SignedString(s.secret)

    // Refresh token (long-lived: 7 days)
    refreshClaims := jwt.RegisteredClaims{
        ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
        IssuedAt:  jwt.NewNumericDate(time.Now()),
        Subject:   user.UUID,
    }
    refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
    refreshSigned, _ := refreshToken.SignedString(s.refreshSecret)

    return &TokenPair{
        AccessToken:  accessSigned,
        RefreshToken: refreshSigned,
        ExpiresAt:    accessClaims.ExpiresAt.Time,
        TokenType:    "Bearer",
    }, nil
}
```

### 5.3 OAuth2/OIDC Integration

```go
// internal/adapter/auth/oauth.go
type OAuthService struct {
    providers map[string]*OAuthProvider
}

type OAuthProvider struct {
    config   oauth2.Config
    verifier *oidc.IDTokenVerifier
}

func (s *OAuthService) GetAuthURL(providerName, state string) (string, error) {
    provider, ok := s.providers[providerName]
    if !ok {
        return "", errors.New("unknown provider")
    }
    return provider.config.AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

func (s *OAuthService) HandleCallback(ctx context.Context, providerName, code string) (*OAuthUserInfo, error) {
    provider := s.providers[providerName]

    // Exchange code for tokens
    token, err := provider.config.Exchange(ctx, code)
    if err != nil {
        return nil, err
    }

    // Extract and verify ID token
    rawIDToken, ok := token.Extra("id_token").(string)
    if !ok {
        return nil, errors.New("no id_token in response")
    }

    idToken, err := provider.verifier.Verify(ctx, rawIDToken)
    if err != nil {
        return nil, err
    }

    // Extract claims
    var claims struct {
        Email         string `json:"email"`
        EmailVerified bool   `json:"email_verified"`
        Name          string `json:"name"`
        Subject       string `json:"sub"`
    }
    idToken.Claims(&claims)

    return &OAuthUserInfo{
        Provider:      providerName,
        ProviderID:    claims.Subject,
        Email:         claims.Email,
        Name:          claims.Name,
        EmailVerified: claims.EmailVerified,
        AccessToken:   token.AccessToken,
        RefreshToken:  token.RefreshToken,
        Expiry:        token.Expiry,
    }, nil
}
```

### 5.4 App Password Authentication for DAV

```go
// internal/adapter/auth/basic.go
func DAVBasicAuthMiddleware(userRepo UserRepository, appPwdRepo AppPasswordRepository) gin.HandlerFunc {
    return func(c *gin.Context) {
        username, password, ok := c.Request.BasicAuth()
        if !ok {
            c.Header("WWW-Authenticate", `Basic realm="CalDAV/CardDAV"`)
            c.AbortWithStatus(http.StatusUnauthorized)
            return
        }

        // Find user by username
        user, err := userRepo.FindByUsername(c.Request.Context(), username)
        if err != nil {
            c.AbortWithStatus(http.StatusUnauthorized)
            return
        }

        // Find active app passwords for user
        appPasswords, err := appPwdRepo.FindActiveByUserID(c.Request.Context(), user.ID)
        if err != nil {
            c.AbortWithStatus(http.StatusUnauthorized)
            return
        }

        // Check each app password
        var matchedPassword *AppPassword
        for _, ap := range appPasswords {
            if bcrypt.CompareHashAndPassword([]byte(ap.PasswordHash), []byte(password)) == nil {
                matchedPassword = &ap
                break
            }
        }

        if matchedPassword == nil {
            c.AbortWithStatus(http.StatusUnauthorized)
            return
        }

        // Update last used
        appPwdRepo.UpdateLastUsed(c.Request.Context(), matchedPassword.ID, c.ClientIP())

        // Set user in context
        c.Set("user", user)
        c.Set("appPassword", matchedPassword)
        c.Next()
    }
}
```

---

## 6. CalDAV Implementation

### 6.1 CalDAV Backend Interface

The `emersion/go-webdav/caldav` package defines the `Backend` interface that must be implemented:

```go
// From emersion/go-webdav/caldav
type Backend interface {
    CalendarHomeSetPath(ctx context.Context) (string, error)
    ListCalendars(ctx context.Context) ([]Calendar, error)
    GetCalendar(ctx context.Context, path string) (*Calendar, error)
    GetCalendarObject(ctx context.Context, path string, req *CalendarCompRequest) (*CalendarObject, error)
    ListCalendarObjects(ctx context.Context, path string, req *CalendarCompRequest) ([]CalendarObject, error)
    QueryCalendarObjects(ctx context.Context, path string, query *CalendarQuery) ([]CalendarObject, error)
    PutCalendarObject(ctx context.Context, path string, calendar *ical.Calendar, opts *PutCalendarObjectOptions) (loc string, err error)
    DeleteCalendarObject(ctx context.Context, path string) error

    // Optional
    CreateCalendar(ctx context.Context, calendar *Calendar) error
    DeleteCalendar(ctx context.Context, path string) error
}
```

### 6.2 CalDAV Backend Implementation

```go
// internal/adapter/webdav/caldav_backend.go
type CalDAVBackend struct {
    calendarRepo CalendarRepository
    eventRepo    CalendarObjectRepository
    syncRepo     SyncRepository
}

func (b *CalDAVBackend) CalendarHomeSetPath(ctx context.Context) (string, error) {
    user := UserFromContext(ctx)
    return fmt.Sprintf("/dav/calendars/%s/", user.Username), nil
}

func (b *CalDAVBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
    user := UserFromContext(ctx)

    // Get owned calendars
    owned, _ := b.calendarRepo.FindByUserID(ctx, user.ID)

    // Get shared calendars
    shared, _ := b.calendarRepo.FindSharedWithUserID(ctx, user.ID)

    var result []caldav.Calendar
    for _, cal := range append(owned, shared...) {
        result = append(result, caldav.Calendar{
            Path:                  cal.Path,
            Name:                  cal.Name,
            Description:           cal.Description,
            MaxResourceSize:       cal.MaxResourceSize,
            SupportedComponentSet: strings.Split(cal.SupportedComponents, ","),
        })
    }
    return result, nil
}

func (b *CalDAVBackend) QueryCalendarObjects(ctx context.Context, path string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
    cal, err := b.calendarRepo.FindByPath(ctx, path)
    if err != nil {
        return nil, err
    }

    // Check access
    if err := b.checkAccess(ctx, cal, "read"); err != nil {
        return nil, err
    }

    // Build database query from CalDAV filter
    filter := b.buildFilter(query.CompFilter)
    objects, _ := b.eventRepo.Query(ctx, cal.ID, filter)

    var result []caldav.CalendarObject
    for _, obj := range objects {
        icalData, _ := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
        result = append(result, caldav.CalendarObject{
            Path:          obj.Path,
            ModTime:       obj.UpdatedAt,
            ContentLength: obj.ContentLength,
            ETag:          obj.ETag,
            Data:          icalData,
        })
    }
    return result, nil
}

func (b *CalDAVBackend) PutCalendarObject(ctx context.Context, path string, calendar *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (string, error) {
    calPath := extractCalendarPath(path)
    cal, _ := b.calendarRepo.FindByPath(ctx, calPath)

    // Check write access
    if err := b.checkAccess(ctx, cal, "write"); err != nil {
        return "", err
    }

    // Serialize iCalendar
    var buf bytes.Buffer
    ical.NewEncoder(&buf).Encode(calendar)
    icalData := buf.String()

    // Extract metadata from iCalendar
    event := extractEventFromICal(calendar)
    etag := generateETag(icalData)

    // Check If-Match / If-None-Match
    existing, _ := b.eventRepo.FindByPath(ctx, path)
    if err := validateConditionalRequest(opts, existing); err != nil {
        return "", err
    }

    // Create or update
    obj := &CalendarObject{
        UUID:          uuid.New().String(),
        CalendarID:    cal.ID,
        Path:          path,
        UID:           event.UID,
        ComponentType: event.ComponentType,
        ETag:          etag,
        ICalData:      icalData,
        ContentLength: int64(len(icalData)),
        Summary:       event.Summary,
        StartTime:     event.StartTime,
        EndTime:       event.EndTime,
        IsAllDay:      event.IsAllDay,
    }

    if existing != nil {
        obj.ID = existing.ID
        b.eventRepo.Update(ctx, obj)
        b.syncRepo.LogChange(ctx, cal.ID, "calendar", path, "modified")
    } else {
        b.eventRepo.Create(ctx, obj)
        b.syncRepo.LogChange(ctx, cal.ID, "calendar", path, "created")
    }

    // Update calendar CTag and SyncToken
    b.updateCalendarMetadata(ctx, cal)

    return path, nil
}
```

### 6.3 WebDAV-Sync Implementation

```go
// internal/adapter/webdav/sync.go
func (b *CalDAVBackend) SyncCollection(ctx context.Context, path string, syncToken string) (*SyncResponse, error) {
    cal, _ := b.calendarRepo.FindByPath(ctx, path)

    if syncToken == "" {
        // Initial sync - return all objects
        objects, _ := b.eventRepo.FindByCalendarID(ctx, cal.ID)
        newToken := b.syncRepo.GetCurrentToken(ctx, cal.ID, "calendar")

        return &SyncResponse{
            SyncToken: newToken,
            Items:     objectsToSyncItems(objects),
        }, nil
    }

    // Incremental sync - return changes since token
    changes, newToken, err := b.syncRepo.GetChangesSince(ctx, cal.ID, "calendar", syncToken)
    if err != nil {
        return nil, &webdav.HTTPError{Code: 400, Err: errors.New("invalid sync-token")}
    }

    return &SyncResponse{
        SyncToken: newToken,
        Changes:   changes,
    }, nil
}
```

---

## 7. CardDAV Implementation

### 7.1 CardDAV Backend Interface

```go
// From emersion/go-webdav/carddav
type Backend interface {
    AddressBookHomeSetPath(ctx context.Context) (string, error)
    ListAddressBooks(ctx context.Context) ([]AddressBook, error)
    GetAddressBook(ctx context.Context, path string) (*AddressBook, error)
    GetAddressObject(ctx context.Context, path string, req *AddressDataRequest) (*AddressObject, error)
    ListAddressObjects(ctx context.Context, path string, req *AddressDataRequest) ([]AddressObject, error)
    QueryAddressObjects(ctx context.Context, path string, query *AddressBookQuery) ([]AddressObject, error)
    PutAddressObject(ctx context.Context, path string, card vcard.Card, opts *PutAddressObjectOptions) (loc string, err error)
    DeleteAddressObject(ctx context.Context, path string) error

    // Optional
    CreateAddressBook(ctx context.Context, addressBook *AddressBook) error
    DeleteAddressBook(ctx context.Context, path string) error
}
```

### 7.2 CardDAV Backend Implementation

```go
// internal/adapter/webdav/carddav_backend.go
type CardDAVBackend struct {
    addressbookRepo AddressBookRepository
    contactRepo     AddressObjectRepository
    syncRepo        SyncRepository
}

func (b *CardDAVBackend) AddressBookHomeSetPath(ctx context.Context) (string, error) {
    user := UserFromContext(ctx)
    return fmt.Sprintf("/dav/addressbooks/%s/", user.Username), nil
}

func (b *CardDAVBackend) QueryAddressObjects(ctx context.Context, path string, query *carddav.AddressBookQuery) ([]carddav.AddressObject, error) {
    ab, _ := b.addressbookRepo.FindByPath(ctx, path)

    if err := b.checkAccess(ctx, ab, "read"); err != nil {
        return nil, err
    }

    // Build query from CardDAV prop-filter
    filter := b.buildFilter(query.PropFilters)
    contacts, _ := b.contactRepo.Query(ctx, ab.ID, filter)

    var result []carddav.AddressObject
    for _, c := range contacts {
        card, _ := vcard.NewDecoder(strings.NewReader(c.VCardData)).Decode()
        result = append(result, carddav.AddressObject{
            Path:          c.Path,
            ModTime:       c.UpdatedAt,
            ContentLength: c.ContentLength,
            ETag:          c.ETag,
            Card:          card,
        })
    }
    return result, nil
}

func (b *CardDAVBackend) PutAddressObject(ctx context.Context, path string, card vcard.Card, opts *carddav.PutAddressObjectOptions) (string, error) {
    abPath := extractAddressBookPath(path)
    ab, _ := b.addressbookRepo.FindByPath(ctx, abPath)

    if err := b.checkAccess(ctx, ab, "write"); err != nil {
        return "", err
    }

    // Serialize vCard
    var buf bytes.Buffer
    vcard.NewEncoder(&buf).Encode(card)
    vcardData := buf.String()

    // Extract metadata
    contact := extractContactFromVCard(card)
    etag := generateETag(vcardData)

    existing, _ := b.contactRepo.FindByPath(ctx, path)
    if err := validateConditionalRequest(opts, existing); err != nil {
        return "", err
    }

    obj := &AddressObject{
        UUID:          uuid.New().String(),
        AddressBookID: ab.ID,
        Path:          path,
        UID:           contact.UID,
        ETag:          etag,
        VCardData:     vcardData,
        VCardVersion:  detectVCardVersion(card),
        ContentLength: int64(len(vcardData)),
        FormattedName: contact.FormattedName,
        GivenName:     contact.GivenName,
        FamilyName:    contact.FamilyName,
        Email:         contact.PrimaryEmail,
        Phone:         contact.PrimaryPhone,
        Organization:  contact.Organization,
    }

    if existing != nil {
        obj.ID = existing.ID
        b.contactRepo.Update(ctx, obj)
        b.syncRepo.LogChange(ctx, ab.ID, "addressbook", path, "modified")
    } else {
        b.contactRepo.Create(ctx, obj)
        b.syncRepo.LogChange(ctx, ab.ID, "addressbook", path, "created")
    }

    b.updateAddressBookMetadata(ctx, ab)

    return path, nil
}
```

---

## 8. REST API Design

### 8.1 API Structure

```
/api/v1/
├── auth/
│   ├── POST   /register           # User registration
│   ├── POST   /login              # Email/password login
│   ├── POST   /logout             # Logout (invalidate session)
│   ├── POST   /refresh            # Refresh access token
│   ├── GET    /oauth/{provider}   # Initiate OAuth flow
│   ├── POST   /oauth/{provider}/callback  # OAuth callback
│   ├── GET    /saml/metadata      # SAML SP metadata
│   └── POST   /saml/acs           # SAML Assertion Consumer Service
│
├── users/
│   ├── GET    /me                 # Get current user profile
│   ├── PATCH  /me                 # Update profile
│   └── PUT    /me/password        # Change password
│
├── app-passwords/
│   ├── GET    /                   # List app passwords
│   ├── POST   /                   # Create app password
│   └── DELETE /{id}               # Revoke app password
│
├── calendars/
│   ├── GET    /                   # List calendars
│   ├── POST   /                   # Create calendar
│   ├── GET    /{id}               # Get calendar
│   ├── PATCH  /{id}               # Update calendar
│   ├── DELETE /{id}               # Delete calendar
│   ├── GET    /{id}/events        # List events (with date filters)
│   ├── POST   /{id}/events        # Create event
│   ├── GET    /{id}/events/{eid}  # Get event
│   ├── PATCH  /{id}/events/{eid}  # Update event
│   ├── DELETE /{id}/events/{eid}  # Delete event
│   ├── GET    /{id}/shares        # List shares
│   ├── POST   /{id}/shares        # Share calendar
│   ├── PATCH  /{id}/shares/{sid}  # Update share
│   ├── DELETE /{id}/shares/{sid}  # Remove share
│   ├── GET    /{id}/credentials        # List per-calendar credentials
│   ├── POST   /{id}/credentials        # Create calendar credential (returns password once)
│   ├── GET    /{id}/credentials/{cid}  # Get credential info (no password)
│   ├── PATCH  /{id}/credentials/{cid}  # Update credential (name, permission, expiry)
│   └── DELETE /{id}/credentials/{cid}  # Revoke credential
│
├── addressbooks/
│   ├── GET    /                   # List address books
│   ├── POST   /                   # Create address book
│   ├── GET    /{id}               # Get address book
│   ├── PATCH  /{id}               # Update address book
│   ├── DELETE /{id}               # Delete address book
│   ├── GET    /{id}/contacts      # List contacts
│   ├── POST   /{id}/contacts      # Create contact
│   ├── GET    /{id}/contacts/{cid}  # Get contact
│   ├── PATCH  /{id}/contacts/{cid}  # Update contact
│   ├── DELETE /{id}/contacts/{cid}  # Delete contact
│   ├── GET    /{id}/credentials        # List per-addressbook credentials
│   ├── POST   /{id}/credentials        # Create addressbook credential
│   ├── GET    /{id}/credentials/{cid}  # Get credential info
│   ├── PATCH  /{id}/credentials/{cid}  # Update credential
│   └── DELETE /{id}/credentials/{cid}  # Revoke credential
│
└── contacts/
    └── GET    /search             # Global contact search
```

### 8.2 Request/Response DTOs

```go
// internal/adapter/http/dto/calendar.go

// Request DTOs
type CreateCalendarRequest struct {
    Name        string `json:"name" validate:"required,max=255"`
    Description string `json:"description" validate:"max=1000"`
    Color       string `json:"color" validate:"omitempty,hexcolor"`
    Timezone    string `json:"timezone" validate:"omitempty,timezone"`
}

type CreateEventRequest struct {
    Summary     string    `json:"summary" validate:"required,max=500"`
    Description string    `json:"description"`
    Location    string    `json:"location" validate:"max=500"`
    StartTime   time.Time `json:"startTime" validate:"required"`
    EndTime     time.Time `json:"endTime" validate:"required,gtfield=StartTime"`
    IsAllDay    bool      `json:"isAllDay"`
    Recurrence  string    `json:"recurrence"` // RRULE string
}

type ShareCalendarRequest struct {
    UserEmail  string `json:"userEmail" validate:"required,email"`
    Permission string `json:"permission" validate:"required,oneof=read read-write"`
}

// Response DTOs
type CalendarResponse struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Description string    `json:"description"`
    Color       string    `json:"color"`
    Timezone    string    `json:"timezone"`
    IsDefault   bool      `json:"isDefault"`
    IsOwner     bool      `json:"isOwner"`
    Permission  string    `json:"permission"`
    EventCount  int       `json:"eventCount"`
    DAVUrl      string    `json:"davUrl"` // CalDAV URL for this calendar
    CreatedAt   time.Time `json:"createdAt"`
    UpdatedAt   time.Time `json:"updatedAt"`
}

type EventResponse struct {
    ID          string    `json:"id"`
    CalendarID  string    `json:"calendarId"`
    Summary     string    `json:"summary"`
    Description string    `json:"description"`
    Location    string    `json:"location"`
    StartTime   time.Time `json:"startTime"`
    EndTime     time.Time `json:"endTime"`
    IsAllDay    bool      `json:"isAllDay"`
    Recurrence  string    `json:"recurrence"`
    CreatedAt   time.Time `json:"createdAt"`
    UpdatedAt   time.Time `json:"updatedAt"`
}

// internal/adapter/http/dto/contact.go

type CreateContactRequest struct {
    FormattedName string           `json:"formattedName" validate:"required,max=500"`
    GivenName     string           `json:"givenName" validate:"max=255"`
    FamilyName    string           `json:"familyName" validate:"max=255"`
    Emails        []EmailDTO       `json:"emails"`
    Phones        []PhoneDTO       `json:"phones"`
    Addresses     []AddressDTO     `json:"addresses"`
    Organization  string           `json:"organization" validate:"max=255"`
    Title         string           `json:"title" validate:"max=255"`
    Notes         string           `json:"notes"`
    Birthday      *time.Time       `json:"birthday"`
}

type EmailDTO struct {
    Type  string `json:"type"` // home, work, other
    Value string `json:"value" validate:"required,email"`
}

type PhoneDTO struct {
    Type  string `json:"type"` // mobile, home, work, fax
    Value string `json:"value" validate:"required"`
}

type ContactResponse struct {
    ID            string       `json:"id"`
    AddressBookID string       `json:"addressBookId"`
    FormattedName string       `json:"formattedName"`
    GivenName     string       `json:"givenName"`
    FamilyName    string       `json:"familyName"`
    Emails        []EmailDTO   `json:"emails"`
    Phones        []PhoneDTO   `json:"phones"`
    Addresses     []AddressDTO `json:"addresses"`
    Organization  string       `json:"organization"`
    Title         string       `json:"title"`
    Notes         string       `json:"notes"`
    Birthday      *time.Time   `json:"birthday"`
    PhotoURL      string       `json:"photoUrl"`
    CreatedAt     time.Time    `json:"createdAt"`
    UpdatedAt     time.Time    `json:"updatedAt"`
}
```

---

## 9. Frontend Architecture

### 9.1 Technology Choice Rationale

**Vue 3 + Nuxt 3** was selected over Angular and Flutter Web for:

| Factor             | Vue 3/Nuxt               | Angular        | Flutter Web         |
| ------------------ | ------------------------ | -------------- | ------------------- |
| Bundle Size        | ~30KB (Vue core)         | ~130KB+        | 1MB+                |
| Learning Curve     | Gentle                   | Steep          | Moderate (Dart)     |
| Calendar Libraries | Excellent (FullCalendar) | Good           | Limited             |
| SSR Support        | Native (Nuxt)            | Requires setup | Poor                |
| Development Speed  | Fast                     | Moderate       | Moderate            |
| Self-hosting Fit   | Excellent                | Good           | Poor (large assets) |

### 9.2 Frontend Structure

```
frontend/
├── nuxt.config.ts
├── app.vue
├── components/
│   ├── calendar/
│   │   ├── CalendarView.vue        # FullCalendar wrapper
│   │   ├── EventForm.vue           # Create/edit event modal
│   │   ├── EventDetail.vue         # Event detail popover
│   │   ├── CalendarList.vue        # Sidebar calendar list
│   │   └── MiniCalendar.vue        # Date picker widget
│   ├── contacts/
│   │   ├── ContactList.vue         # Contact list with search
│   │   ├── ContactForm.vue         # Create/edit contact
│   │   ├── ContactDetail.vue       # Contact details panel
│   │   └── ContactAvatar.vue       # Avatar component
│   ├── sharing/
│   │   ├── ShareModal.vue          # Share dialog
│   │   └── ShareList.vue           # List of shares
│   ├── settings/
│   │   ├── AppPasswordList.vue     # App password management
│   │   ├── AppPasswordCreate.vue   # Create app password dialog
│   │   └── ProfileForm.vue         # Profile settings
│   └── auth/
│       ├── LoginForm.vue
│       ├── RegisterForm.vue
│       └── OAuthButtons.vue
├── composables/
│   ├── useAuth.ts                  # Authentication state & methods
│   ├── useCalendars.ts             # Calendar CRUD operations
│   ├── useEvents.ts                # Event CRUD operations
│   ├── useContacts.ts              # Contact CRUD operations
│   └── useApi.ts                   # API client configuration
├── stores/
│   ├── auth.ts                     # Pinia auth store
│   ├── calendars.ts                # Calendars & events store
│   └── contacts.ts                 # Address books & contacts store
├── pages/
│   ├── index.vue                   # Redirect to /calendar
│   ├── calendar/
│   │   └── [[...view]].vue         # /calendar, /calendar/week, /calendar/day
│   ├── contacts/
│   │   └── index.vue               # Contacts page
│   ├── settings/
│   │   ├── index.vue               # Settings overview
│   │   ├── profile.vue             # Profile settings
│   │   └── app-passwords.vue       # App passwords management
│   ├── setup/
│   │   └── index.vue               # Client setup instructions
│   └── auth/
│       ├── login.vue
│       ├── register.vue
│       └── callback.vue            # OAuth callback handler
├── layouts/
│   ├── default.vue                 # Main app layout with sidebar
│   └── auth.vue                    # Auth pages layout (no sidebar)
├── middleware/
│   └── auth.ts                     # Route authentication guard
├── types/
│   ├── calendar.ts
│   ├── contact.ts
│   └── user.ts
└── utils/
    ├── date.ts                     # Date formatting utilities
    └── validation.ts               # Form validation schemas (Zod)
```

### 9.3 Key Frontend Libraries

```json
{
  "dependencies": {
    "nuxt": "^3.10.0",
    "vue": "^3.4.0",
    "@pinia/nuxt": "^0.5.1",
    "@fullcalendar/vue3": "^6.1.11",
    "@fullcalendar/core": "^6.1.11",
    "@fullcalendar/daygrid": "^6.1.11",
    "@fullcalendar/timegrid": "^6.1.11",
    "@fullcalendar/interaction": "^6.1.11",
    "@fullcalendar/list": "^6.1.11",
    "primevue": "^3.50.0",
    "@vueuse/nuxt": "^10.9.0",
    "zod": "^3.22.4",
    "@tailwindcss/forms": "^0.5.7",
    "date-fns": "^3.3.1"
  }
}
```

---

## 10. Deployment Architecture

### 10.1 Docker Configuration

```dockerfile
# Dockerfile
# Build backend
FROM golang:1.22-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run generate

# Final image
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=backend-builder /app/server .
COPY --from=frontend-builder /frontend/.output/public ./web

EXPOSE 8080
ENTRYPOINT ["./server"]
```

### 10.2 Docker Compose

```yaml
# docker-compose.yaml
version: "3.9"

services:
  caldav:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://caldav:${DB_PASSWORD}@db:5432/caldav?sslmode=disable
      - JWT_SECRET=${JWT_SECRET}
      - JWT_REFRESH_SECRET=${JWT_REFRESH_SECRET}
      - BASE_URL=https://caldav.example.com
      - OAUTH_GOOGLE_CLIENT_ID=${OAUTH_GOOGLE_CLIENT_ID}
      - OAUTH_GOOGLE_CLIENT_SECRET=${OAUTH_GOOGLE_CLIENT_SECRET}
    depends_on:
      db:
        condition: service_healthy
    volumes:
      - ./config.yaml:/app/config.yaml:ro
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  db:
    image: postgres:16-alpine
    environment:
      - POSTGRES_USER=caldav
      - POSTGRES_PASSWORD=${DB_PASSWORD}
      - POSTGRES_DB=caldav
    volumes:
      - postgres_data:/var/lib/postgresql/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U caldav"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
```

### 10.3 Configuration

```yaml
# config.yaml
server:
  host: "0.0.0.0"
  port: 8080
  base_url: "${BASE_URL}"
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
  cors:
    allowed_origins:
      - "https://caldav.example.com"
    allowed_methods:
      - "GET"
      - "POST"
      - "PUT"
      - "PATCH"
      - "DELETE"
      - "OPTIONS"

database:
  driver: postgres # postgres, mysql, sqlite
  url: "${DATABASE_URL}"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 5m

auth:
  jwt:
    secret: "${JWT_SECRET}"
    refresh_secret: "${JWT_REFRESH_SECRET}"
    access_token_ttl: 15m
    refresh_token_ttl: 168h # 7 days

  password:
    bcrypt_cost: 12
    min_length: 8

  oauth:
    google:
      enabled: true
      client_id: "${OAUTH_GOOGLE_CLIENT_ID}"
      client_secret: "${OAUTH_GOOGLE_CLIENT_SECRET}"
    microsoft:
      enabled: false
      client_id: ""
      client_secret: ""
      tenant: "common"

  saml:
    enabled: false
    entity_id: ""
    certificate_file: ""
    key_file: ""
    idp_metadata_url: ""

rate_limiting:
  enabled: true
  requests_per_minute: 100
  auth_requests_per_minute: 10

logging:
  level: info # debug, info, warn, error
  format: json # json, text
```

---

## 11. Security Considerations

### 11.1 Authentication Security

| Aspect           | Implementation                        |
| ---------------- | ------------------------------------- |
| Password Hashing | bcrypt with cost factor 12+           |
| JWT Signing      | HS256 with 256-bit secret             |
| App Passwords    | 32-character cryptographically random |
| Session Tokens   | Refresh token rotation on use         |
| Rate Limiting    | 10 auth requests/minute per IP        |

### 11.2 Input Validation

```go
// All iCalendar/vCard input is validated
func validateICalendar(data string) error {
    cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
    if err != nil {
        return errors.New("invalid iCalendar format")
    }

    // Validate required properties
    for _, event := range cal.Events() {
        if event.Props.Get(ical.PropUID) == nil {
            return errors.New("missing UID property")
        }
        if event.Props.Get(ical.PropDTStamp) == nil {
            return errors.New("missing DTSTAMP property")
        }
    }

    // Size limits
    if len(data) > 10*1024*1024 { // 10MB
        return errors.New("calendar object too large")
    }

    return nil
}
```

### 11.3 Transport Security

- HTTPS required for production (via reverse proxy or native TLS)
- HSTS headers recommended
- Secure cookie flags (Secure, HttpOnly, SameSite=Strict)

### 11.4 Access Control

- All DAV operations verify user ownership or share permissions
- App passwords can be scoped (CalDAV-only, CardDAV-only)
- Shared calendar access respects read/read-write permissions

---

## Appendix: Key Library Documentation

| Library            | Documentation                                    |
| ------------------ | ------------------------------------------------ |
| emersion/go-webdav | https://pkg.go.dev/github.com/emersion/go-webdav |
| GORM               | https://gorm.io/docs/                            |
| coreos/go-oidc     | https://pkg.go.dev/github.com/coreos/go-oidc/v3  |
| crewjam/saml       | https://pkg.go.dev/github.com/crewjam/saml       |
| golang-jwt/jwt     | https://pkg.go.dev/github.com/golang-jwt/jwt/v5  |
| Gin                | https://gin-gonic.com/docs/                      |
| FullCalendar       | https://fullcalendar.io/docs                     |
| Vue 3              | https://vuejs.org/guide/                         |
| Nuxt 3             | https://nuxt.com/docs                            |
| PrimeVue           | https://primevue.org/introduction/               |
