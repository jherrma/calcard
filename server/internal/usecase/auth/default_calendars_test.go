package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// recordingCalendarRepo records every calendar the auth use cases create, in
// order, and fails the Create of the names listed in fail. Any other method
// panics (nil embedded interface): registration must need nothing else.
type recordingCalendarRepo struct {
	calendar.CalendarRepository
	created []calendar.Calendar
	fail    map[string]error
}

func (r *recordingCalendarRepo) Create(_ context.Context, c *calendar.Calendar) error {
	if err := r.fail[c.Name]; err != nil {
		return err
	}
	c.ID = uint(len(r.created) + 1)
	r.created = append(r.created, *c)
	return nil
}

// assertDefaultCalendars pins the defaults of a new account (#170): "Personal"
// stores VEVENT only, "Tasks" VTODO only, both owned by userID at their own
// "<uuid>.ics" path, in that order.
func assertDefaultCalendars(t *testing.T, cals []calendar.Calendar, userID uint) {
	t.Helper()
	require.Len(t, cals, 2, "exactly a calendar and a task list")
	want := []struct {
		name, components, kind, color string
	}{
		{"Personal", "VEVENT", calendar.KindEvents, "#3788d8"},
		{"Tasks", "VTODO", calendar.KindTasks, "#28a745"},
	}
	for i, w := range want {
		c := cals[i]
		assert.Equal(t, w.name, c.Name, "calendar %d", i)
		assert.Equal(t, w.components, c.SupportedComponents, w.name)
		assert.Equal(t, w.kind, calendar.KindOf(&c), w.name)
		assert.Equal(t, w.color, c.Color, w.name)
		assert.Equal(t, userID, c.UserID, w.name)
		_, err := uuid.Parse(c.UUID)
		assert.NoError(t, err, "%s has a UUID", w.name)
		assert.Equal(t, c.UUID+".ics", c.Path, w.name)
		assert.Equal(t, "UTC", c.Timezone, w.name)
		assert.Empty(t, c.Description, w.name)
		assert.False(t, c.Subscribed, w.name)
	}
	assert.NotEqual(t, cals[0].UUID, cals[1].UUID)
	assert.False(t, calendar.SupportsComponent(&cals[0], calendar.ComponentToDo), "Personal is not a task list")
	assert.False(t, calendar.SupportsComponent(&cals[1], calendar.ComponentEvent), "Tasks is not an event calendar")
}

// registerWith runs a registration (no SMTP) whose user gets id 42.
func registerWith(t *testing.T, calRepo calendar.CalendarRepository, abRepo *mockAddressBookRepo) (*user.User, error) {
	t.Helper()
	repo := new(mockUserRepo)
	ctx := context.Background()
	repo.On("GetByEmail", ctx, "new@example.com").Return(nil, nil)
	repo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)
	repo.On("Create", ctx, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*user.User).ID = 42
	}).Return(nil)
	uc := NewRegisterUseCase(repo, calRepo, abRepo, new(mockEmailService), &config.Config{})
	u, _, err := uc.Execute(ctx, "new@example.com", "SecurePass123!", "New")
	return u, err
}

// A newly registered user owns "Personal" (VEVENT) and "Tasks" (VTODO), and
// still the default address book.
func TestRegisterUseCase_CreatesPersonalAndTasks(t *testing.T) {
	calRepo := &recordingCalendarRepo{}
	abRepo := new(mockAddressBookRepo)
	abRepo.On("Create", mock.Anything, mock.Anything).Return(nil)

	u, err := registerWith(t, calRepo, abRepo)

	require.NoError(t, err)
	assertDefaultCalendars(t, calRepo.created, u.ID)
	abRepo.AssertNumberOfCalls(t, "Create", 1)
}

// The first admin created by setup owns the same two collections.
func TestSetupUseCase_CreatesPersonalAndTasks(t *testing.T) {
	repo := new(mockUserRepo)
	calRepo := &recordingCalendarRepo{}
	abRepo := new(mockAddressBookRepo)
	cfg := &config.Config{}
	uc := NewSetupUseCase(repo, NewRegisterUseCase(repo, calRepo, abRepo, new(mockEmailService), cfg), "")
	ctx := context.Background()
	repo.On("Count", ctx).Return(int64(0), nil)
	repo.On("Create", ctx, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*user.User).ID = 7
	}).Return(nil)
	abRepo.On("Create", ctx, mock.Anything).Return(nil)

	u, err := uc.Execute(ctx, "admin", "admin@example.com", "SecurePass123!", "Admin", "")

	require.NoError(t, err)
	require.True(t, u.IsAdmin)
	assertDefaultCalendars(t, calRepo.created, 7)
	abRepo.AssertNumberOfCalls(t, "Create", 1)
}

// A default calendar that cannot be created is logged, never fails the
// account, and does not keep the other default (or the address book) from
// being created.
func TestRegisterUseCase_DefaultCalendarFailures(t *testing.T) {
	boom := errors.New("disk full")
	for _, tc := range []struct {
		name    string
		fail    []string
		created []string
	}{
		{"Personal fails", []string{"Personal"}, []string{"Tasks"}},
		{"Tasks fails", []string{"Tasks"}, []string{"Personal"}},
		{"both fail", []string{"Personal", "Tasks"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calRepo := &recordingCalendarRepo{fail: map[string]error{}}
			for _, name := range tc.fail {
				calRepo.fail[name] = boom
			}
			abRepo := new(mockAddressBookRepo)
			abRepo.On("Create", mock.Anything, mock.Anything).Return(nil)

			u, err := registerWith(t, calRepo, abRepo)

			require.NoError(t, err, "registration must not fail for a default calendar")
			require.NotNil(t, u)
			names := []string{}
			for _, c := range calRepo.created {
				names = append(names, c.Name)
			}
			assert.Equal(t, tc.created, names)
			abRepo.AssertNumberOfCalls(t, "Create", 1)
		})
	}

	t.Run("the error names each failed calendar", func(t *testing.T) {
		calRepo := &recordingCalendarRepo{fail: map[string]error{"Personal": boom, "Tasks": boom}}
		uc := NewRegisterUseCase(new(mockUserRepo), calRepo, new(mockAddressBookRepo), new(mockEmailService), &config.Config{})

		err := uc.createDefaultCalendars(context.Background(), 1)

		require.Error(t, err)
		assert.ErrorIs(t, err, boom)
		assert.Equal(t, "Personal: disk full\nTasks: disk full", err.Error())
	})
}

// createDefaultCalendar takes a kind from the closed enum; an unknown one
// creates nothing.
func TestCreateDefaultCalendar_UnknownKind(t *testing.T) {
	calRepo := &recordingCalendarRepo{}
	uc := NewRegisterUseCase(new(mockUserRepo), calRepo, new(mockAddressBookRepo), new(mockEmailService), &config.Config{})

	err := uc.createDefaultCalendar(context.Background(), 1, "Journal", "journal", "#000000")

	assert.ErrorIs(t, err, calendar.ErrInvalidInput)
	assert.Empty(t, calRepo.created)
}
