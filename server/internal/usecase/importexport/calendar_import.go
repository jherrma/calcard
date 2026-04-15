package importexport

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

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

		// Check for existing event by UID
		existingObjects, _ := uc.calendarRepo.GetCalendarObjects(ctx, cal.ID)
		var existing *calendar.CalendarObject
		for _, obj := range existingObjects {
			if obj.UID == uid {
				existing = obj
				break
			}
		}

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

		// Extract just the VEVENT/VTODO block (remove VCALENDAR wrapper)
		icalData := extractComponentBlock(buf.String(), child.Name)

		// Extract denormalized fields the list/get endpoints rely on. These
		// mirror the population done by event.CreateEventUseCase so that
		// re-imported events are indistinguishable from freshly created ones.
		description := ""
		if p := child.Props.Get(ical.PropDescription); p != nil {
			description = p.Value
		}
		location := ""
		if p := child.Props.Get(ical.PropLocation); p != nil {
			location = p.Value
		}
		componentType := child.Name // VEVENT or VTODO
		startTime, endTime, isAllDay := extractEventTimes(child)

		// Create calendar object. The internal DB UUID must be unique and
		// non-empty (the column has a unique index and NOT NULL) — otherwise
		// the second event in a multi-event import collides on uuid="".
		obj := &calendar.CalendarObject{
			UUID:          uuid.New().String(),
			CalendarID:    cal.ID,
			UID:           uid,
			Path:          fmt.Sprintf("%s.ics", uid),
			ETag:          generateETag(),
			ICalData:      icalData,
			ComponentType: componentType,
			ContentLength: len(icalData),
			Summary:       summary,
			Description:   description,
			Location:      location,
			StartTime:     startTime,
			EndTime:       endTime,
			IsAllDay:      isAllDay,
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

		result.Imported++
	}

	// Update calendar CTag
	cal.CTag = fmt.Sprintf("ctag-%d", time.Now().UnixNano())
	_ = uc.calendarRepo.Update(ctx, cal)

	return result, nil
}

// extractComponentBlock extracts the VEVENT or VTODO block from full iCalendar
func extractComponentBlock(icalData, componentName string) string {
	startTag := "BEGIN:" + componentName
	endTag := "END:" + componentName

	startIdx := strings.Index(icalData, startTag)
	endIdx := strings.LastIndex(icalData, endTag)

	if startIdx == -1 || endIdx == -1 {
		return icalData
	}

	return icalData[startIdx : endIdx+len(endTag)+2] // +2 for \r\n
}

// generateETag generates a unique ETag
func generateETag() string {
	return fmt.Sprintf("\"%d\"", time.Now().UnixNano())
}

// extractEventTimes pulls DTSTART / DTEND out of a VEVENT/VTODO component and
// returns them as pointers along with the all-day flag. Any parse failure or
// missing property returns (nil, nil, false) — the caller writes those as-is
// and downstream list/query code already guards against nil times.
func extractEventTimes(comp *ical.Component) (*time.Time, *time.Time, bool) {
	var start, end *time.Time
	allDay := false

	if prop := comp.Props.Get(ical.PropDateTimeStart); prop != nil {
		if t, err := prop.DateTime(time.UTC); err == nil {
			tt := t
			start = &tt
			if v := prop.Params.Get(ical.ParamValue); v == "DATE" {
				allDay = true
			}
		}
	}
	if prop := comp.Props.Get(ical.PropDateTimeEnd); prop != nil {
		if t, err := prop.DateTime(time.UTC); err == nil {
			tt := t
			end = &tt
		}
	}
	return start, end, allDay
}
