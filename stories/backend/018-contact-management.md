# Story 018: Contact Management REST API

## Title
Implement Contact CRUD Operations via REST API

## Description
As a web UI user, I want to create, view, update, delete, and search contacts so that I can manage my address books through the browser.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AD-4.2.1 | Users can view list of contacts |
| AD-4.2.2 | Users can search contacts by name |
| AD-4.2.3 | Users can search contacts by email |
| AD-4.2.4 | Users can search contacts by phone |
| AD-4.2.5 | Users can create contacts with name |
| AD-4.2.6 | Users can add multiple email addresses to contact |
| AD-4.2.7 | Users can add multiple phone numbers with types |
| AD-4.2.8 | Users can add postal addresses |
| AD-4.2.9 | Users can add organization/company |
| AD-4.2.10 | Users can add notes to contacts |
| AD-4.2.11 | Users can edit existing contacts |
| AD-4.2.12 | Users can delete contacts |
| AD-4.2.13 | Users can add contact photo |

## Acceptance Criteria

### List Contacts

- [ ] REST endpoint `GET /api/v1/addressbooks/{addressbook_id}/contacts` (requires auth)
- [ ] Query parameters:
  - [ ] `limit` (optional): Page size, default 50, max 200
  - [ ] `offset` (optional): Pagination offset
  - [ ] `sort` (optional): Sort field (name, email, updated_at), default name
  - [ ] `order` (optional): asc or desc, default asc
- [ ] Returns paginated list of contacts
- [ ] Each contact includes summary fields (not full vCard)

### Search Contacts

- [ ] REST endpoint `GET /api/v1/contacts/search` (requires auth)
- [ ] Query parameters:
  - [ ] `q` (required): Search query string
  - [ ] `addressbook_id` (optional): Limit to specific address book
  - [ ] `limit` (optional): Result limit, default 20
- [ ] Searches across:
  - [ ] Formatted name
  - [ ] Given name / Family name
  - [ ] Email addresses
  - [ ] Phone numbers
  - [ ] Organization
- [ ] Returns contacts from all accessible address books (owned + shared)
- [ ] Highlights matching fields in response

### Get Single Contact

- [ ] REST endpoint `GET /api/v1/addressbooks/{addressbook_id}/contacts/{contact_id}` (requires auth)
- [ ] Returns full contact details
- [ ] Includes all emails, phones, addresses
- [ ] Includes photo URL if available
- [ ] Returns 404 if not found

### Create Contact

- [ ] REST endpoint `POST /api/v1/addressbooks/{addressbook_id}/contacts` (requires auth)
- [ ] Request body:
  ```json
  {
    "prefix": "Dr.",
    "given_name": "John",
    "middle_name": "William",
    "family_name": "Doe",
    "suffix": "Jr.",
    "nickname": "Johnny",
    "emails": [
      {"type": "work", "value": "john.doe@company.com", "primary": true},
      {"type": "home", "value": "john@personal.com"}
    ],
    "phones": [
      {"type": "cell", "value": "+1-555-123-4567", "primary": true},
      {"type": "work", "value": "+1-555-987-6543"}
    ],
    "addresses": [
      {
        "type": "work",
        "street": "123 Business Ave",
        "city": "San Francisco",
        "state": "CA",
        "postal_code": "94102",
        "country": "USA"
      }
    ],
    "organization": "ACME Corp",
    "title": "Software Engineer",
    "birthday": "1990-05-15",
    "notes": "Met at conference 2023",
    "urls": [
      {"type": "work", "value": "https://company.com/john"}
    ]
  }
  ```
- [ ] At least one name field required (given_name, family_name, or organization)
- [ ] Generates vCard UID
- [ ] Stores as vCard 3.0 format internally
- [ ] Updates address book CTag and sync-token
- [ ] Returns 201 Created with contact data

### Update Contact

- [ ] REST endpoint `PATCH /api/v1/addressbooks/{addressbook_id}/contacts/{contact_id}` (requires auth)
- [ ] All fields optional (partial update)
- [ ] Arrays (emails, phones, addresses) replace existing values
- [ ] Updates address book CTag and sync-token
- [ ] Returns updated contact

### Delete Contact

- [ ] REST endpoint `DELETE /api/v1/addressbooks/{addressbook_id}/contacts/{contact_id}` (requires auth)
- [ ] Updates address book CTag and sync-token
- [ ] Returns 204 No Content

