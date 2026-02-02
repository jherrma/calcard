package sharing

import (
	"context"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocks
type mockShareRepo struct {
	mock.Mock
}

func (m *mockShareRepo) Create(ctx context.Context, share *sharing.CalendarShare) error {
	args := m.Called(ctx, share)
	return args.Error(0)
}
func (m *mockShareRepo) GetByUUID(ctx context.Context, uuid string) (*sharing.CalendarShare, error) {
	args := m.Called(ctx, uuid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sharing.CalendarShare), args.Error(1)
}
func (m *mockShareRepo) ListByCalendarID(ctx context.Context, calendarID uint) ([]sharing.CalendarShare, error) {
	args := m.Called(ctx, calendarID)
	return args.Get(0).([]sharing.CalendarShare), args.Error(1)
}
func (m *mockShareRepo) FindCalendarsSharedWithUser(ctx context.Context, userID uint) ([]sharing.CalendarShare, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]sharing.CalendarShare), args.Error(1)
}
func (m *mockShareRepo) Update(ctx context.Context, share *sharing.CalendarShare) error {
	args := m.Called(ctx, share)
	return args.Error(0)
}
func (m *mockShareRepo) Revoke(ctx context.Context, id uint) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}
func (m *mockShareRepo) GetByCalendarAndUser(ctx context.Context, calendarID, userID uint) (*sharing.CalendarShare, error) {
	args := m.Called(ctx, calendarID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sharing.CalendarShare), args.Error(1)
}

type mockCalendarRepo struct {
	mock.Mock
}

// Implement minimal interface for tests
func (m *mockCalendarRepo) GetByID(ctx context.Context, id uint) (*calendar.Calendar, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*calendar.Calendar), args.Error(1)
}

// Add other required methods as mocks returning defaults/panics if strictly needed by interface but not used in test
func (m *mockCalendarRepo) Create(ctx context.Context, c *calendar.Calendar) error { return nil }
func (m *mockCalendarRepo) GetByUUID(ctx context.Context, u string) (*calendar.Calendar, error) {
	return nil, nil
}
func (m *mockCalendarRepo) ListByUserID(ctx context.Context, id uint) ([]*calendar.Calendar, error) {
	return nil, nil
}
func (m *mockCalendarRepo) Update(ctx context.Context, c *calendar.Calendar) error    { return nil }
func (m *mockCalendarRepo) Delete(ctx context.Context, id uint) error                 { return nil }
func (m *mockCalendarRepo) CountByUserID(ctx context.Context, id uint) (int64, error) { return 0, nil }
func (m *mockCalendarRepo) GetEventCount(ctx context.Context, id uint) (int64, error) { return 0, nil }
func (m *mockCalendarRepo) GetCalendarObjects(ctx context.Context, id uint) ([]*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetByPath(ctx context.Context, uid uint, p string) (*calendar.Calendar, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetCalendarObjectByPath(ctx context.Context, cid uint, p string) (*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) CreateCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return nil
}
func (m *mockCalendarRepo) UpdateCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return nil
}
func (m *mockCalendarRepo) DeleteCalendarObject(ctx context.Context, obj *calendar.CalendarObject) error {
	return nil
}
func (m *mockCalendarRepo) GetChangesSinceToken(ctx context.Context, cid uint, t string) ([]*calendar.SyncChangeLog, error) {
	return nil, nil
}
func (m *mockCalendarRepo) ListEvents(ctx context.Context, cid uint, s, e time.Time) ([]*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetCalendarObjectByUUID(ctx context.Context, u string) (*calendar.CalendarObject, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetUserPermission(ctx context.Context, cid, uid uint) (calendar.CalendarPermission, error) {
	return 0, nil
}

type mockUserRepo struct {
	mock.Mock
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}
func (m *mockUserRepo) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	args := m.Called(ctx, username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

// Stub others
func (m *mockUserRepo) Create(ctx context.Context, u *user.User) error              { return nil }
func (m *mockUserRepo) GetByID(ctx context.Context, id uint) (*user.User, error)    { return nil, nil }
func (m *mockUserRepo) GetByUUID(ctx context.Context, u string) (*user.User, error) { return nil, nil }
func (m *mockUserRepo) Update(ctx context.Context, u *user.User) error              { return nil }
func (m *mockUserRepo) Delete(ctx context.Context, id uint) error                   { return nil }
func (m *mockUserRepo) CreateVerification(ctx context.Context, v *user.EmailVerification) error {
	return nil
}
func (m *mockUserRepo) GetVerification(ctx context.Context, token string) (*user.EmailVerification, error) {
	return nil, nil
}
func (m *mockUserRepo) DeleteVerification(ctx context.Context, token string) error { return nil }
func (m *mockUserRepo) GetByOAuth(ctx context.Context, provider, providerID string) (*user.User, error) {
	return nil, nil
}
func (m *mockUserRepo) GetVerificationByToken(ctx context.Context, token string) (*user.EmailVerification, error) {
	return nil, nil
}

func TestCreateCalendarShare(t *testing.T) {
	shareRepo := new(mockShareRepo)
	calendarRepo := new(mockCalendarRepo)
	userRepo := new(mockUserRepo)

	uc := NewCreateCalendarShareUseCase(shareRepo, calendarRepo, userRepo)

	ctx := context.Background()
	ownerID := uint(1)
	targetID := uint(2)
	calID := uint(10)

	cal := &calendar.Calendar{ID: calID, UserID: ownerID, UUID: "cal-uuid"}
	targetUser := &user.User{ID: targetID, Username: "target", Email: "target@example.com", UUID: "user-uuid"}

	input := CreateCalendarShareInput{
		CalendarID:     calID,
		UserIdentifier: "target@example.com",
		Permission:     "read-write",
	}

	t.Run("Success", func(t *testing.T) {
		calendarRepo.On("GetByID", ctx, calID).Return(cal, nil).Once()
		userRepo.On("GetByEmail", ctx, "target@example.com").Return(targetUser, nil).Once()
		shareRepo.On("GetByCalendarAndUser", ctx, calID, targetID).Return(nil, nil).Once()
		shareRepo.On("Create", ctx, mock.AnythingOfType("*sharing.CalendarShare")).Return(nil).Once()

		output, err := uc.Execute(ctx, ownerID, input)

		assert.NoError(t, err)
		assert.Equal(t, "read-write", output.Permission)
		assert.Equal(t, "target", output.SharedWith.Username)
	})

	t.Run("Permission Denied (Not Owner)", func(t *testing.T) {
		otherCal := &calendar.Calendar{ID: calID, UserID: 999}
		calendarRepo.On("GetByID", ctx, calID).Return(otherCal, nil).Once()

		_, err := uc.Execute(ctx, ownerID, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})
}
