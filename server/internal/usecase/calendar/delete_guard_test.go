package calendar

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deleteStubRepo implements only what the delete use case calls: GetByUUID,
// ListByUserID and Delete. The embedded interface is nil, so any other
// repository call panics (CountByUserID included: the guard no longer counts
// every calendar). ListByUserID returns `listed` as given, so a test can hand
// the use case rows it should not count. Deletes are recorded, not applied.
type deleteStubRepo struct {
	calendar.CalendarRepository

	listed  []*calendar.Calendar
	listErr error

	listCalls int
	deleted   []uint
}

func (r *deleteStubRepo) GetByUUID(_ context.Context, uuid string) (*calendar.Calendar, error) {
	for _, cal := range r.listed {
		if cal.UUID == uuid {
			cp := *cal
			return &cp, nil
		}
	}
	return nil, calendar.ErrNotFound
}

func (r *deleteStubRepo) ListByUserID(context.Context, uint) ([]*calendar.Calendar, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listed, nil
}

func (r *deleteStubRepo) Delete(_ context.Context, id uint) error {
	r.deleted = append(r.deleted, id)
	return nil
}

const lastCalendarMessage = "cannot delete your last calendar"

// ownedCal is a calendar of ownerID storing set.
func ownedCal(id uint, set string) *calendar.Calendar {
	return &calendar.Calendar{
		ID: id, UUID: fmt.Sprintf("cal-%d", id), UserID: ownerID,
		Name: fmt.Sprintf("Calendar %d", id), Timezone: "UTC", SupportedComponents: set,
	}
}

var confirmed = DeleteCalendarRequest{Confirmation: "DELETE"}

// TestDeleteCalendar_LastCalendarGuardPerKind pins the per-kind last-calendar
// guard (#170, spec §5 rule 6) for every stored set of the calendar being
// deleted against every set of the user's other owned calendars: a calendar
// that supports VEVENT (VEVENT, VEVENT,VTODO and a legacy "") is refused when
// no OTHER owned calendar supports VEVENT, with the same 400 as before; a
// VTODO-only task list is always deleted, the user's last one included; task
// lists never count as a remaining calendar.
func TestDeleteCalendar_LastCalendarGuardPerKind(t *testing.T) {
	ctx := context.Background()
	eventCapable := map[string]bool{"VEVENT": true, "VEVENT,VTODO": true, "": true, "VTODO": false}

	for _, target := range []string{"VEVENT", "VEVENT,VTODO", "", "VTODO"} {
		for _, others := range [][]string{
			nil,                 // the only calendar
			{"VTODO"},           // only a task list next to it
			{"VTODO", "VTODO"},  // several task lists
			{"VEVENT"},          // another event calendar
			{"VEVENT,VTODO"},    // a mixed calendar
			{""},                // a legacy row (stores both)
			{"VTODO", "VEVENT"}, // a task list and an event calendar
			{"VTODO", "VEVENT,VTODO"},
		} {
			t.Run(fmt.Sprintf("delete %q next to %q", target, others), func(t *testing.T) {
				doomed := ownedCal(1, target)
				repo := &deleteStubRepo{listed: []*calendar.Calendar{doomed}}
				otherEventCalendar := false
				for i, set := range others {
					repo.listed = append(repo.listed, ownedCal(uint(i+2), set))
					otherEventCalendar = otherEventCalendar || eventCapable[set]
				}

				err := NewDeleteCalendarUseCase(repo, nil).Execute(ctx, ownerID, doomed.UUID, confirmed)

				if eventCapable[target] && !otherEventCalendar {
					require.Error(t, err, "the last calendar that can hold events must be protected")
					assert.True(t, errors.Is(err, calendar.ErrInvalidInput), "a 400, as before #170: %v", err)
					assert.Equal(t, lastCalendarMessage, err.Error(), "the message is unchanged")
					assert.Empty(t, repo.deleted, "nothing may be deleted")
					return
				}
				require.NoError(t, err)
				assert.Equal(t, []uint{1}, repo.deleted)
				if !eventCapable[target] {
					assert.Zero(t, repo.listCalls, "a task list is deleted without looking at the other calendars")
				}
			})
		}
	}
}

