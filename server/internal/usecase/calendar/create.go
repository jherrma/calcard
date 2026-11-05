package calendar

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// CreateCalendarRequest represents the request to create a calendar
type CreateCalendarRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Timezone    string `json:"timezone"`
	// Kind picks what the calendar stores (#170): "events" (VEVENT, the
	// default when empty), "tasks" (VTODO, a task list) or "both"
	// (VEVENT,VTODO). Any other value is calendar.ErrInvalidInput (400). A
	// closed enum on purpose: a free-form component set would let callers
	// store sets CalDAV cannot advertise.
	Kind string `json:"kind"`
}

// CreateCalendarUseCase handles calendar creation
type CreateCalendarUseCase struct {
	repo calendar.CalendarRepository
}

// NewCreateCalendarUseCase creates a new use case
func NewCreateCalendarUseCase(repo calendar.CalendarRepository) *CreateCalendarUseCase {
	return &CreateCalendarUseCase{repo: repo}
}

// Execute creates a new calendar
func (uc *CreateCalendarUseCase) Execute(ctx context.Context, userID uint, req CreateCalendarRequest) (*calendar.Calendar, error) {
	// Validate name
	if err := calendar.ValidateName(req.Name); err != nil {
		return nil, err
	}

	// Validate description
	if err := calendar.ValidateDescription(req.Description); err != nil {
		return nil, err
	}

	// Validate or set default color
	color := req.Color
	if color == "" {
		color = calendar.GenerateRandomColor()
	} else {
		if err := calendar.ValidateHexColor(color); err != nil {
			return nil, err
		}
	}

	// Validate or set default timezone
	timezone := req.Timezone
	if timezone == "" {
		timezone = "UTC"
	} else {
		if err := calendar.ValidateTimezone(timezone); err != nil {
			return nil, err
		}
	}

	// Validate or set default kind. Without one the calendar is a plain event
	// calendar (#170): task lists are asked for explicitly with "tasks".
	kind := req.Kind
	if kind == "" {
		kind = calendar.KindEvents
	}
	components, err := calendar.SupportedComponentsForKind(kind)
	if err != nil {
		return nil, err
	}

	// Generate UUID and path
	calUUID := uuid.New().String()
	path := fmt.Sprintf("%s.ics", calUUID)

	// Create calendar
	cal := &calendar.Calendar{
		UUID:                calUUID,
		UserID:              userID,
		Path:                path,
		Name:                req.Name,
		Description:         req.Description,
		Color:               color,
		Timezone:            timezone,
		SupportedComponents: components,
		// SyncToken/CTag are minted by the repository inside Create, together
		// with a matching change-log anchor row.
	}

	if err := uc.repo.Create(ctx, cal); err != nil {
		return nil, fmt.Errorf("failed to create calendar: %w", err)
	}

	return cal, nil
}
