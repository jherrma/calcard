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
	// Get calendar and verify ownership
	cal, err := uc.calendarRepo.GetByUUID(ctx, calendarUUID)
	if err != nil {
		return nil, fmt.Errorf("calendar not found")
	}
	if cal.UserID != userID {
		return nil, fmt.Errorf("access denied")
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

	// Get all VEVENT and VTODO components
	for _, child := range parsedCal.Children {
		if child.Name != ical.CompEvent && child.Name != ical.CompToDo {
			continue
		}

		result.Total++

		// Get UID
		uidProp := child.Props.Get(ical.PropUID)
		if uidProp == nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index: result.Total - 1,
				Error: "Missing UID property",
			})
			continue
		}
		uid := uidProp.Value

		// Get Summary for error reporting
		summaryProp := child.Props.Get(ical.PropSummary)
		summary := ""
		if summaryProp != nil {
			summary = summaryProp.Value
		}

		// Check for existing event by UID (indexed once, above the loop)
		existing := byUID[uid]

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
			case "duplicate":
				// Generate new UID
				uid = uuid.New().String() + "@imported"
				child.Props.SetText(ical.PropUID, uid)
			}
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
		wrapperCal.Children = append(wrapperCal.Children, child)
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
			Path:       fmt.Sprintf("%s.ics", uid),
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
