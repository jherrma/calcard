package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	gowebdav "github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

// dummyBcryptHash is compared against on the unknown-user path so Basic-Auth
// timing doesn't reveal whether an email is registered.
var dummyBcryptHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-constant-time-compare"), bcrypt.DefaultCost)

type Handler struct {
	caldavHandler   *caldav.Handler
	carddavHandler  *carddav.Handler
	userRepo        user.UserRepository
	appPwdRepo      user.AppPasswordRepository
	caldavCredRepo  user.CalDAVCredentialRepository
	carddavCredRepo user.CardDAVCredentialRepository
	jwtManager      user.TokenProvider
}

func NewHandler(
	caldavBackend *CalDAVBackend,
	carddavBackend *CardDAVBackend,
	userRepo user.UserRepository,
	appPwdRepo user.AppPasswordRepository,
	caldavCredRepo user.CalDAVCredentialRepository,
	carddavCredRepo user.CardDAVCredentialRepository,
	jwtManager user.TokenProvider,
) *Handler {
	return &Handler{
		caldavHandler: &caldav.Handler{
			Backend: caldavBackend,
			Prefix:  "/dav",
		},
		carddavHandler: &carddav.Handler{
			Backend: carddavBackend,
			Prefix:  "/dav",
		},
		userRepo:        userRepo,
		appPwdRepo:      appPwdRepo,
		caldavCredRepo:  caldavCredRepo,
		carddavCredRepo: carddavCredRepo,
		jwtManager:      jwtManager,
	}
}

func (h *Handler) Authenticate() fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			c.Set("WWW-Authenticate", `Basic realm="CalDAV/CardDAV Server"`)
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 {
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		var u *user.User

		switch strings.ToLower(parts[0]) {
		case "bearer":
			userUUID, _, err := h.jwtManager.ValidateAccessToken(parts[1])
			if err == nil {
				u, _ = h.userRepo.GetByUUID(c.Context(), userUUID)
			}
		case "basic":
			payload, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				return c.SendStatus(fiber.StatusUnauthorized)
			}
			pair := strings.SplitN(string(payload), ":", 2)
			if len(pair) != 2 {
				return c.SendStatus(fiber.StatusUnauthorized)
			}

			emailOrUsername, password := pair[0], pair[1]
			u, _ = h.userRepo.GetByEmail(c.Context(), emailOrUsername)
			if u == nil {
				// DAV clients are commonly configured with the account username —
				// it's the segment shown in the DAV URL (/dav/{username}/...) — not
				// the email. Accept both. Email is tried first, so if one user's
				// email equals another's username, email wins (matches login).
				u, _ = h.userRepo.GetByUsername(c.Context(), emailOrUsername)
			}
			if u == nil {
				// Dummy compare so the unknown-identifier path costs roughly the
				// same as a wrong-password path (anti-enumeration). Kept after BOTH
				// lookups fail so the timing signature stays uniform.
				_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
			}
			if u != nil {
				ap, _ := h.appPwdRepo.FindValidForUser(c.Context(), u.ID, password)
				if ap == nil {
					if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
						u = nil
					}
				} else if scope := requiredScopeForPath(c.Path()); scope != "" && !ap.HasScope(scope) {
					// App password authenticated but lacks the scope required for
					// this protocol (e.g. a caldav-only password used against an
					// addressbook path). Reject — the scope restriction shown in
					// the UI must be enforced.
					return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
						"error":   "forbidden",
						"message": "This app password is not valid for this protocol",
					})
				}
				if u != nil {
					c.Locals("can_write", true) // Direct user/app password always has write access
				}
			}

			// If not authenticated as primary user, try dedicated credentials
			if u == nil {
				// Try CalDAV credential
				cred, _ := h.caldavCredRepo.GetByUsername(c.Context(), emailOrUsername)
				if cred != nil && cred.IsValid() {
					if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(password)); err == nil {
						u, _ = h.userRepo.GetByID(c.Context(), cred.UserID)
						if u != nil {
							c.Locals("can_write", cred.CanWrite())
							c.Locals("caldav_credential_id", cred.ID)
							c.Locals("dav_protocol", "caldav")
							go h.caldavCredRepo.UpdateLastUsed(context.Background(), cred.ID, c.IP())
						}
					}
				}

				// If still nil, try CardDAV credential (only if CalDAV failed)
				if u == nil {
					cardCred, _ := h.carddavCredRepo.GetByUsername(c.Context(), emailOrUsername)
					if cardCred != nil && cardCred.IsValid() {
						if err := bcrypt.CompareHashAndPassword([]byte(cardCred.PasswordHash), []byte(password)); err == nil {
							u, _ = h.userRepo.GetByID(c.Context(), cardCred.UserID)
							if u != nil {
								c.Locals("can_write", cardCred.CanWrite())
								c.Locals("carddav_credential_id", cardCred.ID)
								c.Locals("dav_protocol", "carddav")
								go h.carddavCredRepo.UpdateLastUsed(context.Background(), cardCred.ID, c.IP())
							}
						}
					}
				}
			}
		}

		// A deactivated account must not authenticate via any DAV path (Bearer or
		// Basic, primary password, app password, or dedicated DAV credential).
		if u != nil && !u.IsActive {
			u = nil
		}

		if u == nil {
			c.Set("WWW-Authenticate", `Basic realm="CalDAV/CardDAV Server"`)
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		// Check for write permission on non-safe methods if restricted
		if canWrite, ok := c.Locals("can_write").(bool); ok && !canWrite {
			method := c.Method()
			if method != "GET" && method != "HEAD" && method != "PROPFIND" && method != "REPORT" && method != "OPTIONS" {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":   "forbidden",
					"message": "This credential has read-only access",
				})
			}
		}

		// Bind dedicated CalDAV/CardDAV credentials to their own protocol's
		// paths. Principal/root paths carry no required scope so discovery
		// still works for either credential type.
		if proto, ok := c.Locals("dav_protocol").(string); ok {
			if required := requiredScopeForPath(c.Path()); required != "" && required != proto {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":   "forbidden",
					"message": "This credential is not valid for this protocol",
				})
			}
		}

		c.Locals("user", u)
		return c.Next()
	}
}

