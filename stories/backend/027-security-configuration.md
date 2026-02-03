# Story 027: Security Configuration

## Title
Implement TLS, CORS, and Security Hardening

## Description
As a system administrator, I want to configure TLS encryption, CORS policies, and other security settings so that the server is secure in production.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| OP-8.3.1 | HTTPS is supported via TLS configuration |
| OP-8.3.2 | Passwords are hashed with bcrypt |
| OP-8.3.3 | JWT secrets are configurable |
| OP-8.3.4 | CORS is configurable |
| OP-8.3.5 | Rate limiting is configurable |

## Acceptance Criteria

### TLS Configuration

- [ ] Server can run in HTTPS mode with TLS certificates
- [ ] Configuration:
  ```
  CALDAV_TLS_ENABLED=true
  CALDAV_TLS_CERT_FILE=/path/to/cert.pem
  CALDAV_TLS_KEY_FILE=/path/to/key.pem
  ```
- [ ] Supports TLS 1.2 and 1.3 only (no older versions)
- [ ] Automatic HTTP to HTTPS redirect (optional)
- [ ] HSTS header when TLS enabled
- [ ] Let's Encrypt / ACME support (optional, can use reverse proxy)

### CORS Configuration

- [ ] CORS middleware with configurable origins
- [ ] Configuration:
  ```
  CALDAV_CORS_ENABLED=true
  CALDAV_CORS_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com
  CALDAV_CORS_ALLOWED_METHODS=GET,POST,PUT,PATCH,DELETE,OPTIONS
  CALDAV_CORS_ALLOWED_HEADERS=Authorization,Content-Type,X-Requested-With
  CALDAV_CORS_EXPOSE_HEADERS=X-Total-Count,Link
  CALDAV_CORS_ALLOW_CREDENTIALS=true
  CALDAV_CORS_MAX_AGE=86400
  ```
- [ ] Wildcard `*` allowed for development only
- [ ] Preflight requests handled correctly
- [ ] Credentials mode supported

### Rate Limiting

- [ ] Global rate limiting middleware
- [ ] Configuration:
  ```
  CALDAV_RATE_LIMIT_ENABLED=true
  CALDAV_RATE_LIMIT_REQUESTS=100
  CALDAV_RATE_LIMIT_WINDOW=1m
  ```
- [ ] Per-endpoint rate limits for sensitive operations:
  - [ ] Login: 5 requests/minute/IP
  - [ ] Registration: 3 requests/minute/IP
  - [ ] Password reset: 3 requests/hour/email
- [ ] Rate limit headers in responses:
  - [ ] `X-RateLimit-Limit`
  - [ ] `X-RateLimit-Remaining`
  - [ ] `X-RateLimit-Reset`
- [ ] 429 Too Many Requests with `Retry-After` header

### Security Headers

- [ ] Security headers middleware:
  - [ ] `X-Content-Type-Options: nosniff`
  - [ ] `X-Frame-Options: DENY`
  - [ ] `X-XSS-Protection: 1; mode=block`
  - [ ] `Referrer-Policy: strict-origin-when-cross-origin`
  - [ ] `Content-Security-Policy` (configurable)
  - [ ] `Strict-Transport-Security` (when TLS enabled)

### Request Validation

- [ ] Maximum request body size (default: 10MB, configurable)
- [ ] Request timeout (default: 30s, configurable)
- [ ] Validate Content-Type headers
- [ ] Sanitize user inputs

### Logging & Audit

- [ ] Security events logged:
  - [ ] Failed login attempts
  - [ ] Password changes
  - [ ] App password creation/revocation
  - [ ] Credential creation/revocation
  - [ ] Account deletion
  - [ ] Share creation/revocation
- [ ] Log format includes:
  - [ ] Timestamp
  - [ ] Event type
  - [ ] User ID (if applicable)
  - [ ] IP address
  - [ ] User-Agent
- [ ] Sensitive data NOT logged (passwords, tokens)

## Technical Notes

### TLS Configuration
```go
type TLSConfig struct {
    Enabled  bool   `env:"CALDAV_TLS_ENABLED" envDefault:"false"`
    CertFile string `env:"CALDAV_TLS_CERT_FILE"`
    KeyFile  string `env:"CALDAV_TLS_KEY_FILE"`
    MinVersion uint16 // TLS 1.2 minimum
}

func (s *Server) Start() error {
    if s.config.TLS.Enabled {
        tlsConfig := &tls.Config{
            MinVersion: tls.VersionTLS12,
            CurvePreferences: []tls.CurveID{
                tls.CurveP256,
                tls.X25519,
            },
            CipherSuites: []uint16{
                tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
                tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
                tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
                tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
            },
        }
        return s.app.ListenTLSWithCertificate(
            s.config.Address(),
            s.config.TLS.CertFile,
            s.config.TLS.KeyFile,
            tlsConfig,
        )
    }
    return s.app.Listen(s.config.Address())
}
```

### CORS Middleware
```go
func CORSMiddleware(config CORSConfig) fiber.Handler {
    return cors.New(cors.Config{
        AllowOrigins:     strings.Join(config.AllowedOrigins, ","),
        AllowMethods:     strings.Join(config.AllowedMethods, ","),
        AllowHeaders:     strings.Join(config.AllowedHeaders, ","),
        ExposeHeaders:    strings.Join(config.ExposeHeaders, ","),
        AllowCredentials: config.AllowCredentials,
        MaxAge:           config.MaxAge,
    })
}
```

