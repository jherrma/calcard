package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	eventusecase "github.com/jherrma/caldav-server/internal/usecase/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eventNotFoundBody is the 404 an unknown event gets on every event route.
const eventNotFoundBody = `{"status":"error","message":"Event not found"}`

// taskICal is a task as a CalDAV client (Apple Reminders, Tasks.org) writes it.
func taskICal(uid string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\n" +
		"UID:" + uid + "\r\nDTSTAMP:20300101T000000Z\r\nDUE:20300115T100000Z\r\n" +
		"SUMMARY:Renew passport\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
}

// storeTask writes a task into calendarID the way a CalDAV PUT does.
func storeTask(t *testing.T, db database.Database, calendarID uint, uid string) *calendar.CalendarObject {
	t.Helper()
	obj := &calendar.CalendarObject{
		UUID:       uid + "-uuid",
		CalendarID: calendarID,
		Path:       uid + ".ics",
		UID:        uid,
		ETag:       calendar.NewETag(),
		ICalData:   taskICal(uid),
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.Equal(t, calendar.ComponentToDo, obj.ComponentType)
	require.NoError(t, repository.NewCalendarRepository(db.DB()).CreateCalendarObject(context.Background(), obj))
	return obj
}

// objectRow renders everything a write could change about a stored object,
// soft deletion included.
func objectRow(t *testing.T, db database.Database, uuid string) string {
	t.Helper()
	var obj calendar.CalendarObject
	require.NoError(t, db.DB().Unscoped().Where("uuid = ?", uuid).First(&obj).Error)
	return fmt.Sprintf("calendar=%d etag=%s updated=%s deleted=%v ical=%q",
		obj.CalendarID, obj.ETag, obj.UpdatedAt.UTC().Format(time.RFC3339Nano), obj.DeletedAt.Valid, obj.ICalData)
}

// sendEventRequest sends one request to the event routes and returns status
// and body.
func sendEventRequest(t *testing.T, app *fiber.App, token, method, path, body string, header map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, "/api/v1/calendars/"+path, bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// TestEventHandlerTaskIsNotAnEvent (#170, spec §3.1 "Event endpoints do not
// serve tasks"): GET, PATCH, DELETE and move of a task's UUID on the event
// routes answer exactly what an unknown event gets — 404 "Event not found" —
// and leave the task alone. Before, GET returned the task as an event and a
// PATCH reached the update use case and answered 500 ("no VEVENT found").
//
// The 404 comes before every other check, so the task is indistinguishable
// from an unknown event whatever else is wrong with the request: a read-only
// share (would be 403), a task list (400 "does not store events"), a stale
// If-Match (412), a broken body (400), a bad delete scope (400).
func TestEventHandlerTaskIsNotAnEvent(t *testing.T) {
	app, db, owner, mixed, ownerToken := setupEventHandlerTest(t)
	defer db.Close()
	ctx := context.Background()

	sharee := &user.User{UUID: "sharee-uuid", Email: "sharee@example.com", Username: "sharee", IsActive: true}
	require.NoError(t, repository.NewUserRepository(db.DB()).Create(ctx, sharee))
	jwt := authadapter.NewJWTManager(&config.JWTConfig{Secret: "test-secret", AccessExpiry: time.Hour})
	shareeToken, _, err := jwt.GenerateAccessToken(sharee.UUID, sharee.Email)
	require.NoError(t, err)

	newCalendar := func(uuid, components string) *calendar.Calendar {
		cal := &calendar.Calendar{UUID: uuid, UserID: owner.ID, Name: uuid, Path: uuid, SupportedComponents: components}
		require.NoError(t, db.DB().Create(cal).Error)
		return cal
	}
	share := func(cal *calendar.Calendar, permission string) {
		require.NoError(t, db.DB().Create(&sharing.CalendarShare{
			UUID: cal.UUID + "-share", CalendarID: cal.ID, SharedWithID: sharee.ID, Permission: permission,
		}).Error)
	}
	taskList := newCalendar("task-list-uuid", "VTODO")
	eventCalendar := newCalendar("event-calendar-uuid", "VEVENT")
	readOnly := newCalendar("read-only-uuid", "VEVENT,VTODO")
	share(readOnly, "read")
	readWrite := newCalendar("read-write-uuid", "VEVENT,VTODO")
	share(readWrite, "read-write")

	summary := `{"summary":"Hijacked"}`
	for _, tc := range []struct {
		name   string
		token  string
		cal    *calendar.Calendar
		target *calendar.Calendar // a calendar the caller may write and that stores events
	}{
		{"owner, legacy mixed calendar", ownerToken, mixed, eventCalendar},
		{"owner, task list", ownerToken, taskList, eventCalendar},
		{"read-only sharee", shareeToken, readOnly, readWrite},
		{"read-write sharee", shareeToken, readWrite, readWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := storeTask(t, db, tc.cal.ID, "task-in-"+tc.cal.UUID)
			before := objectRow(t, db, task.UUID)

			// The reference answer: an event UUID that does not exist, asked by
			// the same caller under the same calendar.
			code, unknownBody := sendEventRequest(t, app, tc.token, "GET", tc.cal.UUID+"/events/00000000-0000-4000-8000-000000000001", "", nil)
			require.Equal(t, fiber.StatusNotFound, code)
			require.JSONEq(t, eventNotFoundBody, unknownBody)

			base := tc.cal.UUID + "/events/" + task.UUID
			moveTo := func(target *calendar.Calendar) string {
				b, _ := json.Marshal(dto.MoveEventRequest{TargetCalendarID: target.UUID})
				return string(b)
			}
			for _, req := range []struct {
				method, path, body string
				header             map[string]string
			}{
				{"GET", base, "", nil},
				{"PATCH", base, summary, nil},
				{"PATCH", base, summary, map[string]string{"If-Match": `"stale"`}},
				{"PATCH", base, summary, map[string]string{"If-Match": `"` + task.ETag + `"`}},
				{"PATCH", base, `{"summary":`, nil},
				{"PATCH", base + "?scope=this&recurrence_id=20300115T100000Z", summary, nil},
				{"DELETE", base, "", nil},
				{"DELETE", base, "", map[string]string{"If-Match": `"stale"`}},
				{"DELETE", base + "?scope=this&recurrence_id=20300115T100000Z", "", nil},
				{"DELETE", base + "?scope=future", "", nil},
				{"POST", base + "/move", moveTo(tc.target), nil},
				{"POST", base + "/move", moveTo(tc.cal), nil},
				{"POST", base + "/move", moveTo(taskList), nil},
				{"POST", base + "/move", `{"target_calendar_id":`, nil},
			} {
				code, body := sendEventRequest(t, app, tc.token, req.method, req.path, req.body, req.header)
				assert.Equal(t, fiber.StatusNotFound, code, "%s %s %v", req.method, req.path, req.header)
				assert.JSONEq(t, unknownBody, body, "%s %s %v must look exactly like an unknown event", req.method, req.path, req.header)
			}

			assert.Equal(t, before, objectRow(t, db, task.UUID), "the task must be neither changed, moved nor deleted")
		})
	}

	t.Run("events keep working next to a task", func(t *testing.T) {
		storeTask(t, db, mixed.ID, "neighbour-task")
		code, raw := sendEventRequest(t, app, ownerToken, "POST", mixed.UUID+"/events",
			`{"summary":"Standup","start":"2030-01-15T10:00:00Z","end":"2030-01-15T11:00:00Z"}`, nil)
		require.Equal(t, fiber.StatusCreated, code, raw)
		var ev dto.EventResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &ev))
		base := mixed.UUID + "/events/" + ev.ID

		code, raw = sendEventRequest(t, app, ownerToken, "GET", base, "", nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		code, raw = sendEventRequest(t, app, ownerToken, "PATCH", base, `{"summary":"Daily standup"}`, nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		require.NoError(t, json.Unmarshal([]byte(raw), &ev))
		assert.Equal(t, "Daily standup", ev.Summary)
		code, raw = sendEventRequest(t, app, ownerToken, "POST", base+"/move", `{"target_calendar_id":"`+eventCalendar.UUID+`"}`, nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		code, raw = sendEventRequest(t, app, ownerToken, "DELETE", eventCalendar.UUID+"/events/"+ev.ID, "", nil)
		require.Equal(t, fiber.StatusNoContent, code, raw)
	})
}

// becomesTaskRepo hands the event use cases every object as a VTODO: the
// state a CalDAV PUT leaves when it rewrites an event's resource with a task
// (the UUID is kept) after the handler's eventPermission looked at it.
type becomesTaskRepo struct{ calendar.CalendarRepository }

func (r becomesTaskRepo) GetCalendarObjectByUUID(ctx context.Context, uuid string) (*calendar.CalendarObject, error) {
	obj, err := r.CalendarRepository.GetCalendarObjectByUUID(ctx, uuid)
	if err == nil && obj != nil {
		obj.ComponentType = calendar.ComponentToDo
	}
	return obj, err
}

// TestEventHandlerUseCaseNotFoundIs404: when the use case, not the handler's
// own check, finds a task (event.ErrNotFound), PATCH, DELETE and move still
// answer 404 "Event not found" — never the 500 of an unexpected error — and
// write nothing.
func TestEventHandlerUseCaseNotFoundIs404(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	code, raw := sendEventRequest(t, app, token, "POST", cal.UUID+"/events",
		`{"summary":"Standup","start":"2030-01-15T10:00:00Z","end":"2030-01-15T11:00:00Z"}`, nil)
	require.Equal(t, fiber.StatusCreated, code, raw)
	var ev dto.EventResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &ev))
	other := &calendar.Calendar{UUID: "other-uuid", UserID: cal.UserID, Name: "Other", Path: "other"}
	require.NoError(t, db.DB().Create(other).Error)
	before := objectRow(t, db, ev.ID)

	// The same routes, with use cases that see the event turned into a task.
	realRepo := repository.NewCalendarRepository(db.DB())
	racedRepo := becomesTaskRepo{realRepo}
	handler := NewEventHandler(
		eventusecase.NewListEventsUseCase(realRepo),
		eventusecase.NewGetEventUseCase(racedRepo),
		eventusecase.NewCreateEventUseCase(realRepo),
		eventusecase.NewUpdateEventUseCase(racedRepo),
		eventusecase.NewDeleteEventUseCase(racedRepo),
		eventusecase.NewMoveEventUseCase(racedRepo),
		realRepo,
	)
	raced := fiber.New()
	jwt := authadapter.NewJWTManager(&config.JWTConfig{Secret: "test-secret", AccessExpiry: time.Hour})
	events := raced.Group("/api/v1").Group("/calendars", Authenticate(jwt, repository.NewUserRepository(db.DB()))).Group("/:calendar_id/events")
	events.Get("/:event_id", handler.Get)
	events.Patch("/:event_id", handler.Update)
	events.Delete("/:event_id", handler.Delete)
	events.Post("/:event_id/move", handler.Move)

	base := cal.UUID + "/events/" + ev.ID
	for _, req := range []struct{ method, path, body string }{
		{"GET", base, ""},
		{"PATCH", base, `{"summary":"Hijacked"}`},
		{"DELETE", base, ""},
		{"POST", base + "/move", `{"target_calendar_id":"` + other.UUID + `"}`},
	} {
		code, body := sendEventRequest(t, raced, token, req.method, req.path, req.body, nil)
		assert.Equal(t, fiber.StatusNotFound, code, "%s %s", req.method, req.path)
		assert.JSONEq(t, eventNotFoundBody, body, "%s %s", req.method, req.path)
	}
	assert.Equal(t, before, objectRow(t, db, ev.ID), "nothing may be written")
}
