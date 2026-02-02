package sharing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

type CreateCalendarShareInput struct {
	CalendarID     uint   `json:"calendar_id"`
	UserIdentifier string `json:"user_identifier"` // Email or Username
	Permission     string `json:"permission"`
}

type CreateCalendarShareOutput struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendar_id"`
	SharedWith UserInfo  `json:"shared_with"`
	Permission string    `json:"permission"`
	CreatedAt  time.Time `json:"created_at"`
}

type UserInfo struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type CreateCalendarShareUseCase struct {
	shareRepo    sharing.CalendarShareRepository
	calendarRepo calendar.CalendarRepository
	userRepo     user.UserRepository
}

func NewCreateCalendarShareUseCase(
	shareRepo sharing.CalendarShareRepository,
	calendarRepo calendar.CalendarRepository,
	userRepo user.UserRepository,
) *CreateCalendarShareUseCase {
	return &CreateCalendarShareUseCase{
		shareRepo:    shareRepo,
		calendarRepo: calendarRepo,
		userRepo:     userRepo,
	}
}

func (uc *CreateCalendarShareUseCase) Execute(ctx context.Context, requestingUserID uint, input CreateCalendarShareInput) (*CreateCalendarShareOutput, error) {
	// 1. Verify calendar ownership
	cal, err := uc.calendarRepo.GetByID(ctx, input.CalendarID)
	if err != nil {
		return nil, fmt.Errorf("calendar not found")
	}
	if cal.UserID != requestingUserID {
		return nil, fmt.Errorf("permission denied")
	}

	// 2. Find target user
	targetUser, err := uc.userRepo.GetByEmail(ctx, input.UserIdentifier)
	if err != nil || targetUser == nil {
		// Try by username
		targetUser, err = uc.userRepo.GetByUsername(ctx, input.UserIdentifier)
	}
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("user '%s' not found", input.UserIdentifier)
	}

	// 3. Validation
	if targetUser.ID == requestingUserID {
		return nil, fmt.Errorf("cannot share calendar with yourself")
	}
	if input.Permission != "read" && input.Permission != "read-write" {
		return nil, fmt.Errorf("invalid permission")
	}

	// 4. Check existing share
	existing, _ := uc.shareRepo.GetByCalendarAndUser(ctx, input.CalendarID, targetUser.ID)
	if existing != nil {
		// Update existing share if found? Requirement says "Cannot share same calendar to same user twice"
		// implying conflict or update. Let's return error for now as per implied AC
		return nil, fmt.Errorf("calendar is already shared with this user")
	}

	// 5. Create share
	share := &sharing.CalendarShare{
		UUID:         uuid.New().String(),
		CalendarID:   input.CalendarID,
		SharedWithID: targetUser.ID,
		Permission:   input.Permission,
	}

	if err := uc.shareRepo.Create(ctx, share); err != nil {
		return nil, err
	}

	// 6. Return output
	return &CreateCalendarShareOutput{
		ID:         share.UUID,
		CalendarID: cal.UUID,
		SharedWith: UserInfo{
			ID:          targetUser.UUID,
			Username:    targetUser.Username,
			DisplayName: targetUser.DisplayName,
			Email:       targetUser.Email,
		},
		Permission: share.Permission,
		CreatedAt:  share.CreatedAt,
	}, nil
}
