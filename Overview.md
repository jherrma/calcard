# CalDAV/CardDAV Server Project Overview

## Executive Summary

This document outlines the design and rationale for building a modern, self-hostable CalDAV/CardDAV server in Go with a web-based user interface. The project addresses a significant gap in the current open-source ecosystem: the lack of a lightweight, modern calendar and contacts server with proper user management and contemporary authentication methods.

---

## What is CalDAV/CardDAV?

**CalDAV** (Calendaring Extensions to WebDAV) is an Internet standard defined in RFC 4791 that allows clients to access and manage calendar data on remote servers. It enables synchronization of events, appointments, and to-dos across multiple devices and applications.

**CardDAV** (vCard Extensions to WebDAV) is defined in RFC 6352 and provides similar functionality for contact/address book data, allowing synchronization of contacts across devices.

Both protocols are built on WebDAV (Web Distributed Authoring and Versioning), which extends HTTP with methods for collaborative content management.

### Why CalDAV/CardDAV Matters

- **Vendor Independence**: Unlike proprietary sync solutions (Google, Apple, Microsoft), CalDAV/CardDAV is an open standard
- **Self-Hosting**: Users maintain control over their personal data
- **Interoperability**: Works with virtually all calendar and contact applications
- **Privacy**: Data never leaves your infrastructure

---

## The Market Gap

### Current Solutions and Their Limitations

| Solution | Language | Limitations |
|----------|----------|-------------|
| **Radicale** | Python | No web UI, no user self-registration, minimal user management, configuration-file based authentication only |
| **Baikal** | PHP | Dated user interface (2010s design), no OAuth/SAML support, requires PHP hosting, limited to SQLite/MySQL |
| **Nextcloud** | PHP | Overkill - full collaboration suite when only calendar/contacts needed, resource-heavy, complex deployment |
| **SOGo** | Objective-C | Complex installation, enterprise-focused, steep learning curve, requires multiple dependencies |
| **DAViCal** | PHP | Primarily CalDAV only, complex setup, dated codebase, limited CardDAV support |
| **Cyrus IMAP** | C | Part of full mail server, extremely complex, enterprise-only use case |
| **ownCloud** | PHP | Similar to Nextcloud, full suite when simpler solution needed |

### The Gap This Project Fills

**There is no modern, lightweight, self-hostable CalDAV/CardDAV server that provides:**

1. **User Self-Management**
   - User registration without admin intervention
   - Password reset functionality
   - Profile management

2. **Modern Authentication**
   - OAuth2/OpenID Connect integration (Google, Microsoft, custom providers)
   - SAML support for enterprise SSO
   - App-specific passwords for DAV clients

3. **User-Friendly Web Interface**
   - Browse and manage calendars visually
   - Create, edit, and delete events through a modern UI
   - Manage contacts with search and filtering
   - Share calendars with other users

4. **Easy Deployment**
   - Single binary deployment (Go advantage)
   - Docker-ready
   - Minimal dependencies
   - Support for SQLite (simple) or PostgreSQL (scalable)

5. **Modern Codebase**
   - Written in a modern, performant language (Go)
   - Clean architecture
   - Active maintenance potential
   - Comprehensive API for extensions

---

## Target Users

### Primary Users

1. **Privacy-Conscious Individuals**
   - Want control over personal calendar/contact data
   - Prefer self-hosted solutions over cloud services
   - Need sync across multiple devices (phone, tablet, computer)

2. **Small Organizations/Teams**
   - Need shared calendars without enterprise costs
   - Want SSO integration with existing identity providers
   - Require user management without IT overhead

3. **Home Lab Enthusiasts**
   - Self-host their digital infrastructure
   - Value lightweight, efficient software
   - Appreciate Docker-based deployment

4. **Developers/Tech Professionals**
   - Need API access to calendar data
   - Want to integrate with other self-hosted services
   - Value open-source, extensible solutions

### Secondary Users

5. **Small Businesses**
   - Need calendar sharing for scheduling
   - Want employee contact directories
   - Require audit trails and access control

6. **Educational Institutions**
   - Need to integrate with SAML/OAuth identity providers
   - Want lightweight alternatives to commercial solutions

---

## Core Value Propositions

### 1. Simplicity Without Sacrifice

Unlike Nextcloud or SOGo, this server focuses exclusively on calendar and contacts. No file storage, no chat, no office suite - just CalDAV and CardDAV done right.

### 2. Modern Authentication, Legacy Protocol Support

The fundamental challenge with CalDAV/CardDAV is that clients (DAVx5, Apple Calendar, Thunderbird) only support HTTP Basic Authentication, while users expect modern OAuth/SAML login. This project solves this through **app-specific passwords**:

