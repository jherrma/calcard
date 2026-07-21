package webdav

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

// CalDAVBackend implements caldav.Backend
type CalDAVBackend struct {
	calendarRepo calendar.CalendarRepository
	userRepo     user.UserRepository
	shareRepo    sharing.CalendarShareRepository
}

func NewCalDAVBackend(
	calendarRepo calendar.CalendarRepository,
	userRepo user.UserRepository,
	shareRepo sharing.CalendarShareRepository,
) *CalDAVBackend {
	return &CalDAVBackend{
		calendarRepo: calendarRepo,
		userRepo:     userRepo,
		shareRepo:    shareRepo,
	}
}

// CurrentUserPrincipal returns the path to the current user's principal resource.
// Depth 1: /dav/username/
func (b *CalDAVBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return "", webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}
	return fmt.Sprintf("/dav/%s/", u.Username), nil
}

// CalendarHomeSetPath returns the path to the current user's calendar home set.
// Depth 2: /dav/username/calendars/
func (b *CalDAVBackend) CalendarHomeSetPath(ctx context.Context) (string, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return "", webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}
	return fmt.Sprintf("/dav/%s/calendars/", u.Username), nil
}

func (b *CalDAVBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	// 1. Get owned calendars
	owned, err := b.calendarRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	// 2. Get shared calendars
	shared, err := b.shareRepo.FindCalendarsSharedWithUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	res := make([]caldav.Calendar, 0, len(owned)+len(shared))
	for _, c := range owned {
		res = append(res, *b.mapCalendar(u.Username, c, calendar.PermissionOwner))
	}
	for _, s := range shared {
		// Defense in depth: skip a share whose calendar no longer exists
		// (zero-valued preload) so it never surfaces as a blank ghost entry.
		if s.Calendar.ID == 0 {
			continue
		}
		perm := calendar.PermissionRead
		if s.Permission == "read-write" {
			perm = calendar.PermissionReadWrite
		}
		res = append(res, *b.mapCalendar(u.Username, &s.Calendar, perm))
	}

	return res, nil
}

func (b *CalDAVBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	// Single resolver so owned/shared resolution (and UUID-vs-legacy-path
	// matching) can never diverge between GetCalendar and ResolvePath.
	c, u, perm, err := b.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}
	return b.mapCalendar(u.Username, c, perm), nil
}

