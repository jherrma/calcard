package importexport

import (
	"bytes"
	"context"
	"fmt"

	ical "github.com/emersion/go-ical"
	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// CalendarImportUseCase handles calendar import from iCalendar format
type CalendarImportUseCase struct {
	calendarRepo calendar.CalendarRepository
}

// NewCalendarImportUseCase creates a new calendar import use case
func NewCalendarImportUseCase(calendarRepo calendar.CalendarRepository) *CalendarImportUseCase {
	return &CalendarImportUseCase{calendarRepo: calendarRepo}
}

// Execute imports calendar events from iCalendar data
func (uc *CalendarImportUseCase) Execute(ctx context.Context, userID uint, calendarUUID string, data []byte, opts ImportOptions) (*ImportResult, error) {
	// Get calendar and verify ownership. Not owned is the same error as
	// missing, so the caller cannot tell whether someone else's calendar exists.
	cal, err := uc.calendarRepo.GetByUUID(ctx, calendarUUID)
	if err != nil {
		return nil, calendar.ErrNotFound
	}
	if cal.UserID != userID {
		return nil, calendar.ErrNotFound
	}
	// A subscribed calendar (story 100) mirrors a remote feed: anything
	// imported into it survives only until the next refresh, which replaces
	// the collection's contents wholesale. Accepting the import and reporting
	// "42 events imported" would be a lie with a delay on it.
	if cal.Subscribed {
		return nil, fmt.Errorf("cannot import into a subscribed calendar: it mirrors a remote feed and is read-only")
	}

	// Default options
	if opts.DuplicateHandling == "" {
		opts.DuplicateHandling = "skip"
	}

	// Parse iCalendar
	decoder := ical.NewDecoder(bytes.NewReader(data))
	parsedCal, err := decoder.Decode()
	if err != nil {
		return nil, fmt.Errorf("invalid iCalendar format: %w", err)
	}

	result := &ImportResult{}

	// Fetch the calendar's existing objects ONCE and index them by UID, rather
	// than re-materializing the whole object list on every imported event
	// (which made import O(N*M)). The map is kept coherent as the loop mutates
	// the calendar below (see create/replace handling) so duplicate UIDs within
	// the same file behave exactly as a fresh per-event fetch would.
	existingObjects, err := uc.calendarRepo.GetCalendarObjects(ctx, cal.ID)
	if err != nil {
		return nil, err
	}
	byUID := make(map[string]*calendar.CalendarObject, len(existingObjects))
	for _, obj := range existingObjects {
		byUID[obj.UID] = obj
	}
	paths := newObjectPaths(existingObjects)

	// VTIMEZONEs are declared once per file; each object below gets a copy of
	// the ones it references.
	timezones := vtimezonesByID(parsedCal)

	// One calendar object per UID: a recurring master and its RECURRENCE-ID
	// overrides are sibling components in the file, but CalDAV requires them in
	// ONE resource (RFC 4791 §4.1). Every count in the result is per object.
	for _, item := range groupImportItems(parsedCal.Children) {
		result.Total++

		// Get UID
		if !item.hasUID {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index: result.Total - 1,
				Error: "Missing UID property",
			})
			continue
		}
		uid := item.uid

		// Get Summary for error reporting
		summary := item.summary()

		// Check for existing event by UID (indexed once, above the loop)
		existing := byUID[uid]
		// A replaced object keeps its path, so DAV clients see an update.
		preferredPath := ""

		if existing != nil {
			switch opts.DuplicateHandling {
			case "skip":
				result.Skipped++
				continue
			case "replace":
				// Delete existing object
				if err := uc.calendarRepo.DeleteCalendarObject(ctx, existing); err != nil {
					result.Failed++
					result.Errors = append(result.Errors, ImportError{
						Index:   result.Total - 1,
						UID:     uid,
						Summary: summary,
						Error:   fmt.Sprintf("failed to delete existing: %v", err),
					})
					continue
				}
				// Drop the now-deleted object from the index so a subsequent
				// event with the same UID (or a create failure below) sees the
				// same state a fresh re-fetch would.
				delete(byUID, uid)
				delete(paths, existing.Path)
				preferredPath = existing.Path
			case "duplicate":
				// Generate new UID
				uid = uuid.New().String() + "@imported"
				for _, comp := range item.comps {
					comp.Props.SetText(ical.PropUID, uid)
				}
			}
		}

		for _, comp := range item.comps {
			ensureDTStamp(comp)
		}

		// Convert component back to iCalendar string. The encoder insists on
		// a PRODID/VERSION pair on the outer VCALENDAR, so set them before
		// calling Encode — the original parent `parsedCal` may have been
		// anonymous (no props) depending on where the .ics came from.
		var buf bytes.Buffer
		encoder := ical.NewEncoder(&buf)
		wrapperCal := ical.NewCalendar()
		wrapperCal.Props.SetText(ical.PropProductID, "-//CalCard//EN")
		wrapperCal.Props.SetText(ical.PropVersion, "2.0")
		wrapperCal.Children = append(wrapperCal.Children, referencedVTimezones(timezones, item.comps...)...)
		wrapperCal.Children = append(wrapperCal.Children, item.comps...)
		if err := encoder.Encode(wrapperCal); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index:   result.Total - 1,
				UID:     uid,
				Summary: summary,
				Error:   fmt.Sprintf("failed to encode: %v", err),
			})
			continue
		}

		// Store the full VCALENDAR (matching every other write path). The DB
		// UUID must be unique/non-empty (unique index, NOT NULL) — otherwise the
		// second event in a multi-event import collides on uuid="".
		obj := &calendar.CalendarObject{
			UUID:       uuid.New().String(),
			CalendarID: cal.ID,
			UID:        uid,
			Path:       paths.assign(uid, preferredPath),
			ETag:       calendar.NewETag(),
			ICalData:   buf.String(),
		}
		// Derive all denormalized columns (component type, times, recurrence
		// end, etc.) from the stored data — single source of truth.
		if err := obj.PopulateDenormFieldsFromICal(); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index:   result.Total - 1,
				UID:     uid,
				Summary: summary,
				Error:   fmt.Sprintf("failed to parse: %v", err),
			})
			continue
		}

		if err := uc.calendarRepo.CreateCalendarObject(ctx, obj); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index:   result.Total - 1,
				UID:     uid,
				Summary: summary,
				Error:   fmt.Sprintf("failed to create: %v", err),
			})
			continue
		}
		// Keep the index in sync with the calendar we're mutating so a later
		// event carrying the same UID resolves against the just-created object.
		byUID[uid] = obj

		result.Imported++
	}

	// Each CreateCalendarObject already advanced the calendar's sync_token/ctag
	// (and wrote a change-log row) inside its transaction. Do NOT re-Save the
	// stale `cal` struct here — that would clobber those tokens with the
	// pre-import values and desync the change log from the calendar row.

	return result, nil
}

