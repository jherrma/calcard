package sharing

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

type RevokeCalendarShareUseCase struct {
	shareRepo    sharing.CalendarShareRepository
	calendarRepo calendar.CalendarRepository
}

func NewRevokeCalendarShareUseCase(
	shareRepo sharing.CalendarShareRepository,
	calendarRepo calendar.CalendarRepository,
) *RevokeCalendarShareUseCase {
	return &RevokeCalendarShareUseCase{
		shareRepo:    shareRepo,
		calendarRepo: calendarRepo,
	}
}

func (uc *RevokeCalendarShareUseCase) Execute(ctx context.Context, requestingUserID uint, calendarID uint, shareUUID string) error {
	// 1. Verify ownership
	cal, err := uc.calendarRepo.GetByID(ctx, calendarID)
	if err != nil {
		return fmt.Errorf("calendar not found")
	}
	if cal.UserID != requestingUserID {
		return fmt.Errorf("permission denied")
	}

	// 2. Get share
	share, err := uc.shareRepo.GetByUUID(ctx, shareUUID)
	if err != nil || share == nil {
		return fmt.Errorf("share not found")
	}

	// 3. Verify share belongs to calendar
	if share.CalendarID != calendarID {
		return fmt.Errorf("share not found")
	}

	// 4. Revoke (Delete)
	return uc.shareRepo.Revoke(ctx, share.ID)
}
