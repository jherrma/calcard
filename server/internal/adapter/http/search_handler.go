package http

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	domaincontact "github.com/jherrma/caldav-server/internal/domain/contact"
	addressbookuc "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
	searchuc "github.com/jherrma/caldav-server/internal/usecase/search"
)

// SearchHandler serves the unified search endpoint (#156).
type SearchHandler struct {
	searchUC *searchuc.UseCase
}

func NewSearchHandler(searchUC *searchuc.UseCase) *SearchHandler {
	return &SearchHandler{searchUC: searchUC}
}

// SearchEventItem is one matching occurrence. The nested event is byte-identical
// to what GET /calendars/{id}/events returns, and carries the RecurrenceID the
// client needs to open the occurrence rather than the series master.
type SearchEventItem struct {
	Event         dto.EventResponse `json:"event"`
	CalendarUUID  string            `json:"calendar_uuid"`
	CalendarName  string            `json:"calendar_name"`
	CalendarColor string            `json:"calendar_color"`
}

// SearchContactItem is one matching contact. AddressBookName is included
// because the book may be one shared with the caller, which the client cannot
// always label from its own state.
type SearchContactItem struct {
	Contact         *domaincontact.Contact `json:"contact"`
	AddressBookUUID string                 `json:"addressbook_uuid"`
	AddressBookName string                 `json:"addressbook_name"`
}

// Each group carries its own truncation state. `searched` is false for a type
// the caller excluded via `types`, so an empty group is never mistaken for
// "nothing matched".
type SearchEventGroup struct {
	Items    []SearchEventItem `json:"items"`
	Count    int               `json:"count"`
	HasMore  bool              `json:"has_more"`
	Searched bool              `json:"searched"`
}

type SearchContactGroup struct {
	Items    []SearchContactItem `json:"items"`
	Count    int                 `json:"count"`
	HasMore  bool                `json:"has_more"`
	Searched bool                `json:"searched"`
}

type SearchCalendarGroup struct {
	Items    []*calendaruc.CalendarWithEventCount `json:"items"`
	Count    int                                  `json:"count"`
	HasMore  bool                                 `json:"has_more"`
	Searched bool                                 `json:"searched"`
}

type SearchAddressBookGroup struct {
	Items    []*addressbookuc.AddressBookListItem `json:"items"`
	Count    int                                  `json:"count"`
	HasMore  bool                                 `json:"has_more"`
	Searched bool                                 `json:"searched"`
}

// SearchResponse is the grouped result set. Limit/Offset echo what was applied
// PER GROUP, and MaxLimit is the server-side cap, so a client can tell a capped
// request apart from an exhausted result set.
type SearchResponse struct {
	Query        string                 `json:"query"`
	Types        []string               `json:"types"`
	Limit        int                    `json:"limit"`
	Offset       int                    `json:"offset"`
	MaxLimit     int                    `json:"max_limit"`
	Events       SearchEventGroup       `json:"events"`
	Contacts     SearchContactGroup     `json:"contacts"`
	Calendars    SearchCalendarGroup    `json:"calendars"`
	AddressBooks SearchAddressBookGroup `json:"addressbooks"`
}

