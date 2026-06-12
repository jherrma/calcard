package webdav

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// CardDAVBackend implements carddav.Backend
type CardDAVBackend struct {
	addressBookRepo addressbook.Repository
	userRepo        user.UserRepository
	shareRepo       sharing.AddressBookShareRepository
}

func NewCardDAVBackend(addressBookRepo addressbook.Repository, userRepo user.UserRepository, shareRepo sharing.AddressBookShareRepository) *CardDAVBackend {
	return &CardDAVBackend{
		addressBookRepo: addressBookRepo,
		userRepo:        userRepo,
		shareRepo:       shareRepo,
	}
}

// CurrentUserPrincipal returns the path to the current user's principal resource.
func (b *CardDAVBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return "", webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}
	return fmt.Sprintf("/dav/%s/", u.Username), nil
}

// AddressBookHomeSetPath returns the path to the current user's address book home set.
func (b *CardDAVBackend) AddressBookHomeSetPath(ctx context.Context) (string, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return "", webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}
	return fmt.Sprintf("/dav/%s/addressbooks/", u.Username), nil
}

// ListAddressBooks returns all address books for the current user.
func (b *CardDAVBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	// 1. Get owned address books
	books, err := b.addressBookRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	// 2. Get shared address books
	var shared []sharing.AddressBookShare
	if b.shareRepo != nil {
		shared, err = b.shareRepo.FindAddressBooksSharedWithUser(ctx, u.ID)
		if err != nil {
			return nil, err
		}
	}

	res := make([]carddav.AddressBook, 0, len(books)+len(shared))
	for _, ab := range books {
		res = append(res, *b.mapAddressBook(u.Username, &ab))
	}
	for _, s := range shared {
		res = append(res, *b.mapAddressBook(u.Username, &s.AddressBook))
	}

	return res, nil
}

// GetAddressBook returns an address book by path.
func (b *CardDAVBackend) GetAddressBook(ctx context.Context, p string) (*carddav.AddressBook, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	ab, _, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return nil, err
	}

	return b.mapAddressBook(u.Username, ab), nil
}