func (h *Handler) Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		u := c.Locals("user").(*user.User)
		stdCtx := WithUser(c.Context(), u)
		reqPath := c.Path()

		// An addressbook PROPFIND that doesn't request address-data only needs
		// ETags / content-type, so it can be served without hydrating PHOTO blobs
		// (which polling clients would otherwise pull on every refresh interval).
		// Substring sniff is safe: a false positive (the string appears anywhere)
		// merely takes the slow path; a false negative is impossible. An allprop
		// PROPFIND has no "address-data" — correct, since CARDDAV:address-data is
		// not an allprop property and emersion won't serve it for allprop.
		if c.Method() == "PROPFIND" && davCollectionType(reqPath) == "addressbooks" &&
			!bytes.Contains(c.Body(), []byte("address-data")) {
			stdCtx = addressbook.WithSkipPhotoHydration(stdCtx)
		}

		// MKCALENDAR (RFC 4791 §5.3.1) — some clients send this
		// instead of MKCOL for calendar creation. emersion/go-webdav
		// only dispatches MKCOL, so normalise here: the caldav backend
		// treats both identically (CreateCalendar). The MKCALENDAR body
		// is mkcalendar-rooted XML that emersion's MKCOL parser can't
		// read, so we drop the body — displayname can be set by the
		// client via PROPPATCH after creation (Apple Calendar, DAVx5).
		if c.Method() == "MKCALENDAR" {
			c.Request().Header.SetMethod("MKCOL")
			c.Request().SetBody(nil)
			c.Request().Header.SetContentLength(0)
		}

		// Combined principal discovery (RFC 6764). emersion/go-webdav's
		// per-protocol handlers each only advertise their own home set, so a
		// PROPFIND/OPTIONS on the root ("/dav/") or the principal
		// ("/dav/{user}/") routed to the caldav handler would hide the
		// addressbook home set (and vice versa). Serve both home sets from one
		// principal response instead. A dedicated single-protocol credential
		// (dav_protocol set by Authenticate) only sees its own home set.
		discoveryParts := strings.Split(strings.Trim(reqPath, "/"), "/")
		if len(discoveryParts) <= 2 && (c.Method() == "PROPFIND" || c.Method() == "OPTIONS") {
			opts := &gowebdav.ServePrincipalOptions{
				CurrentUserPrincipalPath: fmt.Sprintf("/dav/%s/", u.Username),
			}
			proto, _ := c.Locals("dav_protocol").(string)
			if proto != "carddav" {
				opts.HomeSets = append(opts.HomeSets, caldav.NewCalendarHomeSet(fmt.Sprintf("/dav/%s/calendars/", u.Username)))
				opts.Capabilities = append(opts.Capabilities, caldav.CapabilityCalendar)
			}
			if proto != "caldav" {
				opts.HomeSets = append(opts.HomeSets, carddav.NewAddressBookHomeSet(fmt.Sprintf("/dav/%s/addressbooks/", u.Username)))
				opts.Capabilities = append(opts.Capabilities, carddav.CapabilityAddressBook)
			}
			return adaptor.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gowebdav.ServePrincipal(w, r.WithContext(stdCtx), opts)
			}))(c)
		}

		// Decide the collection type once, by path segment, so REPORT dispatch
		// and backend routing below can never disagree with the scope decision
		// made in Authenticate (both go through davCollectionType).
		collectionType := davCollectionType(reqPath)

		// Enforce conditional DELETE (If-Match). emersion's caldav/carddav
		// dispatch drops If-Match on DELETE, so a stale-ETag If-Match DELETE
		// (sent by iOS/macOS) would otherwise delete unconditionally — the
		// lost-update gap H9 closed for PUT. Resolve the target object's
		// current ETag and reject with 412 when it doesn't match. A missing
		// object falls through so the backend returns the normal 404; the
		// precondition is only enforced when the object actually exists.
		if c.Method() == "DELETE" {
			if ifMatch := gowebdav.ConditionalMatch(c.Get("If-Match")); ifMatch.IsSet() {
				var currentETag string
				var found bool
				if collectionType == "addressbooks" {
					if obj, err := h.carddavHandler.Backend.GetAddressObject(stdCtx, reqPath, nil); err == nil && obj != nil {
						currentETag, found = obj.ETag, true
					}
				} else {
					if obj, err := h.caldavHandler.Backend.GetCalendarObject(stdCtx, reqPath, nil); err == nil && obj != nil {
						currentETag, found = obj.ETag, true
					}
				}
				if found {
					if ok, _ := ifMatch.MatchETag(currentETag); !ok {
						return c.SendStatus(fiber.StatusPreconditionFailed)
					}
				}
			}
		}

		// Handle WebDAV-Sync REPORT for CalDAV
		if c.Method() == "REPORT" && collectionType == "calendars" {
			var syncQuery SyncCollectionQuery
			if err := xml.Unmarshal(c.Body(), &syncQuery); err == nil && syncQuery.XMLName.Local == "sync-collection" {
				return h.handleSyncReport(c, stdCtx, &syncQuery)
			}
		}

		// Handle WebDAV-Sync REPORT for CardDAV
		if c.Method() == "REPORT" && collectionType == "addressbooks" {
			var syncQuery SyncCollectionQuery
			if err := xml.Unmarshal(c.Body(), &syncQuery); err == nil && syncQuery.XMLName.Local == "sync-collection" {
				return h.handleAddressBookSyncReport(c, stdCtx, &syncQuery)
			}
		}

		// Route to appropriate handler based on path
		var httpHandler http.Handler
		if collectionType == "addressbooks" {
			httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.carddavHandler.ServeHTTP(w, r.WithContext(stdCtx))
			})
		} else {
			httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.caldavHandler.ServeHTTP(w, r.WithContext(stdCtx))
			})
		}

		if err := adaptor.HTTPHandler(httpHandler)(c); err != nil {
			return err
		}

		// emersion's CardDAV GET serializes the address object through go-vcard's
		// encoder, which escapes commas in comma-list properties (CATEGORIES),
		// collapsing "Work,Important" into one escaped value. Restore the list
		// separators on a served single-vCard body — mirroring the same fix the
		// REST edit path applies in PatchVCard. Content-Length is recomputed since
		// the restored body is shorter.
		if collectionType == "addressbooks" && c.Method() == fiber.MethodGet {
			body := c.Response().Body()
			if bytes.HasPrefix(bytes.TrimLeft(body, "\r\n \t"), []byte("BEGIN:VCARD")) {
				if restored := addressbook.RestoreVCardCommaLists(string(body)); restored != string(body) {
					c.Response().SetBodyString(restored)
					c.Set(fiber.HeaderContentLength, strconv.Itoa(len(restored)))
				}
			}
		}
		return nil
	}
}

