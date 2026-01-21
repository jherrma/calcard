# Story 024: Address Book Sharing

## Title
Implement Address Book Sharing Between Users

## Description
As a user, I want to share my address books with other users so that we can access common contacts.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| SH-5.2.1 | Users can share address books with other users |
| SH-5.2.2 | Users can grant read-only access to address books |
| SH-5.2.3 | Users can grant read-write access to address books |
| SH-5.2.4 | Users can revoke address book shares |

## Acceptance Criteria

### Create Address Book Share

- [ ] REST endpoint `POST /api/v1/addressbooks/{addressbook_id}/shares` (requires auth)
- [ ] Request body:
  ```json
  {
    "user_identifier": "jane@example.com",
    "permission": "read"
  }
  ```
- [ ] User identifier can be username or email
- [ ] Permission: `read` or `read-write`
- [ ] Cannot share with yourself
- [ ] Cannot share same address book to same user twice
- [ ] Only address book owner can create shares
- [ ] Returns 201 Created with share details

### List Address Book Shares

- [ ] REST endpoint `GET /api/v1/addressbooks/{addressbook_id}/shares` (requires auth)
- [ ] Only address book owner can view shares
- [ ] Returns list of shares:
  - [ ] Share ID
  - [ ] Shared with user (ID, username, display name, email)
  - [ ] Permission level
  - [ ] Created date

### Update Address Book Share

- [ ] REST endpoint `PATCH /api/v1/addressbooks/{addressbook_id}/shares/{share_id}` (requires auth)
- [ ] Request body:
  ```json
  {
    "permission": "read-write"
  }
  ```
- [ ] Only address book owner can update shares
- [ ] Returns updated share

### Revoke Address Book Share

- [ ] REST endpoint `DELETE /api/v1/addressbooks/{addressbook_id}/shares/{share_id}` (requires auth)
- [ ] Only address book owner can revoke shares
- [ ] Returns 204 No Content
- [ ] Shared user immediately loses access

### Recipient: View Shared Address Books

- [ ] `GET /api/v1/addressbooks` includes shared address books
- [ ] Shared address books marked with `shared: true`
- [ ] Include owner information
- [ ] Include permission level
- [ ] Shared address books accessible by ID

### Recipient: Access Shared Address Book

- [ ] Can view contacts (read permission)
- [ ] Can search contacts (read permission)
- [ ] Can create/update/delete contacts (read-write permission)
- [ ] Cannot modify address book properties (name, description)
- [ ] Cannot delete address book
- [ ] Cannot manage shares

### CardDAV Access to Shared Address Books

- [ ] Shared address books appear in PROPFIND on addressbook-home
- [ ] Path: `/dav/addressbooks/{owner-username}/{addressbook-id}/`
- [ ] Shared user can access via their addressbook-home
- [ ] ACL properties reflect actual permissions
- [ ] All standard CardDAV operations work within permission level

## Technical Notes

### Database Model
```go
type AddressBookShare struct {
    ID            uint        `gorm:"primaryKey"`
    UUID          string      `gorm:"uniqueIndex;size:36;not null"`
    AddressBookID uint        `gorm:"index;not null"`
    SharedWithID  uint        `gorm:"index;not null"` // User ID of recipient
    Permission    string      `gorm:"size:20;not null"` // "read" or "read-write"
    CreatedAt     time.Time
    UpdatedAt     time.Time
    AddressBook   AddressBook `gorm:"foreignKey:AddressBookID"`
    SharedWith    User        `gorm:"foreignKey:SharedWithID"`
}

// Unique constraint: (addressbook_id, shared_with_id)
```

### Permission Checking
```go
type AddressBookPermission int

const (
    ABPermissionNone AddressBookPermission = iota
    ABPermissionRead
    ABPermissionReadWrite
    ABPermissionOwner
)

func (r *AddressBookRepo) GetUserPermission(ctx context.Context, addressBookID, userID uint) AddressBookPermission {
    // Check ownership
    var ab AddressBook
    if err := r.db.First(&ab, addressBookID).Error; err != nil {
        return ABPermissionNone
    }
    if ab.UserID == userID {
        return ABPermissionOwner
    }

    // Check share
    var share AddressBookShare
    err := r.db.Where("addressbook_id = ? AND shared_with_id = ?", addressBookID, userID).
        First(&share).Error
    if err != nil {
        return ABPermissionNone
    }

    if share.Permission == "read-write" {
        return ABPermissionReadWrite
    }
    return ABPermissionRead
}
```