// CreateAddressBook creates a new address book.
func (b *CardDAVBackend) CreateAddressBook(ctx context.Context, ab *carddav.AddressBook) error {
	u, ok := UserFromContext(ctx)
	if !ok {
		return webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	// Path: /dav/username/addressbooks/abname/
	parts := strings.Split(strings.Trim(ab.Path, "/"), "/")
	if len(parts) != 4 || parts[1] != u.Username || parts[2] != "addressbooks" {
		return webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	abPath := parts[3]
	newAB := &addressbook.AddressBook{
		UUID:        uuid.New().String(),
		UserID:      u.ID,
		Path:        abPath,
		Name:        ab.Name,
		Description: ab.Description,
	}
	// SyncToken/CTag are minted by Create together with a change-log anchor row.
	return b.addressBookRepo.Create(ctx, newAB)
}

// DeleteAddressBook deletes an address book by path.
func (b *CardDAVBackend) DeleteAddressBook(ctx context.Context, p string) error {
	u, ok := UserFromContext(ctx)
	if !ok {
		return webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	ab, perm, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return err
	}
	if perm != abPermOwner {
		return webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	return b.addressBookRepo.Delete(ctx, ab.ID)
}

// GetAddressObject returns an address object (contact) by path.
func (b *CardDAVBackend) GetAddressObject(ctx context.Context, p string, req *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	obj, _, err := b.resolveAddressObject(ctx, u, p)
	if err != nil {
		return nil, err
	}

	return b.mapAddressObject(p, obj)
}

// ListAddressObjects returns all address objects in an address book.
func (b *CardDAVBackend) ListAddressObjects(ctx context.Context, p string, req *carddav.AddressDataRequest) ([]carddav.AddressObject, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	ab, _, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return nil, err
	}

	// limit=-1 cancels the LIMIT clause; GORM's Limit(0) would emit LIMIT 0
	// and return zero rows, which would make every PROPFIND / lookup see an
	// empty address book.
	objects, _, err := b.addressBookRepo.ListObjects(ctx, ab.ID, -1, 0, "", "")
	if err != nil {
		return nil, err
	}

	res := make([]carddav.AddressObject, 0, len(objects))
	for _, obj := range objects {
		ao, err := b.mapAddressObject(path.Join(p, obj.Path), &obj)
		if err == nil {
			res = append(res, *ao)
		}
	}

	return res, nil
}

// QueryAddressObjects returns address objects matching a query.
// Uses database-level filtering for performance.
func (b *CardDAVBackend) QueryAddressObjects(ctx context.Context, p string, query *carddav.AddressBookQuery) ([]carddav.AddressObject, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	ab, _, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return nil, err
	}

	// Build database query from CardDAV filters
	dbQuery := b.buildDBQuery(query)

	objects, err := b.addressBookRepo.QueryObjects(ctx, ab.ID, dbQuery)
	if err != nil {
		return nil, err
	}

	res := make([]carddav.AddressObject, 0, len(objects))
	for _, obj := range objects {
		ao, err := b.mapAddressObject(path.Join(p, obj.Path), &obj)
		if err != nil {
			continue
		}
		res = append(res, *ao)
	}

	return res, nil
}

// buildDBQuery converts CardDAV query filters to database query format.
func (b *CardDAVBackend) buildDBQuery(query *carddav.AddressBookQuery) *addressbook.ObjectQuery {
	dbQuery := &addressbook.ObjectQuery{
		Limit: int(query.Limit),
	}

	for _, pf := range query.PropFilters {
		filter := addressbook.ObjectQueryFilter{
			PropertyName: pf.Name,
			IsNotDefined: pf.IsNotDefined,
		}

		// If there are text matches, use the first one for DB-level filtering
		// (multiple text-matches are rare in practice)
		if len(pf.TextMatches) > 0 {
			tm := pf.TextMatches[0]
			filter.SearchText = tm.Text
			filter.NegateCondition = tm.NegateCondition

			switch tm.MatchType {
			case carddav.MatchEquals:
				filter.MatchType = "equals"
			case carddav.MatchContains:
				filter.MatchType = "contains"
			case carddav.MatchStartsWith:
				filter.MatchType = "starts-with"
			case carddav.MatchEndsWith:
				filter.MatchType = "ends-with"
			default:
				filter.MatchType = "contains"
			}
		}

		dbQuery.Filters = append(dbQuery.Filters, filter)
	}

	return dbQuery
}

// PutAddressObject creates or updates an address object.
func (b *CardDAVBackend) PutAddressObject(ctx context.Context, p string, card vcard.Card, opts *carddav.PutAddressObjectOptions) (*carddav.AddressObject, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[1] != u.Username || parts[2] != "addressbooks" {
		return nil, webdav.NewHTTPError(http.StatusForbidden, nil)
	}
	objPath := parts[4]

	// Resolve the address book (owned or shared) and require write permission.
	ab, perm, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return nil, err
	}
	if perm != abPermOwner && perm != abPermReadWrite {
		return nil, webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	// Extract UID and metadata
	uid := card.Value(vcard.FieldUID)
	if uid == "" {
		uid = uuid.New().String()
		card.SetValue(vcard.FieldUID, uid)
	}

	// Serialize vCard
	var vcardData strings.Builder
	if err := vcard.NewEncoder(&vcardData).Encode(card); err != nil {
		return nil, err
	}
	data := vcardData.String()

	// Determine vCard version
	version := card.Value(vcard.FieldVersion)
	if version == "" {
		version = "3.0"
	}

	etag := addressbook.NewETag()

	// Look up an existing object at this exact path only (not by UID across
	// paths — that silently clobbered unrelated contacts).
	existing, err := b.addressBookRepo.GetObjectByPath(ctx, ab.ID, objPath)
	if err != nil {
		return nil, err
	}

	// Honor If-Match / If-None-Match preconditions (RFC 6352 §6.3.2), matching
	// the CalDAV behavior, to prevent silent lost updates / clobbering creates.
	if opts != nil {
		if opts.IfNoneMatch.IsSet() && existing != nil {
			return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, nil)
		}
		if opts.IfMatch.IsSet() {
			if existing == nil {
				return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, nil)
			}
			ok, _ := opts.IfMatch.MatchETag(existing.ETag)
			if !ok {
				return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, nil)
			}
		}
	}

	// no-uid-conflict (RFC 6352 §6.3.2): a different resource in this book must
	// not already own this UID.
	if other, _ := b.addressBookRepo.GetObjectByUID(ctx, ab.ID, uid); other != nil && other.Path != objPath {
		return nil, carddav.NewPreconditionError(carddav.PreconditionNoUIDConflict)
	}

	var obj *addressbook.AddressObject
	if existing != nil {
		existing.VCardData = data
		existing.ETag = etag
		existing.ContentLength = len(data)
		existing.VCardVersion = version
		// Rederive denormalized columns from the parsed card — single source
		// of truth across every write path (REST create, DAV PUT, import).
		addressbook.ExtractDenormFieldsFromCard(card, existing)
		if err := b.addressBookRepo.UpdateObject(ctx, existing); err != nil {
			return nil, err
		}
		obj = existing
	} else {
		newObj := &addressbook.AddressObject{
			UUID:          uuid.New().String(),
			AddressBookID: ab.ID,
			Path:          objPath,
			UID:           uid,
			ETag:          etag,
			VCardData:     data,
			VCardVersion:  version,
			ContentLength: len(data),
		}
		addressbook.ExtractDenormFieldsFromCard(card, newObj)
		if err := b.addressBookRepo.CreateObject(ctx, newObj); err != nil {
			return nil, err
		}
		obj = newObj
	}

	// AddressBook sync token / CTag are advanced atomically by CreateObject
	// / UpdateObject through AddressBookRepository.recordAddressBookChange,
	// so we deliberately do NOT call UpdateSyncTokens + Update here — that
	// would generate a second, different token and desync the change log
	// from the address book row.

	return b.mapAddressObject(p, obj)
}

