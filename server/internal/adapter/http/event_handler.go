package http

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/usecase/event"
)

type EventHandler struct {
	listUC       *event.ListEventsUseCase
	getUC        *event.GetEventUseCase
	createUC     *event.CreateEventUseCase
	updateUC     *event.UpdateEventUseCase
	deleteUC     *event.DeleteEventUseCase
	moveUC       *event.MoveEventUseCase
	calendarRepo calendar.CalendarRepository
}

func NewEventHandler(
	listUC *event.ListEventsUseCase,
	getUC *event.GetEventUseCase,
	createUC *event.CreateEventUseCase,
	updateUC *event.UpdateEventUseCase,
	deleteUC *event.DeleteEventUseCase,
	moveUC *event.MoveEventUseCase,
	calendarRepo calendar.CalendarRepository,
) *EventHandler {
	return &EventHandler{
		listUC:       listUC,
		getUC:        getUC,
		createUC:     createUC,
		updateUC:     updateUC,
		deleteUC:     deleteUC,
		moveUC:       moveUC,
		calendarRepo: calendarRepo,
	}
}

// eventResponseFromInstance renders one expanded occurrence as the wire DTO.
// Shared by the per-calendar list and the unified search endpoint (#156) so an
// occurrence looks identical whichever one returned it.
func eventResponseFromInstance(inst calendar.EventInstance) dto.EventResponse {
	resp := dto.EventResponse{
		ID:           inst.ID,
		CalendarID:   inst.CalendarID,
		UID:          inst.UID,
		Summary:      inst.Summary,
		Description:  inst.Description,
		Location:     inst.Location,
		Start:        inst.Start,
		End:          inst.End,
		IsAllDay:     inst.IsAllDay,
		RecurrenceID: &inst.RecurrenceID,
	}
	// Mirror the single-event Get predicate so the frontend can tell a
	// recurring instance apart and show the recurrence scope dialog before
	// a delete/edit hits the whole series.
	if inst.Event != nil {
		resp.IsRecurring = inst.Event.RecurrenceEndTime != nil
	}
	if *resp.RecurrenceID == "" {
		resp.RecurrenceID = nil
	}
	return resp
}

// eventPermission returns the user's permission on the calendar that contains
// the event identified by eventUUID: the shared objectPermission
// (calendar_access.go) with IsEvent as the kind test. An unknown calendar, an
// unknown event, an event filed under a different calendar (F09) and an object
// that is not an event (#170: a task's UUID) all yield PermissionNone, the one
// 404 "Event not found", before any permission, If-Match or body check.
// Without the kind test a PATCH of a task reached the update use case and
// ended in a 500 ("no VEVENT found").
func (h *EventHandler) eventPermission(c fiber.Ctx, eventUUID string) calendar.CalendarPermission {
	perm, _ := objectPermission(c, h.calendarRepo, eventUUID, (*calendar.CalendarObject).IsEvent)
	return perm
}

// eventResponseFromObject builds an EventResponse from a stored calendar
// object, nil-checking StartTime/EndTime (an object written over CalDAV may
// carry neither, and dereferencing them paniced -> 500) and populating
// Description, Location and IsRecurring, which the inline literals previously
// left blank.
func eventResponseFromObject(obj *calendar.CalendarObject) dto.EventResponse {
	resp := dto.EventResponse{
		ID:          obj.UUID,
		CalendarID:  obj.CalendarID,
		UID:         obj.UID,
		Summary:     obj.Summary,
		Description: obj.Description,
		Location:    obj.Location,
		IsAllDay:    obj.IsAllDay,
		IsRecurring: obj.RecurrenceEndTime != nil,
	}
	if obj.StartTime != nil {
		resp.Start = *obj.StartTime
	}
	if obj.EndTime != nil {
		resp.End = *obj.EndTime
	}
	return resp
}

// GET /api/v1/calendars/:calendar_id/events
func (h *EventHandler) List(c fiber.Ctx) error {
	cal, ok := resolveCalendar(c, h.calendarRepo)
	if !ok || calendarPermission(c, h.calendarRepo, cal.ID) == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}
	calendarID := cal.ID
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
		CalendarID: calendarID,
		Start:      start,
		End:        end,
		Expand:     expand,
	})
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list events")
	}

	events := make([]dto.EventResponse, len(instances))
	for i, inst := range instances {
		events[i] = eventResponseFromInstance(inst)
	}

	return c.JSON(dto.EventListResponse{
		Events: events,
		Count:  len(events),
	})
}

// GET /api/v1/calendars/:calendar_id/events/:event_id
func (h *EventHandler) Get(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	if h.eventPermission(c, eventID) == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}
	obj, err := h.getUC.Execute(c.Context(), eventID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}

	return c.JSON(eventResponseFromObject(obj))
}

