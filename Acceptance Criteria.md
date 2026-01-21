# Acceptance Criteria

This document defines the comprehensive acceptance criteria for the CalDAV/CardDAV server project. Each criterion is categorized, numbered, and includes specific testable requirements.

---

## 1. User Management

### 1.1 User Registration

| ID | Criterion | Verification |
|----|-----------|--------------|
| UM-1.1.1 | Users can create an account with email and password | Register via web form, receive confirmation |
| UM-1.1.2 | Email verification is sent upon registration | Check email inbox for verification link |
| UM-1.1.3 | Account is not active until email is verified | Attempt login before verification fails |
| UM-1.1.4 | Duplicate email addresses are rejected | Attempt registration with existing email |
| UM-1.1.5 | Password strength requirements are enforced | Attempt weak passwords, verify rejection |
| UM-1.1.6 | Username uniqueness is enforced | Attempt duplicate username registration |

### 1.2 Password Management

| ID | Criterion | Verification |
|----|-----------|--------------|
| UM-1.2.1 | Users can change their password when logged in | Change password via settings, login with new password |
| UM-1.2.2 | Password change requires current password confirmation | Attempt change without current password fails |
| UM-1.2.3 | Users can request password reset via email | Request reset, receive email with reset link |
| UM-1.2.4 | Password reset links expire after configured time (e.g., 1 hour) | Attempt reset with expired link fails |
| UM-1.2.5 | Password reset invalidates previous reset links | Request multiple resets, only latest works |
| UM-1.2.6 | All sessions are invalidated after password change | Other logged-in sessions are logged out |

### 1.3 Profile Management

| ID | Criterion | Verification |
|----|-----------|--------------|
| UM-1.3.1 | Users can update their display name | Update name, verify change in UI and DAV principal |
| UM-1.3.2 | Users can view their account creation date | Check profile page shows creation timestamp |
| UM-1.3.3 | Users can delete their account | Delete account, verify all data is removed |
| UM-1.3.4 | Account deletion requires password confirmation | Attempt deletion without password fails |

---

## 2. Authentication

### 2.1 Local Authentication

| ID | Criterion | Verification |
|----|-----------|--------------|
| AU-2.1.1 | Users can login with email and password | Login via web form, access dashboard |
| AU-2.1.2 | Failed login attempts show generic error message | Invalid credentials show "Invalid email or password" |
| AU-2.1.3 | Rate limiting prevents brute force attacks | Multiple failed attempts trigger temporary lockout |
| AU-2.1.4 | Sessions expire after configured inactivity period | Idle session is logged out after timeout |
| AU-2.1.5 | Users can manually logout | Click logout, session is invalidated |
| AU-2.1.6 | JWT tokens have configurable expiration | Tokens expire per configuration |

### 2.2 OAuth2/OpenID Connect Authentication

| ID | Criterion | Verification |
|----|-----------|--------------|
| AU-2.2.1 | Users can login via Google OAuth | Click "Login with Google", complete OAuth flow |
| AU-2.2.2 | Users can login via Microsoft/Azure AD | Click "Login with Microsoft", complete OAuth flow |
| AU-2.2.3 | Users can login via custom OIDC provider | Configure custom provider, complete login |
| AU-2.2.4 | First OAuth login creates new account automatically | New OAuth user gets account created |
| AU-2.2.5 | Subsequent OAuth logins link to existing account | Return OAuth user logs into same account |
| AU-2.2.6 | Users can link multiple OAuth providers to one account | Link Google and Microsoft to same account |
| AU-2.2.7 | OAuth-only accounts cannot use password login | Attempt password login fails with appropriate message |
| AU-2.2.8 | Users can disconnect OAuth providers (if other auth method exists) | Remove Google link while Microsoft remains |

### 2.3 SAML Authentication

| ID | Criterion | Verification |
|----|-----------|--------------|
| AU-2.3.1 | Service Provider (SP) metadata is available at standard endpoint | Fetch /api/v1/auth/saml/metadata |
| AU-2.3.2 | Users can login via SAML Identity Provider | Initiate SAML flow, complete SSO |
| AU-2.3.3 | SAML assertions are properly validated | Invalid assertions are rejected |
| AU-2.3.4 | SAML attributes map to user profile fields | Email, name from SAML populate profile |
| AU-2.3.5 | SAML logout (SLO) is supported | SAML logout terminates server session |