### Code Structure
```
internal/domain/sharing/
├── calendar_share.go       # CalendarShare entity (from Story 023)
├── addressbook_share.go    # AddressBookShare entity
└── repository.go           # Repository interface

internal/usecase/sharing/
├── create_addressbook_share.go   # Create share
├── list_addressbook_shares.go    # List shares
├── update_addressbook_share.go   # Update share
└── revoke_addressbook_share.go   # Revoke share

internal/adapter/http/
└── addressbook_share_handler.go  # HTTP handlers
```

### CardDAV Backend Updates
```go
// ListAddressBooks returns owned and shared address books
func (b *CardDAVBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
    user := getUserFromContext(ctx)

    // Get owned address books
    owned, err := b.addressBookRepo.FindByUserID(ctx, user.ID)
    if err != nil {
        return nil, err
    }

    // Get shared address books
    shared, err := b.shareRepo.FindAddressBooksSharedWithUser(ctx, user.ID)
    if err != nil {
        return nil, err
    }

    addressBooks := make([]carddav.AddressBook, 0, len(owned)+len(shared))

    for _, ab := range owned {
        addressBooks = append(addressBooks, toCardDAVAddressBook(ab, ABPermissionOwner))
    }

    for _, share := range shared {
        perm := ABPermissionRead
        if share.Permission == "read-write" {
            perm = ABPermissionReadWrite
        }
        addressBooks = append(addressBooks, toCardDAVAddressBook(share.AddressBook, perm))
    }

    return addressBooks, nil
}
```

## API Response Examples

### Create Share (201 Created)
```json
{
  "id": "bb0e8400-e29b-41d4-a716-446655440001",
  "addressbook_id": "660e8400-e29b-41d4-a716-446655440001",
  "shared_with": {
    "id": "770e8400-e29b-41d4-a716-446655440002",
    "username": "janesmith",
    "display_name": "Jane Smith",
    "email": "jane@example.com"
  },
  "permission": "read",
  "created_at": "2024-01-21T10:00:00Z"
}
```

### List Shares (200 OK)
```json
{
  "shares": [
    {
      "id": "bb0e8400-e29b-41d4-a716-446655440001",
      "shared_with": {
        "id": "770e8400-e29b-41d4-a716-446655440002",
        "username": "janesmith",
        "display_name": "Jane Smith",
        "email": "jane@example.com"
      },
      "permission": "read",
      "created_at": "2024-01-21T10:00:00Z"
    }
  ]
}
```

### List Address Books (includes shared)
```json
{
  "addressbooks": [
    {
      "id": "660e8400-e29b-41d4-a716-446655440000",
      "name": "Contacts",
      "contact_count": 150,
      "shared": false,
      "permission": "owner"
    },
    {
      "id": "660e8400-e29b-41d4-a716-446655440005",
      "name": "Team Contacts",
      "contact_count": 45,
      "shared": true,
      "permission": "read",
      "owner": {
        "id": "880e8400-e29b-41d4-a716-446655440006",
        "username": "teamlead",
        "display_name": "Team Lead"
      }
    }
  ]
}
```

### Permission Denied on Write (403)
```json
{
  "error": "forbidden",
  "message": "You have read-only access to this address book"
}
```

## Search Across Shared Address Books

The contact search endpoint should include contacts from shared address books:

```go
func (uc *SearchContactsUseCase) Execute(ctx context.Context, query string, userID uint) ([]ContactSearchResult, error) {
    // Get all accessible address book IDs
    ownedIDs, err := uc.addressBookRepo.GetIDsByUserID(ctx, userID)
    if err != nil {
        return nil, err
    }

    sharedIDs, err := uc.shareRepo.GetSharedAddressBookIDsForUser(ctx, userID)
    if err != nil {
        return nil, err
    }

    allIDs := append(ownedIDs, sharedIDs...)

    // Search contacts in all accessible address books
    return uc.contactRepo.Search(ctx, query, allIDs)
}
```

## Definition of Done

- [ ] `POST /api/v1/addressbooks/{id}/shares` creates share
- [ ] `GET /api/v1/addressbooks/{id}/shares` lists shares
- [ ] `PATCH /api/v1/addressbooks/{id}/shares/{id}` updates permission
- [ ] `DELETE /api/v1/addressbooks/{id}/shares/{id}` revokes share
- [ ] Shared address books appear in recipient's address book list
- [ ] Read permission allows viewing and searching only
- [ ] Read-write permission allows contact modifications
- [ ] Shared address books accessible via CardDAV
- [ ] WebDAV ACL properties reflect actual permissions
- [ ] Contact search includes shared address books
- [ ] Contact changes sync to all shared users
- [ ] Unit tests for permission checking
- [ ] Integration tests for sharing flow
