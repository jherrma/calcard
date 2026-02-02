package sharing

import (
	"context"
)

// CalendarShareRepository defines the interface for calendar share persistence
type CalendarShareRepository interface {
	Create(ctx context.Context, share *CalendarShare) error
	GetByUUID(ctx context.Context, uuid string) (*CalendarShare, error)
	ListByCalendarID(ctx context.Context, calendarID uint) ([]CalendarShare, error)
	FindCalendarsSharedWithUser(ctx context.Context, userID uint) ([]CalendarShare, error)
	Update(ctx context.Context, share *CalendarShare) error
	Revoke(ctx context.Context, id uint) error
	GetByCalendarAndUser(ctx context.Context, calendarID, userID uint) (*CalendarShare, error)
}

// AddressBookShareRepository defines the interface for address book share persistence
type AddressBookShareRepository interface {
	Create(ctx context.Context, share *AddressBookShare) error
	GetByUUID(ctx context.Context, uuid string) (*AddressBookShare, error)
	ListByAddressBookID(ctx context.Context, addressBookID uint) ([]AddressBookShare, error)
	FindAddressBooksSharedWithUser(ctx context.Context, userID uint) ([]AddressBookShare, error)
	Update(ctx context.Context, share *AddressBookShare) error
	Revoke(ctx context.Context, id uint) error
	GetByAddressBookAndUser(ctx context.Context, addressBookID, userID uint) (*AddressBookShare, error)
}
