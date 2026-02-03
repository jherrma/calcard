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

		// Convert component back to iCalendar string
		var buf bytes.Buffer
		encoder := ical.NewEncoder(&buf)
		wrapperCal := ical.NewCalendar()
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

		// Create calendar object
		obj := &calendar.CalendarObject{
			CalendarID: cal.ID,
			UID:        uid,
			Path:       fmt.Sprintf("%s.ics", uid),
			ETag:       generateETag(),
			ICalData:   icalData,
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
