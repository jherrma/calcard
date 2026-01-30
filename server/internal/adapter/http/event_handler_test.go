package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
	dataDir, err := os.MkdirTemp("", "event-test-*")
	require.NoError(t, err)

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

	handler := NewEventHandler(eventListUC, eventGetUC, eventCreateUC, eventUpdateUC, eventDeleteUC, eventMoveUC)

	v1 := app.Group("/api/v1")
	calendars := v1.Group("/calendars", Authenticate(jwtManager))
	events := calendars.Group("/:calendar_id/events")
	events.Post("/", handler.Create)
	events.Get("/", handler.List)
	events.Get("/:event_id", handler.Get)
	events.Put("/:event_id", handler.Update)
	events.Delete("/:event_id", handler.Delete)
	events.Post("/:event_id/move", handler.Move)

	return app, db, u, cal, token
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
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events", bytes.NewReader(body))
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
		req, _ := http.NewRequest("GET", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events/"+eventID, nil)
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

		req, _ := http.NewRequest("GET", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events", nil)
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
		req, _ := http.NewRequest("PUT", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events/"+eventID, bytes.NewReader(body))
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
			TargetCalendarID: strconv.Itoa(int(cal2.ID)),
		}
		body, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events/"+eventID+"/move", bytes.NewReader(body))
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
		req, _ := http.NewRequest("DELETE", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events/"+eventID, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		// Verify it's gone
		reqGet, _ := http.NewRequest("GET", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events/"+eventID, nil)
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
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events", bytes.NewReader(body))
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
		updUrl := "/api/v1/calendars/" + strconv.Itoa(int(cal.ID)) + "/events/" + recEventID + "?scope=this&recurrence_id=" + recurrenceID
		updReq, _ := http.NewRequest("PUT", updUrl, bytes.NewReader(updBody))
		updReq.Header.Set("Authorization", "Bearer "+token)
		updReq.Header.Set("Content-Type", "application/json")

		updResp, err := app.Test(updReq)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, updResp.StatusCode)

		// Verify List displays both
		start := time.Now().Add(-time.Hour * 24).Format(time.RFC3339)
		end := time.Now().Add(time.Hour * 24 * 14).Format(time.RFC3339)
		listUrl := "/api/v1/calendars/" + strconv.Itoa(int(cal.ID)) + "/events?expand=true&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end)
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
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		var res dto.EventResponse
		json.NewDecoder(resp.Body).Decode(&res)
		recEventID := res.ID

		// Delete the first instance
		recurrenceID := reqBody.Start.UTC().Format("20060102T150405Z")
		delUrl := "/api/v1/calendars/" + strconv.Itoa(int(cal.ID)) + "/events/" + recEventID + "?scope=this&recurrence_id=" + recurrenceID
		delReq, _ := http.NewRequest("DELETE", delUrl, nil)
		delReq.Header.Set("Authorization", "Bearer "+token)

		delResp, err := app.Test(delReq)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, delResp.StatusCode)

		// Verify List does NOT display the deleted instance
		start := time.Now().Add(-time.Hour * 24).Format(time.RFC3339)
		end := time.Now().Add(time.Hour * 24 * 60).Format(time.RFC3339)
		listUrl := "/api/v1/calendars/" + strconv.Itoa(int(cal.ID)) + "/events?expand=true&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end)
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
		req, _ := http.NewRequest("POST", "/api/v1/calendars/"+strconv.Itoa(int(cal.ID))+"/events", bytes.NewReader(body))
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
		updateUrl := "/api/v1/calendars/" + strconv.Itoa(int(cal.ID)) + "/events/" + eventID +
			"?scope=this_and_future&recurrence_id=" + url.QueryEscape(recurrenceID)

		updateReq, _ := http.NewRequest("PUT", updateUrl, bytes.NewReader(updateBody))
		updateReq.Header.Set("Authorization", "Bearer "+token)
		updateReq.Header.Set("Content-Type", "application/json")
		updateResp, _ := app.Test(updateReq)
		assert.Equal(t, fiber.StatusOK, updateResp.StatusCode)

		// 3. Expand and Verify
		listStart := start.Add(-time.Hour).Format(time.RFC3339)
		listEnd := start.Add(time.Hour * 24 * 10).Format(time.RFC3339)
		listUrl := "/api/v1/calendars/" + strconv.Itoa(int(cal.ID)) + "/events?expand=true&start=" + url.QueryEscape(listStart) + "&end=" + url.QueryEscape(listEnd)
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
		// Day 3, 4, 5, 6, 7 should be "Shared Future" (new series has COUNT=5)
		assert.Equal(t, 2, dayCounts["Splittable Series"], "Should have 2 instances of original summary")
		assert.Equal(t, 5, dayCounts["Shared Future"], "Should have 5 instances of new summary")
	})
}
