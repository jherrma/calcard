package mcp

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	eventuc "github.com/jherrma/caldav-server/internal/usecase/event"
	searchuc "github.com/jherrma/caldav-server/internal/usecase/search"
)

// maxEventResults caps how many occurrences a single tool call returns.
//
// A model pays for every token it reads, and an unbounded expansion of a daily
// series over a five-year window is both useless to it and a way to exhaust the
// server. Truncation is always reported in the payload (`truncated`), never
// silent — a silently short list reads as "that's everything".
const maxEventResults = 200

// calendarView is how a calendar is rendered to the model.
type calendarView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	EventCount  int64  `json:"event_count"`
	// Permission is "owner", "read" or "read-write". The model needs it to
	// know a write will be refused before attempting it.
	Permission string `json:"permission"`
	Shared     bool   `json:"shared"`
	OwnerName  string `json:"owner_name,omitempty"`
}

// eventView is how an occurrence is rendered to the model.
type eventView struct {
	ID          string `json:"id"`
	CalendarID  string `json:"calendar_id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Start       string `json:"start"`
	End         string `json:"end"`
	AllDay      bool   `json:"all_day"`
	// RecurrenceID identifies WHICH occurrence of a recurring series this is.
	// Editing or deleting a single occurrence requires passing it back, so it
	// is surfaced rather than hidden.
	RecurrenceID string `json:"recurrence_id,omitempty"`
	Recurring    bool   `json:"recurring"`
}

func (s *Server) registerCalendarTools() {
	s.register(Tool{
		Name: "list_calendars",
		Description: "List the calendars the signed-in user can see, including calendars shared " +
			"with them. Returns each calendar's id (a UUID, required by every other calendar " +
			"tool), name, colour, timezone, event count and the caller's permission on it.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}, s.toolListCalendars)

	s.register(Tool{
		Name: "get_events",
		Description: "List events in one calendar. Recurring series are expanded into their " +
			"individual occurrences within the window. Without start/end the whole calendar is " +
			"returned, which can be large — pass a window whenever the question has one.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "calendar_id": {"type": "string", "description": "Calendar UUID from list_calendars"},
    "start": {"type": "string", "description": "Window start, RFC 3339 (e.g. 2026-03-01T00:00:00Z) or YYYY-MM-DD"},
    "end": {"type": "string", "description": "Window end, RFC 3339 or YYYY-MM-DD"}
  },
  "required": ["calendar_id"],
  "additionalProperties": false
}`),
	}, s.toolGetEvents)

	s.register(Tool{
		Name: "create_event",
		Description: "Create an event in a calendar. Times are RFC 3339; include an offset or a " +
			"trailing Z so the event lands in the intended zone. For an all-day event set " +
			"all_day true and pass dates.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "calendar_id": {"type": "string", "description": "Calendar UUID from list_calendars"},
    "title": {"type": "string", "description": "Event title (iCalendar SUMMARY)"},
    "start": {"type": "string", "description": "Start, RFC 3339 or YYYY-MM-DD for all-day"},
    "end": {"type": "string", "description": "End, RFC 3339 or YYYY-MM-DD for all-day"},
    "description": {"type": "string"},
    "location": {"type": "string"},
    "all_day": {"type": "boolean", "description": "Defaults to false"},
    "timezone": {"type": "string", "description": "IANA timezone, e.g. Europe/Berlin. Defaults to the calendar's timezone"}
  },
  "required": ["calendar_id", "title", "start", "end"],
  "additionalProperties": false
}`),
	}, s.toolCreateEvent)

	s.register(Tool{
		Name: "update_event",
		Description: "Update an event. Only the fields you pass are changed; omitted fields keep " +
			"their stored value. For one occurrence of a recurring series pass its recurrence_id " +
			"and scope \"this\".",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "event_id": {"type": "string", "description": "Event UUID from get_events or search_events"},
    "title": {"type": "string"},
    "description": {"type": "string"},
    "location": {"type": "string"},
    "start": {"type": "string", "description": "RFC 3339"},
    "end": {"type": "string", "description": "RFC 3339"},
    "all_day": {"type": "boolean"},
    "timezone": {"type": "string", "description": "IANA timezone"},
    "recurrence_id": {"type": "string", "description": "Occurrence to edit, from get_events"},
    "scope": {"type": "string", "enum": ["this", "this_and_future", "all"], "description": "Which occurrences the edit applies to. Defaults to all"}
  },
  "required": ["event_id"],
  "additionalProperties": false
}`),
	}, s.toolUpdateEvent)

	s.register(Tool{
		Name: "delete_event",
		Description: "Delete an event. For a recurring series, deleting without a recurrence_id " +
			"removes the whole series; pass recurrence_id with scope \"this\" to remove a single " +
			"occurrence.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "event_id": {"type": "string", "description": "Event UUID"},
    "recurrence_id": {"type": "string", "description": "Occurrence to delete, from get_events"},
    "scope": {"type": "string", "enum": ["this", "all"], "description": "Defaults to all"}
  },
  "required": ["event_id"],
  "additionalProperties": false
}`),
	}, s.toolDeleteEvent)

	s.register(Tool{
		Name: "search_events",
		Description: "Full-text search over the title, location and description of events in every " +
			"calendar the user can read, owned and shared. There is no implicit date bound, so " +
			"past events are found too; pass start/end only to restrict the window. Each matching " +
			"recurring series is returned once.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search text, at least 2 characters"},
    "start": {"type": "string", "description": "Optional lower bound, RFC 3339"},
    "end": {"type": "string", "description": "Optional upper bound, RFC 3339"},
    "limit": {"type": "integer", "description": "Maximum matches, default 20, max 100"}
  },
  "required": ["query"],
  "additionalProperties": false
}`),
	}, s.toolSearchEvents)
}

