package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
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

// alarmTestAcks is the acknowledgement repository of alarmTestApp; tests that
// need a failure or to inspect writes replace its fields (reset by alarmTestApp).
var alarmTestAcks = &alarmAckFake{}

type alarmAckFake struct {
	calendar.AlarmAcknowledgementRepository
	err   error
	users []uint
	keys  [][]string
}

func (f *alarmAckFake) Acknowledge(_ context.Context, userID uint, keys []string, _ time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.users = append(f.users, userID)
	f.keys = append(f.keys, keys)
	return nil
}

func (f *alarmAckFake) AcknowledgedKeys(context.Context, uint, []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func alarmTestApp(repo alarmTestCalendars, withUser bool) *fiber.App {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	*alarmTestAcks = alarmAckFake{}
	uc := alarmuc.NewUpcomingUseCase(repo, alarmTestAcks, ownerzone.New(alarmTestPrefs{}, alarmTestUsers{}), func() time.Time { return now })
	h := NewAlarmHandler(uc, alarmuc.NewAcknowledgeUseCase(alarmTestAcks, func() time.Time { return now }))
	app := fiber.New()
	if withUser {
		app.Use(func(c fiber.Ctx) error { c.Locals("user_id", uint(1)); return c.Next() })
	}
	app.Get("/api/v1/alarms/upcoming", h.Upcoming)
	app.Post("/api/v1/alarms/acknowledge", h.Acknowledge)
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

func alarmPost(t *testing.T, app *fiber.App, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/alarms/acknowledge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAlarmHandlerAcknowledge(t *testing.T) {
	key := strings.Repeat("ab", 16)

	t.Run("204 with an empty body, rows for the caller", func(t *testing.T) {
		app := alarmTestApp(alarmTestCalendars{}, true)
		status, body := alarmPost(t, app, `{"keys":["`+key+`","`+key+`"]}`)
		assert.Equal(t, 204, status)
		assert.Empty(t, body)
		assert.Equal(t, []uint{1}, alarmTestAcks.users)
		assert.Equal(t, [][]string{{key}}, alarmTestAcks.keys, "duplicates collapse")
	})

	bad := func(msg string) string { return `{"status":"error","message":"` + msg + `"}` }
	for name, tc := range map[string]struct{ body, msg string }{
		"invalid json":  {`{"keys":`, "Invalid request body"},
		"wrong type":    {`{"keys":"x"}`, "Invalid request body"},
		"missing keys":  {`{}`, "keys is required"},
		"empty keys":    {`{"keys":[]}`, "keys is required"},
		"null keys":     {`{"keys":null}`, "keys is required"},
		"uppercase key": {`{"keys":["` + key + `","` + strings.ToUpper(key) + `"]}`, "Invalid alarm key at index 1"},
		"short key":     {`{"keys":["` + key[1:] + `"]}`, "Invalid alarm key at index 0"},
		"too many keys": {`{"keys":[` + strings.TrimSuffix(strings.Repeat(`"`+key+`",`, 501), ",") + `]}`, "Too many keys: at most 500 per request"},
	} {
		t.Run("400 "+name, func(t *testing.T) {
			app := alarmTestApp(alarmTestCalendars{}, true)
			status, body := alarmPost(t, app, tc.body)
			assert.Equal(t, 400, status)
			assert.JSONEq(t, bad(tc.msg), body)
			assert.Empty(t, alarmTestAcks.keys)
		})
	}

	t.Run("401 without a user", func(t *testing.T) {
		status, _ := alarmPost(t, alarmTestApp(alarmTestCalendars{}, false), `{"keys":["`+key+`"]}`)
		assert.Equal(t, 401, status)
	})

	t.Run("500 does not echo the cause", func(t *testing.T) {
		app := alarmTestApp(alarmTestCalendars{}, true)
		alarmTestAcks.err = errors.New("pq: relation secret_table")
		status, body := alarmPost(t, app, `{"keys":["`+key+`"]}`)
		assert.Equal(t, 500, status)
		assert.JSONEq(t, bad("Internal server error"), body)
	})
}
