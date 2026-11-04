package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	authadapter "github.com/jherrma/caldav-server/internal/adapter/auth"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalendarHandler_DeleteGuardPerKind pins the per-kind last-calendar guard
// on DELETE /api/v1/calendars/:id (#170, spec §5 rule 6) against the real
// repositories: the user's last OWNED calendar that supports VEVENT is refused
// as before (400, same body) while a task list exists; an event-capable
// calendar goes when another owned one remains (mixed and legacy rows count);
// a VTODO-only task list always goes, the user's last one included; and
// calendars shared WITH the user do not count as theirs.
func TestCalendarHandler_DeleteGuardPerKind(t *testing.T) {
	app, db, cfg := setupTestApp(t)
	ctx := context.Background()
	gdb := db.DB()
	userRepo := repository.NewUserRepository(gdb)
	calRepo := repository.NewCalendarRepository(gdb)
	shareRepo := repository.NewCalendarShareRepository(gdb)
	jwtManager := authadapter.NewJWTManager(&cfg.JWT)

	type account struct {
		u     *user.User
		token string
	}
	mkUser := func(t *testing.T, name string) account {
		u := &user.User{
			Email: name + "@example.com", Username: name, PasswordHash: "hash",
			IsActive: true, EmailVerified: true, UUID: name + "-uuid", DisplayName: name,
		}
		require.NoError(t, userRepo.Create(ctx, u))
		token, _, err := jwtManager.GenerateAccessToken(u.UUID, u.Email)
		require.NoError(t, err)
		return account{u, token}
	}

	seq := 0
	mkCalendar := func(t *testing.T, owner account, name, set string) *calendar.Calendar {
		seq++
		uid := fmt.Sprintf("guard-cal-%d", seq)
		cal := &calendar.Calendar{
			UUID: uid, UserID: owner.u.ID, Name: name, Path: uid + ".ics",
			Timezone: "UTC", Color: "#112233", SupportedComponents: set,
		}
		require.NoError(t, calRepo.Create(ctx, cal))
		return cal
	}
	share := func(t *testing.T, cal *calendar.Calendar, with account, perm string) {
		seq++
		require.NoError(t, shareRepo.Create(ctx, &sharing.CalendarShare{
			UUID: fmt.Sprintf("guard-share-%d", seq), CalendarID: cal.ID, SharedWithID: with.u.ID, Permission: perm,
		}))
	}

	del := func(t *testing.T, caller account, cal *calendar.Calendar) (int, string) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/calendars/"+cal.UUID,
			bytes.NewReader([]byte(`{"confirmation":"DELETE"}`)))
		req.Header.Set("Authorization", "Bearer "+caller.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	exists := func(t *testing.T, cal *calendar.Calendar) bool {
		_, err := calRepo.GetByUUID(ctx, cal.UUID)
		if errors.Is(err, calendar.ErrNotFound) {
			return false
		}
		require.NoError(t, err)
		return true
	}
	// refused asserts the pre-#170 refusal, byte for byte, and that the
	// calendar is still there.
	refused := func(t *testing.T, caller account, cal *calendar.Calendar, why string) {
		t.Helper()
		status, raw := del(t, caller, cal)
		assert.Equal(t, fiber.StatusBadRequest, status, "%s: %s", why, raw)
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &body), raw)
		assert.Equal(t, map[string]any{"status": "error", "message": "cannot delete your last calendar"}, body, why)
		assert.True(t, exists(t, cal), "%s: the calendar must survive", why)
	}
	deleted := func(t *testing.T, caller account, cal *calendar.Calendar, why string) {
		t.Helper()
		status, raw := del(t, caller, cal)
		assert.Equal(t, fiber.StatusNoContent, status, "%s: %s", why, raw)
		assert.False(t, exists(t, cal), "%s: the calendar must be gone", why)
	}
	owned := func(t *testing.T, a account) int64 {
		n, err := calRepo.CountByUserID(ctx, a.u.ID)
		require.NoError(t, err)
		return n
	}

	t.Run("last event-capable calendar protected while a task list exists", func(t *testing.T) {
		a := mkUser(t, "guardpersonal")
		personal := mkCalendar(t, a, "Personal", "VEVENT")
		tasks := mkCalendar(t, a, "Tasks", "VTODO")

		refused(t, a, personal, "Personal next to a task list")
		deleted(t, a, tasks, "the user's last task list")
		refused(t, a, personal, "Personal on its own")
		assert.Equal(t, int64(1), owned(t, a))
	})

	t.Run("last task list deletable when it is the only calendar", func(t *testing.T) {
		a := mkUser(t, "guardonlytasks")
		tasks := mkCalendar(t, a, "Tasks", "VTODO")
		deleted(t, a, tasks, "a user's only calendar that is a task list")
		assert.Zero(t, owned(t, a), "the user may end up with no calendar at all")
	})

	t.Run("an event-capable calendar goes when another owned one remains", func(t *testing.T) {
		a := mkUser(t, "guardseveral")
		personal := mkCalendar(t, a, "Personal", "VEVENT")
		mixed := mkCalendar(t, a, "Mixed", "VEVENT,VTODO")
		legacy := mkCalendar(t, a, "Legacy", "") // pre-#170 row: stores both
		tasks := mkCalendar(t, a, "Tasks", "VTODO")
		moreTasks := mkCalendar(t, a, "More tasks", "VTODO")

		deleted(t, a, personal, "an event calendar next to a mixed and a legacy one")
		deleted(t, a, legacy, "a legacy calendar next to a mixed one")
		refused(t, a, mixed, "the mixed calendar is now the last that can hold events")
		deleted(t, a, tasks, "a task list next to another task list")
		deleted(t, a, moreTasks, "the last task list")
		refused(t, a, mixed, "the mixed calendar on its own")
	})

	t.Run("a mixed or legacy calendar keeps an event calendar deletable", func(t *testing.T) {
		for _, set := range []string{"VEVENT,VTODO", ""} {
			a := mkUser(t, fmt.Sprintf("guardkeeps%d", seq))
			personal := mkCalendar(t, a, "Personal", "VEVENT")
			other := mkCalendar(t, a, "Other", set)
			deleted(t, a, personal, fmt.Sprintf("Personal next to %q", set))
			refused(t, a, other, fmt.Sprintf("%q is now the last", set))
		}
	})

	t.Run("calendars shared with the user do not count as theirs", func(t *testing.T) {
		a := mkUser(t, "guardsharee")
		b := mkUser(t, "guardsharer")
		personal := mkCalendar(t, a, "Personal", "VEVENT")
		tasks := mkCalendar(t, a, "Tasks", "VTODO")
		team := mkCalendar(t, b, "Team", "VEVENT")
		family := mkCalendar(t, b, "Family", "VEVENT,VTODO")
		share(t, team, a, "read-write")
		share(t, family, a, "read")

		refused(t, a, personal, "Personal next to two calendars shared with the user")

		// The sharer's own guard looks at their own calendars only; sharing a
		// calendar out does not change it.
		deleted(t, b, team, "the sharer's event calendar next to another of theirs")
		refused(t, b, family, "the sharer's last event-capable calendar")

		// And the sharee's task list is still deletable.
		deleted(t, a, tasks, "the sharee's task list")
		refused(t, a, personal, "Personal still the last")
	})

	t.Run("an owned subscription counts as an event-capable calendar", func(t *testing.T) {
		// A subscribed mirror is owned and stores VEVENT; the guard counted it
		// before #170 (it counted every calendar) and still does.
		a := mkUser(t, "guardsubscriber")
		personal := mkCalendar(t, a, "Personal", "VEVENT")
		seq++
		require.NoError(t, calRepo.Create(ctx, &calendar.Calendar{
			UUID: fmt.Sprintf("guard-sub-%d", seq), UserID: a.u.ID, Name: "Holidays", Path: fmt.Sprintf("guard-sub-%d.ics", seq),
			Timezone: "UTC", Color: "#112233", SupportedComponents: "VEVENT,VTODO", Subscribed: true,
		}))
		deleted(t, a, personal, "Personal next to an owned subscription")
	})
}
