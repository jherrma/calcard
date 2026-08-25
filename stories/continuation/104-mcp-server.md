# Story 104: MCP Server Integration

## Title

Model Context Protocol (MCP) Server for AI-Assisted Calendar and Contact Management

## Description

As a user, I want CalCard to expose an MCP server so that AI assistants (Claude, etc.) can discover and interact with my calendars and contacts through a standardized protocol. This enables natural language calendar management, contact lookups, and scheduling workflows from any MCP-compatible client.

## Implementation status (implemented 2026-08-25)

**Done.** `internal/adapter/mcp/` holds the JSON-RPC dispatcher, the Streamable HTTP transport, 13
tools and 4 resources; `internal/usecase/mcptoken/` holds the credential. Wired in `routes.go`
behind `cfg.MCP.Enabled` (default true).

Everything in "Acceptance Criteria" and "Definition of Done" below is met, with these deliberate
departures from the sketch — read them before "fixing" the code to match the prose above:

- **Authentication is a dedicated MCP access token**, not (only) the JWT the story assumed. An
  access token lives 10 minutes, which is useless for a client configured once with a static
  `Authorization` header; app passwords were rejected too, because they are scoped to CalDAV/CardDAV
  and widening them would silently grant every existing DAV credential a full read/write tool
  surface. `user.MCPToken` stores a unique-indexed SHA-256 of a 256-bit random secret (not bcrypt:
  the client presents only the opaque string, so it must be findable from that alone). JWTs are
  still accepted, as specified. Minted and revoked at `/api/v1/mcp-tokens`, UI at
  **Settings → MCP Access**.
- **Protocol version is `2025-06-18`**, not the story's `2025-03-26`; `2025-03-26` and `2024-11-05`
  are still accepted, and an unrecognised version is answered with ours rather than refused, so a
  client newer than this code can still connect.
- **Tools answer with JSON, not the prose the examples show.** "Event 'Team Standup' created
  successfully" leaves the model with no id to update or delete it by, forcing a re-list after every
  write. JSON keeps ids, permissions and timestamps addressable.
- **Session state lives in `adapter/mcp/session.go`, not `usecase/mcp/session.go`.** An
  `Mcp-Session-Id` is transport state; putting it in the use-case layer would put an HTTP concern
  inside the layer that must not know about HTTP.
- **`GET /mcp` serves both** the manifest the story asks for and the SSE notification stream, chosen
  by `Accept: text/event-stream`. The stream is real but carries only keep-alives — there is no
  change feed yet — and is bounded by the server's write timeout, since fasthttp applies that to the
  whole streamed response. `resources` therefore does not advertise `subscribe`.
- **`find_free_slots` ignores all-day events by default** (`include_all_day` opts in). Birthdays and
  holidays would otherwise wipe out entire days, which is the opposite of useful.
- **No Go MCP SDK was added.** The protocol subset is small and the SDKs assume `net/http`, which
  would need an adapter into Fiber v3 for no gain.

The tool surface is a facade: every tool calls the same use case the REST handler calls and resolves
permissions through the same `GetUserPermission` methods. That is the invariant to preserve — an MCP
tool that queries or authorizes on its own is a way around the sharing model.

Tests: `internal/adapter/mcp/*_test.go` (tools, protocol, resources, transport, sessions),
`internal/usecase/mcptoken/token_test.go`, `internal/adapter/http/mcp_token_handler_test.go`,
`integration/mcp_test.go` (full lifecycle over a real socket), `webinterface/pages/settings/mcp.spec.ts`.

## Acceptance Criteria

### MCP Server Endpoint

- [ ] New API group `/mcp` mounted on the main Fiber app
- [ ] `GET /mcp` - Server manifest (capabilities, tools, resources)
- [ ] MCP transport: Streamable HTTP (POST `/mcp` with JSON-RPC messages)
- [ ] Authentication via existing JWT tokens (Bearer header)
- [ ] Each authenticated user gets their own scoped MCP session

### MCP Protocol Compliance

- [ ] Implement MCP JSON-RPC message handling (request/response/notification)
- [ ] Support `initialize` handshake with capability negotiation
- [ ] Support `ping` for connection health checks
- [ ] Return proper JSON-RPC error codes for invalid requests
- [ ] Advertise server name, version, and supported protocol version

### Tools (AI-Callable Functions)

#### Calendar Tools

- [ ] `list_calendars` - List all calendars for the authenticated user
- [ ] `get_events` - Get events from a calendar with date range filter
  - Parameters: `calendar_id`, `start` (ISO 8601), `end` (ISO 8601)
- [ ] `create_event` - Create a new calendar event
  - Parameters: `calendar_id`, `title`, `start`, `end`, `description?`, `location?`, `all_day?`
- [ ] `update_event` - Update an existing event
  - Parameters: `event_id`, fields to update
- [ ] `delete_event` - Delete an event
  - Parameters: `event_id`
- [ ] `search_events` - Full-text search across all calendars
  - Parameters: `query`, `start?`, `end?`

#### Contact Tools

- [ ] `list_address_books` - List all address books
- [ ] `get_contacts` - Get contacts from an address book with pagination
  - Parameters: `address_book_id`, `limit?`, `offset?`
- [ ] `search_contacts` - Search contacts by name, email, phone
  - Parameters: `query`
- [ ] `create_contact` - Create a new contact
  - Parameters: `address_book_id`, `first_name`, `last_name`, `email?`, `phone?`, `organization?`
- [ ] `update_contact` - Update an existing contact
  - Parameters: `contact_id`, fields to update
