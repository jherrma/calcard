# Story 019: CardDAV Protocol Implementation

## Title

Implement CardDAV Protocol Endpoints

## Description

As a DAV client user, I want to access my contacts via CardDAV protocol so that I can sync contacts with applications like DAVx5, Apple Contacts, and Thunderbird.

## Related Acceptance Criteria

| ID        | Criterion                                             |
| --------- | ----------------------------------------------------- |
| AD-4.3.1  | Server responds to OPTIONS with CardDAV headers       |
| AD-4.3.2  | /.well-known/carddav redirects to DAV root            |
| AD-4.3.3  | PROPFIND on principal returns addressbook-home-set    |
| AD-4.3.4  | PROPFIND on addressbook-home lists all address books  |
| AD-4.3.5  | PUT creates new contact in address book               |
| AD-4.3.6  | PUT updates existing contact (with correct ETag)      |
| AD-4.3.7  | GET retrieves contact vCard data                      |
| AD-4.3.8  | DELETE removes contact                                |
| AD-4.3.9  | REPORT addressbook-query returns filtered contacts    |
| AD-4.3.10 | REPORT addressbook-multiget returns specific contacts |
| AD-4.3.11 | Server supports vCard 3.0 format                      |
| AD-4.3.12 | Server supports vCard 4.0 format                      |

## Acceptance Criteria

### Service Discovery

- [ ] `GET /.well-known/carddav` returns 301 redirect to `/dav/`
- [ ] `OPTIONS /dav/` returns DAV capabilities:
  - [ ] Header: `DAV: 1, 2, 3, addressbook`
  - [ ] Header: `Allow: OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL, REPORT`

### Principal Discovery

- [ ] `PROPFIND /dav/principals/{username}/` returns:
  - [ ] `addressbook-home-set` -> `/dav/addressbooks/{username}/`
  - [ ] (Already implemented in Story 014 for CalDAV)

### Address Book Home

- [ ] `PROPFIND /dav/addressbooks/{username}/` (Depth: 0) returns:
  - [ ] `resourcetype` (collection)
  - [ ] `displayname`
  - [ ] `current-user-privilege-set`
- [ ] `PROPFIND /dav/addressbooks/{username}/` (Depth: 1) returns:
  - [ ] List of all address books
  - [ ] Each address book's properties

### Address Book Collection Properties

- [ ] `PROPFIND /dav/addressbooks/{username}/{addressbook-id}/` returns:
  - [ ] `resourcetype` (collection, addressbook)
  - [ ] `displayname`
  - [ ] `addressbook-description`
  - [ ] `supported-address-data` (vCard 3.0 and 4.0)
  - [ ] `getctag`
  - [ ] `sync-token`
  - [ ] `max-resource-size` (optional, e.g., 102400 bytes)

### Address Book Operations

- [ ] `MKCOL /dav/addressbooks/{username}/{new-addressbook}/`
  - [ ] Creates new address book
  - [ ] Request body can include properties (displayname)
  - [ ] Returns 201 Created
- [ ] `DELETE /dav/addressbooks/{username}/{addressbook-id}/`
  - [ ] Deletes address book and all contacts
  - [ ] Returns 204 No Content
- [ ] `PROPPATCH /dav/addressbooks/{username}/{addressbook-id}/`
  - [ ] Updates address book properties
  - [ ] Returns 207 Multi-Status

### Contact Operations

- [ ] `PUT /dav/addressbooks/{username}/{addressbook-id}/{contact-uid}.vcf`
  - [ ] Creates new contact if not exists
  - [ ] Updates existing contact with `If-Match: {etag}` header
  - [ ] Accepts both vCard 3.0 and 4.0 formats
  - [ ] Returns 201 Created (new) or 204 No Content (update)
  - [ ] Returns `ETag` header
  - [ ] Updates address book CTag and sync-token
- [ ] `GET /dav/addressbooks/{username}/{addressbook-id}/{contact-uid}.vcf`
  - [ ] Returns vCard data
  - [ ] Returns `ETag` header
  - [ ] Content-Type: `text/vcard; charset=utf-8`