### 2.4 App Passwords (DAV Client Authentication)

| ID | Criterion | Verification |
|----|-----------|--------------|
| AU-2.4.1 | Users can create app-specific passwords | Create password via web UI |
| AU-2.4.2 | App password is displayed only once upon creation | Password shown once, not retrievable after |
| AU-2.4.3 | Users can name app passwords (e.g., "DAVx5 Phone") | Create password with descriptive name |
| AU-2.4.4 | Users can view list of app passwords with creation dates | View list in settings |
| AU-2.4.5 | Users can see last-used date for each app password | Last-used timestamp updates on use |
| AU-2.4.6 | Users can revoke individual app passwords | Revoke password, subsequent DAV auth fails |
| AU-2.4.7 | App passwords can be scoped (CalDAV-only, CardDAV-only, or both) | Create CalDAV-only password, CardDAV access denied |
| AU-2.4.8 | App passwords work with HTTP Basic Auth for DAV endpoints | Configure DAV client with username + app-password |

### 2.5 CalDAV Access Credentials

Users need the ability to create credentials for CalDAV access, independent of their account authentication method (SAML, OAuth, or local password). This enables sharing calendar URLs or using CalDAV clients without exposing the main account credentials. These credentials grant access to ALL of the user's calendars.

| ID | Criterion | Verification |
|----|-----------|--------------|
| AU-2.5.1 | Users can create CalDAV access credentials | Create credentials via settings |
| AU-2.5.2 | CalDAV credentials consist of a custom username and auto-generated password | Create credentials, both username and password are provided |
| AU-2.5.3 | Users can set a custom username for CalDAV credentials | Set username like "calendar-sync", verify it works |
| AU-2.5.4 | CalDAV credentials password is displayed only once upon creation | Password shown once, not retrievable after |
| AU-2.5.5 | Users can have multiple CalDAV credential sets | Create multiple credentials for different purposes |
| AU-2.5.6 | Users can name/label each credential set (e.g., "Google Calendar Import") | Create named credential, visible in list |
| AU-2.5.7 | Users can set credentials as read-only or read-write | Create read-only credential, write operations fail |
| AU-2.5.8 | CalDAV credentials work independently of user's main auth method | SAML/OAuth user can create and use CalDAV credentials |
| AU-2.5.9 | CalDAV credentials grant access to all user's calendars | One credential works for all calendars |
| AU-2.5.10 | Users can view list of CalDAV credentials with creation dates | View list in settings |
| AU-2.5.11 | Users can see last-used date for each CalDAV credential | Last-used timestamp updates on use |
| AU-2.5.12 | Users can revoke individual CalDAV credentials | Revoke credential, subsequent access denied |
| AU-2.5.13 | CalDAV credentials can have optional expiration date | Create credential with 30-day expiry, expires after 30 days |

### 2.6 CardDAV Access Credentials

Users need the ability to create credentials for CardDAV access, independent of their account authentication method. These credentials grant access to ALL of the user's address books.

| ID | Criterion | Verification |
|----|-----------|--------------|
| AU-2.6.1 | Users can create CardDAV access credentials | Create credentials via settings |
| AU-2.6.2 | CardDAV credentials consist of a custom username and auto-generated password | Create credentials, both provided |
| AU-2.6.3 | Users can set a custom username for CardDAV credentials | Set username like "contacts-sync", verify it works |
| AU-2.6.4 | CardDAV credentials password is displayed only once upon creation | Password shown once, not retrievable |
| AU-2.6.5 | Users can have multiple CardDAV credential sets | Create multiple credentials for different purposes |
| AU-2.6.6 | Users can name/label each CardDAV credential set | Create named credential, visible in list |
| AU-2.6.7 | Users can set CardDAV credentials as read-only or read-write | Create read-only credential, write fails |
| AU-2.6.8 | CardDAV credentials work independently of user's main auth method | SAML/OAuth user can use CardDAV credentials |
| AU-2.6.9 | CardDAV credentials grant access to all user's address books | One credential works for all address books |
| AU-2.6.10 | Users can view, track last-used, and revoke CardDAV credentials | Full credential management available |

