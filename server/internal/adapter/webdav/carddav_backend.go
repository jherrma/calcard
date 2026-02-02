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
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// CardDAVBackend implements carddav.Backend
type CardDAVBackend struct {
	addressBookRepo addressbook.Repository
	userRepo        user.UserRepository
}

func NewCardDAVBackend(addressBookRepo addressbook.Repository, userRepo user.UserRepository) *CardDAVBackend {
	return &CardDAVBackend{
		addressBookRepo: addressBookRepo,
		userRepo:        userRepo,
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

	books, err := b.addressBookRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	res := make([]carddav.AddressBook, len(books))
	for i, ab := range books {
		res[i] = *b.mapAddressBook(u.Username, &ab)
	}
	return res, nil
}

// GetAddressBook returns an address book by path.
func (b *CardDAVBackend) GetAddressBook(ctx context.Context, p string) (*carddav.AddressBook, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	ab, err := b.resolveAddressBook(ctx, u, p)
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
	newAB.UpdateSyncTokens()

	return b.addressBookRepo.Create(ctx, newAB)
}

// DeleteAddressBook deletes an address book by path.
func (b *CardDAVBackend) DeleteAddressBook(ctx context.Context, p string) error {
	u, ok := UserFromContext(ctx)
	if !ok {
		return webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	ab, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return err
	}

	return b.addressBookRepo.Delete(ctx, ab.ID)
}

// GetAddressObject returns an address object (contact) by path.
func (b *CardDAVBackend) GetAddressObject(ctx context.Context, p string, req *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	obj, err := b.resolveAddressObject(ctx, u, p)
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

	ab, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return nil, err
	}

	objects, _, err := b.addressBookRepo.ListObjects(ctx, ab.ID, 0, 0, "", "")
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

	ab, err := b.resolveAddressBook(ctx, u, p)
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

	abPath := parts[3]
	objPath := parts[4]

	// Find the address book
	books, err := b.addressBookRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	var ab *addressbook.AddressBook
	for _, book := range books {
		if book.Path == abPath {
			ab = &book
			break
		}
	}
	if ab == nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
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

	etag := fmt.Sprintf("\"%s\"", addressbook.GenerateSyncToken())

	// Check if object exists
	existingObjects, _, err := b.addressBookRepo.ListObjects(ctx, ab.ID, 0, 0, "", "")
	if err != nil {
		return nil, err
	}

	var existing *addressbook.AddressObject
	for _, obj := range existingObjects {
		if obj.Path == objPath || obj.UID == uid {
			existing = &obj
			break
		}
	}

	var obj *addressbook.AddressObject
	if existing != nil {
		existing.VCardData = data
		existing.ETag = etag
		existing.ContentLength = len(data)
		existing.VCardVersion = version
		existing.FormattedName = card.PreferredValue(vcard.FieldFormattedName)
		existing.Email = card.PreferredValue(vcard.FieldEmail)
		existing.Phone = card.PreferredValue(vcard.FieldTelephone)
		existing.Organization = card.PreferredValue(vcard.FieldOrganization)
		if name := card.Name(); name != nil {
			existing.GivenName = name.GivenName
			existing.FamilyName = name.FamilyName
		}
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
			FormattedName: card.PreferredValue(vcard.FieldFormattedName),
			Email:         card.PreferredValue(vcard.FieldEmail),
			Phone:         card.PreferredValue(vcard.FieldTelephone),
			Organization:  card.PreferredValue(vcard.FieldOrganization),
		}
		if name := card.Name(); name != nil {
			newObj.GivenName = name.GivenName
			newObj.FamilyName = name.FamilyName
		}
		if err := b.addressBookRepo.CreateObject(ctx, newObj); err != nil {
			return nil, err
		}
		obj = newObj
	}

	// Update address book sync tokens
	ab.UpdateSyncTokens()
	_ = b.addressBookRepo.Update(ctx, ab)

	return b.mapAddressObject(p, obj)
}

// DeleteAddressObject deletes an address object.
func (b *CardDAVBackend) DeleteAddressObject(ctx context.Context, p string) error {
	u, ok := UserFromContext(ctx)
	if !ok {
		return webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	obj, err := b.resolveAddressObject(ctx, u, p)
	if err != nil {
		return err
	}

	if err := b.addressBookRepo.DeleteObjectByUUID(ctx, obj.UUID); err != nil {
		return err
	}

	// Update address book sync token
	ab, err := b.addressBookRepo.GetByID(ctx, obj.AddressBookID)
	if err == nil && ab != nil {
		ab.UpdateSyncTokens()
		_ = b.addressBookRepo.Update(ctx, ab)
	}

	return nil
}

// resolveAddressBook parses a path and returns the corresponding address book.
func (b *CardDAVBackend) resolveAddressBook(ctx context.Context, u *user.User, p string) (*addressbook.AddressBook, error) {
	// Path: /dav/username/addressbooks/abname/
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 4 || parts[0] != "dav" || parts[1] != u.Username || parts[2] != "addressbooks" {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	abPath := parts[3]
	books, err := b.addressBookRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	for _, ab := range books {
		if ab.Path == abPath {
			return &ab, nil
		}
	}

	return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
}

// resolveAddressObject parses a path and returns the corresponding address object.
func (b *CardDAVBackend) resolveAddressObject(ctx context.Context, u *user.User, p string) (*addressbook.AddressObject, error) {
	// Path: /dav/username/addressbooks/abname/contact.vcf
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[0] != "dav" || parts[1] != u.Username || parts[2] != "addressbooks" {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	abPath := parts[3]
	objPath := parts[4]

	ab, err := b.resolveAddressBook(ctx, u, p)
	if err != nil {
		return nil, err
	}

	objects, _, err := b.addressBookRepo.ListObjects(ctx, ab.ID, 0, 0, "", "")
	if err != nil {
		return nil, err
	}

	for _, obj := range objects {
		if obj.Path == objPath {
			return &obj, nil
		}
	}

	// Also check by UUID in case path is different
	if strings.HasSuffix(abPath, ".vcf") {
		objUUID := strings.TrimSuffix(abPath, ".vcf")
		obj, err := b.addressBookRepo.GetObjectByUUID(ctx, objUUID)
		if err == nil && obj != nil {
			return obj, nil
		}
	}

	return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
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
