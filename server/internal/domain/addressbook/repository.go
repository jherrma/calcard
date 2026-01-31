package addressbook

import "context"

type Repository interface {
	Create(ctx context.Context, addressBook *AddressBook) error
	GetByID(ctx context.Context, id uint) (*AddressBook, error)
	GetByUUID(ctx context.Context, uuid string) (*AddressBook, error)
	ListByUserID(ctx context.Context, userID uint) ([]AddressBook, error)
	Update(ctx context.Context, addressBook *AddressBook) error
	Delete(ctx context.Context, id uint) error
	CreateObject(ctx context.Context, object *AddressObject) error
	GetObjectByID(ctx context.Context, id uint) (*AddressObject, error)
	ListObjects(ctx context.Context, addressBookID uint) ([]AddressObject, error)
}