---

## 3. CalDAV (Calendar) Functionality

### 3.1 Calendar Management

| ID | Criterion | Verification |
|----|-----------|--------------|
| CD-3.1.1 | Users have a default calendar created on account creation | New user has one calendar immediately |
| CD-3.1.2 | Users can create additional calendars | Create calendar via web UI or MKCALENDAR |
| CD-3.1.3 | Users can rename calendars | Update calendar name, verify in clients |
| CD-3.1.4 | Users can set calendar color | Set color, verify in web UI and supported clients |
| CD-3.1.5 | Users can set calendar timezone | Set timezone, events reflect it |
| CD-3.1.6 | Users can delete calendars | Delete calendar, all events removed |
| CD-3.1.7 | Calendar deletion requires confirmation | Accidental deletion prevented |
| CD-3.1.8 | Users can export calendar as .ics file | Download calendar as iCalendar file |

### 3.2 Event Management (Web UI)

| ID | Criterion | Verification |
|----|-----------|--------------|
| CD-3.2.1 | Users can view calendar in month view | Display month grid with events |
| CD-3.2.2 | Users can view calendar in week view | Display week with time slots |
| CD-3.2.3 | Users can view calendar in day view | Display single day with time slots |
| CD-3.2.4 | Users can create events with title, start time, end time | Create event, verify saved |
| CD-3.2.5 | Users can create all-day events | Create all-day event, displays correctly |
| CD-3.2.6 | Users can add event description | Add description, visible in detail view |
| CD-3.2.7 | Users can add event location | Add location, visible in detail view |
| CD-3.2.8 | Users can edit existing events | Modify event, changes persist |
| CD-3.2.9 | Users can delete events | Delete event, removed from calendar |
| CD-3.2.10 | Users can drag-and-drop events to reschedule | Drag event to new time, change saves |
| CD-3.2.11 | Users can resize events to change duration | Resize event, new duration saves |
| CD-3.2.12 | Users can create recurring events (daily, weekly, monthly, yearly) | Create recurring event with RRULE |
| CD-3.2.13 | Users can edit single instance of recurring event | Edit one instance, creates exception |
| CD-3.2.14 | Users can edit all instances of recurring event | Edit series, all instances update |
| CD-3.2.15 | Users can delete single instance of recurring event | Delete one instance, others remain |

### 3.3 CalDAV Protocol Compliance

| ID | Criterion | Verification |
|----|-----------|--------------|
| CD-3.3.1 | Server responds to OPTIONS with correct DAV headers | `DAV: 1, 2, 3, calendar-access` in response |
| CD-3.3.2 | /.well-known/caldav redirects to DAV root | GET /.well-known/caldav returns 301 |
| CD-3.3.3 | PROPFIND on principal returns calendar-home-set | Query principal, get calendar home URL |
| CD-3.3.4 | PROPFIND on calendar-home lists all calendars | Query home, get list of calendars |
| CD-3.3.5 | MKCALENDAR creates new calendar collection | Create calendar via WebDAV |
| CD-3.3.6 | PUT creates new event in calendar | PUT .ics file, event created |
| CD-3.3.7 | PUT updates existing event (with correct ETag) | PUT with If-Match, event updated |
| CD-3.3.8 | PUT with wrong ETag returns 412 Precondition Failed | PUT with old ETag fails |
| CD-3.3.9 | GET retrieves event iCalendar data | GET .ics file, receive valid iCalendar |
| CD-3.3.10 | DELETE removes event | DELETE .ics file, event gone |
| CD-3.3.11 | REPORT calendar-query returns filtered events | Query events in date range |
| CD-3.3.12 | REPORT calendar-multiget returns specific events by URL | Request multiple events by path |
| CD-3.3.13 | ETags change when events are modified | Modify event, ETag changes |
| CD-3.3.14 | CTag changes when calendar contents change | Add/modify/delete event, CTag changes |

### 3.4 WebDAV-Sync (RFC 6578)

