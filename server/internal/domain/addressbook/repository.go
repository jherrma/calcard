package addressbook

import "context"

// ObjectQueryFilter defines filters for querying address objects at database level
type ObjectQueryFilter struct {
	PropertyName    string // vCard property to filter on (e.g., "FN", "EMAIL")
	MatchType       string // "equals", "contains", "starts-with", "ends-with"
	SearchText      string // The text to match
	IsNotDefined    bool   // True if filtering for missing property
	NegateCondition bool   // True to invert the match
}

// ObjectQuery contains query parameters for address object filtering
type ObjectQuery struct {
	Filters []ObjectQueryFilter
	Limit   int
}

type Repository interface {
	Create(ctx context.Context, addressBook *AddressBook) error
	GetByID(ctx context.Context, id uint) (*AddressBook, error)
	GetByUUID(ctx context.Context, uuid string) (*AddressBook, error)
	GetByUserAndPath(ctx context.Context, userID uint, path string) (*AddressBook, error)
	ListByUserID(ctx context.Context, userID uint) ([]AddressBook, error)
	Update(ctx context.Context, addressBook *AddressBook) error
	// UpdateMetadata persists an address-book rename (name/description only) and
	// advances the sync token via a change-log anchor row, atomically in a single
	// transaction. It updates only the named columns (never sync_token/c_tag from
	// the possibly-stale passed struct) and mints a fresh token in the same tx.
	UpdateMetadata(ctx context.Context, addressBook *AddressBook) error
	Delete(ctx context.Context, id uint) error
	CreateObject(ctx context.Context, object *AddressObject) error
	GetObjectByID(ctx context.Context, id uint) (*AddressObject, error)
	GetObjectByPath(ctx context.Context, addressBookID uint, path string) (*AddressObject, error)
	GetObjectByUID(ctx context.Context, addressBookID uint, uid string) (*AddressObject, error)
	ListObjects(ctx context.Context, addressBookID uint, limit, offset int, sort, order string) ([]AddressObject, int64, error)
	QueryObjects(ctx context.Context, addressBookID uint, query *ObjectQuery) ([]AddressObject, error)
	GetObjectByUUID(ctx context.Context, uuid string) (*AddressObject, error)
	UpdateObject(ctx context.Context, object *AddressObject) error
	// MoveObject reassigns an object to a new address book (object.AddressBookID
	// must already be set to the target) and, atomically in a single
	// transaction, records a "modified" change on the target book and a
	// "deleted" change on the source book so both collections' sync clients
	// converge. Used by the cross-book contact move use case.
	MoveObject(ctx context.Context, object *AddressObject, sourceAddressBookID uint) error
	DeleteObjectByUUID(ctx context.Context, uuid string) error

	// SearchObjectsInBooks searches contacts across an explicit set of address
	// books. The caller resolves which books it may read — including books merely
	// shared with it (#53) — and this is the ONLY contact-search entry point, so
	// that resolution can't be bypassed. The owner-scoped predecessor
	// (SearchObjects, filtered on address_books.user_id) was removed in #162: it
	// could not reach a shared book, so a contact the caller could see and edit
	// disappeared as soon as they searched for it. An empty slice matches nothing
	// rather than everything.
	SearchObjectsInBooks(ctx context.Context, addressBookIDs []uint, query string, limit, offset int) ([]AddressObject, error)

	// GetUserPermission resolves a user's effective permission on an address
	// book, accounting for both ownership and shares (#53). Returns
	// PermissionNone — never an error — when the book is missing or the user
	// has no share on it, so callers can treat "no access" and "not found"
	// identically without leaking existence. Mirrors
	// calendar.CalendarRepository.GetUserPermission.
	GetUserPermission(ctx context.Context, addressBookID, userID uint) (AddressBookPermission, error)

	// CountContactsByUserID counts all contacts across all address books for a user
	CountContactsByUserID(ctx context.Context, userID uint) (int64, error)

	// Sync-related methods for WebDAV-Sync (RFC 6578)
	GetChangesSinceToken(ctx context.Context, addressBookID uint, token string) ([]*SyncChangeLog, error)
	RecordChange(ctx context.Context, addressBookID uint, path, uid, changeType string) error
}
