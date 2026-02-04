package http

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/usecase/event"
)

type EventHandler struct {
	listUC   *event.ListEventsUseCase
	getUC    *event.GetEventUseCase
	createUC *event.CreateEventUseCase
	updateUC *event.UpdateEventUseCase
	deleteUC *event.DeleteEventUseCase
	moveUC   *event.MoveEventUseCase
}

func NewEventHandler(
	listUC *event.ListEventsUseCase,
	getUC *event.GetEventUseCase,
	createUC *event.CreateEventUseCase,
	updateUC *event.UpdateEventUseCase,
	deleteUC *event.DeleteEventUseCase,
	moveUC *event.MoveEventUseCase,
) *EventHandler {
	return &EventHandler{
		listUC:   listUC,
		getUC:    getUC,
		createUC: createUC,
		updateUC: updateUC,
		deleteUC: deleteUC,
		moveUC:   moveUC,
	}
}

// List godoc
// @Summary      List events
// @Description  Get events from calendar
// @Tags         Events
// @Produce      json
// @Param        calendar_id  path      integer  true   "Calendar ID"
// @Param        start        query     string   false  "Start time (RFC3339)"
// @Param        end          query     string   false  "End time (RFC3339)"
// @Param        expand       query     boolean  false  "Expand recurring events (default true)"
// @Success      200          {object}  dto.EventListResponse
// @Failure      400          {object}  ErrorResponseBody
// @Failure      500          {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events [get]
func (h *EventHandler) List(c fiber.Ctx) error {
	calendarID, _ := strconv.Atoi(c.Params("calendar_id"))
	startStr := c.Query("start")
	endStr := c.Query("end")
	expandStr := c.Query("expand", "true")

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil && startStr != "" {
		return BadRequestResponse(c, "Invalid start time format")
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil && endStr != "" {
		return BadRequestResponse(c, "Invalid end time format")
	}
	expand := expandStr == "true"

	instances, err := h.listUC.Execute(c.Context(), event.ListEventsInput{
		CalendarID: uint(calendarID),
		Start:      start,
		End:        end,
		Expand:     expand,
	})
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list events")
	}

	events := make([]dto.EventResponse, len(instances))
	for i, inst := range instances {
		events[i] = dto.EventResponse{
			ID:           inst.ID,
			CalendarID:   inst.CalendarID,
			UID:          inst.UID,
			Summary:      inst.Summary,
			Start:        inst.Start,
			End:          inst.End,
			IsAllDay:     inst.IsAllDay,
			RecurrenceID: &inst.RecurrenceID,
		}
		if *events[i].RecurrenceID == "" {
			events[i].RecurrenceID = nil
		}
	}

	return c.JSON(dto.EventListResponse{
		Events: events,
		Count:  len(events),
	})
}

// Get godoc
// @Summary      Get event
// @Description  Get event by ID
// @Tags         Events
// @Produce      json
// @Param        calendar_id  path      integer  true  "Calendar ID"
// @Param        event_id     path      string   true  "Event UUID"
// @Success      200          {object}  dto.EventResponse
// @Failure      404          {object}  ErrorResponseBody
// @Failure      500          {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events/{event_id} [get]
func (h *EventHandler) Get(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	obj, err := h.getUC.Execute(c.Context(), eventID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}

	return c.JSON(dto.EventResponse{
		ID:         obj.UUID,
		CalendarID: obj.CalendarID,
		UID:        obj.UID,
		Summary:    obj.Summary,
		Start:      *obj.StartTime,
		End:        *obj.EndTime,
		IsAllDay:   obj.IsAllDay,
	})
}

