# Story 017: Address Book Management

## Title
Implement Address Book Domain Model and REST API

## Description
As a user, I want to create, view, update, and delete address books so that I can organize my contacts.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AD-4.1.1 | Users have a default address book on account creation |
| AD-4.1.2 | Users can create additional address books |
| AD-4.1.3 | Users can rename address books |
| AD-4.1.4 | Users can delete address books |
| AD-4.1.5 | Users can export address book as .vcf file |

## Acceptance Criteria

### Default Address Book Creation

- [ ] When user account is created, a default address book is created
- [ ] Default address book name: "Contacts"
- [ ] Created alongside default calendar in user registration flow

### Create Address Book

- [ ] REST endpoint `POST /api/v1/addressbooks` (requires auth)
- [ ] Request body:
  ```json
  {
    "name": "Work Contacts",
    "description": "Professional contacts"
  }
  ```
- [ ] Name is required, max 255 characters
- [ ] Description is optional, max 1000 characters
- [ ] UUID generated for address book
- [ ] Path generated from UUID
- [ ] Initial sync_token and ctag generated
- [ ] Returns 201 Created with address book data

### List Address Books

- [ ] REST endpoint `GET /api/v1/addressbooks` (requires auth)
- [ ] Returns all address books owned by user
- [ ] Includes address books shared with user (with `shared: true` flag)
- [ ] Returns:
  - [ ] ID (UUID)
  - [ ] Name
  - [ ] Description
  - [ ] Contact count
  - [ ] Owner info (for shared address books)
  - [ ] Permission level (for shared address books)

### Get Single Address Book

- [ ] REST endpoint `GET /api/v1/addressbooks/{id}` (requires auth)
- [ ] Returns full address book details
- [ ] Returns 404 if address book not found or not accessible

### Update Address Book

- [ ] REST endpoint `PATCH /api/v1/addressbooks/{id}` (requires auth)
- [ ] Updatable fields: name, description
- [ ] Cannot update address books shared with read-only permission
- [ ] Returns updated address book data

### Delete Address Book

- [ ] REST endpoint `DELETE /api/v1/addressbooks/{id}` (requires auth)
- [ ] Request body (confirmation):
  ```json
  {
    "confirmation": "DELETE"
  }
  ```
- [ ] Returns 400 if confirmation not provided
- [ ] Cannot delete last address book (user must have at least one)
- [ ] All contacts in address book are deleted
- [ ] All shares are revoked
- [ ] Returns 204 No Content

### Export Address Book

- [ ] REST endpoint `GET /api/v1/addressbooks/{id}/export` (requires auth)
- [ ] Returns complete vCard (.vcf) file with all contacts
- [ ] Content-Type: `text/vcard`
- [ ] Content-Disposition: `attachment; filename="{addressbook-name}.vcf"`

## Technical Notes

### Database Model
```go
type AddressBook struct {
    ID          uint           `gorm:"primaryKey"`
    UUID        string         `gorm:"uniqueIndex;size:36;not null"`
    UserID      uint           `gorm:"index;not null"`
    Path        string         `gorm:"size:255;not null"`
    Name        string         `gorm:"size:255;not null"`
    Description string         `gorm:"size:1000"`
    SyncToken   string         `gorm:"size:64;not null"`
    CTag        string         `gorm:"size:64;not null"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
    DeletedAt   gorm.DeletedAt `gorm:"index"`
    User        User           `gorm:"foreignKey:UserID"`
    Contacts    []AddressObject `gorm:"foreignKey:AddressBookID"`
}