// permissionLabel renders a calendar permission the way the REST list endpoint
// does, so a model comparing the two surfaces sees one vocabulary.
func permissionLabel(p calendar.CalendarPermission) string {
	switch p {
	case calendar.PermissionOwner:
		return "owner"
	case calendar.PermissionReadWrite:
		return "read-write"
	case calendar.PermissionRead:
		return "read"
	default:
		return "none"
	}
}

func (s *Server) toolListCalendars(cc *callContext, _ json.RawMessage) (*toolCallResult, *RPCError) {
	cals, err := s.deps.CalendarList.Execute(cc.ctx, cc.userID)
	if err != nil {
		return errorResult("Failed to list calendars: " + err.Error()), nil
	}

	views := make([]calendarView, 0, len(cals))
	for _, c := range cals {
		v := calendarView{
			ID:          c.UUID,
			Name:        c.Name,
			Description: c.Description,
			Color:       c.Color,
			Timezone:    c.Timezone,
			EventCount:  c.EventCount,
			Permission:  c.Permission,
			Shared:      c.Shared,
		}
		if c.Owner != nil {
			v.OwnerName = c.Owner.DisplayName
		}
		views = append(views, v)
	}
	return jsonText(map[string]interface{}{"calendars": views, "count": len(views)})
}

func (s *Server) toolGetEvents(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		CalendarID string `json:"calendar_id"`
		Start      string `json:"start"`
		End        string `json:"end"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	calID, perm := s.resolveCalendar(cc, in.CalendarID)
	if perm == calendar.PermissionNone {
		return errorResult("No calendar with id " + in.CalendarID + " is readable by you."), nil
	}

	var start, end time.Time
	if in.Start != "" {
		t, err := parseTime("start", in.Start)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		start = t
	}
	if in.End != "" {
		t, err := parseTime("end", in.End)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		end = t
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return errorResult("end must not be before start."), nil
	}

	instances, err := s.deps.EventList.Execute(cc.ctx, eventuc.ListEventsInput{
		CalendarID: calID,
		Start:      start,
		End:        end,
		Expand:     true,
	})
	if err != nil {
		return errorResult("Failed to list events: " + err.Error()), nil
	}

	truncated := false
	if len(instances) > maxEventResults {
		instances = instances[:maxEventResults]
		truncated = true
	}

	views := make([]eventView, 0, len(instances))
	for _, inst := range instances {
		views = append(views, eventViewFromInstance(inst, in.CalendarID))
	}
	out := map[string]interface{}{
		"events":      views,
		"count":       len(views),
		"calendar_id": in.CalendarID,
	}
	if truncated {
		out["truncated"] = true
		out["note"] = "More events exist than were returned; narrow the window with start/end."
	}
	return jsonText(out)
}

// eventViewFromInstance renders one expanded occurrence. calendarUUID is passed
// in because EventInstance carries only the numeric calendar id, and the model
// must never be handed an identifier it cannot pass back.
func eventViewFromInstance(inst calendar.EventInstance, calendarUUID string) eventView {
	v := eventView{
		ID:           inst.ID,
		CalendarID:   calendarUUID,
		Title:        inst.Summary,
		Description:  inst.Description,
		Location:     inst.Location,
		Start:        inst.Start.Format(time.RFC3339),
		End:          inst.End.Format(time.RFC3339),
		AllDay:       inst.IsAllDay,
		RecurrenceID: inst.RecurrenceID,
	}
	if inst.Event != nil {
		v.Recurring = inst.Event.RecurrenceEndTime != nil
	}
	return v
}

// eventViewFromObject renders a stored event (the shape a write returns).
func eventViewFromObject(obj *calendar.CalendarObject, calendarUUID string) eventView {
	v := eventView{
		ID:          obj.UUID,
		CalendarID:  calendarUUID,
		Title:       obj.Summary,
		Description: obj.Description,
		Location:    obj.Location,
		AllDay:      obj.IsAllDay,
		Recurring:   obj.RecurrenceEndTime != nil,
	}
	if obj.StartTime != nil {
		v.Start = obj.StartTime.Format(time.RFC3339)
	}
	if obj.EndTime != nil {
		v.End = obj.EndTime.Format(time.RFC3339)
	}
	return v
}

func (s *Server) toolCreateEvent(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		CalendarID  string `json:"calendar_id"`
		Title       string `json:"title"`
		Start       string `json:"start"`
		End         string `json:"end"`
		Description string `json:"description"`
		Location    string `json:"location"`
		AllDay      bool   `json:"all_day"`
		Timezone    string `json:"timezone"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}
	if in.Title == "" {
		return errorResult("title is required."), nil
	}

	calID, perm := s.resolveCalendar(cc, in.CalendarID)
	if perm == calendar.PermissionNone {
		return errorResult("No calendar with id " + in.CalendarID + " is readable by you."), nil
	}
	if !canWriteCalendar(perm) {
		return errorResult("You have read-only access to that calendar, so events cannot be created in it."), nil
	}

	start, err := parseTime("start", in.Start)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	end, err := parseTime("end", in.End)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	obj, err := s.deps.EventCreate.Execute(cc.ctx, eventuc.CreateEventInput{
		CalendarID:  calID,
		Summary:     in.Title,
		Description: in.Description,
		Location:    in.Location,
		Start:       start,
		End:         end,
		IsAllDay:    in.AllDay,
		Timezone:    in.Timezone,
	})
	if err != nil {
		if errors.Is(err, eventuc.ErrInvalidInput) {
			return errorResult(err.Error()), nil
		}
		return errorResult("Failed to create the event: " + err.Error()), nil
	}

	return jsonText(map[string]interface{}{
		"created": true,
		"event":   eventViewFromObject(obj, in.CalendarID),
	})
}