- [ ] `DELETE /dav/addressbooks/{username}/{addressbook-id}/{contact-uid}.vcf`
  - [ ] Deletes contact
  - [ ] Updates address book CTag and sync-token
  - [ ] Returns 204 No Content
- [ ] ETag validation (same as CalDAV):
  - [ ] `If-Match: *` allows any existing resource
  - [ ] `If-Match: "etag"` requires exact match
  - [ ] `If-None-Match: *` only allows creating new
  - [ ] Returns 412 Precondition Failed on mismatch

### REPORT Queries

- [ ] `REPORT addressbook-query`:

  ```xml
  <addressbook-query xmlns="urn:ietf:params:xml:ns:carddav">
    <prop>
      <getetag xmlns="DAV:"/>
      <address-data/>
    </prop>
    <filter>
      <prop-filter name="EMAIL">
        <text-match collation="i;unicode-casemap" match-type="contains">
          example.com
        </text-match>
      </prop-filter>
    </filter>
  </addressbook-query>
  ```

  - [ ] Returns contacts matching filter
  - [ ] Supports prop-filter on any vCard property
  - [ ] Supports text-match with collation and match-type
  - [ ] Match types: equals, contains, starts-with, ends-with

- [ ] `REPORT addressbook-multiget`:

  ```xml
  <addressbook-multiget xmlns="urn:ietf:params:xml:ns:carddav">
    <prop>
      <getetag xmlns="DAV:"/>
      <address-data/>
    </prop>
    <href>/dav/addressbooks/user/contacts/contact1.vcf</href>
    <href>/dav/addressbooks/user/contacts/contact2.vcf</href>
  </addressbook-multiget>
  ```

  - [ ] Returns requested contacts by URL
  - [ ] Non-existent URLs return 404 in multistatus

### vCard Version Support

- [ ] Accept vCard 3.0 on PUT
- [ ] Accept vCard 4.0 on PUT
- [ ] Store vCard in original format
- [ ] Return vCard in requested format (via address-data content-type)
- [ ] Default to vCard 3.0 if not specified

## Technical Notes

### Dependencies

```go
github.com/emersion/go-webdav  // CardDAV protocol
github.com/emersion/go-vcard   // vCard parsing
```

### CardDAV Backend Interface

```go
// Implement carddav.Backend from go-webdav
type CardDAVBackend struct {
    addressBookRepo addressbook.Repository
    userRepo        user.Repository
}

func (b *CardDAVBackend) AddressBookHomeSetPath(ctx context.Context) (string, error)
func (b *CardDAVBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error)
func (b *CardDAVBackend) GetAddressBook(ctx context.Context, path string) (*carddav.AddressBook, error)
func (b *CardDAVBackend) CreateAddressBook(ctx context.Context, ab *carddav.AddressBook) error
func (b *CardDAVBackend) DeleteAddressBook(ctx context.Context, path string) error
func (b *CardDAVBackend) GetAddressObject(ctx context.Context, path string) (*carddav.AddressObject, error)
func (b *CardDAVBackend) ListAddressObjects(ctx context.Context, path string, req *carddav.AddressBookQuery) ([]carddav.AddressObject, error)
func (b *CardDAVBackend) PutAddressObject(ctx context.Context, path string, card vcard.Card, opts *carddav.PutAddressObjectOptions) (string, error)
func (b *CardDAVBackend) DeleteAddressObject(ctx context.Context, path string) error
```

### URL Structure

```
/dav/                                         # DAV root
/dav/principals/{username}/                   # User principal
/dav/addressbooks/{username}/                 # Address book home
/dav/addressbooks/{username}/{ab-uuid}/       # Address book collection
/dav/addressbooks/{username}/{ab-uuid}/{contact-uid}.vcf  # Contact resource
```

### vCard Validation

