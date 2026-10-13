# Server Configuration Guide

This document describes all configuration parameters for the CalCard CalDAV/CardDAV server.

The server can be configured using a YAML file and/or environment variables. Environment variables take precedence over YAML values.

## Configuration Methods

### YAML File

By default, the server looks for a configuration file specified by the `--config` flag or the `CALDAV_CONFIG_PATH` environment variable (`CALDAV_CONFIG_FILE` is accepted as an alias and only consulted when neither of the other two is set). Without any of them the server runs on environment variables and defaults alone; a file that was named but cannot be read is a startup error. A template is provided at [server/configs/config.yaml.example](server/configs/config.yaml.example).

### Environment Variables

All configuration options can be set via environment variables. The mapping is described in the sections below.

---

## Configuration Parameters

### General Settings

| YAML Key    | Env Var            | Default  | Description                                                                                    |
| :---------- | :----------------- | :------- | :--------------------------------------------------------------------------------------------- |
| `data_dir`  | `CALDAV_DATA_DIR`  | `./data` | Directory where SQLite database and other data files are stored.                               |
| `log_level` | `CALDAV_LOG_LEVEL` | `info`   | Logging intensity (`debug`, `info`, `warn`, `error`).                                          |
| `log_payloads` | `CALDAV_LOG_PAYLOADS` | `false` | **Debugging only.** Logs every request (method, URL, headers, status) and the full body of requests under `/dav` and `/.well-known` (capped at 64 KiB) to stdout. `Authorization`/`Cookie` headers are redacted and non-DAV bodies (login etc.) are never logged. Writes user data to the log — leave off in production. |
| `base_url`  | `CALDAV_BASE_URL`  | empty    | The public URL clients reach the server at (e.g. `https://caldav.example.com` or `http://<nas-ip>:8751`), an absolute `http(s)` URL. **Required when SMTP or an OAuth provider is configured** — e-mailed verification/reset links and OAuth redirect URIs are built from it and never from a request (a forged `Host` header would otherwise let an attacker point someone's reset link at their own server). Optional otherwise: when empty, the URLs the API returns to the caller (the DAV server URL shown with a new app password, a calendar's `public_url`) are derived from the request — `https` only if this server terminated TLS, host from the `Host` header. |
| `trust_forwarded_headers` | `CALDAV_TRUST_FORWARDED_HEADERS` | `false` | Only used while `base_url` is empty: derive the scheme and host from `X-Forwarded-Proto` / `X-Forwarded-Host`. Enable it **only** behind a reverse proxy that overwrites those headers; otherwise any client could set them. It does not change the client IP used for rate limiting. |

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
| `auto_migrate` | `CALDAV_DB_AUTO_MIGRATE` | `true`    | Automatically run database migrations on startup (schema, then data migrations — see below). |

The data migrations that run with `auto_migrate` (and with `./server migrate`) include two
that **permanently delete** data nobody can reach any more: what remains of accounts, and
of calendars and address books, deleted with older versions. **Back up the database before
upgrading** — see the README, "Upgrading — back up first".

Search (`/api/v1/search`, `/api/v1/contacts/search`, the MCP search tools, CardDAV
`text-match`) ignores case on both drivers, including non-ASCII letters (`ärztin` finds
"Ärztin"). On PostgreSQL the folding of non-ASCII letters follows the database's
`LC_CTYPE`: the official image's default `en_US.utf8` folds them, a database created
with the `C` / `POSIX` locale folds ASCII only.

### JWT Section (`jwt:`)

| YAML Key         | Env Var                        | Default | Description                                                      |
| :--------------- | :----------------------------- | :------ | :--------------------------------------------------------------- |
| `secret`         | `CALDAV_JWT_SECRET`            | -       | Secret key used to sign JWT tokens. Optional: when unset, the server generates one on first start and stores it in the database, so it survives restarts. Set it only to pin a specific secret (e.g. several replicas sharing one database); a value that is set must be at least 16 characters and not `change-me-in-production`. |
| `access_expiry`  | `CALDAV_JWT_ACCESS_EXPIRY`     | `10m`   | Expiration time for access tokens.                               |
| `refresh_expiry` | `CALDAV_JWT_REFRESH_EXPIRY`    | `168h`  | Expiration time for refresh tokens (7 days).                     |
| `reset_expiry`   | `CALDAV_PASSWORD_RESET_EXPIRY` | `1h`    | Expiration time for password reset tokens.                       |

### SMTP Section (`smtp:`)

Outgoing e-mail: account activation links, password-reset links and the "someone
tried to register with your address" notice. SMTP counts as configured as soon as
`host` is set; the other keys are ignored while it is empty.