func (s *Server) toolUpdateEvent(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		EventID      string  `json:"event_id"`
		Title        *string `json:"title"`
		Description  *string `json:"description"`
		Location     *string `json:"location"`
		Start        *string `json:"start"`
		End          *string `json:"end"`
		AllDay       *bool   `json:"all_day"`
		Timezone     *string `json:"timezone"`
		RecurrenceID string  `json:"recurrence_id"`
		Scope        string  `json:"scope"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	obj, perm := s.eventPermission(cc, in.EventID)
	if perm == calendar.PermissionNone {
		return errorResult("No event with id " + in.EventID + " is readable by you."), nil
	}
	if !canWriteCalendar(perm) {
		return errorResult("You have read-only access to the calendar holding that event."), nil
	}

	// Validate the timestamps here rather than letting them through as opaque
	// strings: the use case takes ISO strings, so a typo would otherwise
	// surface as a generic parse failure with no indication of which field.
	for field, value := range map[string]*string{"start": in.Start, "end": in.End} {
		if value != nil && *value != "" {
			if _, err := time.Parse(time.RFC3339, *value); err != nil {
				return errorResult(field + " must be an RFC 3339 timestamp, got " + *value), nil
			}
		}
	}

	scope := in.Scope
	if scope == "" {
		scope = "all"
	}

	updated, err := s.deps.EventUpdate.Execute(cc.ctx, eventuc.UpdateEventInput{
		UUID:         in.EventID,
		Summary:      in.Title,
		Description:  in.Description,
		Location:     in.Location,
		Start:        in.Start,
		End:          in.End,
		IsAllDay:     in.AllDay,
		Timezone:     in.Timezone,
		RecurrenceID: in.RecurrenceID,
		Scope:        scope,
	})
	if err != nil {
		if errors.Is(err, eventuc.ErrInvalidInput) {
			return errorResult(err.Error()), nil
		}
		return errorResult("Failed to update the event: " + err.Error()), nil
	}

	calUUID := s.calendarUUID(cc, obj.CalendarID)
	return jsonText(map[string]interface{}{
		"updated": true,
		"event":   eventViewFromObject(updated, calUUID),
	})
}

func (s *Server) toolDeleteEvent(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		EventID      string `json:"event_id"`
		RecurrenceID string `json:"recurrence_id"`
		Scope        string `json:"scope"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	_, perm := s.eventPermission(cc, in.EventID)
	if perm == calendar.PermissionNone {
		return errorResult("No event with id " + in.EventID + " is readable by you."), nil
	}
	if !canWriteCalendar(perm) {
		return errorResult("You have read-only access to the calendar holding that event."), nil
	}

	scope := in.Scope
	if scope == "" {
		scope = "all"
	}

	if err := s.deps.EventDelete.Execute(cc.ctx, in.EventID, scope, in.RecurrenceID); err != nil {
		if errors.Is(err, eventuc.ErrInvalidInput) {
			return errorResult(err.Error()), nil
		}
		return errorResult("Failed to delete the event: " + err.Error()), nil
	}

	return jsonText(map[string]interface{}{
		"deleted":  true,
		"event_id": in.EventID,
		"scope":    scope,
	})
}

