package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubCalendarRepo implements only what the create and list use cases call.
// The embedded interface is nil, so any other repository call panics: a test
// that passes proves the use case made no other query (e.g. no task COUNT).
type stubCalendarRepo struct {
	calendar.CalendarRepository

	created     []*calendar.Calendar
	owned       []*calendar.Calendar
	eventCounts map[uint]int64
	countCalls  []uint
}

func (r *stubCalendarRepo) Create(_ context.Context, cal *calendar.Calendar) error {
	cp := *cal
	r.created = append(r.created, &cp)
	return nil
}

func (r *stubCalendarRepo) ListByUserID(context.Context, uint) ([]*calendar.Calendar, error) {
	return r.owned, nil
}

func (r *stubCalendarRepo) GetEventCount(_ context.Context, calendarID uint) (int64, error) {
	r.countCalls = append(r.countCalls, calendarID)
	return r.eventCounts[calendarID], nil
}

// stubShareRepo answers FindCalendarsSharedWithUser only.
type stubShareRepo struct {
	sharing.CalendarShareRepository
	shares []sharing.CalendarShare
}

func (r *stubShareRepo) FindCalendarsSharedWithUser(context.Context, uint) ([]sharing.CalendarShare, error) {
	return r.shares, nil
}

// TestCreateCalendar_Kind pins `kind` on POST /api/v1/calendars (#170): the
// closed enum maps to the stored supported_components, an absent kind is a
// plain event calendar (DECISIONS.md 1), and anything else is
// calendar.ErrInvalidInput (400) that stores nothing.
func TestCreateCalendar_Kind(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		kind, stored, reported string
	}{
		{"events", "VEVENT", calendar.KindEvents},
		{"tasks", "VTODO", calendar.KindTasks},
		{"both", "VEVENT,VTODO", calendar.KindBoth},
		{"", "VEVENT", calendar.KindEvents}, // absent: the default
	} {
		t.Run("kind="+tc.kind, func(t *testing.T) {
			repo := &stubCalendarRepo{}
			cal, err := NewCreateCalendarUseCase(repo).Execute(ctx, 7, CreateCalendarRequest{Name: "Errands", Kind: tc.kind})
			require.NoError(t, err)
			require.Len(t, repo.created, 1)
			assert.Equal(t, tc.stored, repo.created[0].SupportedComponents, "stored set")
			assert.Equal(t, tc.stored, cal.SupportedComponents, "returned calendar")
			assert.Equal(t, tc.reported, calendar.KindOf(cal), "the kind the responses report")
			// The other fields are untouched by kind.
			assert.Equal(t, "Errands", cal.Name)
			assert.Equal(t, uint(7), cal.UserID)
			assert.Equal(t, "UTC", cal.Timezone)
		})
	}

	for _, kind := range []string{"Tasks", "TASKS", " tasks", "task", "todo", "VTODO", "VEVENT,VTODO", "journal", "all", "none"} {
		t.Run("unknown kind="+kind, func(t *testing.T) {
			repo := &stubCalendarRepo{}
			cal, err := NewCreateCalendarUseCase(repo).Execute(ctx, 7, CreateCalendarRequest{Name: "Errands", Kind: kind})
			require.Error(t, err)
			assert.Nil(t, cal)
			assert.True(t, errors.Is(err, calendar.ErrInvalidInput), "an unknown kind is a 400, not a server failure: %v", err)
			assert.Equal(t, `kind must be "events", "tasks" or "both"`, err.Error())
			assert.Empty(t, repo.created, "a refused create stores nothing")
		})
	}

	t.Run("other validation still comes first", func(t *testing.T) {
		repo := &stubCalendarRepo{}
		_, err := NewCreateCalendarUseCase(repo).Execute(ctx, 7, CreateCalendarRequest{Kind: "tasks"})
		require.Error(t, err)
		assert.Equal(t, "name cannot be empty", err.Error())
		assert.Empty(t, repo.created)
	})
}