| YAML Key   | Env Var                | Default | Description |
| :--------- | :--------------------- | :------ | :---------- |
| `host`     | `CALDAV_SMTP_HOST`     | empty   | Mail server host name. **Empty turns e-mail off**: nothing is sent (the server only logs that a mail was suppressed, never its recipient or link) and new accounts count as verified at once (see Registration below). Setting it requires `base_url`, because the e-mailed links are built from it. |
| `port`     | `CALDAV_SMTP_PORT`     | `587`   | Mail server port. The connection starts unencrypted and is upgraded with STARTTLS (see below), so use the submission port; implicit-TLS ports (SMTPS, usually `465`) do not work. |
| `user`     | `CALDAV_SMTP_USER`     | empty   | User name for SMTP `AUTH PLAIN`. Leave empty for a relay that takes mail without a login; then no AUTH is attempted. Set it together with `password`. |
| `password` | `CALDAV_SMTP_PASSWORD` | empty   | Password for SMTP `AUTH PLAIN`. |
| `from`     | `CALDAV_SMTP_FROM`     | empty   | Sender address, used both as the envelope sender (`MAIL FROM`) and in the `From:` header. Not validated and has no default, so set it whenever `host` is set. |

**TLS and authentication behaviour.** The server speaks SMTP itself (it no longer
uses `net/smtp.SendMail`) and has no further TLS options:

- It connects to `host:port` in plain text and switches to TLS with **STARTTLS
  whenever the server offers it**. The certificate is verified against `host`, so
  `host` must be a name the certificate is valid for; there is no way to skip
  verification.
- It **never sends mail in clear text to a remote host.** A server that offers no
  STARTTLS makes the send fail with `refusing to send e-mail without TLS`, because
  the mail carries a live reset or activation link. Only `localhost` and loopback
  addresses (`127.0.0.1`, `::1`) take mail without TLS.
- It authenticates with `AUTH PLAIN` using `user` / `password` **only when `user`
  is set.** With `user` set, a server that does not advertise `AUTH` fails the send
  with `smtp: server doesn't support AUTH`, so credentials are never sent to a
  server that did not ask for them. With `user` empty, no AUTH is attempted, which
  suits a relay that takes mail without a login.
- A `password` without a `user` stops the server at start-up while SMTP is on.

### Security Section (`security:`)

| YAML Key           | Env Var                           | Default    | Description                                                      |
| :----------------- | :-------------------------------- | :--------- | :--------------------------------------------------------------- |
| `enabled`          | `CALDAV_SECURITY_HEADERS_ENABLED` | `true`     | Enable/disable security headers (Helmet) and general protection. |
| `hsts_enabled`     | `CALDAV_HSTS_ENABLED`             | `false`    | Enable HTTP Strict Transport Security (HSTS).                    |
| `hsts_max_age`     | `CALDAV_HSTS_MAX_AGE`             | `31536000` | Max age for HSTS (1 year).                                       |
| `max_request_size` | `CALDAV_MAX_REQUEST_SIZE`         | `10485760` | Max body size in bytes (10MB).                                   |
| `request_timeout`  | `CALDAV_REQUEST_TIMEOUT`          | `30s`      | Individual request timeout.                                      |

### Rate Limit Section (`rate_limit:`)

| YAML Key              | Env Var                                 | Default | Description |
| :-------------------- | :-------------------------------------- | :------ | :---------- |
| `enabled`             | `CALDAV_RATE_LIMIT_ENABLED`             | `true`  | Enable rate limiting (the global limiter and the auth limiters below). |
| `requests`            | `CALDAV_RATE_LIMIT_REQUESTS`            | `5000`  | Requests **per client IP** allowed in the window, across the REST API, DAV, MCP and public feeds (web UI static assets are not counted). A value `<= 0` is reset to the default. |
| `window`              | `CALDAV_RATE_LIMIT_WINDOW`              | `1m`    | Time window for rate limiting. |
| `auth_ip_requests`    | `CALDAV_RATE_LIMIT_AUTH_IP_REQUESTS`    | `20`    | Login / forgot-password / reset-password attempts per IP in the window. Not used for DAV Basic auth (see below). Must stay above `auth_email_requests`. |
| `auth_email_requests` | `CALDAV_RATE_LIMIT_AUTH_EMAIL_REQUESTS` | `10`    | Login / forgot-password (and, with SMTP, register) attempts per e-mail address in the window, and **failed** DAV Basic-auth attempts per user name or e-mail address. Addresses are compared trimmed and case-insensitively, as the account lookup does, so `Foo@x`, `foo@x` and ` foo@x ` share one allowance. |

The global limit is keyed on the client IP as the server sees it, and forwarded
headers are not trusted, so **everyone behind one NAT or reverse proxy shares a
single allowance**. That is why the default is generous: it guards against a
flood, not against password guessing — the much tighter auth limits do that. A
throttled request gets `429` with a `Retry-After` header: the seconds until its window resets (also in the JSON body as `retry_after`).