// importItem is one calendar object to be imported: every component of one UID
// that belongs in the same resource (the master plus its RECURRENCE-ID
// overrides), in file order.
type importItem struct {
	uid       string
	hasUID    bool
	compName  string
	comps     []*ical.Component
	instances map[string]bool // RECURRENCE-ID values held; "" is the master
}

// summary returns the master's SUMMARY (the first component without a
// RECURRENCE-ID), falling back to the first component's, for error reporting.
func (it *importItem) summary() string {
	primary := it.comps[0]
	for _, comp := range it.comps {
		if comp.Props.Get(ical.PropRecurrenceID) == nil {
			primary = comp
			break
		}
	}
	if p := primary.Props.Get(ical.PropSummary); p != nil {
		return p.Value
	}
	return ""
}

// groupImportItems groups the VEVENT/VTODO children of a VCALENDAR into calendar
// objects, ordered by their first component in the file. Components sharing a
// UID go into one object (RFC 4791 §4.1), whatever their order in the file.
//
// A component only starts a second object for a UID already seen when it could
// not share the first one's resource: a second master, the same RECURRENCE-ID
// twice, or a different component type. Such a file is not one valid series, so
// the extra object goes through the duplicate handling like a re-imported event
// would. Components without a UID are an item each, so each one is reported.
func groupImportItems(children []*ical.Component) []*importItem {
	var items []*importItem
	latest := map[string]*importItem{}
	for _, child := range children {
		if child.Name != ical.CompEvent && child.Name != ical.CompToDo {
			continue
		}
		uidProp := child.Props.Get(ical.PropUID)
		if uidProp == nil {
			items = append(items, &importItem{comps: []*ical.Component{child}})
			continue
		}
		instance := ""
		if rid := child.Props.Get(ical.PropRecurrenceID); rid != nil {
			instance = rid.Value
		}
		it := latest[uidProp.Value]
		if it == nil || it.compName != child.Name || it.instances[instance] {
			it = &importItem{
				uid:       uidProp.Value,
				hasUID:    true,
				compName:  child.Name,
				instances: map[string]bool{},
			}
			items = append(items, it)
			latest[uidProp.Value] = it
		}
		it.comps = append(it.comps, child)
		it.instances[instance] = true
	}
	return items
}
