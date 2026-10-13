# Auth Use Cases

This directory contains the business logic for user authentication and authorization, including standard email/password flows and OAuth2/OIDC integration.

## Core Components

### Standard Authentication

- **Login** (`login.go`): Authenticates users via email and password. Generates access/refresh JWT tokens via `TokenProvider`. An unconfirmed address does not block login; only `IsActive=false` (a deactivated account) does, as in refresh, OAuth, the REST/MCP auth middleware and DAV auth.
- **Register** (`register.go`): Handles new user creation, password hashing, and triggering verification emails. Every new account is active (can log in) at once; with SMTP `EmailVerified` stays false until the mailed link is opened, without SMTP it is true at once. An address that is already taken returns `ErrUserAlreadyExists`, which `AuthHandler.Register` answers with a 409 in both modes. With SMTP configured it first mails the owner — a "someone tried to register" notice, or a fresh activation link if the address was never confirmed (the password is never touched).
- **Verify** (`verify.go`): Sets `EmailVerified` — and nothing else: a deactivated account stays deactivated — via token (`GET /api/v1/auth/verify?token=`, the only verify route). The activation mail does not link that endpoint: it links the SPA page `<cfg.BaseURL>/auth/verify?token=` (`activationLink` in `register.go`), which calls it and shows the outcome — the same pattern as the reset mail's `/auth/reset-password?token=`. Mails sent before still link the API endpoint, which keeps working.
- **Refresh** (`refresh.go`): Exchanges a valid refresh token for a new access token AND rotates the refresh token (OWASP refresh-token rotation, #75). Every login opens a token "family" (`RefreshToken.FamilyID`); each refresh revokes the presented token (setting `ReplacedByHash` to its successor) and mints a fresh one via `RefreshTokenRepository.Rotate` (atomic). Presenting an already-revoked token is reuse detection: within a short grace window (`rotationGrace`, 15s) it's treated as a benign multi-tab race and simply rejected; outside the window (or a replayed stolen token) it revokes the entire family via `RevokeFamily`. Legacy pre-migration tokens carry an empty `FamilyID` and are only ever rejected as a single token — `RevokeFamily` refuses an empty family id so unrelated users are never logged out. Detected reuse is recorded as a `refresh_token_reuse` security event via `SecurityLogger` (Warn level), including cases where the `RevokeFamily` containment call itself fails.
- **Logout** (`logout.go`): Revokes the presented refresh token and invalidates **all** of the account's access tokens (`InvalidateAccessTokens`, P02). The account is identified by a live refresh token in the body or, failing that, a valid bearer token — a rotated/revoked refresh token identifies nobody. Account-wide on purpose: a per-token JWT denylist would need new state, and the user's other sessions lose nothing but one transparent `/auth/refresh` (their refresh tokens are untouched).

### Password Management

- **Change Password** (`change_password.go`): Authenticated password change (requires current password). Revokes all refresh tokens and all access tokens, THEN mints the replacement access token it returns — that order is what keeps the calling session alive. `UserHandler.ChangePassword` answers a wrong current password (`ErrIncorrectPassword`, checked first) with 401, but a new password equal to the current one (`ErrSamePassword`) or one failing the policy with 400: those are body validation errors, and a 401 would make the SPA refresh its session and replay the request (F10).
- **Forgot Password** (`forgot_password.go`): Initiates password reset by sending a reset email.
- **Reset Password** (`reset_password.go`): Completes the reset flow using a token from the reset email. Sets the new password and `EmailVerified=true` (the reset proved control of the mailbox) and, in one transaction (`UserRepository.CompletePasswordReset`), revokes all access tokens and deletes every refresh token, reset link, app password, CalDAV/CardDAV credential, MCP token and OAuth connection of the account — the credential set of `repository.PurgeAccountData`. An account is usable before confirmation, so whoever registered the address may not be its owner; the owner's reset must leave them nothing.

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
  - **Login Flow**: Logs in the user if the provider account is already linked, or auto-links it to the existing account with the same e-mail — only when the provider asserts the address is verified (`ErrEmailNotVerified` otherwise) AND that local account has confirmed it (`EmailVerified`; `ErrLocalEmailNotVerified` otherwise, because an account is usable before confirmation and may have been registered by someone else). Refused cases never fall through to provisioning a second account for the address.
  - **Registration Flow**: Creates a new user account if no matching user is found.
  - **Linking Flow**: Links the provider to an existing authenticated user account. Handles errors if the account is already linked to another user.
  - **Token Updates**: Updates stored access/refresh tokens if the user is already linked.

- **UnlinkProviderUseCase** (`oauth_link.go`):
  - Removes the link between a user account and an OAuth provider.
  - Enforces a safety check: prevents unlinking if it's the user's only authentication method (i.e., no password set and no other linked providers).

- **ListLinkedProvidersUseCase** (`oauth_providers.go`):
  - Returns a list of OAuth providers linked to the current user's account.
  - Indicates whether the user has a password set, helping the frontend decide if "Unlink" should be enabled.

### Utilities

- **Email Service** (`email_service.go`): Interface for sending auth-related emails (verification, password reset).
- **Setup** (`setup.go`): First-run setup. Creates the first account as admin (active + verified, SMTP or not) while `UserRepository.Count` — which includes soft-deleted rows — is 0; refuses with `ErrSetupCompleted` (409) afterwards. The check-then-insert is serialised in-process only. When `Registration.SetupToken` is set (`CALDAV_SETUP_TOKEN`) the request must carry it (constant-time compare, 403 otherwise) and `RegisterUseCase` / OAuth sign-up refuse with `ErrSetupRequired` until the first account exists; empty keeps the old open behaviour.
- **Username Util** (`username_util.go`): `GenerateUniqueUsername`, used by registration and OAuth sign-up: 16 random lowercase letters `a`–`z`, retried while `GetByUsername` (case-insensitive) finds a match. Lowercase like the set-up admin's name (`setup.go` lower-cases and validates it) and every new DAV credential's; accounts registered before keep their mixed-case names, which are their `/dav/{username}/` segment and never rewritten.

## Dependencies

These use cases rely on:

- `domain/user.UserRepository`: For user persistence.
- `domain/user.OAuthConnectionRepository`: For managing OAuth links.
- `domain/user.TokenProvider`: For generating JWTs.
- `adapter/auth.OAuthProviderManager`: For abstraction over OIDC providers.

## Testing Notes

When adding methods to `UserRepository` interface, update mock implementations in: `register_test.go`, `profile_test.go`, `calendar_share_test.go` (the auth package shares one mock across tests).
