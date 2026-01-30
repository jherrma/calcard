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
	"github.com/jherrma/caldav-server/internal/domain/user"
)

// CalDAVBackend implements caldav.Backend
type CalDAVBackend struct {
	calendarRepo calendar.CalendarRepository
	userRepo     user.UserRepository
}

func NewCalDAVBackend(calendarRepo calendar.CalendarRepository, userRepo user.UserRepository) *CalDAVBackend {
	return &CalDAVBackend{
		calendarRepo: calendarRepo,
		userRepo:     userRepo,
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

	calendars, err := b.calendarRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	res := make([]caldav.Calendar, len(calendars))
	for i, c := range calendars {
		res[i] = *b.mapCalendar(u.Username, c)
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
	if err != nil || c == nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	return b.mapCalendar(u.Username, c), nil
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
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	// Path: /dav/username/calendars/calname/obj.ics
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[1] != u.Username || parts[2] != "calendars" {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	calPath := parts[3]
	objPath := parts[4]

	c, err := b.calendarRepo.GetByPath(ctx, u.ID, calPath)
	if err != nil || c == nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	obj, err := b.calendarRepo.GetCalendarObjectByPath(ctx, c.ID, objPath)
	if err != nil || obj == nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	return b.mapCalendarObject(p, obj)
}

func (b *CalDAVBackend) ListCalendarObjects(ctx context.Context, p string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 4 || parts[1] != u.Username || parts[2] != "calendars" {
		// If it's the home set (calendars/), we might get this call?
		// Actually go-webdav should handle this.
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	calPath := parts[3]
	c, err := b.calendarRepo.GetByPath(ctx, u.ID, calPath)
	if err != nil || c == nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	objects, err := b.calendarRepo.GetCalendarObjects(ctx, c.ID)
	if err != nil {
		return nil, err
	}

	res := make([]caldav.CalendarObject, 0, len(objects))
	for _, obj := range objects {
		co, err := b.mapCalendarObject(path.Join(p, obj.Path), obj)
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
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[1] != u.Username || parts[2] != "calendars" {
		return nil, webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	calPath := parts[3]
	objPath := parts[4]

	c, err := b.calendarRepo.GetByPath(ctx, u.ID, calPath)
	if err != nil || c == nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

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
	u, ok := UserFromContext(ctx)
	if !ok {
		return webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[1] != u.Username || parts[2] != "calendars" {
		return webdav.NewHTTPError(http.StatusForbidden, nil)
	}

	calPath := parts[3]
	objPath := parts[4]

	c, err := b.calendarRepo.GetByPath(ctx, u.ID, calPath)
	if err != nil || c == nil {
		return webdav.NewHTTPError(http.StatusNotFound, nil)
	}

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

func (b *CalDAVBackend) ResolvePath(ctx context.Context, p string) (*calendar.Calendar, *user.User, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, nil, webdav.NewHTTPError(http.StatusUnauthorized, nil)
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 4 || parts[0] != "dav" || parts[1] != u.Username || parts[2] != "calendars" {
		return nil, nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	calPath := parts[3]
	c, err := b.calendarRepo.GetByPath(ctx, u.ID, calPath)
	if err != nil || c == nil {
		return nil, nil, webdav.NewHTTPError(http.StatusNotFound, nil)
	}

	return c, u, nil
}

func (b *CalDAVBackend) GetSyncChanges(ctx context.Context, calendarPath, token string) ([]*calendar.SyncChangeLog, string, error) {
	c, _, err := b.ResolvePath(ctx, calendarPath)
	if err != nil {
		return nil, "", err
	}

	changes, err := b.calendarRepo.GetChangesSinceToken(ctx, c.ID, token)
	if err != nil {
		return nil, "", err
	}

	return changes, c.SyncToken, nil
}

func (b *CalDAVBackend) mapCalendar(username string, c *calendar.Calendar) *caldav.Calendar {
	return &caldav.Calendar{
		Path:                  fmt.Sprintf("/dav/%s/calendars/%s/", username, c.Path),
		Name:                  c.Name,
		Description:           c.Description,
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