// GET /api/v1/search
//
// Searches events, contacts, calendars and address books for the authenticated
// user in one request. `q` is required (minimum 2 characters); `types` narrows
// the search to a comma-separated subset of events,contacts,calendars,addressbooks.
//
// Events are searched across every calendar the caller can read — owned and
// shared — over their denormalized summary, location and description, with no
// implicit date bound: `start`/`end` (RFC 3339) apply only if a window is
// actually wanted. Contacts are searched across every readable address book,
// including books shared with the caller.
//
// Each matching recurring series is returned once, represented by the
// occurrence that best describes it: the first occurrence at or after now, or
// the last one for a series wholly in the past. The occurrence carries its own
// recurrence_id, so a client can open it directly instead of the series master.
//
// Ranking: upcoming occurrences first (soonest first), then past ones (most
// recent first). Contacts are ordered by name, collections by the order the
// list endpoints return.
//
// `limit` and `offset` apply per group and `limit` is capped at 100 (echoed as
// `max_limit`); each group reports `has_more` rather than truncating silently.
func (h *SearchHandler) Search(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)

	query := strings.TrimSpace(c.Query("q"))
	if len(query) < searchuc.MinQueryLength {
		return BadRequestResponse(c, "Query parameter 'q' must be at least 2 characters")
	}

	types, ok := parseSearchTypes(c.Query("types"))
	if !ok {
		return BadRequestResponse(c, "Invalid 'types' value: expected a comma-separated subset of events,contacts,calendars,addressbooks")
	}

	// A mistyped limit/offset is rejected rather than defaulted: silently
	// answering a paginated request with page 1 would look like a result set
	// that simply ran out.
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return BadRequestResponse(c, "Invalid 'limit' value")
		}
		limit = parsed
	}
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return BadRequestResponse(c, "Invalid 'offset' value")
		}
		offset = parsed
	}

	start, err := parseOptionalTime(c.Query("start"))
	if err != nil {
		return BadRequestResponse(c, "Invalid start time format")
	}
	end, err := parseOptionalTime(c.Query("end"))
	if err != nil {
		return BadRequestResponse(c, "Invalid end time format")
	}
	if start != nil && end != nil && !end.After(*start) {
		return BadRequestResponse(c, "'end' must be after 'start'")
	}

	out, err := h.searchUC.Execute(c.Context(), searchuc.Input{
		UserID: userID,
		Query:  query,
		Types:  types,
		Limit:  limit,
		Offset: offset,
		Start:  start,
		End:    end,
		Now:    time.Now(),
	})
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Search failed")
	}

	// Raw JSON, no SuccessResponse envelope — matching the sibling
	// /contacts/search and /calendars endpoints the client already consumes.
	return c.JSON(newSearchResponse(out, types))
}

// parseSearchTypes normalises the `types` parameter. An empty parameter means
// "all types"; an unrecognised name is an error rather than being dropped,
// because silently ignoring it would return an empty group that reads as "no
// matches of that kind".
func parseSearchTypes(raw string) ([]string, bool) {
	if strings.TrimSpace(raw) == "" {
		return searchuc.AllTypes, true
	}

	valid := map[string]bool{
		searchuc.TypeEvents:       true,
		searchuc.TypeContacts:     true,
		searchuc.TypeCalendars:    true,
		searchuc.TypeAddressBooks: true,
	}

	seen := map[string]bool{}
	types := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if !valid[name] {
			return nil, false
		}
		if !seen[name] {
			seen[name] = true
			types = append(types, name)
		}
	}
	if len(types) == 0 {
		return nil, false
	}
	return types, true
}

func parseOptionalTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func newSearchResponse(out *searchuc.Output, types []string) SearchResponse {
	resp := SearchResponse{
		Query:    out.Query,
		Types:    types,
		Limit:    out.Limit,
		Offset:   out.Offset,
		MaxLimit: out.MaxLimit,
		Events: SearchEventGroup{
			Items:    make([]SearchEventItem, 0, len(out.Events.Items)),
			Count:    out.Events.Count,
			HasMore:  out.Events.HasMore,
			Searched: out.Events.Searched,
		},
		Contacts: SearchContactGroup{
			Items:    make([]SearchContactItem, 0, len(out.Contacts.Items)),
			Count:    out.Contacts.Count,
			HasMore:  out.Contacts.HasMore,
			Searched: out.Contacts.Searched,
		},
		Calendars: SearchCalendarGroup{
			Items:    out.Calendars.Items,
			Count:    out.Calendars.Count,
			HasMore:  out.Calendars.HasMore,
			Searched: out.Calendars.Searched,
		},
		AddressBooks: SearchAddressBookGroup{
			Items:    out.AddressBooks.Items,
			Count:    out.AddressBooks.Count,
			HasMore:  out.AddressBooks.HasMore,
			Searched: out.AddressBooks.Searched,
		},
	}

	// Never emit null for an empty group: the client indexes into these arrays.
	if resp.Calendars.Items == nil {
		resp.Calendars.Items = []*calendaruc.CalendarWithEventCount{}
	}
	if resp.AddressBooks.Items == nil {
		resp.AddressBooks.Items = []*addressbookuc.AddressBookListItem{}
	}

	for _, hit := range out.Events.Items {
		resp.Events.Items = append(resp.Events.Items, SearchEventItem{
			Event:         eventResponseFromInstance(hit.Instance),
			CalendarUUID:  hit.CalendarUUID,
			CalendarName:  hit.CalendarName,
			CalendarColor: hit.CalendarColor,
		})
	}
	for _, hit := range out.Contacts.Items {
		resp.Contacts.Items = append(resp.Contacts.Items, SearchContactItem{
			Contact:         hit.Contact,
			AddressBookUUID: hit.AddressBookUUID,
			AddressBookName: hit.AddressBookName,
		})
	}

	return resp
}