### Upload Contact Photo

- [ ] REST endpoint `PUT /api/v1/addressbooks/{addressbook_id}/contacts/{contact_id}/photo` (requires auth)
- [ ] Content-Type: `image/jpeg`, `image/png`, or `image/gif`
- [ ] Max size: 1MB
- [ ] Photo stored as base64 in vCard PHOTO property
- [ ] Returns 204 No Content

### Delete Contact Photo

- [ ] REST endpoint `DELETE /api/v1/addressbooks/{addressbook_id}/contacts/{contact_id}/photo` (requires auth)
- [ ] Removes PHOTO property from vCard
- [ ] Returns 204 No Content

### Move Contact

- [ ] REST endpoint `POST /api/v1/addressbooks/{addressbook_id}/contacts/{contact_id}/move` (requires auth)
- [ ] Request body:
  ```json
  {
    "target_addressbook_id": "uuid-of-target-addressbook"
  }
  ```
- [ ] Moves contact to different address book
- [ ] Updates both address books' CTag and sync-token
- [ ] Returns updated contact

## Technical Notes

### vCard Generation
```go
func contactToVCard(contact *Contact) string {
    var b strings.Builder
    b.WriteString("BEGIN:VCARD\r\n")
    b.WriteString("VERSION:3.0\r\n")
    b.WriteString(fmt.Sprintf("UID:%s\r\n", contact.UID))

    // N: Family;Given;Middle;Prefix;Suffix
    b.WriteString(fmt.Sprintf("N:%s;%s;%s;%s;%s\r\n",
        contact.FamilyName, contact.GivenName, contact.MiddleName,
        contact.Prefix, contact.Suffix))

    // FN: Formatted Name
    b.WriteString(fmt.Sprintf("FN:%s\r\n", contact.FormattedName))

    // Emails
    for _, email := range contact.Emails {
        b.WriteString(fmt.Sprintf("EMAIL;TYPE=%s:%s\r\n",
            strings.ToUpper(email.Type), email.Value))
    }

    // Phones
    for _, phone := range contact.Phones {
        b.WriteString(fmt.Sprintf("TEL;TYPE=%s:%s\r\n",
            strings.ToUpper(phone.Type), phone.Value))
    }

    // Addresses
    for _, addr := range contact.Addresses {
        // ADR: PO Box;Extended;Street;City;State;Postal;Country
        b.WriteString(fmt.Sprintf("ADR;TYPE=%s:;;%s;%s;%s;%s;%s\r\n",
            strings.ToUpper(addr.Type), addr.Street, addr.City,
            addr.State, addr.PostalCode, addr.Country))
    }

    if contact.Organization != "" {
        b.WriteString(fmt.Sprintf("ORG:%s\r\n", contact.Organization))
    }
    if contact.Title != "" {
        b.WriteString(fmt.Sprintf("TITLE:%s\r\n", contact.Title))
    }
    if contact.Birthday != "" {
        b.WriteString(fmt.Sprintf("BDAY:%s\r\n", contact.Birthday))
    }
    if contact.Notes != "" {
        b.WriteString(fmt.Sprintf("NOTE:%s\r\n", escapeVCardText(contact.Notes)))
    }
    if contact.Photo != "" {
        b.WriteString(fmt.Sprintf("PHOTO;ENCODING=b;TYPE=JPEG:%s\r\n", contact.Photo))
    }

    b.WriteString(fmt.Sprintf("REV:%s\r\n", time.Now().Format("20060102T150405Z")))
    b.WriteString("END:VCARD\r\n")
    return b.String()
}
```

### vCard Parsing
```go
func parseVCard(data string) (*Contact, error) {
    // Use go-vcard library for parsing
    dec := vcard.NewDecoder(strings.NewReader(data))
    card, err := dec.Decode()
    if err != nil {
        return nil, err
    }

    contact := &Contact{
        UID:           card.Value(vcard.FieldUID),
        FormattedName: card.PreferredValue(vcard.FieldFormattedName),
    }

    // Parse N field
    if names := card.Name(); names != nil {
        contact.FamilyName = names.FamilyName
        contact.GivenName = names.GivenName
        contact.MiddleName = strings.Join(names.AdditionalName, " ")
        contact.Prefix = strings.Join(names.HonorificPrefix, " ")
        contact.Suffix = strings.Join(names.HonorificSuffix, " ")
    }

    // Parse emails, phones, addresses...
    return contact, nil
}
```

