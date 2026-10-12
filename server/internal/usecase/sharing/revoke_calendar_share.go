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
	if _, err := ownedCalendar(ctx, uc.calendarRepo, requestingUserID, calendarID); err != nil {
		return err
	}

	// 2. Get share. The repository answers (nil, nil) for an unknown id; an
	// error is a storage failure, not a missing share.
	share, err := uc.shareRepo.GetByUUID(ctx, shareUUID)
	if err != nil {
		return fmt.Errorf("failed to load share: %w", err)
	}
	if share == nil {
		return ErrCalendarShareNotFound
	}

	// 3. Verify share belongs to calendar
	if share.CalendarID != calendarID {
		return ErrCalendarShareNotFound
	}

	// 4. Revoke (Delete)
	if err := uc.shareRepo.Revoke(ctx, share.ID); err != nil {
		return fmt.Errorf("failed to revoke share: %w", err)
	}
	return nil
}
