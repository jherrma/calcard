package database

import (
	"github.com/jherrma/caldav-server/internal/domain/addressbook"

	"github.com/jherrma/caldav-server/internal/domain"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// Models returns all domain models for migration
func Models() []interface{} {
	return []interface{}{
		&user.User{},
		&user.EmailVerification{},
		&user.RefreshToken{},
		&user.PasswordReset{},
		&user.AppPassword{},
		&user.OAuthConnection{},
		&user.SAMLSession{},
		&domain.SystemSetting{},
		&calendar.Calendar{},
		&calendar.CalendarObject{},
		&calendar.SyncChangeLog{},
		&addressbook.AddressBook{},
		&addressbook.AddressObject{},
		&addressbook.ContactPhoto{},
		&addressbook.SyncChangeLog{},
	}
}
