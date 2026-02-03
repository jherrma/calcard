# Server Configuration Guide

This document describes all configuration parameters for the CalCard CalDAV/CardDAV server.

The server can be configured using a YAML file and/or environment variables. Environment variables take precedence over YAML values.

## Configuration Methods

### YAML File

By default, the server looks for a configuration file specified by the `--config` flag or the `CALDAV_CONFIG_PATH` environment variable. A template is provided at [server/configs/config.yaml.example](file:///home/jherrmann/go/src/calcard/server/configs/config.yaml.example).

### Environment Variables

All configuration options can be set via environment variables. The mapping is described in the sections below.

---

## Configuration Parameters

### General Settings

| YAML Key    | Env Var            | Default  | Description                                                                                    |
| :---------- | :----------------- | :------- | :--------------------------------------------------------------------------------------------- |
| `data_dir`  | `CALDAV_DATA_DIR`  | `./data` | Directory where SQLite database and other data files are stored.                               |
| `log_level` | `CALDAV_LOG_LEVEL` | `info`   | Logging intensity (`debug`, `info`, `warn`, `error`).                                          |
| `base_url`  | `CALDAV_BASE_URL`  | -        | The base URL where the server is reachable (e.g., `https://caldav.example.com`). **Required.** |

### Server Section (`server:`)

| YAML Key | Env Var              | Default   | Description                         |
| :------- | :------------------- | :-------- | :---------------------------------- |
| `host`   | `CALDAV_SERVER_HOST` | `0.0.0.0` | IP address/URL the server binds to. |
| `port`   | `CALDAV_SERVER_PORT` | `8080`    | Port the server listens on.         |

### Database Section (`database:`)

| YAML Key       | Env Var                  | Default   | Description                                         |
| :------------- | :----------------------- | :-------- | :-------------------------------------------------- |
| `driver`       | `CALDAV_DB_DRIVER`       | `sqlite`  | Database driver (`sqlite` or `postgres`).           |
| `host`         | `CALDAV_DB_HOST`         | -         | Database host (Postgres only).                      |
| `port`         | `CALDAV_DB_PORT`         | `5432`    | Database port (Postgres only).                      |
| `user`         | `CALDAV_DB_USER`         | -         | Database user (Postgres only).                      |
| `password`     | `CALDAV_DB_PASSWORD`     | -         | Database password (Postgres only).                  |
| `name`         | `CALDAV_DB_NAME`         | `caldav`  | Database name (Postgres only).                      |
| `ssl_mode`     | `CALDAV_DB_SSLMODE`      | `disable` | SSL mode for Postgres (e.g., `require`, `disable`). |
| `auto_migrate` | `CALDAV_DB_AUTO_MIGRATE` | `true`    | Automatically run database migrations on startup.   |

### JWT Section (`jwt:`)

| YAML Key         | Env Var                        | Default | Description                                                      |
| :--------------- | :----------------------------- | :------ | :--------------------------------------------------------------- |
| `secret`         | `CALDAV_JWT_SECRET`            | -       | Secret key used to sign JWT tokens. **Required (min 32 chars).** |
| `access_expiry`  | `CALDAV_JWT_ACCESS_EXPIRY`     | `10m`   | Expiration time for access tokens.                               |
| `refresh_expiry` | `CALDAV_JWT_REFRESH_EXPIRY`    | `168h`  | Expiration time for refresh tokens (7 days).                     |
| `reset_expiry`   | `CALDAV_PASSWORD_RESET_EXPIRY` | `1h`    | Expiration time for password reset tokens.                       |

### Security Section (`security:`)

| YAML Key           | Env Var                           | Default    | Description                                                      |
| :----------------- | :-------------------------------- | :--------- | :--------------------------------------------------------------- |
| `enabled`          | `CALDAV_SECURITY_HEADERS_ENABLED` | `true`     | Enable/disable security headers (Helmet) and general protection. |
| `hsts_enabled`     | `CALDAV_HSTS_ENABLED`             | `false`    | Enable HTTP Strict Transport Security (HSTS).                    |
| `hsts_max_age`     | `CALDAV_HSTS_MAX_AGE`             | `31536000` | Max age for HSTS (1 year).                                       |
| `max_request_size` | `CALDAV_MAX_REQUEST_SIZE`         | `10485760` | Max body size in bytes (10MB).                                   |
| `request_timeout`  | `CALDAV_REQUEST_TIMEOUT`          | `30s`      | Individual request timeout.                                      |

### Rate Limit Section (`rate_limit:`)

| YAML Key   | Env Var                      | Default | Description                               |
| :--------- | :--------------------------- | :------ | :---------------------------------------- |
| `enabled`  | `CALDAV_RATE_LIMIT_ENABLED`  | `true`  | Enable global rate limiting.              |
| `requests` | `CALDAV_RATE_LIMIT_REQUESTS` | `100`   | Number of requests allowed in the window. |
| `window`   | `CALDAV_RATE_LIMIT_WINDOW`   | `1m`    | Time window for rate limiting.            |

### TLS Section (`tls:`)

| YAML Key    | Env Var                | Default | Description                       |
| :---------- | :--------------------- | :------ | :-------------------------------- |
| `enabled`   | `CALDAV_TLS_ENABLED`   | `false` | Enable HTTPS support.             |
| `cert_file` | `CALDAV_TLS_CERT_FILE` | -       | Path to the SSL certificate file. |
| `key_file`  | `CALDAV_TLS_KEY_FILE`  | -       | Path to the SSL private key file. |

### CORS Section (`cors:`)

| YAML Key            | Env Var                         | Default               | Description                                    |
| :------------------ | :------------------------------ | :-------------------- | :--------------------------------------------- |
| `enabled`           | `CALDAV_CORS_ENABLED`           | `false`               | Enable Cross-Origin Resource Sharing.          |
| `allowed_origins`   | `CALDAV_CORS_ALLOWED_ORIGINS`   | `*`                   | List of allowed origins (comma-separated env). |
| `expose_headers`    | `CALDAV_CORS_EXPOSE_HEADERS`    | `ETag,DAV,Allow,Link` | Headers exposed to the browser.                |
| `allow_credentials` | `CALDAV_CORS_ALLOW_CREDENTIALS` | `true`                | Allow credentials (cookies, auth).             |
| `max_age`           | `CALDAV_CORS_MAX_AGE`           | `86400`               | Preflight cache lifetime (24h).                |

### OAuth Section (`oauth:`)

The server supports `google`, `microsoft`, and `custom` OIDC providers.

| YAML Path           | Env Var Prefix            | Description                        |
| :------------------ | :------------------------ | :--------------------------------- |
| `oauth.google.*`    | `CALDAV_OAUTH_GOOGLE_`    | Settings for Google login.         |
| `oauth.microsoft.*` | `CALDAV_OAUTH_MICROSOFT_` | Settings for Microsoft login.      |
| `oauth.custom.*`    | `CALDAV_OAUTH_CUSTOM_`    | Settings for custom OIDC provider. |

Each provider accepts:

- `client_id` (`CLIENT_ID`)
- `client_secret` (`CLIENT_SECRET`)
- `issuer` (`ISSUER`) - Required for `custom` providers.

---

## Important Security Requirements

> [!IMPORTANT]
> **JWT Secret Length**
> The `CALDAV_JWT_SECRET` must be at least **32 characters long** and should be a cryptographically secure random string. If it is shorter or set to the default "change-me-in-production", the server will fail to start for security reasons.

> [!CAUTION]
> **Production Deployment**
> In production environments, always enable `TLS` or run the server behind a secure reverse proxy that provides TLS termination and HSTS.
