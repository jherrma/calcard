package sharing

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

type ListCalendarSharesUseCase struct {
	shareRepo    sharing.CalendarShareRepository
	calendarRepo calendar.CalendarRepository
}

func NewListCalendarSharesUseCase(
	shareRepo sharing.CalendarShareRepository,
	calendarRepo calendar.CalendarRepository,
) *ListCalendarSharesUseCase {
	return &ListCalendarSharesUseCase{
		shareRepo:    shareRepo,
		calendarRepo: calendarRepo,
	}
}

func (uc *ListCalendarSharesUseCase) Execute(ctx context.Context, requestingUserID uint, calendarID uint) ([]CreateCalendarShareOutput, error) {
	// 1. Verify ownership
	cal, err := uc.calendarRepo.GetByID(ctx, calendarID)
	if err != nil {
		return nil, fmt.Errorf("calendar not found")
	}
	if cal.UserID != requestingUserID {
		return nil, fmt.Errorf("permission denied")
	}

	// 2. List shares
	shares, err := uc.shareRepo.ListByCalendarID(ctx, calendarID)
	if err != nil {
		return nil, err
	}

	// 3. Map to output
	output := make([]CreateCalendarShareOutput, len(shares))
	for i, share := range shares {
		output[i] = CreateCalendarShareOutput{
			ID:         share.UUID,
			CalendarID: cal.UUID,
			SharedWith: UserInfo{
				ID:          share.SharedWith.UUID,
				Username:    share.SharedWith.Username,
				DisplayName: share.SharedWith.DisplayName,
				Email:       share.SharedWith.Email,
			},
			Permission: share.Permission,
			CreatedAt:  share.CreatedAt,
		}
	}

	return output, nil
}
