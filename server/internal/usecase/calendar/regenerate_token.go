package calendar

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type RegenerateTokenOutput struct {
	Enabled   bool       `json:"enabled"`
	PublicURL *string    `json:"public_url"`
	Token     *string    `json:"token"`
	EnabledAt *time.Time `json:"enabled_at"`
	Message   string     `json:"message"`
}

type RegenerateTokenUseCase struct {
	calendarRepo calendar.CalendarRepository
	baseURL      string
}

func NewRegenerateTokenUseCase(calendarRepo calendar.CalendarRepository, baseURL string) *RegenerateTokenUseCase {
	return &RegenerateTokenUseCase{
		calendarRepo: calendarRepo,
		baseURL:      baseURL,
	}
}

func (uc *RegenerateTokenUseCase) Execute(ctx context.Context, userID, calendarID uint) (*RegenerateTokenOutput, error) {
	cal, err := uc.calendarRepo.GetByID(ctx, calendarID)
	if err != nil || cal == nil {
		return nil, fmt.Errorf("calendar not found")
	}
	if cal.UserID != userID {
		return nil, fmt.Errorf("permission denied")
	}

	if !cal.PublicEnabled {
		return nil, fmt.Errorf("public access is not enabled")
	}

	// Generate new token
	token := generatePublicToken()
	cal.PublicToken = &token
	now := time.Now()
	cal.PublicEnabledAt = &now

	if err := uc.calendarRepo.Update(ctx, cal); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/public/calendar/%s.ics", uc.baseURL, *cal.PublicToken)
	return &RegenerateTokenOutput{
		Enabled:   true,
		PublicURL: &url,
		Token:     cal.PublicToken,
		EnabledAt: cal.PublicEnabledAt,
		Message:   "Previous public URL is no longer valid",
	}, nil
}
