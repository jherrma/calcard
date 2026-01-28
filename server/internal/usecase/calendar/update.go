package calendar

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// UpdateCalendarRequest represents the request to update a calendar
type UpdateCalendarRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Timezone    *string `json:"timezone"`
}

// UpdateCalendarUseCase handles calendar updates
type UpdateCalendarUseCase struct {
	repo calendar.CalendarRepository
}

// NewUpdateCalendarUseCase creates a new use case
func NewUpdateCalendarUseCase(repo calendar.CalendarRepository) *UpdateCalendarUseCase {
	return &UpdateCalendarUseCase{repo: repo}
}

// Execute updates a calendar
func (uc *UpdateCalendarUseCase) Execute(ctx context.Context, userID uint, calendarUUID string, req UpdateCalendarRequest) (*calendar.Calendar, error) {
	// Get existing calendar
	cal, err := uc.repo.GetByUUID(ctx, calendarUUID)
	if err != nil {
		return nil, fmt.Errorf("calendar not found")
	}

	// Verify ownership
	if cal.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	// Update fields if provided
	if req.Name != nil {
		if err := calendar.ValidateName(*req.Name); err != nil {
			return nil, err
		}
		cal.Name = *req.Name
	}

	if req.Description != nil {
		if err := calendar.ValidateDescription(*req.Description); err != nil {
			return nil, err
		}
		cal.Description = *req.Description
	}

	if req.Color != nil {
		if err := calendar.ValidateHexColor(*req.Color); err != nil {
			return nil, err
		}
		cal.Color = *req.Color
	}

	if req.Timezone != nil {
		if err := calendar.ValidateTimezone(*req.Timezone); err != nil {
			return nil, err
		}
		cal.Timezone = *req.Timezone
	}

	// Update sync tokens
	cal.UpdateSyncTokens()

	if err := uc.repo.Update(ctx, cal); err != nil {
		return nil, fmt.Errorf("failed to update calendar: %w", err)
	}

	return cal, nil
}
