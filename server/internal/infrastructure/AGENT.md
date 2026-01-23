# Infrastructure Layer

This directory contains the technical implementations and external library integrations. It follows the principles of **Clean Architecture** by providing concrete implementations for the interfaces required by the application's upper layers.

## Subdirectories

### [database/](file:///home/jherrmann/go/src/calcard/server/internal/infrastructure/database)

- **Purpose**: Manages the persistence layer.
- **Key Components**:
  - **Driver Support**: Implementations for both `sqlite.go` and `postgres.go` using GORM.
  - **Connection Management**: `database.go` provides a unified way to initialize the database connection based on configuration.
  - **Migrations**: `migrations.go` handles the automatic database schema updates using GORM's `AutoMigrate`.

### [server/](file:///home/jherrmann/go/src/calcard/server/internal/infrastructure/server)

- **Purpose**: Manages the HTTP server lifecycle and request pipeline.
- **Key Components**:
  - **Initialization**: `server.go` configures the Fiber application instance.
  - **Routing**: `routes.go` is where all API endpoints are registered and handlers are injected with their dependencies.
  - **Middleware**: `middleware.go` configures global HTTP middleware like CORS, Recovery, and Request ID logging.

### [email/](file:///home/jherrmann/go/src/calcard/server/internal/infrastructure/email)

- **Purpose**: Handles external communication services.
- **Key Components**:
  - **SMTP**: `smtp.go` provides the concrete implementation for sending emails via SMTP, satisfying the email service interfaces used by authentication use cases.

## Design Philosophy

The infrastructure layer is the "outermost" layer. It depends on internal layers (like `config` or `adapter`) but the internal business logic (in `domain` and `usecase`) never depends on code in this directory. Instead, they use interfaces which are fulfilled by the implementations found here.
