package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	alarmuc "github.com/jherrma/caldav-server/internal/usecase/alarm"
	"github.com/jherrma/caldav-server/internal/usecase/ownerzone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type alarmTestCalendars struct {
	calendar.CalendarRepository
	objects []*calendar.CalendarObject
	err     error
}

func (r alarmTestCalendars) ListReadableCalendarIDs(context.Context, uint) ([]calendar.ReadableCalendar, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []calendar.ReadableCalendar{{ID: 1, UUID: "cal-uuid", Name: "Personal", Color: "#3788d8", OwnerID: 1}}, nil
}

func (r alarmTestCalendars) ListAlarmCandidates(context.Context, []uint, time.Time, time.Time) ([]*calendar.CalendarObject, error) {
	return r.objects, nil
}

type alarmTestPrefs struct{ user.UserPreferenceRepository }

func (alarmTestPrefs) ListByKey(context.Context, []uint, string) ([]user.UserPreference, error) {
	return nil, nil
}

type alarmTestUsers struct{ user.UserRepository }

func (alarmTestUsers) GetByID(context.Context, uint) (*user.User, error) {
	return &user.User{UUID: "u"}, nil
}

func alarmTestApp(repo alarmTestCalendars, withUser bool) *fiber.App {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	uc := alarmuc.NewUpcomingUseCase(repo, ownerzone.New(alarmTestPrefs{}, alarmTestUsers{}), func() time.Time { return now })
	h := NewAlarmHandler(uc)
	app := fiber.New()
	if withUser {
		app.Use(func(c fiber.Ctx) error { c.Locals("user_id", uint(1)); return c.Next() })
	}
	app.Get("/api/v1/alarms/upcoming", h.Upcoming)
	return app
}

func alarmGet(t *testing.T, app *fiber.App, query string) (*fiberResp, []byte) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/alarms/upcoming"+query, nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return &fiberResp{Status: resp.StatusCode, CacheControl: resp.Header.Get("Cache-Control"), ContentType: resp.Header.Get("Content-Type")}, b
}

type fiberResp struct {
	Status       int
	CacheControl string
	ContentType  string
}

func eventObject(start time.Time) *calendar.CalendarObject {
	s := start.UTC().Format("20060102T150405Z")
	e := start.Add(time.Hour).UTC().Format("20060102T150405Z")
	st := start.UTC()
	return &calendar.CalendarObject{ID: 1, UUID: "obj-uuid", CalendarID: 1, UID: "uid-1", ComponentType: "VEVENT", StartTime: &st,
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:uid-1\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:" + s + "\r\nDTEND:" + e +
			"\r\nSUMMARY:Sync\r\nLOCATION:Room 2\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"}
}

func TestAlarmHandlerUpcoming(t *testing.T) {
	start := time.Date(2026, 11, 4, 13, 0, 0, 0, time.UTC)

	t.Run("raw JSON with every nullable key and no-store", func(t *testing.T) {
		app := alarmTestApp(alarmTestCalendars{objects: []*calendar.CalendarObject{eventObject(start)}}, true)
		resp, body := alarmGet(t, app, "")
		require.Equal(t, 200, resp.Status, string(body))
		assert.Equal(t, "no-store", resp.CacheControl)
		assert.Contains(t, resp.ContentType, "application/json")

		var wire map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &wire))
		assert.NotContains(t, wire, "status", "no envelope")
		assert.NotContains(t, wire, "data")
		for _, k := range []string{"alarms", "count", "from", "until", "server_time", "truncated"} {
			assert.Contains(t, wire, k)
		}
		var alarms []map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(wire["alarms"], &alarms))
		require.Len(t, alarms, 1)
		for _, k := range []string{"key", "trigger_at", "uid", "recurrence_id", "alarm_index", "action", "description", "object_type",
			"object_id", "calendar_id", "calendar_uuid", "calendar_name", "calendar_color", "summary", "location", "all_day",
			"start", "start_date", "end", "due", "due_date"} {
			assert.Containsf(t, alarms[0], k, "key %s", k)
		}
		assert.JSONEq(t, `null`, string(alarms[0]["recurrence_id"]))
		assert.JSONEq(t, `null`, string(alarms[0]["description"]))
		assert.JSONEq(t, `"2026-11-04T12:45:00Z"`, string(alarms[0]["trigger_at"]))
		assert.JSONEq(t, `"display"`, string(alarms[0]["action"]))
		assert.JSONEq(t, `"Room 2"`, string(alarms[0]["location"]))
		assert.NotContains(t, string(body), "owner_id")
		assert.NotContains(t, string(body), "user_id")
	})

	t.Run("an empty answer is [] not null", func(t *testing.T) {
		resp, body := alarmGet(t, alarmTestApp(alarmTestCalendars{}, true), "")
		require.Equal(t, 200, resp.Status)
		assert.Contains(t, string(body), `"alarms":[]`)
	})

	t.Run("400 for a bad until", func(t *testing.T) {
		resp, body := alarmGet(t, alarmTestApp(alarmTestCalendars{}, true), "?until=2026-11-05T10:00:00")
		assert.Equal(t, 400, resp.Status)
		assert.JSONEq(t, `{"status":"error","message":"Invalid 'until' value"}`, string(body))
	})

	t.Run("401 without a user", func(t *testing.T) {
		resp, _ := alarmGet(t, alarmTestApp(alarmTestCalendars{}, false), "")
		assert.Equal(t, 401, resp.Status)
	})

	t.Run("500 on a broken repository does not echo the cause", func(t *testing.T) {
		resp, body := alarmGet(t, alarmTestApp(alarmTestCalendars{err: errors.New("pq: relation secret_table")}, true), "")
		assert.Equal(t, 500, resp.Status)
		assert.JSONEq(t, `{"status":"error","message":"Internal server error"}`, string(body))
	})
}
