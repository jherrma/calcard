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
	Delete(ctx context.Context, id uint) error
	CreateObject(ctx context.Context, object *AddressObject) error
	GetObjectByID(ctx context.Context, id uint) (*AddressObject, error)
	GetObjectByPath(ctx context.Context, addressBookID uint, path string) (*AddressObject, error)
	ListObjects(ctx context.Context, addressBookID uint, limit, offset int, sort, order string) ([]AddressObject, int64, error)
	QueryObjects(ctx context.Context, addressBookID uint, query *ObjectQuery) ([]AddressObject, error)
	GetObjectByUUID(ctx context.Context, uuid string) (*AddressObject, error)
	UpdateObject(ctx context.Context, object *AddressObject) error
	DeleteObjectByUUID(ctx context.Context, uuid string) error
	SearchObjects(ctx context.Context, userID uint, query string, addressBookID *uint, limit int) ([]AddressObject, error)

	// Sync-related methods for WebDAV-Sync (RFC 6578)
	GetChangesSinceToken(ctx context.Context, addressBookID uint, token string) ([]*SyncChangeLog, error)
	RecordChange(ctx context.Context, addressBookID uint, path, uid, changeType, token string) error
}