func (s *Server) toolSearchEvents(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		Query string `json:"query"`
		Start string `json:"start"`
		End   string `json:"end"`
		Limit int    `json:"limit"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}
	if len(in.Query) < searchuc.MinQueryLength {
		return errorResult("query must be at least 2 characters."), nil
	}

	input := searchuc.Input{
		UserID: cc.userID,
		Query:  in.Query,
		Types:  []string{searchuc.TypeEvents},
		Limit:  in.Limit,
		Now:    cc.now,
	}
	if in.Start != "" {
		t, err := parseTime("start", in.Start)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		input.Start = &t
	}
	if in.End != "" {
		t, err := parseTime("end", in.End)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		input.End = &t
	}

	out, err := s.deps.Search.Execute(cc.ctx, input)
	if err != nil {
		return errorResult("Search failed: " + err.Error()), nil
	}

	views := make([]map[string]interface{}, 0, len(out.Events.Items))
	for _, hit := range out.Events.Items {
		views = append(views, map[string]interface{}{
			"event":          eventViewFromInstance(hit.Instance, hit.CalendarUUID),
			"calendar_name":  hit.CalendarName,
			"calendar_id":    hit.CalendarUUID,
			"calendar_color": hit.CalendarColor,
		})
	}

	return jsonText(map[string]interface{}{
		"query":    out.Query,
		"matches":  views,
		"count":    out.Events.Count,
		"has_more": out.Events.HasMore,
	})
}

// calendarUUID maps a numeric calendar id back to the UUID the model uses.
// A lookup failure yields "" rather than an error: the write it accompanies has
// already succeeded, and failing the call at that point would tell the model
// the opposite of what happened.
func (s *Server) calendarUUID(cc *callContext, calendarID uint) string {
	cal, err := s.deps.CalendarRepo.GetByID(cc.ctx, calendarID)
	if err != nil || cal == nil {
		return ""
	}
	return cal.UUID
}
