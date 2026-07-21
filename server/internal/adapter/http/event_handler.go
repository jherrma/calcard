package http

import (
	"errors"
	"strconv"
	"time"

	gowebdav "github.com/emersion/go-webdav"
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

// calendarPermission returns the authenticated user's effective permission on
// the calendar with the given numeric id. GetUserPermission already resolves
// ownership (PermissionOwner) and shares (PermissionRead / PermissionReadWrite),
// and returns PermissionNone when the calendar is missing or unshared — which
// callers treat exactly like "not found" (404) so existence isn't leaked.
func (h *EventHandler) calendarPermission(c fiber.Ctx, calendarID uint) calendar.CalendarPermission {
	userID := c.Locals("user_id").(uint)
	perm, err := h.calendarRepo.GetUserPermission(c.Context(), calendarID, userID)
	if err != nil {
		return calendar.PermissionNone
	}
	return perm
}

// eventPermission returns the user's permission on the calendar that contains
// the event identified by eventUUID (PermissionNone when the event doesn't
// exist or the calendar is neither owned nor shared with the user).
func (h *EventHandler) eventPermission(c fiber.Ctx, eventUUID string) calendar.CalendarPermission {
	obj, err := h.calendarRepo.GetCalendarObjectByUUID(c.Context(), eventUUID)
	if err != nil || obj == nil {
		return calendar.PermissionNone
	}
	return h.calendarPermission(c, obj.CalendarID)
}

// canWrite reports whether a permission grants write access (owner or
// read-write). Read access is any permission other than PermissionNone.
func canWrite(p calendar.CalendarPermission) bool {
	return p == calendar.PermissionOwner || p == calendar.PermissionReadWrite
}

// ifMatchOK enforces an optional If-Match precondition on REST writes, giving
// the web UI the same optimistic-concurrency guard the DAV PUT/DELETE path has
// (two concurrent sessions could otherwise silently clobber each other). It
// reuses the DAV path's ConditionalMatch parser/comparer. When the client sends
// no If-Match the write stays unconditional (legacy behavior). It returns
// (true, nil) to proceed; on a mismatch it returns (false, <412 response>),
// which the caller must return. A vanished object falls through so the usecase
// still produces its normal not-found result.
func (h *EventHandler) ifMatchOK(c fiber.Ctx, eventUUID string) (bool, error) {
	match := gowebdav.ConditionalMatch(c.Get("If-Match"))
	if !match.IsSet() {
		return true, nil
	}
	obj, err := h.calendarRepo.GetCalendarObjectByUUID(c.Context(), eventUUID)
	if err != nil || obj == nil {
		return true, nil
	}
	if ok, _ := match.MatchETag(obj.ETag); !ok {
		return false, ErrorResponse(c, fiber.StatusPreconditionFailed, "ETag precondition failed")
	}
	return true, nil
}

// eventResponseFromObject builds an EventResponse from a stored calendar
// object, nil-checking StartTime/EndTime (a VTODO may carry neither, and
// dereferencing them paniced -> 500) and populating Description, Location and
// IsRecurring, which the inline literals previously left blank.
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
	if h.calendarPermission(c, uint(calendarID)) == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}
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
			events[i].IsRecurring = inst.Event.RecurrenceEndTime != nil
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
	if h.eventPermission(c, eventID) == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}
	obj, err := h.getUC.Execute(c.Context(), eventID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}

	return c.JSON(eventResponseFromObject(obj))
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
	perm := h.calendarPermission(c, uint(calendarID))
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
		CalendarID:  uint(calendarID),
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

// Update godoc
// @Summary      Update event
// @Description  Update event details
// @Tags         Events
// @Accept       json
// @Produce      json
// @Param        calendar_id    path      integer                 true   "Calendar ID"
// @Param        event_id       path      string                  true   "Event UUID"
// @Param        recurrence_id  query     string                  false  "Recurrence ID (for recurring events)"
// @Param        scope          query     string                  false  "Update scope (this, all, this_and_future)"
// @Param        event          body      dto.UpdateEventRequest  true   "Event updates"
// @Param        If-Match       header    string                  false  "Optional ETag precondition; 412 on mismatch"
// @Success      200            {object}  dto.EventResponse
// @Failure      400            {object}  ErrorResponseBody
// @Failure      412            {object}  ErrorResponseBody  "ETag precondition failed"
// @Failure      500            {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events/{event_id} [put]
func (h *EventHandler) Update(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	perm := h.eventPermission(c, eventID)
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}
	if !canWrite(perm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to this calendar")
	}
	if ok, resp := h.ifMatchOK(c, eventID); !ok {
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
		// The update request carries all_day whenever the client edits the
		// series (the web UI always sends it alongside recurrence); a nil flag
		// defaults to a timed UNTIL, preserving prior behavior.
		allDay := req.AllDay != nil && *req.AllDay
		rruleStr := req.Recurrence.ToRRule(allDay)
		input.RRule = &rruleStr
	}

	obj, err := h.updateUC.Execute(c.Context(), input)
	if err != nil {
		if errors.Is(err, event.ErrInvalidInput) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update event")
	}

	return c.JSON(eventResponseFromObject(obj))
}

// Delete godoc
// @Summary      Delete event
// @Description  Delete an event
// @Tags         Events
// @Param        calendar_id    path      integer  true   "Calendar ID"
// @Param        event_id       path      string   true   "Event UUID"
// @Param        scope          query     string   false  "Delete scope (this, all, this_and_future)"
// @Param        recurrence_id  query     string   false  "Recurrence ID (for recurring events)"
// @Param        If-Match       header    string   false  "Optional ETag precondition; 412 on mismatch"
// @Success      204
// @Failure      412  {object}  ErrorResponseBody  "ETag precondition failed"
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /calendars/{calendar_id}/events/{event_id} [delete]
func (h *EventHandler) Delete(c fiber.Ctx) error {
	eventID := c.Params("event_id")
	perm := h.eventPermission(c, eventID)
	if perm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Event not found")
	}
	if !canWrite(perm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to this calendar")
	}
	if ok, resp := h.ifMatchOK(c, eventID); !ok {
		return resp
	}
	scope := c.Query("scope", "all")
	recurrenceID := c.Query("recurrence_id")

	if err := h.deleteUC.Execute(c.Context(), eventID, scope, recurrenceID); err != nil {
		if errors.Is(err, event.ErrInvalidInput) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
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

	targetCalendarID, _ := strconv.Atoi(req.TargetCalendarID)
	targetPerm := h.calendarPermission(c, uint(targetCalendarID))
	if targetPerm == calendar.PermissionNone {
		return ErrorResponse(c, fiber.StatusNotFound, "Calendar not found")
	}
	if !canWrite(targetPerm) {
		return ErrorResponse(c, fiber.StatusForbidden, "You have read-only access to the target calendar")
	}
	obj, err := h.moveUC.Execute(c.Context(), event.MoveEventInput{
		EventUUID:        eventID,
		TargetCalendarID: uint(targetCalendarID),
	})
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to move event")
	}

	return c.JSON(eventResponseFromObject(obj))
}
