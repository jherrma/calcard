package calendar

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type GetPublicStatusUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewGetPublicStatusUseCase(calendarRepo calendar.CalendarRepository) *GetPublicStatusUseCase {
	return &GetPublicStatusUseCase{calendarRepo: calendarRepo}
}

// Execute reports the public-access state. baseURL is the origin the
// returned public_url is built on (the handler resolves it per request).
func (uc *GetPublicStatusUseCase) Execute(ctx context.Context, userID, calendarID uint, baseURL string) (*EnablePublicOutput, error) {
	cal, err := LoadOwnedCalendarByID(ctx, uc.calendarRepo, userID, calendarID)
	if err != nil {
		return nil, err
	}

	output := &EnablePublicOutput{
		Enabled:   cal.PublicEnabled,
		EnabledAt: cal.PublicEnabledAt,
	}
	if cal.PublicEnabled && cal.PublicToken != nil && *cal.PublicToken != "" {
		url := fmt.Sprintf("%s/public/calendar/%s.ics", baseURL, *cal.PublicToken)
		output.PublicURL = &url
		output.Token = cal.PublicToken
	}

	return output, nil
}
