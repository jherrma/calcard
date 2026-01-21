# Story 028: Client Setup Instructions API

## Title
Implement API Endpoints for Client Setup Instructions

## Description
As a user, I want to access setup instructions for various CalDAV/CardDAV clients so that I can configure my devices to sync with the server.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| UI-7.2.1 | Web UI provides DAVx5 setup instructions |
| UI-7.2.2 | Web UI provides Apple device setup instructions |
| UI-7.2.3 | Web UI provides Thunderbird setup instructions |
| UI-7.2.4 | Server URL is clearly displayed for copying |

## Acceptance Criteria

### Server Info Endpoint

- [ ] REST endpoint `GET /api/v1/server/info` (public, no auth)
- [ ] Returns server information:
  - [ ] Server name/version
  - [ ] Base URL
  - [ ] CalDAV URL
  - [ ] CardDAV URL
  - [ ] Well-known URLs
  - [ ] Supported features

### User-Specific Setup Info

- [ ] REST endpoint `GET /api/v1/users/me/setup` (requires auth)
- [ ] Returns personalized setup information:
  - [ ] Username
  - [ ] CalDAV URL with username path
  - [ ] CardDAV URL with username path
  - [ ] Principal URL
  - [ ] Instructions for each client

### Client Instructions Endpoint

- [ ] REST endpoint `GET /api/v1/setup/instructions/{client}` (public)
- [ ] Supported clients:
  - [ ] `davx5` - DAVx5 (Android)
  - [ ] `apple` - Apple Calendar/Contacts (iOS/macOS)
  - [ ] `thunderbird` - Thunderbird
  - [ ] `outlook` - Outlook (CalDAV add-in)
  - [ ] `gnome` - GNOME Calendar/Contacts
- [ ] Returns step-by-step instructions
- [ ] Includes server URLs with placeholders

### QR Code Generation

- [ ] REST endpoint `GET /api/v1/users/me/setup/qr` (requires auth)
- [ ] Generates QR code for DAVx5 quick setup
- [ ] QR code contains connection info (not password)
- [ ] Returns PNG image

## Technical Notes

### Server Info Response
```go
type ServerInfo struct {
    Name        string   `json:"name"`
    Version     string   `json:"version"`
    BaseURL     string   `json:"base_url"`
    CalDAVURL   string   `json:"caldav_url"`
    CardDAVURL  string   `json:"carddav_url"`
    WellKnown   WellKnown `json:"well_known"`
    Features    []string `json:"features"`
}

type WellKnown struct {
    CalDAV  string `json:"caldav"`
    CardDAV string `json:"carddav"`
}
```

### User Setup Response
```go
type UserSetupInfo struct {
    Username      string            `json:"username"`
    ServerURL     string            `json:"server_url"`
    CalDAVURL     string            `json:"caldav_url"`
    CardDAVURL    string            `json:"carddav_url"`
    PrincipalURL  string            `json:"principal_url"`
    Instructions  map[string]string `json:"instructions"`
    QRCodeURL     string            `json:"qr_code_url"`
}
```

### Client Instructions Template
```go
var clientInstructions = map[string]ClientInstructions{
    "davx5": {
        Name: "DAVx5",
        Platform: "Android",
        Steps: []string{
            "Install DAVx5 from Google Play or F-Droid",
            "Open DAVx5 and tap the '+' button",
            "Select 'Login with URL and user name'",
            "Enter the server URL: {{.ServerURL}}",
            "Enter your username: {{.Username}}",
            "Enter your app password (create one in Settings → App Passwords)",
            "Select which calendars and address books to sync",
            "Tap the sync button to start synchronization",
        },
        Notes: []string{
            "Use an App Password, not your main account password",
            "DAVx5 will automatically discover all your calendars and contacts",
        },
        Links: []Link{
            {Title: "DAVx5 Website", URL: "https://www.davx5.com/"},
            {Title: "Google Play", URL: "https://play.google.com/store/apps/details?id=at.bitfire.davdroid"},
        },
    },
    "apple": {
        Name: "Apple Calendar & Contacts",
        Platform: "iOS / macOS",
        Steps: []string{
            "Open Settings (iOS) or System Preferences (macOS)",
            "Go to 'Calendar' → 'Accounts' (iOS) or 'Internet Accounts' (macOS)",
            "Tap 'Add Account' → 'Other'",
            "For calendars: Select 'Add CalDAV Account'",
            "For contacts: Select 'Add CardDAV Account'",
            "Enter the following details:",
            "  Server: {{.ServerURL}}",
            "  User Name: {{.Username}}",
            "  Password: Your app password",
            "Tap 'Next' and wait for verification",
            "Select which calendars/contacts to sync",
        },
        Notes: []string{
            "Use an App Password for the password field",
            "You may need to add CalDAV and CardDAV accounts separately",
            "iOS may show a certificate warning for self-signed certificates",
        },
    },
    "thunderbird": {
        Name: "Thunderbird",
        Platform: "Windows / macOS / Linux",
        Steps: []string{
            "Open Thunderbird",
            "For calendars:",
            "  Go to the Calendar tab",
            "  Right-click in the calendar list → 'New Calendar'",
            "  Select 'On the Network' → 'CalDAV'",
            "  Enter location: {{.CalDAVURL}}",
            "  Enter your username and app password when prompted",
            "For contacts:",
            "  Go to the Address Book",
            "  File → New → CardDAV Address Book",
            "  Enter location: {{.CardDAVURL}}",
            "  Enter your username and app password",
        },
        Notes: []string{
            "Thunderbird 102+ has built-in CalDAV/CardDAV support",
            "For older versions, install the 'TbSync' add-on",
        },
    },
}
```

