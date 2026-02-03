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
