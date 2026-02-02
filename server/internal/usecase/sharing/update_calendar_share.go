package sharing

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

type UpdateCalendarShareInput struct {
	Permission string `json:"permission"`
}

type UpdateCalendarShareUseCase struct {
	shareRepo    sharing.CalendarShareRepository
	calendarRepo calendar.CalendarRepository
}

func NewUpdateCalendarShareUseCase(
	shareRepo sharing.CalendarShareRepository,
	calendarRepo calendar.CalendarRepository,
) *UpdateCalendarShareUseCase {
	return &UpdateCalendarShareUseCase{
		shareRepo:    shareRepo,
		calendarRepo: calendarRepo,
	}
}

func (uc *UpdateCalendarShareUseCase) Execute(ctx context.Context, requestingUserID uint, calendarID uint, shareUUID string, input UpdateCalendarShareInput) (*CreateCalendarShareOutput, error) {
	// 1. Verify ownership
	cal, err := uc.calendarRepo.GetByID(ctx, calendarID)
	if err != nil {
		return nil, fmt.Errorf("calendar not found")
	}
	if cal.UserID != requestingUserID {
		return nil, fmt.Errorf("permission denied")
	}

	// 2. Get share
	share, err := uc.shareRepo.GetByUUID(ctx, shareUUID)
	if err != nil || share == nil {
		return nil, fmt.Errorf("share not found")
	}

	// 3. Verify share belongs to calendar
	if share.CalendarID != calendarID {
		return nil, fmt.Errorf("share not found")
	}

	// 4. Validate permission
	if input.Permission != "read" && input.Permission != "read-write" {
		return nil, fmt.Errorf("invalid permission")
	}

	// 5. Update share
	share.Permission = input.Permission
	share.UpdatedAt = time.Now()

	if err := uc.shareRepo.Update(ctx, share); err != nil {
		return nil, err
	}

	// 6. Return output
	return &CreateCalendarShareOutput{
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
	}, nil
}
