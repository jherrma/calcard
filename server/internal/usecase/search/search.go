// Package search implements the unified cross-resource search behind
// GET /api/v1/search (#156).
//
// Before this existed the web client approximated it: contacts came from
// /contacts/search, but events were fetched one request per calendar over a
// rolling ±6-month window and filtered in the browser, which made anything
// outside that window unfindable. This use case answers the whole question
// server-side, with no implicit date bound.
package search

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	domaincontact "github.com/jherrma/caldav-server/internal/domain/contact"
	addressbookuc "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
	contactuc "github.com/jherrma/caldav-server/internal/usecase/contact"
)

// Searchable result types, as accepted in the `types` query parameter.
const (
	TypeEvents       = "events"
	TypeContacts     = "contacts"
	TypeCalendars    = "calendars"
	TypeAddressBooks = "addressbooks"
)

// AllTypes is the default set searched when the caller names none.
var AllTypes = []string{TypeEvents, TypeContacts, TypeCalendars, TypeAddressBooks}

const (
	// MinQueryLength mirrors the web client's minimum (story 044). Below it the
	// query is rejected rather than answered with the first N rows of every
	// collection.
	MinQueryLength = 2
	// DefaultLimit / MaxLimit bound the items returned PER GROUP. The cap is
	// reported back in the response (`max_limit`) so a client can tell a capped
	// request from an exhausted one.
	DefaultLimit = 20
	MaxLimit     = 100

	// Probe windows used to resolve which occurrence of a recurring series
	// represents it in the results. Short first (one request handles almost
	// everything, including yearly series, at 366 beats worst case), then a wide
	// fallback for genuinely sparse rules such as FREQ=YEARLY;INTERVAL=5.
	shortProbe = 400 * 24 * time.Hour
	longProbe  = 3653 * 24 * time.Hour
)

// Input is one search request. Start/End are optional explicit bounds: nil
// means unbounded, which is the default and the whole point of this endpoint.
type Input struct {
	UserID uint
	Query  string
	Types  []string
	Limit  int
	Offset int
	Start  *time.Time
	End    *time.Time
	// Now is the pivot separating "upcoming" from "past". Injected rather than
	// read from the clock so tests are deterministic.
	Now time.Time
}

// EventHit is a matching event together with the calendar it lives in, so the
// caller can render and deep-link a row without a second request.
type EventHit struct {
	// Instance is the occurrence that represents the match — see
	// resolveInstance. For a recurring series this is NOT the stored master:
	// it carries the occurrence's own start/end and RecurrenceID.
	Instance      calendar.EventInstance
	CalendarUUID  string
	CalendarName  string
	CalendarColor string
}

// ContactHit is a matching contact plus the name of the book holding it (which
// may be a book shared with the caller, hence not necessarily their own).
type ContactHit struct {
	Contact         *domaincontact.Contact
	AddressBookUUID string
	AddressBookName string
}

// Each group reports its own truncation state. Searched distinguishes "this
// type was not requested" from "requested and found nothing" — without it an
// empty group is ambiguous and a client is liable to render "no matches" for
// something it never asked about.
type EventGroup struct {
	Items    []EventHit
	Count    int
	HasMore  bool
	Searched bool
}

type ContactGroup struct {
	Items    []ContactHit
	Count    int
	HasMore  bool
	Searched bool
}

type CalendarGroup struct {
	Items    []*calendaruc.CalendarWithEventCount
	Count    int
	HasMore  bool
	Searched bool
}

type AddressBookGroup struct {
	Items    []*addressbookuc.AddressBookListItem
	Count    int
	HasMore  bool
	Searched bool
}

// Output is the grouped result set.
type Output struct {
	Query        string
	Limit        int
	Offset       int
	MaxLimit     int
	Events       EventGroup
	Contacts     ContactGroup
	Calendars    CalendarGroup
	AddressBooks AddressBookGroup
}

// UseCase answers unified search queries.
//
// It resolves readable collections through the same list use cases the REST
// list endpoints use, so owner/share resolution (#53) lives in exactly one
// place and the calendar/address-book hits are shaped identically to
// GET /calendars and GET /addressbooks. The cost is the per-collection event
// count those use cases compute, which search does not need — acceptable at
// this deployment's scale, and cheaper than a second, divergent copy of the
// permission merge.
type UseCase struct {
	calRepo calendar.CalendarRepository
	abRepo  addressbook.Repository
	calList *calendaruc.ListCalendarsUseCase
	abList  *addressbookuc.ListUseCase
}

func NewUseCase(
	calRepo calendar.CalendarRepository,
	abRepo addressbook.Repository,
	calList *calendaruc.ListCalendarsUseCase,
	abList *addressbookuc.ListUseCase,
) *UseCase {
	return &UseCase{calRepo: calRepo, abRepo: abRepo, calList: calList, abList: abList}
}