DAV Basic auth (`/dav`) accepts the account password, so it is covered by the
per-account auth limit too (`auth_email_requests`, per user name or e-mail
address), but only **failed** attempts count, and only once they have failed: a
check that is still running counts nothing, and a correct password never
counts. A DAV client authenticates on every request, often over several
connections at once, and a correctly configured one never uses the allowance
up. A request without credentials (the 401 challenge clients start with) does
not count either. The window starts with the first failure. Once a user name or
e-mail address has used up its failed attempts, every Basic-auth request for it
gets `429` (with `Retry-After`) until the window ends, a correct password
included, because answering a correct password differently would confirm the
guess.

Separately, at most twice `auth_email_requests` password checks (20 by default)
may be running for one user name or e-mail address at a time; a request beyond
that gets `429` with `Retry-After: 1`, whatever its password. This bounds the
bcrypt work a flood can start. Because failures are counted when they finish, a
burst of parallel wrong passwords can overshoot the allowance by what was still
running: at most `3 × auth_email_requests − 1` (29 by default) wrong passwords
per window. So whoever knows an account's
user name or e-mail address can hold up the DAV clients that sign in with it
for the rest of the window; a CalDAV/CardDAV credential has a user name of its
own and is not affected.

There is **no per-IP limit on `/dav`**: behind a reverse proxy every client has
the proxy's address, so a per-IP limit would let one outsider's wrong passwords
throttle every DAV client of the server. Guessing across many accounts from one
address is therefore bounded only by each account's allowance and the global
`requests` limit.

### Registration Section (`registration:`)

| YAML Key      | Env Var                       | Default | Description |
| :------------ | :---------------------------- | :------ | :---------- |
| `disabled`    | `CALDAV_REGISTRATION_DISABLED` | `false` | Turns off self-service registration (local and OAuth sign-up). First-run setup still works. |
| `setup_token` | `CALDAV_SETUP_TOKEN`          | empty   | Guards **first-run setup**. When set, `POST /api/v1/auth/setup` (the "Initial Setup" page) must present this token, and until the first account exists self-service registration is refused. This stops whoever reaches a fresh, internet-exposed server first from claiming the admin account, or from registering as a plain user and so closing setup for good. Must be at least 16 characters (the server refuses to start otherwise); surrounding whitespace is trimmed. **Empty keeps the previous behaviour**: no token is asked for and registration is open. Once the first account exists the token is no longer needed. |

**A new account can log in and use the app at once, SMTP or not.** With SMTP
configured (`smtp.host` / `CALDAV_SMTP_HOST` set) its address is not confirmed
yet: it is created with `email_verified: false` and an activation link is
mailed; opening the link sets `email_verified: true`. Without SMTP the account
is created verified. Until its address is confirmed, an OAuth sign-in whose
provider reports that address is not linked to the account, because anyone can
register someone else's address (signing in with the password and linking the
provider from the account settings still works). Login and sharing wait for
nothing: a share addressed to the account by e-mail reaches it either way.
Completing a password reset also confirms the address, and it signs out every
session and deletes every app password, CalDAV/CardDAV credential, MCP token
and OAuth link of the account, so whoever registered the address first keeps no
way back in.

An address that already has an account answers `409 Email already registered`
in both modes, so registration reveals whether an address has an account (the
new account would be usable at once, so the outcome could not be hidden). With
SMTP the owner also gets a "someone tried to register with your address" e-mail
(the password is never changed). It never carries an activation link, not even
for an address that was never confirmed: that account has the password of
whoever registered first, so the e-mail sends the owner to the password reset,
which confirms the address and removes everyone else's access. `/register`
carries the same per-IP and per-email
limiters as forgot-password (`rate_limit.enabled` must be on). If enumeration
matters on an internet-facing server, set `CALDAV_REGISTRATION_DISABLED=true`.

Deleting an account (`DELETE /api/v1/users/me`) frees its e-mail address: it can
register again (or sign in again through OAuth) as a brand-new account. The deleted
row is kept with its e-mail and username replaced by `deleted:<uuid>`, so it still
counts as "an account has existed" and first-run setup stays closed.

### MCP Section (`mcp:`)

Controls the Model Context Protocol endpoint at `/mcp` (story 104), which lets an
AI assistant read and manage a user's calendars and contacts. Access always
requires an MCP access token that the user mints themselves under
**Settings → MCP Access**, so the endpoint exposes nothing on its own — but
disabling it unregisters the routes entirely for operators who do not want an AI
tool surface on their server at all.

| YAML Key   | Env Var                            | Default | Description                                                                                     |
| :--------- | :--------------------------------- | :------ | :---------------------------------------------------------------------------------------------- |
| `enabled`  | `CALDAV_MCP_ENABLED`               | `true`  | Register the `/mcp` routes. `false` removes them; existing tokens then authenticate nothing.     |
| `requests` | `CALDAV_MCP_RATE_LIMIT_REQUESTS`   | `120`   | Per-**user** requests allowed per `rate_limit.window`. Only applied when `rate_limit.enabled`.   |

