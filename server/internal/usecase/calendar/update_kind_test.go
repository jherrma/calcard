package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateStubRepo implements only what the update use case calls: GetByUUID,
// UpdateMetadata and UpdateSupportedComponents. The embedded interface is nil,
// so any other repository call panics. Writes are recorded, not applied, so a
// test sees exactly what the use case asked the repository to persist.
type updateStubRepo struct {
	calendar.CalendarRepository

	cal *calendar.Calendar

	metadataWrites []calendar.Calendar
	setWrites      []setWrite
	setErr         error
}

type setWrite struct {
	id  uint
	set string
}

func (r *updateStubRepo) GetByUUID(_ context.Context, uuid string) (*calendar.Calendar, error) {
	if r.cal == nil || r.cal.UUID != uuid {
		return nil, calendar.ErrNotFound
	}
	cp := *r.cal
	return &cp, nil
}

func (r *updateStubRepo) UpdateMetadata(_ context.Context, cal *calendar.Calendar) error {
	r.metadataWrites = append(r.metadataWrites, *cal)
	return nil
}

func (r *updateStubRepo) UpdateSupportedComponents(_ context.Context, id uint, set string) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.setWrites = append(r.setWrites, setWrite{id, set})
	return nil
}

const ownerID uint = 7

func storedCalendar(set string) *calendar.Calendar {
	return &calendar.Calendar{
		ID: 42, UUID: "cal-uuid", UserID: ownerID, Name: "Work", Color: "#112233",
		Timezone: "UTC", SupportedComponents: set,
	}
}

func strPtr(s string) *string { return &s }

const narrowMessage = "Cannot narrow a calendar's component set"

// TestUpdateCalendar_KindWidenOnly pins the one rule for `kind` on
// PATCH /api/v1/calendars/:id (#170) for every stored set and every kind:
// "both" widens an event calendar or a task list through
// UpdateSupportedComponents, the calendar's own kind is a no-op that writes
// nothing, and every change that drops a component is calendar.ErrConflict
// with nothing written. A legacy "" row already stores both components, so
// "both" leaves it as it is and the single kinds narrow it.
func TestUpdateCalendar_KindWidenOnly(t *testing.T) {
	const (
		noop   = "no-op"
		narrow = "narrow"
	)
	for _, tc := range []struct {
		stored, kind string
		want         string // noop, narrow, or the set to store
	}{
		{"VEVENT", "events", noop},
		{"VEVENT", "both", "VEVENT,VTODO"},
		{"VEVENT", "tasks", narrow},

		{"VTODO", "tasks", noop},
		{"VTODO", "both", "VEVENT,VTODO"},
		{"VTODO", "events", narrow},

		{"VEVENT,VTODO", "both", noop},
		{"VEVENT,VTODO", "events", narrow},
		{"VEVENT,VTODO", "tasks", narrow},

		{"", "both", noop},
		{"", "events", narrow},
		{"", "tasks", narrow},
	} {
		t.Run(tc.stored+" to "+tc.kind, func(t *testing.T) {
			repo := &updateStubRepo{cal: storedCalendar(tc.stored)}
			cal, err := NewUpdateCalendarUseCase(repo).Execute(context.Background(), ownerID, "cal-uuid",
				UpdateCalendarRequest{Kind: tc.kind})

			switch tc.want {
			case narrow:
				require.Error(t, err)
				assert.Nil(t, cal)
				assert.True(t, errors.Is(err, calendar.ErrConflict), "narrowing is a conflict: %v", err)
				assert.False(t, errors.Is(err, calendar.ErrInvalidInput), "not a 400")
				assert.Equal(t, narrowMessage, err.Error())
				assert.Empty(t, repo.setWrites, "a refused narrowing writes nothing")
				assert.Empty(t, repo.metadataWrites, "a refused narrowing writes nothing")
			case noop:
				require.NoError(t, err)
				assert.Equal(t, tc.stored, cal.SupportedComponents, "the stored set is kept as it is")
				assert.Equal(t, tc.kind, calendar.KindOf(cal))
				assert.Empty(t, repo.setWrites, "the calendar's own kind writes no component set")
				assert.Empty(t, repo.metadataWrites, "a kind-only PATCH with the stored kind writes nothing")
			default:
				require.NoError(t, err)
				assert.Equal(t, []setWrite{{42, tc.want}}, repo.setWrites,
					"the widening goes through UpdateSupportedComponents")
				assert.Empty(t, repo.metadataWrites, "a kind-only PATCH writes no metadata")
				assert.Equal(t, tc.want, cal.SupportedComponents, "the response reports the new set")
				assert.Equal(t, calendar.KindBoth, calendar.KindOf(cal))
				assert.Equal(t, "Work", cal.Name, "nothing else changes")
			}
		})
	}
}