// davCollectionType returns the fixed collection-type segment of a DAV request
// path — "calendars" or "addressbooks" — or "" for principal/root/discovery
// paths. DAV paths are shaped "/dav/{user}/{calendars|addressbooks}/...", so
// the type lives at segment index 2. Deciding by segment (rather than a
// substring match anywhere in the path) is essential: a collection or username
// literally named "calendars"/"addressbooks" must not flip the decision, and
// scope selection must agree with backend routing.
func davCollectionType(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	switch parts[2] {
	case "calendars":
		return "calendars"
	case "addressbooks":
		return "addressbooks"
	default:
		return ""
	}
}

// requiredScopeForPath returns the app-password scope / dav protocol a DAV
// request path requires: "caldav" for calendar paths, "carddav" for address
// book paths, and "" for principal/root/discovery paths (which both protocols
// must be able to reach).
func requiredScopeForPath(path string) string {
	switch davCollectionType(path) {
	case "calendars":
		return "caldav"
	case "addressbooks":
		return "carddav"
	default:
		return ""
	}
}

func WellKnownCalDAVRedirect(c fiber.Ctx) error {
	return c.Redirect().Status(fiber.StatusMovedPermanently).To("/dav/")
}

func WellKnownCardDAVRedirect(c fiber.Ctx) error {
	return c.Redirect().Status(fiber.StatusMovedPermanently).To("/dav/")
}