// Execute runs the requested searches. A failure in one type fails the whole
// request: a partially-filled result set that looked complete would be read as
// "nothing else matched", which is precisely the false negative this endpoint
// exists to remove.
func (uc *UseCase) Execute(ctx context.Context, in Input) (*Output, error) {
	query := strings.TrimSpace(in.Query)
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	out := &Output{Query: query, Limit: limit, Offset: offset, MaxLimit: MaxLimit}

	wanted := make(map[string]bool, len(in.Types))
	for _, t := range in.Types {
		wanted[t] = true
	}
	if len(wanted) == 0 {
		for _, t := range AllTypes {
			wanted[t] = true
		}
	}

	// Both collection lists are needed whenever events or contacts are
	// searched, because they define WHICH collections may be searched at all.
	needCalendars := wanted[TypeEvents] || wanted[TypeCalendars]
	needBooks := wanted[TypeContacts] || wanted[TypeAddressBooks]

	var calendars []*calendaruc.CalendarWithEventCount
	var books []*addressbookuc.AddressBookListItem
	var err error

	if needCalendars {
		if calendars, err = uc.calList.Execute(ctx, in.UserID); err != nil {
			return nil, err
		}
	}
	if needBooks {
		if books, err = uc.abList.Execute(ctx, in.UserID); err != nil {
			return nil, err
		}
	}

	if wanted[TypeEvents] {
		group, err := uc.searchEvents(ctx, calendars, query, limit, offset, now, in.Start, in.End)
		if err != nil {
			return nil, err
		}
		out.Events = group
	}

	if wanted[TypeContacts] {
		group, err := uc.searchContacts(ctx, books, query, limit, offset)
		if err != nil {
			return nil, err
		}
		out.Contacts = group
	}

	if wanted[TypeCalendars] {
		matched := make([]*calendaruc.CalendarWithEventCount, 0, len(calendars))
		for _, cal := range calendars {
			if contains(cal.Name, query) || contains(cal.Description, query) {
				matched = append(matched, cal)
			}
		}
		items, hasMore := page(matched, limit, offset)
		out.Calendars = CalendarGroup{Items: items, Count: len(items), HasMore: hasMore, Searched: true}
	}

	if wanted[TypeAddressBooks] {
		matched := make([]*addressbookuc.AddressBookListItem, 0, len(books))
		for _, ab := range books {
			if contains(ab.Name, query) || contains(ab.Description, query) {
				matched = append(matched, ab)
			}
		}
		items, hasMore := page(matched, limit, offset)
		out.AddressBooks = AddressBookGroup{Items: items, Count: len(items), HasMore: hasMore, Searched: true}
	}

	return out, nil
}

func (uc *UseCase) searchEvents(
	ctx context.Context,
	calendars []*calendaruc.CalendarWithEventCount,
	query string,
	limit, offset int,
	now time.Time,
	start, end *time.Time,
) (EventGroup, error) {
	group := EventGroup{Items: []EventHit{}, Searched: true}

	// Every calendar in the list is readable — owned or shared (#53). A user
	// with none searches nothing rather than everything (SearchEvents treats an
	// empty id set as "match nothing", not "no filter").
	ids := make([]uint, 0, len(calendars))
	byID := make(map[uint]*calendaruc.CalendarWithEventCount, len(calendars))
	for _, cal := range calendars {
		ids = append(ids, cal.ID)
		byID[cal.ID] = cal
	}
	if len(ids) == 0 {
		return group, nil
	}

	// One row over the page size tells us there is a next page without a
	// second COUNT query.
	objects, err := uc.calRepo.SearchEvents(ctx, calendar.EventSearchQuery{
		CalendarIDs: ids,
		Text:        query,
		Start:       start,
		End:         end,
		Pivot:       now,
		Limit:       limit + 1,
		Offset:      offset,
	})
	if err != nil {
		return group, err
	}
	group.HasMore = len(objects) > limit
	if group.HasMore {
		objects = objects[:limit]
	}

	for _, obj := range objects {
		cal := byID[obj.CalendarID]
		if cal == nil {
			// Can't happen: the ids came from this very list. Skipping rather
			// than dereferencing keeps a future refactor from panicking here.
			continue
		}
		inst, ok := resolveInstance(obj, now)
		if !ok {
			continue
		}
		group.Items = append(group.Items, EventHit{
			Instance:      inst,
			CalendarUUID:  cal.UUID,
			CalendarName:  cal.Name,
			CalendarColor: cal.Color,
		})
	}

	// Which rows are on this page is decided by the SQL ordering; how they are
	// ordered WITHIN it is decided by the resolved occurrence, because that is
	// the date the row displays. Upcoming first (soonest first), then past
	// (most recent first) — a search for "standup" should surface the next
	// standup, not the first one ever held.
	sortHits(group.Items, now)
	group.Count = len(group.Items)
	return group, nil
}

