# Story 012: SAML Authentication

## Title
Implement SAML 2.0 Single Sign-On

## Description
As an enterprise user, I want to login using my organization's SAML Identity Provider so that I can use single sign-on with my corporate credentials.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AU-2.3.1 | Service Provider (SP) metadata is available at standard endpoint |
| AU-2.3.2 | Users can login via SAML Identity Provider |
| AU-2.3.3 | SAML assertions are properly validated |
| AU-2.3.4 | SAML attributes map to user profile fields |
| AU-2.3.5 | SAML logout (SLO) is supported |

## Acceptance Criteria

### Service Provider Metadata

- [ ] REST endpoint `GET /api/v1/auth/saml/metadata`
- [ ] Returns XML SP metadata document
- [ ] Includes:
  - [ ] Entity ID (configured or derived from base URL)
  - [ ] ACS (Assertion Consumer Service) URL
  - [ ] SLO (Single Logout) URL
  - [ ] NameID format (email preferred)
  - [ ] Signing certificate (if configured)
- [ ] Content-Type: `application/samlmetadata+xml`

### SAML Login Flow

- [ ] REST endpoint `GET /api/v1/auth/saml/login`
- [ ] Generates SAML AuthnRequest
- [ ] Redirects to IdP SSO URL with SAMLRequest parameter
- [ ] Supports HTTP-Redirect binding
- [ ] REST endpoint `POST /api/v1/auth/saml/acs` (Assertion Consumer Service)
- [ ] Receives SAMLResponse from IdP
- [ ] Validates SAML Response:
  - [ ] Signature verification
  - [ ] Issuer matches configured IdP
  - [ ] Destination matches ACS URL
  - [ ] NotBefore/NotOnOrAfter timestamps
  - [ ] Audience restriction
- [ ] Extracts user attributes from assertion:
  - [ ] NameID (required, typically email)
  - [ ] Email attribute
  - [ ] First name / Last name / Display name
- [ ] If user exists with SAML link: Login
- [ ] If no user exists: Create account
- [ ] Issues JWT tokens
- [ ] Redirects to web app with tokens

### SAML Logout (SLO)

- [ ] REST endpoint `GET /api/v1/auth/saml/logout` (requires auth)
- [ ] Generates SAML LogoutRequest
- [ ] Redirects to IdP SLO URL
- [ ] REST endpoint `POST /api/v1/auth/saml/slo`
- [ ] Receives and validates LogoutResponse
- [ ] Invalidates local session
- [ ] REST endpoint `POST /api/v1/auth/saml/slo` (IdP-initiated)
- [ ] Receives LogoutRequest from IdP
- [ ] Invalidates user session
- [ ] Returns LogoutResponse

### Attribute Mapping

- [ ] Configurable attribute mapping:
  ```yaml
  saml:
    attribute_mapping:
      email: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
      first_name: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"
      last_name: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname"
      display_name: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name"
  ```
- [ ] Fallback to common attribute names if not configured

## Technical Notes

### New Configuration
```
CALDAV_SAML_ENABLED               (default: "false")
CALDAV_SAML_ENTITY_ID             (default: derived from base URL)
CALDAV_SAML_IDP_METADATA_URL      (IdP metadata URL for auto-config)
CALDAV_SAML_IDP_SSO_URL           (manual: SSO URL)
CALDAV_SAML_IDP_SLO_URL           (manual: SLO URL)
CALDAV_SAML_IDP_CERT              (manual: IdP signing certificate)
CALDAV_SAML_SP_CERT               (SP signing certificate)
CALDAV_SAML_SP_KEY                (SP private key)
CALDAV_SAML_SIGN_REQUESTS         (default: "true")
CALDAV_SAML_WANT_SIGNED_ASSERTIONS (default: "true")
```

### Database Model
```go
// Reuse OAuthConnection with provider = "saml"
type OAuthConnection struct {
    // ... existing fields ...
    Provider      string    `gorm:"size:50;not null"`  // "saml"
    ProviderID    string    `gorm:"size:255;not null"` // NameID
    // AccessToken/RefreshToken unused for SAML
}
```