| ID | Criterion | Verification |
|----|-----------|--------------|
| CD-3.4.1 | Server provides sync-token for calendars | PROPFIND returns sync-token |
| CD-3.4.2 | REPORT sync-collection returns changes since token | Request sync with old token, get changes |
| CD-3.4.3 | Sync response includes created events | Add event, sync shows it |
| CD-3.4.4 | Sync response includes modified events | Modify event, sync shows update |
| CD-3.4.5 | Sync response includes deleted events (as 404 responses) | Delete event, sync indicates removal |
| CD-3.4.6 | Initial sync (no token) returns all events | First sync returns complete collection |

---

## 4. CardDAV (Contacts) Functionality

### 4.1 Address Book Management

| ID | Criterion | Verification |
|----|-----------|--------------|
| AD-4.1.1 | Users have a default address book on account creation | New user has one address book |
| AD-4.1.2 | Users can create additional address books | Create via web UI or MKCOL |
| AD-4.1.3 | Users can rename address books | Update name, verify in clients |
| AD-4.1.4 | Users can delete address books | Delete address book, all contacts removed |
| AD-4.1.5 | Users can export address book as .vcf file | Download contacts as vCard file |

### 4.2 Contact Management (Web UI)

| ID | Criterion | Verification |
|----|-----------|--------------|
| AD-4.2.1 | Users can view list of contacts | Display contact list |
| AD-4.2.2 | Users can search contacts by name | Search "John", matching contacts shown |
| AD-4.2.3 | Users can search contacts by email | Search email, matching contact shown |
| AD-4.2.4 | Users can search contacts by phone | Search phone number, contact found |
| AD-4.2.5 | Users can create contacts with name | Create contact with formatted name |
| AD-4.2.6 | Users can add multiple email addresses to contact | Add work and personal emails |
| AD-4.2.7 | Users can add multiple phone numbers with types | Add mobile, home, work phones |
| AD-4.2.8 | Users can add postal addresses | Add address with street, city, country |
| AD-4.2.9 | Users can add organization/company | Add company name and title |
| AD-4.2.10 | Users can add notes to contacts | Add free-text notes |
| AD-4.2.11 | Users can edit existing contacts | Modify contact, changes persist |
| AD-4.2.12 | Users can delete contacts | Delete contact, removed from list |
| AD-4.2.13 | Users can add contact photo | Upload photo, displayed in contact |

### 4.3 CardDAV Protocol Compliance

| ID | Criterion | Verification |
|----|-----------|--------------|
| AD-4.3.1 | Server responds to OPTIONS with CardDAV headers | `DAV: 1, 2, 3, addressbook` in response |
| AD-4.3.2 | /.well-known/carddav redirects to DAV root | GET /.well-known/carddav returns 301 |
| AD-4.3.3 | PROPFIND on principal returns addressbook-home-set | Query principal, get addressbook home URL |
| AD-4.3.4 | PROPFIND on addressbook-home lists all address books | Query home, get list of addressbooks |
| AD-4.3.5 | PUT creates new contact in address book | PUT .vcf file, contact created |
| AD-4.3.6 | PUT updates existing contact (with correct ETag) | PUT with If-Match, contact updated |
| AD-4.3.7 | GET retrieves contact vCard data | GET .vcf file, receive valid vCard |
| AD-4.3.8 | DELETE removes contact | DELETE .vcf file, contact gone |
| AD-4.3.9 | REPORT addressbook-query returns filtered contacts | Query contacts matching filter |
| AD-4.3.10 | REPORT addressbook-multiget returns specific contacts | Request multiple contacts by path |
| AD-4.3.11 | Server supports vCard 3.0 format | Create/read vCard 3.0 contacts |
| AD-4.3.12 | Server supports vCard 4.0 format | Create/read vCard 4.0 contacts |

### 4.4 CardDAV WebDAV-Sync

| ID | Criterion | Verification |
|----|-----------|--------------|
| AD-4.4.1 | Server provides sync-token for address books | PROPFIND returns sync-token |
| AD-4.4.2 | REPORT sync-collection returns contact changes | Sync returns created/modified/deleted |

---

## 5. Sharing

### 5.1 Calendar Sharing