func (uc *UseCase) searchContacts(
	ctx context.Context,
	books []*addressbookuc.AddressBookListItem,
	query string,
	limit, offset int,
) (ContactGroup, error) {
	group := ContactGroup{Items: []ContactHit{}, Searched: true}

	ids := make([]uint, 0, len(books))
	byID := make(map[uint]*addressbookuc.AddressBookListItem, len(books))
	for _, ab := range books {
		ids = append(ids, ab.ID)
		byID[ab.ID] = ab
	}
	if len(ids) == 0 {
		return group, nil
	}

	objs, err := uc.abRepo.SearchObjectsInBooks(ctx, ids, query, limit+1, offset)
	if err != nil {
		return group, err
	}
	group.HasMore = len(objs) > limit
	if group.HasMore {
		objs = objs[:limit]
	}

	for i := range objs {
		c := contactuc.FromAddressObject(&objs[i])
		if c == nil {
			continue
		}
		hit := ContactHit{Contact: c}
		if ab := byID[objs[i].AddressBookID]; ab != nil {
			hit.AddressBookUUID = ab.UUID
			hit.AddressBookName = ab.Name
		}
		group.Items = append(group.Items, hit)
	}
	group.Count = len(group.Items)
	return group, nil
}

// resolveInstance picks the occurrence that represents a stored object in the
// results, and is why the response can be deep-linked: the frontend must never
// open a recurring series master as if it were an occurrence.
//
//   - non-recurring: the row itself.
//   - a series still running or in the future: its first occurrence at or after
//     the pivot (an occurrence already under way counts — expansion is
//     overlap-based).
//   - a series wholly in the past: its last occurrence.
//
// Expansion is bounded by probe windows instead of walking the whole rule, so
// a daily series that started years ago costs one short expansion.
func resolveInstance(obj *calendar.CalendarObject, pivot time.Time) (calendar.EventInstance, bool) {
	if obj == nil || obj.StartTime == nil {
		return calendar.EventInstance{}, false
	}
	endTime := *obj.StartTime
	if obj.EndTime != nil {
		endTime = *obj.EndTime
	}
	master := calendar.ToEventInstance(obj, *obj.StartTime, endTime, "", nil, nil)

	// RecurrenceEndTime is set only for recurring series (maintained by
	// PopulateDenormFieldsFromICal), so its absence means one occurrence.
	if obj.RecurrenceEndTime == nil {
		return master, true
	}
	seriesEnd := *obj.RecurrenceEndTime

	if seriesEnd.Before(pivot) {
		for _, probe := range []time.Duration{shortProbe, longProbe} {
			from := seriesEnd.Add(-probe)
			if obj.StartTime.After(from) {
				from = *obj.StartTime
			}
			// The window is half-open, so nudge past the last occurrence's end.
			if insts := expand(obj, from, seriesEnd.Add(time.Second)); len(insts) > 0 {
				return insts[len(insts)-1], true
			}
		}
		return master, true
	}

	for _, probe := range []time.Duration{shortProbe, longProbe} {
		until := pivot.Add(probe)
		if seriesEnd.Before(until) {
			until = seriesEnd.Add(time.Second)
		}
		if insts := expand(obj, pivot, until); len(insts) > 0 {
			return insts[0], true
		}
	}

	// Unparseable iCalendar data, or a rule that yields nothing in either
	// probe. Showing the stored row (with no recurrence id, so the client
	// treats it as the series) beats dropping a genuine match from the results.
	return master, true
}

func expand(obj *calendar.CalendarObject, from, until time.Time) []calendar.EventInstance {
	insts, err := calendar.ExpandRecurringEvent(obj, from, until)
	if err != nil || len(insts) == 0 {
		return nil
	}
	// Masters and stray exception overrides are appended in rule order, not
	// chronological order, so "first"/"last" need an explicit sort.
	sort.SliceStable(insts, func(i, j int) bool { return insts[i].Start.Before(insts[j].Start) })
	return insts
}

func sortHits(hits []EventHit, pivot time.Time) {
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i].Instance.Start, hits[j].Instance.Start
		aUpcoming, bUpcoming := !a.Before(pivot), !b.Before(pivot)
		if aUpcoming != bUpcoming {
			return aUpcoming
		}
		if aUpcoming {
			return a.Before(b)
		}
		return b.Before(a)
	})
}

func contains(haystack, needle string) bool {
	if haystack == "" || needle == "" {
		return false
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// page applies the group's offset/limit and reports whether anything was cut
// off the end.
func page[T any](items []T, limit, offset int) ([]T, bool) {
	if offset >= len(items) {
		return []T{}, false
	}
	items = items[offset:]
	if len(items) > limit {
		return items[:limit], true
	}
	return items, false
}
