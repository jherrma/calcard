package repository

import (
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// revokedCredentialUsername is the SQL expression a CalDAV/CardDAV credential's
// username is set to on revoke: "revoked:" || uuid (44 characters, fits the
// 50-character column). The username column has a plain unique index that
// also covers revoked rows, so a revoked row keeping its name would make that
// name unusable forever (F24). The same expression is used by the data
// migration that releases names revoked before this existed.
var revokedCredentialUsername = gorm.Expr("'" + user.RevokedCredentialUsernamePrefix + "' || uuid")

// isUniqueViolation reports whether err is a unique-constraint violation,
// using the dialect's own error translation (SQLite extended code 2067/1555,
// PostgreSQL SQLSTATE 23505) since the connection does not set TranslateError.
func isUniqueViolation(db *gorm.DB, err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	if t, ok := db.Dialector.(gorm.ErrorTranslator); ok {
		return errors.Is(t.Translate(err), gorm.ErrDuplicatedKey)
	}
	return false
}
