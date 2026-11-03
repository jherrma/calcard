package http

import (
	gowebdav "github.com/emersion/go-webdav"
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// Access helpers shared by the routes that address a calendar object under
// /api/v1/calendars/:calendar_id/… — events and tasks (#170) follow exactly
// the same permission, 404 and If-Match rules, so they are written once here
// and take the repository as a parameter instead of living on one handler.

// resolveCalendar maps the :calendar_id path segment — a calendar UUID, the
// canonical external identifier (#52) — to the calendar. Returns (nil, false)
// when the UUID doesn't resolve; callers turn that into a 404 so existence
// isn't leaked and behavior matches an unshared/unknown calendar.
func resolveCalendar(c fiber.Ctx, repo calendar.CalendarRepository) (*calendar.Calendar, bool) {
	cal, err := repo.GetByUUID(c.Context(), c.Params("calendar_id"))
	if err != nil || cal == nil {
		return nil, false
	}
	return cal, true
}

// calendarPermission returns the authenticated user's effective permission on
// the calendar with the given numeric id. GetUserPermission already resolves
// ownership (PermissionOwner) and shares (PermissionRead / PermissionReadWrite),
// and returns PermissionNone when the calendar is missing or unshared — which
// callers treat exactly like "not found" (404) so existence isn't leaked.
func calendarPermission(c fiber.Ctx, repo calendar.CalendarRepository, calendarID uint) calendar.CalendarPermission {
	userID := c.Locals("user_id").(uint)
	perm, err := repo.GetUserPermission(c.Context(), calendarID, userID)
	if err != nil {
		return calendar.PermissionNone
	}
	if perm == calendar.PermissionNone {
		return perm
	}
	// A subscribed calendar (story 100) mirrors a remote feed and is read-only
	// to everyone, its owner included: the next refresh replaces its contents
	// wholesale, so a write accepted here would vanish without a trace. Load
	// the calendar only once we know the caller can see it at all, so this
	// costs nothing on the "no access" path.
	cal, err := repo.GetByID(c.Context(), calendarID)
	if err != nil {
		return calendar.PermissionNone
	}
	return calendar.EffectivePermission(cal, perm)
}

// objectPermission returns the user's permission on the calendar that holds
// the object identified by objectUUID, and that calendar (PermissionNone and
// nil when the object doesn't exist or the calendar is neither owned nor
// shared with the user).
//
// The object must also live in the calendar named by the :calendar_id path
// segment (F09): an unknown calendar, an unknown object and an object filed
// under a different calendar all yield PermissionNone, so callers answer one
// identical 404 and the URL cannot be used to probe which calendar holds an
// object.
//
// So does an object of the wrong kind (#170): isKind is the route's own test,
// (*calendar.CalendarObject).IsEvent on the event routes and IsTask on the
// task routes, so a task's UUID is an unknown event and an event's UUID an
// unknown task — before any permission, If-Match or body check, so a
// read-only sharee gets the same 404 and not a 403.
func objectPermission(c fiber.Ctx, repo calendar.CalendarRepository, objectUUID string, isKind func(*calendar.CalendarObject) bool) (calendar.CalendarPermission, *calendar.Calendar) {
	cal, ok := resolveCalendar(c, repo)
	if !ok {
		return calendar.PermissionNone, nil
	}
	obj, err := repo.GetCalendarObjectByUUID(c.Context(), objectUUID)
	if err != nil || !isKind(obj) || obj.CalendarID != cal.ID {
		return calendar.PermissionNone, nil
	}
	perm := calendarPermission(c, repo, obj.CalendarID)
	if perm == calendar.PermissionNone {
		return perm, nil
	}
	return perm, cal
}

// canWrite reports whether a permission grants write access (owner or
// read-write). Read access is any permission other than PermissionNone.
func canWrite(p calendar.CalendarPermission) bool {
	return p == calendar.PermissionOwner || p == calendar.PermissionReadWrite
}

// ifMatchOK enforces an optional If-Match precondition on REST writes, giving
// the web UI the same optimistic-concurrency guard the DAV PUT/DELETE path has
// (two concurrent sessions could otherwise silently clobber each other). It
// reuses the DAV path's ConditionalMatch parser/comparer. When the client sends
// no If-Match the write stays unconditional (legacy behavior). It returns
// (true, nil) to proceed; on a mismatch it returns (false, <412 response>),
// which the caller must return. A vanished object falls through so the usecase
// still produces its normal not-found result.
func ifMatchOK(c fiber.Ctx, repo calendar.CalendarRepository, objectUUID string) (bool, error) {
	match := gowebdav.ConditionalMatch(c.Get("If-Match"))
	if !match.IsSet() {
		return true, nil
	}
	obj, err := repo.GetCalendarObjectByUUID(c.Context(), objectUUID)
	if err != nil || obj == nil {
		return true, nil
	}
	if ok, _ := match.MatchETag(obj.ETag); !ok {
		return false, ErrorResponse(c, fiber.StatusPreconditionFailed, "ETag precondition failed")
	}
	return true, nil
}
