package logging

import (
	"context"
	"log/slog"
	"time"
)

// SecurityLogger handles logging of security-related events
type SecurityLogger struct {
	logger *slog.Logger
}

// NewSecurityLogger creates a new SecurityLogger
func NewSecurityLogger(logger *slog.Logger) *SecurityLogger {
	return &SecurityLogger{logger: logger}
}

// SecurityEvent represents a structured security event
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

// LogLoginAttempt logs a user login attempt
func (l *SecurityLogger) LogLoginAttempt(ctx context.Context, email string, ip string, userAgent string, success bool, details string) {
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "login_attempt",
		Username:  email,
		IP:        ip,
		UserAgent: userAgent,
		Success:   success,
		Details:   details,
	}
	level := slog.LevelInfo
	if !success {
		level = slog.LevelWarn
	}
	l.logger.Log(ctx, level, "security_event", slog.Any("event", event))
}

// LogPasswordChange logs a password change event
func (l *SecurityLogger) LogPasswordChange(ctx context.Context, userID uint, ip string, userAgent string) {
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "password_changed",
		UserID:    &userID,
		IP:        ip,
		UserAgent: userAgent,
		Success:   true,
	}
	l.logger.Info("security_event", slog.Any("event", event))
}

// LogAppPasswordCreated logs the creation of an app password
func (l *SecurityLogger) LogAppPasswordCreated(ctx context.Context, userID uint, name string, ip string, userAgent string) {
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "app_password_created",
		UserID:    &userID,
		Details:   "Name: " + name,
		IP:        ip,
		UserAgent: userAgent,
		Success:   true,
	}
	l.logger.Info("security_event", slog.Any("event", event))
}

// LogAppPasswordRevoked logs the revocation of an app password
func (l *SecurityLogger) LogAppPasswordRevoked(ctx context.Context, userID uint, name string, ip string, userAgent string) {
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "app_password_revoked",
		UserID:    &userID,
		Details:   "Name: " + name,
		IP:        ip,
		UserAgent: userAgent,
		Success:   true,
	}
	l.logger.Info("security_event", slog.Any("event", event))
}

// LogMCPTokenCreated logs the creation of an MCP access token (story 104).
// MCP tokens get their own events rather than reusing the app-password ones so
// an audit can tell a DAV credential apart from a credential that grants the
// AI tool surface.
func (l *SecurityLogger) LogMCPTokenCreated(ctx context.Context, userID uint, name string, ip string, userAgent string) {
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "mcp_token_created",
		UserID:    &userID,
		Details:   "Name: " + name,
		IP:        ip,
		UserAgent: userAgent,
		Success:   true,
	}
	l.logger.Info("security_event", slog.Any("event", event))
}

// LogMCPTokenRevoked logs the revocation of an MCP access token.
func (l *SecurityLogger) LogMCPTokenRevoked(ctx context.Context, userID uint, name string, ip string, userAgent string) {
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "mcp_token_revoked",
		UserID:    &userID,
		Details:   "Name: " + name,
		IP:        ip,
		UserAgent: userAgent,
		Success:   true,
	}
	l.logger.Info("security_event", slog.Any("event", event))
}

// LogMCPAuthFailure logs a rejected MCP bearer credential. reason is the
// sentinel the use case returned (unknown / revoked / expired / inactive) — a
// revoked token being replayed is a different signal from a random string.
func (l *SecurityLogger) LogMCPAuthFailure(ctx context.Context, reason string, ip string, userAgent string) {
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "mcp_auth_failed",
		IP:        ip,
		UserAgent: userAgent,
		Success:   false,
		Details:   reason,
	}
	l.logger.Warn("security_event", slog.Any("event", event))
}

// LogRefreshTokenReuse logs a detected refresh-token reuse — the signal that a
// token was replayed (possible theft). revokeErr, when non-nil, records that
// the containment action (revoking the token family) itself failed.
func (l *SecurityLogger) LogRefreshTokenReuse(ctx context.Context, userID uint, familyID string, ip string, userAgent string, revokeErr error) {
	details := "refresh token reuse detected; token family revoked"
	if familyID == "" {
		details = "refresh token reuse detected on legacy token (no family); token rejected"
	}
	if revokeErr != nil {
		details = "refresh token reuse detected but family revocation FAILED: " + revokeErr.Error()
	}
	event := SecurityEvent{
		Timestamp: time.Now(),
		Event:     "refresh_token_reuse",
		UserID:    &userID,
		IP:        ip,
		UserAgent: userAgent,
		Success:   false,
		Details:   details,
	}
	l.logger.Log(ctx, slog.LevelWarn, "security_event", slog.Any("event", event))
}
