package calendar

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type GetPublicStatusUseCase struct {
	calendarRepo calendar.CalendarRepository
	baseURL      string
}

func NewGetPublicStatusUseCase(calendarRepo calendar.CalendarRepository, baseURL string) *GetPublicStatusUseCase {
	return &GetPublicStatusUseCase{
		calendarRepo: calendarRepo,
		baseURL:      baseURL,
	}
}

func (uc *GetPublicStatusUseCase) Execute(ctx context.Context, userID, calendarID uint) (*EnablePublicOutput, error) {
	cal, err := uc.calendarRepo.GetByID(ctx, calendarID)
	if err != nil || cal == nil {
		return nil, fmt.Errorf("calendar not found")
	}
	if cal.UserID != userID {
		return nil, fmt.Errorf("permission denied")
	}

	output := &EnablePublicOutput{
		Enabled:   cal.PublicEnabled,
		EnabledAt: cal.PublicEnabledAt,
	}
	if cal.PublicEnabled && cal.PublicToken != nil && *cal.PublicToken != "" {
		url := fmt.Sprintf("%s/public/calendar/%s.ics", uc.baseURL, *cal.PublicToken)
		output.PublicURL = &url
		output.Token = cal.PublicToken
	}

	return output, nil
}
