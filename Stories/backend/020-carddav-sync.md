# Story 020: WebDAV-Sync for Address Books

## Title
Implement WebDAV-Sync Protocol (RFC 6578) for Address Books

## Description
As a DAV client user, I want efficient contact synchronization using sync-tokens so that my client only downloads changes since the last sync.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AD-4.4.1 | Server provides sync-token for address books |
| AD-4.4.2 | REPORT sync-collection returns contact changes |

## Acceptance Criteria

### Sync Token in Properties

- [ ] `PROPFIND /dav/addressbooks/{user}/{ab}/` returns:
  - [ ] `sync-token` property with current sync token
  - [ ] Format: `https://caldav.example.com/sync/{token-value}`
- [ ] Sync token changes on any modification to address book contents
- [ ] Sync token is address-book-specific (not global)

### Sync REPORT - Initial Sync

- [ ] `REPORT sync-collection` without sync-token:
  ```xml
  <sync-collection xmlns="DAV:">
    <sync-token/>
    <sync-level>1</sync-level>
    <prop>
      <getetag/>
      <address-data xmlns="urn:ietf:params:xml:ns:carddav"/>
    </prop>
  </sync-collection>
  ```
- [ ] Returns all contacts in address book
- [ ] Returns new sync-token for subsequent syncs
- [ ] Each contact includes requested properties

### Sync REPORT - Incremental Sync

- [ ] `REPORT sync-collection` with sync-token:
  ```xml
  <sync-collection xmlns="DAV:">
    <sync-token>https://caldav.example.com/sync/abc123</sync-token>
    <sync-level>1</sync-level>
    <prop>
      <getetag/>
      <address-data xmlns="urn:ietf:params:xml:ns:carddav"/>
    </prop>
  </sync-collection>
  ```
- [ ] Returns only changes since provided token:
  - [ ] Created contacts: Full vCard data with 200 status
  - [ ] Modified contacts: Full vCard data with 200 status
  - [ ] Deleted contacts: href only with 404 status
- [ ] Returns new sync-token in response

### Sync Token Validation

- [ ] Invalid sync-token returns 403 Forbidden with:
  ```xml
  <error xmlns="DAV:">
    <valid-sync-token/>
  </error>
  ```
- [ ] Client should perform full sync on 403 error
- [ ] Old but valid tokens should work (return all changes since that point)

### Change Tracking

- [ ] Track changes in database for sync queries:
  - [ ] Contact created: Record creation with sync-token
  - [ ] Contact modified: Record modification with new sync-token
  - [ ] Contact deleted: Record deletion with sync-token
- [ ] Reuse SyncChangeLog table from Story 015 with collection_type = "addressbook"
- [ ] Support configurable retention of change history

## Technical Notes

### Database Model Extension
```go
// Extend SyncChangeLog from Story 015 to support address books
type SyncChangeLog struct {
    ID             uint      `gorm:"primaryKey"`
    CollectionID   uint      `gorm:"index;not null"`       // Calendar or AddressBook ID
    CollectionType string    `gorm:"size:20;index;not null"` // "calendar" or "addressbook"
    ResourcePath   string    `gorm:"size:255;not null"`    // Event or Contact path
    ResourceUID    string    `gorm:"size:255"`             // iCal/vCard UID
    ChangeType     string    `gorm:"size:20;not null"`     // created, modified, deleted
    SyncToken      string    `gorm:"index;size:64;not null"`
    CreatedAt      time.Time `gorm:"index"`
}

// Composite index for efficient queries
// CREATE INDEX idx_sync_changelog_collection ON sync_change_log(collection_type, collection_id, sync_token);
```

### Change Recording for Address Books
```go
// On contact creation
func (r *AddressBookRepo) CreateContact(ctx context.Context, contact *AddressObject) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        // Create contact
        if err := tx.Create(contact).Error; err != nil {
            return err
        }

        // Update address book sync token
        newToken := generateSyncToken()
        if err := tx.Model(&AddressBook{}).
            Where("id = ?", contact.AddressBookID).
            Updates(map[string]interface{}{
                "sync_token": newToken,
                "ctag":       newToken,
            }).Error; err != nil {
            return err
        }

        // Record change
        return tx.Create(&SyncChangeLog{
            CollectionID:   contact.AddressBookID,
            CollectionType: "addressbook",
            ResourcePath:   contact.Path,
            ResourceUID:    contact.UID,
            ChangeType:     "created",
            SyncToken:      newToken,
        }).Error
    })
}
```