// TestUpdateCalendar_KindIsValidated: PATCH validates the same closed enum as
// create (400, the create message), and nothing is written.
func TestUpdateCalendar_KindIsValidated(t *testing.T) {
	for _, stored := range []string{"VEVENT", "VTODO", "VEVENT,VTODO", ""} {
		for _, kind := range []string{"Both", "BOTH", " both", "todo", "VTODO", "VEVENT,VTODO", "journal", "none"} {
			t.Run(stored+" to "+kind, func(t *testing.T) {
				repo := &updateStubRepo{cal: storedCalendar(stored)}
				cal, err := NewUpdateCalendarUseCase(repo).Execute(context.Background(), ownerID, "cal-uuid",
					UpdateCalendarRequest{Name: strPtr("Renamed"), Kind: kind})
				require.Error(t, err)
				assert.Nil(t, cal)
				assert.True(t, errors.Is(err, calendar.ErrInvalidInput), "an unknown kind is a 400: %v", err)
				assert.False(t, errors.Is(err, calendar.ErrConflict))
				assert.Equal(t, `kind must be "events", "tasks" or "both"`, err.Error())
				assert.Empty(t, repo.setWrites)
				assert.Empty(t, repo.metadataWrites, "the valid name is not written either")
			})
		}
	}
}

// TestUpdateCalendar_KindWithOtherFields: the whole request is checked before
// anything is written, and a kind combines with the metadata fields.
func TestUpdateCalendar_KindWithOtherFields(t *testing.T) {
	ctx := context.Background()

	t.Run("rename and widen write both", func(t *testing.T) {
		repo := &updateStubRepo{cal: storedCalendar("VTODO")}
		cal, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid",
			UpdateCalendarRequest{Name: strPtr("Errands"), Kind: "both"})
		require.NoError(t, err)
		require.Len(t, repo.metadataWrites, 1)
		assert.Equal(t, "Errands", repo.metadataWrites[0].Name)
		assert.Equal(t, []setWrite{{42, "VEVENT,VTODO"}}, repo.setWrites)
		assert.Equal(t, "Errands", cal.Name)
		assert.Equal(t, "VEVENT,VTODO", cal.SupportedComponents)
	})

	t.Run("rename with the stored kind writes the rename only", func(t *testing.T) {
		repo := &updateStubRepo{cal: storedCalendar("VTODO")}
		cal, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid",
			UpdateCalendarRequest{Color: strPtr("#abcdef"), Kind: "tasks"})
		require.NoError(t, err)
		require.Len(t, repo.metadataWrites, 1)
		assert.Equal(t, "#abcdef", repo.metadataWrites[0].Color)
		assert.Empty(t, repo.setWrites)
		assert.Equal(t, "VTODO", cal.SupportedComponents)
	})

	t.Run("a narrowing refuses the rename that comes with it", func(t *testing.T) {
		repo := &updateStubRepo{cal: storedCalendar("VEVENT,VTODO")}
		_, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid",
			UpdateCalendarRequest{Name: strPtr("Renamed"), Kind: "events"})
		require.True(t, errors.Is(err, calendar.ErrConflict), "%v", err)
		assert.Empty(t, repo.metadataWrites)
		assert.Empty(t, repo.setWrites)
	})

	t.Run("an invalid field refuses the widening that comes with it", func(t *testing.T) {
		repo := &updateStubRepo{cal: storedCalendar("VEVENT")}
		_, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid",
			UpdateCalendarRequest{Color: strPtr("blue"), Kind: "both"})
		require.True(t, errors.Is(err, calendar.ErrInvalidInput), "%v", err)
		assert.Empty(t, repo.metadataWrites)
		assert.Empty(t, repo.setWrites)
	})

	t.Run("field validation answers before the conflict", func(t *testing.T) {
		repo := &updateStubRepo{cal: storedCalendar("VEVENT,VTODO")}
		_, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid",
			UpdateCalendarRequest{Name: strPtr(""), Kind: "tasks"})
		require.True(t, errors.Is(err, calendar.ErrInvalidInput), "%v", err)
		assert.Equal(t, "name cannot be empty", err.Error())
	})

	t.Run("without a kind the metadata write runs as before", func(t *testing.T) {
		for _, req := range []UpdateCalendarRequest{{}, {Name: strPtr("Renamed")}} {
			repo := &updateStubRepo{cal: storedCalendar("VEVENT")}
			cal, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid", req)
			require.NoError(t, err)
			assert.Len(t, repo.metadataWrites, 1, "an empty body still goes through UpdateMetadata")
			assert.Empty(t, repo.setWrites)
			assert.Equal(t, "VEVENT", cal.SupportedComponents)
		}
	})
}