func (b *CalDAVBackend) CreateCalendar(ctx context.Context, cal *caldav.Calendar) error {
	u, ok := UserFromContext(ctx)
	if !ok {
		return webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	// Path: /dav/username/calendars/calname/
	parts := strings.Split(strings.Trim(cal.Path, "/"), "/")
	if len(parts) != 4 || parts[1] != u.Username || parts[2] != "calendars" {
		return webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	calPath := parts[3]
	c := &calendar.Calendar{
		UUID:                uuid.New().String(),
		UserID:              u.ID,
		Path:                calPath,
		Name:                cal.Name,
		Description:         cal.Description,
		Color:               "#3788d8",
		Timezone:            "UTC",
		SupportedComponents: "VEVENT,VTODO",
	}
	// SyncToken/CTag are minted by Create together with a change-log anchor row.
	return b.calendarRepo.Create(ctx, c)
}

func (b *CalDAVBackend) GetCalendarObject(ctx context.Context, p string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	c, _, perm, err := b.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}

	// A request without an object segment ("/dav/{user}/calendars/{cal}/")
	// targets the collection, not an object — there is no object to GET.
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 5 {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}
	objPath := parts[4]

	obj, err := b.calendarRepo.GetCalendarObjectByPath(ctx, c.ID, objPath)
	if err != nil || obj == nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	// Build ACL based on perm
	// Permission check happens before mapping the object.

	if perm == calendar.PermissionNone {
		return nil, webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	return b.mapCalendarObject(p, obj)
}

func (b *CalDAVBackend) ListCalendarObjects(ctx context.Context, p string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	// Trim trailing slash for ResolvePath compatibility if needed, or handle it
	c, _, perm, err := b.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}

	if perm == calendar.PermissionNone {
		return nil, webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	objects, err := b.calendarRepo.GetCalendarObjects(ctx, c.ID)
	if err != nil {
		return nil, err
	}

	res := make([]caldav.CalendarObject, 0, len(objects))
	for _, obj := range objects {
		// Join path correctly
		objUrl := path.Join(p, obj.Path)
		// Ensure it starts with /dav/ if p didn't
		if !strings.HasPrefix(objUrl, "/dav/") {
			// This shouldn't happen given standard usage, but let's be safe
		}

		co, err := b.mapCalendarObject(objUrl, obj)
		if err == nil {
			res = append(res, *co)
		}
	}

	return res, nil
}

func (b *CalDAVBackend) QueryCalendarObjects(ctx context.Context, p string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	all, err := b.ListCalendarObjects(ctx, p, &query.CompRequest)
	if err != nil {
		return nil, err
	}
	// Apply the query's CompFilter (e.g. VEVENT time-range) so that clients
	// like Apple Calendar / Thunderbird / DAVx5 only see the events that fall
	// inside the window they requested. Without this, every calendar-query
	// REPORT effectively behaves like an unfiltered list.
	return caldav.Filter(query, all)
}

func (b *CalDAVBackend) PutCalendarObject(ctx context.Context, p string, icalCal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	c, _, perm, err := b.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}

	// Check Write Permission
	if perm != calendar.PermissionOwner && perm != calendar.PermissionReadWrite {
		return nil, webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	// A PUT without an object segment ("/dav/{user}/calendars/{cal}/")
	// targets the collection itself, which is not a writable resource.
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 5 {
		return nil, webdav.NewHTTPError(http.StatusMethodNotAllowed, nil)
	}
	objPath := parts[4]

	_, uid, err := caldav.ValidateCalendarObject(icalCal)
	if err != nil {
		return nil, err
	}

	// Fix up missing required properties
	if icalCal.Props.Get(ical.PropProductID) == nil {
		icalCal.Props.SetText(ical.PropProductID, "-//CalCard//EN")
	}
	if icalCal.Props.Get(ical.PropVersion) == nil {
		icalCal.Props.SetText(ical.PropVersion, "2.0")
	}

	for _, comp := range icalCal.Children {
		if comp.Name == ical.CompEvent || comp.Name == ical.CompToDo {
			if comp.Props.Get(ical.PropDateTimeStamp) == nil {
				comp.Props.SetDateTime(ical.PropDateTimeStamp, time.Now())
			}
		}
	}

	// A swallowed lookup error would make a transient DB failure look like a
	// missing object, turning what should be an update into a create (and
	// skipping the UID-change / no-uid-conflict checks below). Only a genuine
	// "not found" means we should proceed as a create.
	existing, err := b.calendarRepo.GetCalendarObjectByPath(ctx, c.ID, objPath)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Honor If-Match / If-None-Match preconditions (RFC 4791 §5.3.4). Without
	// these, two concurrent clients would silently overwrite each other.
	if opts != nil {
		if opts.IfNoneMatch.IsSet() && existing != nil {
			// If-None-Match: * (or any) with an existing resource → 412.
			return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, nil)
		}
		if opts.IfMatch.IsSet() {
			if existing == nil {
				return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, nil)
			}
			ok, _ := opts.IfMatch.MatchETag(existing.ETag)
			if !ok {
				return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, nil)
			}
		}
	}

	// A PUT must not change an existing resource's UID. RFC 4791 binds the UID
	// to the resource for its lifetime; silently accepting a different UID would
	// leave the stored uid column, the no-uid-conflict lookup, and the sync log
	// stale, letting a later resource legitimately reuse the old UID and produce
	// two objects with the same UID. Reject rather than silently rename.
	if existing != nil && existing.UID != uid {
		return nil, caldav.NewPreconditionError(caldav.PreconditionNoUIDConflict)
	}

	// no-uid-conflict (RFC 4791 §5.3.2): a different resource in this calendar
	// must not already own this UID.
	other, err := b.calendarRepo.GetCalendarObjectByUID(ctx, c.ID, uid)
	if err != nil {
		return nil, err
	}
	if other != nil && other.Path != objPath {
		return nil, caldav.NewPreconditionError(caldav.PreconditionNoUIDConflict)
	}

	var icalData strings.Builder
	if err := ical.NewEncoder(&icalData).Encode(icalCal); err != nil {
		return nil, err
	}
	data := icalData.String()
	etag := calendar.NewETag()

	var obj *calendar.CalendarObject
	if existing != nil {
		existing.ICalData = data
		existing.ETag = etag
		// Rederive all denormalized columns (component type, times,
		// recurrence_end_time, content length) from the stored data.
		if err := existing.PopulateDenormFieldsFromICal(); err != nil {
			return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
		}
		if err := b.calendarRepo.UpdateCalendarObject(ctx, existing); err != nil {
			return nil, err
		}
		obj = existing
	} else {
		newObj := &calendar.CalendarObject{
			UUID:       uuid.New().String(),
			CalendarID: c.ID,
			Path:       objPath,
			UID:        uid,
			ETag:       etag,
			ICalData:   data,
		}
		if err := newObj.PopulateDenormFieldsFromICal(); err != nil {
			return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
		}
		if err := b.calendarRepo.CreateCalendarObject(ctx, newObj); err != nil {
			return nil, err
		}
		obj = newObj
	}

	return b.mapCalendarObject(p, obj)
}