### Code Structure
```
internal/adapter/webdav/
├── sync.go                  # Shared sync implementation (extended)
├── sync_addressbook.go      # Address book specific sync logic
└── sync_report.go           # REPORT sync-collection handler (extended)

internal/adapter/repository/
└── sync_changelog_repo.go   # Extended for address books
```

## Response Examples

### Initial Sync Response (207 Multi-Status)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <response>
    <href>/dav/addressbooks/johndoe/contacts/contact1.vcf</href>
    <propstat>
      <prop>
        <getetag>"v1-abc123"</getetag>
        <CR:address-data>BEGIN:VCARD
VERSION:3.0
UID:contact1@caldav.example.com
FN:John Doe
EMAIL:john@example.com
END:VCARD</CR:address-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <response>
    <href>/dav/addressbooks/johndoe/contacts/contact2.vcf</href>
    <propstat>
      <prop>
        <getetag>"v1-def456"</getetag>
        <CR:address-data>BEGIN:VCARD
VERSION:3.0
UID:contact2@caldav.example.com
FN:Jane Smith
EMAIL:jane@example.com
END:VCARD</CR:address-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <sync-token>https://caldav.example.com/sync/1705833600-abc123</sync-token>
</multistatus>
```

### Incremental Sync Response (207 Multi-Status)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <!-- New contact -->
  <response>
    <href>/dav/addressbooks/johndoe/contacts/new-contact.vcf</href>
    <propstat>
      <prop>
        <getetag>"v1-new123"</getetag>
        <CR:address-data>BEGIN:VCARD...END:VCARD</CR:address-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <!-- Modified contact -->
  <response>
    <href>/dav/addressbooks/johndoe/contacts/contact1.vcf</href>
    <propstat>
      <prop>
        <getetag>"v2-abc123"</getetag>
        <CR:address-data>BEGIN:VCARD...END:VCARD</CR:address-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <!-- Deleted contact -->
  <response>
    <href>/dav/addressbooks/johndoe/contacts/deleted-contact.vcf</href>
    <status>HTTP/1.1 404 Not Found</status>
  </response>
  <sync-token>https://caldav.example.com/sync/1705920000-def456</sync-token>
</multistatus>
```

### Invalid Sync Token (403 Forbidden)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<error xmlns="DAV:">
  <valid-sync-token/>
</error>
```

### No Changes (207 Multi-Status)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:">
  <sync-token>https://caldav.example.com/sync/1705833600-abc123</sync-token>
</multistatus>
```

## Unified Sync Implementation

Since calendars and address books share the same sync mechanism, consolidate:

```go
// Generic sync handler for both CalDAV and CardDAV
type SyncHandler struct {
    changeLogRepo *SyncChangeLogRepository
}

func (h *SyncHandler) HandleSyncReport(
    ctx context.Context,
    collectionType string,       // "calendar" or "addressbook"
    collectionID uint,
    clientToken string,          // Empty for initial sync
    requestedProps []string,
) (*SyncResponse, error) {
    if clientToken == "" {
        // Initial sync: return all resources
        return h.initialSync(ctx, collectionType, collectionID, requestedProps)
    }

    // Validate token
    if !h.changeLogRepo.IsValidToken(ctx, collectionType, collectionID, clientToken) {
        return nil, ErrInvalidSyncToken
    }

    // Incremental sync: return changes since token
    return h.incrementalSync(ctx, collectionType, collectionID, clientToken, requestedProps)
}
```

## Definition of Done

- [ ] PROPFIND returns sync-token property for address books
- [ ] Sync-token updates on address book modifications
- [ ] REPORT sync-collection without token returns all contacts
- [ ] REPORT sync-collection with token returns only changes
- [ ] Created contacts appear in sync response
- [ ] Modified contacts appear in sync response
- [ ] Deleted contacts appear as 404 in sync response
- [ ] Invalid sync-token returns 403 with valid-sync-token error
- [ ] Change history recorded for all contact operations
- [ ] Sync implementation shared between CalDAV and CardDAV
- [ ] DAVx5 incremental contact sync works correctly
- [ ] Apple Contacts incremental sync works correctly
- [ ] Unit tests for address book sync
- [ ] Integration tests for sync scenarios