| ID | Criterion | Verification |
|----|-----------|--------------|
| SH-5.1.1 | Users can share calendars with other users by username/email | Share calendar, recipient can access |
| SH-5.1.2 | Users can grant read-only access | Share with read, recipient cannot edit |
| SH-5.1.3 | Users can grant read-write access | Share with read-write, recipient can edit |
| SH-5.1.4 | Users can view list of shares for their calendars | View who has access to calendar |
| SH-5.1.5 | Users can modify share permissions | Change from read to read-write |
| SH-5.1.6 | Users can revoke shares | Remove share, access denied |
| SH-5.1.7 | Shared calendars appear in recipient's calendar list | Recipient sees shared calendar |
| SH-5.1.8 | Shared calendars are accessible via CalDAV | DAV client syncs shared calendars |
| SH-5.1.9 | Changes by one user sync to other shared users | Create event, other user sees it |

### 5.2 Address Book Sharing

| ID | Criterion | Verification |
|----|-----------|--------------|
| SH-5.2.1 | Users can share address books with other users | Share addressbook, recipient can access |
| SH-5.2.2 | Users can grant read-only access to address books | Share read-only, recipient cannot edit |
| SH-5.2.3 | Users can grant read-write access to address books | Share read-write, recipient can edit |
| SH-5.2.4 | Users can revoke address book shares | Remove share, access denied |

---

## 6. Client Compatibility

### 6.1 DAVx5 (Android)

| ID | Criterion | Verification |
|----|-----------|--------------|
| CL-6.1.1 | DAVx5 can discover server via base URL | Enter URL, DAVx5 finds CalDAV/CardDAV |
| CL-6.1.2 | DAVx5 can authenticate with username + app-password | Enter credentials, authentication succeeds |
| CL-6.1.3 | DAVx5 syncs all user calendars | All calendars appear in DAVx5 |
| CL-6.1.4 | DAVx5 syncs all user address books | All address books appear in DAVx5 |
| CL-6.1.5 | Events created in DAVx5 appear on server | Create event in Android, visible in web UI |
| CL-6.1.6 | Events created on server sync to DAVx5 | Create event in web UI, visible in Android |
| CL-6.1.7 | Contacts sync bidirectionally | Create/edit contacts on either end |
| CL-6.1.8 | Recurring events sync correctly | Create recurring event, instances appear |
| CL-6.1.9 | Contact photos sync correctly | Photo uploaded in web UI visible in Android |

### 6.2 Apple Calendar/Contacts (iOS/macOS)

| ID | Criterion | Verification |
|----|-----------|--------------|
| CL-6.2.1 | iOS/macOS can add CalDAV account | Settings > Calendar Accounts > Add CalDAV |
| CL-6.2.2 | iOS/macOS can add CardDAV account | Settings > Contacts Accounts > Add CardDAV |
| CL-6.2.3 | Calendars appear in Apple Calendar app | All calendars visible |
| CL-6.2.4 | Contacts appear in Apple Contacts app | All contacts visible |
| CL-6.2.5 | Events sync bidirectionally | Create/edit works both directions |
| CL-6.2.6 | Contacts sync bidirectionally | Create/edit works both directions |
| CL-6.2.7 | Shared calendars appear and function correctly | Shared calendars accessible |

### 6.3 Thunderbird

| ID | Criterion | Verification |
|----|-----------|--------------|
| CL-6.3.1 | Thunderbird can add CalDAV calendar | Add network calendar with URL |
| CL-6.3.2 | Thunderbird can add CardDAV address book | Add CardDAV address book |
| CL-6.3.3 | Events sync bidirectionally | Create/edit works both directions |
| CL-6.3.4 | Contacts sync bidirectionally | Create/edit works both directions |
| CL-6.3.5 | Recurring events display correctly | Recurring event instances shown |

### 6.4 Google Calendar Import

| ID | Criterion | Verification |
|----|-----------|--------------|
| CL-6.4.1 | Users can get public iCal URL for their calendars | Copy URL from web UI |
| CL-6.4.2 | Google Calendar can subscribe to iCal URL | Add URL in Google Calendar |
| CL-6.4.3 | Events appear in Google Calendar (read-only) | Events visible in Google |
| CL-6.4.4 | Updates on server reflect in Google Calendar | Modify event, Google shows update (after sync) |

---

## 7. Web UI

### 7.1 General UI Requirements