// TestDeleteCalendar_GuardStory pins the two cases spec §5.1 names: with
// "Personal" and a "Tasks" list, Personal (the last event-capable calendar) is
// protected while the task list exists, and the task list (the user's last)
// can be deleted. Afterwards Personal is still protected on its own.
func TestDeleteCalendar_GuardStory(t *testing.T) {
	ctx := context.Background()
	personal := ownedCal(1, "VEVENT")
	tasks := ownedCal(2, "VTODO")
	repo := &deleteStubRepo{listed: []*calendar.Calendar{personal, tasks}}
	uc := NewDeleteCalendarUseCase(repo, nil)

	err := uc.Execute(ctx, ownerID, personal.UUID, confirmed)
	require.Error(t, err, "last event-capable calendar protected while a task list exists")
	assert.True(t, errors.Is(err, calendar.ErrInvalidInput))
	assert.Equal(t, lastCalendarMessage, err.Error())
	assert.Empty(t, repo.deleted)

	require.NoError(t, uc.Execute(ctx, ownerID, tasks.UUID, confirmed), "last task list deletable")
	assert.Equal(t, []uint{tasks.ID}, repo.deleted)

	repo.listed = []*calendar.Calendar{personal}
	err = uc.Execute(ctx, ownerID, personal.UUID, confirmed)
	assert.True(t, errors.Is(err, calendar.ErrInvalidInput), "still the last calendar: %v", err)
	assert.Equal(t, []uint{tasks.ID}, repo.deleted)
}

// TestDeleteCalendar_GuardCountsOwnedCalendarsOnly: a calendar of another user
// never keeps the guard from firing, even if a repository handed one back
// (the real ListByUserID returns owner rows only; shares are resolved
// elsewhere, see the REST test TestCalendarHandler_DeleteGuardPerKind).
func TestDeleteCalendar_GuardCountsOwnedCalendarsOnly(t *testing.T) {
	ctx := context.Background()
	personal := ownedCal(1, "VEVENT")
	foreign := ownedCal(2, "VEVENT,VTODO")
	foreign.UserID = ownerID + 1
	repo := &deleteStubRepo{listed: []*calendar.Calendar{personal, foreign}}

	err := NewDeleteCalendarUseCase(repo, nil).Execute(ctx, ownerID, personal.UUID, confirmed)
	require.Error(t, err)
	assert.True(t, errors.Is(err, calendar.ErrInvalidInput))
	assert.Equal(t, lastCalendarMessage, err.Error())
	assert.Empty(t, repo.deleted)
}

// TestDeleteCalendar_GuardOrderAndFailures: the guard keeps its place between
// the confirmation and the delete, and a failure to read the user's calendars
// is an internal error (500), never the 400 refusal and never a delete.
func TestDeleteCalendar_GuardOrderAndFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("an unknown or foreign calendar is not found before anything else", func(t *testing.T) {
		foreign := ownedCal(1, "VTODO")
		foreign.UserID = ownerID + 1
		repo := &deleteStubRepo{listed: []*calendar.Calendar{foreign}}
		uc := NewDeleteCalendarUseCase(repo, nil)
		for _, uuid := range []string{foreign.UUID, "missing"} {
			err := uc.Execute(ctx, ownerID, uuid, DeleteCalendarRequest{})
			assert.True(t, errors.Is(err, calendar.ErrNotFound), "%s: %v", uuid, err)
		}
		assert.Zero(t, repo.listCalls)
		assert.Empty(t, repo.deleted)
	})

	t.Run("the confirmation is checked before the guard, for task lists too", func(t *testing.T) {
		for _, set := range []string{"VTODO", "VEVENT"} {
			repo := &deleteStubRepo{listed: []*calendar.Calendar{ownedCal(1, set), ownedCal(2, "VEVENT")}}
			err := NewDeleteCalendarUseCase(repo, nil).Execute(ctx, ownerID, "cal-1", DeleteCalendarRequest{Confirmation: "delete"})
			require.Error(t, err, set)
			assert.True(t, errors.Is(err, calendar.ErrInvalidInput), set)
			assert.Equal(t, "please type DELETE to confirm calendar deletion", err.Error(), set)
			assert.Zero(t, repo.listCalls, set)
			assert.Empty(t, repo.deleted, set)
		}
	})

	t.Run("a failed calendar listing is an internal error and deletes nothing", func(t *testing.T) {
		storage := errors.New("database is locked")
		repo := &deleteStubRepo{listed: []*calendar.Calendar{ownedCal(1, "VEVENT"), ownedCal(2, "VEVENT")}, listErr: storage}
		err := NewDeleteCalendarUseCase(repo, nil).Execute(ctx, ownerID, "cal-1", confirmed)
		require.Error(t, err)
		assert.True(t, errors.Is(err, storage), "the cause is kept for the log")
		assert.False(t, errors.Is(err, calendar.ErrInvalidInput), "not the 400 refusal")
		assert.False(t, errors.Is(err, calendar.ErrNotFound), "not a 404")
		assert.Empty(t, repo.deleted)
	})
}