// Create godoc
// @Summary      Create event
// @Description  Create a new event
// @Tags         Events
// @Accept       json
// @Produce      json
// @Param        calendar_id  path      integer                 true  "Calendar ID"
// @Param        event        body      dto.CreateEventRequest  true  "Event details"
// @Success      201          {object}  dto.EventResponse
// @Failure      400          {object}  ErrorResponseBody
// @Failure      500          {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events [post]
func (h *EventHandler) Create(c fiber.Ctx) error {
	calendarID, _ := strconv.Atoi(c.Params("calendar_id"))
	var req dto.CreateEventRequest
	if err := c.Bind().Body(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	input := event.CreateEventInput{
		CalendarID:  uint(calendarID),
		Summary:     req.Summary,
		Description: req.Description,
		Location:    req.Location,
		Start:       req.Start,
		End:         req.End,
		IsAllDay:    req.AllDay,
	}
	if req.Recurrence != nil {
		input.RRule = req.Recurrence.ToRRule() // Need to add ToRRule to DTO
	}

	obj, err := h.createUC.Execute(c.Context(), input)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create event")
	}

	return c.Status(fiber.StatusCreated).JSON(dto.EventResponse{
		ID:         obj.UUID,
		CalendarID: obj.CalendarID,
		UID:        obj.UID,
		Summary:    obj.Summary,
		Start:      *obj.StartTime,
		End:        *obj.EndTime,
		IsAllDay:   obj.IsAllDay,
	})
}

// Update godoc
// @Summary      Update event
// @Description  Update event details
// @Tags         Events
// @Accept       json
// @Produce      json
// @Param        calendar_id    path      integer                 true   "Calendar ID"
// @Param        event_id       path      string                  true   "Event UUID"
// @Param        recurrence_id  query     string                  false  "Recurrence ID (for recurring events)"
// @Param        scope          query     string                  false  "Update scope (this, all, future)"
// @Param        event          body      dto.UpdateEventRequest  true   "Event updates"
// @Success      200            {object}  dto.EventResponse
// @Failure      400            {object}  ErrorResponseBody
// @Failure      500            {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events/{event_id} [put]
func (h *EventHandler) Update(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	var req dto.UpdateEventRequest
	if err := c.Bind().Body(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	input := event.UpdateEventInput{
		UUID:         eventID,
		Summary:      req.Summary,
		Description:  req.Description,
		Location:     req.Location,
		Start:        req.Start,
		End:          req.End,
		IsAllDay:     req.AllDay,
		RecurrenceID: c.Query("recurrence_id"),
		Scope:        c.Query("scope", "all"),
	}

	if req.Recurrence != nil {
		rruleStr := req.Recurrence.ToRRule()
		input.RRule = &rruleStr
	}

	obj, err := h.updateUC.Execute(c.Context(), input)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update event")
	}

	return c.JSON(dto.EventResponse{
		ID:         obj.UUID,
		CalendarID: obj.CalendarID,
		UID:        obj.UID,
		Summary:    obj.Summary,
		Start:      *obj.StartTime,
		End:        *obj.EndTime,
		IsAllDay:   obj.IsAllDay,
	})
}

// Delete godoc
// @Summary      Delete event
// @Description  Delete an event
// @Tags         Events
// @Param        calendar_id    path      integer  true   "Calendar ID"
// @Param        event_id       path      string   true   "Event UUID"
// @Param        scope          query     string   false  "Delete scope (this, all, future)"
// @Param        recurrence_id  query     string   false  "Recurrence ID (for recurring events)"
// @Success      204
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events/{event_id} [delete]
func (h *EventHandler) Delete(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	scope := c.Query("scope", "all")
	recurrenceID := c.Query("recurrence_id")

	if err := h.deleteUC.Execute(c.Context(), eventID, scope, recurrenceID); err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete event")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// Move godoc
// @Summary      Move event
// @Description  Move event to another calendar
// @Tags         Events
// @Accept       json
// @Produce      json
// @Param        calendar_id  path      integer               true  "Source Calendar ID"
// @Param        event_id     path      string                true  "Event UUID"
// @Param        request      body      dto.MoveEventRequest  true  "Target calendar"
// @Success      200          {object}  dto.EventResponse
// @Failure      400          {object}  ErrorResponseBody
// @Failure      500          {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events/{event_id}/move [post]
func (h *EventHandler) Move(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	var req dto.MoveEventRequest
	if err := c.Bind().Body(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	targetCalendarID, _ := strconv.Atoi(req.TargetCalendarID)
	obj, err := h.moveUC.Execute(c.Context(), event.MoveEventInput{
		EventUUID:        eventID,
		TargetCalendarID: uint(targetCalendarID),
	})
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to move event")
	}

	return c.JSON(dto.EventResponse{
		ID:         obj.UUID,
		CalendarID: obj.CalendarID,
		UID:        obj.UID,
		Summary:    obj.Summary,
		Start:      *obj.StartTime,
		End:        *obj.EndTime,
		IsAllDay:   obj.IsAllDay,
	})
}
