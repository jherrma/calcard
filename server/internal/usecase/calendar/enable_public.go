package calendar

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type EnablePublicInput struct {
	Enabled bool `json:"enabled"`
}

type EnablePublicOutput struct {
	Enabled   bool       `json:"enabled"`
	PublicURL *string    `json:"public_url"`
	Token     *string    `json:"token"`
	EnabledAt *time.Time `json:"enabled_at"`
}

type EnablePublicUseCase struct {
	calendarRepo calendar.CalendarRepository
	baseURL      string
}

func NewEnablePublicUseCase(calendarRepo calendar.CalendarRepository, baseURL string) *EnablePublicUseCase {
	return &EnablePublicUseCase{
		calendarRepo: calendarRepo,
		baseURL:      baseURL,
	}
}

func generatePublicToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func (uc *EnablePublicUseCase) Execute(ctx context.Context, userID, calendarID uint, input EnablePublicInput) (*EnablePublicOutput, error) {
	cal, err := uc.calendarRepo.GetByID(ctx, calendarID)
	if err != nil || cal == nil {
		return nil, fmt.Errorf("calendar not found")
	}
	if cal.UserID != userID {
		return nil, fmt.Errorf("permission denied")
	}

	if input.Enabled {
		// Enable public access
		if cal.PublicToken == nil || *cal.PublicToken == "" {
			token := generatePublicToken()
			cal.PublicToken = &token
		}
		cal.PublicEnabled = true
		now := time.Now()
		cal.PublicEnabledAt = &now
	} else {
		// Disable public access
		cal.PublicEnabled = false
		cal.PublicToken = nil
		cal.PublicEnabledAt = nil
	}

	if err := uc.calendarRepo.Update(ctx, cal); err != nil {
		return nil, err
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