### SAML Session Tracking
```go
type SAMLSession struct {
    ID          uint      `gorm:"primaryKey"`
    UserID      uint      `gorm:"index;not null"`
    SessionID   string    `gorm:"uniqueIndex;size:64;not null"` // SAML SessionIndex
    NameID      string    `gorm:"size:255;not null"`
    ExpiresAt   time.Time `gorm:"index;not null"`
    CreatedAt   time.Time
}
```

### Dependencies
```go
github.com/crewjam/saml  // SAML library
```

### Code Structure
```
internal/usecase/auth/
├── saml_login.go          # SAML login flow
├── saml_logout.go         # SAML logout flow
└── saml_metadata.go       # SP metadata generation

internal/adapter/auth/
├── saml.go                # SAML service provider setup
└── saml_middleware.go     # SAML-related middleware

internal/adapter/repository/
└── saml_session_repo.go   # SAML session tracking
```

## API Response Examples

### SP Metadata (200 OK)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata"
                  entityID="https://caldav.example.com/saml">
  <SPSSODescriptor AuthnRequestsSigned="true"
                   WantAssertionsSigned="true"
                   protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</NameIDFormat>
    <AssertionConsumerService
        Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
        Location="https://caldav.example.com/api/v1/auth/saml/acs"
        index="0" />
    <SingleLogoutService
        Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
        Location="https://caldav.example.com/api/v1/auth/saml/slo" />
  </SPSSODescriptor>
</EntityDescriptor>
```

### SAML Login Success (Redirect)
After successful SAML assertion validation, redirects to:
```
https://caldav.example.com/auth/callback?
  access_token=eyJhbGciOiJIUzI1NiIs...&
  refresh_token=dGhpcyBpcyBhIHJlZnJlc2g...&
  expires_in=900
```

### SAML Assertion Invalid (400)
```json
{
  "error": "saml_error",
  "message": "SAML assertion validation failed: signature verification error"
}
```

### SAML Not Configured (501)
```json
{
  "error": "not_implemented",
  "message": "SAML authentication is not configured"
}
```

## IdP Configuration Guide

For common IdPs, users need to configure:

### Okta
1. Create SAML 2.0 App
2. Set Single Sign-On URL: `https://caldav.example.com/api/v1/auth/saml/acs`
3. Set Audience URI: `https://caldav.example.com/saml`
4. Attribute mappings: email, firstName, lastName

### Azure AD
1. Enterprise Application > New > Non-gallery
2. Set up Single Sign-On > SAML
3. Basic SAML Configuration:
   - Identifier: `https://caldav.example.com/saml`
   - Reply URL: `https://caldav.example.com/api/v1/auth/saml/acs`

### OneLogin
1. Add App > SAML Test Connector
2. Configuration:
   - ACS URL: `https://caldav.example.com/api/v1/auth/saml/acs`
   - Entity ID: `https://caldav.example.com/saml`

## Security Considerations

1. **Signature Validation**: Always verify assertion signatures
2. **Replay Prevention**: Track and reject reused assertion IDs
3. **Time Validation**: Strict checking of NotBefore/NotOnOrAfter
4. **Audience Restriction**: Verify assertion is for this SP
5. **HTTPS Only**: SAML endpoints must be HTTPS in production
6. **Clock Skew**: Allow configurable tolerance (default 30 seconds)

## Definition of Done

- [ ] SP metadata endpoint returns valid SAML metadata
- [ ] SAML login flow redirects to IdP and handles response
- [ ] SAML assertions are validated (signature, timestamps, audience)
- [ ] User attributes are extracted and mapped to profile
- [ ] New users are created from SAML assertions
- [ ] Existing SAML users can login
- [ ] SAML SLO terminates local session
- [ ] IdP-initiated logout is handled
- [ ] Unit tests for assertion validation
- [ ] Integration tests with test IdP