```
User Experience:
1. Log into web UI via Google/Microsoft/SAML
2. Navigate to Settings > App Passwords
3. Create password named "My iPhone"
4. Enter username + app-password in iPhone settings
5. Calendar/contacts sync automatically
```

### 3. Web UI for Non-Technical Users

Most CalDAV servers assume users will only interact through native apps. This project provides a full web interface for:
- Viewing and managing calendars
- Creating and editing events
- Browsing and editing contacts
- Sharing calendars with other users
- Managing account settings

### 4. Single Binary Deployment

Go compiles to a single static binary with no runtime dependencies. Deployment is as simple as:

```bash
./caldav-server --config config.yaml
```

Or with Docker:

```bash
docker run -p 8080:8080 -v ./config.yaml:/app/config.yaml caldav-server
```

---

## Feature Comparison Matrix

| Feature | Radicale | Baikal | Nextcloud | **This Project** |
|---------|----------|--------|-----------|------------------|
| CalDAV | Yes | Yes | Yes | Yes |
| CardDAV | Yes | Yes | Yes | Yes |
| Web Calendar UI | No | Limited | Yes | Yes |
| Web Contact UI | No | Limited | Yes | Yes |
| User Registration | No | Admin only | Yes | Yes |
| OAuth2/OIDC | No | No | Yes | Yes |
| SAML | No | No | Plugin | Yes |
| App Passwords | No | No | Yes | Yes |
| Calendar Sharing | Limited | Limited | Yes | Yes |
| Single Binary | No (Python) | No (PHP) | No (PHP) | Yes |
| Docker-Native | Community | Community | Official | Official |
| Lightweight | Yes | Yes | No | Yes |
| Active Development | Moderate | Low | High | - |

---

## Why Go?

### Technical Advantages

1. **Single Binary**: No runtime dependencies, no virtual environments, no package managers needed on target system

2. **Performance**: Go's compiled nature and efficient concurrency model handle many simultaneous sync requests efficiently

3. **Memory Efficiency**: Significantly lower memory footprint than PHP/Python applications

4. **Cross-Platform**: Compile for Linux, macOS, Windows, ARM (Raspberry Pi) from single codebase

5. **Strong Typing**: Catches errors at compile time rather than runtime

6. **Excellent HTTP Support**: Go's standard library provides production-ready HTTP server capabilities

7. **Growing Ecosystem**: Mature libraries for CalDAV/CardDAV (emersion/go-webdav), authentication (coreos/go-oidc), and databases (GORM)

### Operational Advantages

1. **Easy Updates**: Replace single binary, restart service

2. **Low Resource Usage**: Suitable for VPS, Raspberry Pi, or NAS deployment

3. **Container-Friendly**: Small container images (~20MB vs hundreds for PHP applications)

4. **Predictable Performance**: No garbage collection pauses like JVM, no cold starts like PHP

---

## Project Scope

### In Scope

- Full CalDAV implementation (RFC 4791)
- Full CardDAV implementation (RFC 6352)
- WebDAV-Sync for efficient synchronization (RFC 6578)
- Service discovery (RFC 6764)
- User registration and management
- OAuth2/OIDC authentication
- SAML authentication
- App-specific passwords for DAV clients
- Calendar and addressbook sharing
- Web UI for calendar management
- Web UI for contact management
- Docker deployment
- PostgreSQL and SQLite support

### Out of Scope

- Scheduling/Free-Busy queries (RFC 6638) - Future enhancement
- CalDAV scheduling (iTIP/iMIP) - Future enhancement
- File storage/WebDAV file shares
- Email integration
- Chat/messaging features
- Office document editing
- Mobile native applications (rely on existing clients)

---

## Success Criteria

The project will be considered successful when:

1. **DAVx5 on Android** can discover, authenticate, and fully sync calendars and contacts
2. **Apple Calendar/Contacts** on iOS/macOS can connect and sync without issues
3. **Thunderbird** can add the server as a calendar/contact source
4. **Users can self-register** and manage their accounts without admin intervention
5. **OAuth login works** with at least Google and Microsoft providers
6. **Calendar sharing** allows users to grant read or read-write access to other users
7. **Web UI** provides full CRUD for calendars, events, addressbooks, and contacts
8. **Docker deployment** works with a single `docker-compose up`

---

## Conclusion

This project addresses a clear need in the self-hosting ecosystem: a modern, lightweight CalDAV/CardDAV server with proper user management and authentication. By building on Go's strengths and leveraging existing protocol libraries, the project can deliver significant value to privacy-conscious individuals and small organizations seeking alternatives to cloud-based calendar and contact services.

The combination of protocol compliance, modern authentication, web-based management, and easy deployment will differentiate this solution from existing options and provide a foundation for a sustainable open-source project.
