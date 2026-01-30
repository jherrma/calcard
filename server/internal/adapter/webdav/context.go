package webdav

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

type contextKey string

const userContextKey contextKey = "user"

// WithUser adds a user to the context
func WithUser(ctx context.Context, u *user.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// UserFromContext retrieves a user from the context
func UserFromContext(ctx context.Context) (*user.User, bool) {
	u, ok := ctx.Value(userContextKey).(*user.User)
	return u, ok
}