type AddressObject struct {
    ID            uint           `gorm:"primaryKey"`
    UUID          string         `gorm:"uniqueIndex;size:36;not null"`
    AddressBookID uint           `gorm:"index;not null"`
    Path          string         `gorm:"size:255;not null"`
    UID           string         `gorm:"index;size:255;not null"` // vCard UID
    ETag          string         `gorm:"size:64;not null"`
    VCardData     string         `gorm:"type:text;not null"`
    VCardVersion  string         `gorm:"size:5;not null"`  // "3.0" or "4.0"
    ContentLength int            `gorm:"not null"`
    // Denormalized fields for search
    FormattedName string         `gorm:"size:500;index"`
    GivenName     string         `gorm:"size:255"`
    FamilyName    string         `gorm:"size:255"`
    Email         string         `gorm:"size:255;index"`  // Primary email
    Phone         string         `gorm:"size:50"`         // Primary phone
    Organization  string         `gorm:"size:255"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt `gorm:"index"`
    AddressBook   AddressBook    `gorm:"foreignKey:AddressBookID"`
}
```

### User Registration Update
```go
// In user registration use case, add address book creation
func (uc *RegisterUseCase) Execute(ctx context.Context, input RegisterInput) (*User, error) {
    // ... existing user creation logic ...

    // Create default calendar (from Story 013)
    defaultCalendar := &Calendar{
        UserID: user.ID,
        UUID:   uuid.New().String(),
        Name:   "Personal",
        Color:  "#3788d8",
        // ...
    }
    uc.calendarRepo.Create(ctx, defaultCalendar)

    // Create default address book
    defaultAddressBook := &AddressBook{
        UserID:    user.ID,
        UUID:      uuid.New().String(),
        Name:      "Contacts",
        SyncToken: generateSyncToken(),
        CTag:      generateCTag(),
    }
    uc.addressBookRepo.Create(ctx, defaultAddressBook)

    return user, nil
}
```

### Code Structure
```
internal/domain/addressbook/
├── addressbook.go       # AddressBook entity
├── address_object.go    # AddressObject (contact) entity
├── repository.go        # Repository interface
└── validation.go        # Validation rules

internal/usecase/addressbook/
├── create.go            # Create address book
├── list.go              # List address books
├── get.go               # Get address book
├── update.go            # Update address book
├── delete.go            # Delete address book
└── export.go            # Export address book

internal/adapter/http/
└── addressbook_handler.go  # HTTP handlers

internal/adapter/repository/
└── addressbook_repo.go  # GORM repository
```

## API Response Examples

### Create Address Book (201 Created)
```json
{
  "id": "660e8400-e29b-41d4-a716-446655440001",
  "name": "Work Contacts",
  "description": "Professional contacts",
  "contact_count": 0,
  "created_at": "2024-01-21T10:00:00Z",
  "updated_at": "2024-01-21T10:00:00Z",
  "carddav_url": "/dav/addressbooks/johndoe/660e8400-e29b-41d4-a716-446655440001/"
}
```

### List Address Books (200 OK)
```json
{
  "addressbooks": [
    {
      "id": "660e8400-e29b-41d4-a716-446655440000",
      "name": "Contacts",
      "description": null,
      "contact_count": 150,
      "is_default": true,
      "shared": false,
      "created_at": "2024-01-15T10:00:00Z"
    },
    {
      "id": "660e8400-e29b-41d4-a716-446655440001",
      "name": "Work Contacts",
      "description": "Professional contacts",
      "contact_count": 45,
      "is_default": false,
      "shared": false,
      "created_at": "2024-01-21T10:00:00Z"
    },
    {
      "id": "770e8400-e29b-41d4-a716-446655440002",
      "name": "Team Directory",
      "contact_count": 30,
      "shared": true,
      "owner": {
        "id": "880e8400-e29b-41d4-a716-446655440003",
        "display_name": "Jane Smith"
      },
      "permission": "read"
    }
  ]
}
```

### Export Address Book (200 OK)
```
Content-Type: text/vcard; charset=utf-8
Content-Disposition: attachment; filename="Work Contacts.vcf"

BEGIN:VCARD
VERSION:3.0
FN:John Doe
N:Doe;John;;;
EMAIL;TYPE=WORK:john.doe@company.com
TEL;TYPE=CELL:+1-555-123-4567
ORG:ACME Corp
END:VCARD
BEGIN:VCARD
VERSION:3.0
FN:Jane Smith
N:Smith;Jane;;;
EMAIL;TYPE=WORK:jane.smith@company.com
END:VCARD
```

### Delete Address Book - Last Address Book (400)
```json
{
  "error": "bad_request",
  "message": "Cannot delete your last address book"
}
```

## Definition of Done

- [ ] Default address book created when user registers
- [ ] `POST /api/v1/addressbooks` creates new address book
- [ ] `GET /api/v1/addressbooks` lists all owned and shared address books
- [ ] `GET /api/v1/addressbooks/{id}` returns single address book
- [ ] `PATCH /api/v1/addressbooks/{id}` updates address book properties
- [ ] `DELETE /api/v1/addressbooks/{id}` deletes with confirmation
- [ ] Cannot delete last address book
- [ ] `GET /api/v1/addressbooks/{id}/export` returns .vcf file
- [ ] Sync token and CTag generated and updated appropriately
- [ ] Unit tests for address book operations
- [ ] Integration tests for CRUD flow
