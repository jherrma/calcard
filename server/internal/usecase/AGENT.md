# Usecase Layer

This directory contains the application-specific business rules. It orchestrates the flow of data to and from the domain entities, and directs those domain entities to use their critical business rules to achieve the goals of the use case.

## Responsibility

The usecase layer is responsible for:

- Implementing the user stories (e.g., "User Login", "Account Registration").
- Coordinating interactions between domain models and data access (repositories).
- Validation of business constraints that cross multiple entities.
- Ensuring the data is in the correct format for the domain layer.

## Subdirectories

### [auth/](file:///home/jherrmann/go/src/calcard/server/internal/usecase/auth)

Provides all authentication and authorization logic:

- **`register.go`**: Handles new user creation, password hashing, and triggering verification emails.
- **`login.go`**: Validates credentials and generates access/refresh tokens via the `TokenProvider`.
- **`refresh.go`**: Exchanges a valid refresh token for a new access token.
- **`verify.go`**: Handles email verification tokens.
- **`logout.go`**: Invalidates refresh tokens to end a session.

## Key Principles

1. **No External Dependencies**: This layer remains ignorant of whether it's being called by an HTTP request, a CLI command, or a background worker.
2. **Interface Driven**: It interacts with the outside world (database, email, tokens) exclusively via interfaces defined in the `domain` layer.
3. **Pure Logic**: It should ideally contain no technical "leaks" from infrastructure frameworks like GORM or Fiber.
