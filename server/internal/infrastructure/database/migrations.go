package database

import (
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// Models returns all domain models for migration
func Models() []interface{} {
	return []interface{}{
		&user.User{},
		&user.EmailVerification{},
	}
}