The MCP limit is keyed on the authenticated user rather than the IP: an
assistant calls from one address on behalf of one account, so an IP-keyed limit
would either throttle a single legitimate conversation or be far too loose on a
shared host. It is separate from `rate_limit.requests` because one MCP call can
be a whole conversation turn's worth of work.

### Subscriptions Section (`subscriptions:`)

Controls remote calendar subscriptions (story 100) — the background worker that
mirrors third-party iCalendar feeds into read-only calendars, and the
`/api/v1/calendar-subscriptions` endpoints behind **Settings → Subscriptions**.

This is the only feature that makes the server issue outbound HTTP requests to
URLs its users choose, so read the two `allow_*` keys before changing them.

| YAML Key              | Env Var                                          | Default   | Description                                                                                                     |
| :-------------------- | :----------------------------------------------- | :-------- | :-------------------------------------------------------------------------------------------------------------- |
| `enabled`             | `CALDAV_SUBSCRIPTIONS_ENABLED`                   | `true`    | Register the routes and start the worker. `false` removes both — no outbound fetches at all.                     |
| `worker_interval`     | `CALDAV_SUBSCRIPTIONS_WORKER_INTERVAL`           | `1m`      | How often the worker looks for due feeds. **Not** a refresh interval — each subscription carries its own. `0` disables the worker while leaving manual refresh working. |
| `max_failures`        | `CALDAV_SUBSCRIPTIONS_MAX_FAILURES`              | `5`       | Consecutive failures after which a subscription's auto-sync is switched off. `0` never disables.                 |
| `max_feed_size`       | `CALDAV_SUBSCRIPTIONS_MAX_FEED_SIZE`             | `5242880` | Byte cap on a feed body. An oversized feed fails rather than being truncated.                                    |
| `fetch_timeout`       | `CALDAV_SUBSCRIPTIONS_FETCH_TIMEOUT`             | `30s`     | Bounds one fetch end to end, redirects included.                                                                 |
| `max_per_user`        | `CALDAV_SUBSCRIPTIONS_MAX_PER_USER`              | `20`      | Subscriptions one account may hold. Each is a recurring outbound request made on that user's behalf.             |
| `allow_insecure_urls` | `CALDAV_SUBSCRIPTIONS_ALLOW_INSECURE_URLS`       | `false`   | Permit plain `http://` feeds. See below.                                                                         |
| `allow_private_hosts` | `CALDAV_SUBSCRIPTIONS_ALLOW_PRIVATE_HOSTS`       | `false`   | Permit feeds on loopback, link-local and private address space. See below.                                       |

A user may pick any of `15m`, `30m`, `1h` (default), `6h`, `12h` or `24h` as a
subscription's own refresh interval. That set is closed and not configurable: a
plain minimum lets a caller choose `15m1s` to look compliant while polling
continuously.

**`allow_private_hosts` is an SSRF control.** A subscription URL is input the
user chooses and the *server* fetches, so without a guard "subscribe to a
calendar" becomes a way to read any URL the server can reach — an internal admin
panel, a cloud instance-metadata endpoint — and pipe the response back through
the user's own calendar. The check runs at connect time on the resolved IP, so
DNS rebinding and redirects are covered too. Turn it on only for a deployment
whose users are meant to subscribe to feeds on the same private network.

**`allow_insecure_urls`** exists for the same kind of deployment. A feed fetched
over plain http is trivially modifiable in transit, and the events it injects
appear in the user's calendar as if the server vouched for them. `webcal://`
links are rewritten to `https://` and are unaffected by this setting.

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
- `assume_email_verified` (`ASSUME_EMAIL_VERIFIED`) - Default `false`. Treat every e-mail address this provider returns as verified. Without a verified address the server refuses to create an account from an OAuth login or to link it onto an existing account with the same address; some providers (e.g. Microsoft's userinfo endpoint) never send the `email_verified` claim, so their logins are refused unless this is on. Enable it only for a provider you trust to return addresses its users actually control.

A provider is set up once both its client id and client secret are set; that also makes `base_url` required.

---

## Important Security Requirements

> [!IMPORTANT]
> **JWT Secret Length**
> `CALDAV_JWT_SECRET` may be left unset: the server then generates a secret on first start and keeps it in the database. If you set it, use a cryptographically secure random string; a value shorter than **16 characters** or equal to "change-me-in-production" makes the server refuse to start.

> [!CAUTION]
> **Production Deployment**
> In production environments, always enable `TLS` or run the server behind a secure reverse proxy that provides TLS termination and HSTS.
