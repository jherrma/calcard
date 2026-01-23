# Domain Layer

This is the innermost layer of the application. It contains the core business logic and state of the application, completely independent of any external frameworks, databases, or UI.

## Responsibility

The domain layer defines:

- **Entities**: Business models representing the core concepts (e.g., `User`, `SystemSetting`).
- **Interfaces**: Crucial abstractions for data persistence (`UserRepository`, `RefreshTokenRepository`) and external services (`TokenProvider`).
- **Domain Logic**: Business rules that are intrinsic to the data itself (e.g., password hashing methods on the `User` model).

## Key Components

### [User](file:///home/jherrmann/go/src/calcard/server/internal/domain/user/user.go)

The core user entity, containing profile data and security status.

### [RefreshToken](file:///home/jherrmann/go/src/calcard/server/internal/domain/user/refresh_token.go)

Opaque tokens used for session persistence, linked to users and identifying client context (User Agent, IP).

### [SystemSetting](file:///home/jherrmann/go/src/calcard/server/internal/domain/system_setting.go)

Persistent system configuration stored in the database, such as the dynamically generated JWT secret.

### [Repository Interfaces](file:///home/jherrmann/go/src/calcard/server/internal/domain/user/repository.go)

Describe how use cases should interact with data storage.

## Design Constraints

- **Zero Dependencies**: This package must not import anything from `usecase`, `adapter`, or `infrastructure`.
- **Pure Go**: Should only depend on the Go standard library (and potentially very minimal utility libraries if absolutely necessary).
- **Stability**: This is the most stable part of the codebase; changes here usually trigger changes in all other layers.
