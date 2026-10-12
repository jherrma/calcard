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
	cal, err := ownedCalendar(ctx, uc.calendarRepo, requestingUserID, calendarID)
	if err != nil {
		return nil, err
	}

	// 2. Get share
	// The repository answers (nil, nil) for an unknown id; an error is a
	// storage failure, not a missing share.
	share, err := uc.shareRepo.GetByUUID(ctx, shareUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to load share: %w", err)
	}
	if share == nil {
		return nil, ErrCalendarShareNotFound
	}

	// 3. Verify share belongs to calendar
	if share.CalendarID != calendarID {
		return nil, ErrCalendarShareNotFound
	}

	// 4. Validate permission
	if input.Permission != "read" && input.Permission != "read-write" {
		return nil, calendar.Invalidf("invalid permission")
	}

	// 5. Update share
	share.Permission = input.Permission
	share.UpdatedAt = time.Now()

	if err := uc.shareRepo.Update(ctx, share); err != nil {
		return nil, fmt.Errorf("failed to update share: %w", err)
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