// DeleteAddressObject deletes an address object.
func (b *CardDAVBackend) DeleteAddressObject(ctx context.Context, p string) error {
	u, ok := UserFromContext(ctx)
	if !ok {
		return webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	obj, perm, err := b.resolveAddressObject(ctx, u, p)
	if err != nil {
		return err
	}
	if perm != abPermOwner && perm != abPermReadWrite {
		return webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	// DeleteObjectByUUID records the change-log entry and advances the
	// AddressBook sync token in one transaction — no manual post-delete
	// UpdateSyncTokens call needed.
	if err := b.addressBookRepo.DeleteObjectByUUID(ctx, obj.UUID); err != nil {
		return err
	}

	return nil
}

// Address book permission levels returned by resolveAddressBook.
const (
	abPermOwner     = "owner"
	abPermRead      = "read"
	abPermReadWrite = "read-write"
)

// resolveAddressBook parses a path and returns the corresponding address book
// along with the requesting user's permission ("owner", "read", or
// "read-write"). It resolves both owned and shared-with-the-user books so
// CardDAV sharing actually works.
func (b *CardDAVBackend) resolveAddressBook(ctx context.Context, u *user.User, p string) (*addressbook.AddressBook, string, error) {
	// Path: /dav/username/addressbooks/abname/
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 4 || parts[0] != "dav" || parts[1] != u.Username || parts[2] != "addressbooks" {
		return nil, "", webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	abPath := parts[3]
	books, err := b.addressBookRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}

	// Owned books win on a path collision (same caveat as the calendar side).
	for _, ab := range books {
		if ab.Path == abPath {
			ab := ab
			return &ab, abPermOwner, nil
		}
	}

	// Fall back to address books shared with this user.
	if b.shareRepo != nil {
		shared, err := b.shareRepo.FindAddressBooksSharedWithUser(ctx, u.ID)
		if err != nil {
			return nil, "", err
		}
		for _, s := range shared {
			if s.AddressBook.Path == abPath {
				ab := s.AddressBook
				return &ab, s.Permission, nil
			}
		}
	}

	return nil, "", webdav.NewHTTPError(http.StatusNotFound, nil)
}

// resolveAddressObject parses a path and returns the corresponding address
// object along with the requesting user's permission on its address book.
func (b *CardDAVBackend) resolveAddressObject(ctx context.Context, u *user.User, p string) (*addressbook.AddressObject, string, error) {
	// Path: /dav/username/addressbooks/abname/contact.vcf
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[0] != "dav" || parts[1] != u.Username || parts[2] != "addressbooks" {
		return nil, "", webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	objPath := parts[4]

	ab, perm, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return nil, "", err
	}

	// limit=-1 cancels the LIMIT clause; GORM's Limit(0) would emit LIMIT 0
	// and return zero rows, which would make every PROPFIND / lookup see an
	// empty address book.
	objects, _, err := b.addressBookRepo.ListObjects(ctx, ab.ID, -1, 0, "", "")
	if err != nil {
		return nil, "", err
	}

	for _, obj := range objects {
		if obj.Path == objPath {
			obj := obj
			return &obj, perm, nil
		}
	}

	// Fall back to a UUID-named object path (<uuid>.vcf). Test objPath (not the
	// address book segment) and scope strictly to the resolved book so a known
	// UUID can't reach another tenant's contact.
	if strings.HasSuffix(objPath, ".vcf") {
		objUUID := strings.TrimSuffix(objPath, ".vcf")
		obj, err := b.addressBookRepo.GetObjectByUUID(ctx, objUUID)
		if err == nil && obj != nil && obj.AddressBookID == ab.ID {
			return obj, perm, nil
		}
	}

	return nil, "", webdav.NewHTTPError(http.StatusNotFound, nil)
}

// mapAddressBook converts domain AddressBook to carddav.AddressBook.
func (b *CardDAVBackend) mapAddressBook(username string, ab *addressbook.AddressBook) *carddav.AddressBook {
	return &carddav.AddressBook{
		Path:            fmt.Sprintf("/dav/%s/addressbooks/%s/", username, ab.Path),
		Name:            ab.Name,
		Description:     ab.Description,
		MaxResourceSize: 102400, // 100KB
		SupportedAddressData: []carddav.AddressDataType{
			{ContentType: "text/vcard", Version: "3.0"},
			{ContentType: "text/vcard", Version: "4.0"},
		},
	}
}

// mapAddressObject converts domain AddressObject to carddav.AddressObject.
func (b *CardDAVBackend) mapAddressObject(p string, obj *addressbook.AddressObject) (*carddav.AddressObject, error) {
	card, err := vcard.NewDecoder(strings.NewReader(obj.VCardData)).Decode()
	if err != nil {
		return nil, err
	}

	return &carddav.AddressObject{
		Path:          p,
		Card:          card,
		ETag:          obj.ETag,
		ContentLength: int64(obj.ContentLength),
		ModTime:       obj.UpdatedAt,
	}, nil
}

// GetSyncChanges returns all changes since the given sync token.
func (b *CardDAVBackend) GetSyncChanges(ctx context.Context, addressBookPath, token string) ([]*addressbook.SyncChangeLog, string, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, "", fmt.Errorf("unauthorized")
	}

	ab, _, err := b.resolveAddressBook(ctx, u, addressBookPath)
	if err != nil {
		return nil, "", err
	}

	changes, err := b.addressBookRepo.GetChangesSinceToken(ctx, ab.ID, token)
	if err != nil {
		return nil, "", err
	}

	return changes, ab.SyncToken, nil
}

// GetAddressObjectByPath returns an address object by its path within an address book.
func (b *CardDAVBackend) GetAddressObjectByPath(ctx context.Context, addressBookID uint, objPath string) (*addressbook.AddressObject, error) {
	return b.addressBookRepo.GetObjectByPath(ctx, addressBookID, objPath)
}
