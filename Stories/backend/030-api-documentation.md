# Story 030: API Documentation

## Title
Implement OpenAPI Documentation and API Reference

## Description
As a developer, I want comprehensive API documentation so that I can integrate with the CalDAV/CardDAV server or build custom clients.

## Acceptance Criteria

### OpenAPI Specification

- [ ] OpenAPI 3.1 specification document
- [ ] All REST API endpoints documented
- [ ] Request/response schemas defined
- [ ] Authentication methods documented
- [ ] Error responses documented
- [ ] Available at `/api/v1/openapi.json` and `/api/v1/openapi.yaml`

### Interactive Documentation

- [ ] Swagger UI available at `/api/docs`
- [ ] Try-it-out functionality (with authentication)
- [ ] Grouped by resource (auth, calendars, contacts, etc.)
- [ ] Examples for all endpoints

### WebDAV/CalDAV/CardDAV Documentation

- [ ] Separate documentation page for DAV protocols
- [ ] Available at `/api/docs/dav`
- [ ] Lists all WebDAV methods (PROPFIND, REPORT, etc.)
- [ ] Example requests and responses
- [ ] XML schemas for common operations

### Code Generation Support

- [ ] OpenAPI spec compatible with code generators
- [ ] TypeScript types can be generated
- [ ] Go client can be generated
- [ ] Server stubs can be generated

## Technical Notes

### OpenAPI Structure
```yaml
openapi: 3.1.0
info:
  title: CalDAV/CardDAV Server API
  version: 1.0.0
  description: |
    REST API for the CalDAV/CardDAV server.

    ## Authentication

    Most endpoints require authentication via JWT Bearer token.
    Obtain a token via the `/api/v1/auth/login` endpoint.

    ## Rate Limiting

    API requests are rate limited. See response headers:
    - `X-RateLimit-Limit`: Maximum requests per window
    - `X-RateLimit-Remaining`: Remaining requests
    - `X-RateLimit-Reset`: Unix timestamp when limit resets

servers:
  - url: https://caldav.example.com
    description: Production server
  - url: http://localhost:8080
    description: Development server

tags:
  - name: Authentication
    description: User authentication and session management
  - name: Users
    description: User profile management
  - name: Calendars
    description: Calendar management
  - name: Events
    description: Calendar event management
  - name: Address Books
    description: Address book management
  - name: Contacts
    description: Contact management
  - name: Sharing
    description: Calendar and address book sharing
  - name: Credentials
    description: CalDAV/CardDAV access credentials
  - name: Setup
    description: Server info and client setup

components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
    basicAuth:
      type: http
      scheme: basic
      description: Used for DAV endpoints with app passwords or credentials

  schemas:
    Error:
      type: object
      properties:
        error:
          type: string
        message:
          type: string
        details:
          type: array
          items:
            type: object
            properties:
              field:
                type: string
              message:
                type: string

    User:
      type: object
      properties:
        id:
          type: string
          format: uuid
        email:
          type: string
          format: email
        username:
          type: string
        display_name:
          type: string
        created_at:
          type: string
          format: date-time

    Calendar:
      type: object
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        description:
          type: string
        color:
          type: string
          pattern: '^#[0-9A-Fa-f]{6}$'
        timezone:
          type: string
        event_count:
          type: integer
        caldav_url:
          type: string
          format: uri

    Event:
      type: object
      properties:
        id:
          type: string
          format: uuid
        calendar_id:
          type: string
          format: uuid
        summary:
          type: string
        description:
          type: string
        location:
          type: string
        start:
          type: string
          format: date-time
        end:
          type: string
          format: date-time
        all_day:
          type: boolean
        recurrence:
          $ref: '#/components/schemas/RecurrenceRule'

    RecurrenceRule:
      type: object
      properties:
        frequency:
          type: string
          enum: [daily, weekly, monthly, yearly]
        interval:
          type: integer
          minimum: 1
        by_day:
          type: array
          items:
            type: string
            enum: [MO, TU, WE, TH, FR, SA, SU]
        until:
          type: string
          format: date
        count:
          type: integer

paths:
  /api/v1/auth/login:
    post:
      tags: [Authentication]
      summary: Login with email and password
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [email, password]
              properties:
                email:
                  type: string
                  format: email
                password:
                  type: string
                  format: password
      responses:
        '200':
          description: Login successful
          content:
            application/json:
              schema:
                type: object
                properties:
                  access_token:
                    type: string
                  refresh_token:
                    type: string
                  expires_in:
                    type: integer
                  user:
                    $ref: '#/components/schemas/User'
        '401':
          description: Invalid credentials
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Error'
        '429':
          description: Too many login attempts

  /api/v1/calendars:
    get:
      tags: [Calendars]
      summary: List all calendars
      security:
        - bearerAuth: []
      responses:
        '200':
          description: List of calendars
          content:
            application/json:
              schema:
                type: object
                properties:
                  calendars:
                    type: array
                    items:
                      $ref: '#/components/schemas/Calendar'
    post:
      tags: [Calendars]
      summary: Create a new calendar
      security:
        - bearerAuth: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name:
                  type: string
                  maxLength: 255
                description:
                  type: string
                  maxLength: 1000
                color:
                  type: string
                  pattern: '^#[0-9A-Fa-f]{6}$'
                timezone:
                  type: string
      responses:
        '201':
          description: Calendar created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Calendar'
```

