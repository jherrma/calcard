# Auth Use Cases

This directory contains the business logic for user authentication and authorization, including standard email/password flows, OAuth2/OIDC, and SAML 2.0 integration.

## Core Components

### Standard Authentication

- **Login** (`login.go`): Authenticates users via email and password. Generates access/refresh JWT tokens via `TokenProvider`.
- **Register** (`register.go`): Handles new user creation, password hashing, and triggering verification emails. When SMTP is not configured, users are auto-activated.
- **Verify** (`verify.go`): Verifies email addresses via token.
- **Refresh** (`refresh.go`): Exchanges a valid refresh token for a new access token.
- **Logout** (`logout.go`): Revokes refresh tokens to end a session.

### Password Management

- **Change Password** (`change_password.go`): Authenticated password change (requires current password).
- **Forgot Password** (`forgot_password.go`): Initiates password reset by sending a reset email.
- **Reset Password** (`reset_password.go`): Completes the reset flow using a token from the reset email.

### OAuth2/OIDC Authentication

The OAuth implementation follows the standard authorization code flow.

- **InitiateOAuthUseCase** (`oauth_initiate.go`):
  - Generates the authorization URL for a specific provider (Google, Microsoft, custom OIDC).
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
  - Enforces a safety check: prevents unlinking if it's the user's only authentication method (i.e., no password set and no other linked providers).

- **ListLinkedProvidersUseCase** (`oauth_providers.go`):
  - Returns a list of OAuth providers linked to the current user's account.
  - Indicates whether the user has a password set, helping the frontend decide if "Unlink" should be enabled.

### SAML 2.0 Authentication

- **SAMLLoginUseCase** (`saml_login.go`): Processes SAML assertions after IdP redirect. Creates or links user accounts based on SAML attributes.
- **SAMLMetadataUseCase** (`saml_metadata.go`): Generates SP metadata XML for IdP configuration.

### Utilities

- **Email Service** (`email_service.go`): Interface for sending auth-related emails (verification, password reset).
- **Username Util** (`username_util.go`): Generates unique usernames from email addresses or OAuth profile data.

## Dependencies

These use cases rely on:

- `domain/user.UserRepository`: For user persistence.
- `domain/user.OAuthConnectionRepository`: For managing OAuth links.
- `domain/user.TokenProvider`: For generating JWTs.
- `adapter/auth.OAuthProviderManager`: For abstraction over OIDC providers.

## Testing Notes

When adding methods to `UserRepository` interface, update mock implementations in: `register_test.go`, `profile_test.go`, `calendar_share_test.go` (the auth package shares one mock across tests).