// TestCreateCalendarRequest_KindFromJSON pins the request field name the REST
// handler binds: "kind".
func TestCreateCalendarRequest_KindFromJSON(t *testing.T) {
	var req CreateCalendarRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Errands","kind":"tasks"}`), &req))
	assert.Equal(t, "tasks", req.Kind)
}

// TestListCalendars_Kind pins `kind` on every listed calendar (#170) — owned
// and shared, legacy "" included — and that the list still runs exactly one
// COUNT per calendar: no task count was added next to the event count.
func TestListCalendars_Kind(t *testing.T) {
	repo := &stubCalendarRepo{
		owned: []*calendar.Calendar{
			{ID: 1, UUID: "events", SupportedComponents: "VEVENT"},
			{ID: 2, UUID: "tasks", SupportedComponents: "VTODO"},
			{ID: 3, UUID: "mixed", SupportedComponents: "VEVENT,VTODO"},
			{ID: 4, UUID: "legacy", SupportedComponents: ""},
		},
		eventCounts: map[uint]int64{1: 5, 2: 0, 3: 2, 4: 1, 5: 3},
	}
	shares := &stubShareRepo{shares: []sharing.CalendarShare{{
		Permission: "read",
		Calendar: calendar.Calendar{
			ID: 5, UUID: "shared-tasks", SupportedComponents: "VTODO",
			Owner: user.User{UUID: "owner-uuid", DisplayName: "Owner"},
		},
	}}}

	got, err := NewListCalendarsUseCase(repo, shares).Execute(context.Background(), 9)
	require.NoError(t, err)

	want := map[string]struct {
		kind   string
		count  int64
		shared bool
	}{
		"events":       {calendar.KindEvents, 5, false},
		"tasks":        {calendar.KindTasks, 0, false},
		"mixed":        {calendar.KindBoth, 2, false},
		"legacy":       {calendar.KindBoth, 1, false},
		"shared-tasks": {calendar.KindTasks, 3, true},
	}
	require.Len(t, got, len(want))
	for _, cal := range got {
		w, ok := want[cal.UUID]
		require.True(t, ok, cal.UUID)
		assert.Equal(t, w.kind, cal.Kind, cal.UUID)
		assert.Equal(t, w.count, cal.EventCount, cal.UUID)
		assert.Equal(t, w.shared, cal.Shared, cal.UUID)
	}
	assert.ElementsMatch(t, []uint{1, 2, 3, 4, 5}, repo.countCalls,
		"exactly one COUNT per calendar, the event count; no task count")

	// The JSON shape every consumer (REST list, /search, the SPA) reads.
	b, err := json.Marshal(got[1])
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(b, &fields))
	assert.Equal(t, "tasks", fields["kind"])
	assert.Equal(t, "VTODO", fields["supported_components"], "supported_components stays for compatibility")
	assert.Equal(t, float64(0), fields["event_count"])
	assert.NotContains(t, fields, "task_count")
}

// TestNewCalendarWithKind pins the shape of the REST create, get and update
// responses: the calendar's own fields, unchanged, plus a read-only kind.
func TestNewCalendarWithKind(t *testing.T) {
	cal := &calendar.Calendar{ID: 3, UUID: "cal-uuid", Name: "Errands", SupportedComponents: "VTODO"}
	b, err := json.Marshal(NewCalendarWithKind(cal))
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(b, &fields))
	assert.Equal(t, "tasks", fields["kind"])
	assert.Equal(t, "VTODO", fields["supported_components"])
	assert.Equal(t, "cal-uuid", fields["uuid"])
	assert.Equal(t, "Errands", fields["name"])
	assert.Equal(t, float64(3), fields["id"])

	plain, err := json.Marshal(cal)
	require.NoError(t, err)
	var plainFields map[string]any
	require.NoError(t, json.Unmarshal(plain, &plainFields))
	delete(fields, "kind")
	assert.Equal(t, plainFields, fields, "kind is the only field added")
}