### QR Code Generation
```go
import "github.com/skip2/go-qrcode"

func (h *SetupHandler) GenerateQRCode(c fiber.Ctx) error {
    user := getUserFromContext(c)

    // DAVx5 uses a specific URL format for quick setup
    // Format: caldavs://username@server.example.com/path
    setupURL := fmt.Sprintf("caldavs://%s@%s%s",
        user.Username,
        h.config.BaseURLHost(),
        h.config.DAVPath(),
    )

    png, err := qrcode.Encode(setupURL, qrcode.Medium, 256)
    if err != nil {
        return c.Status(500).JSON(fiber.Map{"error": "Failed to generate QR code"})
    }

    c.Set("Content-Type", "image/png")
    c.Set("Cache-Control", "private, max-age=3600")
    return c.Send(png)
}
```

### Code Structure
```
internal/usecase/setup/
├── server_info.go          # Server info
├── user_setup.go           # User-specific setup
└── instructions.go         # Client instructions

internal/adapter/http/
└── setup_handler.go        # HTTP handlers

internal/static/
└── instructions/           # Instruction templates
    ├── davx5.md
    ├── apple.md
    └── thunderbird.md
```

## API Response Examples

### Server Info (200 OK)
```json
{
  "name": "CalDAV Server",
  "version": "1.0.0",
  "base_url": "https://caldav.example.com",
  "caldav_url": "https://caldav.example.com/dav/calendars/",
  "carddav_url": "https://caldav.example.com/dav/addressbooks/",
  "well_known": {
    "caldav": "https://caldav.example.com/.well-known/caldav",
    "carddav": "https://caldav.example.com/.well-known/carddav"
  },
  "features": [
    "calendar-access",
    "addressbook",
    "sync-collection",
    "calendar-auto-schedule"
  ]
}
```

### User Setup Info (200 OK)
```json
{
  "username": "johndoe",
  "server_url": "https://caldav.example.com",
  "caldav_url": "https://caldav.example.com/dav/calendars/johndoe/",
  "carddav_url": "https://caldav.example.com/dav/addressbooks/johndoe/",
  "principal_url": "https://caldav.example.com/dav/principals/johndoe/",
  "qr_code_url": "/api/v1/users/me/setup/qr",
  "clients": [
    {
      "id": "davx5",
      "name": "DAVx5",
      "platform": "Android",
      "instructions_url": "/api/v1/setup/instructions/davx5"
    },
    {
      "id": "apple",
      "name": "Apple Calendar & Contacts",
      "platform": "iOS / macOS",
      "instructions_url": "/api/v1/setup/instructions/apple"
    },
    {
      "id": "thunderbird",
      "name": "Thunderbird",
      "platform": "Windows / macOS / Linux",
      "instructions_url": "/api/v1/setup/instructions/thunderbird"
    }
  ]
}
```

### Client Instructions (200 OK)
```json
{
  "client": "davx5",
  "name": "DAVx5",
  "platform": "Android",
  "steps": [
    {
      "number": 1,
      "title": "Install DAVx5",
      "description": "Install DAVx5 from Google Play or F-Droid"
    },
    {
      "number": 2,
      "title": "Add Account",
      "description": "Open DAVx5 and tap the '+' button"
    },
    {
      "number": 3,
      "title": "Select Login Type",
      "description": "Select 'Login with URL and user name'"
    },
    {
      "number": 4,
      "title": "Enter Server URL",
      "description": "Enter the server URL",
      "value": "https://caldav.example.com",
      "copyable": true
    },
    {
      "number": 5,
      "title": "Enter Username",
      "description": "Enter your username",
      "placeholder": "your-username"
    },
    {
      "number": 6,
      "title": "Enter Password",
      "description": "Enter your app password (create one in Settings → App Passwords)",
      "note": "Do NOT use your main account password"
    },
    {
      "number": 7,
      "title": "Select Data to Sync",
      "description": "Select which calendars and address books to sync"
    },
    {
      "number": 8,
      "title": "Start Sync",
      "description": "Tap the sync button to start synchronization"
    }
  ],
  "notes": [
    "Use an App Password, not your main account password",
    "DAVx5 will automatically discover all your calendars and contacts"
  ],
  "links": [
    {
      "title": "DAVx5 Website",
      "url": "https://www.davx5.com/"
    },
    {
      "title": "Google Play",
      "url": "https://play.google.com/store/apps/details?id=at.bitfire.davdroid"
    },
    {
      "title": "F-Droid",
      "url": "https://f-droid.org/packages/at.bitfire.davdroid/"
    }
  ]
}
```

### QR Code (200 OK)
```
Content-Type: image/png
Cache-Control: private, max-age=3600

[PNG binary data]
```

## Definition of Done

- [ ] `GET /api/v1/server/info` returns server information
- [ ] `GET /api/v1/users/me/setup` returns personalized setup URLs
- [ ] `GET /api/v1/setup/instructions/{client}` returns client instructions
- [ ] Instructions available for DAVx5, Apple, Thunderbird
- [ ] `GET /api/v1/users/me/setup/qr` generates QR code
- [ ] QR code works with DAVx5 quick setup
- [ ] URLs are properly formatted with server base URL
- [ ] Instructions clearly indicate to use App Passwords
- [ ] Unit tests for URL generation
- [ ] Integration tests for all endpoints
