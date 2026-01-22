package auth

import "context"

// EmailService defines the interface for sending emails
type EmailService interface {
	SendActivationEmail(ctx context.Context, to, link string) error
}