- [ ] `delete_contact` - Delete a contact
  - Parameters: `contact_id`

#### Scheduling Tools

- [ ] `find_free_slots` - Find available time slots in a date range
  - Parameters: `start`, `end`, `duration_minutes`, `calendar_ids?`

### Resources (AI-Readable Data)

- [ ] `calendars://list` - Summary of all calendars (name, color, event count)
- [ ] `calendars://{id}/events` - Events for a specific calendar
- [ ] `contacts://list` - Summary of all address books
- [ ] `contacts://{id}` - Full details of a specific contact

### Error Handling

- [ ] Return MCP-compliant error responses for:
  - Unauthorized access (invalid/expired token)
  - Resource not found (invalid calendar/contact ID)
  - Validation errors (missing required fields, invalid date formats)
  - Rate limiting (per-user)
- [ ] Tool errors include human-readable descriptions

## Technical Notes

### MCP Protocol Overview

MCP uses JSON-RPC 2.0 over HTTP. The server advertises capabilities and the client discovers available tools and resources at runtime.

### Transport: Streamable HTTP

```
POST /mcp
Content-Type: application/json
Authorization: Bearer <jwt_token>

{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {...}}
```

For server-to-client notifications (e.g., resource updates), use SSE on `GET /mcp` with the `Accept: text/event-stream` header.

### Server Manifest

```json
{
  "name": "calcard",
  "version": "1.0.0",
  "protocolVersion": "2025-03-26",
  "capabilities": {
    "tools": {},
    "resources": {}
  }
}
```

### Database Schema

No new tables required. The MCP server acts as a facade over existing calendar and contact repositories.

### Code Structure

```
internal/adapter/mcp/
  server.go              # MCP server setup, JSON-RPC dispatcher
  handler.go             # Streamable HTTP transport handler (Fiber)
  tools_calendar.go      # Calendar tool implementations
  tools_contact.go       # Contact tool implementations
  tools_scheduling.go    # Scheduling tool implementations
  resources.go           # Resource provider implementations
  protocol.go            # JSON-RPC types, MCP message types
  errors.go              # MCP error codes and helpers

internal/usecase/mcp/
  session.go             # Per-user MCP session management
```

### Route Registration

```go
// In routes.go
mcpGroup := api.Group("/mcp", authMiddleware)
mcpHandler := mcp.NewHandler(calendarRepo, eventRepo, addressBookRepo, contactRepo)
mcpGroup.Get("/", mcpHandler.ServerSentEvents)   // SSE for notifications
mcpGroup.Post("/", mcpHandler.HandleMessage)      // JSON-RPC messages
```

### Tool Definition Example

```go
type Tool struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"` // JSON Schema
}

var listCalendarsTool = Tool{
    Name:        "list_calendars",
    Description: "List all calendars for the current user",
    InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
}

var createEventTool = Tool{
    Name:        "create_event",
    Description: "Create a new calendar event",
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "calendar_id": {"type": "string", "description": "Calendar ID"},
            "title": {"type": "string", "description": "Event title"},
            "start": {"type": "string", "format": "date-time", "description": "Start time (ISO 8601)"},
            "end": {"type": "string", "format": "date-time", "description": "End time (ISO 8601)"},
            "description": {"type": "string", "description": "Event description"},
            "location": {"type": "string", "description": "Event location"},
            "all_day": {"type": "boolean", "description": "All-day event"}
        },
        "required": ["calendar_id", "title", "start", "end"]
    }`),
}
```

### Dependencies

Consider using an existing Go MCP SDK if available (e.g., `github.com/mark3labs/mcp-go`) to handle protocol details, or implement the subset needed from scratch since the JSON-RPC layer is straightforward.

## API Response Examples

### Initialize Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-03-26",
    "serverInfo": {
      "name": "calcard",
      "version": "1.0.0"
    },
    "capabilities": {
      "tools": {},
      "resources": {}
    }
  }
}
```

### tools/list Response

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "list_calendars",
        "description": "List all calendars for the current user",
        "inputSchema": {"type": "object", "properties": {}}
      },
      {
        "name": "create_event",
        "description": "Create a new calendar event",
        "inputSchema": {
          "type": "object",
          "properties": {
            "calendar_id": {"type": "string"},
            "title": {"type": "string"},
            "start": {"type": "string", "format": "date-time"},
            "end": {"type": "string", "format": "date-time"}
          },
          "required": ["calendar_id", "title", "start", "end"]
        }
      }
    ]
  }
}
```

### tools/call Response (create_event)

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Event 'Team Standup' created successfully on calendar 'Work' from 2026-03-06T09:00:00Z to 2026-03-06T09:30:00Z"
      }
    ]
  }
}
```

### tools/call Response (search_contacts)

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Found 2 contacts matching 'smith':\n\n1. Jane Smith - jane@example.com - +1-555-0123\n2. Bob Smith - bob@company.org - +1-555-0456"
      }
    ]
  }
}
```

## Definition of Done

- [ ] `POST /mcp` handles MCP JSON-RPC messages with JWT auth
- [ ] `initialize` handshake returns server capabilities
- [ ] `tools/list` returns all available tools with JSON Schema
- [ ] `tools/call` executes calendar and contact tools correctly
- [ ] `resources/list` and `resources/read` expose calendar/contact data
- [ ] All tools respect user authentication scope (users only see their own data)
- [ ] Error responses follow MCP and JSON-RPC 2.0 specifications
- [ ] Unit tests for each tool implementation
- [ ] Integration test for full MCP session lifecycle (initialize -> list tools -> call tool)
