# CalDAV/CardDAV Server Backend

This is the backend for the CalCard project, a modern, self-hostable CalDAV/CardDAV server written in Go.

## Setup

1.  Ensure you have Go 1.22+ installed.
2.  Navigate to this directory: `cd server`
3.  Install dependencies: `go mod tidy`
4.  Run the server: `go run ./cmd/server`

## Configuration

The server can be configured using a `config.yaml` file in the current directory or environment variables.

### OAuth2/OpenID Connect Setup

The server supports logging in via OAuth2/OIDC providers (Google, Microsoft, etc.).

#### 1. Configure Providers in `config.yaml`

Create a `config.yaml` file in the `server` directory if it doesn't exist. Add your provider credentials:

```yaml
oauth:
  google:
    client_id: "YOUR_GOOGLE_CLIENT_ID"
    client_secret: "YOUR_GOOGLE_CLIENT_SECRET"
    issuer: "https://accounts.google.com"
  microsoft:
    client_id: "YOUR_MICROSOFT_CLIENT_ID"
    client_secret: "YOUR_MICROSOFT_CLIENT_SECRET"
    issuer: "https://login.microsoftonline.com/common/v2.0"
```

#### 2. Register Applications

You must register an application with each provider to obtain the Client ID and Secret.

**Redirect URLs:**
The server listens on `http://localhost:8080` by default. You must whitelist the callback URL for each provider:

- **Google**: `http://localhost:8080/api/v1/auth/oauth/google/callback`
- **Microsoft**: `http://localhost:8080/api/v1/auth/oauth/microsoft/callback`

_(Replace `http://localhost:8080` with your actual public base URL if deployed)_

**Google Setup:**

1.  Go to the [Google Cloud Console](https://console.cloud.google.com/).
2.  Create a new project or select an existing one.
3.  Navigate to **APIs & Services > Credentials**.
4.  Create **OAuth client ID** credentials.
5.  Select **Web application**.
6.  Add the Redirect URI mentioned above.
7.  Copy the Client ID and Client Secret to your `config.yaml`.

**Microsoft Setup:**

1.  Go to the [Azure Portal](https://portal.azure.com/).
2.  Search for **App registrations** and create a new registration.
3.  Set the Redirect URI (Web) to the URL mentioned above.
4.  Once created, note the **Application (client) ID**.
5.  Under **Certificates & secrets**, create a new client secret and copy the value.
6.  Update your `config.yaml`.

**Custom OIDC Provider Setup:**

You can configure a generic OpenID Connect provider (e.g., Keycloak, Auth0, Zitadel).

1.  Add the `custom` section to your `config.yaml`:

```yaml
oauth:
  custom:
    client_id: "YOUR_CUSTOM_CLIENT_ID"
    client_secret: "YOUR_CUSTOM_CLIENT_SECRET"
    issuer: "https://your-oidc-provider.com/realms/your-realm"
```

2.  Ensure your provider supports OpenID Connect discovery (`/.well-known/openid-configuration`).
3.  Whitelist the callback URL: `http://localhost:8080/api/v1/auth/oauth/custom/callback`.

## Architecture

The project follows Clean Architecture principles:

- `internal/domain`: Core entities and business rules.
- `internal/usecase`: Application-specific business logic.
- `internal/adapter`: Interface adapters (HTTP handlers, database repositories).
- `internal/infrastructure`: External tools and frameworks (database setup, configuration).
