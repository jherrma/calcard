package mcp

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	eventuc "github.com/jherrma/caldav-server/internal/usecase/event"
)

// Resource URI scheme. Resources are the read-only half of the surface: the
// same data the list tools return, addressable by URI so a client can attach a
// calendar or a contact to its context without spending a tool call.
const (
	uriCalendarList  = "calendars://list"
	uriContactList   = "contacts://list"
	calendarURIStem  = "calendars://"
	contactsURIStem  = "contacts://"
	eventsURISuffix  = "/events"
	resourceMIMEType = "application/json"
)

// listResources enumerates the concrete resources available to this caller.
//
// The per-calendar event lists are enumerated too, not only offered as a
// template, because a client that cannot expand templates would otherwise see
// only two resources and have no way to discover the rest.
func (s *Server) listResources(cc *callContext) []Resource {
	resources := []Resource{
		{
			URI:         uriCalendarList,
			Name:        "Calendars",
			Description: "Every calendar the user can read, with colour, timezone and event count.",
			MIMEType:    resourceMIMEType,
		},
		{
			URI:         uriContactList,
			Name:        "Address books",
			Description: "Every address book the user can read.",
			MIMEType:    resourceMIMEType,
		},
	}

	// A failure here yields the two static entries rather than an error: a
	// partial resource list is useful, and resources/list has no way to report
	// "some of these are missing".
	cals, err := s.deps.CalendarList.Execute(cc.ctx, cc.userID)
	if err != nil {
		return resources
	}
	for _, c := range cals {
		resources = append(resources, Resource{
			URI:         calendarURIStem + c.UUID + eventsURISuffix,
			Name:        c.Name + " — events",
			Description: "Upcoming events in the calendar " + c.Name + ".",
			MIMEType:    resourceMIMEType,
		})
	}
	return resources
}

// resourceTemplates describes the URI families a client can construct itself.
func resourceTemplates() []ResourceTemplate {
	return []ResourceTemplate{
		{
			URITemplate: "calendars://{calendar_id}/events",
			Name:        "Calendar events",
			Description: "Events in one calendar, from now forward. calendar_id is a calendar UUID.",
			MIMEType:    resourceMIMEType,
		},
		{
			URITemplate: "contacts://{contact_id}",
			Name:        "Contact",
			Description: "Full details of one contact. contact_id is a contact UUID.",
			MIMEType:    resourceMIMEType,
		},
	}
}

// resourceEventWindow is how far ahead a calendar's event resource reaches.
//
// A resource has no parameters to narrow it with, so it needs a defensible
// fixed window: far enough to answer "what's coming up", short enough that
// attaching it to a model's context is cheap. Anything else is a get_events
// call.
const resourceEventWindow = 30 * 24 * time.Hour

// handleResourceRead serves resources/read.
func (s *Server) handleResourceRead(cc *callContext, req *Request) *Response {
	var params resourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		// Not the decoder's text, which names Go types.
		return errorResponse(req.ID, rpcError(CodeInvalidParams, `invalid resources/read params: expected {"uri": string}`))
	}
	if params.URI == "" {
		return errorResponse(req.ID, rpcError(CodeInvalidParams, "uri is required"))
	}

	body, rpcErr := s.readResource(cc, params.URI)
	if rpcErr != nil {
		return errorResponse(req.ID, rpcErr)
	}

	return resultResponse(req.ID, resourcesReadResult{
		Contents: []ResourceContents{{
			URI:      params.URI,
			MIMEType: resourceMIMEType,
			Text:     body,
		}},
	})
}

// readResource resolves one URI to its JSON body.
//
// Every branch re-checks access through the same helpers the tools use: a
// resource URI is guessable (calendars://<uuid>/events), so it would otherwise
// be the one door into the data that skips the permission check.
//
// The list resources render through the same functions as list_calendars and
// list_address_books, so the two surfaces can never drift apart. A failure the
// caller cannot fix is resourceInternalError: a generic JSON-RPC error and the
// cause in the log, as for the tools.
func (s *Server) readResource(cc *callContext, uri string) (string, *RPCError) {
	switch {
	case uri == uriCalendarList:
		listing, err := s.calendarListing(cc)
		if err != nil {
			return "", resourceInternalError(uri, err)
		}
		return marshalResource(uri, listing)

	case uri == uriContactList:
		listing, err := s.addressBookListing(cc)
		if err != nil {
			return "", resourceInternalError(uri, err)
		}
		return marshalResource(uri, listing)

	case strings.HasPrefix(uri, calendarURIStem) && strings.HasSuffix(uri, eventsURISuffix):
		calUUID := strings.TrimSuffix(strings.TrimPrefix(uri, calendarURIStem), eventsURISuffix)
		return s.readCalendarEvents(cc, uri, calUUID)

	case strings.HasPrefix(uri, contactsURIStem):
		contactUUID := strings.TrimPrefix(uri, contactsURIStem)
		return s.readContact(cc, uri, contactUUID)

	default:
		return "", rpcErrorf(CodeResourceNotFound, "unknown resource uri %q", uri)
	}
}

func (s *Server) readCalendarEvents(cc *callContext, uri, calUUID string) (string, *RPCError) {
	calID, perm := s.resolveCalendar(cc, calUUID)
	if perm == calendar.PermissionNone {
		return "", rpcErrorf(CodeResourceNotFound, "no readable calendar behind %q", uri)
	}

	instances, err := s.deps.EventList.Execute(cc.ctx, eventuc.ListEventsInput{
		CalendarID: calID,
		Start:      cc.now,
		End:        cc.now.Add(resourceEventWindow),
		Expand:     true,
	})
	if err != nil {
		return "", resourceInternalError(uri, err)
	}
	if len(instances) > maxEventResults {
		instances = instances[:maxEventResults]
	}

	views := make([]eventView, 0, len(instances))
	for _, inst := range instances {
		views = append(views, eventViewFromInstance(inst, calUUID))
	}
	return marshalResource(uri, map[string]interface{}{
		"calendar_id":  calUUID,
		"window_start": cc.now.Format(time.RFC3339),
		"window_end":   cc.now.Add(resourceEventWindow).Format(time.RFC3339),
		"events":       views,
		"count":        len(views),
	})
}

func (s *Server) readContact(cc *callContext, uri, contactUUID string) (string, *RPCError) {
	abID, perm := s.contactPermission(cc, contactUUID)
	if !perm.CanRead() {
		return "", rpcErrorf(CodeResourceNotFound, "no readable contact behind %q", uri)
	}
	c, err := s.deps.ContactGet.Execute(cc.ctx, abID, contactUUID)
	if err != nil {
		// The contact exists and is readable (checked above), so this is a
		// failure to load or parse it — the server's, not a missing contact.
		return "", resourceInternalError(uri, err)
	}
	if c == nil {
		return "", rpcErrorf(CodeResourceNotFound, "no readable contact behind %q", uri)
	}
	return marshalResource(uri, contactViewOf(c, s.addressBookUUID(cc, abID)))
}

func marshalResource(uri string, v interface{}) (string, *RPCError) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", resourceInternalError(uri, err)
	}
	return string(buf), nil
}