### Swagger UI Integration
```go
import "github.com/gofiber/swagger"

func SetupDocs(app *fiber.App) {
    // Serve OpenAPI spec
    app.Get("/api/v1/openapi.json", func(c fiber.Ctx) error {
        return c.SendFile("./docs/openapi.json")
    })
    app.Get("/api/v1/openapi.yaml", func(c fiber.Ctx) error {
        return c.SendFile("./docs/openapi.yaml")
    })

    // Swagger UI
    app.Get("/api/docs/*", swagger.New(swagger.Config{
        URL:         "/api/v1/openapi.json",
        DeepLinking: true,
        DocExpansion: "list",
    }))
}
```

### DAV Documentation Page
```go
// Serve static DAV documentation
app.Static("/api/docs/dav", "./docs/dav")

// DAV documentation structure:
// docs/dav/
// ├── index.html
// ├── caldav.md
// ├── carddav.md
// ├── examples/
// │   ├── propfind-calendar.xml
// │   ├── calendar-query.xml
// │   └── sync-collection.xml
```

### Code Structure
```
docs/
├── openapi.yaml            # OpenAPI specification
├── openapi.json            # Generated JSON version
├── schemas/                # Reusable schemas
│   ├── user.yaml
│   ├── calendar.yaml
│   ├── event.yaml
│   └── contact.yaml
└── dav/
    ├── index.html          # DAV documentation landing
    ├── caldav.md           # CalDAV protocol docs
    ├── carddav.md          # CardDAV protocol docs
    └── examples/           # XML examples
```

### Build-time Spec Generation
```go
// Use swag or oapi-codegen for generation
//go:generate swag init -g cmd/server/main.go -o docs/

// Or manual OpenAPI annotations:
// @Summary Create a new calendar
// @Tags Calendars
// @Accept json
// @Produce json
// @Param calendar body CreateCalendarRequest true "Calendar details"
// @Success 201 {object} Calendar
// @Router /api/v1/calendars [post]
func (h *CalendarHandler) Create(c fiber.Ctx) error {
    // ...
}
```

## API Documentation Content

### Authentication Section
- JWT Bearer token authentication
- How to obtain tokens (login, OAuth, SAML)
- Token refresh flow
- Logout and session management

### Error Handling Section
- Standard error response format
- Common error codes
- Validation error details
- Rate limit error handling

### Pagination Section
- Query parameters: `limit`, `offset`
- Response headers: `X-Total-Count`
- Link headers for navigation

### WebDAV Section
- HTTP methods: PROPFIND, PROPPATCH, MKCALENDAR, MKCOL, REPORT
- XML namespaces
- Property names
- Example requests with curl

## API Response Examples in Docs

### Example: Create Event
```bash
curl -X POST https://caldav.example.com/api/v1/calendars/{id}/events \
  -H "Authorization: Bearer eyJ..." \
  -H "Content-Type: application/json" \
  -d '{
    "summary": "Team Meeting",
    "start": "2024-01-22T09:00:00",
    "end": "2024-01-22T10:00:00",
    "timezone": "America/New_York",
    "recurrence": {
      "frequency": "weekly",
      "by_day": ["MO", "WE", "FR"]
    }
  }'
```

### Example: CalDAV PROPFIND
```bash
curl -X PROPFIND https://caldav.example.com/dav/calendars/johndoe/ \
  -H "Authorization: Basic am9obmRvZTpwYXNzd29yZA==" \
  -H "Content-Type: application/xml" \
  -H "Depth: 1" \
  -d '<?xml version="1.0" encoding="UTF-8"?>
<propfind xmlns="DAV:">
  <prop>
    <displayname/>
    <resourcetype/>
    <getctag xmlns="http://calendarserver.org/ns/"/>
  </prop>
</propfind>'
```

## Definition of Done

- [ ] OpenAPI 3.1 spec covers all REST endpoints
- [ ] Spec available at `/api/v1/openapi.json` and `.yaml`
- [ ] Swagger UI available at `/api/docs`
- [ ] All request/response schemas defined
- [ ] Authentication documented
- [ ] Error responses documented
- [ ] DAV documentation available at `/api/docs/dav`
- [ ] Example requests for all endpoints
- [ ] Spec validates against OpenAPI validator
- [ ] TypeScript types can be generated from spec
- [ ] Documentation builds as part of CI
