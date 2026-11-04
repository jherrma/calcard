package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	eventusecase "github.com/jherrma/caldav-server/internal/usecase/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupEventHandlerTest(t *testing.T) (*fiber.App, database.Database, *user.User, *calendar.Calendar, string) {
	dataDir := t.TempDir() // auto-removed at test end

	cfg := &config.Config{
		DataDir: dataDir,
		Database: config.DatabaseConfig{
			Driver: "sqlite",
		},
		JWT: config.JWTConfig{
			Secret:       "test-secret",
			AccessExpiry: time.Hour,
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)

	err = db.Migrate(database.Models()...)
	require.NoError(t, err)

	app := fiber.New()

	userRepo := repository.NewUserRepository(db.DB())
	calendarRepo := repository.NewCalendarRepository(db.DB())
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	u := &user.User{
		UUID:     "user-uuid",
		Email:    "test@example.com",
		Username: "testuser",
		IsActive: true,
	}
	err = userRepo.Create(context.Background(), u)
	require.NoError(t, err)

	cal := &calendar.Calendar{
		UUID:   "cal-uuid",
		UserID: u.ID,
		Name:   "Work",
		Path:   "work",
	}
	err = calendarRepo.Create(context.Background(), cal)
	require.NoError(t, err)

	token, _, _ := jwtManager.GenerateAccessToken(u.UUID, u.Email)

	eventListUC := eventusecase.NewListEventsUseCase(calendarRepo)
	eventGetUC := eventusecase.NewGetEventUseCase(calendarRepo)
	eventCreateUC := eventusecase.NewCreateEventUseCase(calendarRepo)
	eventUpdateUC := eventusecase.NewUpdateEventUseCase(calendarRepo)
	eventDeleteUC := eventusecase.NewDeleteEventUseCase(calendarRepo)
	eventMoveUC := eventusecase.NewMoveEventUseCase(calendarRepo)

	handler := NewEventHandler(eventListUC, eventGetUC, eventCreateUC, eventUpdateUC, eventDeleteUC, eventMoveUC, calendarRepo)

	v1 := app.Group("/api/v1")
	calendars := v1.Group("/calendars", Authenticate(jwtManager, userRepo))
	events := calendars.Group("/:calendar_id/events")
	events.Post("/", handler.Create)
	events.Get("/", handler.List)
	events.Get("/:event_id", handler.Get)
	events.Put("/:event_id", handler.Update)
	events.Patch("/:event_id", handler.Update) // the verb routes.go registers
	events.Delete("/:event_id", handler.Delete)
	events.Post("/:event_id/move", handler.Move)

	return app, db, u, cal, token
}

// TestEventHandlerNilTimes is the regression test for M19: a stored object
// without DTSTART/DTEND must serialize through the REST Get handler without
// panicking on a nil time pointer (which returned a 500).
// eventResponseFromObject now zero-values the missing times. The object used to
// be a VTODO; since #170 a task is not served by the event routes at all
// (TestEventHandlerTaskIsNotAnEvent), so a timeless VEVENT carries the check.
func TestEventHandlerNilTimes(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	// Insert a timeless VEVENT directly — REST Create always supplies times, so
	// such an object can only arrive via CalDAV PUT in production.
	obj := &calendar.CalendarObject{
		UUID:          "timeless-uuid",
		CalendarID:    cal.ID,
		UID:           "timeless-uid",
		Path:          "timeless-uid.ics",
		ComponentType: calendar.ComponentEvent,
		Summary:       "Someday",
		ETag:          "etag-timeless",
		ICalData:      "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:timeless-uid\r\nSUMMARY:Someday\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		// StartTime and EndTime intentionally nil.
	}
	require.NoError(t, db.DB().Create(obj).Error)

	req, _ := http.NewRequest("GET", "/api/v1/calendars/"+cal.UUID+"/events/"+obj.UUID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode, "GET of a timeless event must not 500")

	var res dto.EventResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&res))
	assert.Equal(t, "timeless-uid", res.UID)
	assert.Equal(t, "Someday", res.Summary)
	assert.True(t, res.Start.IsZero(), "missing DTSTART must serialize as a zero time, not panic")
	assert.True(t, res.End.IsZero(), "missing DTEND must serialize as a zero time, not panic")
}