func (b *CalDAVBackend) DeleteCalendarObject(ctx context.Context, p string) error {
	c, _, perm, err := b.ResolvePath(ctx, p)
	if err != nil {
		return err
	}

	// Check Write Permission
	if perm != calendar.PermissionOwner && perm != calendar.PermissionReadWrite {
		return webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	// emersion/go-webdav dispatches every DELETE through this method,
	// including DELETE on the calendar collection itself. Detect that
	// case by path shape ("/dav/{user}/calendars/{cal}/" has no object
	// segment) and delete the whole calendar. Only the owner may.
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 5 {
		if perm != calendar.PermissionOwner {
			return webdav.NewHTTPError(http.StatusForbidden, nil)
		}
		if err := b.calendarRepo.Delete(ctx, c.ID); err != nil {
			return err
		}
		// Revoke every share of this calendar so it doesn't linger as a ghost
		// entry in the sharees' calendar lists (mirrors the REST delete path).
		if err := b.shareRepo.DeleteByCalendarID(ctx, c.ID); err != nil {
			return err
		}
		return nil
	}

	objPath := parts[4]

	obj, err := b.calendarRepo.GetCalendarObjectByPath(ctx, c.ID, objPath)
	if err != nil || obj == nil {
		return webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	if err := b.calendarRepo.DeleteCalendarObject(ctx, obj); err != nil {
		return err
	}

	return nil
}

func (b *CalDAVBackend) GetCalendarObjectByPath(ctx context.Context, calendarID uint, path string) (*calendar.CalendarObject, error) {
	return b.calendarRepo.GetCalendarObjectByPath(ctx, calendarID, path)
}

// ResolvePath resolves a path to a calendar, user, and permission
func (b *CalDAVBackend) ResolvePath(ctx context.Context, p string) (*calendar.Calendar, *user.User, calendar.CalendarPermission, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, nil, calendar.PermissionNone, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 4 || parts[0] != "dav" || parts[1] != u.Username || parts[2] != "calendars" {
		return nil, nil, calendar.PermissionNone, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	calPath := parts[3]

	// 1. Try owned calendar
	c, err := b.calendarRepo.GetByPath(ctx, u.ID, calPath)
	if err == nil && c != nil {
		return c, u, calendar.PermissionOwner, nil
	}

	// 2. Try shared calendar
	// We need to fetch ID to check permission, but GetByPath failed for user.
	// So we need to look up shared calendars by path.
	// This is inefficient loop, but acceptable for MVP.
	shared, err := b.shareRepo.FindCalendarsSharedWithUser(ctx, u.ID)
	if err != nil {
		return nil, nil, calendar.PermissionNone, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	for _, s := range shared {
		// Match by UUID (the path shares are now advertised under) or by the
		// legacy owner path, so clients still connected to the old URL don't 404
		// mid-flight. An owned calendar still shadows a colliding legacy path
		// (step 1 above wins) — exactly today's behavior.
		if s.Calendar.UUID == calPath || s.Calendar.Path == calPath {
			perm := calendar.PermissionRead
			if s.Permission == "read-write" {
				perm = calendar.PermissionReadWrite
			}
			// Preload of Calendar is expected in FindCalendarsSharedWithUser
			return &s.Calendar, u, perm, nil
		}
	}

	return nil, nil, calendar.PermissionNone, webdav.NewHTTPError(http.StatusNotFound, nil)
}

func (b *CalDAVBackend) GetSyncChanges(ctx context.Context, calendarPath, token string) ([]*calendar.SyncChangeLog, string, error) {
	c, _, perm, err := b.ResolvePath(ctx, calendarPath)
	if err != nil {
		return nil, "", err
	}

	if perm == calendar.PermissionNone {
		return nil, "", webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	changes, err := b.calendarRepo.GetChangesSinceToken(ctx, c.ID, token)
	if err != nil {
		return nil, "", err
	}

	return changes, c.SyncToken, nil
}

func (b *CalDAVBackend) mapCalendar(username string, c *calendar.Calendar, permission calendar.CalendarPermission) *caldav.Calendar {
	// Set Description
	desc := c.Description
	if desc == "" {
		if permission == calendar.PermissionOwner {
			desc = "My Calendar"
		} else {
			desc = "Shared Calendar"
			if c.Owner.Username != "" {
				desc = fmt.Sprintf("Shared Calendar (%s)", c.Owner.Username)
			}
		}
	}

	pathSeg := c.Path
	if permission != calendar.PermissionOwner {
		// Shared calendars are addressed by UUID: the owner's path segment can
		// collide with one of the recipient's own paths (or another share's),
		// and both would then occupy the same DAV URL — making the share
		// unreachable and misrouting writes into the owned calendar. UUIDs are
		// globally unique, so collisions become impossible.
		pathSeg = c.UUID
	}

	return &caldav.Calendar{
		Path:                  fmt.Sprintf("/dav/%s/calendars/%s/", username, pathSeg),
		Name:                  c.Name,
		Description:           desc,
		SupportedComponentSet: []string{"VEVENT", "VTODO"},
	}
}

func (b *CalDAVBackend) mapCalendarObject(p string, obj *calendar.CalendarObject) (*caldav.CalendarObject, error) {
	cal, err := ical.NewDecoder(strings.NewReader(obj.ICalData)).Decode()
	if err != nil {
		return nil, err
	}
	return &caldav.CalendarObject{
		Path:          p,
		Data:          cal,
		ETag:          obj.ETag,
		ContentLength: int64(obj.ContentLength),
		ModTime:       obj.UpdatedAt,
	}, nil
}
