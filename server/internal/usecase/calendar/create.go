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
		SupportedComponents: "VEVENT,VTODO",
		SyncToken:           calendar.GenerateSyncToken(),
		CTag:                calendar.GenerateCTag(),
	}

	if err := uc.repo.Create(ctx, cal); err != nil {
		return nil, fmt.Errorf("failed to create calendar: %w", err)
	}

	return cal, nil
}