```go
func validateVCard(data []byte) (*vcard.Card, error) {
    dec := vcard.NewDecoder(bytes.NewReader(data))
    card, err := dec.Decode()
    if err != nil {
        return nil, fmt.Errorf("invalid vCard: %w", err)
    }

    // Must have UID
    if card.Value(vcard.FieldUID) == "" {
        return nil, errors.New("vCard missing required UID property")
    }

    // Must have FN (formatted name) or N (structured name)
    if card.PreferredValue(vcard.FieldFormattedName) == "" && card.Name() == nil {
        return nil, errors.New("vCard missing required name property (FN or N)")
    }

    return card, nil
}
```

### Code Structure

```
internal/adapter/webdav/
├── carddav_backend.go    # Implements carddav.Backend
├── carddav_handler.go    # HTTP handler setup
├── carddav_props.go      # CardDAV-specific properties
└── carddav_reports.go    # REPORT method handling
```

## Response Examples

### OPTIONS Response

```http
HTTP/1.1 200 OK
DAV: 1, 2, 3, addressbook
Allow: OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL, REPORT
```

### PROPFIND Address Book Response (207 Multi-Status)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <response>
    <href>/dav/addressbooks/johndoe/660e8400-e29b-41d4-a716-446655440000/</href>
    <propstat>
      <prop>
        <resourcetype>
          <collection/>
          <CR:addressbook/>
        </resourcetype>
        <displayname>Contacts</displayname>
        <CR:addressbook-description>Personal contacts</CR:addressbook-description>
        <CR:supported-address-data>
          <CR:address-data-type content-type="text/vcard" version="3.0"/>
          <CR:address-data-type content-type="text/vcard" version="4.0"/>
        </CR:supported-address-data>
        <getctag>1705833600-abc123</getctag>
        <sync-token>https://caldav.example.com/sync/1705833600-abc123</sync-token>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
</multistatus>
```

### PUT Contact (201 Created)

```http
HTTP/1.1 201 Created
ETag: "v1-a1b2c3d4"
Location: /dav/addressbooks/johndoe/contacts/contact-123.vcf
```

### addressbook-query Response

```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <response>
    <href>/dav/addressbooks/johndoe/contacts/contact-123.vcf</href>
    <propstat>
      <prop>
        <getetag>"v1-a1b2c3d4"</getetag>
        <CR:address-data>BEGIN:VCARD
VERSION:3.0
UID:contact-123@caldav.example.com
FN:John Doe
EMAIL;TYPE=WORK:john.doe@example.com
END:VCARD</CR:address-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
</multistatus>
```

### addressbook-multiget Response

```xml
<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <response>
    <href>/dav/addressbooks/johndoe/contacts/contact-123.vcf</href>
    <propstat>
      <prop>
        <getetag>"v1-a1b2c3d4"</getetag>
        <CR:address-data>BEGIN:VCARD...END:VCARD</CR:address-data>
      </prop>
      <status>HTTP/1.1 200 OK</status>
    </propstat>
  </response>
  <response>
    <href>/dav/addressbooks/johndoe/contacts/nonexistent.vcf</href>
    <status>HTTP/1.1 404 Not Found</status>
  </response>
</multistatus>
```

## Definition of Done

- [ ] `/.well-known/carddav` redirects to DAV root
- [ ] OPTIONS returns correct CardDAV headers
- [ ] PROPFIND on principal returns addressbook-home-set
- [ ] PROPFIND on addressbook-home lists address books
- [ ] MKCOL creates address books
- [ ] PUT creates/updates contacts with ETag validation
- [ ] GET retrieves contacts
- [ ] DELETE removes contacts
- [ ] REPORT addressbook-query returns filtered contacts
- [ ] REPORT addressbook-multiget returns specific contacts
- [ ] vCard 3.0 format supported
- [ ] vCard 4.0 format supported
- [ ] ETags update on modification
- [ ] CTag updates when address book changes
- [ ] DAVx5 can sync contacts
- [ ] Apple Contacts can sync contacts
- [ ] Integration tests for all CardDAV operations