// TestEventHandlerListPopulatesRecurrenceFields is the regression test for #3:
// the List handler previously left is_recurring=false and description/location
// empty on expanded occurrences, so the frontend never showed the recurrence
// scope dialog and a single-occurrence delete wiped the whole series. Every
// expanded instance of a recurring event must now report is_recurring=true and
// carry the master's description/location.
func TestEventHandlerListPopulatesRecurrenceFields(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	start := time.Now().Add(time.Hour * 24).Truncate(time.Second)
	count := 5
	createInput := dto.CreateEventRequest{
		Summary:     "Daily Standup",
		Description: "Team sync",
		Location:    "Room A",
		Start:       start,
		End:         start.Add(time.Hour),
		Recurrence: &dto.RecurrenceRuleDTO{
			Frequency: "DAILY",
			Count:     &count,
		},
	}
	body, _ := json.Marshal(createInput)
	req, _ := http.NewRequest("POST", "/api/v1/calendars/"+cal.UUID+"/events", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	listStart := start.Add(-time.Hour).Format(time.RFC3339)
	listEnd := start.Add(time.Hour * 24 * 10).Format(time.RFC3339)
	listUrl := "/api/v1/calendars/" + cal.UUID + "/events?expand=true&start=" + url.QueryEscape(listStart) + "&end=" + url.QueryEscape(listEnd)
	listReq, _ := http.NewRequest("GET", listUrl, nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp, err := app.Test(listReq)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, listResp.StatusCode)

	var listRes dto.EventListResponse
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&listRes))

	occurrences := 0
	for _, e := range listRes.Events {
		if e.Summary != "Daily Standup" {
			continue
		}
		occurrences++
		assert.True(t, e.IsRecurring, "every expanded occurrence must report is_recurring=true")
		assert.Equal(t, "Team sync", e.Description, "description must be populated on list instances")
		assert.Equal(t, "Room A", e.Location, "location must be populated on list instances")
	}
	assert.Equal(t, count, occurrences, "all %d occurrences should be listed", count)
}

