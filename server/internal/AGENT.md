# Internal Server Architecture

This directory contains the core business logic and implementation details of the CalDAV/CardDAV server, following a **Clean Architecture** (Hexagonal Architecture) pattern.

## Directory Structure

### [domain/](file:///home/jherrmann/go/src/calcard/server/internal/domain)

- **Purpose**: Contains the core business entities and the logic that is intrinsic to the domain.
- **Contents**:
  - Domain models (e.g., `User`, `RefreshToken`, `SystemSetting`).
  - Repository interfaces (defining _what_ data needs to be stored, but not _how_).
  - Domain service interfaces.
- **Dependencies**: None. This is the heart of the application.

### [usecase/](file:///home/jherrmann/go/src/calcard/server/internal/usecase)

- **Purpose**: Implements the application-specific business rules.
- **Contents**:
  - Use cases (e.g., `LoginUseCase`, `RegisterUseCase`, `RefreshUseCase`).
  - Orchestrating domain objects to perform specific actions.
- **Dependencies**: Only depends on `domain`.

### [adapter/](file:///home/jherrmann/go/src/calcard/server/internal/adapter)

- **Purpose**: Translates data between the internal layers and the external world.
- **Contents**:
  - **http/**: Controllers/Handlers that receive HTTP requests, parse them into usecase inputs, and format usecase outputs into HTTP responses (DTOs).
  - **repository/**: Concrete implementations of the interfaces defined in the `domain` layer (e.g., GORM database repositories).
  - **auth/**: Concrete implementations of security/token providers (e.g., JWT).
- **Dependencies**: Depends on `domain` and `usecase`.

### [infrastructure/](file:///home/jherrmann/go/src/calcard/server/internal/infrastructure)

- **Purpose**: Contains the purely technical implementations and external libraries.
- **Contents**:
  - Database connection setup (SQLite/Postgres).
  - Server initialization (Fiber framework).
  - Email delivery implementations (SMTP).
  - Database migrations.
- **Dependencies**: Can depend on any other layer, but mostly provides implementation details for `adapter`.

### [config/](file:///home/jherrmann/go/src/calcard/server/internal/config)

- **Purpose**: Manages application configuration.
- **Contents**:
  - Loading settings from environment variables and YAML files.
  - Default configuration values.
- **Dependencies**: None.

## Request Flow

1. **Infrastructure**: Server (Fiber) accepts a request and passes it to an **HTTP Adapter** (Handler).
2. **Adapter (HTTP)**: Validates input, converts it to a **Usecase** request object.
3. **Usecase**: Executes business logic, interacts with **Domain** entities and calls **Repository** interfaces.
4. **Adapter (Repository)**: The concrete implementation of the repository interface handles the actual database interaction.
5. **Usecase**: Returns a result back to the **Adapter (HTTP)**.
6. **Adapter (HTTP)**: Formats the result into an HTTP response (JSON).
