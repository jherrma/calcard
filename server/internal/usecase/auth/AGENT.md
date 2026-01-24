# Auth Use Cases

This directory contains the business logic for user authentication and authorization, including standard email/password flows and OAuth2/OIDC integration.

## Core Components

### Standard Authentication

- **Login**: Authenticates users via email and password (`login.go`).
- **Register**: Handles user registration and initial email verification (`register.go`).
- **Verify**: Verifies email addresses via token (`verify.go`).
- **Refresh**: Refreshes JWT access tokens using refresh tokens (`refresh.go`).
- **Logout**: Revokes refresh tokens (`logout.go`).
- **Password Management**: Handles forgot/reset flows and password changes.

### OAuth2/OIDC Authentication

The OAuth implementation follows the standard authorization code flow.

- **InitiateOAuthUseCase** (`oauth_initiate.go`):
  - Generates the authorization URL for a specific provider (Google, Microsoft, etc.).
  - Creates a secure state parameter to prevent CSRF.
  - Returns the URL for the frontend to redirect the user.

- **OAuthCallbackUseCase** (`oauth_callback.go`):
  - Validates the callback code and state from the provider.
  - Exchanges the code for access/refresh tokens.
  - Retrieves user profile information (email, sub, name) from the provider.
  - **Login Flow**: Logs in the user if the provider account is already linked or if the email matches an existing account.
  - **Registration Flow**: Creates a new user account if no matching user is found.
  - **Linking Flow**: Links the provider to an existing authenticated user account. Handles errors if the account is already linked to another user.
  - **Token Updates**: Updates stored access/refresh tokens if the user is already linked.

- **UnlinkProviderUseCase** (`oauth_link.go`):
  - Removes the link between a user account and an OAuth provider.
  - Enforces a safety check: storage key logic prevents unlinking if it's the user's only authentication method (i.e., no password set and no other linked providers).

- **ListLinkedProvidersUseCase** (`oauth_providers.go`):
  - Returns a list of OAuth providers linked to the current user's account.
  - Indicates whether the user has a password set, helping the frontend decide if "Unlink" should be enabled.

## Dependencies

These use cases rely on:

- `domain/user.UserRepository`: For user persistence.
- `domain/user.OAuthConnectionRepository`: For managing OAuth links.
- `domain/user.TokenProvider`: For generating JWTs.
- `adapter/auth.OAuthProviderManager`: For abstraction over OIDC providers.
