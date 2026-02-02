package webdav

import (
	"context"
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
		perm := calendar.PermissionRead
		if s.Permission == "read-write" {
			perm = calendar.PermissionReadWrite
		}
		res = append(res, *b.mapCalendar(u.Username, &s.Calendar, perm))
	}

	return res, nil
}

func (b *CalDAVBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	// Path: /dav/username/calendars/calname/
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 4 || parts[0] != "dav" || parts[1] != u.Username || parts[2] != "calendars" {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	calPath := parts[3]
	c, err := b.calendarRepo.GetByPath(ctx, u.ID, calPath)

	// If not found in owned, check shared
	var perm calendar.CalendarPermission
	if err != nil || c == nil {
		// Try to find if it's a shared calendar.
		// Since GetByPath checks user_id, it won't find shared calendars.
		// We iterate shared calendars to find a match by path.
		// Note: Potential name collisions are not yet handled; first match wins.
		shared, err := b.shareRepo.FindCalendarsSharedWithUser(ctx, u.ID)
		if err != nil {
			return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
		}
		found := false
		for _, s := range shared {
			if s.Calendar.Path == calPath {
				c = &s.Calendar
				if s.Permission == "read-write" {
					perm = calendar.PermissionReadWrite
				} else {
					perm = calendar.PermissionRead
				}
				found = true
				break
			}
		}
		if !found {
			return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
		}
	} else {
		perm = calendar.PermissionOwner
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
	c.UpdateSyncTokens()

	return b.calendarRepo.Create(ctx, c)
}

func (b *CalDAVBackend) GetCalendarObject(ctx context.Context, p string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	c, _, perm, err := b.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
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
	return b.ListCalendarObjects(ctx, p, &query.CompRequest)
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

	parts := strings.Split(strings.Trim(p, "/"), "/")
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

	existing, _ := b.calendarRepo.GetCalendarObjectByPath(ctx, c.ID, objPath)

	// Extract metadata
	summary := ""
	var startTime, endTime *time.Time
	for _, comp := range icalCal.Children {
		if comp.Name == ical.CompEvent {
			if prop := comp.Props.Get(ical.PropSummary); prop != nil {
				summary = prop.Value
			}
			if prop := comp.Props.Get(ical.PropDateTimeStart); prop != nil {
				if t, err := prop.DateTime(time.UTC); err == nil {
					startTime = &t
				}
			}
			if prop := comp.Props.Get(ical.PropDateTimeEnd); prop != nil {
				if t, err := prop.DateTime(time.UTC); err == nil {
					endTime = &t
				}
			}
			break
		}
	}

	var icalData strings.Builder
	if err := ical.NewEncoder(&icalData).Encode(icalCal); err != nil {
		return nil, err
	}
	data := icalData.String()
	etag := fmt.Sprintf("\"%s\"", calendar.GenerateSyncToken())

	var obj *calendar.CalendarObject
	if existing != nil {
		existing.ICalData = data
		existing.ETag = etag
		existing.ContentLength = len(data)
		existing.Summary = summary
		existing.StartTime = startTime
		existing.EndTime = endTime
		if err := b.calendarRepo.UpdateCalendarObject(ctx, existing); err != nil {
			return nil, err
		}
		obj = existing
	} else {
		newObj := &calendar.CalendarObject{
			UUID:          uuid.New().String(),
			CalendarID:    c.ID,
			Path:          objPath,
			UID:           uid,
			ETag:          etag,
			ComponentType: "VEVENT",
			ICalData:      data,
			ContentLength: len(data),
			Summary:       summary,
			StartTime:     startTime,
			EndTime:       endTime,
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

	parts := strings.Split(strings.Trim(p, "/"), "/")
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
		if s.Calendar.Path == calPath {
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

	return &caldav.Calendar{
		Path:                  fmt.Sprintf("/dav/%s/calendars/%s/", username, c.Path),
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
