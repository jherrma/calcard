package webdav

import (
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	caldavHandler  *caldav.Handler
	carddavHandler *carddav.Handler
	userRepo       user.UserRepository
	appPwdRepo     user.AppPasswordRepository
	jwtManager     user.TokenProvider
}

func NewHandler(
	caldavBackend *CalDAVBackend,
	carddavBackend *CardDAVBackend,
	userRepo user.UserRepository,
	appPwdRepo user.AppPasswordRepository,
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
		userRepo:   userRepo,
		appPwdRepo: appPwdRepo,
		jwtManager: jwtManager,
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

			email, password := pair[0], pair[1]
			u, _ = h.userRepo.GetByEmail(c.Context(), email)
			if u != nil {
				ap, _ := h.appPwdRepo.FindValidForUser(c.Context(), u.ID, password)
				if ap == nil {
					if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
						u = nil
					}
				}
			}
		}

		if u == nil {
			c.Set("WWW-Authenticate", `Basic realm="CalDAV/CardDAV Server"`)
			return c.SendStatus(fiber.StatusUnauthorized)
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

		// Handle WebDAV-Sync REPORT for CalDAV
		if c.Method() == "REPORT" && strings.Contains(reqPath, "/calendars/") {
			var syncQuery SyncCollectionQuery
			if err := xml.Unmarshal(c.Body(), &syncQuery); err == nil && syncQuery.XMLName.Local == "sync-collection" {
				return h.handleSyncReport(c, stdCtx, &syncQuery)
			}
		}

		// Handle WebDAV-Sync REPORT for CardDAV
		if c.Method() == "REPORT" && strings.Contains(reqPath, "/addressbooks/") {
			var syncQuery SyncCollectionQuery
			if err := xml.Unmarshal(c.Body(), &syncQuery); err == nil && syncQuery.XMLName.Local == "sync-collection" {
				return h.handleAddressBookSyncReport(c, stdCtx, &syncQuery)
			}
		}

		// Route to appropriate handler based on path
		var httpHandler http.Handler
		if strings.Contains(reqPath, "/addressbooks/") {
			httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.carddavHandler.ServeHTTP(w, r.WithContext(stdCtx))
			})
		} else {
			httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.caldavHandler.ServeHTTP(w, r.WithContext(stdCtx))
			})
		}

		return adaptor.HTTPHandler(httpHandler)(c)
	}
}

func WellKnownCalDAVRedirect(c fiber.Ctx) error {
	return c.Redirect().Status(fiber.StatusMovedPermanently).To("/dav/")
}

func WellKnownCardDAVRedirect(c fiber.Ctx) error {
	return c.Redirect().Status(fiber.StatusMovedPermanently).To("/dav/")
}