### Code Structure
```
internal/usecase/contact/
├── list.go              # List contacts
├── search.go            # Search contacts
├── get.go               # Get single contact
├── create.go            # Create contact
├── update.go            # Update contact
├── delete.go            # Delete contact
├── move.go              # Move contact
└── photo.go             # Photo upload/delete

internal/adapter/http/
└── contact_handler.go   # HTTP handlers

internal/domain/addressbook/
└── contact.go           # Contact domain types
```

## API Response Examples

### List Contacts (200 OK)
```json
{
  "contacts": [
    {
      "id": "770e8400-e29b-41d4-a716-446655440010",
      "formatted_name": "John Doe",
      "primary_email": "john.doe@company.com",
      "primary_phone": "+1-555-123-4567",
      "organization": "ACME Corp",
      "has_photo": true,
      "updated_at": "2024-01-20T14:00:00Z"
    },
    {
      "id": "770e8400-e29b-41d4-a716-446655440011",
      "formatted_name": "Jane Smith",
      "primary_email": "jane@example.com",
      "primary_phone": null,
      "organization": null,
      "has_photo": false,
      "updated_at": "2024-01-19T10:00:00Z"
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

### Search Contacts (200 OK)
```json
{
  "results": [
    {
      "id": "770e8400-e29b-41d4-a716-446655440010",
      "addressbook_id": "660e8400-e29b-41d4-a716-446655440000",
      "addressbook_name": "Contacts",
      "formatted_name": "John Doe",
      "primary_email": "john.doe@company.com",
      "match_field": "email",
      "match_highlight": "john.<mark>doe</mark>@company.com"
    }
  ],
  "query": "doe",
  "count": 1
}
```

### Get Contact (200 OK)
```json
{
  "id": "770e8400-e29b-41d4-a716-446655440010",
  "addressbook_id": "660e8400-e29b-41d4-a716-446655440000",
  "uid": "770e8400-e29b-41d4-a716-446655440010@caldav.example.com",
  "prefix": "Dr.",
  "given_name": "John",
  "middle_name": "William",
  "family_name": "Doe",
  "suffix": "Jr.",
  "formatted_name": "Dr. John William Doe Jr.",
  "nickname": "Johnny",
  "emails": [
    {"type": "work", "value": "john.doe@company.com", "primary": true},
    {"type": "home", "value": "john@personal.com", "primary": false}
  ],
  "phones": [
    {"type": "cell", "value": "+1-555-123-4567", "primary": true},
    {"type": "work", "value": "+1-555-987-6543", "primary": false}
  ],
  "addresses": [
    {
      "type": "work",
      "street": "123 Business Ave",
      "city": "San Francisco",
      "state": "CA",
      "postal_code": "94102",
      "country": "USA"
    }
  ],
  "organization": "ACME Corp",
  "title": "Software Engineer",
  "birthday": "1990-05-15",
  "notes": "Met at conference 2023",
  "urls": [
    {"type": "work", "value": "https://company.com/john"}
  ],
  "photo_url": "/api/v1/addressbooks/660e.../contacts/770e.../photo",
  "created_at": "2024-01-15T10:00:00Z",
  "updated_at": "2024-01-20T14:00:00Z"
}
```

### Create Contact - Validation Error (400)
```json
{
  "error": "validation_error",
  "message": "Validation failed",
  "details": [
    {"field": "given_name", "message": "At least one name field is required (given_name, family_name, or organization)"}
  ]
}
```

## Definition of Done

- [ ] `GET /api/v1/addressbooks/{id}/contacts` returns paginated contacts
- [ ] `GET /api/v1/contacts/search` searches across all accessible address books
- [ ] Search works for name, email, phone, organization
- [ ] `POST /api/v1/addressbooks/{id}/contacts` creates contact
- [ ] `PATCH /api/v1/addressbooks/{id}/contacts/{id}` updates contact
- [ ] `DELETE /api/v1/addressbooks/{id}/contacts/{id}` deletes contact
- [ ] `PUT /api/v1/.../contacts/{id}/photo` uploads photo
- [ ] Contact changes update address book CTag and sync-token
- [ ] vCard data correctly generated and stored
- [ ] Unit tests for vCard generation/parsing
- [ ] Integration tests for CRUD and search operations