// POST /api/v1/calendars/:calendar_id/events
func (h *EventHandler) Create(c fiber.Ctx) error {
	cal, ok := resolveCalendar(c, h.calendarRepo)
	if !ok {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}
	calendarID := cal.ID
	perm := calendarPermission(c, h.calendarRepo, calendarID)
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}
	if !canWrite(perm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to this calendar")
	}
	var req dto.CreateEventRequest
	if err := c.Bind().Body(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	input := event.CreateEventInput{
		CalendarID:  calendarID,
		Summary:     req.Summary,
		Description: req.Description,
		Location:    req.Location,
		Start:       req.Start,
		End:         req.End,
		IsAllDay:    req.AllDay,
		Timezone:    req.Timezone,
	}
	if req.Recurrence != nil {
		input.RRule = req.Recurrence.ToRRule(req.AllDay)
	}

	obj, err := h.createUC.Execute(c.Context(), input)
	if err != nil {
		if errors.Is(err, event.ErrInvalidInput) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create event")
	}

	return c.Status(fiber.StatusCreated).JSON(eventResponseFromObject(obj))
}

// eventWriteError answers an event write (update, delete, move) the use case
// refused: ErrNotFound — the object became a task after eventPermission
// looked, a CalDAV PUT keeps the UUID (#170) — is the 404 of an unknown
// event, ErrInvalidInput a 400 with the error's own message, anything else a
// 500 with the route's failure text.
func eventWriteError(c fiber.Ctx, err error, failure string) error {
	switch {
	case errors.Is(err, event.ErrNotFound):
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	case errors.Is(err, event.ErrInvalidInput):
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	return ErrorResponse(c, fiber.StatusInternalServerError, failure)
}

// PATCH /api/v1/calendars/:calendar_id/events/:event_id
func (h *EventHandler) Update(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	perm := h.eventPermission(c, eventID)
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}
	if !canWrite(perm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to this calendar")
	}
	if ok, resp := ifMatchOK(c, h.calendarRepo, eventID); !ok {
		return resp
	}
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
		Timezone:     req.Timezone,
		RecurrenceID: c.Query("recurrence_id"),
		Scope:        c.Query("scope", "all"),
	}

	if req.Recurrence != nil {
		// Pass the raw recurrence through; the RRULE (and its UNTIL value type)
		// is rendered in the use case against the *effective* all-day state. A
		// partial update from an API/MCP client may omit all_day, so rendering
		// here with the request's flag would persist a DATE-TIME UNTIL on an
		// all-day series — the exact issue #118 violation, plus an off-by-one day.
		input.Recurrence = req.Recurrence.ToDomain()
	}

	obj, err := h.updateUC.Execute(c.Context(), input)
	if err != nil {
		return eventWriteError(c, err, "Failed to update event")
	}

	return c.JSON(eventResponseFromObject(obj))
}

// DELETE /api/v1/calendars/:calendar_id/events/:event_id
func (h *EventHandler) Delete(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	perm := h.eventPermission(c, eventID)
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}
	if !canWrite(perm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to this calendar")
	}
	if ok, resp := ifMatchOK(c, h.calendarRepo, eventID); !ok {
		return resp
	}
	scope := c.Query("scope", "all")
	recurrenceID := c.Query("recurrence_id")

	if err := h.deleteUC.Execute(c.Context(), eventID, scope, recurrenceID); err != nil {
		return eventWriteError(c, err, "Failed to delete event")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/v1/calendars/:calendar_id/events/:event_id/move
func (h *EventHandler) Move(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	srcPerm := h.eventPermission(c, eventID)
	if srcPerm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}
	if !canWrite(srcPerm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to the source calendar")
	}
	var req dto.MoveEventRequest
	if err := c.Bind().Body(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// req.TargetCalendarID is the target calendar's UUID (#52); resolve it to the
	// internal id here so the use case still receives a uint.
	targetCal, err := h.calendarRepo.GetByUUID(c.Context(), req.TargetCalendarID)
	if err != nil || targetCal == nil {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}
	targetPerm := calendarPermission(c, h.calendarRepo, targetCal.ID)
	if targetPerm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}
	if !canWrite(targetPerm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to the target calendar")
	}
	obj, err := h.moveUC.Execute(c.Context(), event.MoveEventInput{
		EventUUID:        eventID,
		TargetCalendarID: targetCal.ID,
	})
	if err != nil {
		// A target that does not store events (#170) is ErrInvalidInput:
		// 400 "This calendar does not store events".
		return eventWriteError(c, err, "Failed to move event")
	}

	return c.JSON(eventResponseFromObject(obj))
}
