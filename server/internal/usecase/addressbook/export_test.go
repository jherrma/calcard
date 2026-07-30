package addressbook_test

import (
	"context"
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	addressbookuc "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock repository
type mockRepo struct {
	mock.Mock
}

func (m *mockRepo) GetByID(ctx context.Context, id uint) (*addressbook.AddressBook, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*addressbook.AddressBook), args.Error(1)
}

func (m *mockRepo) ListObjects(ctx context.Context, addressBookID uint, limit, offset int, sort, order string) ([]addressbook.AddressObject, int64, error) {
	args := m.Called(ctx, addressBookID)
	return args.Get(0).([]addressbook.AddressObject), int64(len(args.Get(0).([]addressbook.AddressObject))), args.Error(1)
}

// Implement other methods to satisfy interface...
func (m *mockRepo) Create(ctx context.Context, ab *addressbook.AddressBook) error { return nil }
func (m *mockRepo) ListByUserID(ctx context.Context, userID uint) ([]addressbook.AddressBook, error) {
	return nil, nil
}
func (m *mockRepo) Update(ctx context.Context, ab *addressbook.AddressBook) error { return nil }
func (m *mockRepo) UpdateMetadata(ctx context.Context, ab *addressbook.AddressBook) error {
	return nil
}
func (m *mockRepo) Delete(ctx context.Context, id uint) error { return nil }
func (m *mockRepo) CreateObject(ctx context.Context, object *addressbook.AddressObject) error {
	return nil
}
func (m *mockRepo) GetObjectByID(ctx context.Context, id uint) (*addressbook.AddressObject, error) {
	return nil, nil
}
func (m *mockRepo) GetByUUID(ctx context.Context, uuid string) (*addressbook.AddressBook, error) {
	return nil, nil
}
func (m *mockRepo) GetObjectByUUID(ctx context.Context, uuid string) (*addressbook.AddressObject, error) {
	return nil, nil
}
func (m *mockRepo) UpdateObject(ctx context.Context, object *addressbook.AddressObject) error {
	return nil
}
func (m *mockRepo) MoveObject(ctx context.Context, object *addressbook.AddressObject, sourceAddressBookID uint) error {
	return nil
}
func (m *mockRepo) DeleteObjectByUUID(ctx context.Context, uuid string) error { return nil }
func (m *mockRepo) SearchObjects(ctx context.Context, userID uint, query string, addressBookID *uint, limit int) ([]addressbook.AddressObject, error) {
	return nil, nil
}
func (m *mockRepo) GetByUserAndPath(ctx context.Context, userID uint, path string) (*addressbook.AddressBook, error) {
	return nil, nil
}
func (m *mockRepo) GetObjectByPath(ctx context.Context, addressBookID uint, path string) (*addressbook.AddressObject, error) {
	return nil, nil
}
func (m *mockRepo) GetObjectByUID(ctx context.Context, addressBookID uint, uid string) (*addressbook.AddressObject, error) {
	return nil, nil
}
func (m *mockRepo) QueryObjects(ctx context.Context, addressBookID uint, query *addressbook.ObjectQuery) ([]addressbook.AddressObject, error) {
	return nil, nil
}
func (m *mockRepo) GetChangesSinceToken(ctx context.Context, addressBookID uint, token string) ([]*addressbook.SyncChangeLog, error) {
	return nil, nil
}
func (m *mockRepo) RecordChange(ctx context.Context, addressBookID uint, path, uid, changeType string) error {
	return nil
}
func (m *mockRepo) CountContactsByUserID(ctx context.Context, userID uint) (int64, error) {
	return 0, nil
}

// GetUserPermission: the export use case doesn't consult permissions, so a
// fixed owner keeps this mock satisfying the interface without affecting the
// assertions below.
func (m *mockRepo) GetUserPermission(ctx context.Context, addressBookID, userID uint) (addressbook.AddressBookPermission, error) {
	return addressbook.PermissionOwner, nil
}

func TestExportUseCase_Execute(t *testing.T) {
	repo := new(mockRepo)
	uc := addressbookuc.NewExportUseCase(repo)
	ctx := context.Background()

	userID := uint(1)
	abID := uint(10)

	ab := &addressbook.AddressBook{
		ID:     abID,
		UserID: userID,
		Name:   "MyContacts",
	}

	contact1Data := "BEGIN:VCARD\nVERSION:3.0\nFN:John Doe\nEND:VCARD"
	contact2Data := "BEGIN:VCARD\nVERSION:3.0\nFN:Jane Smith\nEND:VCARD"

	contacts := []addressbook.AddressObject{
		{VCardData: contact1Data},
		{VCardData: contact2Data},
	}

	repo.On("GetByID", ctx, abID).Return(ab, nil)
	repo.On("ListObjects", ctx, abID).Return(contacts, nil)

	data, filename, err := uc.Execute(ctx, abID, userID)

	assert.NoError(t, err)
	assert.Equal(t, "MyContacts.vcf", filename)

	// Verify we can parse multiple cards from the output
	dec := vcard.NewDecoder(strings.NewReader(string(data)))

	card1, err := dec.Decode()
	assert.NoError(t, err)
	assert.Equal(t, "John Doe", card1.PreferredValue(vcard.FieldFormattedName))

	card2, err := dec.Decode()
	assert.NoError(t, err)
	assert.Equal(t, "Jane Smith", card2.PreferredValue(vcard.FieldFormattedName))
}

// TestExportUseCase_SanitizesUnsafeFilename feeds sanitizeFilename an actually
// dangerous name (path traversal + shell/FS metacharacters). The "MyContacts"
// case above would pass even if the sanitizer were a no-op, so it never proved
// anything; this case does.
func TestExportUseCase_SanitizesUnsafeFilename(t *testing.T) {
	repo := new(mockRepo)
	uc := addressbookuc.NewExportUseCase(repo)
	ctx := context.Background()

	userID := uint(1)
	abID := uint(11)
	ab := &addressbook.AddressBook{ID: abID, UserID: userID, Name: `../../etc/passwd:a*b?"<>|`}

	repo.On("GetByID", ctx, abID).Return(ab, nil)
	repo.On("ListObjects", ctx, abID).Return([]addressbook.AddressObject{}, nil)

	_, filename, err := uc.Execute(ctx, abID, userID)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	for _, bad := range []string{"/", `\`, ":", "*", "?", `"`, "<", ">", "|"} {
		assert.NotContainsf(t, filename, bad, "sanitized filename %q must not contain %q", filename, bad)
	}
	assert.NotContains(t, filename, "../", "sanitized filename must not be a path-traversal string")
	assert.NotContains(t, filename, `..\`, "sanitized filename must not be a path-traversal string")
	assert.True(t, strings.HasSuffix(filename, ".vcf"), "expected .vcf extension, got %q", filename)
}

func TestCreateContactUseCase_Execute_AddressBookNotFound(t *testing.T) {
	repo := new(mockRepo)
	uc := addressbookuc.NewCreateContactUseCase(repo)
	ctx := context.Background()

	abID := uint(999)
	// AddressBookRepository.GetByID returns (nil, nil) when the row is not found.
	repo.On("GetByID", ctx, abID).Return((*addressbook.AddressBook)(nil), nil)

	_, err := uc.Execute(ctx, addressbookuc.CreateContactInput{
		AddressBookID: abID,
		UserID:        1,
		VCardData:     "BEGIN:VCARD\nVERSION:3.0\nFN:John Doe\nEND:VCARD",
	})

	// Must return an error rather than panicking on the nil address book.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	repo.AssertExpectations(t)
}