### Rate Limiting
```go
func RateLimitMiddleware(config RateLimitConfig) fiber.Handler {
    return limiter.New(limiter.Config{
        Max:        config.Requests,
        Expiration: config.Window,
        KeyGenerator: func(c fiber.Ctx) string {
            return c.IP()
        },
        LimitReached: func(c fiber.Ctx) error {
            retryAfter := config.Window.Seconds()
            c.Set("Retry-After", fmt.Sprintf("%.0f", retryAfter))
            return c.Status(429).JSON(fiber.Map{
                "error":       "rate_limit_exceeded",
                "message":     "Too many requests. Please try again later.",
                "retry_after": retryAfter,
            })
        },
        SkipFailedRequests: false,
        SkipSuccessfulRequests: false,
    })
}

// Specific rate limits for sensitive endpoints
func LoginRateLimiter() fiber.Handler {
    return limiter.New(limiter.Config{
        Max:        5,
        Expiration: time.Minute,
        KeyGenerator: func(c fiber.Ctx) string {
            return c.IP() // Per IP
        },
    })
}

func PasswordResetRateLimiter() fiber.Handler {
    return limiter.New(limiter.Config{
        Max:        3,
        Expiration: time.Hour,
        KeyGenerator: func(c fiber.Ctx) string {
            // Rate limit by email from request body
            var req struct{ Email string }
            c.BodyParser(&req)
            return "pwd-reset:" + req.Email
        },
    })
}
```

### Security Headers Middleware
```go
func SecurityHeadersMiddleware() fiber.Handler {
    return func(c fiber.Ctx) error {
        c.Set("X-Content-Type-Options", "nosniff")
        c.Set("X-Frame-Options", "DENY")
        c.Set("X-XSS-Protection", "1; mode=block")
        c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
        c.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

        // HSTS only when TLS is enabled
        if c.Protocol() == "https" {
            c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
        }

        return c.Next()
    }
}
```

### Security Event Logging
```go
type SecurityEvent struct {
    Timestamp time.Time `json:"timestamp"`
    Event     string    `json:"event"`
    UserID    *uint     `json:"user_id,omitempty"`
    Username  string    `json:"username,omitempty"`
    IP        string    `json:"ip"`
    UserAgent string    `json:"user_agent"`
    Success   bool      `json:"success"`
    Details   string    `json:"details,omitempty"`
}

func (l *SecurityLogger) LogLoginAttempt(ctx context.Context, email string, ip string, userAgent string, success bool) {
    event := SecurityEvent{
        Timestamp: time.Now(),
        Event:     "login_attempt",
        Username:  email, // Don't log full email, just identifier
        IP:        ip,
        UserAgent: userAgent,
        Success:   success,
    }
    l.logger.Info("security_event", slog.Any("event", event))
}

func (l *SecurityLogger) LogPasswordChange(ctx context.Context, userID uint, ip string) {
    event := SecurityEvent{
        Timestamp: time.Now(),
        Event:     "password_changed",
        UserID:    &userID,
        IP:        ip,
        Success:   true,
    }
    l.logger.Info("security_event", slog.Any("event", event))
}
```

### Configuration
```
# TLS
CALDAV_TLS_ENABLED=true
CALDAV_TLS_CERT_FILE=/etc/ssl/certs/caldav.pem
CALDAV_TLS_KEY_FILE=/etc/ssl/private/caldav.key

# CORS
CALDAV_CORS_ENABLED=true
CALDAV_CORS_ALLOWED_ORIGINS=https://app.example.com

# Rate Limiting
CALDAV_RATE_LIMIT_ENABLED=true
CALDAV_RATE_LIMIT_REQUESTS=100
CALDAV_RATE_LIMIT_WINDOW=1m

# Request Limits
CALDAV_MAX_REQUEST_SIZE=10485760  # 10MB
CALDAV_REQUEST_TIMEOUT=30s

# Security Headers
CALDAV_HSTS_ENABLED=true
CALDAV_HSTS_MAX_AGE=31536000
```

## Reverse Proxy Configuration

For production, recommend using a reverse proxy (nginx, Caddy, Traefik):

### Nginx Example
```nginx
server {
    listen 443 ssl http2;
    server_name caldav.example.com;

    ssl_certificate /etc/letsencrypt/live/caldav.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/caldav.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebDAV methods
        proxy_pass_request_headers on;
        proxy_set_header Destination $http_destination;
    }
}
```

### Caddy Example
```
caldav.example.com {
    reverse_proxy localhost:8080
}
```

## Definition of Done

- [ ] TLS configuration works with certificate files
- [ ] TLS 1.2+ only, secure cipher suites
- [ ] CORS middleware with configurable origins
- [ ] Rate limiting with configurable limits
- [ ] Per-endpoint rate limits for sensitive operations
- [ ] Security headers applied to all responses
- [ ] HSTS enabled when TLS active
- [ ] Security events logged with relevant details
- [ ] Sensitive data never logged
- [ ] Request size limits enforced
- [ ] Documentation for reverse proxy setup
- [ ] Unit tests for security middleware