| ID | Criterion | Verification |
|----|-----------|--------------|
| UI-7.1.1 | Web UI is responsive (mobile, tablet, desktop) | Test on various screen sizes |
| UI-7.1.2 | Web UI works in Chrome, Firefox, Safari, Edge | Test in all major browsers |
| UI-7.1.3 | Loading states are shown during async operations | Spinners/skeletons during loads |
| UI-7.1.4 | Error messages are user-friendly | Errors explain issue and next steps |
| UI-7.1.5 | Success confirmations are shown for actions | Toast/notification on save |

### 7.2 Setup Instructions

| ID | Criterion | Verification |
|----|-----------|--------------|
| UI-7.2.1 | Web UI provides DAVx5 setup instructions | Instructions page with steps |
| UI-7.2.2 | Web UI provides Apple device setup instructions | Instructions for iOS/macOS |
| UI-7.2.3 | Web UI provides Thunderbird setup instructions | Instructions for Thunderbird |
| UI-7.2.4 | Server URL is clearly displayed for copying | Easy copy button for URL |

---

## 8. Deployment & Operations

### 8.1 Docker Deployment

| ID | Criterion | Verification |
|----|-----------|--------------|
| OP-8.1.1 | Server runs via docker-compose with single command | `docker-compose up` starts server |
| OP-8.1.2 | Database is automatically initialized on first run | Migrations run automatically |
| OP-8.1.3 | Configuration is provided via environment variables | All config via env vars |
| OP-8.1.4 | Configuration can be provided via config file | config.yaml is read |
| OP-8.1.5 | Data persists across container restarts | Stop/start retains data |
| OP-8.1.6 | Health check endpoint is available | GET /health returns 200 |

### 8.2 Database Support

| ID | Criterion | Verification |
|----|-----------|--------------|
| OP-8.2.1 | Server works with PostgreSQL | Configure PostgreSQL, all features work |
| OP-8.2.2 | Server works with SQLite | Configure SQLite, all features work |
| OP-8.2.3 | Database migrations run automatically | Schema created/updated on startup |

### 8.3 Security

| ID | Criterion | Verification |
|----|-----------|--------------|
| OP-8.3.1 | HTTPS is supported via TLS configuration | Configure TLS cert/key, HTTPS works |
| OP-8.3.2 | Passwords are hashed with bcrypt | Database shows hashed values only |
| OP-8.3.3 | JWT secrets are configurable | Different secrets in different envs |
| OP-8.3.4 | CORS is configurable | Configure allowed origins |
| OP-8.3.5 | Rate limiting is configurable | Configure limits, excess requests blocked |

---

## 9. Non-Functional Requirements

### 9.1 Performance

| ID | Criterion | Verification |
|----|-----------|--------------|
| NF-9.1.1 | Server handles 100 concurrent sync requests | Load test with concurrent clients |
| NF-9.1.2 | Calendar with 1000 events syncs in < 10 seconds | Measure sync time |
| NF-9.1.3 | Address book with 1000 contacts syncs in < 10 seconds | Measure sync time |
| NF-9.1.4 | Web UI responds within 500ms for typical operations | Measure response times |

### 9.2 Reliability

| ID | Criterion | Verification |
|----|-----------|--------------|
| NF-9.2.1 | Server recovers gracefully from database connection loss | Disconnect DB, reconnects automatically |
| NF-9.2.2 | Invalid iCalendar data is rejected with clear error | PUT malformed .ics, get 400 error |
| NF-9.2.3 | Invalid vCard data is rejected with clear error | PUT malformed .vcf, get 400 error |
| NF-9.2.4 | Concurrent edits to same resource are handled safely | Two clients edit same event, no data loss |

---

## Summary Statistics

| Category | Total Criteria |
|----------|----------------|
| User Management | 16 |
| Authentication (Local, OAuth, SAML, App Passwords) | 26 |
| Authentication (CalDAV Access Credentials) | 13 |
| Authentication (CardDAV Access Credentials) | 10 |
| CalDAV | 30 |
| CardDAV | 22 |
| Sharing | 11 |
| Client Compatibility | 21 |
| Web UI | 9 |
| Deployment & Operations | 13 |
| Non-Functional | 7 |
| **Total** | **178** |
