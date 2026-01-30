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

func (h *EventHandler) Delete(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	scope := c.Query("scope", "all")
	recurrenceID := c.Query("recurrence_id")

	if err := h.deleteUC.Execute(c.Context(), eventID, scope, recurrenceID); err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete event")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

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
