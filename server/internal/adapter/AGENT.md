# Adapter Layer

The adapter layer is responsible for translating data between the application's internal layers (domain and use case) and the outside world. It follows the **Clean Architecture** pattern by implementing the "Port" interfaces defined in the domain layer.

## Subdirectories

### [http/](file:///home/jherrmann/go/src/calcard/server/internal/adapter/http)

- **Purpose**: Handles HTTP communication using the Fiber framework.
- **Key Components**:
  - **Handlers**: Orchestrate use cases based on incoming requests (e.g., `auth_handler.go` for standard auth, `oauth_handler.go` for OAuth flows).
  - **DTOs**: Data Transfer Objects used to define the API request/response schema.
  - **Middleware**: Application-specific HTTP logic (e.g., `auth_middleware.go`).
  - **Responses**: Helpers for consistent JSON response formatting.

### [repository/](file:///home/jherrmann/go/src/calcard/server/internal/adapter/repository)

- **Purpose**: Implements persistence logic for domain entities.
- **Key Components**:
  - **GORM Implementations**: Concrete repository classes (e.g., `gorm_user_repo.go`, `oauth_connection_repo.go`) that implement the `domain/...Repository` interfaces using GORM.
  - **Unit of Work**: Handles database transactions and complex queries.

### [auth/](file:///home/jherrmann/go/src/calcard/server/internal/adapter/auth)

- **Purpose**: Implements authentication-related services.
- **Key Components**:
  - **JWT**: Concrete implementation of token generation and validation (`jwt.go`).
  - **OAuth**: Implementation of OIDC/OAuth2 providers using `go-oidc` and `golang.org/x/oauth2` (`oauth.go`).

## Design Philosophy

Adapters are where the specific technical choices (like using JSON for API or GORM for DB) are confined. By isolating these choices here, the core business logic remains portable and easy to test. This layer depends on `domain` and `usecase`.