// TestEventHandlerCreateRequiresStartAndEnd is the regression test for F13:
// POST /events without start or end (missing or null) answered 201 and stored
// the event at 0001-01-01. It must be a 400 that stores nothing.
func TestEventHandlerCreateRequiresStartAndEnd(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	for name, body := range map[string]string{
		"no times":    `{"summary":"x"}`,
		"null times":  `{"summary":"x","start":null,"end":null}`,
		"only start":  `{"summary":"x","start":"2030-01-15T10:00:00Z"}`,
		"only end":    `{"summary":"x","end":"2030-01-15T11:00:00Z"}`,
		"end is null": `{"summary":"x","start":"2030-01-15T10:00:00Z","end":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", "/api/v1/calendars/"+cal.UUID+"/events", bytes.NewReader([]byte(body)))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		})
	}

	var count int64
	require.NoError(t, db.DB().Model(&calendar.CalendarObject{}).Count(&count).Error)
	assert.Zero(t, count, "a rejected create must not store an event")
}

// TestEventHandlerCalendarMustStoreEvents (#170, one component-set rule on every
// write path): event create into, update in and move into a calendar that does
// not store VEVENT (a task list, supported_components "VTODO") answer 400
// "This calendar does not store events" and write nothing. A "VEVENT" and a
// legacy "" calendar behave as before.
func TestEventHandlerCalendarMustStoreEvents(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	taskList := &calendar.Calendar{UUID: "task-list-uuid", UserID: cal.UserID, Name: "Tasks", Path: "tasks", SupportedComponents: "VTODO"}
	require.NoError(t, db.DB().Create(taskList).Error)
	eventsOnly := &calendar.Calendar{UUID: "events-only-uuid", UserID: cal.UserID, Name: "Events", Path: "events", SupportedComponents: "VEVENT"}
	require.NoError(t, db.DB().Create(eventsOnly).Error)

	const notStored = `{"status":"error","message":"This calendar does not store events"}`
	send := func(method, path string, body any) (int, string) {
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, "/api/v1/calendars/"+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}
	create := func(calUUID, summary string) dto.EventResponse {
		code, raw := send("POST", calUUID+"/events", dto.CreateEventRequest{
			Summary: summary,
			Start:   time.Date(2030, 1, 15, 10, 0, 0, 0, time.UTC),
			End:     time.Date(2030, 1, 15, 11, 0, 0, 0, time.UTC),
		})
		require.Equal(t, fiber.StatusCreated, code, raw)
		var ev dto.EventResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &ev))
		return ev
	}
	objectsIn := func(calendarID uint) int64 {
		var n int64
		require.NoError(t, db.DB().Model(&calendar.CalendarObject{}).Where("calendar_id = ?", calendarID).Count(&n).Error)
		return n
	}

	t.Run("create into a task list is 400", func(t *testing.T) {
		code, body := send("POST", taskList.UUID+"/events", dto.CreateEventRequest{
			Summary: "Planning",
			Start:   time.Date(2030, 1, 15, 10, 0, 0, 0, time.UTC),
			End:     time.Date(2030, 1, 15, 11, 0, 0, 0, time.UTC),
		})
		assert.Equal(t, fiber.StatusBadRequest, code)
		assert.JSONEq(t, notStored, body)
		assert.Zero(t, objectsIn(taskList.ID), "a refused create must store nothing")
	})

	t.Run("create into a VEVENT calendar still works", func(t *testing.T) {
		ev := create(eventsOnly.UUID, "Standup")
		assert.Equal(t, eventsOnly.ID, ev.CalendarID)
	})

	t.Run("move into a task list is 400", func(t *testing.T) {
		ev := create(cal.UUID, "Movable")
		code, body := send("POST", cal.UUID+"/events/"+ev.ID+"/move", dto.MoveEventRequest{TargetCalendarID: taskList.UUID})
		assert.Equal(t, fiber.StatusBadRequest, code)
		assert.JSONEq(t, notStored, body)

		code, raw := send("GET", cal.UUID+"/events/"+ev.ID, nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		var got dto.EventResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		assert.Equal(t, cal.ID, got.CalendarID, "the event must stay where it was")
		assert.Zero(t, objectsIn(taskList.ID))

		// The same event moves into a VEVENT calendar as before.
		code, raw = send("POST", cal.UUID+"/events/"+ev.ID+"/move", dto.MoveEventRequest{TargetCalendarID: eventsOnly.UUID})
		require.Equal(t, fiber.StatusOK, code, raw)
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		assert.Equal(t, eventsOnly.ID, got.CalendarID)
	})

	t.Run("update in a calendar that no longer stores events is 400", func(t *testing.T) {
		// An event that reached a calendar before it was narrowed to VTODO.
		narrowed := &calendar.Calendar{UUID: "narrowed-uuid", UserID: cal.UserID, Name: "Narrowed", Path: "narrowed"}
		require.NoError(t, db.DB().Create(narrowed).Error)
		ev := create(narrowed.UUID, "Before narrowing")
		require.NoError(t, db.DB().Model(narrowed).Update("supported_components", "VTODO").Error)

		summary := "After narrowing"
		code, body := send("PUT", narrowed.UUID+"/events/"+ev.ID, dto.UpdateEventRequest{Summary: &summary})
		assert.Equal(t, fiber.StatusBadRequest, code)
		assert.JSONEq(t, notStored, body)

		code, raw := send("GET", narrowed.UUID+"/events/"+ev.ID, nil)
		require.Equal(t, fiber.StatusOK, code, raw)
		var got dto.EventResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		assert.Equal(t, "Before narrowing", got.Summary, "a refused update must write nothing")

		// Deleting it stays possible: that writes no event into the calendar.
		code, raw = send("DELETE", narrowed.UUID+"/events/"+ev.ID, nil)
		assert.Equal(t, fiber.StatusNoContent, code, raw)
	})

	t.Run("update in a VEVENT calendar still works", func(t *testing.T) {
		ev := create(eventsOnly.UUID, "Retro")
		summary := "Retrospective"
		code, raw := send("PUT", eventsOnly.UUID+"/events/"+ev.ID, dto.UpdateEventRequest{Summary: &summary})
		require.Equal(t, fiber.StatusOK, code, raw)
		var got dto.EventResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		assert.Equal(t, "Retrospective", got.Summary)
	})
}

// TestEventHandlerRejectsWrongCalendar is the regression test for F09: the
// event routes looked the event up by its UUID alone and ignored :calendar_id,
// so an event answered under any calendar UUID the caller could name — another
// of their calendars, an all-zero UUID, or one that does not exist. Every verb
// must now answer the same 404 an unknown event gets and leave the event alone.
func TestEventHandlerRejectsWrongCalendar(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	other := &calendar.Calendar{UUID: "cal-uuid-other", UserID: cal.UserID, Name: "Personal", Path: "personal"}
	require.NoError(t, db.DB().Create(other).Error)

	send := func(method, path string, body any) (int, string) {
		var rd *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		} else {
			rd = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, "/api/v1/calendars/"+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}

	code, raw := send("POST", cal.UUID+"/events", dto.CreateEventRequest{
		Summary: "Anchored",
		Start:   time.Date(2030, 1, 15, 10, 0, 0, 0, time.UTC),
		End:     time.Date(2030, 1, 15, 11, 0, 0, 0, time.UTC),
	})
	require.Equal(t, fiber.StatusCreated, code, raw)
	var created dto.EventResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &created))
	eventID := created.ID

	// The reference answer: an event id that does not exist at all.
	notFoundCode, notFoundBody := send("GET", cal.UUID+"/events/00000000-0000-0000-0000-000000000001", nil)
	require.Equal(t, fiber.StatusNotFound, notFoundCode)

	summary := "hijacked"
	for _, wrong := range []string{other.UUID, "00000000-0000-0000-0000-000000000000", "7d3f0c52-0000-4000-8000-000000000000"} {
		base := wrong + "/events/" + eventID
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{"GET", base, nil},
			{"PUT", base, dto.UpdateEventRequest{Summary: &summary}},
			{"DELETE", base, nil},
			{"POST", base + "/move", dto.MoveEventRequest{TargetCalendarID: other.UUID}},
		} {
			code, body := send(tc.method, tc.path, tc.body)
			assert.Equal(t, fiber.StatusNotFound, code, "%s %s", tc.method, tc.path)
			assert.JSONEq(t, notFoundBody, body, "%s %s must look exactly like an unknown event", tc.method, tc.path)
		}
	}

	// The event is untouched and still lives where it was created.
	code, raw = send("GET", cal.UUID+"/events/"+eventID, nil)
	require.Equal(t, fiber.StatusOK, code, raw)
	var got dto.EventResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	assert.Equal(t, "Anchored", got.Summary)
	assert.Equal(t, cal.ID, got.CalendarID)
}

func TestEventHandler(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	var eventID string

	t.Run("Create Event", func(t *testing.T) {
		reqBody := dto.CreateEventRequest{
			Summary: "Test Event",
			Start:   time.Now(),
			End:     time.Now().Add(time.Hour),
		}
		body, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+cal.UUID+"/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

		var res dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&res)
		assert.Equal(t, "Test Event", res.Summary)
		eventID = res.ID
	})

	t.Run("Get Event", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/calendars/"+cal.UUID+"/events/"+eventID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&res)
		assert.Equal(t, eventID, res.ID)
	})

	t.Run("List Events", func(t *testing.T) {
		start := time.Now().Add(-time.Hour * 24).Format(time.RFC3339)
		end := time.Now().Add(time.Hour * 24).Format(time.RFC3339)

		req, _ := http.NewRequest("GET", "/api/v1/calendars/"+cal.UUID+"/events", nil)
		q := req.URL.Query()
		q.Add("start", start)
		q.Add("end", end)
		q.Add("expand", "true")
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res dto.EventListResponse
		json.NewDecoder(resp.Body).Decode(&res)
		assert.GreaterOrEqual(t, res.Count, 1)
	})

	t.Run("Update Event", func(t *testing.T) {
		newSummary := "Updated Summary"
		reqBody := dto.UpdateEventRequest{
			Summary: &newSummary,
		}
		body, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("PUT", "/api/v1/calendars/"+cal.UUID+"/events/"+eventID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&res)
		assert.Equal(t, newSummary, res.Summary)
	})

	t.Run("Move Event", func(t *testing.T) {
		// Create another calendar
		cal2 := &calendar.Calendar{
			UUID:   "cal-uuid-2",
			UserID: cal.UserID,
			Name:   "Personal",
			Path:   "personal",
		}
		db.DB().Create(cal2)

		reqBody := dto.MoveEventRequest{
			TargetCalendarID: cal2.UUID,
		}
		body, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+cal.UUID+"/events/"+eventID+"/move", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var res dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&res)
		assert.Equal(t, cal2.ID, res.CalendarID)
	})

	t.Run("Delete Event", func(t *testing.T) {
		// "Move Event" filed the event under cal-uuid-2, so that is now its URL
		// (F09): the old calendar's URL no longer reaches it.
		req, _ := http.NewRequest("DELETE", "/api/v1/calendars/cal-uuid-2/events/"+eventID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify it's gone
		reqGet, _ := http.NewRequest("GET", "/api/v1/calendars/cal-uuid-2/events/"+eventID, nil)
		reqGet.Header.Set("Authorization", "Bearer "+token)
		respGet, _ := app.Test(reqGet)
		assert.Equal(t, fiber.StatusNotFound, respGet.StatusCode)
	})

	t.Run("Update Recurring Event Instance (This Scope)", func(t *testing.T) {
		// Create a recurring event
		reqBody := dto.CreateEventRequest{
			Summary: "Weekly Sync",
			Start:   time.Now().Add(time.Hour * 24).Truncate(time.Second),
			End:     time.Now().Add(time.Hour * 25).Truncate(time.Second),
			Recurrence: &dto.RecurrenceRuleDTO{
				Frequency: "WEEKLY",
			},
		}
		body, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+cal.UUID+"/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		var res dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&res)
		recEventID := res.ID

		// Update a single instance
		newSummary := "Specific Instance Update"
		recurrenceID := reqBody.Start.UTC().Format("20060102T150405Z")
		updateReq := dto.UpdateEventRequest{
			Summary: &newSummary,
		}
		updBody, _ := json.Marshal(updateReq)
		updUrl := "/api/v1/calendars/" + cal.UUID + "/events/" + recEventID + "?scope=this&recurrence_id=" + recurrenceID
		updReq, _ := http.NewRequest("PUT", updUrl, bytes.NewReader(updBody))
		updReq.Header.Set("Authorization", "Bearer "+token)
		updReq.Header.Set("Content-Type", "application/json")

		updResp, err := app.Test(updReq)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, updResp.StatusCode)

		// Verify List displays both
		start := time.Now().Add(-time.Hour * 24).Format(time.RFC3339)
		end := time.Now().Add(time.Hour * 24 * 14).Format(time.RFC3339)
		listUrl := "/api/v1/calendars/" + cal.UUID + "/events?expand=true&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end)
		listReq, _ := http.NewRequest("GET", listUrl, nil)
		listReq.Header.Set("Authorization", "Bearer "+token)
		listResp, _ := app.Test(listReq)
		var listRes dto.EventListResponse
		json.NewDecoder(listResp.Body).Decode(&listRes)

		foundException := false
		for _, e := range listRes.Events {
			if e.Summary == newSummary {
				foundException = true
				break
			}
		}
		assert.True(t, foundException, "Should have found the updated instance in the list")
	})

	t.Run("Delete Recurring Event Instance (This Scope)", func(t *testing.T) {
		// Create a recurring event
		reqBody := dto.CreateEventRequest{
			Summary: "Monthly Meeting",
			Start:   time.Now().Add(time.Hour * 24).Truncate(time.Second),
			End:     time.Now().Add(time.Hour * 25).Truncate(time.Second),
			Recurrence: &dto.RecurrenceRuleDTO{
				Frequency: "MONTHLY",
			},
		}
		body, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+cal.UUID+"/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		var res dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&res)
		recEventID := res.ID

		// Delete the first instance
		recurrenceID := reqBody.Start.UTC().Format("20060102T150405Z")
		delUrl := "/api/v1/calendars/" + cal.UUID + "/events/" + recEventID + "?scope=this&recurrence_id=" + recurrenceID
		delReq, _ := http.NewRequest("DELETE", delUrl, nil)
		delReq.Header.Set("Authorization", "Bearer "+token)

		delResp, err := app.Test(delReq)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, delResp.StatusCode)

		// Verify List does NOT display the deleted instance
		start := time.Now().Add(-time.Hour * 24).Format(time.RFC3339)
		end := time.Now().Add(time.Hour * 24 * 60).Format(time.RFC3339)
		listUrl := "/api/v1/calendars/" + cal.UUID + "/events?expand=true&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end)
		listReq, _ := http.NewRequest("GET", listUrl, nil)
		listReq.Header.Set("Authorization", "Bearer "+token)
		listResp, _ := app.Test(listReq)
		var listRes dto.EventListResponse
		json.NewDecoder(listResp.Body).Decode(&listRes)

		foundDeleted := false
		for _, e := range listRes.Events {
			if e.ID == recEventID && e.RecurrenceID != nil && *e.RecurrenceID == recurrenceID {
				foundDeleted = true
				break
			}
		}
		assert.False(t, foundDeleted, "Should NOT have found the deleted instance in the list")
	})

	t.Run("Update_Recurring_Event_Instance_(This_And_Future_Scope)", func(t *testing.T) {
		// 1. Create a recurring event
		start := time.Now().Add(time.Hour * 48)
		end := start.Add(time.Hour)
		count := 5
		createInput := dto.CreateEventRequest{
			Summary: "Splittable Series",
			Start:   start,
			End:     end,
			AllDay:  false,
			Recurrence: &dto.RecurrenceRuleDTO{
				Frequency: "DAILY",
				Count:     &count,
			},
		}
		body, _ := json.Marshal(createInput)
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+cal.UUID+"/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		var createRes dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&createRes)
		eventID := createRes.ID

		// 2. Update from 3rd instance onwards (Day 3)
		day3 := start.Add(time.Hour * 24 * 2)
		recurrenceID := day3.UTC().Format("20060102T150405Z")
		newSummary := "Shared Future"
		updateBody, _ := json.Marshal(map[string]interface{}{
			"summary": newSummary,
		})
		updateUrl := "/api/v1/calendars/" + cal.UUID + "/events/" + eventID +
			"?scope=this_and_future&recurrence_id=" + url.QueryEscape(recurrenceID)

		updateReq, _ := http.NewRequest("PUT", updateUrl, bytes.NewReader(updateBody))
		updateReq.Header.Set("Authorization", "Bearer "+token)
		updateReq.Header.Set("Content-Type", "application/json")
		updateResp, _ := app.Test(updateReq)
		assert.Equal(t, fiber.StatusOK, updateResp.StatusCode)

		// 3. Expand and Verify
		listStart := start.Add(-time.Hour).Format(time.RFC3339)
		listEnd := start.Add(time.Hour * 24 * 10).Format(time.RFC3339)
		listUrl := "/api/v1/calendars/" + cal.UUID + "/events?expand=true&start=" + url.QueryEscape(listStart) + "&end=" + url.QueryEscape(listEnd)
		listReq, _ := http.NewRequest("GET", listUrl, nil)
		listReq.Header.Set("Authorization", "Bearer "+token)
		listResp, _ := app.Test(listReq)
		var listRes dto.EventListResponse
		json.NewDecoder(listResp.Body).Decode(&listRes)

		dayCounts := make(map[string]int)
		for _, e := range listRes.Events {
			if e.ID == eventID {
				dayCounts[e.Summary]++
			}
		}

		// Day 1, 2 should be "Splittable Series"
		// Day 3, 4, 5 should be "Shared Future" (new series has COUNT=3 because
		// the old master consumes 2 of the original COUNT=5)
		assert.Equal(t, 2, dayCounts["Splittable Series"], "Should have 2 instances of original summary")
		assert.Equal(t, 3, dayCounts["Shared Future"], "Should have 3 instances of new summary")
	})
}

// TestEventHandlerRemindersOnReads (#169 story 10): a DAV-written event with
// three VALARMs reports them as `reminders` on Get and on the expanded and the
// plain list; an event without VALARM reports [].
func TestEventHandlerRemindersOnReads(t *testing.T) {
	app, db, _, cal, token := setupEventHandlerTest(t)
	defer db.Close()

	mk := func(uid, alarms string) *calendar.CalendarObject {
		data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20301101T000000Z\r\n" +
			"DTSTART:20301104T100000Z\r\nDTEND:20301104T110000Z\r\nSUMMARY:" + uid + "\r\n" + alarms + "END:VEVENT\r\nEND:VCALENDAR\r\n"
		obj := &calendar.CalendarObject{
			UUID: uid + "-uuid", CalendarID: cal.ID, UID: uid, Path: uid + ".ics",
			ComponentType: calendar.ComponentEvent, ETag: "e-" + uid, ICalData: data,
		}
		require.NoError(t, obj.PopulateDenormFieldsFromICal())
		require.NoError(t, db.DB().Create(obj).Error)
		return obj
	}
	three := mk("three", "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n"+
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20301104T080000Z\r\nEND:VALARM\r\n"+
		"BEGIN:VALARM\r\nACTION:EMAIL\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n")
	none := mk("none", "")

	get := func(path string) []byte {
		req, _ := http.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		b, _ := io.ReadAll(resp.Body)
		return b
	}
	base := "/api/v1/calendars/" + cal.UUID + "/events"
	wantThree := `[{"index":0,"action":"display","related":"start","offset_seconds":-900,"at":null,"editable":true},
	{"index":1,"action":"display","related":null,"offset_seconds":null,"at":"2030-11-04T08:00:00Z","editable":true},
	{"index":2,"action":"email","related":"start","offset_seconds":-900,"at":null,"editable":false}]`

	var one struct {
		Reminders json.RawMessage `json:"reminders"`
	}
	require.NoError(t, json.Unmarshal(get(base+"/"+three.UUID), &one))
	assert.JSONEq(t, wantThree, string(one.Reminders))
	require.NoError(t, json.Unmarshal(get(base+"/"+none.UUID), &one))
	assert.JSONEq(t, `[]`, string(one.Reminders))

	rng := "start=" + url.QueryEscape("2030-11-04T00:00:00Z") + "&end=" + url.QueryEscape("2030-11-05T00:00:00Z")
	for _, q := range []string{"?expand=true&" + rng, "?" + rng} {
		var list struct {
			Events []struct {
				UID       string          `json:"uid"`
				Reminders json.RawMessage `json:"reminders"`
			} `json:"events"`
		}
		require.NoError(t, json.Unmarshal(get(base+q), &list), q)
		require.Len(t, list.Events, 2, q)
		for _, e := range list.Events {
			if e.UID == "three" {
				assert.JSONEq(t, wantThree, string(e.Reminders), q)
			} else {
				assert.JSONEq(t, `[]`, string(e.Reminders), q)
			}
		}
	}
}