// TestUpdateCalendar_KindOwnershipAndFailures: ownership is resolved before the
// kind is looked at, and a failed component-set write is never reported as a
// conflict or a validation error.
func TestUpdateCalendar_KindOwnershipAndFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("a calendar the caller does not own is not found, whatever the kind", func(t *testing.T) {
		for _, kind := range []string{"both", "events", "tasks", "bogus"} {
			repo := &updateStubRepo{cal: storedCalendar("VEVENT,VTODO")}
			_, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID+1, "cal-uuid", UpdateCalendarRequest{Kind: kind})
			assert.Equal(t, calendar.ErrNotFound, err, kind)
			_, err = NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "unknown-uuid", UpdateCalendarRequest{Kind: kind})
			assert.Equal(t, calendar.ErrNotFound, err, kind)
			assert.Empty(t, repo.setWrites)
			assert.Empty(t, repo.metadataWrites)
		}
	})

	t.Run("a calendar deleted before the write is not found", func(t *testing.T) {
		repo := &updateStubRepo{cal: storedCalendar("VEVENT"), setErr: calendar.ErrNotFound}
		_, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid", UpdateCalendarRequest{Kind: "both"})
		assert.True(t, errors.Is(err, calendar.ErrNotFound), "%v", err)
	})

	t.Run("a storage failure is an internal error", func(t *testing.T) {
		repo := &updateStubRepo{cal: storedCalendar("VEVENT"), setErr: errors.New("disk full")}
		cal, err := NewUpdateCalendarUseCase(repo).Execute(ctx, ownerID, "cal-uuid", UpdateCalendarRequest{Kind: "both"})
		require.Error(t, err)
		assert.Nil(t, cal)
		for _, typed := range []error{calendar.ErrConflict, calendar.ErrInvalidInput, calendar.ErrNotFound} {
			assert.False(t, errors.Is(err, typed), "must be a 500, not %v", typed)
		}
	})
}

// TestUpdateCalendarRequest_KindFromJSON pins the field name the REST handler
// binds, and that an absent kind and an explicit "" both leave the set alone.
func TestUpdateCalendarRequest_KindFromJSON(t *testing.T) {
	var req UpdateCalendarRequest
	require.NoError(t, json.Unmarshal([]byte(`{"kind":"both"}`), &req))
	assert.Equal(t, "both", req.Kind)

	for _, body := range []string{`{}`, `{"kind":""}`, `{"kind":null}`} {
		req = UpdateCalendarRequest{}
		require.NoError(t, json.Unmarshal([]byte(body), &req))
		repo := &updateStubRepo{cal: storedCalendar("VTODO")}
		cal, err := NewUpdateCalendarUseCase(repo).Execute(context.Background(), ownerID, "cal-uuid", req)
		require.NoError(t, err, body)
		assert.Empty(t, repo.setWrites, body)
		assert.Equal(t, "VTODO", cal.SupportedComponents, body)
	}
}
